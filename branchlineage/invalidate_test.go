// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func staleMap(inv Invalidation) map[string]Reason {
	out := map[string]Reason{}
	for _, s := range inv.Carriers {
		out[s.Carrier] = s.Reason
	}
	return out
}

// TestInvalidate_Correction: a changed pull request is stale itself, and so is
// the carrier that carried it; an unchanged re-delivery invalidates nothing.
func TestInvalidate_Correction(t *testing.T) {
	stored := []PR{
		mpr("L", 60, "feature/a", "staging", 300),
		mpr("A", 100, "staging", "main", 600),
	}
	tests := []struct {
		name   string
		mutate func(p *PR)
		want   map[string]Reason
	}{
		{
			name:   "the merge time moves earlier",
			mutate: func(p *PR) { p.MergedAt = at(200) },
			want:   map[string]Reason{"L": ReasonCorrection, "A": ReasonCorrection},
		},
		{
			name:   "the base branch is retargeted",
			mutate: func(p *PR) { p.BaseBranch = "release/2.0" },
			want:   map[string]Reason{"L": ReasonCorrection, "A": ReasonCorrection},
		},
		{
			name:   "the line counts are enriched",
			mutate: func(p *PR) { p.LinesAdded = 999 },
			want:   map[string]Reason{"L": ReasonCorrection, "A": ReasonCorrection},
		},
		{
			name:   "the merge commit time is enriched",
			mutate: func(p *PR) { p.MergeCommitAt = at(299) },
			want:   map[string]Reason{"L": ReasonCorrection, "A": ReasonCorrection},
		},
		{
			name:   "the same inputs again change nothing",
			mutate: func(*PR) {},
			want:   map[string]Reason{},
		},
		{
			name:   "a field the deriver does not read changes nothing",
			mutate: func(p *PR) { p.OpenedAt = at(-500) },
			want:   map[string]Reason{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := stored[0]
			tt.mutate(&in)
			got := Invalidate(history(t, stored...), []PR{in})
			assert.Equal(t, tt.want, staleMap(got))
		})
	}
}

// TestInvalidate_LateLeaf: a new leaf inside a promotion's window makes that
// promotion stale, and only it.
func TestInvalidate_LateLeaf(t *testing.T) {
	h := history(t,
		mpr("L0", 1, "feature/z", "staging", 50),
		mpr("B", 90, "staging", "main", 200),
		mpr("A", 100, "staging", "main", 600),
	)
	got := Invalidate(h, []PR{mpr("LATE", 61, "feature/late", "staging", 300)})
	assert.Equal(t, map[string]Reason{"A": ReasonLateLeaf}, staleMap(got))
}

// TestInvalidate_LateLeafPerLane: one late leaf falls in one promotion per
// target branch it is promoted to.
func TestInvalidate_LateLeafPerLane(t *testing.T) {
	fork := mpr("HOTFIX-FORK", 60, "staging", "hotfix", 400)
	fork.CrossRepository = true
	h := history(t,
		mpr("P1", 10, "staging", "main", 200),
		mpr("P9", 90, "staging", "main", 320),
		mpr("P2", 20, "staging", "main", 400),
		mpr("REL", 40, "staging", "release/2.0", 500),
		mpr("DEV", 50, "staging", "develop", 350),
		fork,
	)
	got := Invalidate(h, []PR{mpr("LATE", 100, "feature/x", "staging", 300)})
	assert.Equal(t, map[string]Reason{"P9": ReasonLateLeaf, "REL": ReasonLateLeaf, "DEV": ReasonLateLeaf}, staleMap(got),
		"the earliest promotion at or after the leaf on each lane; a fork never covers")
}

// TestInvalidate_HeadCommit: a late leaf committed after a promotion's head
// commit falls in the next promotion's window, not the first one merged after
// it; and a promotion's head commit bounds both its own window and the next one's.
func TestInvalidate_HeadCommit(t *testing.T) {
	l0 := mpr("L0", 1, "feature/z", "staging", 50)
	l0.MergeCommitAt = at(50)
	b := mpr("B", 90, "staging", "main", 200)
	b.HeadCommitAt = at(50)
	stored := []PR{l0, b, mpr("A", 100, "staging", "main", 600)}

	late := mpr("LATE", 61, "feature/late", "staging", 150)
	late.MergeCommitAt = at(150)
	assert.Equal(t, map[string]Reason{"A": ReasonLateLeaf}, staleMap(Invalidate(history(t, stored...), []PR{late})),
		"B's head did not hold it")

	moved := b
	moved.HeadCommitAt = at(40)
	assert.Equal(t, map[string]Reason{"B": ReasonCorrection, "A": ReasonLatePromotion},
		staleMap(Invalidate(history(t, stored...), []PR{moved})))
}

