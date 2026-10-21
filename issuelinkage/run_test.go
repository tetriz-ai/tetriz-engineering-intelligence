// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/memory"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/sqlite"
)

// recording wraps a store and records every Rearm. rearmErr fails every
// Rearm; rearmFailKey fails only that entity's.
type recording struct {
	issuelinkage.Store
	rearmed      []issuelinkage.Entity
	lookErr      error
	rearmErr     error
	rearmFailKey string
	trackerErr   error
	rowsErr      error
}

// eachStore runs f once on a memory store and once on a SQLite store.
func eachStore(t *testing.T, f func(t *testing.T, s *recording)) {
	t.Run("memory", func(t *testing.T) { f(t, &recording{Store: memory.New()}) })
	t.Run("sqlite", func(t *testing.T) {
		db, err := sqlite.Open(":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		f(t, &recording{Store: db})
	})
}

func (r *recording) Rows(ctx context.Context) ([]issuelinkage.Row, error) {
	if r.rowsErr != nil {
		return nil, r.rowsErr
	}
	return r.Store.Rows(ctx)
}

func (r *recording) Rearm(ctx context.Context, e issuelinkage.Entity) error {
	r.rearmed = append(r.rearmed, e)
	if r.rearmErr != nil {
		return r.rearmErr
	}
	if r.rearmFailKey != "" && e.Key == r.rearmFailKey {
		return errors.New("re-arm down")
	}
	return r.Store.Rearm(ctx, e)
}

func (r *recording) Trackers(ctx context.Context) ([]issuelinkage.Vendor, error) {
	if r.trackerErr != nil {
		return nil, r.trackerErr
	}
	return r.Store.Trackers(ctx)
}

func (r *recording) IssuesByIdentifier(ctx context.Context, ident string) ([]issuelinkage.Ref, error) {
	if r.lookErr != nil {
		return nil, r.lookErr
	}
	return r.Store.IssuesByIdentifier(ctx, ident)
}

func jiraIssue(key, ident string) issuelinkage.Issue {
	return issuelinkage.Issue{Key: key, Vendor: issuelinkage.VendorJira, Identifiers: []string{ident}}
}

// TestRun_RearmsOnlyNewCounterparts: a counterpart already linked on the
// prior run is not re-armed, so a linked pair cannot re-arm each other forever.
func TestRun_RearmsOnlyNewCounterparts(t *testing.T) {
	ctx := context.Background()
	s := &recording{Store: memory.New()}
	pr := issuelinkage.PR{Key: "pr-1", Vendor: issuelinkage.VendorGitHub, Body: "PROJ-1"}
	require.NoError(t, s.Put(ctx, []issuelinkage.PR{pr}, []issuelinkage.Issue{jiraIssue("issue-1", "PROJ-1"), jiraIssue("issue-2", "PROJ-2")}))
	_, err := issuelinkage.Run(ctx, s, time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC), issuelinkage.Config{})
	require.NoError(t, err)
	assert.Equal(t, []issuelinkage.Entity{{Side: issuelinkage.SideIssue, Key: "issue-1"}}, s.rearmed)

	s.rearmed = nil
	pr.Body = "PROJ-1 PROJ-2"
	require.NoError(t, s.Put(ctx, []issuelinkage.PR{pr}, nil))
	_, err = issuelinkage.Run(ctx, s, time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC), issuelinkage.Config{})
	require.NoError(t, err)
	assert.Equal(t, []issuelinkage.Entity{{Side: issuelinkage.SideIssue, Key: "issue-2"}}, s.rearmed, "only the newly linked issue")
}

// TestRun_ALookupErrorLeavesTheRowArmed: a failed row is reported and stays
// armed; nothing is written for it.
func TestRun_ALookupErrorLeavesTheRowArmed(t *testing.T) {
	eachStore(t, func(t *testing.T, s *recording) {
		ctx := context.Background()
		s.lookErr = errors.New("db blip")
		require.NoError(t, s.Put(ctx, []issuelinkage.PR{{Key: "pr-1", Vendor: issuelinkage.VendorGitHub, Body: "PROJ-1"}}, nil))
		rep, err := issuelinkage.Run(ctx, s, time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC), issuelinkage.Config{})
		require.NoError(t, err)
		require.Len(t, rep.Failures, 1)
		assert.Equal(t, "pr-1", rep.Failures[0].Entity.Key)
		r, _, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: "pr-1"})
		require.NoError(t, err)
		assert.Equal(t, issuelinkage.StatusYetToProcess, r.Status)
		assert.Empty(t, s.rearmed)
	})
}

