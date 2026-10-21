// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package localsession

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

func obj(s string) json.RawMessage { return json.RawMessage(s) }

func str(s string) *string { return &s }

func i64(v int64) *float64 { f := float64(v); return &f }

func convert(t *testing.T, row Row) *attributionv1.SessionEvidence {
	t.Helper()
	if row.SessionID == "" {
		row.SessionID = "s1"
	}
	got, err := Convert(row, "org-1", "p1")
	require.NoError(t, err)
	return got
}

// The editor table is an exact lookup, so no other spelling, case or padding
// translates.
func TestEditorNamesFollowTheUploadTable(t *testing.T) {
	tests := map[string]string{
		"claude-code":   "IDE_TYPE_CLAUDE_CODE",
		"cursor":        "IDE_TYPE_CURSOR",
		"copilot":       "IDE_TYPE_COPILOT",
		"antigravity":   "IDE_TYPE_ANTIGRAVITY",
		"codex":         "IDE_TYPE_CODEX",
		"kiro":          "IDE_TYPE_KIRO",
		"Kiro":          "IDE_TYPE_UNSPECIFIED",
		" cursor ":      "IDE_TYPE_UNSPECIFIED",
		"IDE_TYPE_KIRO": "IDE_TYPE_UNSPECIFIED",
		"Claude Code":   "IDE_TYPE_UNSPECIFIED",
		"windsurf":      "IDE_TYPE_UNSPECIFIED",
		"codeium":       "IDE_TYPE_UNSPECIFIED",
		"":              "IDE_TYPE_UNSPECIFIED",
	}
	for local, want := range tests {
		assert.Equal(t, want, IDEType(local), "%q", local)
		assert.Equal(t, want, convert(t, Row{IDEType: local}).GetIdeType(), "%q through Convert", local)
	}
}

// Kiro's own minutes win over the timestamps.
func TestKiroDurationIsItsOwnMinutes(t *testing.T) {
	row := Row{
		IDEType:      "kiro",
		SessionMeta:  obj(`{"duration_minutes": 90}`),
		Conversation: []Prompt{{Timestamp: str("2026-03-02T09:00:00Z")}},
		LastMtimeMS:  i64(time.Date(2026, 3, 2, 9, 10, 0, 0, time.UTC).UnixMilli()),
	}
	assert.Equal(t, int64(5400), convert(t, row).GetSessionDurationSeconds())

	// Zero minutes falls through to the span.
	row.SessionMeta = obj(`{"duration_minutes": 0}`)
	assert.Equal(t, int64(600), convert(t, row).GetSessionDurationSeconds())
}

// Every other editor's length is last update minus start, whatever
// duration_minutes says, and never negative.
func TestOtherEditorsDurationIsTheSpan(t *testing.T) {
	row := Row{
		IDEType:      "claude-code",
		SessionMeta:  obj(`{"duration_minutes": 90}`),
		Conversation: []Prompt{{Timestamp: str("2026-03-02T09:00:00.900Z")}},
		LastMtimeMS:  i64(time.Date(2026, 3, 2, 9, 30, 0, 500_000_000, time.UTC).UnixMilli()),
	}
	assert.Equal(t, int64(1799), convert(t, row).GetSessionDurationSeconds(), "whole seconds, truncated")

	row.LastMtimeMS = i64(time.Date(2026, 3, 2, 8, 0, 0, 0, time.UTC).UnixMilli())
	assert.Equal(t, int64(0), convert(t, row).GetSessionDurationSeconds(), "an end before the start is zero")

	row.LastMtimeMS = nil
	assert.Equal(t, int64(0), convert(t, row).GetSessionDurationSeconds(), "no end is zero")
}

