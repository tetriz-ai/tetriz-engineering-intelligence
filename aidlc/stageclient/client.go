// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package stageclient asks a running model-service which lifecycle stage each AI-harness
// artifact serves, and turns its answers into the stage map fold.Facts.StageMap reads.
//
// It is a thin HTTP client over model-service's POST /classify-stage: the service runs the model, the
// three-sample strict-majority vote and the bucket-to-stage mapping. The package lives apart from
// aidlc and aidlc/fold so that those stay free of network calls; it imports only the generated
// aidlc/v1 types, and its StageMap result is the map type fold.Facts.StageMap already is.
package stageclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"unicode/utf8"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// DefaultMaxPerRequest is the service's default cap on artifacts per request. Classify sends each
// distinct artifact once and splits more distinct artifacts than this into requests of at most
// this many.
const DefaultMaxPerRequest = 250

// MaxTextLen is the most characters the service accepts in one artifact's Description or Body.
// Classify cuts a longer one to this many characters, on a character boundary, before sending,
// so one long file does not fail the whole request.
const MaxTextLen = 50_000

// Artifact is one AI-harness artifact to classify. Only skills, agents and commands are sent to
// the model; any other type comes back Skipped.
type Artifact struct {
	// ID is the caller's artifact id, the StageMap key. It must be unique in one Classify call.
	ID   string
	Name string
	Type aidlcv1.HarnessArtifactType
	// Description and Body are optional; the service redacts both before the model reads them.
	// Each is sent cut to its first MaxTextLen characters.
	Description string
	Body        string
}

// Outcome says whether an artifact got a verdict.
type Outcome string

// The outcomes the service reports. Only Succeeded carries a stage.
const (
	// Succeeded means a strict majority of the samples agreed on a bucket.
	Succeeded Outcome = "SUCCEEDED"
	// Unresolved means every sample voted and no bucket held a majority.
	Unresolved Outcome = "UNRESOLVED"
	// Deferred means a sample's call failed and the votes that arrived held no majority.
	Deferred Outcome = "DEFERRED"
	// Skipped means the type is not authored practice (skill, agent or command), so it was not sent.
	Skipped Outcome = "SKIPPED"
)

// Result is one artifact's answer.
type Result struct {
	ID      string
	Outcome Outcome
	// Bucket is the model's bucket (a stage, or an exclusion such as "x_gtm" or "none"); empty
	// unless Succeeded.
	Bucket string
	// Stage is the lifecycle stage when Succeeded. AIDLC_STAGE_UNSPECIFIED is then a definitive
	// "serves no stage".
	Stage aidlcv1.AidlcStage
	// Votes is the bucket each sample voted, in sample order; a failed sample leaves no vote.
	Votes []string
}

// Classification is the answer for every artifact, in input order.
type Classification struct {
	Results []Result
	// FailedCalls counts the model calls whose reply could not be used, across all requests.
	FailedCalls int
}

// Client calls a model-service.
type Client struct {
	BaseURL string
	HTTP    *http.Client
	// MaxPerRequest caps the artifacts in one request; zero means DefaultMaxPerRequest.
	MaxPerRequest int
}

// NewClient returns a client for the model-service at baseURL. One request makes three model
// calls per batch of 25 artifacts, so the HTTP timeout is generous.
func NewClient(baseURL string) *Client {
	return &Client{BaseURL: baseURL, HTTP: &http.Client{Timeout: 10 * time.Minute}}
}

type wireArtifact struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Body        string `json:"body,omitempty"`
}

type wireResult struct {
	ID      string   `json:"id"`
	Outcome string   `json:"outcome"`
	Bucket  *string  `json:"bucket"`
	Stage   *string  `json:"stage"`
	Votes   []string `json:"votes"`
}

type wireResponse struct {
	Results     []wireResult `json:"results"`
	FailedCalls int          `json:"failed_calls"`
}

// Classify asks the service for every artifact's stage. Artifacts with the same type, name,
// description and body are sent once and share that one verdict, so copies of one artifact never
// disagree. Results come back one per input artifact, in input order. An error means a request
// failed or the service answered something this client cannot read; no partial answer is
// returned then.
func (c *Client) Classify(ctx context.Context, artifacts []Artifact) (Classification, error) {
	seen := make(map[string]bool, len(artifacts))
	for _, a := range artifacts {
		if seen[a.ID] {
			return Classification{}, fmt.Errorf("artifact id %q appears more than once", a.ID)
		}
		seen[a.ID] = true
	}
	limit := c.MaxPerRequest
	if limit <= 0 {
		limit = DefaultMaxPerRequest
	}
	distinct, groupOf := dedupe(artifacts)
	verdicts := make([]Result, 0, len(distinct))
	var failed int
	for start := 0; start < len(distinct); start += limit {
		chunk := distinct[start:min(start+limit, len(distinct))]
		resp, err := c.post(ctx, chunk)
		if err != nil {
			return Classification{}, err
		}
		results, err := decode(chunk, resp)
		if err != nil {
			return Classification{}, err
		}
		verdicts = append(verdicts, results...)
		failed += resp.FailedCalls
	}
	out := Classification{Results: make([]Result, len(artifacts)), FailedCalls: failed}
	for i, a := range artifacts {
		r := verdicts[groupOf[i]]
		r.ID = a.ID
		r.Votes = append([]string(nil), r.Votes...)
		out.Results[i] = r
	}
	return out, nil
}

