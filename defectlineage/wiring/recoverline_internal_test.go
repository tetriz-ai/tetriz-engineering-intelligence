// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package wiring

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	connv1 "github.com/tetriz-ai/tetriz-connectors/gen/lineage/v1"
	connlineage "github.com/tetriz-ai/tetriz-connectors/sdk/lineage"
)

// TestTheRecoveredLineMatchesTheRemovedText: the culprit touched two places, and
// the fix removed the second. Taking the first added line measures structural
// distance between the fix and an unrelated part of the same commit, which is a
// measured-looking verdict about the wrong region.
func TestTheRecoveredLineMatchesTheRemovedText(t *testing.T) {
	added := connlineage.CulpritAddedLinesFn(
		func(context.Context, string, string) ([]connlineage.AddedLine, error) {
			return []connlineage.AddedLine{
				{Text: "func unrelated() {}", NewLine: 2},
				{Text: "  return a / b", NewLine: 7}, // indented differently from the diff
			}, nil
		})

	fix := &connv1.PendingStructuralScoring{FixAnchors: []*connv1.LineageBlameAnchor{
		{Path: "m.go", StartLine: 1, RemovedText: []string{"return a / b"}},
	}}

	got := recoverLine(context.Background(), added, "CULPRIT", "m.go", removedTextOf(fix))
	assert.EqualValues(t, 7, got,
		"the added line matching the removed text, compared without whitespace")
}

// TestTheFirstAddedLineIsTheFallback: nothing matches, so the walk's own choice
// of representative — the first substantive line — still gives an answer.
func TestTheFirstAddedLineIsTheFallback(t *testing.T) {
	added := connlineage.CulpritAddedLinesFn(
		func(context.Context, string, string) ([]connlineage.AddedLine, error) {
			return []connlineage.AddedLine{
				{Text: "", NewLine: 1},
				{Text: "something else entirely", NewLine: 4},
			}, nil
		})

	fix := &connv1.PendingStructuralScoring{FixAnchors: []*connv1.LineageBlameAnchor{
		{Path: "m.go", StartLine: 1, RemovedText: []string{"return a / b"}},
	}}

	got := recoverLine(context.Background(), added, "CULPRIT", "m.go", removedTextOf(fix))
	assert.EqualValues(t, 4, got, "the first substantive added line")
}

// TestNoLookupRecoversNothing: a host that supplies no lookup degrades rather
// than measuring at a guessed line.
func TestNoLookupRecoversNothing(t *testing.T) {
	got := recoverLine(context.Background(), nil, "CULPRIT", "m.go", nil)
	assert.Zero(t, got)
}
