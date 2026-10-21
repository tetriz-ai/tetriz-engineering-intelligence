// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeLookup is an in-memory Lookup.
type fakeLookup struct {
	issues   map[string][]Ref // by identifier
	prs      map[string][]Ref // by "repo#N"
	mergedAt map[string]time.Time
	closers  map[string]string
	err      error
}

func (f *fakeLookup) IssuesByIdentifier(_ context.Context, ident string) ([]Ref, error) {
	return f.issues[ident], f.err
}

func (f *fakeLookup) PRsByNumber(_ context.Context, repo string, n int) ([]Ref, error) {
	return f.prs[repo+"#"+strconv.Itoa(n)], f.err
}

func (f *fakeLookup) MergedAt(context.Context, []string) (map[string]time.Time, error) {
	return f.mergedAt, f.err
}

func (f *fakeLookup) Closers(context.Context, []string) (map[string]string, error) {
	return f.closers, f.err
}

func issueRefs(byIdent map[string]string) map[string][]Ref {
	out := map[string][]Ref{}
	for id, key := range byIdent {
		out[id] = []Ref{{Key: key, Vendor: VendorJira}}
	}
	return out
}

var (
	prEntity = Entity{Side: SidePR, Key: "pr-1"}
	now0     = time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
)

func cand(sig SignalType, field SourceField, intent Intent, section Section) Candidate {
	return Candidate{Signal: sig, Identifier: "PROJ-1", Field: field, Intent: intent, Section: section}
}

// TestProcess_VendorNativeBeatsANegativeMention: a native link and a
// negative-heading mention of one issue fold into the native link's class, and
// the negative mention neither tags negative_section nor corroborates.
func TestProcess_VendorNativeBeatsANegativeMention(t *testing.T) {
	l := &fakeLookup{issues: issueRefs(map[string]string{"PROJ-1": "issue-1"})}
	for i := 0; i < 50; i++ {
		res, err := Process(context.Background(), l, prEntity, Extraction{Candidates: []Candidate{
			cand(SignalNative, FieldNativeLink, IntentFixes, SectionNone),
			cand(SignalText, FieldPRBody, IntentReferencesContext, SectionNegative),
		}}, now0)
		require.NoError(t, err)
		require.Len(t, res.Links, 1)
		ln := res.Links[0]
		assert.Equal(t, "issue-1", ln.Issue)
		assert.Equal(t, BucketDirect, ln.Bucket)
		assert.Equal(t, TierHigh, ln.Tier)
		assert.Equal(t, RelationFixes, ln.Relation)
		assert.True(t, ln.Closes)
		assert.NotContains(t, ln.Blocking, BlockedNegativeSection)
		assert.Contains(t, ln.Blocking, BlockedSingleSignalFloor)
		assert.Equal(t, []SourceField{FieldNativeLink}, ln.Provenance)
		assert.Len(t, ln.Evidence, 2)
	}
}

