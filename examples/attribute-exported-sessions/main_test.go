// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/adapter/localsession"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

type outcome struct {
	matches []string // "<SIDE> <pr_key>", sorted
	reason  attributionv1.SessionToPRUnmatchedReason
}

// attribute runs the composition over the fixtures and returns each session's
// outcome.
func attribute(t *testing.T, sessions []*attributionv1.SessionEvidence) map[string]outcome {
	t.Helper()
	prs, err := loadPullRequests("testdata/connector-records.jsonl")
	require.NoError(t, err)

	engine := attribution.New(attribution.DefaultParams(), prs, identities{})
	results, err := engine.Attribute(context.Background(), orgID, sessions)
	require.NoError(t, err)

	out := map[string]outcome{}
	for _, r := range results {
		o := outcome{reason: r.Attribution.GetUnmatched().GetReason()}
		for _, m := range r.Attribution.GetMatches() {
			o.matches = append(o.matches, trim(m.GetParticipationKind().String(), "SESSION_PARTICIPATION_KIND_")+" "+m.GetPrKey())
		}
		sort.Strings(o.matches)
		out[r.Session.GetSessionId()] = o
	}
	return out
}

func loadFixtureSessions(t *testing.T) []*attributionv1.SessionEvidence {
	t.Helper()
	sessions, err := loadExports("testdata")
	require.NoError(t, err)
	require.Len(t, sessions, 8)
	return sessions
}

// The composition runs from the two producers' own files, with no network and
// no credential, and reaches the outcomes the fixtures are built to show:
// matches on both sides, and abstentions with a stated reason.
func TestReferenceComposition(t *testing.T) {
	got := attribute(t, loadFixtureSessions(t))

	assert.Equal(t, []string{"AUTHORED demo-gh-startup/mobile#PR-110#merged"}, got["s-named-pr"].matches,
		"a session that named its pull request links to it")
	assert.Equal(t, []string{"AUTHORED demo-gh-startup/mobile#PR-120#merged"}, got["s-pushed-branch"].matches,
		"a session that pushed the head branch links to it, once, although the connector delivered it three times")
	assert.Contains(t, got["s-shared-edits"].matches, "AUTHORED demo-gh-startup/mobile#PR-81#merged",
		"a session whose edits land in the diff links to it")
	assert.Equal(t, []string{"REVIEWED demo-gh-startup/mobile#PR-117#merged"}, got["s-reviewed-pr"].matches,
		"a session that named a pull request its person reviewed links on the reviewer side")

	noMatch := attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR
	assert.Empty(t, got["s-unrelated"].matches)
	assert.Equal(t, noMatch, got["s-unrelated"].reason, "thin evidence abstains with a reason")
	assert.Empty(t, got["s-workspace-branch-only"].matches,
		"a branch the workspace merely held is not the session's own evidence")
	assert.Equal(t, noMatch, got["s-workspace-branch-only"].reason)
	assert.Empty(t, got["s-closed-pr"].matches, "a pull request closed without a merge is no candidate")
	assert.Equal(t, noMatch, got["s-closed-pr"].reason)
	assert.Empty(t, got["s-unlinked-person"].matches)
	assert.Equal(t, attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_VCS_NOT_LINKED,
		got["s-unlinked-person"].reason, "an unresolved identity abstains with a reason")
}

// The connector file holds every lifecycle record of each pull request, and
// some of them more than once. Only #merged becomes a candidate, once, dated by
// its merge, and with the fields the matcher reads.
func TestTheConnectorFileGivesOneCandidatePerMergedPullRequest(t *testing.T) {
	prs, err := loadPullRequests("testdata/connector-records.jsonl")
	require.NoError(t, err)
	assert.Equal(t, 50, prs.read)

	authored, err := prs.ListAuthoredPRs(context.Background(), orgID, []string{"3953824"}, time.Time{}, time.Time{})
	require.NoError(t, err)
	var keys []string
	for _, c := range authored {
		keys = append(keys, c.GetPrKey())
		assert.NotZero(t, c.GetPrNumber(), c.GetPrKey())
		assert.Equal(t, "demo-gh-startup/mobile", c.GetPrRepository(), c.GetPrKey())
		assert.NotEmpty(t, c.GetHeadBranch(), c.GetPrKey())
		assert.NotNil(t, c.GetMergedAt(), c.GetPrKey())
		assert.NotNil(t, c.GetOpenedAt(), c.GetPrKey())
	}
	sort.Strings(keys)
	assert.Equal(t, []string{
		"demo-gh-startup/mobile#PR-110#merged", "demo-gh-startup/mobile#PR-120#merged",
		"demo-gh-startup/mobile#PR-73#merged", "demo-gh-startup/mobile#PR-79#merged",
		"demo-gh-startup/mobile#PR-81#merged",
	}, keys, "no #opened, #closed or #snapshot record, no copy, and no one else's pull request")

	for _, c := range authored {
		if c.GetPrKey() == "demo-gh-startup/mobile#PR-120#merged" {
			assert.Equal(t, time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC), c.GetMergedAt().AsTime())
		}
	}
}

