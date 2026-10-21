// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package storetest is the conformance suite every [issuelinkage.Store] must
// pass.
//
// Each rule is a property the reference runner depends on, asserted only
// through the Store interface, so passing it means an implementation is
// interchangeable with the shipped ones.
package storetest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
)

// NewStore builds an empty store for one sub-test.
type NewStore func(t *testing.T) issuelinkage.Store

// RunConformance runs every rule against the stores newStore builds.
func RunConformance(t *testing.T, newStore NewStore) {
	t.Helper()
	rules := []struct {
		name string
		run  func(*testing.T, issuelinkage.Store)
	}{
		{"ANewRecordIsArmed", newRecordIsArmed},
		{"AnUnchangedPutChangesNothing", unchangedPutChangesNothing},
		{"AChangedRecordRearmsATerminalRow", changedRecordRearms},
		{"AnArmedRowStaysArmedOnAPut", armedStaysArmed},
		{"TheRowKeepsTheNewestUpdate", keepsNewestUpdate},
		{"EligibleOrderAndWindow", eligibleOrderAndWindow},
		{"EligibleHonoursTheBatchSize", eligibleBatchSize},
		{"ForceMarksAndSkipsUnknown", forceMarks},
		{"RearmMovesOnlyTerminalRows", rearmOnlyTerminal},
		{"WriteNeedsARowAndATerminalStatus", writeNeedsRow},
		{"ABadBatchWritesNothing", badBatchWritesNothing},
		{"LookupsReadTheStoredRecords", lookups},
		{"ARunLinksBothSidesAndSettles", runLinksBothSides},
		{"AReplayChangesNothing", replayChangesNothing},
		{"FirstSeenIsKeptAcrossRuns", firstSeenIsKept},
		{"ANilNativeListKeepsTheStoredOne", nilNativeKeeps},
		{"AnEmptyNativeListClearsTheStoredOne", emptyNativeClears},
		{"ALaterMergeMovesTheCloser", laterMergeMovesTheCloser},
		{"AClaimInAPullRequestFollowsTheElection", claimFollowsTheElection},
		{"AMovedClosedByListMovesTheCloser", movedClosedByList},
		{"AnIssuePutReArmsTheRowsWaitingForIt", issuePutRearmsWaiting},
		{"ARecordWithNoUpdateTimeIsDatedByItsFirstRun", noUpdateTimeIsDatedByFirstRun},
		{"ALaterIssueMakesALinkedKeyAmbiguous", laterIssueMakesAmbiguous},
		{"AnIssueThatDropsAKeyReleasesItsLinks", droppedKeyReleasesLinks},
		{"AnIssueThatDropsAKeyResolvesTheAmbiguity", droppedKeyResolvesAmbiguity},
		{"AnUnchangedIssuePutReArmsNothing", unchangedIssuePutRearmsNothing},
		{"AnIssueRowNamingAKeyIsNotReArmed", issueRowNamingAKeyIsNotRearmed},
		{"APullRequestsOwnClaimCannotWinLimit", ownClaimCannotWinLimit},
		{"AnUnmergedOwnClaimLosesTheElection", unmergedOwnClaimLoses},
		{"ANewPullRequestDoesNotReArmAWaitingIssueLimit", newPRDoesNotRearmWaitingIssueLimit},
		{"APaddedNativeKeyIsReArmedByItsIssue", paddedNativeKeyIsRearmed},
		{"ARenamedIssueKeepsItsLinksAndTakesTheNewKey", renamedIssueKeepsAndTakes},
		{"AnIssueThatDropsEveryKeyReleasesItsLinks", droppedEveryKeyReleasesLinks},
		{"AnUnchangedPullRequestPutWithItsIssueIsReArmed", unchangedPRWithItsIssueIsRearmed},
	}
	for _, r := range rules {
		t.Run(r.name, func(t *testing.T) { r.run(t, newStore(t)) })
	}
}

var (
	ctx  = context.Background()
	base = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
)

func pr(key string, number int, body string, minute int) issuelinkage.PR {
	return issuelinkage.PR{
		Key: key, Vendor: issuelinkage.VendorGitHub, Repository: "acme/web", Number: number,
		Title: "change " + key, Body: body, UpdatedAt: base.Add(time.Duration(minute) * time.Minute),
	}
}

func issue(key, ident string, minute int) issuelinkage.Issue {
	return issuelinkage.Issue{
		Key: key, Vendor: issuelinkage.VendorJira, Identifiers: []string{ident},
		Title: "issue " + key, UpdatedAt: base.Add(time.Duration(minute) * time.Minute),
	}
}

func put(t *testing.T, s issuelinkage.Store, prs []issuelinkage.PR, issues []issuelinkage.Issue) {
	t.Helper()
	require.NoError(t, s.Put(ctx, prs, issues))
}

func row(t *testing.T, s issuelinkage.Store, side issuelinkage.Side, key string) issuelinkage.Row {
	t.Helper()
	r, ok, err := s.Row(ctx, issuelinkage.Entity{Side: side, Key: key})
	require.NoError(t, err)
	require.True(t, ok, "row %s %s", side, key)
	return r
}

func terminal(t *testing.T, s issuelinkage.Store, e issuelinkage.Entity, st issuelinkage.Status, at time.Time) {
	t.Helper()
	require.NoError(t, s.Write(ctx, issuelinkage.Result{Entity: e, Status: st}, at))
}

func prE(key string) issuelinkage.Entity {
	return issuelinkage.Entity{Side: issuelinkage.SidePR, Key: key}
}

func issueE(key string) issuelinkage.Entity {
	return issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: key}
}

