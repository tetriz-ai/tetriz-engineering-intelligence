// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"testing"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"

	"github.com/stretchr/testify/assert"
)

const (
	srcCommand = attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND
	srcVendor  = attributionv1.SignalSource_SIGNAL_SOURCE_VENDOR_FIELD
	srcProse   = attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_PROSE
	srcLocal   = attributionv1.SignalSource_SIGNAL_SOURCE_LOCAL_GIT_WINDOWED
	srcUnknown = attributionv1.SignalSource_SIGNAL_SOURCE_UNSPECIFIED
)

func sig(name string, src attributionv1.SignalSource) *attributionv1.BranchSignal {
	return &attributionv1.BranchSignal{Name: name, Source: src}
}

// Pins the rank as a strict total order. What a future author needs is not that
// prose beats a local-git read (that follows declaration order) but that the
// ordering is explicit per value, so appending a strong source cannot silently
// land it at the weak end.
func TestBranchSourceRank_StrictTotalOrder(t *testing.T) {
	t.Parallel()
	ordered := []attributionv1.SignalSource{srcCommand, srcVendor, srcProse, srcLocal, srcUnknown}
	for i := 0; i < len(ordered)-1; i++ {
		assert.Greater(t, branchSourceRank(ordered[i]), branchSourceRank(ordered[i+1]),
			"%v must outrank %v", ordered[i], ordered[i+1])
	}
}

func TestCollectBranches_MergesRatherThanTakingPrecedence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		um   *usageMeta
		want map[string]attributionv1.SignalSource
	}{
		{
			name: "stamped signals keep their source",
			um:   &usageMeta{BranchSignals: []*attributionv1.BranchSignal{sig("feat/a", srcCommand)}},
			want: map[string]attributionv1.SignalSource{"feat/a": srcCommand},
		},
		{
			name: "the git_branches field enters unclassified, not as ambient",
			um:   &usageMeta{GitBranches: []string{"feat/a"}},
			want: map[string]attributionv1.SignalSource{"feat/a": srcUnknown},
		},
		{
			// What the merge exists for. A producer that stamps one branch and not
			// the other must not lose the second: a precedence rule would drop
			// feat/b entirely.
			name: "a producer stamping only some branches loses none",
			um: &usageMeta{
				BranchSignals: []*attributionv1.BranchSignal{sig("feat/a", srcCommand)},
				GitBranches:   []string{"feat/a", "feat/b"},
			},
			want: map[string]attributionv1.SignalSource{"feat/a": srcCommand, "feat/b": srcUnknown},
		},
		{
			name: "same branch from two sources keeps the stronger",
			um: &usageMeta{
				BranchSignals: []*attributionv1.BranchSignal{sig("feat/a", srcLocal), sig("feat/a", srcProse)},
			},
			want: map[string]attributionv1.SignalSource{"feat/a": srcProse},
		},
		{
			// Order must not decide the winner.
			name: "stronger source first is not overwritten by a weaker one",
			um: &usageMeta{
				BranchSignals: []*attributionv1.BranchSignal{sig("feat/a", srcCommand), sig("feat/a", srcLocal)},
			},
			want: map[string]attributionv1.SignalSource{"feat/a": srcCommand},
		},
		{
			name: "the git_branch scalar is the fallback when nothing else carries a branch",
			um:   &usageMeta{GitBranch: "feat/scalar"},
			want: map[string]attributionv1.SignalSource{"feat/scalar": srcUnknown},
		},
		{
			name: "scalar does not widen the set when a repeated field carries one",
			um: &usageMeta{
				GitBranches: []string{"feat/a"},
				GitBranch:   "feat/stale",
			},
			want: map[string]attributionv1.SignalSource{"feat/a": srcUnknown},
		},
		{
			name: "empty names never enter the set",
			um: &usageMeta{
				BranchSignals: []*attributionv1.BranchSignal{sig("", srcCommand)},
				GitBranches:   []string{""},
			},
			want: map[string]attributionv1.SignalSource{},
		},
		{
			name: "no branches at all yields an empty set",
			um:   &usageMeta{},
			want: map[string]attributionv1.SignalSource{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, collectBranches(tt.um))
		})
	}
}

func TestIDETypeIsGated(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		ideType string
		want    bool
	}{
		{"kiro is gated", "IDE_TYPE_KIRO", true},
		{"case-insensitive", "ide_type_kiro", true},
		// Fails open: an unrecognised editor keeps attributing as an ungated
		// editor rather than having attributions silently withdrawn.
		{"claude code is not gated", "IDE_TYPE_CLAUDE_CODE", false},
		{"empty is not gated", "", false},
		{"unrecognised is not gated", "IDE_TYPE_SOMETHING_NEW", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IDETypeIsGated(tt.ideType))
		})
	}
}

// The central scenario of the design: one session holding both a `gh pr create`
// receipt and a URL scraped from a teammate's commit, for the SAME PR. Dedup is
// on (repo, number), so the merge must say which source survives — keeping the
// weaker one would let the scrape shadow the receipt and make the reference
// look inadmissible.
func TestCollectPRRefs_StrongestSourceSurvivesDedup(t *testing.T) {
	t.Parallel()
	refs := collectPRRefs(&usageMeta{
		PrReferences: []*attributionv1.PRReference{
			{PrRepository: "org/repo", PrNumber: 42, Source: srcLocal},
			{PrRepository: "org/repo", PrNumber: 42, Source: srcCommand},
		},
	})
	assert.Len(t, refs, 1)
	assert.Equal(t, srcCommand, refs[0].source)
	assert.True(t, refs[0].isSessionProduced())
}

func TestSourcedPRRef_IsSessionProduced(t *testing.T) {
	t.Parallel()
	tests := []struct {
		src  attributionv1.SignalSource
		want bool
	}{
		{srcCommand, true},
		{srcVendor, true},
		{srcProse, true}, // caller repo-gates this one
		// A URL scraped from a teammate's commit, and the unstamped references
		// it cannot be told apart from.
		{srcLocal, false},
		{srcUnknown, false},
	}
	for _, tt := range tests {
		t.Run(tt.src.String(), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, sourcedPRRef{source: tt.src}.isSessionProduced())
		})
	}
}