func TestProcess_Combine(t *testing.T) {
	tests := []struct {
		name     string
		cands    []Candidate
		bucket   Bucket
		tier     Tier
		relation Relation
		closes   bool
		negSec   bool
		floor    bool
		prov     []SourceField
	}{
		{name: "two positive fields clear the floor",
			cands:  []Candidate{cand(SignalText, FieldPRBody, IntentFixes, SectionNone), cand(SignalText, FieldPRTitle, IntentFixes, SectionNone)},
			bucket: BucketDirect, tier: TierHigh, relation: RelationFixes, closes: true, prov: []SourceField{FieldPRBody, FieldPRTitle}},
		{name: "a bare title mention keeps the link unblocked beside a negative mention",
			cands:  []Candidate{cand(SignalText, FieldPRBody, IntentReferencesContext, SectionNegative), cand(SignalText, FieldPRTitle, IntentBare, SectionNone)},
			bucket: BucketReference, tier: TierLow, relation: RelationReferenceOnly, prov: []SourceField{FieldPRTitle}},
		{name: "a keyword mention elsewhere wins over a negative mention",
			cands:  []Candidate{cand(SignalText, FieldPRBody, IntentReferencesContext, SectionNegative), cand(SignalText, FieldPRBody, IntentFixes, SectionNone)},
			bucket: BucketDirect, tier: TierHigh, relation: RelationFixes, closes: true, floor: true, prov: []SourceField{FieldPRBody}},
		{name: "two negative mentions alone stay blocked",
			cands:  []Candidate{cand(SignalText, FieldPRBody, IntentReferencesContext, SectionNegative), cand(SignalText, FieldIssueBody, IntentReferencesContext, SectionNegative)},
			bucket: BucketReference, tier: TierLow, relation: RelationReferencesContext, negSec: true, prov: []SourceField{}},
		{name: "a negative mention alone has no provenance and no floor",
			cands:  []Candidate{cand(SignalText, FieldPRBody, IntentReferencesContext, SectionNegative)},
			bucket: BucketReference, tier: TierLow, relation: RelationReferencesContext, negSec: true, prov: []SourceField{}},
		{name: "implements and bare: direct, medium, no floor below high",
			cands:  []Candidate{cand(SignalText, FieldPRBody, IntentImplements, SectionNone), cand(SignalText, FieldPRTitle, IntentBare, SectionNone)},
			bucket: BucketDirect, tier: TierMedium, relation: RelationImplements, prov: []SourceField{FieldPRBody, FieldPRTitle}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &fakeLookup{issues: issueRefs(map[string]string{"PROJ-1": "issue-1"})}
			for i := 0; i < 25; i++ {
				res, err := Process(context.Background(), l, prEntity, Extraction{Candidates: tt.cands}, now0)
				require.NoError(t, err)
				require.Len(t, res.Links, 1)
				ln := res.Links[0]
				assert.Equal(t, tt.bucket, ln.Bucket)
				assert.Equal(t, tt.tier, ln.Tier)
				assert.Equal(t, tt.relation, ln.Relation)
				assert.Equal(t, tt.closes, ln.Closes)
				assert.Equal(t, tt.negSec, contains(ln.Blocking, BlockedNegativeSection))
				assert.Equal(t, tt.floor, contains(ln.Blocking, BlockedSingleSignalFloor))
				assert.ElementsMatch(t, tt.prov, ln.Provenance)
			}
		})
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// TestStronger: stronger is a total order over the classes classify emits,
// and folding them in any order gives the same winner.
func TestStronger(t *testing.T) {
	native := rawClass{BucketDirect, TierHigh, RelationFixes, true, false}
	impl := rawClass{BucketDirect, TierMedium, RelationImplements, false, false}
	negative := rawClass{BucketReference, TierLow, RelationReferencesContext, false, true}
	floor := rawClass{BucketReference, TierLow, RelationReferenceOnly, false, false}
	all := []rawClass{native, impl, negative, floor}
	for i, x := range all {
		assert.True(t, stronger(x, x))
		for j, y := range all {
			if i != j {
				assert.NotEqual(t, stronger(x, y), stronger(y, x), "%v vs %v", x, y)
			}
		}
	}
	assert.True(t, stronger(native, impl))
	assert.True(t, stronger(impl, negative))
	assert.True(t, stronger(negative, floor))
	assert.True(t, stronger(rawClass{BucketDirect, TierHigh, RelationFixes, true, false}, rawClass{BucketDirect, TierHigh, RelationFixes, false, false}))
	for _, perm := range permute(all) {
		got := floor
		for _, rc := range perm {
			if stronger(rc, got) {
				got = rc
			}
		}
		assert.Equal(t, native, got)
	}
}

func permute(in []rawClass) [][]rawClass {
	if len(in) <= 1 {
		return [][]rawClass{append([]rawClass(nil), in...)}
	}
	var out [][]rawClass
	for i := range in {
		rest := append(append([]rawClass(nil), in[:i]...), in[i+1:]...)
		for _, p := range permute(rest) {
			out = append(out, append([]rawClass{in[i]}, p...))
		}
	}
	return out
}

// TestRelationPriority_RanksEveryClassifiedRelation: every relation classify
// can emit has a positive rank, so stronger never ranks it below the floor.
func TestRelationPriority_RanksEveryClassifiedRelation(t *testing.T) {
	for _, sig := range []SignalType{SignalText, SignalNative} {
		for in := IntentBare; in <= IntentReferencesContext; in++ {
			for sec := SectionNone; sec <= SectionNegative; sec++ {
				r := classify(Candidate{Signal: sig, Intent: in, Section: sec}).relation
				assert.Positive(t, relationPriority(r), "%v", r)
			}
		}
	}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		c    Candidate
		want rawClass
	}{
		{"native", Candidate{Signal: SignalNative}, rawClass{BucketDirect, TierHigh, RelationFixes, true, false}},
		{"fixes", Candidate{Signal: SignalText, Intent: IntentFixes}, rawClass{BucketDirect, TierHigh, RelationFixes, true, false}},
		{"implements", Candidate{Signal: SignalText, Intent: IntentImplements}, rawClass{BucketDirect, TierMedium, RelationImplements, false, false}},
		{"negative heading overrides fixes", Candidate{Signal: SignalText, Intent: IntentFixes, Section: SectionNegative},
			rawClass{BucketReference, TierLow, RelationReferencesContext, false, true}},
		{"bare", Candidate{Signal: SignalText, Intent: IntentBare}, rawClass{BucketReference, TierLow, RelationReferenceOnly, false, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, classify(tt.c))
		})
	}
}