func eligible(t *testing.T, s issuelinkage.Store, now time.Time, w issuelinkage.Window) []issuelinkage.Entity {
	t.Helper()
	es, err := s.Eligible(ctx, now, w)
	require.NoError(t, err)
	return es
}

func newRecordIsArmed(t *testing.T, s issuelinkage.Store) {
	put(t, s, []issuelinkage.PR{pr("P1", 1, "", 0)}, []issuelinkage.Issue{issue("I1", "PROJ-1", 0)})
	assert.Equal(t, issuelinkage.StatusYetToProcess, row(t, s, issuelinkage.SidePR, "P1").Status)
	assert.Equal(t, issuelinkage.StatusYetToProcess, row(t, s, issuelinkage.SideIssue, "I1").Status)
	assert.Equal(t, []issuelinkage.Entity{prE("P1"), issueE("I1")}, eligible(t, s, base, issuelinkage.Window{}))
	got, ok, err := s.PR(ctx, "P1")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "change P1", got.Title)
}

func unchangedPutChangesNothing(t *testing.T, s issuelinkage.Store) {
	p := pr("P1", 1, "Fixes PROJ-1", 0)
	put(t, s, []issuelinkage.PR{p}, nil)
	terminal(t, s, prE("P1"), issuelinkage.StatusProcessed, base)
	p.UpdatedAt = p.UpdatedAt.In(time.FixedZone("east", 5*3600))
	put(t, s, []issuelinkage.PR{p}, nil)
	assert.Equal(t, issuelinkage.StatusProcessed, row(t, s, issuelinkage.SidePR, "P1").Status)
}

func changedRecordRearms(t *testing.T, s issuelinkage.Store) {
	p := pr("P1", 1, "Fixes PROJ-1", 0)
	put(t, s, []issuelinkage.PR{p}, nil)
	terminal(t, s, prE("P1"), issuelinkage.StatusNotFound, base)
	p.Body = "Fixes PROJ-2"
	put(t, s, []issuelinkage.PR{p}, nil)
	assert.Equal(t, issuelinkage.StatusToBeReprocessed, row(t, s, issuelinkage.SidePR, "P1").Status)
}

func armedStaysArmed(t *testing.T, s issuelinkage.Store) {
	p := pr("P1", 1, "", 0)
	put(t, s, []issuelinkage.PR{p}, nil)
	require.NoError(t, s.Force(ctx, prE("P1")))
	p.Body = "changed"
	put(t, s, []issuelinkage.PR{p}, nil)
	assert.Equal(t, issuelinkage.StatusMarkedForForceReprocess, row(t, s, issuelinkage.SidePR, "P1").Status)
}

func keepsNewestUpdate(t *testing.T, s issuelinkage.Store) {
	put(t, s, []issuelinkage.PR{pr("P1", 1, "", 30)}, nil)
	put(t, s, []issuelinkage.PR{pr("P1", 1, "older", 10)}, nil)
	assert.True(t, row(t, s, issuelinkage.SidePR, "P1").UpdatedAt.Equal(base.Add(30*time.Minute)))
}

// eligibleOrderAndWindow: armed rows first, then partially processed rows
// updated within the window and not run within the floor; a not-found or
// processed row is never picked.
func eligibleOrderAndWindow(t *testing.T, s issuelinkage.Store) {
	now := base.Add(40 * 24 * time.Hour)
	put(t, s, []issuelinkage.PR{
		pr("ARMED", 1, "", 0),
		pr("RECENT", 2, "", 39*24*60),
		pr("STALE", 3, "", 0),
		pr("JUSTRUN", 4, "", 39*24*60),
		pr("NOTFOUND", 5, "", 39*24*60),
		pr("DONE", 6, "", 39*24*60),
		pr("EDGE", 7, "", 10*24*60),
		pr("FORCED", 8, "", 0),
		pr("FLOOR", 9, "", 39*24*60),
	}, nil)
	terminal(t, s, prE("RECENT"), issuelinkage.StatusPartiallyProcessed, now.Add(-time.Hour))
	terminal(t, s, prE("STALE"), issuelinkage.StatusPartiallyProcessed, now.Add(-time.Hour))
	terminal(t, s, prE("JUSTRUN"), issuelinkage.StatusPartiallyProcessed, now.Add(-time.Minute))
	terminal(t, s, prE("NOTFOUND"), issuelinkage.StatusNotFound, now.Add(-time.Hour))
	terminal(t, s, prE("DONE"), issuelinkage.StatusProcessed, now.Add(-time.Hour))
	// EDGE was updated exactly at the window's edge: still in.
	terminal(t, s, prE("EDGE"), issuelinkage.StatusPartiallyProcessed, now.Add(-2*time.Hour))
	// FORCED ran more recently than RECENT, but an armed row goes first.
	terminal(t, s, prE("FORCED"), issuelinkage.StatusProcessed, now.Add(-10*time.Minute))
	require.NoError(t, s.Force(ctx, prE("FORCED")))
	// FLOOR ran exactly RetryFloor ago: not yet due.
	terminal(t, s, prE("FLOOR"), issuelinkage.StatusPartiallyProcessed, now.Add(-issuelinkage.RetryFloor))
	assert.Equal(t, []issuelinkage.Entity{prE("ARMED"), prE("FORCED"), prE("EDGE"), prE("RECENT")}, eligible(t, s, now, issuelinkage.Window{}))
	assert.Equal(t, []issuelinkage.Entity{prE("ARMED"), prE("FORCED"), prE("EDGE"), prE("RECENT"), prE("STALE")},
		eligible(t, s, now, issuelinkage.Window{Retry: 41 * 24 * time.Hour}))
}

