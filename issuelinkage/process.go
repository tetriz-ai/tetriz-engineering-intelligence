// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Ref is a stored record a lookup found.
type Ref struct {
	Key    string `json:"key"`
	Vendor Vendor `json:"vendor"`
}

// Lookup is what stage 2 reads about the other side. The stores implement it.
type Lookup interface {
	// IssuesByIdentifier returns the issues with ident among their
	// Identifiers, sorted by key.
	IssuesByIdentifier(ctx context.Context, ident string) ([]Ref, error)
	// PRsByNumber returns the pull requests in repository with number, sorted
	// by key.
	PRsByNumber(ctx context.Context, repository string, number int) ([]Ref, error)
	// MergedAt returns the merge time of each merged pull request among keys.
	MergedAt(ctx context.Context, prKeys []string) (map[string]time.Time, error)
	// Closers returns each issue's elected closing pull request, for the
	// issues among keys that have one.
	Closers(ctx context.Context, issueKeys []string) (map[string]string, error)
}

// Link is one resolved pull request to issue relationship, as one side's
// evidence states it.
type Link struct {
	// PR and Issue are the two keys.
	PR    string `json:"pr"`
	Issue string `json:"issue"`
	// From is the side whose evidence produced the link.
	From     Side     `json:"from"`
	Bucket   Bucket   `json:"bucket"`
	Tier     Tier     `json:"tier"`
	Relation Relation `json:"relation"`
	// Closes is the closure claim. On an issue's row it is true for the
	// elected closer; on a pull request's row it follows the issue's election
	// once the issue has one.
	Closes bool `json:"closes"`
	// Signals and Provenance are the signal types and the source fields that
	// support the link; negative-heading mentions are not provenance.
	Signals    []SignalType  `json:"signals"`
	Provenance []SourceField `json:"provenance"`
	// Blocking lists the reasons the link is weaker than its tier says.
	Blocking []string `json:"blocking"`
	// Evidence is every candidate that resolved to the counterpart.
	Evidence []Candidate `json:"evidence"`
	// FirstSeenAt is when stage 2 first saw the link, kept across runs;
	// LastCorroboratedAt is the latest run that saw it.
	FirstSeenAt        time.Time `json:"first_seen_at"`
	LastCorroboratedAt time.Time `json:"last_corroborated_at"`
}

// Counterpart returns the key on the side opposite from.
func (l Link) Counterpart(from Side) string {
	if from == SidePR {
		return l.Issue
	}
	return l.PR
}

// Unresolved is a candidate that named no stored record.
type Unresolved struct {
	Candidate Candidate `json:"candidate"`
	// Reason is ReasonNotFound, ReasonAmbiguous or ReasonUnsupported.
	Reason string `json:"reason"`
}

// Result is stage 2's output for one entity.
type Result struct {
	Entity Entity `json:"entity"`
	// Status is StatusPartiallyProcessed when a candidate did not resolve
	// for a reason a later run can change (not_found, ambiguous),
	// StatusNotFound when there is no link and nothing to retry, and
	// StatusProcessed otherwise. An unsupported candidate, and a case-folded
	// branch key that did not resolve, is reported in Unresolved and never
	// retried.
	Status     Status       `json:"status"`
	Links      []Link       `json:"links"`
	Unresolved []Unresolved `json:"unresolved"`
	// Closer is the elected closing pull request, on an issue's row.
	Closer string `json:"closer"`
	// TitleFlags are the title's noise markers.
	TitleFlags []string `json:"title_flags"`
	// NativeRefsTruncated reports that the vendor's native link list was cut.
	NativeRefsTruncated bool `json:"native_refs_truncated"`
}

var (
	// prHashKeyPattern matches the GitHub and Bitbucket pull-request key.
	prHashKeyPattern = regexp.MustCompile(`^([\w.-]+/[\w.-]+)#PR-(\d+)$`)
	// fullKeyPattern matches an identifier that is a project key and
	// nothing else.
	fullKeyPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,9}-[0-9]{1,7}$`)
	// pathHashKeyPattern matches a "path#N" reference, nested groups allowed.
	pathHashKeyPattern = regexp.MustCompile(`^[\w.-]+(?:/[\w.-]+)+#\d+$`)
	// issueHashKeyPattern matches a GitHub "owner/repo#N" reference.
	issueHashKeyPattern = regexp.MustCompile(`^([\w.-]+/[\w.-]+)#(\d+)$`)
	// gitlabMRKeyPattern matches a path-based GitLab merge-request key,
	// nested groups allowed.
	gitlabMRKeyPattern = regexp.MustCompile(`^(.+)#MR-(\d+)$`)
)

