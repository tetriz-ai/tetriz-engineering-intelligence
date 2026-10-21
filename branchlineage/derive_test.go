// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"fmt"
	"testing"
	"time"
	_ "time/tzdata" // the zoned rule needs America/New_York on any machine

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDerive_Rules is the per-rule table. Each case is a repository history and
// the carrier X derived from it; feature branches merge into staging, which
// promotes to main.
func TestDerive_Rules(t *testing.T) {
	x := func() PR { return mpr("X", 100, "staging", "main", 600) }
	lookbackFloor := at(600).AddDate(0, 0, -ColdStartLookbackDays)
	// landed dates p's merge commit two seconds before its recorded merge, as
	// GitHub's often are.
	landed := func(p PR) PR { p.MergeCommitAt = p.MergedAt.Add(-2 * time.Second); return p }
	headAt := func(p PR, t time.Time) PR { p.HeadCommitAt = t; return p }
	tests := []struct {
		name        string
		carrier     PR
		others      []PR
		wantOutcome Outcome
		wantCarried []string
		wantStart   time.Time
		wantChain   string
		wantCold    ColdStart
	}{
		{
			name:        "rule 1: a head branch that never received a merge carries nothing",
			carrier:     x(),
			wantOutcome: OutcomeNonCarrier,
		},
		{
			name:        "rule 2: a fork is dropped before the carrier test",
			carrier:     func() PR { p := x(); p.CrossRepository = true; return p }(),
			others:      []PR{mpr("L1", 60, "feature/a", "staging", 200)},
			wantOutcome: OutcomeDropped,
		},
		{
			name:        "rule 2: an empty base branch is dropped",
			carrier:     func() PR { p := x(); p.BaseBranch = ""; return p }(),
			others:      []PR{mpr("L1", 60, "feature/a", "staging", 200)},
			wantOutcome: OutcomeDropped,
		},
		{
			name:        "rule 2: no repository is dropped",
			carrier:     func() PR { p := x(); p.Repository = ""; return p }(),
			others:      []PR{mpr("L1", 60, "feature/a", "staging", 200)},
			wantOutcome: OutcomeDropped,
		},
		{
			name:        "rule 2: a branch merged into itself is dropped",
			carrier:     func() PR { p := x(); p.BaseBranch = "staging"; return p }(),
			others:      []PR{mpr("L1", 60, "feature/a", "staging", 200)},
			wantOutcome: OutcomeDropped,
		},
		{
			name:    "rules 4 and 6: the merges after the previous promotion are carried, a carrier among them too",
			carrier: x(),
			others: []PR{
				mpr("L0", 40, "feature/old", "staging", 50),
				mpr("P", 50, "staging", "main", 100),
				mpr("L1", 60, "feature/a", "staging", 200),
				mpr("L2", 61, "feature/b", "staging", 300),
			},
			wantOutcome: OutcomeCarrier,
			wantCarried: []string{"L1", "L2"},
			wantStart:   at(100), wantChain: "P",
		},
		{
			name:    "rule 4: a merge committed after the promotion's head commit is not carried, though it merged first",
			carrier: headAt(x(), landed(mpr("L1", 60, "feature/a", "staging", 200)).MergeCommitAt), // the head is L1's merge commit
			others: []PR{
				mpr("P", 50, "staging", "main", 100),
				landed(mpr("L1", 60, "feature/a", "staging", 200)),
				landed(mpr("L2", 61, "feature/b", "staging", 300)),
			},
			wantOutcome: OutcomeCarrier,
			wantCarried: []string{"L1"},
			wantStart:   at(100), wantChain: "P",
		},
		{
			name:    "rule 4: a merge the previous promotion's head did not hold is carried by the next",
			carrier: x(),
			others: []PR{
				landed(mpr("L0", 40, "feature/old", "staging", 50)),
				landed(mpr("GAP", 41, "feature/gap", "staging", 80)),
				headAt(mpr("P", 50, "staging", "main", 100), at(50).Add(-2*time.Second)),
				landed(mpr("L1", 60, "feature/a", "staging", 200)),
			},
			wantOutcome: OutcomeCarrier,
			wantCarried: []string{"GAP", "L1"},
			wantStart:   at(50).Add(-2 * time.Second), wantChain: "P",
		},
		{
			name:    "rule 4: a rebased head, committed after every merge it replays, carries them all",
			carrier: headAt(x(), at(550)),
			others: []PR{
				mpr("P", 50, "staging", "main", 100),
				landed(mpr("L1", 60, "feature/a", "staging", 200)),
				landed(mpr("L2", 61, "feature/b", "staging", 300)),
			},
			wantOutcome: OutcomeCarrier,
			wantCarried: []string{"L1", "L2"},
			wantStart:   at(100), wantChain: "P",
		},
		{
			name:    "rule 4: a merge with no commit time is not left out by the head commit",
			carrier: headAt(x(), at(250)),
			others: []PR{
				mpr("P", 50, "staging", "main", 100),
				landed(mpr("L1", 60, "feature/a", "staging", 200)),
				mpr("L2", 61, "feature/b", "staging", 300),
			},
			wantOutcome: OutcomeCarrier,
			wantCarried: []string{"L1", "L2"},
			wantStart:   at(100), wantChain: "P",
		},
		{
			name:    "rule 5: a pair's first promotion carries only what its head held",
			carrier: headAt(x(), at(200).Add(-2*time.Second)),
			others: []PR{
				landed(mpr("L1", 60, "feature/a", "staging", 200)),
				landed(mpr("L2", 61, "feature/b", "staging", 300)),
			},
			wantOutcome: OutcomeCarrier,
			wantCarried: []string{"L1"},
			wantStart:   lookbackFloor, wantCold: ColdStartLookback,
		},
		{
			name:    "rule 3: a merge into the head branch whose own head is the base branch is a back-merge",
			carrier: x(),
			others: []PR{
				mpr("P", 50, "staging", "main", 100),
				mpr("L1", 60, "feature/a", "staging", 200),
				mpr("BM", 61, "main", "staging", 300),
			},
			wantOutcome: OutcomeCarrier,
			wantCarried: []string{"L1"},
			wantStart:   at(100), wantChain: "P",
		},
		{
			name:        "a sync pull request derives edges like any other; the walk's flow keeps them inert",
			carrier:     mpr("SYNC", 100, "main", "feature/x", 600),
			others:      []PR{mpr("S1", 60, "feature/y", "main", 200)},
			wantOutcome: OutcomeCarrier,
			wantCarried: []string{"S1"},
			wantStart:   lookbackFloor, wantCold: ColdStartLookback,
		},
		{
			name:    "rule 5: no previous promotion and no first commit opens the lookback window",
			carrier: x(),
			others: []PR{
				{Key: "OLD", Repository: testRepo, Number: 59, HeadBranch: "feature/old", BaseBranch: "staging", MergedAt: lookbackFloor.Add(-time.Minute)},
				{Key: "EDGE", Repository: testRepo, Number: 58, HeadBranch: "feature/edge", BaseBranch: "staging", MergedAt: lookbackFloor},
				mpr("L1", 60, "feature/a", "staging", 200),
			},
			wantOutcome: OutcomeCarrier,
			wantCarried: []string{"EDGE", "L1"},
			wantStart:   lookbackFloor, wantCold: ColdStartLookback,
		},
		{
			name:        "rule 5: a first commit exactly at the lookback floor is the lookback",
			carrier:     func() PR { p := x(); p.FirstCommitAt = lookbackFloor; return p }(),
			others:      []PR{mpr("L1", 60, "feature/a", "staging", 200)},
			wantOutcome: OutcomeCarrier,
			wantCarried: []string{"L1"},
			wantStart:   lookbackFloor, wantCold: ColdStartLookback,
		},
		{
			name: "rule 5: the lookback floor is taken in UTC whatever the merge time's zone",
			carrier: func() PR {
				newYork, err := time.LoadLocation("America/New_York")
				if err != nil {
					panic(err)
				}
				p := x()
				p.MergedAt = time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC).In(newYork)
				return p
			}(),
			others: []PR{{Key: "L", Repository: testRepo, Number: 60, HeadBranch: "feature/f", BaseBranch: "staging",
				MergedAt: time.Date(2026, 2, 10, 0, 30, 0, 0, time.UTC)}},
			wantOutcome: OutcomeCarrier,
			wantCarried: []string{"L"},
			wantStart:   time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC), wantCold: ColdStartLookback,
		},
		{
			name:    "rule 5: a first commit later than the lookback opens the window",
			carrier: func() PR { p := x(); p.FirstCommitAt = at(500); return p }(),
			others: []PR{
				mpr("BEFORE", 59, "feature/old", "staging", 450),
				mpr("L1", 60, "feature/a", "staging", 550),
			},
			wantOutcome: OutcomeCarrier,
			wantCarried: []string{"L1"},
			wantStart:   at(500), wantCold: ColdStartFirstCommit,
		},
		{
			name:        "rule 5: a first commit after the merge (clock skew) falls back to the lookback",
			carrier:     func() PR { p := x(); p.FirstCommitAt = at(700); return p }(),
			others:      []PR{mpr("L1", 60, "feature/a", "staging", 200)},
			wantOutcome: OutcomeCarrier,
			wantCarried: []string{"L1"},
			wantStart:   lookbackFloor, wantCold: ColdStartLookback,
		},
		{
			name:    "a promotion whose window held nothing is derived empty",
			carrier: x(),
			others: []PR{
				mpr("L0", 40, "feature/old", "staging", 50),
				mpr("P", 50, "staging", "main", 100),
			},
			wantOutcome: OutcomeDerivedEmpty,
			wantStart:   at(100), wantChain: "P",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := history(t, append([]PR{tt.carrier}, tt.others...)...)
			d, err := h.Derive("X")
			if tt.carrier.Key == "SYNC" {
				d, err = h.Derive("SYNC")
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantOutcome, d.Outcome)
			assert.Equal(t, tt.wantCarried, nilIfEmpty(carried(d)))
			if tt.wantOutcome == OutcomeCarrier || tt.wantOutcome == OutcomeDerivedEmpty {
				require.NotNil(t, d.Evidence)
				assert.Equal(t, tt.wantStart, d.Evidence.WindowStart)
				assert.Equal(t, tt.carrier.MergedAt, d.Evidence.WindowEnd)
				assert.Equal(t, tt.wantChain, d.Evidence.ChainedFrom)
				assert.Equal(t, tt.wantCold, d.Evidence.ColdStart)
			} else {
				assert.Nil(t, d.Evidence)
			}
			for _, e := range d.Edges {
				assert.Equal(t, tt.carrier.HeadBranch, e.Source)
				assert.Equal(t, tt.carrier.BaseBranch, e.Target)
				require.NotNil(t, e.CarrierLines)
				assert.Equal(t, int64(15), *e.CarrierLines)
			}
		})
	}
}