func eligibleBatchSize(t *testing.T, s issuelinkage.Store) {
	put(t, s, []issuelinkage.PR{pr("A", 1, "", 0), pr("B", 2, "", 5), pr("C", 3, "", 9)}, nil)
	// Never run, so the newest update goes first.
	assert.Equal(t, []issuelinkage.Entity{prE("C"), prE("B")}, eligible(t, s, base, issuelinkage.Window{BatchSize: 2}))
}

func forceMarks(t *testing.T, s issuelinkage.Store) {
	put(t, s, []issuelinkage.PR{pr("P1", 1, "", 0)}, nil)
	terminal(t, s, prE("P1"), issuelinkage.StatusProcessed, base)
	require.NoError(t, s.Force(ctx, prE("P1"), prE("NOPE")))
	assert.Equal(t, issuelinkage.StatusMarkedForForceReprocess, row(t, s, issuelinkage.SidePR, "P1").Status)
	_, ok, err := s.Row(ctx, prE("NOPE"))
	require.NoError(t, err)
	assert.False(t, ok)
}

func rearmOnlyTerminal(t *testing.T, s issuelinkage.Store) {
	put(t, s, []issuelinkage.PR{pr("DONE", 1, "", 0), pr("NEW", 2, "", 0)}, nil)
	terminal(t, s, prE("DONE"), issuelinkage.StatusProcessed, base)
	require.NoError(t, s.Rearm(ctx, prE("DONE")))
	require.NoError(t, s.Rearm(ctx, prE("NEW")))
	require.NoError(t, s.Rearm(ctx, prE("NOPE")))
	assert.Equal(t, issuelinkage.StatusToBeReprocessed, row(t, s, issuelinkage.SidePR, "DONE").Status)
	assert.Equal(t, issuelinkage.StatusYetToProcess, row(t, s, issuelinkage.SidePR, "NEW").Status)
}

func writeNeedsRow(t *testing.T, s issuelinkage.Store) {
	require.Error(t, s.Write(ctx, issuelinkage.Result{Entity: prE("NOPE"), Status: issuelinkage.StatusProcessed}, base))
	put(t, s, []issuelinkage.PR{pr("P1", 1, "", 0)}, nil)
	require.Error(t, s.Write(ctx, issuelinkage.Result{Entity: prE("P1"), Status: issuelinkage.StatusYetToProcess}, base))
	terminal(t, s, prE("P1"), issuelinkage.StatusProcessed, base)
	r := row(t, s, issuelinkage.SidePR, "P1")
	assert.True(t, r.LastRunAt.Equal(base))
}

func badBatchWritesNothing(t *testing.T, s issuelinkage.Store) {
	err := s.Put(ctx, []issuelinkage.PR{pr("P1", 1, "", 0), pr("P1", 2, "", 0)}, nil)
	require.ErrorIs(t, err, issuelinkage.ErrInvalidRecord)
	err = s.Put(ctx, []issuelinkage.PR{pr("P2", 1, "", 0)}, []issuelinkage.Issue{issue("", "PROJ-1", 0)})
	require.ErrorIs(t, err, issuelinkage.ErrInvalidRecord)
	rows, err := s.Rows(ctx)
	require.NoError(t, err)
	assert.Empty(t, rows)
}

func lookups(t *testing.T, s issuelinkage.Store) {
	merged := pr("P1", 7, "", 0)
	merged.MergedAt = base.Add(time.Hour)
	put(t, s, []issuelinkage.PR{merged, pr("P2", 8, "", 0)},
		[]issuelinkage.Issue{issue("I1", "PROJ-1", 0), issue("I2", "PROJ-1", 0), issue("I3", "PROJ-3", 0)})
	refs, err := s.IssuesByIdentifier(ctx, "PROJ-1")
	require.NoError(t, err)
	assert.Equal(t, []issuelinkage.Ref{{Key: "I1", Vendor: issuelinkage.VendorJira}, {Key: "I2", Vendor: issuelinkage.VendorJira}}, refs)
	refs, err = s.PRsByNumber(ctx, "acme/web", 7)
	require.NoError(t, err)
	assert.Equal(t, []issuelinkage.Ref{{Key: "P1", Vendor: issuelinkage.VendorGitHub}}, refs)
	at, err := s.MergedAt(ctx, []string{"P1", "P2", "NOPE"})
	require.NoError(t, err)
	require.Len(t, at, 1)
	assert.True(t, at["P1"].Equal(merged.MergedAt))
	require.NoError(t, s.Write(ctx, issuelinkage.Result{Entity: issueE("I3"), Status: issuelinkage.StatusProcessed, Closer: "P1"}, base))
	closers, err := s.Closers(ctx, []string{"I1", "I3"})
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"I3": "P1"}, closers)
	trackers, err := s.Trackers(ctx)
	require.NoError(t, err)
	assert.Equal(t, []issuelinkage.Vendor{issuelinkage.VendorJira}, trackers)
	// A re-put replaces the identifiers whole.
	put(t, s, nil, []issuelinkage.Issue{issue("I2", "PROJ-2", 0)})
	refs, err = s.IssuesByIdentifier(ctx, "PROJ-1")
	require.NoError(t, err)
	assert.Equal(t, []issuelinkage.Ref{{Key: "I1", Vendor: issuelinkage.VendorJira}}, refs)
}