// resolve maps one candidate to the counterpart's key, or "" and a reason.
func resolve(ctx context.Context, l Lookup, from Side, c Candidate) (string, string, error) {
	ident := c.Identifier
	if ident == "" {
		return "", ReasonNotFound, nil
	}
	if from == SidePR {
		refs, err := l.IssuesByIdentifier(ctx, ident)
		if err != nil {
			return "", "", fmt.Errorf("resolving issue %q: %w", ident, err)
		}
		return pick(refs, c.Namespace)
	}
	// On an issue, a reference to another issue names no pull request:
	// a key, a Jira or Linear URL, or a GitLab issue "path#N".
	if fullKeyPattern.MatchString(ident) || (c.Namespace == VendorGitLab && pathHashKeyPattern.MatchString(ident)) {
		return "", ReasonUnsupported, nil
	}
	if m := prHashKeyPattern.FindStringSubmatch(ident); m != nil {
		n, err := strconv.Atoi(m[2])
		if err != nil {
			return "", "", fmt.Errorf("pull request key %q: %w", ident, err)
		}
		refs, err := l.PRsByNumber(ctx, m[1], n)
		if err != nil {
			return "", "", fmt.Errorf("resolving pull request %q: %w", ident, err)
		}
		return pick(refs, VendorAny)
	}
	if m := gitlabMRKeyPattern.FindStringSubmatch(ident); m != nil {
		n, err := strconv.Atoi(m[2])
		if err != nil {
			return "", "", fmt.Errorf("merge request key %q: %w", ident, err)
		}
		refs, err := l.PRsByNumber(ctx, m[1], n)
		if err != nil {
			return "", "", fmt.Errorf("resolving merge request %q: %w", ident, err)
		}
		return pick(refs, VendorGitLab)
	}
	if m := issueHashKeyPattern.FindStringSubmatch(ident); m != nil && c.Namespace == VendorGitHub {
		// GitHub numbers issues and pull requests in one sequence, so
		// "acme/web#7" on an issue names pull request 7 when one exists.
		n, err := strconv.Atoi(m[2])
		if err != nil {
			return "", "", fmt.Errorf("reference %q: %w", ident, err)
		}
		refs, err := l.PRsByNumber(ctx, m[1], n)
		if err != nil {
			return "", "", fmt.Errorf("resolving pull request %q: %w", ident, err)
		}
		return pick(refs, VendorGitHub)
	}
	if strings.HasPrefix(ident, "mr-") {
		return "", ReasonUnsupported, nil
	}
	return "", ReasonNotFound, nil
}

// pick returns the one ref in namespace ns (any when ns is VendorAny).
func pick(refs []Ref, ns Vendor) (string, string, error) {
	var keys []string
	for _, r := range refs {
		if ns == VendorAny || r.Vendor == ns {
			keys = append(keys, r.Key)
		}
	}
	switch len(keys) {
	case 0:
		return "", ReasonNotFound, nil
	case 1:
		return keys[0], "", nil
	}
	return "", ReasonAmbiguous, nil
}

// rawClass is the classification one candidate implies.
type rawClass struct {
	bucket   Bucket
	tier     Tier
	relation Relation
	closes   bool
	negSec   bool // the candidate sat under a negative heading
}

// classify maps one candidate to its classification: a negative heading is
// read first, then the signal type, then the intent; anything else is a bare
// reference.
func classify(c Candidate) rawClass {
	if c.Section == SectionNegative {
		return rawClass{bucket: BucketReference, tier: TierLow, relation: RelationReferencesContext, negSec: true}
	}
	if c.Signal == SignalNative {
		return rawClass{bucket: BucketDirect, tier: TierHigh, relation: RelationFixes, closes: true}
	}
	switch c.Intent {
	case IntentFixes:
		return rawClass{bucket: BucketDirect, tier: TierHigh, relation: RelationFixes, closes: true}
	case IntentImplements:
		return rawClass{bucket: BucketDirect, tier: TierMedium, relation: RelationImplements}
	}
	return rawClass{bucket: BucketReference, tier: TierLow, relation: RelationReferenceOnly}
}

