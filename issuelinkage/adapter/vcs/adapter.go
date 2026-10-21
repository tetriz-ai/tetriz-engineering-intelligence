// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package vcs turns fetched vendor records into the pull requests and issues
// the issuelinkage engine reads.
//
// Record is the minimal shape this module's adapters read, so one fetch feeds
// every package. A fetch gives one record per lifecycle event (opened,
// merged, snapshot, ...), and PRs and Issues fold the records of one entity
// into one value keyed by EntityKey.
package vcs

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
)

// Record is one fetched record.
type Record struct {
	// ExternalID is the record's identity at the vendor, such as
	// "acme/web#PR-42#merged" or "10001#created".
	ExternalID string
	// DataDate is when the event happened; zero for a current-state record.
	DataDate time.Time
	// Metadata holds the record's indexed fields.
	Metadata map[string]string
	// Payload is the vendor's own JSON.
	Payload json.RawMessage
}

// lifecycle suffixes a fetch appends to an entity's key.
var lifecycle = map[string]bool{
	"opened": true, "merged": true, "closed": true, "snapshot": true, "created": true, "resolved": true,
}

// EntityKey returns externalID without its lifecycle suffix: "acme/web#PR-42"
// for "acme/web#PR-42#merged", "10001" for "10001#created". An id with no
// such suffix is returned as is.
func EntityKey(externalID string) string {
	if i := strings.LastIndexByte(externalID, '#'); i > 0 && lifecycle[externalID[i+1:]] {
		return externalID[:i]
	}
	return externalID
}

// group folds records by entity key: the snapshot record, else the last one
// given, supplies the content. Any merged record gives the merge time, from
// its payload or else its date, the earliest kept, whatever the order.
type group struct {
	key      string
	content  Record
	snapshot bool
	mergedAt time.Time
}