// runLinksBothSides: a pull request naming an issue links it, the run re-arms
// the issue so its row records the link too, and the pair then settles.
func runLinksBothSides(t *testing.T, s issuelinkage.Store) {
	put(t, s, []issuelinkage.PR{pr("P1", 1, "Fixes PROJ-1", 0)}, []issuelinkage.Issue{issue("I1", "PROJ-1", 0)})
	now := base.Add(time.Hour)
	rep, err := issuelinkage.Run(ctx, s, now, issuelinkage.Config{})
	require.NoError(t, err)
	assert.Empty(t, rep.Failures)
	assert.Equal(t, 2, rep.Processed)
	p := row(t, s, issuelinkage.SidePR, "P1")
	assert.Equal(t, issuelinkage.StatusProcessed, p.Status)
	require.Len(t, p.Result.Links, 1)
	assert.Equal(t, "I1", p.Result.Links[0].Issue)
	assert.Equal(t, issuelinkage.RelationFixes, p.Result.Links[0].Relation)
	for i := 1; i <= 3; i++ {
		_, err = issuelinkage.Run(ctx, s, now.Add(time.Duration(i)*time.Hour), issuelinkage.Config{})
		require.NoError(t, err)
	}
	es, err := s.Eligible(ctx, now.Add(4*time.Hour), issuelinkage.Window{})
	require.NoError(t, err)
	assert.Empty(t, es, "a linked pair settles")
}

func rows(t *testing.T, s issuelinkage.Store) []issuelinkage.Row {
	t.Helper()
	rs, err := s.Rows(ctx)
	require.NoError(t, err)
	return rs
}

func run(t *testing.T, s issuelinkage.Store, now time.Time) {
	t.Helper()
	rep, err := issuelinkage.Run(ctx, s, now, issuelinkage.Config{})
	require.NoError(t, err)
	require.Empty(t, rep.Failures)
}

// settle runs at now until no armed row is left.
func settle(t *testing.T, s issuelinkage.Store, now time.Time) {
	t.Helper()
	for i := 0; i < 10; i++ {
		run(t, s, now)
		armed := false
		for _, r := range rows(t, s) {
			armed = armed || r.Status.Armed()
		}
		if !armed {
			return
		}
	}
	t.Fatal("rows did not settle")
}

func forceAll(t *testing.T, s issuelinkage.Store) {
	t.Helper()
	for _, r := range rows(t, s) {
		require.NoError(t, s.Force(ctx, r.Entity))
	}
}

// replayChangesNothing: forcing every row and running again at the same
// clock writes the same rows, field for field.
func replayChangesNothing(t *testing.T, s issuelinkage.Store) {
	native := issue("I1", "PROJ-1", 0)
	native.Vendor = issuelinkage.VendorGitHub
	native.Identifiers = []string{"acme/web#1"}
	native.NativePRRefs = []issuelinkage.NativeRef{{Key: "acme/web#PR-10", Source: issuelinkage.VendorGitHub}, {Key: "acme/web#PR-11", Source: issuelinkage.VendorGitHub}}
	p10, p11 := pr("P10", 10, "Fixes acme/web#1 PROJ-2 PROJ-3", 0), pr("P11", 11, "", 0)
	p10.MergedAt, p11.MergedAt = base, base
	put(t, s, []issuelinkage.PR{p10, p11}, []issuelinkage.Issue{native, issue("I2", "PROJ-2", 0), issue("I3", "PROJ-3", 0)})
	now := base.Add(time.Hour)
	settle(t, s, now)
	first := rows(t, s)
	for i := 0; i < 5; i++ {
		forceAll(t, s)
		settle(t, s, now)
		assert.Equal(t, first, rows(t, s), "replay %d", i)
	}
}

// firstSeenIsKept: a later run keeps the link's FirstSeenAt and moves its
// LastCorroboratedAt.
func firstSeenIsKept(t *testing.T, s issuelinkage.Store) {
	put(t, s, []issuelinkage.PR{pr("P1", 1, "Fixes PROJ-1", 0)}, []issuelinkage.Issue{issue("I1", "PROJ-1", 0)})
	t1, t2 := base.Add(time.Hour), base.Add(2*time.Hour)
	run(t, s, t1)
	forceAll(t, s)
	run(t, s, t2)
	ln := row(t, s, issuelinkage.SidePR, "P1").Result.Links[0]
	assert.True(t, ln.FirstSeenAt.Equal(t1), "first seen %s", ln.FirstSeenAt)
	assert.True(t, ln.LastCorroboratedAt.Equal(t2), "last corroborated %s", ln.LastCorroboratedAt)
}

func nilNativeKeeps(t *testing.T, s issuelinkage.Store) {
	p := pr("P1", 1, "", 0)
	p.NativeIssueRefs = []issuelinkage.NativeRef{{Key: "acme/web#7", Source: issuelinkage.VendorGitHub}}
	is := issue("I1", "PROJ-1", 0)
	is.NativePRRefs = []issuelinkage.NativeRef{{Key: "acme/web#PR-1", Source: issuelinkage.VendorLinear}}
	is.NativeRefsTruncated = true
	put(t, s, []issuelinkage.PR{p}, []issuelinkage.Issue{is})
	terminal(t, s, prE("P1"), issuelinkage.StatusProcessed, base)
	p.NativeIssueRefs, is.NativePRRefs, is.NativeRefsTruncated = nil, nil, false
	put(t, s, []issuelinkage.PR{p}, []issuelinkage.Issue{is})
	gotPR, _, err := s.PR(ctx, "P1")
	require.NoError(t, err)
	assert.Equal(t, []issuelinkage.NativeRef{{Key: "acme/web#7", Source: issuelinkage.VendorGitHub}}, gotPR.NativeIssueRefs)
	gotIssue, _, err := s.Issue(ctx, "I1")
	require.NoError(t, err)
	assert.Len(t, gotIssue.NativePRRefs, 1)
	assert.True(t, gotIssue.NativeRefsTruncated)
	assert.Equal(t, issuelinkage.StatusProcessed, row(t, s, issuelinkage.SidePR, "P1").Status, "nothing changed")
}