// The start is the first prompt that has a timestamp, in order, then the
// editor's creation time, then created_at.
func TestStartTimeOrder(t *testing.T) {
	src := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	row := Row{
		Conversation: []Prompt{
			{Timestamp: nil},
			{Timestamp: str("2026-03-02T10:00:00.123456Z")},
			{Timestamp: str("2026-03-02T09:00:00Z")},
		},
		ConversationSourceCreatedAtMS: i64(src.UnixMilli()),
		CreatedAt:                     str("2026-02-01 08:00:00"),
	}
	got := convert(t, row)
	assert.Equal(t, time.Date(2026, 3, 2, 10, 0, 0, 123_000_000, time.UTC), got.GetStartedAt().AsTime(),
		"the first timestamped prompt, not the earliest, to the millisecond")
	assert.Equal(t, time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC), got.GetStartDate().AsTime())

	row.Conversation = nil
	assert.Equal(t, src, convert(t, row).GetStartedAt().AsTime())

	row.ConversationSourceCreatedAtMS = nil
	assert.Equal(t, time.Date(2026, 2, 1, 8, 0, 0, 0, time.UTC), convert(t, row).GetStartedAt().AsTime(),
		"a space between date and time reads as a T, and no zone as UTC")

	row.CreatedAt = nil
	got = convert(t, row)
	assert.Nil(t, got.GetStartedAt())
	assert.Nil(t, got.GetStartDate())
}

// The project name is the part of the path after its last "/".
func TestProjectNameIsTheLastPathSegment(t *testing.T) {
	for path, want := range map[string]string{
		"/Users/user_0a1b2c3d/src/widgets": "widgets",
		"widgets":                          "widgets",
		"/src/widgets/":                    "",
		"":                                 "",
	} {
		assert.Equal(t, want, convert(t, Row{ProjectPath: str(path)}).GetProjectName(), "%q", path)
	}
	assert.Empty(t, convert(t, Row{}).GetProjectName())
}

func TestRepoNamePassesThrough(t *testing.T) {
	assert.Equal(t, "acme/widgets", convert(t, Row{RepoName: str("acme/widgets")}).GetRepoName())
}

// Relevant files and tracked paths, prefix-stripped, deduplicated and sorted.
func TestFilesEditedAreTheStrippedUnion(t *testing.T) {
	row := Row{
		Conversation: []Prompt{
			{Metadata: obj(`{"relevantFiles": ["/Users/user_0a1b2c3d/src/widgets/limiter/rate.go", "docs/a.md"]}`)},
			{Metadata: obj(`{"relevant_files": ["/home/dev/work/widgets/limiter/rate.go"], "filesEdited": ["ignored.go"]}`)},
		},
		SessionMeta: obj(`{"tracked_file_paths": ["/root/work/widgets/limiter/bucket.go", "/Users/user_0a1b2c3d/short.go", "  "]}`),
	}
	assert.Equal(t, []string{"docs/a.md", "limiter/bucket.go", "limiter/rate.go"}, convert(t, row).GetFilesEdited(),
		"a home path too short to strip is dropped; a prompt's own filesEdited is not read")
}

// A non-empty files_edited replaces the union as given, unstripped, then sorted.
func TestSessionFilesEditedOverride(t *testing.T) {
	row := Row{
		Conversation: []Prompt{{Metadata: obj(`{"relevantFiles": ["limiter/rate.go"]}`)}},
		SessionMeta:  obj(`{"files_edited": ["z.go", "/Users/user_0a1b2c3d/src/widgets/a.go"]}`),
	}
	assert.Equal(t, []string{"/Users/user_0a1b2c3d/src/widgets/a.go", "z.go"}, convert(t, row).GetFilesEdited())
}

// The confidence band: pr_url or a positive AI-code share, the v2 share
// preferred when positive.
func TestConfidenceFollowsTheParser(t *testing.T) {
	tests := []struct {
		name, meta, ai, want string
	}{
		{name: "a pr_url is high", meta: `{"pr_url": "https://example.com/acme/widgets/pull/1"}`, want: "high"},
		{name: "pr_references alone is none", meta: `{"pr_references": [{"pr_number": 1, "pr_repository": "acme/widgets"}]}`, want: "none"},
		{name: "v1 above zero is high", ai: `{"ai_code_percent": 0.4}`, want: "high"},
		{name: "v2 above zero is high", ai: `{"ai_code_percent_v2": 0.2}`, want: "high"},
		{name: "v2 at zero falls back to v1", ai: `{"ai_code_percent_v2": 0, "ai_code_percent": 0.3}`, want: "high"},
		{name: "v2 below zero falls back to v1", ai: `{"ai_code_percent_v2": -1, "ai_code_percent": 0}`, want: "none"},
		{name: "nothing is none", want: "none"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			row := Row{}
			if tt.meta != "" {
				row.SessionMeta = obj(tt.meta)
			}
			if tt.ai != "" {
				row.AIAttribution = obj(tt.ai)
			}
			assert.Equal(t, tt.want, convert(t, row).GetAiAttributionConfidence())
		})
	}
}