// contentKey is what the model reads for one artifact: its type, name, and the description and
// body as sent. Two artifacts with the same key are one classification, as in the production
// classifier, whose reuse digest is type, name and rendered body.
type contentKey struct {
	typ              aidlcv1.HarnessArtifactType
	name, desc, body string
}

// dedupe returns one artifact per distinct content key, in first-seen order, and for each input
// artifact the index of its group's representative among them. Each copy of an artifact (one per
// person who installed it) is then classified once and gets that one verdict, rather than each
// copy drawing its own and the copies disagreeing.
func dedupe(artifacts []Artifact) ([]Artifact, []int) {
	index := make(map[contentKey]int, len(artifacts))
	var distinct []Artifact
	groupOf := make([]int, len(artifacts))
	for i, a := range artifacts {
		k := contentKey{a.Type, a.Name, truncate(a.Description, MaxTextLen), truncate(a.Body, MaxTextLen)}
		g, ok := index[k]
		if !ok {
			g = len(distinct)
			index[k] = g
			distinct = append(distinct, a)
		}
		groupOf[i] = g
	}
	return distinct, groupOf
}

// BuildStageMap classifies the artifacts and returns their stage map; see StageMap.
func (c *Client) BuildStageMap(ctx context.Context, artifacts []Artifact) (map[string]aidlcv1.AidlcStage, error) {
	got, err := c.Classify(ctx, artifacts)
	if err != nil {
		return nil, err
	}
	return StageMap(got.Results), nil
}

// StageMap keeps the Succeeded results, keyed by artifact id, for fold.Facts.StageMap. Every other
// outcome is left out: an absent artifact is one whose stage is unknown, which is not the same as
// AIDLC_STAGE_UNSPECIFIED's "serves no stage".
func StageMap(results []Result) map[string]aidlcv1.AidlcStage {
	m := make(map[string]aidlcv1.AidlcStage, len(results))
	for _, r := range results {
		if r.Outcome == Succeeded {
			m[r.ID] = r.Stage
		}
	}
	return m
}

func (c *Client) post(ctx context.Context, chunk []Artifact) (wireResponse, error) {
	req := struct {
		Artifacts []wireArtifact `json:"artifacts"`
	}{Artifacts: make([]wireArtifact, len(chunk))}
	for i, a := range chunk {
		req.Artifacts[i] = wireArtifact{
			ID: a.ID, Name: a.Name, Type: a.Type.String(),
			Description: truncate(a.Description, MaxTextLen), Body: truncate(a.Body, MaxTextLen),
		}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return wireResponse{}, fmt.Errorf("encoding the classify-stage request: %w", err)
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/classify-stage", bytes.NewReader(body))
	if err != nil {
		return wireResponse{}, fmt.Errorf("building the classify-stage request: %w", err)
	}
	hreq.Header.Set("Content-Type", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(hreq)
	if err != nil {
		return wireResponse{}, fmt.Errorf("calling classify-stage: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return wireResponse{}, fmt.Errorf("/classify-stage: status %d", resp.StatusCode)
	}
	var out wireResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return wireResponse{}, fmt.Errorf("decoding the classify-stage response: %w", err)
	}
	return out, nil
}

// decode maps the service's results back onto the request, insisting on one result per artifact.
func decode(chunk []Artifact, resp wireResponse) ([]Result, error) {
	if len(resp.Results) != len(chunk) {
		return nil, fmt.Errorf("/classify-stage: %d results for %d artifacts", len(resp.Results), len(chunk))
	}
	out := make([]Result, len(chunk))
	for i, w := range resp.Results {
		if w.ID != chunk[i].ID {
			return nil, fmt.Errorf("/classify-stage: result %d is for %q, want %q", i, w.ID, chunk[i].ID)
		}
		r := Result{ID: w.ID, Outcome: Outcome(w.Outcome), Votes: w.Votes}
		switch r.Outcome {
		case Succeeded:
			if w.Stage == nil || w.Bucket == nil {
				return nil, fmt.Errorf("/classify-stage: %q succeeded without a stage", w.ID)
			}
			v, ok := aidlcv1.AidlcStage_value[*w.Stage]
			if !ok {
				return nil, fmt.Errorf("/classify-stage: %q has unknown stage %q", w.ID, *w.Stage)
			}
			r.Stage, r.Bucket = aidlcv1.AidlcStage(v), *w.Bucket
		case Unresolved, Deferred, Skipped:
		default:
			return nil, fmt.Errorf("/classify-stage: %q has unknown outcome %q", w.ID, w.Outcome)
		}
		out[i] = r
	}
	return out, nil
}

// truncate returns s's first n characters, cut at a character boundary. Characters are counted as
// the service counts them, one per Unicode code point.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	i := 0
	for count := 0; count < n; count++ {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
	}
	return s[:i]
}
