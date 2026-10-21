// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func set(keys ...string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, k := range keys {
		out[k] = struct{}{}
	}
	return out
}

// TestProject_CrossAuthorCarrier: a promotion that carried other people's
// attributed work is credited, whoever opened it.
func TestProject_CrossAuthorCarrier(t *testing.T) {
	prs := []PR{
		mpr("LX", 1, "feature/x", "develop", 60),
		mpr("LY", 2, "feature/y", "develop", 120),
		mpr("C", 10, "develop", "main", 180),
	}
	g, err := Derive(prs)
	require.NoError(t, err)
	proj, err := Project(context.Background(), g, ProjectInput{
		Seeds:      TrackedSeeds(prs, func(_, b string) bool { return b == "main" }),
		Flows:      map[string][]Hop{testRepo: {{"develop", "main"}}},
		Attributed: set("LX", "LY"),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"C"}, proj.Keys())
	assert.Equal(t, []string{"C", "LX"}, proj.Credits[0].Path, "the shortest chain, ties broken by key")
	assert.Equal(t, testRepo, proj.Credits[0].Repository)
}

// TestProject_NothingToProject pins the early outs: no attributed work, no
// flow for the repository, or a carrier that carried nothing attributed.
func TestProject_NothingToProject(t *testing.T) {
	prs := []PR{mpr("L", 1, "feature/x", "develop", 60), mpr("C", 10, "develop", "main", 180)}
	g, err := Derive(prs)
	require.NoError(t, err)
	seeds := TrackedSeeds(prs, func(_, b string) bool { return b == "main" })
	flows := map[string][]Hop{testRepo: {{"develop", "main"}}}
	tests := []struct {
		name      string
		in        ProjectInput
		wantWalks []string
	}{
		{name: "no attributed keys still walks", in: ProjectInput{Seeds: seeds, Flows: flows}, wantWalks: []string{"C->L"}},
		{name: "no flow for the repository", in: ProjectInput{Seeds: seeds, Flows: map[string][]Hop{"other/repo": {{"develop", "main"}}}, Attributed: set("L")}},
		{name: "nothing attributed is carried", in: ProjectInput{Seeds: seeds, Flows: flows, Attributed: set("UNRELATED")}, wantWalks: []string{"C->L"}},
		{name: "a flow that does not admit the hop", in: ProjectInput{Seeds: seeds, Flows: map[string][]Hop{testRepo: {{"release", "main"}}}, Attributed: set("L")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proj, err := Project(context.Background(), g, tt.in)
			require.NoError(t, err)
			assert.Empty(t, proj.Credits)
			assert.Equal(t, tt.wantWalks, nilIfEmpty(pairs(proj.Walk.Edges)), "the walk is reported whether or not anything is credited")
		})
	}
}

// TestProject_ReachesThroughACycle: a carrier graph a caller's edge source
// returns with a cycle still credits every carrier that reaches attributed
// work, whichever carrier is examined first.
func TestProject_ReachesThroughACycle(t *testing.T) {
	src := edgeList{
		edge("A", "B", "s1", "main"),
		edge("B", "A", "s2", "s1"),
		edge("A", "D", "s1", "main"),
		edge("D", "LEAF", "s3", "s1"),
	}
	proj, err := Project(context.Background(), src, ProjectInput{
		Seeds:      []Seed{{Repository: testRepo, Carrier: "A", Target: "main"}},
		Flows:      map[string][]Hop{testRepo: {{"s1", "main"}, {"s2", "s1"}, {"s3", "s1"}}},
		Attributed: set("LEAF"),
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B", "D"}, proj.Keys())
}

// TestProject_SourceErrorIsReturned: a failed walk fails the projection.
func TestProject_SourceErrorIsReturned(t *testing.T) {
	_, err := Project(context.Background(), failingSource{}, ProjectInput{
		Seeds:      []Seed{{Repository: testRepo, Carrier: "A", Target: "main"}},
		Flows:      map[string][]Hop{testRepo: {{"s1", "main"}}},
		Attributed: set("LEAF"),
	})
	require.Error(t, err)
}

// TestProject_OneCreditPerCarrierDeclined pins a design choice: a promotion is
// credited once, whole, however little of what it carried was attributed, and its
// credit names one chain, even when several lead to attributed work. There is no
// fractional credit.
func TestProject_OneCreditPerCarrierDeclined(t *testing.T) {
	src := edgeList{
		edge("X", "A", "staging", "main"), edge("X", "B", "staging", "main"),
		edge("X", "C", "staging", "main"),
		edge("A", "L", "feature", "staging"), edge("B", "L", "feature", "stage"),
	}
	proj, err := Project(context.Background(), src, ProjectInput{
		Seeds:      []Seed{{Repository: testRepo, Carrier: "X", Target: "main"}},
		Flows:      map[string][]Hop{testRepo: {{"staging", "main"}, {"feature", "staging"}, {"feature", "stage"}}},
		Attributed: set("L"),
	})
	require.NoError(t, err)
	got := map[string][]string{}
	for _, c := range proj.Credits {
		got[c.Carrier] = c.Path
	}
	assert.Equal(t, []string{"X", "A", "L"}, got["X"], "one chain, the first by key, though X also reaches L through B")
	assert.Len(t, proj.Credits, 3, "X, A and B are each credited once and whole")
}

// TestProject_HeadCommitBoundsTheCredit: PR-2 merged into s3 after the last push
// to PR-10's head, so PR-10 did not deliver it; PR-11 did. With the commit times
// known, PR-10 is not credited with PR-2. Without them the window runs to PR-10's
// merge and it is, a known limit.
func TestProject_HeadCommitBoundsTheCredit(t *testing.T) {
	hour := func(h float64) time.Time { return testBase.Add(time.Duration(h * float64(time.Hour))) }
	pr := func(n int, head, base string, h float64) PR {
		return PR{Key: fmt.Sprint("PR-", n), Repository: "r", Number: n, HeadBranch: head, BaseBranch: base, MergedAt: hour(h)}
	}
	tests := []struct {
		name        string
		commitTimes bool
		want        []string
	}{
		{name: "with the commit times, PR-10 carried only what its head held", commitTimes: true, want: nil},
		{name: "without them, PR-10 is credited with PR-2", commitTimes: false, want: []string{"PR-10 [PR-10 PR-2]"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prs := []PR{
				pr(1, "s4", "s3", 1),
				pr(2, "s4b", "s3", 5),     // merged into s3 after PR-10's last push
				pr(10, "s3", "main", 5.1), // merges its older head
				pr(11, "s4b", "main", 11), // PR-2's work reaching main on its own
			}
			prs[2].FirstCommitAt = testBase
			if tt.commitTimes {
				for i := range prs {
					prs[i].MergeCommitAt = prs[i].MergedAt.Add(-2 * time.Second)
				}
				prs[2].HeadCommitAt = prs[0].MergeCommitAt // PR-10's head is PR-1's merge commit
			}
			g, err := Derive(prs)
			require.NoError(t, err)
			proj, err := Project(context.Background(), g, ProjectInput{
				Seeds:      TrackedSeeds(prs, func(_, b string) bool { return b == "main" }),
				Flows:      map[string][]Hop{"r": {{Source: "s3", Target: "main"}}},
				Attributed: set("PR-2", "PR-11"),
			})
			require.NoError(t, err)
			var got []string
			for _, c := range proj.Credits {
				got = append(got, fmt.Sprint(c.Carrier, " ", c.Path))
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