// TestProcess_LookupErrorIsReturned: a lookup error is returned with no
// result, so the row stays armed and retries.
func TestProcess_LookupErrorIsReturned(t *testing.T) {
	_, err := Process(context.Background(), &fakeLookup{err: errors.New("db down")}, prEntity,
		Extraction{Candidates: []Candidate{cand(SignalText, FieldPRBody, IntentBare, SectionNone)}}, now0)
	require.Error(t, err)
}

func TestProcess_Status(t *testing.T) {
	l := &fakeLookup{issues: issueRefs(map[string]string{"PROJ-1": "issue-1"})}
	res, err := Process(context.Background(), l, prEntity, Extraction{}, now0)
	require.NoError(t, err)
	assert.Equal(t, StatusNotFound, res.Status)
	res, err = Process(context.Background(), l, prEntity, Extraction{Candidates: []Candidate{
		cand(SignalText, FieldPRBody, IntentBare, SectionNone), {Signal: SignalText, Identifier: "PROJ-9", Field: FieldPRBody},
	}}, now0)
	require.NoError(t, err)
	assert.Equal(t, StatusPartiallyProcessed, res.Status)
	require.Len(t, res.Unresolved, 1)
	assert.Equal(t, ReasonNotFound, res.Unresolved[0].Reason)
	res, err = Process(context.Background(), l, prEntity, Extraction{Candidates: []Candidate{cand(SignalText, FieldPRBody, IntentBare, SectionNone)}}, now0)
	require.NoError(t, err)
	assert.Equal(t, StatusProcessed, res.Status)
}

func TestResolve(t *testing.T) {
	l := &fakeLookup{
		prs: map[string][]Ref{
			"acme/web#42":      {{Key: "pr-gh", Vendor: VendorGitHub}},
			"acme/team/web#12": {{Key: "mr-1", Vendor: VendorGitLab}},
			"acme/web#9":       {{Key: "pr-gh-9", Vendor: VendorGitHub}},
		},
		issues: map[string][]Ref{
			"ENG-123": {{Key: "jira-1", Vendor: VendorJira}, {Key: "linear-1", Vendor: VendorLinear}},
		},
	}
	tests := []struct {
		name   string
		from   Side
		c      Candidate
		key    string
		reason string
	}{
		{"github pull-request key", SideIssue, Candidate{Identifier: "acme/web#PR-42"}, "pr-gh", ""},
		{"github pull-request key, no such pull request", SideIssue, Candidate{Identifier: "acme/web#PR-43"}, "", ReasonNotFound},
		{"gitlab path key", SideIssue, Candidate{Identifier: "acme/team/web#MR-12"}, "mr-1", ""},
		{"gitlab path key needs a gitlab pull request", SideIssue, Candidate{Identifier: "acme/web#MR-9"}, "", ReasonNotFound},
		{"gitlab id key is unsupported", SideIssue, Candidate{Identifier: "mr-123!7"}, "", ReasonUnsupported},
		{"malformed key", SideIssue, Candidate{Identifier: "not-a-pr-key"}, "", ReasonNotFound},
		{"empty identifier", SideIssue, Candidate{Identifier: ""}, "", ReasonNotFound},
		{"github owner/repo#N on an issue names that pull request", SideIssue, Candidate{Identifier: "acme/web#42", Namespace: VendorGitHub}, "pr-gh", ""},
		{"owner/repo#N on an issue with no such pull request", SideIssue, Candidate{Identifier: "acme/web#41", Namespace: VendorGitHub}, "", ReasonNotFound},
		{"a gitlab issue reference never names a github pull request", SideIssue, Candidate{Identifier: "acme/web#42", Namespace: VendorGitLab}, "", ReasonUnsupported},
		{"a gitlab issue reference is not a merge request", SideIssue, Candidate{Identifier: "acme/team/web#12", Namespace: VendorGitLab}, "", ReasonUnsupported},
		{"an issue key on an issue names no pull request", SideIssue, Candidate{Identifier: "PROJ-2", Namespace: VendorJira}, "", ReasonUnsupported},
		{"issue key scoped to jira", SidePR, Candidate{Identifier: "ENG-123", Namespace: VendorJira}, "jira-1", ""},
		{"issue key scoped to linear", SidePR, Candidate{Identifier: "ENG-123", Namespace: VendorLinear}, "linear-1", ""},
		{"issue key in two trackers, no namespace", SidePR, Candidate{Identifier: "ENG-123"}, "", ReasonAmbiguous},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, reason, err := resolve(context.Background(), l, tt.from, tt.c)
			require.NoError(t, err)
			assert.Equal(t, tt.key, key)
			assert.Equal(t, tt.reason, reason)
		})
	}
}