// TestInvalidate_LatePromotion: a new promotion re-bounds the next promotion of
// its pair; the earlier one is untouched.
func TestInvalidate_LatePromotion(t *testing.T) {
	h := history(t,
		mpr("L0", 1, "feature/z", "staging", 50),
		mpr("P_PREV", 10, "staging", "main", 100),
		mpr("P_NEXT", 30, "staging", "main", 600),
	)
	got := Invalidate(h, []PR{mpr("P_MID", 20, "staging", "main", 300)})
	assert.Equal(t, map[string]Reason{"P_NEXT": ReasonLatePromotion}, staleMap(got))
}

// TestInvalidate_Retarget: a leaf moved to another base re-derives the carriers
// on both branch pairs.
func TestInvalidate_Retarget(t *testing.T) {
	h := history(t,
		mpr("L", 60, "feature/a", "staging", 300),
		mpr("A_OLD", 100, "staging", "main", 600),
		mpr("A_NEW", 110, "release/2.0", "main", 500),
	)
	moved := mpr("L", 60, "feature/a", "release/2.0", 300)
	got := Invalidate(h, []PR{moved})
	assert.Equal(t, map[string]Reason{"L": ReasonCorrection, "A_OLD": ReasonCorrection, "A_NEW": ReasonLateLeaf}, staleMap(got))
}

// TestInvalidate_FirstCommit: a pull request is replaced whole, so any change to
// its first commit re-derives it; the same value does not.
func TestInvalidate_FirstCommit(t *testing.T) {
	stored := mpr("C", 100, "staging", "main", 600)
	stored.FirstCommitAt = at(200)
	tests := []struct {
		name string
		fca  time.Time
		want map[string]Reason
	}{
		{name: "earlier", fca: at(100), want: map[string]Reason{"C": ReasonCorrection}},
		{name: "later", fca: at(300), want: map[string]Reason{"C": ReasonCorrection}},
		{name: "equal", fca: at(200), want: map[string]Reason{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := stored
			in.FirstCommitAt = tt.fca
			assert.Equal(t, tt.want, staleMap(Invalidate(history(t, stored), []PR{in})))
		})
	}
}

// TestInvalidate_Unmerged: a stored merged pull request replaced by an unmerged
// one leaves its old window, so the promotion that carried it re-derives.
func TestInvalidate_Unmerged(t *testing.T) {
	h := history(t,
		mpr("L", 60, "feature/a", "staging", 300),
		mpr("A", 100, "staging", "main", 600),
	)
	reopened := mpr("L", 60, "feature/a", "staging", 300)
	reopened.MergedAt = time.Time{}
	got := Invalidate(h, []PR{reopened})
	assert.Equal(t, map[string]Reason{"L": ReasonCorrection, "A": ReasonCorrection}, staleMap(got))
}

// TestInvalidate_ReportsReasons pins the Observer event, one per stale carrier.
func TestInvalidate_ReportsReasons(t *testing.T) {
	rec := newRecorder()
	h := history(t, mpr("L0", 1, "feature/z", "staging", 50), mpr("A", 100, "staging", "main", 600))
	Invalidate(h, []PR{mpr("LATE", 61, "feature/late", "staging", 300)}, WithObserver(rec))
	assert.Equal(t, 1, rec.counts["invalidation"])
	assert.Equal(t, []map[string]string{{"reason": "late_leaf"}}, rec.labels["invalidation"])
}

// TestInvalidate_RepositoryGivenLater: a stored merged pull request that had no
// repository, and so derived nothing, is stale once a later version gives it one.
func TestInvalidate_RepositoryGivenLater(t *testing.T) {
	carrier := mpr("C1", 100, "develop", "main", 120)
	carrier.Repository = ""
	h := history(t, mpr("L1", 1, "feature/a", "develop", 60), carrier)
	got := Invalidate(h, []PR{mpr("C1", 100, "develop", "main", 120)})
	assert.Equal(t, map[string]Reason{"C1": ReasonCorrection}, staleMap(got))
}