func emptyNativeClears(t *testing.T, s issuelinkage.Store) {
	p := pr("P1", 1, "", 0)
	p.NativeIssueRefs = []issuelinkage.NativeRef{{Key: "acme/web#7", Source: issuelinkage.VendorGitHub}}
	put(t, s, []issuelinkage.PR{p}, nil)
	terminal(t, s, prE("P1"), issuelinkage.StatusProcessed, base)
	p.NativeIssueRefs = []issuelinkage.NativeRef{}
	put(t, s, []issuelinkage.PR{p}, nil)
	got, _, err := s.PR(ctx, "P1")
	require.NoError(t, err)
	assert.Empty(t, got.NativeIssueRefs)
	assert.NotNil(t, got.NativeIssueRefs)
	assert.Equal(t, issuelinkage.StatusToBeReprocessed, row(t, s, issuelinkage.SidePR, "P1").Status)
}

// laterMergeMovesTheCloser: an issue's closer election runs again when a
// claimer it links merges later, so the newest merge wins, and each pull
// request's own row reads the new election.
func laterMergeMovesTheCloser(t *testing.T, s issuelinkage.Store) {
	t1 := base.Add(time.Hour)
	p11, p12 := pr("PR-11", 11, "Fixes acme/web#9", 0), pr("PR-12", 12, "Fixes acme/web#9", 0)
	p11.MergedAt = t1
	is := issue("I9", "acme/web#9", 0)
	is.Vendor = issuelinkage.VendorGitHub
	is.NativePRRefs = []issuelinkage.NativeRef{{Key: "acme/web#PR-11", Source: issuelinkage.VendorGitHub}, {Key: "acme/web#PR-12", Source: issuelinkage.VendorGitHub}}
	put(t, s, []issuelinkage.PR{p11, p12}, []issuelinkage.Issue{is})
	settle(t, s, t1)
	assert.Equal(t, "PR-11", row(t, s, issuelinkage.SideIssue, "I9").Result.Closer)

	p12.MergedAt = t1.Add(48 * time.Hour)
	put(t, s, []issuelinkage.PR{p12}, nil)
	settle(t, s, t1.Add(49*time.Hour))
	got := row(t, s, issuelinkage.SideIssue, "I9").Result
	assert.Equal(t, "PR-12", got.Closer)
	for _, l := range got.Links {
		assert.Equal(t, l.PR == "PR-12", l.Closes, l.PR)
	}
	// Each pull request's own row reads the new election.
	for key, closes := range map[string]bool{"PR-11": false, "PR-12": true} {
		links := row(t, s, issuelinkage.SidePR, key).Result.Links
		require.Len(t, links, 1, key)
		assert.Equal(t, closes, links[0].Closes, key)
	}
}

func linkOf(t *testing.T, s issuelinkage.Store, prKey, issueKey string) issuelinkage.Link {
	t.Helper()
	for _, l := range row(t, s, issuelinkage.SidePR, prKey).Result.Links {
		if l.Issue == issueKey {
			return l
		}
	}
	t.Fatalf("%s has no link to %s", prKey, issueKey)
	return issuelinkage.Link{}
}

func assertLost(t *testing.T, l issuelinkage.Link) {
	t.Helper()
	assert.False(t, l.Closes, l.PR)
	assert.Equal(t, issuelinkage.RelationPartOf, l.Relation, l.PR)
	assert.Contains(t, l.Blocking, issuelinkage.BlockedNonElectedCloser, l.PR)
}

// claimFollowsTheElection: a pull request whose own text claims an issue
// stops claiming it once the issue elects another pull request, though the
// issue's evidence never names it.
func claimFollowsTheElection(t *testing.T, s issuelinkage.Store) {
	t0 := base.Add(time.Hour)
	p1, p2 := pr("PR-1", 1, "Fixes ENG-1", 0), pr("PR-2", 2, "", 0)
	p1.HeadBranch = "eng-1"
	p1.MergedAt, p2.MergedAt = t0, t0.Add(time.Hour)
	is := issuelinkage.Issue{Key: "u1", Vendor: issuelinkage.VendorLinear, Identifiers: []string{"ENG-1"},
		NativePRRefs: []issuelinkage.NativeRef{{Key: "acme/web#PR-2", Source: issuelinkage.VendorLinear}}}
	put(t, s, []issuelinkage.PR{p1, p2}, []issuelinkage.Issue{is})
	settle(t, s, t0.Add(2*time.Hour))
	assert.Equal(t, "PR-2", row(t, s, issuelinkage.SideIssue, "u1").Result.Closer)
	assertLost(t, linkOf(t, s, "PR-1", "u1"))
}

