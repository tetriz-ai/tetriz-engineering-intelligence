// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package vcs turns a connector's merged pull-request records into the pull
// requests the branchlineage engine derives from.
//
// Record is the same minimal shape the attribution package's connector adapter
// reads, so one fetch feeds both engines, and Convert keys each pull request by
// ExternalID, the key attribution matches sessions to.
package vcs

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
)

// Record is one fetched pull-request record.
type Record struct {
	// ExternalID is the record's stable identity at the vendor; it becomes Key.
	ExternalID string
	// DataDate is when the event happened: for a merged-pull-request record,
	// the merge. Zero means not merged.
	DataDate time.Time
	// Metadata holds the indexed fields. Recognised keys are listed on Convert.
	Metadata map[string]string
	// Payload is the vendor's own JSON. Read only for the open date and the
	// fork flag, under the spellings vendors use.
	Payload json.RawMessage
}

// Convert turns records into pull requests.
//
// Recognised metadata keys:
//
//	repo, owner          the repository path; owner is joined when repo is bare
//	head_branch          the branch merged from
//	base_ref_name        the branch merged into (GitHub)
//	target_branch        the branch merged into (GitLab)
//	destination_branch   the branch merged into (Bitbucket)
//	first_commit_at      the earliest commit, RFC 3339
//	head_commit_at       the committer date of the head commit merged, RFC 3339
//	merge_commit_at      the committer date of the commit the merge put on the
//	                     base branch, RFC 3339
//	is_cross_repository  "true" for a fork (GitLab, Bitbucket)
//	files_line_stats     per-file line counts, a JSON array of
//	                     {"additions":N,"deletions":N}, such as
//	                     [{"additions":40,"deletions":10}]
//
// Number is read from the key: 42 from "acme/shop#PR-42#merged", 7 from
// "mr-1234!7#merged". Any other key gives 0.
//
// From the payload: the open date (createdAt, created_at or created_on) and
// GitHub's isCrossRepository. An unreadable field is left at its zero value
// rather than failing the record, so a missing first commit falls back to the
// lookback window and missing line stats read as no diff data.
func Convert(records []Record) []branchlineage.PR {
	out := make([]branchlineage.PR, 0, len(records))
	for _, r := range records {
		added, deleted := sumLineStats(r.Metadata)
		p := decode(r.Payload)
		out = append(out, branchlineage.PR{
			Key:             r.ExternalID,
			Repository:      repoPath(r.Metadata),
			Number:          int(ParseNumber(r.ExternalID)),
			HeadBranch:      r.Metadata["head_branch"],
			BaseBranch:      baseBranch(r.Metadata),
			OpenedAt:        payloadTime(p, "createdAt", "created_at", "created_on"),
			MergedAt:        r.DataDate,
			FirstCommitAt:   metaTime(r.Metadata, "first_commit_at"),
			HeadCommitAt:    metaTime(r.Metadata, "head_commit_at"),
			MergeCommitAt:   metaTime(r.Metadata, "merge_commit_at"),
			LinesAdded:      added,
			LinesDeleted:    deleted,
			CrossRepository: r.Metadata["is_cross_repository"] == "true" || payloadBool(p, "isCrossRepository"),
		})
	}
	return out
}

// baseBranch reads the base branch under whichever vendor key is present.
func baseBranch(m map[string]string) string {
	for _, k := range []string{"base_ref_name", "target_branch", "destination_branch"} {
		if v := m[k]; v != "" {
			return v
		}
	}
	return ""
}

// repoPath returns "owner/repo", rejoining a bare repo name with its owner.
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
// "mr-{project}!{iid}#event" form or the "repo#PR-N#event" one. Zero when the key
// carries no positive decimal number.
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

func sumLineStats(m map[string]string) (added, deleted int64) {
	raw := m["files_line_stats"]
	if raw == "" {
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
		deleted += s.Deletions
	}
	return added, deleted
}

func metaTime(m map[string]string, key string) time.Time {
	t, err := time.Parse(time.RFC3339, m[key])
	if err != nil {
		return time.Time{}
	}
	return t
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

func payloadTime(p map[string]json.RawMessage, keys ...string) time.Time {
	for _, k := range keys {
		var t time.Time
		if raw, ok := p[k]; ok && json.Unmarshal(raw, &t) == nil && !t.IsZero() {
			return t
		}
	}
	return time.Time{}
}

func payloadBool(p map[string]json.RawMessage, key string) bool {
	var b bool
	raw, ok := p[key]
	return ok && json.Unmarshal(raw, &b) == nil && b
}