// Branches, their provenance and PR references pass through, and the single
// branch fills an empty list.
func TestBranchesAndReferences(t *testing.T) {
	got := convert(t, Row{SessionMeta: obj(`{"git_branch": "feat/a"}`)})
	assert.Equal(t, "feat/a", got.GetGitBranch())
	assert.Equal(t, []string{"feat/a"}, got.GetGitBranches())

	got = convert(t, Row{SessionMeta: obj(`{
		"git_branch": "feat/a", "git_branches": ["feat/b"], "pr_number": 7, "pr_repository": "acme/widgets",
		"branch_signals": [
			{"name": "feat/b", "source": "SIGNAL_SOURCE_SESSION_COMMAND"},
			{"name": "feat/c", "source": "nonsense"}
		],
		"prReferences": [{"prNumber": 42, "prRepository": "acme/widgets", "source": "SIGNAL_SOURCE_SESSION_PROSE"}]
	}`)})
	assert.Equal(t, []string{"feat/b"}, got.GetGitBranches())
	assert.Equal(t, int32(7), got.GetPrNumber())
	require.Len(t, got.GetBranchSignals(), 2)
	assert.Equal(t, attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND, got.GetBranchSignals()[0].GetSource())
	// Decoding ignores unknown values, which
	// leaves an unknown enum name unset.
	assert.Equal(t, attributionv1.SignalSource_SIGNAL_SOURCE_UNSPECIFIED, got.GetBranchSignals()[1].GetSource())
	require.Len(t, got.GetPrReferences(), 1, "the JSON field name decodes as the proto name does")
	assert.Equal(t, attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_PROSE, got.GetPrReferences()[0].GetSource())
}

// A payload that does not decode fails the session, and the error names it.
func TestUndecodablePayloadsFail(t *testing.T) {
	tests := map[string]Row{
		"session_meta as a JSON string":      {SessionMeta: obj(`"{\"duration_minutes\": 5}"`)},
		"ai_attribution as a JSON string":    {AIAttribution: obj(`"{}"`)},
		"a wrongly typed field":              {SessionMeta: obj(`{"git_branches": "feat/a"}`)},
		"a prompt timestamp with no zone":    {Conversation: []Prompt{{Timestamp: str("2026-03-02T09:00:00")}}},
		"a prompt timestamp that is no time": {Conversation: []Prompt{{Timestamp: str("yesterday")}}},
		"an empty prompt timestamp":          {Conversation: []Prompt{{Timestamp: str("")}}},
	}
	for name, row := range tests {
		t.Run(name, func(t *testing.T) {
			row.SessionID = "s-bad"
			_, err := Convert(row, "org-1", "p1")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "s-bad")
		})
	}
}

// A line of the extractor's export decodes into Row as written, and a column
// Row does not read is ignored.
func TestAnExportLineDecodes(t *testing.T) {
	line := `{"ide_type":"claude-code","session_id":"-Users-user_0a1b2c3d-src-widgets::0b7c","conversation":[` +
		`{"timestamp":"2026-03-02T09:00:00.000Z","metadata":{"relevantFiles":["limiter/rate.go"]}}],` +
		`"project_path":"/Users/user_0a1b2c3d/src/widgets","repo_name":null,"last_mtime_ms":1772445600000,` +
		`"conversation_source_created_at_ms":null,"session_meta":{"git_branches":["feat/a"]},"ai_attribution":null,` +
		`"some_other_column":"ignored"}`
	var row Row
	require.NoError(t, json.Unmarshal([]byte(line), &row))
	got := convert(t, row)
	assert.Equal(t, "IDE_TYPE_CLAUDE_CODE", got.GetIdeType())
	assert.Equal(t, "-Users-user_0a1b2c3d-src-widgets::0b7c", got.GetSessionId())
	assert.Equal(t, "widgets", got.GetProjectName())
	assert.Equal(t, []string{"limiter/rate.go"}, got.GetFilesEdited())
	assert.Equal(t, int64(3600), got.GetSessionDurationSeconds())
	assert.Equal(t, "none", got.GetAiAttributionConfidence())
}