func TestElectCloser(t *testing.T) {
	t0 := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	claim := func(pr string) Link { return Link{PR: pr, Issue: "i", Closes: true, Relation: RelationFixes} }
	tests := []struct {
		name     string
		links    []Link
		mergedAt map[string]time.Time
		closer   string
		closes   map[string]bool
		relation map[string]Relation
	}{
		{name: "a single claimer wins", links: []Link{claim("pr-1")}, mergedAt: map[string]time.Time{"pr-1": t0},
			closer: "pr-1", closes: map[string]bool{"pr-1": true}},
		{name: "the newest merge wins, the older is part of", links: []Link{claim("pr-old"), claim("pr-new")},
			mergedAt: map[string]time.Time{"pr-old": t0, "pr-new": t0.Add(48 * time.Hour)},
			closer:   "pr-new", closes: map[string]bool{"pr-old": false, "pr-new": true},
			relation: map[string]Relation{"pr-old": RelationPartOf, "pr-new": RelationFixes}},
		{name: "no claimers, no closer", links: []Link{{PR: "pr-1", Issue: "i"}}, closer: "", closes: map[string]bool{"pr-1": false}},
		{name: "a tie on merge time goes to the smaller key", links: []Link{claim("pr-b"), claim("pr-a")},
			mergedAt: map[string]time.Time{"pr-a": t0, "pr-b": t0}, closer: "pr-a",
			closes: map[string]bool{"pr-a": true, "pr-b": false}},
		{name: "a single unmerged claimer is not elected", links: []Link{claim("pr-open")},
			closer: "", closes: map[string]bool{"pr-open": true}},
		{name: "no merged claimer, no closer, every claim stands", links: []Link{claim("pr-1"), claim("pr-2")},
			closer: "", closes: map[string]bool{"pr-1": true, "pr-2": true}},
		{name: "an unmerged claimer loses to a merged one", links: []Link{claim("pr-merged"), claim("pr-open")},
			mergedAt: map[string]time.Time{"pr-merged": t0}, closer: "pr-merged",
			closes: map[string]bool{"pr-merged": true, "pr-open": false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Result{Entity: Entity{Side: SideIssue, Key: "i"}, Links: tt.links}
			require.NoError(t, electCloser(context.Background(), &fakeLookup{mergedAt: tt.mergedAt}, &res))
			assert.Equal(t, tt.closer, res.Closer)
			for _, ln := range res.Links {
				if want, ok := tt.closes[ln.PR]; ok {
					assert.Equal(t, want, ln.Closes, ln.PR)
				}
				if want, ok := tt.relation[ln.PR]; ok {
					assert.Equal(t, want, ln.Relation, ln.PR)
				}
			}
		})
	}
}