func groups(records []Record) []*group {
	byKey := map[string]*group{}
	var order []string
	for _, r := range records {
		k := EntityKey(r.ExternalID)
		g, ok := byKey[k]
		if !ok {
			g = &group{key: k}
			byKey[k] = g
			order = append(order, k)
		}
		isSnapshot := strings.HasSuffix(r.ExternalID, "#snapshot")
		if isSnapshot || !g.snapshot {
			g.content, g.snapshot = r, isSnapshot || g.snapshot
		}
		if strings.HasSuffix(r.ExternalID, "#merged") {
			at := payloadTime(decode(r.Payload), "mergedAt", "merged_at")
			if at.IsZero() {
				at = r.DataDate
			}
			if !at.IsZero() && (g.mergedAt.IsZero() || at.Before(g.mergedAt)) {
				g.mergedAt = at
			}
		}
	}
	sort.Strings(order)
	out := make([]*group, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out
}

// PRs turns one code host's pull-request records into pull requests, sorted
// by key. vendor is github, gitlab or bitbucket.
//
// Read from the payload: number (number, iid or id), title, body
// (body, or description with Bitbucket's summary.raw as fallback), the merge
// time (mergedAt or merged_at, from the content record or any merged record,
// else the merged record's date), the update time
// (updatedAt, updated_at or updated_on), the head branch when the metadata
// lacks it (headRefName, source_branch, source.branch.name), and GitHub's
// closingIssuesReferences. Read from the metadata: repo (owner joined when
// bare; else, for an "owner/repo#PR-N" key, the key's prefix), head_branch,
// and GitLab's closes_issues. A field that does not parse
// is left at its zero value. A native list the record does not carry, or
// carries in a form that names no issue (an external tracker's), is nil, so a
// store keeps the list it holds; a list the vendor returned empty is empty.
func PRs(vendor issuelinkage.Vendor, records []Record) []issuelinkage.PR {
	var out []issuelinkage.PR
	for _, g := range groups(records) {
		p := decode(g.content.Payload)
		md := g.content.Metadata
		repo := repoPath(md)
		if i := strings.Index(g.key, "#PR-"); repo == "" && i > 0 {
			repo = g.key[:i]
		}
		pr := issuelinkage.PR{
			Key:        g.key,
			Vendor:     vendor,
			Repository: repo,
			Number:     payloadInt(p, "number", "iid", "id"),
			Title:      payloadString(p, "title"),
			MergedAt:   payloadTime(p, "mergedAt", "merged_at"),
			UpdatedAt:  payloadTime(p, "updatedAt", "updated_at", "updated_on"),
			HeadBranch: md["head_branch"],
		}
		if pr.MergedAt.IsZero() {
			pr.MergedAt = g.mergedAt
		}
		if pr.HeadBranch == "" {
			pr.HeadBranch = firstNonEmpty(payloadString(p, "headRefName"), payloadString(p, "source_branch"), nestedString(p, "source", "branch", "name"))
		}
		switch vendor {
		case issuelinkage.VendorGitHub:
			pr.Body = payloadString(p, "body")
			if refs, ok, err := closingIssueKeys(g.content.Payload, repo); err == nil && ok {
				pr.NativeIssueRefs = refs
			}
		case issuelinkage.VendorGitLab:
			pr.Body = payloadString(p, "description")
			if refs, ok, err := gitlabClosesIssues(json.RawMessage(md["closes_issues"]), repo); err == nil && ok {
				pr.NativeIssueRefs = refs
			}
		case issuelinkage.VendorBitbucket:
			pr.Body = firstNonEmpty(payloadString(p, "description"), nestedString(p, "summary", "raw"))
		}
		out = append(out, pr)
	}
	return out
}

// Issues turns one tracker's issue records into issues, sorted by key. vendor
// is github, gitlab, jira or linear.
//
// Each issue is keyed by its stable id and answers to its current key:
// GitHub "owner/repo#N" (repository.nameWithOwner or the repo metadata, and
// number), GitLab references.full, Jira key (or the issue_key metadata),
// Linear identifier. Title and body come from title and body or description,
// or Jira's fields.summary and fields.description (a string or an Atlassian
// Document Format tree). Native links: GitHub's closedByPullRequestsReferences
// and Linear's GitHub and GitLab attachments.
func Issues(vendor issuelinkage.Vendor, records []Record) []issuelinkage.Issue {
	var out []issuelinkage.Issue
	for _, g := range groups(records) {
		p := decode(g.content.Payload)
		md := g.content.Metadata
		is := issuelinkage.Issue{Key: g.key, Vendor: vendor}
		var ident string
		switch vendor {
		case issuelinkage.VendorGitHub:
			repo := firstNonEmpty(nestedString(p, "repository", "nameWithOwner"), repoPath(md))
			if n := payloadInt(p, "number"); repo != "" && n > 0 {
				ident = repo + "#" + strconv.Itoa(n)
			}
			is.Title, is.Body = payloadString(p, "title"), payloadString(p, "body")
			is.UpdatedAt = payloadTime(p, "updatedAt")
			if refs, ok, err := closingPRKeys(g.content.Payload, repo); err == nil && ok {
				is.NativePRRefs = refs
			}
		case issuelinkage.VendorGitLab:
			ident = nestedString(p, "references", "full")
			is.Title, is.Body = payloadString(p, "title"), payloadString(p, "description")
			is.UpdatedAt = payloadTime(p, "updated_at")
		case issuelinkage.VendorJira:
			ident = firstNonEmpty(payloadString(p, "key"), md["issue_key"])
			var f struct {
				Summary     string          `json:"summary"`
				Description json.RawMessage `json:"description"`
				Updated     string          `json:"updated"`
			}
			if raw, ok := p["fields"]; ok && json.Unmarshal(raw, &f) == nil {
				is.Title = f.Summary
				if body, err := jiraDescription(f.Description); err == nil {
					is.Body = body
				}
				is.UpdatedAt = jiraTime(f.Updated)
			}
		case issuelinkage.VendorLinear:
			ident = payloadString(p, "identifier")
			is.Title, is.Body = payloadString(p, "title"), payloadString(p, "description")
			is.UpdatedAt = payloadTime(p, "updatedAt")
			if refs, ok, truncated, err := linearPRKeys(g.content.Payload); err == nil && ok {
				is.NativePRRefs, is.NativeRefsTruncated = refs, truncated
			}
		}
		if ident != "" {
			is.Identifiers = []string{ident}
		}
		out = append(out, is)
	}
	return out
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

func decode(payload json.RawMessage) map[string]json.RawMessage {
	var p map[string]json.RawMessage
	if len(payload) == 0 || json.Unmarshal(payload, &p) != nil {
		return nil
	}
	return p
}

func payloadString(p map[string]json.RawMessage, key string) string {
	var s string
	if raw, ok := p[key]; ok && json.Unmarshal(raw, &s) == nil {
		return s
	}
	return ""
}

func nestedString(p map[string]json.RawMessage, path ...string) string {
	cur := p
	for i, k := range path {
		raw, ok := cur[k]
		if !ok {
			return ""
		}
		if i == len(path)-1 {
			var s string
			if json.Unmarshal(raw, &s) == nil {
				return s
			}
			return ""
		}
		var next map[string]json.RawMessage
		if json.Unmarshal(raw, &next) != nil {
			return ""
		}
		cur = next
	}
	return ""
}

func payloadInt(p map[string]json.RawMessage, keys ...string) int {
	for _, k := range keys {
		var n int
		if raw, ok := p[k]; ok && json.Unmarshal(raw, &n) == nil && n > 0 {
			return n
		}
	}
	return 0
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

// jiraTime parses Jira's timestamp, such as 2026-07-07T02:00:00.000+0000.
func jiraTime(s string) time.Time {
	for _, layout := range []string{"2006-01-02T15:04:05.000-0700", time.RFC3339Nano} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