// relationPriority ranks relations for the strength tie-break; higher is
// stronger.
func relationPriority(r Relation) int {
	switch r {
	case RelationFixes:
		return 5
	case RelationImplements:
		return 4
	case RelationPartOf:
		return 3
	case RelationReferencesContext:
		return 2
	case RelationReferenceOnly:
		return 1
	}
	return 0
}

// stronger reports whether a is at least as strong as b: bucket, then tier,
// then closes, then relation. The order is total, so folding candidates in any
// order keeps the same winner.
func stronger(a, b rawClass) bool {
	if a.bucket != b.bucket {
		return a.bucket > b.bucket
	}
	if a.tier != b.tier {
		return a.tier > b.tier
	}
	if a.closes != b.closes {
		return a.closes
	}
	return relationPriority(a.relation) >= relationPriority(b.relation)
}

// agg accumulates the candidates that resolved to one counterpart.
type agg struct {
	// best is the strongest class among the candidates outside a negative
	// heading; valid when positive is set.
	best      rawClass
	positive  bool
	signals   map[SignalType]bool
	provClass map[SourceField]bool
	evidence  []Candidate
}

// class is the link's class: the strongest candidate outside a negative
// heading wins, and a negative-heading mention decides only when it is the
// only evidence.
func (a *agg) class() rawClass {
	if a.positive {
		return a.best
	}
	return rawClass{bucket: BucketReference, tier: TierLow, relation: RelationReferencesContext, negSec: true}
}

// Process runs stage 2 for one entity: it resolves each candidate of ex
// through l, folds the candidates that name one counterpart into one link of
// the strongest class, and, on an issue, elects the closing pull request; on
// a pull request, it reads each linked issue's election back.
//
// now stamps each link's FirstSeenAt and LastCorroboratedAt; Run keeps an
// earlier FirstSeenAt from the entity's prior row. An error from l is
// returned as is and no result is written: the row stays armed for the next
// run.
func Process(ctx context.Context, l Lookup, e Entity, ex Extraction, now time.Time) (Result, error) {
	now = now.UTC()
	out := map[string]*agg{}
	res := Result{Entity: e, TitleFlags: ex.TitleFlags}
	for _, c := range ex.Candidates {
		other, reason, err := resolve(ctx, l, e.Side, c)
		if err != nil {
			return Result{}, err
		}
		if other == "" {
			res.Unresolved = append(res.Unresolved, Unresolved{Candidate: c, Reason: reason})
			continue
		}
		a, ok := out[other]
		if !ok {
			a = &agg{signals: map[SignalType]bool{}, provClass: map[SourceField]bool{}}
			out[other] = a
		}
		a.signals[c.Signal] = true
		if c.Section != SectionNegative {
			a.provClass[c.Field] = true
		}
		a.evidence = append(a.evidence, c)
		if c.Section == SectionNegative {
			continue
		}
		if cls := classify(c); !a.positive || stronger(cls, a.best) {
			a.best, a.positive = cls, true
		}
	}
	others := make([]string, 0, len(out))
	for other := range out {
		others = append(others, other)
	}
	sort.Strings(others)
	for _, other := range others {
		a := out[other]
		cls := a.class()
		signals := make([]SignalType, 0, len(a.signals))
		for s := range a.signals {
			signals = append(signals, s)
		}
		sort.Slice(signals, func(i, j int) bool { return signals[i] < signals[j] })
		prov := make([]SourceField, 0, len(a.provClass))
		for f := range a.provClass {
			prov = append(prov, f)
		}
		sort.Slice(prov, func(i, j int) bool { return prov[i] < prov[j] })
		var blocking []string
		if cls.negSec {
			blocking = append(blocking, BlockedNegativeSection)
		}
		if len(prov) < 2 && cls.tier == TierHigh {
			blocking = append(blocking, BlockedSingleSignalFloor)
		}
		link := Link{
			From: e.Side, Bucket: cls.bucket, Tier: cls.tier, Relation: cls.relation, Closes: cls.closes,
			Signals: signals, Provenance: prov, Blocking: blocking, Evidence: a.evidence,
			FirstSeenAt: now, LastCorroboratedAt: now,
		}
		if e.Side == SidePR {
			link.PR, link.Issue = e.Key, other
		} else {
			link.PR, link.Issue = other, e.Key
		}
		res.Links = append(res.Links, link)
	}
	retry := false
	for _, u := range res.Unresolved {
		retry = retry || (u.Reason != ReasonUnsupported && !u.Candidate.CaseFolded)
	}
	switch {
	case retry:
		res.Status = StatusPartiallyProcessed
	case len(res.Links) == 0:
		res.Status = StatusNotFound
	default:
		res.Status = StatusProcessed
	}
	if e.Side == SideIssue {
		if err := electCloser(ctx, l, &res); err != nil {
			return Result{}, err
		}
	} else if err := deriveCloses(ctx, l, &res); err != nil {
		return Result{}, err
	}
	return res, nil
}

