// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package storetest is the conformance suite every [branchlineage.Store] must
// pass.
//
// Each rule is a property the indexer depends on, asserted only through the
// Store interface, so passing it means an implementation is interchangeable with
// the shipped ones. The central rule: after any sequence of Put and Derive, the
// stored edges equal a full derivation of the stored pull requests.
package storetest

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"
	_ "time/tzdata" // the zone rule needs America/New_York on any machine

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
)

// NewStore builds an empty store for one sub-test.
type NewStore func(t *testing.T) branchlineage.Store

// RunConformance runs every rule against the stores newStore builds.
func RunConformance(t *testing.T, newStore NewStore) {
	t.Helper()
	t.Run("ADerivedStoreMatchesAFullDerivation", func(t *testing.T) { matchesFullDerivation(t, newStore(t)) })
	t.Run("AReplayChangesNothing", func(t *testing.T) { replayChangesNothing(t, newStore(t)) })
	t.Run("ALateLeafInvalidatesAndRederives", func(t *testing.T) { lateLeafRederives(t, newStore(t)) })
	t.Run("ALateLeafAfterAPromotionsHeadGoesToTheNextOne", func(t *testing.T) { lateLeafAfterAHead(t, newStore(t)) })
	t.Run("AStaleCarrierHasNoEdgesUntilDerived", func(t *testing.T) { staleCarrierHasNoEdges(t, newStore(t)) })
	t.Run("ARederiveReplacesTheWholeEdgeSet", func(t *testing.T) { rederiveReplacesWhole(t, newStore(t)) })
	t.Run("AnUnmergedPullRequestLeavesTheGraph", func(t *testing.T) { unmergedLeaves(t, newStore(t)) })
	t.Run("APullRequestGivenARepositoryLaterIsDerived", func(t *testing.T) { repositoryGivenLater(t, newStore(t)) })
	t.Run("AMergeTimeInAnyZoneDerivesTheSame", func(t *testing.T) { zonedMergeTime(t, newStore(t)) })
	t.Run("ABadBatchWritesNothing", func(t *testing.T) { badBatchWritesNothing(t, newStore(t)) })
	t.Run("RandomBatchesConvergeToAFullDerivation", func(t *testing.T) { randomBatchesConverge(t, newStore) })
}

const (
	repo      = "acme/conformance"
	otherRepo = "acme/other"
)

var base = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

func merged(key string, number int, head, baseBranch string, minute int) branchlineage.PR {
	return branchlineage.PR{
		Key: key, Repository: repo, Number: number, HeadBranch: head, BaseBranch: baseBranch,
		MergedAt: base.Add(time.Duration(minute) * time.Minute), LinesAdded: 10, LinesDeleted: 5,
	}
}

func history() []branchlineage.PR {
	return []branchlineage.PR{
		merged("L1", 1, "feature/a", "develop", 10),
		merged("L2", 2, "feature/b", "develop", 20),
		merged("C1", 10, "develop", "release", 30),
		merged("L3", 3, "feature/c", "develop", 40),
		merged("C2", 11, "release", "main", 50),
		merged("C3", 12, "develop", "main", 60),
	}
}

func put(t *testing.T, s branchlineage.Store, prs ...branchlineage.PR) branchlineage.Invalidation {
	t.Helper()
	inv, err := s.Put(context.Background(), prs...)
	require.NoError(t, err)
	return inv
}

func derive(t *testing.T, s branchlineage.Store) int {
	t.Helper()
	n, err := s.Derive(context.Background())
	require.NoError(t, err)
	return n
}

func edgesOf(t *testing.T, s branchlineage.Store, repository string) []string {
	t.Helper()
	es, err := s.Edges(context.Background(), repository)
	require.NoError(t, err)
	return render(es)
}

func render(es []branchlineage.Edge) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, fmt.Sprintf("%s->%s %s->%s %s/%s", e.Carrier, e.Carried, e.Source, e.Target, lineText(e.CarrierLines), lineText(e.CarriedLines)))
	}
	sort.Strings(out)
	return out
}

func lineText(p *int64) string {
	if p == nil {
		return "nil"
	}
	return fmt.Sprint(*p)
}

// full derives prs from scratch, for one repository.
func full(t *testing.T, prs []branchlineage.PR, repository string) []string {
	t.Helper()
	g, err := branchlineage.Derive(prs)
	require.NoError(t, err)
	var es []branchlineage.Edge
	for _, e := range g.Edges() {
		if e.Repository == repository {
			es = append(es, e)
		}
	}
	return render(es)
}

func pending(t *testing.T, s branchlineage.Store) []string {
	t.Helper()
	p, err := s.Pending(context.Background())
	require.NoError(t, err)
	return p
}