// A row with no session id is refused, whatever else it holds.
func TestARowWithNoSessionIDIsRefused(t *testing.T) {
	defaultExportLine := `{"sessionId":"-Users-user_0a1b2c3d-src-widgets::0b7c","ideType":"IDE_TYPE_CLAUDE_CODE",` +
		`"projectName":"widgets","prompts":[{"role":"ROLE_USER","timestamp":"2026-03-02T09:00:00Z"}],` +
		`"conversationStartedAt":"2026-03-02T09:00:00Z","sessionMeta":{"gitBranches":["feat/a"]}}`
	var fromDefault Row
	require.NoError(t, json.Unmarshal([]byte(defaultExportLine), &fromDefault))

	for name, row := range map[string]Row{
		"a default export line": fromDefault,
		"an empty row":          {},
		"a blank id":            {SessionID: "  ", IDEType: "cursor"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Convert(row, "org-1", "p1")
			require.Error(t, err)
			assert.Nil(t, got)
			assert.Contains(t, err.Error(), "--raw")
		})
	}
}

// A negative Kiro length is not used, and a span that ends before it starts is
// zero.
func TestKiroLengthEdges(t *testing.T) {
	start := time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)
	row := Row{
		IDEType:      "kiro",
		SessionMeta:  obj(`{"duration_minutes": -3}`),
		Conversation: []Prompt{{Timestamp: str("2026-03-02T09:00:00Z")}},
		LastMtimeMS:  i64(start.Add(time.Minute).UnixMilli()),
	}
	assert.Equal(t, int64(60), convert(t, row).GetSessionDurationSeconds(), "negative minutes fall through to the span")

	row.SessionMeta = obj(`{"duration_minutes": 0}`)
	row.LastMtimeMS = i64(start.Add(-time.Hour).UnixMilli())
	assert.Equal(t, int64(0), convert(t, row).GetSessionDurationSeconds())
}

// Windows paths. The project name splits on "/" only, so a backslash path is
// kept whole. Both home-directory forms are stripped.
func TestWindowsPaths(t *testing.T) {
	assert.Equal(t, `C:\src\widgets`, convert(t, Row{ProjectPath: str(`C:\src\widgets`)}).GetProjectName())
	assert.Equal(t, "widgets", convert(t, Row{ProjectPath: str("C:/src/widgets")}).GetProjectName())

	row := Row{SessionMeta: obj(`{"tracked_file_paths": [
		"C:\\Users\\bob\\ws\\proj\\c.go",
		"C:/Users/bob/ws/proj/d.go",
		"C:\\Users\\bob\\e.go"]}`)}
	assert.Equal(t, []string{"c.go", "d.go"}, convert(t, row).GetFilesEdited(),
		"a home path too short to strip is dropped")
}

// Shapes the conversion handles as they come.
func TestInputShapes(t *testing.T) {
	got := convert(t, Row{CreatedAt: str("2026-02-01T08:00:00+05:30")})
	assert.Equal(t, time.Date(2026, 2, 1, 2, 30, 0, 0, time.UTC), got.GetStartedAt().AsTime(), "created_at with a zone")

	got = convert(t, Row{SessionMeta: obj(`{"tracked_file_paths": ["./a.go", "a.go", "/opt/x/b.go"]}`)})
	assert.Equal(t, []string{"./a.go", "/opt/x/b.go", "a.go"}, got.GetFilesEdited(),
		"a leading ./ and a path outside a home directory are kept as written")

	got = convert(t, Row{SessionMeta: obj(`{"files_edited": ["z.go", "z.go"]}`)})
	assert.Equal(t, []string{"z.go", "z.go"}, got.GetFilesEdited(), "files_edited is not deduplicated")

	got = convert(t, Row{SessionMeta: obj(`{"git_branches": ["feat/a"], "some_future_field": {"x": 1}}`)})
	assert.Equal(t, []string{"feat/a"}, got.GetGitBranches(), "an unknown session_meta key is ignored")

	_, err := Convert(Row{SessionID: "s-list", SessionMeta: obj(`[]`)}, "org-1", "p1")
	require.Error(t, err, "session_meta as a list is refused")
}