// movedClosedByList: when an issue's closed-by list moves from one pull
// request to another, the first stops claiming it.
func movedClosedByList(t *testing.T, s issuelinkage.Store) {
	t0 := base.Add(time.Hour)
	p1, p2 := pr("PR-1", 1, "Fixes acme/web#9", 0), pr("PR-2", 2, "", 0)
	p1.MergedAt, p2.MergedAt = t0, t0.Add(time.Hour)
	is := issue("I9", "acme/web#9", 0)
	is.Vendor = issuelinkage.VendorGitHub
	is.NativePRRefs = []issuelinkage.NativeRef{{Key: "acme/web#PR-1", Source: issuelinkage.VendorGitHub}}
	put(t, s, []issuelinkage.PR{p1, p2}, []issuelinkage.Issue{is})
	settle(t, s, t0.Add(2*time.Hour))
	assert.True(t, linkOf(t, s, "PR-1", "I9").Closes)

	is.NativePRRefs = []issuelinkage.NativeRef{{Key: "acme/web#PR-2", Source: issuelinkage.VendorGitHub}}
	put(t, s, nil, []issuelinkage.Issue{is})
	settle(t, s, t0.Add(3*time.Hour))
	assert.Equal(t, "PR-2", row(t, s, issuelinkage.SideIssue, "I9").Result.Closer)
	assertLost(t, linkOf(t, s, "PR-1", "I9"))

	// The list moves back: PR-1 closes again.
	is.NativePRRefs = []issuelinkage.NativeRef{{Key: "acme/web#PR-1", Source: issuelinkage.VendorGitHub}}
	put(t, s, nil, []issuelinkage.Issue{is})
	settle(t, s, t0.Add(4*time.Hour))
	assert.Equal(t, "PR-1", row(t, s, issuelinkage.SideIssue, "I9").Result.Closer)
	l := linkOf(t, s, "PR-1", "I9")
	assert.True(t, l.Closes)
	assert.Equal(t, issuelinkage.RelationFixes, l.Relation)

	// The list empties: no closer, and the pull request's own claim stands.
	is.NativePRRefs = []issuelinkage.NativeRef{}
	put(t, s, nil, []issuelinkage.Issue{is})
	settle(t, s, t0.Add(5*time.Hour))
	assert.Equal(t, "", row(t, s, issuelinkage.SideIssue, "I9").Result.Closer)
	l = linkOf(t, s, "PR-1", "I9")
	assert.True(t, l.Closes)
	assert.Equal(t, issuelinkage.RelationFixes, l.Relation)
}

// issuePutRearmsWaiting: an issue that arrives after its pull request links,
// even when the pull request's update time is outside the retry window, or
// its key was read case-folded from a branch name.
func issuePutRearmsWaiting(t *testing.T, s issuelinkage.Store) {
	old := pr("OLD", 1, "Fixes OPS-12", 0)
	old.UpdatedAt = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	folded := pr("FOLDED", 2, "", 0)
	folded.HeadBranch = "ops-13-retry"
	put(t, s, []issuelinkage.PR{old, folded}, nil)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	settle(t, s, now)
	assert.Equal(t, issuelinkage.StatusPartiallyProcessed, row(t, s, issuelinkage.SidePR, "OLD").Status)
	assert.Empty(t, eligible(t, s, now.Add(time.Hour), issuelinkage.Window{}), "outside the window, nothing retries")

	put(t, s, nil, []issuelinkage.Issue{issue("I14", "OPS-14", 0)})
	assert.Equal(t, issuelinkage.StatusPartiallyProcessed, row(t, s, issuelinkage.SidePR, "OLD").Status, "OPS-14 is not what OLD waits for")
	put(t, s, nil, []issuelinkage.Issue{issue("I12", "OPS-12", 0), issue("I13", "OPS-13", 0)})
	settle(t, s, now.Add(time.Hour))
	assert.Equal(t, "I12", linkOf(t, s, "OLD", "I12").Issue)
	assert.Equal(t, "I13", linkOf(t, s, "FOLDED", "I13").Issue)
}

// noUpdateTimeIsDatedByFirstRun: a record put without an update time is
// dated by its first run, so its retry window opens.
func noUpdateTimeIsDatedByFirstRun(t *testing.T, s issuelinkage.Store) {
	p := pr("P1", 1, "Fixes OPS-12", 0)
	p.UpdatedAt = time.Time{}
	put(t, s, []issuelinkage.PR{p}, nil)
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	settle(t, s, now)
	assert.True(t, row(t, s, issuelinkage.SidePR, "P1").UpdatedAt.Equal(now))
	assert.Empty(t, eligible(t, s, now.Add(issuelinkage.RetryFloor), issuelinkage.Window{}), "not due at the floor")
	assert.Equal(t, []issuelinkage.Entity{prE("P1")}, eligible(t, s, now.Add(issuelinkage.RetryFloor+time.Second), issuelinkage.Window{}))
	assert.Equal(t, []issuelinkage.Entity{prE("P1")}, eligible(t, s, now.Add(issuelinkage.DefaultRetryWindow), issuelinkage.Window{}), "in at the window's edge")
	assert.Empty(t, eligible(t, s, now.Add(issuelinkage.DefaultRetryWindow+time.Second), issuelinkage.Window{}), "out past it")
}

func unresolvedOf(t *testing.T, s issuelinkage.Store, prKey string) []string {
	t.Helper()
	var out []string
	for _, u := range row(t, s, issuelinkage.SidePR, prKey).Result.Unresolved {
		out = append(out, u.Candidate.Identifier+"/"+u.Reason)
	}
	return out
}

func linkedIssues(t *testing.T, s issuelinkage.Store, prKey string) []string {
	t.Helper()
	var out []string
	for _, l := range row(t, s, issuelinkage.SidePR, prKey).Result.Links {
		out = append(out, l.Issue)
	}
	return out
}

// laterIssueMakesAmbiguous: a second issue answering to a key a pull request
// already linked makes the key ambiguous, as if both had arrived together.
func laterIssueMakesAmbiguous(t *testing.T, s issuelinkage.Store) {
	put(t, s, []issuelinkage.PR{pr("P3", 3, "Fixes OPS-30", 0)}, []issuelinkage.Issue{issue("C30", "OPS-30", 0)})
	now := base.Add(time.Hour)
	settle(t, s, now)
	assert.Equal(t, []string{"C30"}, linkedIssues(t, s, "P3"))
	put(t, s, nil, []issuelinkage.Issue{issue("D30", "OPS-30", 0)})
	settle(t, s, now.Add(time.Hour))
	assert.Empty(t, linkedIssues(t, s, "P3"))
	assert.Equal(t, []string{"OPS-30/ambiguous"}, unresolvedOf(t, s, "P3"))
}

