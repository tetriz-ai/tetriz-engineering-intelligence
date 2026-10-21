// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage

import (
	"strconv"
	"strings"

	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// CorroborationInput is everything [ScoreE] reads. It is deliberately plain data
// so the scorer stays pure and trivially testable; a caller fills it from the
// fix pull request, the resolved candidate pull request, and the stored
// candidate row.
type CorroborationInput struct {
	// CandidatePRResolved is false when the culprit commit has no resolved pull
	// request. The candidate-specific bucket then has nothing to match on and E
	// degrades to the defect-level bucket — coverage, not a silent zero.
	CandidatePRResolved bool

	// Fix side, always available.
	FixTitle  string
	FixBranch string
	FixBody   string

	// Candidate side, meaningful only when CandidatePRResolved.
	CandidateTitle     string
	CandidateBranch    string
	CandidatePRNumber  int    // 0 when unknown
	CandidateCommitSHA string // normalised prefix; empty when unknown
	SameAuthor         bool

	// Defect-level signals — properties of the fix, identical across its
	// candidates.
	LinkedDefectIssue     bool
	RegressionTestChanged bool
	IncidentOrRollback    bool
}

// CorroborationFlags is the per-signal breakdown [ScoreEWithFlags] evaluated to
// produce E — the exact booleans that fed the two buckets. Persisting it makes
// the E score explainable and re-derivable. Plain data mirroring the stored
// message, kept here so the scorer stays free of generated types.
type CorroborationFlags struct {
	// Candidate-specific — false whenever the candidate pull request is
	// unresolved, because the bucket is then skipped.
	ExplicitCandidateReference bool
	TitleOverlap               bool
	BranchOverlap              bool
	SameAuthor                 bool
	// Defect-level.
	LinkedDefectIssue     bool
	RegressionTestChanged bool
	IncidentOrRollback    bool
}

// ScoreEWithFlags is corroboration, split into a dominant candidate-specific
// bucket and a low-weight defect-level bucket. Candidate-specific signals point
// at this candidate — an explicit reference in the fix, title or branch token
// overlap, the same author; defect-level signals only confirm a real defect and
// are the same for every candidate, so they cannot rank. Text overlap is plain
// token-set intersection.
//
// It returns the per-signal booleans alongside the score, because every caller
// persists them: a score nobody can explain is a number nobody acts on. The
// candidate-specific flags stay false when the candidate pull request is
// unresolved — the bucket contributes nothing and there is nothing to explain.
func ScoreEWithFlags(in CorroborationInput, cfg Config) (float64, CorroborationFlags) {
	flags := CorroborationFlags{
		LinkedDefectIssue:     in.LinkedDefectIssue,
		RegressionTestChanged: in.RegressionTestChanged,
		IncidentOrRollback:    in.IncidentOrRollback,
	}
	defectBucket := fraction(flags.LinkedDefectIssue, flags.RegressionTestChanged, flags.IncidentOrRollback)

	var candidateBucket float64
	if in.CandidatePRResolved {
		flags.ExplicitCandidateReference = referencesCandidate(in.FixBody, in.CandidatePRNumber, in.CandidateCommitSHA)
		flags.TitleOverlap = hasTokenOverlap(in.FixTitle, in.CandidateTitle)
		flags.BranchOverlap = hasTokenOverlap(in.FixBranch, in.CandidateBranch)
		flags.SameAuthor = in.SameAuthor
		candidateBucket = fraction(flags.ExplicitCandidateReference, flags.TitleOverlap, flags.BranchOverlap, flags.SameAuthor)
	}

	return cfg.ECandidateWeight*candidateBucket + (1-cfg.ECandidateWeight)*defectBucket, flags
}

// fraction is the share of the given signals that are true, in [0,1].
func fraction(signals ...bool) float64 {
	if len(signals) == 0 {
		return 0
	}
	n := 0
	for _, s := range signals {
		if s {
			n++
		}
	}
	return float64(n) / float64(len(signals))
}

// referencesCandidate is true when the fix body names the candidate by its
// pull-request number, as `#34`, or by its commit hash prefix. Both are bounded
// so a longer token cannot false-match: `#34` does not match inside `#345`, and
// the hash prefix must sit on an alphanumeric word boundary — so `9c1f2a3b4c5d`
// matches "reverts 9c1f2a3b4c5d" but neither a longer hex run, which is a
// different hash, nor a longer word.
func referencesCandidate(fixBody string, prNumber int, commitSHA string) bool {
	if prNumber > 0 {
		n := strconv.Itoa(prNumber)
		// isAlnum, not a digits-only class: "#34abc" is a different reference
		// that a digits-only boundary accepts as pull request 34.
		if containsToken(fixBody, "#"+n, isAlnum) {
			return true
		}
		// The form GitHub autolinks, which carries no "#" at all.
		if containsToken(strings.ToLower(fixBody), "pull/"+n, isAlnum) ||
			containsToken(strings.ToLower(fixBody), "merge_requests/"+n, isAlnum) {
			return true
		}
	}
	if len(commitSHA) >= 7 && containsToken(strings.ToLower(fixBody), strings.ToLower(commitSHA), isAlnum) {
		return true
	}
	return false
}

// isAlnum is the commit-hash boundary class: any ASCII letter or digit continues
// the same token. Using it rather than a hex-only class means a hash prefix
// bounded by a non-hex letter is correctly rejected as part of a longer word,
// not only when the next character is hex.
func isAlnum(r rune) bool {
	return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// containsToken reports whether needle appears in haystack not extended on
// either side by a character the token class considers part of the same token,
// so `#34` is found in "fixes #34." but not in "#345". The boundary check indexes
// bytes, so the sameToken classifiers must stay ASCII-only — a non-ASCII class
// would misread a UTF-8 continuation byte.
func containsToken(haystack, needle string, sameToken func(rune) bool) bool {
	from := 0
	for {
		i := strings.Index(haystack[from:], needle)
		if i < 0 {
			return false
		}
		start := from + i
		end := start + len(needle)
		beforeOK := start == 0 || !sameToken(rune(haystack[start-1]))
		afterOK := end == len(haystack) || !sameToken(rune(haystack[end]))
		if beforeOK && afterOK {
			return true
		}
		from = start + 1
	}
}

// hasTokenOverlap is true when the two strings share at least one meaningful
// token. Tokens are lowercased alphanumeric runs of length three or more with
// common boilerplate removed, so overlap reflects a shared meaningful term
// rather than a shared branch prefix or verb. There is no stemming, so "order"
// and "orders" do not match.
func hasTokenOverlap(a, b string) bool {
	sa := tokenSet(a)
	if len(sa) == 0 {
		return false
	}
	for t := range tokenSet(b) {
		if sa[t] {
			return true
		}
	}
	return false
}

// commonTokens are branch-namespace prefixes and title verbs that appear in most
// branches and pull-request titles. Counting them as overlap would make title
// and branch matching fire on boilerplate shared by every pair, so they never
// count as a meaningful token.
var commonTokens = map[string]bool{
	"fix": true, "feat": true, "chore": true, "bug": true, "bugfix": true, "refactor": true,
	"test": true, "tests": true, "docs": true, "doc": true,
	"add": true, "adds": true, "update": true, "updates": true, "remove": true, "removes": true,
	"the": true, "and": true, "for": true, "with": true, "from": true, "into": true,
	// Tracker names. Every branch in an organisation that prefixes its keys
	// carries the same one, so "fix/JIRA-123-null-check" and
	// "feat/JIRA-456-new-page" overlap on "jira" alone and claim a relationship
	// neither branch has.
	"jira": true, "linear": true, "proj": true, "issue": true, "ticket": true,
	"task": true, "story": true, "epic": true, "release": true, "hotfix": true,
}

func tokenSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, tok := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < '0' || r > '9')
	}) {
		if len(tok) >= 3 && !commonTokens[tok] {
			out[tok] = true
		}
	}
	return out
}

// Proto returns the flags in the form a score's stored derivation carries.
func (f CorroborationFlags) Proto() *defectlineagev1.DefectCorroborationFlags {
	return &defectlineagev1.DefectCorroborationFlags{
		ExplicitCandidateReference: f.ExplicitCandidateReference,
		TitleOverlap:               f.TitleOverlap,
		BranchOverlap:              f.BranchOverlap,
		SameAuthor:                 f.SameAuthor,
		LinkedDefectIssue:          f.LinkedDefectIssue,
		RegressionTestChanged:      f.RegressionTestChanged,
		IncidentOrRollback:         f.IncidentOrRollback,
	}
}
