// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prworktype

import (
	"context"
	"errors"
	"fmt"
)

// Routing thresholds: accept the embedding when top score >= MinScore AND
// margin >= MinMargin, else escalate.
const (
	DefaultMinScore  = 0.35
	DefaultMinMargin = 0.08
)

// Classifier composes the cascade over the model server:
// type = rule → embedding → LLM ; domain = embedding → LLM (nullable).
type Classifier struct {
	Client    *ModelClient
	MinScore  float64
	MinMargin float64
	TopK      int  // 0 = full taxonomy on escalation (best on this centroid)
	UseLLM    bool // false → embedding-only with honest abstention
}

// NewClassifier builds a classifier over the model client, with the default routing thresholds.
func NewClassifier(client *ModelClient, useLLM bool) *Classifier {
	return &Classifier{Client: client, MinScore: DefaultMinScore, MinMargin: DefaultMinMargin, UseLLM: useLLM}
}

// minScore / minMargin fall back to the defaults for a non-positive threshold, so
// a zero-value or partially-built Classifier abstains on weak signal instead of
// silently accepting every candidate (a threshold of 0 accepts almost anything).
func (c *Classifier) minScore() float64 {
	if c.MinScore <= 0 {
		return DefaultMinScore
	}
	return c.MinScore
}

func (c *Classifier) minMargin() float64 {
	if c.MinMargin <= 0 {
		return DefaultMinMargin
	}
	return c.MinMargin
}

// Input is one PR's signals.
type Input struct {
	Title       string
	Branch      string
	AuthorLogin string
	Files       []string
	Body        string
}

func (c *Classifier) candidates(cands []Candidate) []string {
	n := len(cands)
	if c.TopK > 0 && c.TopK < n {
		n = c.TopK
	}
	out := make([]string, 0, n)
	for _, cd := range cands[:n] {
		out = append(out, cd.Bucket)
	}
	return out
}

// resolve runs one axis's cascade. It returns an error only when the model
// server fails or answers outside the taxonomy; a confident-but-absent outcome
// is an abstention (MethodAbstain), not an error.
func (c *Classifier) resolve(ctx context.Context, axis string, in Input, allowNull bool) (AxisResult, error) {
	emb, err := c.Client.EmbedClassify(ctx, axis, in.Title, in.Branch)
	if err != nil {
		return AxisResult{}, fmt.Errorf("embed-classify %s: %w", axis, err)
	}
	if len(emb.Candidates) == 0 {
		return AxisResult{Method: MethodAbstain}, nil
	}
	top := emb.Candidates[0]
	if emb.TopScore >= c.minScore() && emb.Margin >= c.minMargin() {
		return AxisResult{Bucket: top.Bucket, Method: MethodEmbedding, Score: top.Score}, nil
	}
	if !c.UseLLM {
		return AxisResult{Method: MethodAbstain}, nil
	}
	pick, available, err := c.Client.LLMClassify(ctx, axis, in.Title, in.Branch,
		c.candidates(emb.Candidates), allowNull, in.Files, in.Body)
	if err != nil {
		return AxisResult{}, fmt.Errorf("llm-classify %s: %w", axis, err)
	}
	if !available {
		// The server has no LLM configured — degrade to an honest abstention
		// rather than mislabel the embedding's unsure guess as a confident one.
		return AxisResult{Method: MethodAbstain}, nil
	}
	if pick != "" {
		if !validBucket(axis, pick) {
			return AxisResult{}, fmt.Errorf("llm-classify %s: bucket %q is outside the taxonomy", axis, pick)
		}
		return AxisResult{Bucket: pick, Method: MethodLLM}, nil
	}
	if allowNull {
		// domain: the LLM found no domain strong enough to assign — a real null.
		return AxisResult{Method: MethodLLM}, nil
	}
	// type: the LLM declined to pick though asked to. Fall back to the embedding's
	// top bucket, labelled embedding and carrying its (sub-threshold) score so a
	// reader sees the low confidence behind the answer.
	return AxisResult{Bucket: top.Bucket, Method: MethodEmbedding, Score: top.Score}, nil
}

// Classify runs the full per-axis cascade for one PR. It returns an error only
// when the model server fails (unreachable, 5xx, malformed, or an out-of-
// taxonomy answer); a confident-but-absent result is an abstention, not an error.
func (c *Classifier) Classify(ctx context.Context, in Input) (Classification, error) {
	if c.Client == nil {
		return Classification{}, errors.New("prworktype: Classifier has a nil model client")
	}
	var typeRes AxisResult
	if rule := classifyTypeRule(in.Title, in.Branch, in.AuthorLogin); rule != "" {
		typeRes = AxisResult{Bucket: rule, Method: MethodRule}
	} else {
		var err error
		if typeRes, err = c.resolve(ctx, "type", in, false); err != nil {
			return Classification{}, err
		}
	}
	domainRes, err := c.resolve(ctx, "domain", in, true)
	if err != nil {
		return Classification{}, err
	}
	return Classification{Type: typeRes, Domain: domainRes}, nil
}