// droppedKeyReleasesLinks: an issue whose key changes away from the one a
// pull request named no longer links it.
func droppedKeyReleasesLinks(t *testing.T, s issuelinkage.Store) {
	put(t, s, []issuelinkage.PR{pr("P4", 4, "Fixes OPS-40", 0)}, []issuelinkage.Issue{issue("E40", "OPS-40", 0)})
	now := base.Add(time.Hour)
	settle(t, s, now)
	assert.Equal(t, []string{"E40"}, linkedIssues(t, s, "P4"))
	put(t, s, nil, []issuelinkage.Issue{issue("E40", "NEW-1", 30)})
	settle(t, s, now.Add(time.Hour))
	assert.Empty(t, linkedIssues(t, s, "P4"))
	assert.Equal(t, []string{"OPS-40/not_found"}, unresolvedOf(t, s, "P4"))
}

// droppedKeyResolvesAmbiguity: when one of two issues answering to a key
// drops it, the pull request links the other, even past the retry window.
func droppedKeyResolvesAmbiguity(t *testing.T, s issuelinkage.Store) {
	p := pr("P2", 2, "Fixes OPS-20", 0)
	p.UpdatedAt = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	put(t, s, []issuelinkage.PR{p}, []issuelinkage.Issue{issue("A20", "OPS-20", 0), issue("B20", "OPS-20", 0)})
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	settle(t, s, now)
	assert.Equal(t, []string{"OPS-20/ambiguous"}, unresolvedOf(t, s, "P2"))
	put(t, s, nil, []issuelinkage.Issue{issue("B20", "OPS-21", 30)})
	settle(t, s, now.Add(time.Hour))
	assert.Equal(t, []string{"A20"}, linkedIssues(t, s, "P2"))
	assert.Empty(t, unresolvedOf(t, s, "P2"))
}

// unchangedIssuePutRearmsNothing: putting an issue again unchanged does not
// re-arm the pull request waiting on its key.
func unchangedIssuePutRearmsNothing(t *testing.T, s issuelinkage.Store) {
	p := pr("P2", 2, "Fixes OPS-20", 0)
	p.UpdatedAt = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	a := issue("A20", "OPS-20", 0)
	put(t, s, []issuelinkage.PR{p}, []issuelinkage.Issue{a, issue("B20", "OPS-20", 0)})
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	settle(t, s, now)
	put(t, s, nil, []issuelinkage.Issue{a})
	assert.Equal(t, issuelinkage.StatusPartiallyProcessed, row(t, s, issuelinkage.SidePR, "P2").Status)
	assert.Empty(t, eligible(t, s, now.Add(time.Hour), issuelinkage.Window{}))
}

// issueRowNamingAKeyIsNotRearmed: an issue's row whose text names another
// issue's key stays as it is when that issue is put; only pull-request rows
// wait for issues.
func issueRowNamingAKeyIsNotRearmed(t *testing.T, s issuelinkage.Store) {
	dup := issue("I1", "OPS-11", 0)
	dup.Body = "Duplicate of OPS-12"
	put(t, s, nil, []issuelinkage.Issue{dup})
	now := base.Add(time.Hour)
	settle(t, s, now)
	assert.Equal(t, issuelinkage.StatusNotFound, row(t, s, issuelinkage.SideIssue, "I1").Status)
	put(t, s, nil, []issuelinkage.Issue{issue("I12", "OPS-12", 0)})
	assert.Equal(t, issuelinkage.StatusNotFound, row(t, s, issuelinkage.SideIssue, "I1").Status)
}

// ownClaimCannotWinLimit pins that the election reads only the issue's own
// evidence: a pull request whose own text claims the issue loses to the one
// the issue names, even when it merged later.
func ownClaimCannotWinLimit(t *testing.T, s issuelinkage.Store) {
	t0 := base.Add(time.Hour)
	p1, p2 := pr("PR-1", 1, "Fixes acme/web#9", 0), pr("PR-2", 2, "", 0)
	p1.MergedAt, p2.MergedAt = t0.Add(time.Hour), t0
	is := issue("I9", "acme/web#9", 0)
	is.Vendor = issuelinkage.VendorGitHub
	is.NativePRRefs = []issuelinkage.NativeRef{{Key: "acme/web#PR-2", Source: issuelinkage.VendorGitHub}}
	put(t, s, []issuelinkage.PR{p1, p2}, []issuelinkage.Issue{is})
	settle(t, s, t0.Add(2*time.Hour))
	assert.Equal(t, "PR-2", row(t, s, issuelinkage.SideIssue, "I9").Result.Closer)
	assertLost(t, linkOf(t, s, "PR-1", "I9"))
}

// unmergedOwnClaimLoses: an open pull request claiming the issue in its own
// text loses to the issue's elected closer.
func unmergedOwnClaimLoses(t *testing.T, s issuelinkage.Store) {
	t0 := base.Add(time.Hour)
	p2, p4 := pr("PR-2", 2, "", 0), pr("PR-4", 4, "Fixes acme/web#9", 0)
	p2.MergedAt = t0
	is := issue("I9", "acme/web#9", 0)
	is.Vendor = issuelinkage.VendorGitHub
	is.NativePRRefs = []issuelinkage.NativeRef{{Key: "acme/web#PR-2", Source: issuelinkage.VendorGitHub}}
	put(t, s, []issuelinkage.PR{p2, p4}, []issuelinkage.Issue{is})
	settle(t, s, t0.Add(2*time.Hour))
	assertLost(t, linkOf(t, s, "PR-4", "I9"))
}