// electCloser picks the closing pull request among an issue's closure claims:
// the newest merge wins, a tie goes to the smaller key, and the other
// claimers become part-of links that no longer close. A claimer that has not
// merged cannot be elected; with no merged claimer there is no closer and
// every claim stands.
func electCloser(ctx context.Context, l Lookup, res *Result) error {
	var claimers []int
	for i, ln := range res.Links {
		if ln.Closes {
			claimers = append(claimers, i)
		}
	}
	if len(claimers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(claimers))
	for _, i := range claimers {
		keys = append(keys, res.Links[i].PR)
	}
	mergedAt, err := l.MergedAt(ctx, keys)
	if err != nil {
		return fmt.Errorf("reading merge times: %w", err)
	}
	winner := ""
	var winnerAt time.Time
	for _, i := range claimers {
		pr := res.Links[i].PR
		ts, ok := mergedAt[pr]
		if !ok {
			continue
		}
		if winner == "" || ts.After(winnerAt) || (ts.Equal(winnerAt) && pr < winner) {
			winner, winnerAt = pr, ts
		}
	}
	if winner == "" {
		return nil
	}
	for _, i := range claimers {
		ln := &res.Links[i]
		if ln.PR == winner {
			continue
		}
		ln.Closes = false
		ln.Relation = RelationPartOf
		ln.Blocking = append(ln.Blocking, BlockedNonElectedCloser)
	}
	res.Closer = winner
	return nil
}

// deriveCloses sets a pull request's closure claim from each linked issue's
// election: it closes the issue only when it is the elected closer, and then
// its link is a direct, high-tier fixes link without negative_section. A claim another pull
// request won becomes a part-of link with non_elected_closer, as on the
// issue's row; a link that claimed nothing is left as it is. An issue with no
// election leaves the claim as scored. The election reads only the issue's
// own evidence; a pull request's own claim cannot win it.
func deriveCloses(ctx context.Context, l Lookup, res *Result) error {
	if len(res.Links) == 0 {
		return nil
	}
	keys := make([]string, 0, len(res.Links))
	for _, ln := range res.Links {
		keys = append(keys, ln.Issue)
	}
	closers, err := l.Closers(ctx, keys)
	if err != nil {
		return fmt.Errorf("reading closers: %w", err)
	}
	for i := range res.Links {
		ln := &res.Links[i]
		closer, ok := closers[ln.Issue]
		if !ok || closer == "" {
			continue
		}
		if closer == res.Entity.Key {
			// The issue's own evidence elected this pull request, so the
			// link closes the issue whatever this row's own evidence says.
			ln.Closes = true
			ln.Relation = RelationFixes
			ln.Bucket, ln.Tier = BucketDirect, TierHigh
			ln.Blocking = without(ln.Blocking, BlockedNegativeSection)
			continue
		}
		if ln.Closes {
			// This pull request claimed closure and another won the election.
			ln.Closes = false
			ln.Relation = RelationPartOf
			ln.Blocking = append(ln.Blocking, BlockedNonElectedCloser)
		}
	}
	return nil
}

// without returns reasons with r left out.
func without(reasons []string, r string) []string {
	var out []string
	for _, x := range reasons {
		if x != r {
			out = append(out, x)
		}
	}
	return out
}
