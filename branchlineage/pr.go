// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// PR is one pull request, as the deriver reads it.
type PR struct {
	// Key identifies the pull request. It is the caller's own string, compared
	// exactly; use the same key the attribution engine gives the pull request
	// (its pr_key) so the two join without a lookup.
	Key string
	// Repository scopes the pull request. Edges never cross repositories, and a
	// pull request with no repository is never a carrier.
	Repository string
	// Number is the vendor's pull-request number within Repository. With
	// MergedAt it orders the repository's merges: (MergedAt, Number), then Key.
	Number int
	// HeadBranch is the branch merged from; BaseBranch the branch merged into.
	HeadBranch string
	BaseBranch string
	// OpenedAt is when the pull request was opened. The deriver does not read it.
	OpenedAt time.Time
	// MergedAt is when the pull request merged. Zero means not merged: an
	// unmerged pull request neither carries nor is carried.
	MergedAt time.Time
	// FirstCommitAt is the pull request's earliest commit, zero when unknown. It
	// opens the window of a pair's first promotion (see Evidence).
	FirstCommitAt time.Time
	// HeadCommitAt is the committer date of the head commit the pull request
	// merged, and MergeCommitAt the committer date of the commit its merge put on
	// BaseBranch (the merge or squash commit, or the head commit when it fast-
	// forwarded). Zero means unknown.
	//
	// Together they say what a promotion merged: a merge into its head branch
	// committed after the promotion's HeadCommitAt was not in the head it merged,
	// so it is not carried, and a later promotion of the pair whose head held it
	// carries it instead. The rule applies only when both times are known,
	// because it compares the head commit with the merge commit: a merge commit is
	// often dated seconds before the merge is recorded, so a head commit compared
	// with MergedAt would drop the very merge the head points at. A merge commit
	// dated after its merge counts at the merge. Use committer dates, not author dates: a rebase moves
	// the committer date past every merge it replays and keeps the author date.
	HeadCommitAt  time.Time
	MergeCommitAt time.Time
	// LinesAdded and LinesDeleted are the pull request's changed lines. Both
	// zero means no diff data, recorded as an unknown line count.
	LinesAdded   int64
	LinesDeleted int64
	// CrossRepository marks a pull request from a fork. A fork is never a
	// promotion, though it can be carried.
	CrossRepository bool
}

// Merged reports whether the pull request has merged.
func (p PR) Merged() bool { return !p.MergedAt.IsZero() }

// lines is the changed-line count recorded on an edge: nil when the pull
// request carries no diff data, never a zero standing in for unknown.
func (p PR) lines() *int64 {
	total := p.LinesAdded + p.LinesDeleted
	if total <= 0 {
		return nil
	}
	return &total
}

// pos is a pull request's position in its repository's merge order.
type pos struct {
	at     time.Time
	number int
	key    string
}

func (p PR) pos() pos { return pos{at: p.MergedAt, number: p.Number, key: p.Key} }

// inHead reports whether the merge l was in the head the promotion p merged: l
// merged before p and, when l's merge commit and p's head commit are both
// dated, was committed no later than that head. A merge cannot land after it is
// recorded, so a merge commit dated after its merge counts at the merge.
func inHead(l, p PR) bool {
	if !l.pos().before(p.pos()) {
		return false
	}
	if p.HeadCommitAt.IsZero() || l.MergeCommitAt.IsZero() {
		return true
	}
	landed := l.MergeCommitAt
	if l.MergedAt.Before(landed) {
		landed = l.MergedAt
	}
	return !landed.After(p.HeadCommitAt)
}

// before reports whether a sorts strictly before b in the merge order
// (merged time, then number, then key so two rows sharing both still order).
func (a pos) before(b pos) bool {
	if !a.at.Equal(b.at) {
		return a.at.Before(b.at)
	}
	if a.number != b.number {
		return a.number < b.number
	}
	return a.key < b.key
}

// ErrInvalidPR wraps every rejection of a pull-request set.
var ErrInvalidPR = errors.New("branchlineage: invalid pull request")

// validate rejects a set the engine cannot read unambiguously: an empty key, or
// one key given twice.
func validate(prs []PR) error {
	seen := make(map[string]struct{}, len(prs))
	for i, p := range prs {
		if p.Key == "" {
			return fmt.Errorf("%w: pull request %d has no key", ErrInvalidPR, i)
		}
		if _, dup := seen[p.Key]; dup {
			return fmt.Errorf("%w: key %q appears twice", ErrInvalidPR, p.Key)
		}
		seen[p.Key] = struct{}{}
	}
	return nil
}

// sortByPos sorts pull requests into merge order.
func sortByPos(prs []PR) {
	sort.Slice(prs, func(i, j int) bool { return prs[i].pos().before(prs[j].pos()) })
}