// TestDeriveCloses: a pull request closes an issue only when it is the
// issue's elected closer; an issue with no election leaves the claim.
func TestDeriveCloses(t *testing.T) {
	res := Result{Entity: Entity{Side: SidePR, Key: "pr-1"}, Links: []Link{
		{PR: "pr-1", Issue: "won", Closes: true, Relation: RelationFixes},
		{PR: "pr-1", Issue: "lost", Closes: true, Relation: RelationFixes, Blocking: []string{BlockedSingleSignalFloor}},
		{PR: "pr-1", Issue: "open", Closes: true, Relation: RelationFixes},
		{PR: "pr-1", Issue: "won-by-context", Bucket: BucketReference, Tier: TierLow, Relation: RelationReferencesContext, Blocking: []string{BlockedNegativeSection}},
		{PR: "pr-1", Issue: "lost-by-mention", Relation: RelationReferenceOnly},
		{PR: "pr-1", Issue: "lost-by-implements", Relation: RelationImplements},
	}}
	closers := map[string]string{"won": "pr-1", "lost": "pr-2", "won-by-context": "pr-1", "lost-by-mention": "pr-2", "lost-by-implements": "pr-2"}
	require.NoError(t, deriveCloses(context.Background(), &fakeLookup{closers: closers}, &res))
	l := res.Links
	assert.True(t, l[0].Closes)
	assert.False(t, l[1].Closes)
	assert.Equal(t, RelationPartOf, l[1].Relation)
	assert.Equal(t, []string{BlockedSingleSignalFloor, BlockedNonElectedCloser}, l[1].Blocking)
	assert.True(t, l[2].Closes, "no election leaves the claim")
	assert.True(t, l[3].Closes, "the elected closer closes, whatever its own evidence")
	assert.Equal(t, RelationFixes, l[3].Relation)
	assert.Empty(t, l[3].Blocking)
	assert.Equal(t, BucketDirect, l[3].Bucket, "a fixes link is direct and high, as the README's Process table says")
	assert.Equal(t, TierHigh, l[3].Tier)
	assert.Equal(t, Link{PR: "pr-1", Issue: "lost-by-mention", Relation: RelationReferenceOnly}, l[4], "a mention is not demoted")
	assert.Equal(t, Link{PR: "pr-1", Issue: "lost-by-implements", Relation: RelationImplements}, l[5], "an implements link is not demoted")
}

// TestProcess_OrderIsStable: links come sorted by counterpart, and signals
// and provenance by value, whatever the map order.
func TestProcess_OrderIsStable(t *testing.T) {
	l := &fakeLookup{issues: issueRefs(map[string]string{"PROJ-1": "i1", "PROJ-2": "i2", "PROJ-3": "i3"})}
	ex := ExtractPR(PR{Key: "P", Vendor: VendorGitHub, Title: "Fixes PROJ-3", Body: "Fixes PROJ-2 PROJ-1 PROJ-3", HeadBranch: "f/PROJ-3",
		NativeIssueRefs: []NativeRef{{Key: "PROJ-3", Source: VendorGitHub}}}, VendorAny, DefaultSignals())
	for i := 0; i < 200; i++ {
		res, err := Process(context.Background(), l, prEntity, ex, now0)
		require.NoError(t, err)
		require.Len(t, res.Links, 3)
		assert.Equal(t, []string{"i1", "i2", "i3"}, []string{res.Links[0].Issue, res.Links[1].Issue, res.Links[2].Issue})
		assert.Equal(t, []SignalType{SignalText, SignalNative}, res.Links[2].Signals)
		assert.Equal(t, []SourceField{FieldPRTitle, FieldPRBody, FieldBranch, FieldNativeLink}, res.Links[2].Provenance)
	}
}

// TestProcess_TitleMentionKeepsABackgroundMentionUnblocked: the title names
// PROJ-1 and a Background section mentions it too; the strongest evidence wins,
// so the one link is unblocked.
func TestProcess_TitleMentionKeepsABackgroundMentionUnblocked(t *testing.T) {
	l := &fakeLookup{issues: issueRefs(map[string]string{"PROJ-1": "issue-1"})}
	ex := ExtractPR(PR{Key: "pr-1", Vendor: VendorGitHub, Title: "PROJ-1 login retry", Body: "## Background\n\nPROJ-1 came up last week"}, VendorAny, DefaultSignals())
	res, err := Process(context.Background(), l, prEntity, ex, now0)
	require.NoError(t, err)
	require.Len(t, res.Links, 1)
	assert.Empty(t, res.Links[0].Blocking)
	assert.Equal(t, RelationReferenceOnly, res.Links[0].Relation)
	assert.Len(t, res.Links[0].Evidence, 2)
}