type gateCandidates struct{}

func (gateCandidates) ListAuthoredPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return []*attributionv1.PRCandidate{{
		PrKey: "acme/w#7", PrRepository: "acme/w", PrNumber: 7, HeadBranch: "feat/x",
		MergedAt: timestamppb.New(time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC)),
	}}, nil
}

func (gateCandidates) ListReviewedPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, nil
}

type gateIdentity struct{}

func (gateIdentity) Resolve(context.Context, string, string) (attribution.Identity, error) {
	return attribution.Identity{AuthorIDs: []string{"a"}}, nil
}

// End to end: only the editor table's spelling reaches the engine's gate. Any
// other spelling arrives unspecified and is not gated.
func TestOnlyTheUploadSpellingReachesTheGate(t *testing.T) {
	meta := obj(`{"duration_minutes": 10,
		"branch_signals": [{"name": "feat/x", "source": "SIGNAL_SOURCE_LOCAL_GIT_WINDOWED"}]}`)
	for ide, gated := range map[string]bool{"kiro": true, "Kiro": false, "IDE_TYPE_KIRO": false} {
		s, err := Convert(Row{
			SessionID: "s", IDEType: ide, SessionMeta: meta,
			Conversation: []Prompt{{Timestamp: str("2026-03-02T09:00:00Z")}},
			LastMtimeMS:  i64(time.Date(2026, 3, 2, 9, 10, 0, 0, time.UTC).UnixMilli()),
		}, "", "p")
		require.NoError(t, err)
		results, err := attribution.New(attribution.Params{}, gateCandidates{}, gateIdentity{}).
			Attribute(context.Background(), "", []*attributionv1.SessionEvidence{s})
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.Equal(t, gated, len(results[0].Attribution.GetMatches()) == 0,
			"%q: a workspace-only branch attributes only when the session is not gated", ide)
	}
}

// A millisecond timestamp may carry a fraction. It is read as a JSON number and
// truncated toward zero to the millisecond.
func TestFractionalMillisecondsAreTruncated(t *testing.T) {
	line := `{"ide_type":"copilot","session_id":"s-frac",` +
		`"conversation_source_created_at_ms":1772442000000.75,"last_mtime_ms":1772445600999.9}`
	var row Row
	require.NoError(t, json.Unmarshal([]byte(line), &row))
	got := convert(t, row)
	assert.Equal(t, time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC), got.GetStartedAt().AsTime(), "the fraction is dropped")
	assert.Equal(t, int64(3600), got.GetSessionDurationSeconds(), "10:00:00.999 minus 09:00:00.000, whole seconds")
}

// A home path loses exactly three segments (user, workspace, project), and one
// with fewer is dropped.
func TestHomePathsLoseThreeSegments(t *testing.T) {
	row := Row{SessionMeta: obj(`{"tracked_file_paths": [
		"/Users/ada/widgets/main.go",
		"/Users/ada/widgets/pkg/x.go",
		"/Users/ada/src/team/widgets/pkg/y.go"]}`)}
	assert.Equal(t, []string{"widgets/pkg/y.go", "x.go"}, convert(t, row).GetFilesEdited())
	assert.Equal(t, "", stripPathPrefix("/Users/ada/widgets/main.go"))
	assert.Equal(t, "x.go", stripPathPrefix("/Users/ada/widgets/pkg/x.go"))
	assert.Equal(t, "widgets/pkg/x.go", stripPathPrefix("/Users/ada/src/team/widgets/pkg/x.go"))
}
