// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

import (
	"errors"
	"fmt"
	"time"
)

// PR is one pull request, as the engine reads it.
type PR struct {
	// Key identifies the pull request. It is the caller's own string, compared
	// exactly; use the key the other engines give the pull request so their
	// results join without a lookup.
	Key string `json:"key"`
	// Vendor is the code host: github, gitlab or bitbucket.
	Vendor Vendor `json:"vendor"`
	// Repository is the repository path, such as "acme/web", or a GitLab
	// project's full path. Bare "#12" references resolve inside it.
	Repository string `json:"repository"`
	// Number is the pull request's number in Repository (a GitLab iid).
	Number int `json:"number"`
	// Title, Body and HeadBranch are scanned for references.
	Title      string `json:"title"`
	Body       string `json:"body"`
	HeadBranch string `json:"head_branch"`
	// MergedAt is when the pull request merged, zero when it has not. The
	// closer election reads it.
	MergedAt time.Time `json:"merged_at"`
	// UpdatedAt is the vendor's last-change time. The retry window reads it.
	UpdatedAt time.Time `json:"updated_at"`
	// NativeIssueRefs are the issues the vendor records this pull request as
	// closing, keyed in the issue identifier shape, such as "acme/web#7".
	// nil means the record did not carry the vendor's list, and a store keeps
	// the list it holds; an empty, non-nil slice means the vendor lists none.
	NativeIssueRefs []NativeRef `json:"native_issue_refs"`
}

// Issue is one tracker issue, as the engine reads it.
type Issue struct {
	// Key identifies the issue. Use the vendor's stable id, which survives a
	// move to another project or team; Identifiers carries the moving keys.
	Key string `json:"key"`
	// Vendor is the tracker: jira, linear, github or gitlab.
	Vendor Vendor `json:"vendor"`
	// Identifiers are the strings a reference to this issue uses, such as
	// "ENG-42" or "acme/web#7". A reference resolves to the issue when it
	// equals one of them exactly.
	Identifiers []string `json:"identifiers"`
	// Title and Body are scanned for references.
	Title string `json:"title"`
	Body  string `json:"body"`
	// UpdatedAt is the vendor's last-change time. The retry window reads it.
	UpdatedAt time.Time `json:"updated_at"`
	// NativePRRefs are the pull requests the vendor links to this issue,
	// keyed "owner/repo#PR-N" or, for GitLab, "path#MR-N". nil means the
	// record did not carry the vendor's list, and a store keeps the list (and
	// NativeRefsTruncated) it holds; an empty, non-nil slice means none.
	NativePRRefs []NativeRef `json:"native_pr_refs"`
	// NativeRefsTruncated is set when the vendor returned only the first
	// page of NativePRRefs.
	NativeRefsTruncated bool `json:"native_refs_truncated"`
}

// NativeRef is one link the vendor records.
type NativeRef struct {
	// Key is the linked entity's identifier.
	Key string `json:"key"`
	// Source is the vendor that recorded the link. For a Linear attachment it
	// is linear, whatever host the linked pull request lives on.
	Source Vendor `json:"source"`
}

// ErrInvalidRecord wraps every rejection of a record batch.
var ErrInvalidRecord = errors.New("issuelinkage: invalid record")

// Validate rejects a batch the engine cannot read unambiguously: an empty
// key, or one key given twice on one side. It also rejects a scanned field
// longer than MaxTextBytes, so a record that would make the scan truncate is
// refused at the door, where the caller can still see which field it was,
// rather than silently losing references later.
func Validate(prs []PR, issues []Issue) error {
	seen := make(map[string]struct{}, len(prs))
	for i, p := range prs {
		if p.Key == "" {
			return fmt.Errorf("%w: pull request %d has no key", ErrInvalidRecord, i)
		}
		if _, dup := seen[p.Key]; dup {
			return fmt.Errorf("%w: pull request key %q appears twice", ErrInvalidRecord, p.Key)
		}
		seen[p.Key] = struct{}{}
		for _, f := range [...]textField{
			{"title", p.Title}, {"body", p.Body}, {"head branch", p.HeadBranch},
		} {
			if err := f.check("pull request", p.Key); err != nil {
				return err
			}
		}
	}
	seen = make(map[string]struct{}, len(issues))
	for i, is := range issues {
		if is.Key == "" {
			return fmt.Errorf("%w: issue %d has no key", ErrInvalidRecord, i)
		}
		if _, dup := seen[is.Key]; dup {
			return fmt.Errorf("%w: issue key %q appears twice", ErrInvalidRecord, is.Key)
		}
		seen[is.Key] = struct{}{}
		for _, f := range [...]textField{{"title", is.Title}, {"body", is.Body}} {
			if err := f.check("issue", is.Key); err != nil {
				return err
			}
		}
	}
	return nil
}

// textField is one scanned field of a record, named for the error it may
// raise.
type textField struct {
	name string
	text string
}

// check rejects the field when it is over the scan's limit.
func (f textField) check(side, key string) error {
	if len(f.text) <= MaxTextBytes {
		return nil
	}
	return fmt.Errorf("%w: %s %q has a %s of %d bytes, over the %d-byte limit",
		ErrInvalidRecord, side, key, f.name, len(f.text), MaxTextBytes)
}