func matchesFullDerivation(t *testing.T, s branchlineage.Store) {
	prs := history()
	put(t, s, prs...)
	assert.Equal(t, 6, derive(t, s), "every merged pull request is derived once")
	assert.Equal(t, full(t, prs, repo), edgesOf(t, s, repo))
	assert.Empty(t, pending(t, s))

	walkFrom, err := s.EdgesFrom(context.Background(), []string{"C2", "UNKNOWN"})
	require.NoError(t, err)
	assert.Equal(t, []string{"C2->C1 release->main 15/15"}, render(walkFrom), "an unknown carrier has no edges")
}

func replayChangesNothing(t *testing.T, s branchlineage.Store) {
	put(t, s, history()...)
	derive(t, s)
	before := edgesOf(t, s, repo)
	inv := put(t, s, history()...)
	assert.Empty(t, inv.Carriers, "the same batch again invalidates nothing")
	assert.Empty(t, pending(t, s), "and queues nothing")
	assert.Zero(t, derive(t, s))
	assert.Equal(t, before, edgesOf(t, s, repo))
}

func lateLeafRederives(t *testing.T, s branchlineage.Store) {
	prs := history()
	put(t, s, prs...)
	derive(t, s)
	late := merged("LATE", 4, "feature/late", "develop", 25)
	inv := put(t, s, late)
	assert.Equal(t, []branchlineage.Stale{
		{Carrier: "C1", Reason: branchlineage.ReasonLateLeaf},
		{Carrier: "C3", Reason: branchlineage.ReasonLateLeaf},
	}, inv.Carriers, "the late leaf falls in one window per target branch")
	assert.Equal(t, []string{"C1", "C3", "LATE"}, pending(t, s))
	derive(t, s)
	assert.Equal(t, full(t, append(prs, late), repo), edgesOf(t, s, repo))
}

func lateLeafAfterAHead(t *testing.T, s branchlineage.Store) {
	l1 := merged("L1", 1, "feature/a", "develop", 10)
	l1.MergeCommitAt = l1.MergedAt
	c1 := merged("C1", 10, "develop", "main", 30)
	c1.HeadCommitAt = l1.MergeCommitAt
	prs := []branchlineage.PR{l1, c1, merged("C2", 11, "develop", "main", 60)}
	put(t, s, prs...)
	derive(t, s)
	late := merged("LATE", 4, "feature/late", "develop", 20)
	late.MergeCommitAt = late.MergedAt
	inv := put(t, s, late)
	assert.Equal(t, []branchlineage.Stale{{Carrier: "C2", Reason: branchlineage.ReasonLateLeaf}}, inv.Carriers,
		"C1 merged after it, but C1's head did not hold it")
	derive(t, s)
	assert.Equal(t, full(t, append(prs, late), repo), edgesOf(t, s, repo))
	assert.Equal(t, []string{"C1->L1 develop->main 15/15", "C2->LATE develop->main 15/15"}, edgesOf(t, s, repo))
}

func staleCarrierHasNoEdges(t *testing.T, s branchlineage.Store) {
	put(t, s, history()...)
	derive(t, s)
	put(t, s, merged("LATE", 4, "feature/late", "develop", 25))
	got, err := s.EdgesFrom(context.Background(), []string{"C1"})
	require.NoError(t, err)
	assert.Empty(t, got, "a stale carrier reads as no edges, never a mix of old and new")
}

func rederiveReplacesWhole(t *testing.T, s branchlineage.Store) {
	prs := history()
	put(t, s, prs...)
	derive(t, s)
	// L2 moves to another branch: C1 must lose it, not keep it beside the rest.
	moved := merged("L2", 2, "feature/b", "hotfix", 20)
	put(t, s, moved)
	derive(t, s)
	prs[1] = moved
	assert.Equal(t, full(t, prs, repo), edgesOf(t, s, repo))
	got, err := s.EdgesFrom(context.Background(), []string{"C1"})
	require.NoError(t, err)
	assert.Equal(t, []string{"C1->L1 develop->release 15/15"}, render(got))
}

func unmergedLeaves(t *testing.T, s branchlineage.Store) {
	prs := history()
	put(t, s, prs...)
	derive(t, s)
	reopened := prs[0]
	reopened.MergedAt = time.Time{}
	put(t, s, reopened)
	assert.Equal(t, []string{"C1", "C3"}, pending(t, s), "the promotions that carried it are queued, and it is not")
	derive(t, s)
	prs[0] = reopened
	assert.Equal(t, full(t, prs, repo), edgesOf(t, s, repo))
	assert.Empty(t, pending(t, s))
}

func repositoryGivenLater(t *testing.T, s branchlineage.Store) {
	leaf := merged("L1", 1, "feature/a", "develop", 10)
	carrier := merged("C1", 10, "develop", "main", 20)
	carrier.Repository = ""
	put(t, s, leaf, carrier)
	derive(t, s)
	assert.Empty(t, edgesOf(t, s, repo), "with no repository the promotion carries nothing")
	carrier.Repository = repo
	put(t, s, carrier)
	derive(t, s)
	assert.Equal(t, full(t, []branchlineage.PR{leaf, carrier}, repo), edgesOf(t, s, repo))
	assert.Len(t, edgesOf(t, s, repo), 1)
}