// Carol reviewed some of her own pull requests in the simulator's data. Those
// stay author-side: the reviewer side holds only someone else's pull request.
func TestAReviewOfOnesOwnPullRequestIsNotReviewerSide(t *testing.T) {
	prs, err := loadPullRequests("testdata/connector-records.jsonl")
	require.NoError(t, err)
	require.True(t, prs.reviewers["demo-gh-startup/mobile#PR-117"]["3953824"])

	reviewed, err := prs.ListReviewedPRs(context.Background(), orgID, []string{"3953824"}, time.Time{}, time.Time{})
	require.NoError(t, err)
	require.Len(t, reviewed, 1)
	assert.Equal(t, "demo-gh-startup/mobile#PR-117#merged", reviewed[0].GetPrKey())
}

// A records file that is not the sink's envelopes is refused, rather than read
// as records with every field empty.
func TestAFileThatIsNotEnvelopesIsRefused(t *testing.T) {
	flat := `{"external_id":"acme/widgets#PR-101#merged","data_date":"2026-03-01T00:00:00Z","metadata":{"repo":"acme/widgets"}}`
	path := filepath.Join(t.TempDir(), "records.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(flat+"\n"), 0o600))

	_, err := loadPullRequests(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "envelopes")
}

// A row holds a whole conversation, and real ones pass several MiB on one line.
func TestALongExportLineIsRead(t *testing.T) {
	data, err := os.ReadFile("testdata/sessions-carol.jsonl")
	require.NoError(t, err)
	var row map[string]any
	require.NoError(t, json.Unmarshal(bytes.SplitN(data, []byte("\n"), 2)[0], &row))
	row["conversation"] = append(row["conversation"].([]any), map[string]any{"role": "assistant", "content": strings.Repeat("x", 9<<20)})
	line, err := json.Marshal(row)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	require.NoError(t, os.WriteFile(path, append(line, '\n'), 0o600))

	got, err := loadSessions(path, "carol")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "s-named-pr", got[0].GetSessionId())
}

// A pending review was never submitted, so it makes no one a reviewer.
func TestAPendingReviewIsNotAReview(t *testing.T) {
	data, err := os.ReadFile("testdata/connector-records.jsonl")
	require.NoError(t, err)
	var kept [][]byte
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var env envelope
		require.NoError(t, json.Unmarshal(line, &env))
		e := env.Events[0]
		if e.OperationID == "fetch_reviews" && e.Metadata["pr_key"] == "demo-gh-startup/mobile#PR-117" {
			var p map[string]any
			require.NoError(t, json.Unmarshal(e.Payload, &p))
			p["state"] = "PENDING"
			env.Events[0].Payload, _ = json.Marshal(p)
			line, _ = json.Marshal(env)
		}
		kept = append(kept, line)
	}
	path := filepath.Join(t.TempDir(), "records.jsonl")
	require.NoError(t, os.WriteFile(path, append(bytes.Join(kept, []byte("\n")), '\n'), 0o600))

	prs, err := loadPullRequests(path)
	require.NoError(t, err)
	reviewed, err := prs.ListReviewedPRs(context.Background(), orgID, []string{"3953824"}, time.Time{}, time.Time{})
	require.NoError(t, err)
	assert.Empty(t, reviewed)
}

// The extractor's default export is refused rather than read as sessions with
// no id.
func TestTheDefaultExportIsRefused(t *testing.T) {
	conversation := `{"sessionId":"s-1","ideType":"IDE_TYPE_CURSOR","projectName":"mobile",` +
		`"prompts":[{"role":"ROLE_USER","timestamp":"2026-09-14T22:00:00Z"}],"conversationStartedAt":"2026-09-14T22:00:00Z"}`
	path := filepath.Join(t.TempDir(), "sessions.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(conversation+"\n"), 0o600))

	_, err := loadSessions(path, "carol")
	require.ErrorIs(t, err, localsession.ErrNoSessionID)
	assert.Contains(t, err.Error(), "--raw")
}

// upstreamRowColumns is every column `local-session-extractor export --raw`
// writes, verbatim, at agent-desk 70d295c.
var upstreamRowColumns = []string{
	"ai_attribution", "content_hash", "conversation", "conversation_source_created_at_ms", "created_at",
	"event_coverage", "extractor_version", "id", "ide_type", "installation_metadata", "last_mtime_ms",
	"last_offset", "last_reextract_at", "last_size", "project_path", "relation_type", "repo_name",
	"session_events", "session_facets", "session_id", "session_meta", "stats_cache", "tokens_observed",
	"updated_at",
}

// The session fixtures use only the extractor's own column names, so they are
// rows a real `export --raw` could have written.
func TestTheSessionFixturesAreExportRows(t *testing.T) {
	for _, e := range exports {
		data, err := os.ReadFile(filepath.Join("testdata", e.file))
		require.NoError(t, err)
		for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
			var row map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(line, &row))
			for col := range row {
				assert.Contains(t, upstreamRowColumns, col, "%s: a column the extractor does not write", e.file)
			}
		}
	}
}

// Only the enum name is gated (attribution.IDETypeIsGated). The adapter emits
// it for the extractor's "kiro", so the fixture's session is refused. Evidence
// that arrives with "kiro" itself is not gated, and the same workspace-only
// branch then matches.
func TestOnlyTheEnumNameReachesTheGateDeclined(t *testing.T) {
	sessions := loadFixtureSessions(t)
	assert.Empty(t, attribute(t, sessions)["s-workspace-branch-only"].matches)

	for _, s := range sessions {
		if s.GetIdeType() == "IDE_TYPE_KIRO" {
			s.IdeType = "kiro"
		}
	}
	assert.NotEmpty(t, attribute(t, sessions)["s-workspace-branch-only"].matches,
		"a kebab-case name in evidence is not gated")
}