// TestProcess_AnIssueKeyOnAnIssueIsNotRetried: an issue's own text naming
// another issue (a key or a Jira, Linear or GitLab issue URL) names no pull
// request. It is reported as unsupported, and it does not hold the row
// partially processed, so the row is not retried for it.
func TestProcess_AnIssueKeyOnAnIssueIsNotRetried(t *testing.T) {
	for _, body := range []string{
		"Duplicate of PROJ-2",
		"See https://example.atlassian.net/browse/PROJ-2",
		"See https://linear.app/team/issue/ENG-2/x",
		"See https://gitlab.com/acme/web/-/issues/2",
	} {
		ex := ExtractIssue(Issue{Key: "i", Vendor: VendorJira, Body: body}, DefaultSignals())
		res, err := Process(context.Background(), &fakeLookup{}, Entity{Side: SideIssue, Key: "i"}, ex, now0)
		require.NoError(t, err)
		assert.Equal(t, StatusNotFound, res.Status, body)
		require.Len(t, res.Unresolved, 1, body)
		assert.Equal(t, ReasonUnsupported, res.Unresolved[0].Reason, body)
	}
	// A pull-request reference beside it still holds the row for a retry.
	ex := ExtractIssue(Issue{Key: "i", Vendor: VendorJira, Body: "Duplicate of PROJ-2, fixed in https://github.com/acme/web/pull/3"}, DefaultSignals())
	res, err := Process(context.Background(), &fakeLookup{}, Entity{Side: SideIssue, Key: "i"}, ex, now0)
	require.NoError(t, err)
	assert.Equal(t, StatusPartiallyProcessed, res.Status)
}

// TestProcess_ALaterHeadingEndsTheBackground: a closing reference under a
// later heading is a closing link, unblocked.
func TestProcess_ALaterHeadingEndsTheBackground(t *testing.T) {
	l := &fakeLookup{issues: issueRefs(map[string]string{"PROJ-3": "i3", "PROJ-7": "i7"})}
	ex := ExtractPR(PR{Key: "pr-1", Vendor: VendorGitHub, Body: "## Background\n\nPROJ-3 tried this.\n\n## Changes\n\nFixes PROJ-7.\n"}, VendorAny, DefaultSignals())
	res, err := Process(context.Background(), l, prEntity, ex, now0)
	require.NoError(t, err)
	require.Len(t, res.Links, 2)
	assert.Equal(t, []string{BlockedNegativeSection}, res.Links[0].Blocking)
	assert.Equal(t, "i7", res.Links[1].Issue)
	assert.Equal(t, RelationFixes, res.Links[1].Relation)
	assert.NotContains(t, res.Links[1].Blocking, BlockedNegativeSection)
}

// TestProcess_ALowerCaseBranchKeyIsNotRetried: a key read case-folded from a
// branch name links when its issue is stored, and when none is, it is reported
// but does not hold the row for a retry. A later put of its issue re-arms the
// row (AnIssuePutReArmsTheRowsWaitingForIt in store/storetest).
func TestProcess_ALowerCaseBranchKeyIsNotRetried(t *testing.T) {
	l := &fakeLookup{issues: issueRefs(map[string]string{"ENG-42": "i42", "PROJ-1": "i1"})}
	run := func(pr PR) Result {
		pr.Key, pr.Vendor = "pr-1", VendorGitHub
		res, err := Process(context.Background(), l, prEntity, ExtractPR(pr, VendorAny, DefaultSignals()), now0)
		require.NoError(t, err)
		return res
	}
	res := run(PR{Title: "Bump deps", HeadBranch: "release/v1-2"})
	assert.Equal(t, StatusNotFound, res.Status)
	require.Len(t, res.Unresolved, 1)
	assert.Equal(t, "V1-2", res.Unresolved[0].Candidate.Identifier)
	assert.Equal(t, ReasonNotFound, res.Unresolved[0].Reason)

	res = run(PR{Title: "Fixes PROJ-1", HeadBranch: "release/v1-2"})
	assert.Equal(t, StatusProcessed, res.Status)
	require.Len(t, res.Unresolved, 1)

	res = run(PR{Title: "Retry limiter", HeadBranch: "eng-42-x"})
	assert.Equal(t, StatusProcessed, res.Status)
	require.Len(t, res.Links, 1)
	assert.Equal(t, "i42", res.Links[0].Issue)

	res = run(PR{Title: "Retry limiter", HeadBranch: "ENG-43-x"})
	assert.Equal(t, StatusPartiallyProcessed, res.Status, "an upper-case key keeps the retry")
}