func zonedMergeTime(t *testing.T, s branchlineage.Store) {
	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	// 90 calendar days before this merge crosses a daylight-saving change in New
	// York, so the lookback floor depends on the zone unless it is taken in UTC.
	carrier := merged("C", 10, "develop", "main", 0)
	carrier.MergedAt = time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC).In(newYork)
	leaf := merged("L", 1, "feature/f", "develop", 0)
	leaf.MergedAt = time.Date(2026, 2, 10, 0, 30, 0, 0, time.UTC)
	put(t, s, leaf, carrier)
	derive(t, s)
	assert.Equal(t, full(t, []branchlineage.PR{leaf, carrier}, repo), edgesOf(t, s, repo))
	assert.Equal(t, []string{"C->L develop->main 15/15"}, edgesOf(t, s, repo), "the floor is 90 days before the merge in UTC")
}

func badBatchWritesNothing(t *testing.T, s branchlineage.Store) {
	put(t, s, history()...)
	derive(t, s)
	before := edgesOf(t, s, repo)
	_, err := s.Put(context.Background(), merged("NEW", 9, "feature/z", "develop", 70), merged("NEW", 9, "feature/z", "develop", 70))
	require.ErrorIs(t, err, branchlineage.ErrInvalidPR)
	assert.Empty(t, pending(t, s), "a rejected batch queues nothing")
	assert.Equal(t, before, edgesOf(t, s, repo))
}

// randomBatchesConverge feeds random histories in random batches, with late
// arrivals and corrections, deriving between batches, and checks the store
// against a full derivation each time. It is the differential test of the
// incremental invalidation.
func randomBatchesConverge(t *testing.T, newStore NewStore) {
	branches := []string{"feature/a", "feature/b", "feature/c", "develop", "staging", "release", "main"}
	repos := []string{repo, repo, otherRepo, ""}
	for seed := int64(1); seed <= 40; seed++ {
		rng := rand.New(rand.NewSource(seed))
		s := newStore(t)
		state := map[string]branchlineage.PR{}
		for round := 0; round < 6; round++ {
			var batch []branchlineage.PR
			n := 1 + rng.Intn(6)
			used := map[string]bool{}
			for i := 0; i < n; i++ {
				var p branchlineage.PR
				if len(state) > 0 && rng.Intn(3) == 0 {
					keys := sortedKeys(state)
					p = state[keys[rng.Intn(len(keys))]]
				} else {
					num := 1 + rng.Intn(40)
					p = branchlineage.PR{Key: fmt.Sprintf("pr-%d", num), Number: num}
				}
				if used[p.Key] {
					continue
				}
				used[p.Key] = true
				p.Repository = repos[rng.Intn(len(repos))]
				p.HeadBranch = branches[rng.Intn(len(branches))]
				p.BaseBranch = branches[rng.Intn(len(branches))]
				p.MergedAt = base.Add(time.Duration(rng.Intn(20)) * time.Hour)
				if rng.Intn(8) == 0 {
					p.MergedAt = time.Time{}
				}
				if rng.Intn(4) == 0 {
					p.FirstCommitAt = base.Add(time.Duration(rng.Intn(20)) * time.Hour)
				}
				p.CrossRepository = rng.Intn(10) == 0
				p.LinesAdded = int64(rng.Intn(3) * 10)
				p.LinesDeleted = int64(rng.Intn(3) * 5)
				p.HeadCommitAt, p.MergeCommitAt = time.Time{}, time.Time{}
				if p.Merged() && rng.Intn(2) == 0 {
					// A head committed up to a few hours before the merge, so merges
					// land between a promotion's head and its merge.
					p.HeadCommitAt = p.MergedAt.Add(-time.Duration(rng.Intn(4)) * time.Hour)
				}
				if p.Merged() && rng.Intn(2) == 0 {
					// Mostly before the recorded merge, sometimes after it (clock skew).
					p.MergeCommitAt = p.MergedAt.Add(time.Duration(rng.Intn(5)-3) * time.Hour)
				}
				batch = append(batch, p)
			}
			put(t, s, batch...)
			for _, p := range batch {
				state[p.Key] = p
			}
			derive(t, s)
			var all []branchlineage.PR
			for _, k := range sortedKeys(state) {
				all = append(all, state[k])
			}
			for _, r := range []string{repo, otherRepo} {
				require.Equal(t, full(t, all, r), edgesOf(t, s, r), "seed %d round %d repository %s", seed, round, r)
			}
			require.Empty(t, pending(t, s), "seed %d round %d", seed, round)
		}
	}
}

func sortedKeys(m map[string]branchlineage.PR) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