func nilIfEmpty(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

// TestDerive_MergeOrderTies pins the (merged time, number) order every rule
// shares: a prior merge tied on merged time with a lower number is strictly
// before the carrier, and an equal position is not before itself.
func TestDerive_MergeOrderTies(t *testing.T) {
	h := history(t,
		mpr("TIE", 400, "feature/tie", "rc", 600),
		mpr("X", 500, "rc", "main", 600),
	)
	d, err := h.Derive("X")
	require.NoError(t, err)
	assert.Equal(t, OutcomeCarrier, d.Outcome, "a same-time, lower-number merge into rc passes the carrier test")
	assert.Equal(t, []string{"TIE"}, carried(d))

	ri := h.byRepo[testRepo]
	tie, _ := h.PR("TIE")
	assert.False(t, ri.hasPriorInto("rc", tie.pos()), "a position is not strictly before itself")
}

// TestDerive_PreviousPromotionSkipsAFork pins rule 4's lower bound: the nearest
// same-pair merge from a fork is not a promotion, so it cannot shorten the window.
func TestDerive_PreviousPromotionSkipsAFork(t *testing.T) {
	fork := mpr("FORK", 28, "staging", "main", 450)
	fork.CrossRepository = true
	h := history(t,
		mpr("L0", 5, "feature/z", "staging", 50),
		mpr("P1", 10, "staging", "main", 100),
		mpr("P2", 20, "staging", "main", 300),
		mpr("OTHER", 25, "develop", "main", 400),
		fork,
		mpr("L1", 26, "feature/a", "staging", 350),
		mpr("X", 30, "staging", "main", 500),
	)
	d, err := h.Derive("X")
	require.NoError(t, err)
	assert.Equal(t, "P2", d.Evidence.ChainedFrom, "the previous same-pair promotion, past the nearer fork")
	assert.Equal(t, []string{"L1"}, carried(d))

	p1, err := h.Derive("P1")
	require.NoError(t, err)
	require.NotNil(t, p1.Evidence)
	assert.Empty(t, p1.Evidence.ChainedFrom, "nothing before the first promotion: a cold start")
}

// TestDerive_WindowIsHalfOpen pins the contents window: the merge at the lower
// bound is out, the carrier's own position is in, a later merge is out, a merge
// into another branch is out, and a fork's merge is carried.
func TestDerive_WindowIsHalfOpen(t *testing.T) {
	fork := mpr("IN-FORK", 62, "fork/x", "staging", 350)
	fork.CrossRepository = true
	h := history(t,
		mpr("P", 50, "staging", "main", 100),
		mpr("AT-LOWER", 49, "feature/a", "staging", 100),
		mpr("IN-1", 60, "feature/b", "staging", 200),
		fork,
		mpr("WRONG", 63, "feature/f", "develop", 250),
		mpr("ABOVE", 80, "feature/e", "staging", 500),
		mpr("X", 70, "staging", "main", 400),
	)
	d, err := h.Derive("X")
	require.NoError(t, err)
	assert.Equal(t, []string{"IN-1", "IN-FORK"}, carried(d), "merge order, ascending")
}

// TestDerive_MultiHop derives two carriers from one history, each with its own
// edge set. The release carrier recovers only the fix merged into the release
// branch by pull request: the content present when the branch was cut is not a
// merge, a known recall gap the coverage signal shows.
func TestDerive_MultiHop(t *testing.T) {
	g, err := Derive([]PR{
		mpr("L1", 60, "feature/a", "staging", 200),
		mpr("L2", 61, "feature/b", "staging", 300),
		mpr("FIX", 70, "hotfix/x", "release/2.0", 400),
		mpr("C1", 200, "staging", "main", 900),
		mpr("C2", 201, "release/2.0", "main", 950),
	})
	require.NoError(t, err)
	byCarrier := map[string][]string{}
	for _, d := range g.Derivations() {
		byCarrier[d.Carrier] = carried(d)
	}
	assert.Equal(t, []string{"L1", "L2"}, byCarrier["C1"])
	assert.Equal(t, []string{"FIX"}, byCarrier["C2"])
}

// TestDerive_LinesUnknownIsNil pins the line snapshot: a pull request with no
// diff data records nil, never a zero standing in for unknown.
func TestDerive_LinesUnknownIsNil(t *testing.T) {
	leaf := mpr("L1", 60, "feature/a", "staging", 200)
	leaf.LinesAdded, leaf.LinesDeleted = 0, 0
	h := history(t, leaf, mpr("X", 100, "staging", "main", 600))
	d, err := h.Derive("X")
	require.NoError(t, err)
	require.Len(t, d.Edges, 1)
	assert.Nil(t, d.Edges[0].CarriedLines)
	require.NotNil(t, d.Edges[0].CarrierLines)
}

// TestDerive_RejectsWhatItCannotRead pins the errors: a missing key, an
// unmerged pull request, and an invalid set.
func TestDerive_RejectsWhatItCannotRead(t *testing.T) {
	open := mpr("OPEN", 1, "feature/a", "staging", 0)
	open.MergedAt = time.Time{}
	h := history(t, open)
	_, err := h.Derive("NOPE")
	assert.ErrorIs(t, err, ErrInvalidPR)
	_, err = h.Derive("OPEN")
	assert.ErrorIs(t, err, ErrInvalidPR)

	tests := []struct {
		name string
		prs  []PR
	}{
		{name: "an empty key", prs: []PR{{Key: ""}}},
		{name: "a repeated key", prs: []PR{{Key: "A"}, {Key: "A"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewHistory(tt.prs)
			assert.ErrorIs(t, err, ErrInvalidPR)
		})
	}
}

// TestDerive_ReportsOutcomes pins the Observer events a derivation sends.
func TestDerive_ReportsOutcomes(t *testing.T) {
	rec := newRecorder()
	_, err := Derive([]PR{
		mpr("L1", 60, "feature/a", "staging", 200),
		mpr("X", 100, "staging", "main", 600),
	}, WithObserver(rec))
	require.NoError(t, err)
	assert.Equal(t, 2, rec.counts["carrier_outcome"])
	assert.Equal(t, []float64{1}, rec.samples["edges_per_carrier"])
}

// quickStartRepo is the README's nine pull requests: develop is promoted to
// release and to main, and release to main.
func quickStartRepo(withFirstCommit bool) []PR {
	hour := func(h float64) time.Time {
		return time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC).Add(time.Duration(h * float64(time.Hour)))
	}
	pr := func(n int, head, base string, h float64) PR {
		return PR{Key: fmt.Sprint("PR-", n), Repository: "acme/shop", Number: n, HeadBranch: head, BaseBranch: base, MergedAt: hour(h)}
	}
	prs := []PR{
		pr(1, "feature/login", "develop", 1), pr(2, "feature/cart", "develop", 2),
		pr(4, "feature/search", "develop", 3), pr(5, "feature/fix", "main", 4),
		pr(6, "feature/docs", "main", 5), pr(10, "develop", "release", 6),
		pr(3, "feature/pay", "develop", 7), pr(11, "release", "main", 8),
		pr(12, "develop", "main", 9),
	}
	if withFirstCommit {
		prs[8].FirstCommitAt = hour(6.5)
	}
	return prs
}

// TestDerive_WithoutFirstCommitALaneRecarries: without FirstCommitAt, a pair's
// first promotion reads back 90 days, so it also carries work another lane
// already promoted, and both promotions are credited for that work.
func TestDerive_WithoutFirstCommitALaneRecarries(t *testing.T) {
	carriedBy := func(prs []PR) map[string][]string {
		g, err := Derive(prs)
		require.NoError(t, err)
		out := map[string][]string{}
		for _, e := range g.Edges() {
			out[e.Carrier] = append(out[e.Carrier], e.Carried)
		}
		return out
	}
	assert.Equal(t, []string{"PR-3"}, carriedBy(quickStartRepo(true))["PR-12"])
	without := carriedBy(quickStartRepo(false))
	assert.ElementsMatch(t, []string{"PR-1", "PR-2", "PR-4", "PR-3"}, without["PR-12"], "the develop->main lane re-carries what develop->release carried")
	assert.ElementsMatch(t, []string{"PR-1", "PR-2", "PR-4"}, without["PR-10"])
}

// TestDerive_CherryPickCopiesAreNotLinkedDeclined pins a design choice: a change
// merged into main and its cherry-picked copy merged into develop are two pull
// requests, never linked, so the promotion that later carries the copy to main
// carries it as new work.
func TestDerive_CherryPickCopiesAreNotLinkedDeclined(t *testing.T) {
	g, err := Derive([]PR{
		mpr("FIX", 1, "hotfix/price", "main", 10),
		mpr("PICK", 2, "pick/price", "develop", 20),
		mpr("PROMO", 3, "develop", "main", 30),
	})
	require.NoError(t, err)
	var got []string
	for _, e := range g.Edges() {
		got = append(got, e.Carrier+"->"+e.Carried)
	}
	assert.Equal(t, []string{"PROMO->PICK"}, got, "nothing relates PICK to FIX")
}

// TestDerive_BranchNameReuseDeclined pins a design choice: branches are known by
// name alone, so a branch deleted and created again under the same name inherits
// the old one's merges inside the lookback, unless FirstCommitAt bounds the window.
func TestDerive_BranchNameReuseDeclined(t *testing.T) {
	old := mpr("OLD", 1, "feature/z", "topic", 10)
	reused := mpr("REUSED", 2, "topic", "main", 600)
	g, err := Derive([]PR{old, reused})
	require.NoError(t, err)
	require.Len(t, g.Edges(), 1)
	assert.Equal(t, "OLD", g.Edges()[0].Carried, "the reused branch's promotion carries the old branch's merge")

	reused.FirstCommitAt = at(500)
	g, err = Derive([]PR{old, reused})
	require.NoError(t, err)
	assert.Empty(t, g.Edges(), "its first commit bounds the window, and the old merge drops out")
}

// TestFixedConstants pins the two design constants a caller cannot change.
func TestFixedConstants(t *testing.T) {
	assert.Equal(t, 90, ColdStartLookbackDays)
	assert.Equal(t, 2, DetectionSupport)
}

// TestDerive_ARevertIsCarriedLikeAnyMerge pins an inherent limit: the package
// reads no commits, so a pull request and the pull request reverting it, both
// merged into develop, are both carried by the next promotion.
func TestDerive_ARevertIsCarriedLikeAnyMerge(t *testing.T) {
	h := history(t,
		mpr("L", 1, "feature/a", "develop", 10),
		mpr("REVERT-L", 2, "revert-feature/a", "develop", 20),
		mpr("PROMO", 3, "develop", "main", 30),
	)
	d, err := h.Derive("PROMO")
	require.NoError(t, err)
	assert.Equal(t, []string{"L", "REVERT-L"}, carried(d))
}
