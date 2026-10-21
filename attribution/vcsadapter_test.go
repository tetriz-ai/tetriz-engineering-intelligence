// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// The helpers in this file, buildPRCandidates among them, are a test-local twin
// of vcs.Convert over the same record shape, so the signal tests build
// candidates without importing the adapter.

func testMetadata(kvs map[string]any) json.RawMessage {
	b, _ := json.Marshal(kvs)
	return b
}

// metaString reads a string field, treating a non-string as absent.
func metaString(m map[string]json.RawMessage, key string) string {
	if m == nil {
		return ""
	}
	raw, ok := m[key]
	if !ok {
		return ""
	}
	var val string
	if err := json.Unmarshal(raw, &val); err != nil {
		return ""
	}
	return val
}

// metaStringSlice reads a string array, accepting both a native JSON array and
// the string-encoded form vendors actually send.
func metaStringSlice(m map[string]json.RawMessage, key string) []string {
	if m == nil {
		return nil
	}
	raw, ok := m[key]
	if !ok {
		return nil
	}
	var vals []string
	if err := json.Unmarshal(raw, &vals); err != nil {
		var str string
		if json.Unmarshal(raw, &str) != nil {
			return nil
		}
		if json.Unmarshal([]byte(str), &vals) != nil {
			return nil
		}
	}
	return vals
}

// sumLineStats totals a record's per-file additions and deletions, accepting the
// same two encodings as metaStringSlice.
func sumLineStats(m map[string]json.RawMessage) (int64, int64) {
	if m == nil {
		return 0, 0
	}
	raw, ok := m["files_line_stats"]
	if !ok {
		return 0, 0
	}
	type fileStat struct {
		Additions int64 `json:"additions"`
		Deletions int64 `json:"deletions"`
	}
	var stats []fileStat
	if err := json.Unmarshal(raw, &stats); err != nil {
		var str string
		if json.Unmarshal(raw, &str) == nil {
			_ = json.Unmarshal([]byte(str), &stats)
		}
	}
	var added, removed int64
	for _, s := range stats {
		added += s.Additions
		removed += s.Deletions
	}
	return added, removed
}

// fullRepoPath returns the canonical "owner/repo" path. A connector configured
// with bare repo names stores the owner separately, and without rejoining them
// the repository reads as "gadgets" and every derived URL 404s.
func fullRepoPath(m map[string]json.RawMessage) string {
	repo := metaString(m, "repo")
	if repo == "" || strings.Contains(repo, "/") {
		return repo
	}
	owner := metaString(m, "owner")
	if owner == "" {
		return repo
	}
	return owner + "/" + repo
}

// parsePRNumber extracts the number from a record key, in either the GitLab
// "mr-{project}!{iid}#event" form or the GitHub/Bitbucket "repo#PR-N#event" one.
func parsePRNumber(key string) int32 {
	if strings.HasPrefix(key, "mr-") {
		base := strings.SplitN(key, "#", 2)[0]
		if idx := strings.Index(base, "!"); idx >= 0 {
			var iid int32
			if _, err := fmt.Sscanf(base[idx+1:], "%d", &iid); err != nil {
				return 0
			}
			return iid
		}
		return 0
	}
	parts := strings.SplitN(key, "#", 3)
	if len(parts) < 2 {
		return 0
	}
	var num int32
	if _, err := fmt.Sscanf(parts[1], "PR-%d", &num); err != nil {
		return 0
	}
	return num
}

// payloadBody reads the description under either vendor spelling.
func payloadBody(payload json.RawMessage) string {
	if len(payload) == 0 {
		return ""
	}
	var p map[string]json.RawMessage
	if json.Unmarshal(payload, &p) != nil {
		return ""
	}
	for _, key := range []string{"body", "description"} {
		raw, ok := p[key]
		if !ok {
			continue
		}
		var val string
		if json.Unmarshal(raw, &val) != nil {
			continue
		}
		if val != "" {
			return val
		}
	}
	return ""
}

// payloadOpenedDate reads the opened date under any of the three vendor
// spellings, normalised to the UTC calendar date so the opened-vs-window compare
// happens at the same granularity as the merge date.
func payloadOpenedDate(payload json.RawMessage) time.Time {
	if len(payload) == 0 {
		return time.Time{}
	}
	var p map[string]json.RawMessage
	if json.Unmarshal(payload, &p) != nil {
		return time.Time{}
	}
	for _, key := range []string{"createdAt", "created_at", "created_on"} {
		raw, ok := p[key]
		if !ok {
			continue
		}
		var t time.Time
		if json.Unmarshal(raw, &t) != nil {
			continue
		}
		if !t.IsZero() {
			return t.UTC().Truncate(24 * time.Hour)
		}
	}
	return time.Time{}
}

