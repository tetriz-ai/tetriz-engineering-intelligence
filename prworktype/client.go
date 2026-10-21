// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prworktype

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ModelClient calls the Python model server's two endpoints (the model layers).
type ModelClient struct {
	BaseURL string
	HTTP    *http.Client
}

// NewModelClient returns a client for the model-service model server at baseURL.
func NewModelClient(baseURL string) *ModelClient {
	return &ModelClient{BaseURL: baseURL, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// StatusError is returned when the model server answers with a non-200 status,
// so a caller can tell a deliberate 503 (the LLM layer is not configured) from
// a genuine failure.
type StatusError struct {
	Path string
	Code int
}

func (e *StatusError) Error() string { return fmt.Sprintf("%s: status %d", e.Path, e.Code) }

// Candidate is one ranked bucket from /embed-classify.
type Candidate struct {
	Bucket string  `json:"bucket"`
	Score  float64 `json:"score"`
}

// EmbedResp is the /embed-classify response.
type EmbedResp struct {
	Candidates []Candidate `json:"candidates"`
	TopScore   float64     `json:"top_score"`
	Margin     float64     `json:"margin"`
}

// httpClient guards against a zero-value ModelClient built without NewModelClient:
// a nil HTTP field falls back to the shared default client rather than panicking.
func (c *ModelClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *ModelClient) post(ctx context.Context, path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return &StatusError{Path: path, Code: resp.StatusCode}
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s: decoding response: %w", path, err)
	}
	return nil
}

// EmbedClassify ranks the axis's buckets for one PR.
func (c *ModelClient) EmbedClassify(ctx context.Context, axis, title, branch string) (EmbedResp, error) {
	var out EmbedResp
	err := c.post(ctx, "/embed-classify",
		map[string]any{"axis": axis, "title": title, "branch": branch}, &out)
	return out, err
}

// LLMClassify asks the LLM to pick one candidate (or none) for the axis. It
// separates the three outcomes the cascade must treat differently:
//
//   - available == false: the server has no LLM configured (HTTP 503). The
//     caller degrades to an honest abstention — this is not a failure.
//   - err != nil: a transport or server failure (timeout, 5xx, bad JSON, or a
//     404 from a mis-built URL). The caller surfaces it instead of guessing.
//   - pick == "" with err == nil and available: the LLM made no pick (no
//     confident bucket, or a null domain).
func (c *ModelClient) LLMClassify(ctx context.Context, axis, title, branch string,
	candidates []string, allowNull bool, files []string, body string) (pick string, available bool, err error) {
	var out struct {
		Bucket *string `json:"bucket"`
	}
	err = c.post(ctx, "/llm-classify", map[string]any{
		"axis": axis, "title": title, "branch": branch, "candidates": candidates,
		"allow_null": allowNull, "files": files, "body": body,
	}, &out)
	if err != nil {
		var se *StatusError
		if errors.As(err, &se) && se.Code == http.StatusServiceUnavailable {
			return "", false, nil
		}
		return "", true, err
	}
	if out.Bucket == nil {
		return "", true, nil
	}
	return *out.Bucket, true, nil
}
