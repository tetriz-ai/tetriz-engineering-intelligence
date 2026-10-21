// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package vcs turns raw pull-request records from a connector into the
// candidates the engine matches against.
//
// Convert reads the metadata keys listed on it and the description and open
// date from Payload.
//
// Record is the minimal pull-request record shape: a stable vendor id, an event
// date, string metadata and the vendor's JSON payload.
package vcs

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// Record is one fetched pull-request record.
type Record struct {
	// ExternalID is the record's identity at the vendor, and becomes the
	// candidate's key. It must be stable: a key composed from anything the
	// vendor can rename forks the entity into a second row when it moves.
	ExternalID string
	// DataDate is when the event happened — for a merged-pull-request record,
	// the merge. Zero means the pull request has not merged, which is a state
	// the engine handles rather than an error.
	DataDate time.Time
	// Metadata holds the indexed fields. Recognised keys are listed on Convert.
	Metadata map[string]string
	// Payload is the vendor's own JSON. Read only for the description and the
	// open date, under any of the spellings vendors use.
	Payload json.RawMessage
}

// Convert turns records into candidates, stamping each with how the session's
// owner took part.
//
// Recognised metadata keys:
//
//	repo, owner        the repository path; owner is joined when repo is bare
//	head_branch        the source branch
//	files_changed      changed paths, a JSON array encoded as the string value
//	files_line_stats   per-file additions and deletions, a JSON array the same way
//
// An unparseable field is dropped rather than failing the record: a candidate
// missing its file list can still match on a branch or a direct reference, and
// refusing the whole record would lose that.
func Convert(records []Record, kind attributionv1.SessionParticipationKind) []*attributionv1.PRCandidate {
	out := make([]*attributionv1.PRCandidate, 0, len(records))
	for _, r := range records {
		added, removed := sumLineStats(r.Metadata)
		out = append(out, &attributionv1.PRCandidate{
			PrKey:             r.ExternalID,
			PrRepository:      repoPath(r.Metadata),
			PrNumber:          ParseNumber(r.ExternalID),
			HeadBranch:        r.Metadata["head_branch"],
			FilesChanged:      stringSlice(r.Metadata, "files_changed"),
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

// repoPath returns the canonical "owner/repo". A connector configured with bare
// repository names stores the owner separately; without rejoining them the
// repository reads as a bare name and every derived URL is wrong.
func repoPath(m map[string]string) string {
	repo := m["repo"]
	if repo == "" || strings.Contains(repo, "/") {
		return repo
	}
	if owner := m["owner"]; owner != "" {
		return owner + "/" + repo
	}
	return repo
}

// ParseNumber extracts the pull-request number from a record key, in either the
// "mr-{project}!{iid}#event" form or the "repo#PR-N#event" one. The number must be
// a positive decimal integer and nothing else. Zero when the key carries no such
// number.
func ParseNumber(key string) int32 {
	if strings.HasPrefix(key, "mr-") {
		base := strings.SplitN(key, "#", 2)[0]
		idx := strings.Index(base, "!")
		if idx < 0 {
			return 0
		}
		return positive(base[idx+1:])
	}
	parts := strings.SplitN(key, "#", 3)
	if len(parts) < 2 || !strings.HasPrefix(parts[1], "PR-") {
		return 0
	}
	return positive(strings.TrimPrefix(parts[1], "PR-"))
}

// positive parses s as a positive 32-bit decimal integer made only of digits,
// returning zero for anything else.
func positive(s string) int32 {
	if s == "" {
		return 0
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n <= 0 {
		return 0
	}
	return int32(n)
}

// stringSlice reads a string array from a metadata value holding a JSON array.
// Any other value (a bare count some connectors write, a malformed array) yields
// nil rather than an error, per Convert.
func stringSlice(m map[string]string, key string) []string {
	raw, ok := m[key]
	if !ok || raw == "" {
		return nil
	}
	var vals []string
	if json.Unmarshal([]byte(raw), &vals) != nil {
		return nil
	}
	return vals
}

func sumLineStats(m map[string]string) (added, removed int64) {
	raw, ok := m["files_line_stats"]
	if !ok || raw == "" {
		return 0, 0
	}
	var stats []struct {
		Additions int64 `json:"additions"`
		Deletions int64 `json:"deletions"`
	}
	if json.Unmarshal([]byte(raw), &stats) != nil {
		return 0, 0
	}
	for _, s := range stats {
		added += s.Additions
		removed += s.Deletions
	}
	return added, removed
}

// payloadBody reads the description under either vendor spelling.
func payloadBody(payload json.RawMessage) string {
	p := decode(payload)
	if p == nil {
		return ""
	}
	for _, key := range []string{"body", "description"} {
		var val string
		if raw, ok := p[key]; ok && json.Unmarshal(raw, &val) == nil && val != "" {
			return val
		}
	}
	return ""
}

// payloadOpenedDate reads the open date under any of the three vendor spellings,
// normalised to the UTC calendar date so the opened-versus-window comparison
// happens at the same granularity as the merge date.
func payloadOpenedDate(payload json.RawMessage) time.Time {
	p := decode(payload)
	if p == nil {
		return time.Time{}
	}
	for _, key := range []string{"createdAt", "created_at", "created_on"} {
		var t time.Time
		if raw, ok := p[key]; ok && json.Unmarshal(raw, &t) == nil && !t.IsZero() {
			return t.UTC().Truncate(24 * time.Hour)
		}
	}
	return time.Time{}
}

func decode(payload json.RawMessage) map[string]json.RawMessage {
	if len(payload) == 0 {
		return nil
	}
	var p map[string]json.RawMessage
	if json.Unmarshal(payload, &p) != nil {
		return nil
	}
	return p
}

// ts leaves the zero time unset, so an unmerged pull request stays undated
// rather than being dated to the epoch and falling before every session.
func ts(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}