// newPRDoesNotRearmWaitingIssueLimit pins that putting a pull request does
// not re-arm an issue row whose closed-by list names it: the row stays
// partially processed, and only the retry window reads it again.
func newPRDoesNotRearmWaitingIssueLimit(t *testing.T, s issuelinkage.Store) {
	is := issue("I9", "acme/web#9", 0)
	is.Vendor = issuelinkage.VendorGitHub
	is.NativePRRefs = []issuelinkage.NativeRef{{Key: "acme/web#PR-2", Source: issuelinkage.VendorGitHub}}
	put(t, s, nil, []issuelinkage.Issue{is})
	settle(t, s, base.Add(time.Hour))
	require.Equal(t, issuelinkage.StatusPartiallyProcessed, row(t, s, issuelinkage.SideIssue, "I9").Status)
	put(t, s, []issuelinkage.PR{pr("PR-2", 2, "", 0)}, nil)
	assert.Equal(t, issuelinkage.StatusPartiallyProcessed, row(t, s, issuelinkage.SideIssue, "I9").Status)
}

// outsideWindow is an update time outside the retry window of a run at
// afterWindow, so only a re-arm reads such a row again.
var (
	outsideWindow = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	afterWindow   = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
)

// paddedNativeKeyIsRearmed: a native key with white space around it is read
// trimmed, so the issue that answers to it re-arms the row waiting on it.
func paddedNativeKeyIsRearmed(t *testing.T, s issuelinkage.Store) {
	p := pr("WS", 1, "", 0)
	p.UpdatedAt = outsideWindow
	p.NativeIssueRefs = []issuelinkage.NativeRef{{Key: " OPS-12", Source: issuelinkage.VendorGitHub}}
	put(t, s, []issuelinkage.PR{p}, nil)
	settle(t, s, afterWindow)
	assert.Equal(t, []string{"OPS-12/not_found"}, unresolvedOf(t, s, "WS"))
	put(t, s, nil, []issuelinkage.Issue{issue("I12", "OPS-12", 0)})
	settle(t, s, afterWindow.Add(time.Hour))
	assert.Equal(t, []string{"I12"}, linkedIssues(t, s, "WS"))
}

// renamedIssueKeepsAndTakes: a Jira issue renamed from OPS-9 to NEW-9 that
// answers to both links the pull request waiting on NEW-9 and keeps the one
// linked through OPS-9.
func renamedIssueKeepsAndTakes(t *testing.T, s issuelinkage.Store) {
	old, waiting := pr("P1", 1, "Fixes OPS-9", 0), pr("P2", 2, "Fixes NEW-9", 0)
	old.UpdatedAt, waiting.UpdatedAt = outsideWindow, outsideWindow
	put(t, s, []issuelinkage.PR{old, waiting}, []issuelinkage.Issue{issue("I9", "OPS-9", 0)})
	settle(t, s, afterWindow)
	assert.Equal(t, []string{"NEW-9/not_found"}, unresolvedOf(t, s, "P2"))
	renamed := issue("I9", "NEW-9", 30)
	renamed.Identifiers = []string{"NEW-9", "OPS-9"}
	put(t, s, nil, []issuelinkage.Issue{renamed})
	settle(t, s, afterWindow.Add(time.Hour))
	assert.Equal(t, []string{"I9"}, linkedIssues(t, s, "P1"))
	assert.Equal(t, []string{"I9"}, linkedIssues(t, s, "P2"))
}

// droppedEveryKeyReleasesLinks: an issue put with no identifiers at all
// releases the pull request that linked it through its old key.
func droppedEveryKeyReleasesLinks(t *testing.T, s issuelinkage.Store) {
	put(t, s, []issuelinkage.PR{pr("P7", 7, "Fixes OPS-7", 0)}, []issuelinkage.Issue{issue("I7", "OPS-7", 0)})
	now := base.Add(time.Hour)
	settle(t, s, now)
	assert.Equal(t, []string{"I7"}, linkedIssues(t, s, "P7"))
	bare := issue("I7", "", 30)
	bare.Identifiers = []string{}
	put(t, s, nil, []issuelinkage.Issue{bare})
	settle(t, s, now.Add(time.Hour))
	assert.Empty(t, linkedIssues(t, s, "P7"))
	assert.Equal(t, []string{"OPS-7/not_found"}, unresolvedOf(t, s, "P7"))
}

// unchangedPRWithItsIssueIsRearmed: a finished pull request put again
// unchanged in the same batch as the issue it waits for is re-armed by that
// issue and links it.
func unchangedPRWithItsIssueIsRearmed(t *testing.T, s issuelinkage.Store) {
	p := pr("P5", 5, "Fixes OPS-5", 0)
	p.UpdatedAt = outsideWindow
	put(t, s, []issuelinkage.PR{p}, nil)
	settle(t, s, afterWindow)
	require.Equal(t, issuelinkage.StatusPartiallyProcessed, row(t, s, issuelinkage.SidePR, "P5").Status)
	put(t, s, []issuelinkage.PR{p}, []issuelinkage.Issue{issue("I5", "OPS-5", 0)})
	assert.True(t, row(t, s, issuelinkage.SidePR, "P5").Status.Armed())
	settle(t, s, afterWindow.Add(time.Hour))
	assert.Equal(t, []string{"I5"}, linkedIssues(t, s, "P5"))
}
