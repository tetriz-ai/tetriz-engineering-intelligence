// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package prworktype is a Go orchestrator for the PR Work-Type classifier.
//
// It runs the classifier's Go side: Layer-0 rules + routing (+ optional
// storage) in Go, calling the Python model server's /embed-classify and
// /llm-classify endpoints over HTTP for the embedding and LLM layers. The model
// itself stays in Python (SBERT); this package does everything around it.
package prworktype

// Method records how an axis was decided (the per-axis provenance).
type Method string

// The methods an axis can be decided by: one per cascade layer, plus abstain.
const (
	MethodRule      Method = "rule"      // Layer 0 rule (bot author / cherry-pick)
	MethodEmbedding Method = "embedding" // Layer 1 centroid, confident
	MethodLLM       Method = "llm"       // Layer 2 LLM disambiguation
	MethodAbstain   Method = "abstain"   // no confident answer and no LLM
)

// Non-taxonomy type markers the Layer-0 rule can emit.
const (
	Uncategorised = "uncategorised"
	CherryPick    = "cherry-pick"
)

// AxisResult is one axis's verdict. Bucket == "" means none (domain) or abstain.
type AxisResult struct {
	Bucket string  `json:"bucket"`
	Method Method  `json:"method"`
	Score  float64 `json:"score,omitempty"`
}

// Classification is the full per-PR result.
type Classification struct {
	Type   AxisResult `json:"type"`
	Domain AxisResult `json:"domain"`
}