// TestRun_GitLabTextReferencesLink: "#N" and "path#N" in a GitLab merge
// request's body resolve to GitLab issues.
func TestRun_GitLabTextReferencesLink(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	gl := func(key, ident string) issuelinkage.Issue {
		return issuelinkage.Issue{Key: key, Vendor: issuelinkage.VendorGitLab, Identifiers: []string{ident}}
	}
	require.NoError(t, s.Put(ctx,
		[]issuelinkage.PR{{Key: "mr-1!1", Vendor: issuelinkage.VendorGitLab, Repository: "grp/proj", Number: 1,
			Body: "Closes #7\n\nAlso closes grp/proj#8"}},
		[]issuelinkage.Issue{gl("issue-1!7", "grp/proj#7"), gl("issue-1!8", "grp/proj#8")}))
	_, err := issuelinkage.Run(ctx, s, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), issuelinkage.Config{})
	require.NoError(t, err)
	r, _, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: "mr-1!1"})
	require.NoError(t, err)
	assert.Equal(t, issuelinkage.StatusProcessed, r.Status)
	require.Len(t, r.Result.Links, 2)
	assert.Equal(t, "issue-1!7", r.Result.Links[0].Issue)
	assert.Equal(t, "issue-1!8", r.Result.Links[1].Issue)
}

// TestRun_ATruncatedNativeListIsFlaggedLimit: an issue whose native list was cut says
// so on its result.
func TestRun_ATruncatedNativeListIsFlaggedLimit(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	require.NoError(t, s.Put(ctx, nil, []issuelinkage.Issue{{Key: "u", Vendor: issuelinkage.VendorLinear,
		NativePRRefs: []issuelinkage.NativeRef{}, NativeRefsTruncated: true}}))
	_, err := issuelinkage.Run(ctx, s, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), issuelinkage.Config{})
	require.NoError(t, err)
	r, _, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: "u"})
	require.NoError(t, err)
	assert.True(t, r.Result.NativeRefsTruncated)
}

// TestRun_ATrackersErrorFailsTheRun: Run fails when it cannot list the
// trackers, and writes nothing.
func TestRun_ATrackersErrorFailsTheRun(t *testing.T) {
	eachStore(t, func(t *testing.T, s *recording) {
		ctx := context.Background()
		s.trackerErr = errors.New("db down")
		require.NoError(t, s.Put(ctx, []issuelinkage.PR{{Key: "pr-1", Vendor: issuelinkage.VendorGitHub}}, nil))
		_, err := issuelinkage.Run(ctx, s, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), issuelinkage.Config{})
		require.Error(t, err)
		r, _, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: "pr-1"})
		require.NoError(t, err)
		assert.Equal(t, issuelinkage.StatusYetToProcess, r.Status)
	})
}

// TestRun_AFailedReArmIsReported: a counterpart Run could not re-arm is in
// Report.Failures, and the row itself is still written.
func TestRun_AFailedReArmIsReported(t *testing.T) {
	eachStore(t, func(t *testing.T, s *recording) {
		ctx := context.Background()
		s.rearmErr = errors.New("db down")
		require.NoError(t, s.Put(ctx, []issuelinkage.PR{{Key: "pr-1", Vendor: issuelinkage.VendorGitHub, Body: "PROJ-1"}},
			[]issuelinkage.Issue{jiraIssue("issue-1", "PROJ-1")}))
		rep, err := issuelinkage.Run(ctx, s, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), issuelinkage.Config{})
		require.NoError(t, err)
		require.Len(t, rep.Failures, 1)
		assert.Equal(t, issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: "issue-1"}, rep.Failures[0].Entity)
		assert.Equal(t, 2, rep.Processed)
	})
}

