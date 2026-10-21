// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func vhop(src, dst string) Hop { return Hop{Source: src, Target: dst} }

func TestValidateFlow(t *testing.T) {
	tests := []struct {
		name    string
		hops    []Hop
		wantErr string // substring; "" = valid
	}{
		{
			name: "a linear promotion chain is valid",
			hops: []Hop{vhop("feature", "develop"), vhop("develop", "main")},
		},
		{
			name: "a converging tree toward one target is valid",
			hops: []Hop{vhop("feat-a", "develop"), vhop("feat-b", "develop"), vhop("develop", "main")},
		},
		{
			name: "a single hop is valid",
			hops: []Hop{vhop("develop", "main")},
		},
		{
			name:    "a self-loop is rejected",
			hops:    []Hop{vhop("main", "main")},
			wantErr: "into itself",
		},
		{
			name:    "a two-branch cycle is rejected",
			hops:    []Hop{vhop("a", "b"), vhop("b", "a")},
			wantErr: "cycle",
		},
		{
			name:    "a three-branch cycle is rejected",
			hops:    []Hop{vhop("a", "b"), vhop("b", "c"), vhop("c", "a")},
			wantErr: "cycle",
		},
		{
			name:    "disjoint flows with two targets are rejected",
			hops:    []Hop{vhop("a", "b"), vhop("c", "d")},
			wantErr: "single target branch",
		},
		{
			name:    "a duplicate hop is rejected",
			hops:    []Hop{vhop("a", "b"), vhop("a", "b")},
			wantErr: "duplicate hop",
		},
		{
			name:    "two terminals sharing a source are rejected",
			hops:    []Hop{vhop("develop", "main"), vhop("develop", "release")},
			wantErr: "single target branch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateFlow(tt.hops)
			if tt.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestTrackedBranchOnFlow(t *testing.T) {
	// feature -> staging -> main: target nodes are {staging, main}; feature is a pure
	// source (never a hop target), so tracking it alone admits nothing.
	chain := []Hop{vhop("feature", "staging"), vhop("staging", "main")}
	tests := []struct {
		name    string
		hops    []Hop
		tracked []string
		want    bool
	}{
		{
			name:    "terminal tracked is on the flow",
			hops:    chain,
			tracked: []string{"main"},
			want:    true,
		},
		{
			name:    "intermediate tracked is on the flow (middle of the lineage)",
			hops:    chain,
			tracked: []string{"staging"},
			want:    true,
		},
		{
			name:    "glob tracked matching an intermediate hop target is on the flow",
			hops:    []Hop{vhop("feature", "release/1.0"), vhop("release/1.0", "main")},
			tracked: []string{"release/*"},
			want:    true,
		},
		{
			name:    "no tracked branch anywhere on the flow is rejected",
			hops:    chain,
			tracked: []string{"develop"},
			want:    false,
		},
		{
			name:    "a pure leaf source tracked is rejected (target-node refinement)",
			hops:    chain,
			tracked: []string{"feature"},
			want:    false,
		},
		{
			// The false-accept the target-only refinement exists to prevent: a glob
			// that matches ONLY a pure source (feature/x, never a hop target) must
			// not admit the flow, even though BranchMatches(entry, source) is true.
			name:    "a glob matching only a pure source is rejected",
			hops:    []Hop{vhop("feature/x", "staging"), vhop("staging", "main")},
			tracked: []string{"feature/*"},
			want:    false,
		},
		{
			name:    "a glob matching no branch on the flow is rejected",
			hops:    chain,
			tracked: []string{"hotfix/*"},
			want:    false,
		},
		{
			// Any-match across entries: a non-matching entry followed by a matching
			// one still admits the flow (the loop returns on the first match, order-independent).
			name:    "one of several tracked entries matching a target admits the flow",
			hops:    chain,
			tracked: []string{"develop", "main"},
			want:    true,
		},
		{
			name:    "empty tracked is not on any flow",
			hops:    chain,
			tracked: nil,
			want:    false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, TrackedBranchOnFlow(tt.hops, tt.tracked))
		})
	}
}

// TestValidateFlow_RejectsAnEmptyBranch: a hop with an empty source or target
// is not a flow a person can confirm.
func TestValidateFlow_RejectsAnEmptyBranch(t *testing.T) {
	for _, h := range []Hop{
		{Source: "", Target: "main"}, {Source: "develop", Target: ""},
		{Source: " ", Target: "main"}, {Source: "\t", Target: "main"}, {Source: "develop", Target: " "},
		{Source: "\n", Target: "main"}, {Source: "\u00a0", Target: "main"},
	} {
		t.Run(fmt.Sprintf("%q->%q", h.Source, h.Target), func(t *testing.T) {
			require.ErrorIs(t, ValidateFlow([]Hop{h}), ErrInvalidFlow)
		})
	}
	for _, h := range []Hop{{Source: " develop", Target: "main"}, {Source: "develop ", Target: "main"}} {
		t.Run(fmt.Sprintf("%q->%q is a name, not blank", h.Source, h.Target), func(t *testing.T) {
			require.NoError(t, ValidateFlow([]Hop{h}))
		})
	}
}