// ts wraps a time as a Timestamp, leaving the zero time unset so the engine's
// "undated candidate" path is reachable from a test.
func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// testPRCandidate builds a candidate the way a raw merged-PR record would,
// defaulting to AUTHORED.
func testPRCandidate(recordKey string, mergedDate time.Time, meta map[string]any) *attributionv1.PRCandidate {
	return testPRCandidateFull(recordKey, mergedDate, meta, nil,
		attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED)
}

// testPRCandidateWithKind stamps the participation kind explicitly.
func testPRCandidateWithKind(recordKey string, mergedDate time.Time, meta map[string]any,
	kind attributionv1.SessionParticipationKind,
) *attributionv1.PRCandidate {
	return testPRCandidateFull(recordKey, mergedDate, meta, nil, kind)
}

func testPRCandidateWithPayload(recordKey string, mergedDate time.Time, meta map[string]any,
	payload json.RawMessage,
) *attributionv1.PRCandidate {
	return testPRCandidateFull(recordKey, mergedDate, meta, payload,
		attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED)
}

func testPRCandidateFull(recordKey string, mergedDate time.Time, meta map[string]any,
	payload json.RawMessage, kind attributionv1.SessionParticipationKind,
) *attributionv1.PRCandidate {
	m := map[string]json.RawMessage{}
	_ = json.Unmarshal(testMetadata(meta), &m)
	added, removed := sumLineStats(m)
	return &attributionv1.PRCandidate{
		PrKey:             recordKey,
		PrRepository:      fullRepoPath(m),
		PrNumber:          parsePRNumber(recordKey),
		HeadBranch:        metaString(m, "head_branch"),
		FilesChanged:      metaStringSlice(m, "files_changed"),
		LinesAdded:        added,
		LinesRemoved:      removed,
		Body:              payloadBody(payload),
		MergedAt:          ts(mergedDate),
		OpenedAt:          ts(payloadOpenedDate(payload)),
		ParticipationKind: kind,
	}
}

// testParams are the parameters the signal tests run under.
func testParams() Params {
	return Params{
		TimeWindowAfterDays:        5,
		MinConfidencePercent:       30,
		RetryCooldownHours:         12,
		MaxRetryDays:               5,
		IdentityResolveConcurrency: 10,
		ComputeMatchConcurrency:    10,
	}
}

// tsPtr wraps an optional instant, leaving nil unset.
func tsPtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil || t.IsZero() {
		return nil
	}
	return timestamppb.New(*t)
}

// usageMeta aliases SessionEvidence so the test tables read as session metadata.
type usageMeta = attributionv1.SessionEvidence

// testParamsWithSpan carries an explicit MaxSessionSpanDays. Pass 0 to exercise
// the unset-config fallback.
func testParamsWithSpan(spanDays int) Params {
	p := testParams()
	p.TimeWindowAfterDays = 3
	p.StrongSignalWindowAfterDays = 14
	p.MaxRetryDays = 14
	p.MaxSessionSpanDays = spanDays
	return p
}

// testParamsWithStrongWindow varies only the strong-signal window.
func testParamsWithStrongWindow(strongDays int) Params {
	p := testParams()
	p.TimeWindowAfterDays = 3
	p.StrongSignalWindowAfterDays = strongDays
	p.MaxRetryDays = 14
	p.MaxSessionSpanDays = 30
	return p
}

// rawRecord is one vendor pull-request record as a connector lands it.
type rawRecord struct {
	RecordKey string
	DataDate  time.Time
	Metadata  json.RawMessage
	Payload   json.RawMessage
}

// buildPRCandidates converts raw records into candidates, stamping kind on each.
// This is the batch form of the adapter a caller must supply.
func buildPRCandidates(recs []rawRecord, kind attributionv1.SessionParticipationKind) []*attributionv1.PRCandidate {
	out := make([]*attributionv1.PRCandidate, 0, len(recs))
	for _, r := range recs {
		m := map[string]json.RawMessage{}
		_ = json.Unmarshal(r.Metadata, &m)
		added, removed := sumLineStats(m)
		out = append(out, &attributionv1.PRCandidate{
			PrKey:             r.RecordKey,
			PrRepository:      fullRepoPath(m),
			PrNumber:          parsePRNumber(r.RecordKey),
			HeadBranch:        metaString(m, "head_branch"),
			FilesChanged:      metaStringSlice(m, "files_changed"),
			LinesAdded:        added,
			LinesRemoved:      removed,
			Body:              payloadBody(r.Payload),
			MergedAt:          ts(r.DataDate),
			OpenedAt:          ts(payloadOpenedDate(r.Payload)),
			ParticipationKind: kind,
		})
	}
	return out
}