// TestRun_AZeroPriorFirstSeenIsReplaced: a prior link with no FirstSeenAt
// takes the run's clock.
func TestRun_AZeroPriorFirstSeenIsReplaced(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	require.NoError(t, s.Put(ctx, []issuelinkage.PR{{Key: "pr-1", Vendor: issuelinkage.VendorGitHub, Body: "PROJ-1"}},
		[]issuelinkage.Issue{jiraIssue("issue-1", "PROJ-1")}))
	e := issuelinkage.Entity{Side: issuelinkage.SidePR, Key: "pr-1"}
	require.NoError(t, s.Write(ctx, issuelinkage.Result{Entity: e, Status: issuelinkage.StatusProcessed,
		Links: []issuelinkage.Link{{PR: "pr-1", Issue: "issue-1", From: issuelinkage.SidePR}}}, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, s.Force(ctx, e))
	now := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	_, err := issuelinkage.Run(ctx, s, now, issuelinkage.Config{})
	require.NoError(t, err)
	r, _, err := s.Row(ctx, e)
	require.NoError(t, err)
	assert.True(t, r.Result.Links[0].FirstSeenAt.Equal(now), r.Result.Links[0].FirstSeenAt)
}

func ghPR(n int, body string, merged time.Time) issuelinkage.PR {
	return issuelinkage.PR{Key: "acme/web#PR-" + strconv.Itoa(n), Vendor: issuelinkage.VendorGitHub, Repository: "acme/web",
		Number: n, Body: body, MergedAt: merged}
}

func ghIssue(closedBy ...int) issuelinkage.Issue {
	refs := []issuelinkage.NativeRef{}
	for _, n := range closedBy {
		refs = append(refs, issuelinkage.NativeRef{Key: "acme/web#PR-" + strconv.Itoa(n), Source: issuelinkage.VendorGitHub})
	}
	return issuelinkage.Issue{Key: "I9", Vendor: issuelinkage.VendorGitHub, Identifiers: []string{"acme/web#9"}, NativePRRefs: refs}
}

func settleRun(t *testing.T, s issuelinkage.Store, now time.Time) {
	t.Helper()
	for i := 0; i < 6; i++ {
		_, err := issuelinkage.Run(context.Background(), s, now, issuelinkage.Config{})
		require.NoError(t, err)
	}
}

// TestRun_AFailedElectionReArmIsRetried: when the re-arm after a moved
// election fails, nothing is written, the issue's row stays armed, and the
// next run finishes the job, so no pull request keeps a stale claim.
func TestRun_AFailedElectionReArmIsRetried(t *testing.T) {
	eachStore(t, func(t *testing.T, s *recording) {
		ctx := context.Background()
		t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		require.NoError(t, s.Put(ctx, []issuelinkage.PR{ghPR(1, "Fixes acme/web#9", t0), ghPR(2, "", t0.Add(time.Hour))},
			[]issuelinkage.Issue{ghIssue(1)}))
		settleRun(t, s, t0.Add(2*time.Hour))

		require.NoError(t, s.Put(ctx, nil, []issuelinkage.Issue{ghIssue(2)}))
		s.rowsErr = errors.New("rows down")
		rep, err := issuelinkage.Run(ctx, s, t0.Add(3*time.Hour), issuelinkage.Config{})
		require.NoError(t, err)
		require.Len(t, rep.Failures, 1)
		assert.Equal(t, issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: "I9"}, rep.Failures[0].Entity)
		r, _, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: "I9"})
		require.NoError(t, err)
		assert.True(t, r.Status.Armed(), "the issue's row stays armed")
		assert.Equal(t, "acme/web#PR-1", r.Result.Closer, "nothing new was written")

		s.rowsErr = nil
		settleRun(t, s, t0.Add(4*time.Hour))
		pr1, _, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: "acme/web#PR-1"})
		require.NoError(t, err)
		require.Len(t, pr1.Result.Links, 1)
		assert.False(t, pr1.Result.Links[0].Closes)
		assert.Equal(t, issuelinkage.RelationPartOf, pr1.Result.Links[0].Relation)
	})
}

// TestRun_APartlyFailedElectionReArmSettles: when re-arming one of the pull
// requests after a moved election fails, the run reports the issue, writes
// nothing for it and leaves it armed; the pull requests already re-armed run
// again harmlessly, and the next run finishes the job.
func TestRun_APartlyFailedElectionReArmSettles(t *testing.T) {
	eachStore(t, func(t *testing.T, s *recording) {
		ctx := context.Background()
		t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		require.NoError(t, s.Put(ctx, []issuelinkage.PR{ghPR(1, "Fixes acme/web#9", t0), ghPR(2, "", t0.Add(time.Hour)), ghPR(3, "Fixes acme/web#9", time.Time{})},
			[]issuelinkage.Issue{ghIssue(1)}))
		settleRun(t, s, t0.Add(2*time.Hour))

		require.NoError(t, s.Put(ctx, nil, []issuelinkage.Issue{ghIssue(2)}))
		s.rearmFailKey = "acme/web#PR-3"
		s.rearmed = nil
		rep, err := issuelinkage.Run(ctx, s, t0.Add(3*time.Hour), issuelinkage.Config{})
		require.NoError(t, err)
		require.Len(t, rep.Failures, 1)
		assert.Equal(t, issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: "I9"}, rep.Failures[0].Entity)
		assert.Contains(t, s.rearmed, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: "acme/web#PR-1"}, "re-armed before the failure")
		r, _, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: "I9"})
		require.NoError(t, err)
		assert.True(t, r.Status.Armed(), "the issue's row stays armed")
		assert.Equal(t, "acme/web#PR-1", r.Result.Closer, "nothing new was written")

		s.rearmFailKey = ""
		settleRun(t, s, t0.Add(4*time.Hour))
		r, _, err = s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: "I9"})
		require.NoError(t, err)
		assert.Equal(t, "acme/web#PR-2", r.Result.Closer)
		for _, k := range []string{"acme/web#PR-1", "acme/web#PR-3"} {
			pr, _, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: k})
			require.NoError(t, err)
			require.Len(t, pr.Result.Links, 1, k)
			assert.False(t, pr.Result.Links[0].Closes, k)
			assert.Equal(t, issuelinkage.RelationPartOf, pr.Result.Links[0].Relation, k)
		}
	})
}

// TestRun_AFailedElectionReArmKeepsARetriedRowArmed: an issue row the retry
// window picked up, not an armed one, is re-armed when the election's re-arm
// fails, so the next run retries it after the window has closed.
func TestRun_AFailedElectionReArmKeepsARetriedRowArmed(t *testing.T) {
	eachStore(t, func(t *testing.T, s *recording) {
		ctx := context.Background()
		t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		i9 := ghIssue(2)
		i9.UpdatedAt = t0
		require.NoError(t, s.Put(ctx, []issuelinkage.PR{ghPR(1, "Fixes acme/web#9", t0)}, []issuelinkage.Issue{i9}))
		settleRun(t, s, t0.Add(time.Hour))
		ie := issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: "I9"}
		r, _, err := s.Row(ctx, ie)
		require.NoError(t, err)
		require.Equal(t, issuelinkage.StatusPartiallyProcessed, r.Status, "waiting on PR-2")

		require.NoError(t, s.Put(ctx, []issuelinkage.PR{ghPR(2, "", t0.Add(time.Hour))}, nil))
		s.rowsErr = errors.New("rows down")
		rep, err := issuelinkage.Run(ctx, s, t0.Add(2*time.Hour), issuelinkage.Config{})
		require.NoError(t, err)
		require.Len(t, rep.Failures, 1)
		assert.Equal(t, ie, rep.Failures[0].Entity)
		r, _, err = s.Row(ctx, ie)
		require.NoError(t, err)
		assert.True(t, r.Status.Armed(), "the issue's row is armed, so the retry window no longer decides")

		s.rowsErr = nil
		settleRun(t, s, t0.Add(31*24*time.Hour))
		r, _, err = s.Row(ctx, ie)
		require.NoError(t, err)
		assert.Equal(t, "acme/web#PR-2", r.Result.Closer)
		pr1, _, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: "acme/web#PR-1"})
		require.NoError(t, err)
		require.Len(t, pr1.Result.Links, 1)
		assert.False(t, pr1.Result.Links[0].Closes)
	})
}

// TestRun_ElectionReArmsOnlyPullRequestRows: a moved election re-arms pull
// request rows only, even one whose key equals an issue's.
func TestRun_ElectionReArmsOnlyPullRequestRows(t *testing.T) {
	eachStore(t, func(t *testing.T, s *recording) {
		ctx := context.Background()
		t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		shared := issuelinkage.PR{Key: "I9", Vendor: issuelinkage.VendorGitHub, Repository: "acme/other", Number: 1}
		i10 := issuelinkage.Issue{Key: "I10", Vendor: issuelinkage.VendorGitHub, Identifiers: []string{"acme/web#10"}}
		require.NoError(t, s.Put(ctx, []issuelinkage.PR{ghPR(1, "Fixes acme/web#9", t0), ghPR(2, "", t0.Add(time.Hour)), ghPR(4, "Fixes acme/web#10", t0), shared},
			[]issuelinkage.Issue{ghIssue(1), i10}))
		settleRun(t, s, t0.Add(2*time.Hour))
		pr4, _, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: "acme/web#PR-4"})
		require.NoError(t, err)
		require.Len(t, pr4.Result.Links, 1, "PR-4 links I10")
		require.NoError(t, s.Put(ctx, nil, []issuelinkage.Issue{ghIssue(2)}))
		s.rearmed = nil
		_, err = issuelinkage.Run(ctx, s, t0.Add(3*time.Hour), issuelinkage.Config{})
		require.NoError(t, err)
		assert.Equal(t, []issuelinkage.Entity{{Side: issuelinkage.SidePR, Key: "acme/web#PR-1"}, {Side: issuelinkage.SidePR, Key: "acme/web#PR-2"}}, s.rearmed,
			"neither the issue-keyed pull request nor PR-4, which links another issue")
	})
}
