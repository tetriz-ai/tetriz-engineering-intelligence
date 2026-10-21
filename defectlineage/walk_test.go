// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// fakeSource is a hand-built history: blame answers per (revision, path), and a
// first-parent chain. Building the history explicitly is what lets each test
// state the exact shape it is about.
type fakeSource struct {
	lines     map[string][]defectlineage.BlamedLine // "rev\x00path" -> lines
	parents   map[string]string
	linesErr  map[string]error
	parentErr map[string]error
}

func key(rev, path string) string { return rev + "\x00" + path }

func (f *fakeSource) Lines(_ context.Context, rev, path string) ([]defectlineage.BlamedLine, error) {
	if err := f.linesErr[key(rev, path)]; err != nil {
		return nil, err
	}
	return f.lines[key(rev, path)], nil
}

func (f *fakeSource) FirstParent(_ context.Context, sha string) (string, error) {
	if err := f.parentErr[sha]; err != nil {
		return "", err
	}
	return f.parents[sha], nil
}

func line(text, owner string) defectlineage.BlamedLine {
	return defectlineage.BlamedLine{
		Text: text, OwnerSHA: owner,
		CommittedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		AuthorName:  "A Dev", AuthorEmail: "dev@example.com",
	}
}

func fixWith(removed ...string) *defectlineagev1.PendingLineageBlame {
	return &defectlineagev1.PendingLineageBlame{
		BugFixPrId: "fix-1", PlatformRepoId: "r1", RepoFullName: "o/r", FixCommitSha: "FIX",
		LocationAnchors: []*defectlineagev1.LineageBlameAnchor{{
			Path: "a.go", StartLine: 10, EndLine: 10, RemovedText: removed,
		}},
	}
}

func newWalker(t *testing.T) *defectlineage.Walker {
	t.Helper()
	w, err := defectlineage.NewWalker(defectlineage.WalkParams{})
	require.NoError(t, err)
	return w
}

// TestBlameDirectNamesTheOwner is the base case: the removed line is absent at
// the owner's parent, so the owner wrote it.
func TestBlameDirectNamesTheOwner(t *testing.T) {
	src := &fakeSource{
		lines: map[string][]defectlineage.BlamedLine{
			key("P", "a.go"):  {line("x := 1", "CULPRIT")},
			key("C0", "a.go"): {line("other", "OLD")},
		},
		parents: map[string]string{"FIX": "P", "CULPRIT": "C0"},
	}
	state, results, err := newWalker(t).Walk(context.Background(), src, fixWith("x := 1"))
	require.NoError(t, err)
	assert.Equal(t, defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_RESOLVED, state)
	require.Len(t, results, 1)
	assert.Equal(t, "CULPRIT", results[0].GetCulpritSha())
	assert.EqualValues(t, 0, results[0].GetHopsWalked())
	assert.Equal(t, defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_DIRECT,
		results[0].GetEvidenceType())
	assert.EqualValues(t, 10, results[0].GetStartLine(), "the anchor's line rides back on the result")
}

// A whitespace-only reformat is walked past, not blamed.
func TestAWhitespaceReformatIsWalkedPast(t *testing.T) {
	src := &fakeSource{
		lines: map[string][]defectlineage.BlamedLine{
			key("P", "a.go"):  {line("    x := 1", "REFORMAT")},
			key("C1", "a.go"): {line("x := 1", "REAL")},
			key("C0", "a.go"): {line("other", "OLD")},
		},
		parents: map[string]string{"FIX": "P", "REFORMAT": "C1", "REAL": "C0"},
	}
	state, results, err := newWalker(t).Walk(context.Background(), src, fixWith("    x := 1"))
	require.NoError(t, err)
	assert.Equal(t, defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_RESOLVED, state)
	require.Len(t, results, 1)
	assert.Equal(t, "REAL", results[0].GetCulpritSha(), "the reformat must not be blamed")
	assert.EqualValues(t, 1, results[0].GetHopsWalked())
	assert.Equal(t, defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_AFTER_HOPS,
		results[0].GetEvidenceType(), "a hopped result records that it hopped")
}

// TestAnAmbiguousParentProducesNothing is the fail-closed rule at its sharpest.
// Removed text is routinely not unique — a bare return, a lone brace — and
// picking one occurrence would be a coin-flip that yields a textually
// indistinguishable wrong culprit.
func TestAnAmbiguousParentProducesNothing(t *testing.T) {
	src := &fakeSource{
		lines: map[string][]defectlineage.BlamedLine{
			key("P", "a.go"):  {line("return nil", "OWNER")},
			key("C1", "a.go"): {line("return nil", "X"), line("return nil", "Y")},
		},
		parents: map[string]string{"FIX": "P", "OWNER": "C1"},
	}
	state, results, err := newWalker(t).Walk(context.Background(), src, fixWith("return nil"))
	require.NoError(t, err)
	assert.Empty(t, results, "an ambiguous ancestry names nobody")
	assert.Equal(t, defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_UNATTRIBUTED, state)
}

// TestANonUniqueAnchorProducesNothing refuses at the anchor itself, before any
// walking: if the removed line appears twice at the anchor revision there is no
// single line to attribute.
func TestANonUniqueAnchorProducesNothing(t *testing.T) {
	src := &fakeSource{
		lines: map[string][]defectlineage.BlamedLine{
			key("P", "a.go"): {line("}", "A"), line("}", "B")},
		},
		parents: map[string]string{"FIX": "P"},
	}
	_, results, err := newWalker(t).Walk(context.Background(), src, fixWith("}"))
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestAVanishedPathProducesNothing covers the rename a path-scoped blame cannot
// follow. The file not existing at the parent could mean "created here" or
// "renamed from elsewhere", and the two are indistinguishable without the
// owner's diff — so the walk refuses rather than claim the owner wrote the line.
func TestAVanishedPathProducesNothing(t *testing.T) {
	src := &fakeSource{
		lines: map[string][]defectlineage.BlamedLine{
			key("P", "a.go"): {line("x := 1", "OWNER")},
			// C1 holds no a.go at all.
		},
		parents: map[string]string{"FIX": "P", "OWNER": "C1"},
	}
	_, results, err := newWalker(t).Walk(context.Background(), src, fixWith("x := 1"))
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestTheHopCapYieldsNothing pins that reaching the cap resolves to nothing,
// never to the last commit walked to — which would be a confident wrong answer
// dressed as a real one.
func TestTheHopCapYieldsNothing(t *testing.T) {
	// A line carried unchanged through more commits than the cap allows.
	src := &fakeSource{
		lines:   map[string][]defectlineage.BlamedLine{},
		parents: map[string]string{"FIX": "P"},
	}
	chain := []string{"P", "C1", "C2", "C3", "C4", "C5"}
	for i, rev := range chain {
		owner := "O" + rev
		src.lines[key(rev, "a.go")] = []defectlineage.BlamedLine{line("carried", owner)}
		if i+1 < len(chain) {
			src.parents[owner] = chain[i+1]
		}
	}
	_, results, err := newWalker(t).Walk(context.Background(), src, fixWith("carried"))
	require.NoError(t, err)
	assert.Empty(t, results, "reaching the cap must name nobody")
}

// TestARootCommitIsTheCulprit: nothing precedes a root commit, so it wrote the
// line.
func TestARootCommitIsTheCulprit(t *testing.T) {
	src := &fakeSource{
		lines:   map[string][]defectlineage.BlamedLine{key("P", "a.go"): {line("x := 1", "ROOT")}},
		parents: map[string]string{"FIX": "P", "ROOT": ""},
	}
	state, results, err := newWalker(t).Walk(context.Background(), src, fixWith("x := 1"))
	require.NoError(t, err)
	assert.Equal(t, defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_RESOLVED, state)
	require.Len(t, results, 1)
	assert.Equal(t, "ROOT", results[0].GetCulpritSha())
}

// A generated file names nobody.
func TestAGeneratedFileNamesNobody(t *testing.T) {
	src := &fakeSource{
		lines: map[string][]defectlineage.BlamedLine{
			key("P", "a.go"): {
				line("// Code generated by protoc-gen-go. DO NOT EDIT.", "GEN"),
				line("x := 1", "GEN"),
			},
			key("C0", "a.go"): {line("other", "OLD")},
		},
		parents: map[string]string{"FIX": "P", "GEN": "C0"},
	}
	_, results, err := newWalker(t).Walk(context.Background(), src, fixWith("x := 1"))
	require.NoError(t, err)
	assert.Empty(t, results, "a generated file must not name a culprit")
}

// TestOneAnchorsFailureKeepsAnothersCulprit: a fix resolves when any anchor
// names a culprit. The error still rides back, so the caller can tell a partial
// resolve from a complete one and re-offer the fix for the failed anchor.
func TestOneAnchorsFailureKeepsAnothersCulprit(t *testing.T) {
	boom := errors.New("provider unavailable")
	src := &fakeSource{
		lines: map[string][]defectlineage.BlamedLine{
			key("P", "a.go"):  {line("x := 1", "CULPRIT")},
			key("C0", "a.go"): {line("other", "OLD")},
		},
		parents:  map[string]string{"FIX": "P", "CULPRIT": "C0"},
		linesErr: map[string]error{key("P", "b.go"): boom},
	}
	fix := fixWith("x := 1")
	fix.LocationAnchors = append(fix.LocationAnchors, &defectlineagev1.LineageBlameAnchor{
		Path: "b.go", StartLine: 20, EndLine: 20, RemovedText: []string{"y := 2"},
	})

	state, results, err := newWalker(t).Walk(context.Background(), src, fix)
	assert.ErrorIs(t, err, boom, "the failed anchor is reported")
	assert.Equal(t, defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_RESOLVED, state)
	require.Len(t, results, 1, "the healthy anchor's culprit survives")
	assert.Equal(t, "CULPRIT", results[0].GetCulpritSha())
}

// TestEveryAnchorFailingIsAnErrorNotAnAnswer: with no culprit and a provider
// failure, reporting UNATTRIBUTED would claim "we looked and found none" when
// the truth is that the look failed.
func TestEveryAnchorFailingIsAnErrorNotAnAnswer(t *testing.T) {
	boom := errors.New("provider unavailable")
	src := &fakeSource{
		lines:    map[string][]defectlineage.BlamedLine{},
		parents:  map[string]string{"FIX": "P"},
		linesErr: map[string]error{key("P", "a.go"): boom},
	}
	state, results, err := newWalker(t).Walk(context.Background(), src, fixWith("x := 1"))
	assert.ErrorIs(t, err, boom)
	assert.Empty(t, results)
	assert.Equal(t, defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_UNSPECIFIED, state,
		"a failure must not read as a clean no-attribution")
}

// TestABlankOnlyRemovalNamesNobody: the walk reasons about the first substantive
// removed line, and a blank-only removal has none.
func TestABlankOnlyRemovalNamesNobody(t *testing.T) {
	src := &fakeSource{parents: map[string]string{"FIX": "P"}}
	_, results, err := newWalker(t).Walk(context.Background(), src, fixWith("", "   "))
	require.NoError(t, err)
	assert.Empty(t, results)
}

// TestResolveCandidatePRsCostsOneCallPerCulprit: a culprit reached from several
// anchors must not be looked up once per anchor, and a miss must be remembered
// so it is not re-queried.
func TestResolveCandidatePRsCostsOneCallPerCulprit(t *testing.T) {
	calls := map[string]int{}
	resolve := func(_ context.Context, sha string) (int32, bool) {
		calls[sha]++
		if sha == "HIT" {
			return 7, true
		}
		return 0, false
	}
	results := []*defectlineagev1.LineageBlameResult{
		{CulpritSha: "HIT"}, {CulpritSha: "HIT"}, {CulpritSha: "MISS"}, {CulpritSha: "MISS"},
	}
	newWalker(t).ResolveCandidatePRs(context.Background(), results, resolve)

	assert.Equal(t, 1, calls["HIT"], "a repeated culprit costs one lookup")
	assert.Equal(t, 1, calls["MISS"], "a miss is cached too")
	assert.EqualValues(t, 7, results[0].GetCandidatePrNumber())
	assert.EqualValues(t, 7, results[1].GetCandidatePrNumber())
	assert.EqualValues(t, 0, results[2].GetCandidatePrNumber(), "an unresolved culprit stays 0")
}

// TestANilResolverIsANoOp covers a provider with no commit-to-pull-request
// endpoint.
func TestANilResolverIsANoOp(t *testing.T) {
	results := []*defectlineagev1.LineageBlameResult{{CulpritSha: "X"}}
	newWalker(t).ResolveCandidatePRs(context.Background(), results, nil)
	assert.EqualValues(t, 0, results[0].GetCandidatePrNumber())
}

// TestANegativeHopCapIsRejected: a cap below the walk's own first step would
// silently resolve nothing, so it is refused where it is actionable.
func TestANegativeHopCapIsRejected(t *testing.T) {
	_, err := defectlineage.NewWalker(defectlineage.WalkParams{HopCap: -1})
	require.Error(t, err)
}

// TestOneCulpritReachedTwiceKeepsItsStrongestEvidence. Several anchors naming
// one culprit is ordinary for a multi-hunk fix, and a store keys a candidate on
// (fix, culprit), so they collapse to one row. Without a decision here that row
// would record whichever strength came last in diff order — so a culprit blamed
// directly by one anchor would be stored as a weaker hop result purely because
// another anchor appeared later in the file.
func TestOneCulpritReachedTwiceKeepsItsStrongestEvidence(t *testing.T) {
	src := &fakeSource{
		lines: map[string][]defectlineage.BlamedLine{
			// Anchor one: the line is absent at the parent, so this is direct.
			key("P", "a.go"):  {line("x := 1", "CULPRIT")},
			key("C0", "a.go"): {line("other", "OLDER")},
			// Anchor two reaches the same culprit, but through a reformat.
			key("P", "b.go"):  {line("    y := 2", "REFORMAT")},
			key("C1", "b.go"): {line("y := 2", "CULPRIT")},
			key("C2", "b.go"): {line("other", "OLDER")},
		},
		parents: map[string]string{"FIX": "P", "CULPRIT": "C0", "REFORMAT": "C1"},
	}
	fix := fixWith("x := 1")
	fix.LocationAnchors = append(fix.LocationAnchors, &defectlineagev1.LineageBlameAnchor{
		Path: "b.go", StartLine: 20, EndLine: 20, RemovedText: []string{"    y := 2"},
	})

	state, results, err := newWalker(t).Walk(context.Background(), src, fix)
	require.NoError(t, err)
	assert.Equal(t, defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_RESOLVED, state)
	require.Len(t, results, 1, "one culprit must yield one result, not one per anchor")
	assert.Equal(t, "CULPRIT", results[0].GetCulpritSha())
	assert.Equal(t, defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_DIRECT,
		results[0].GetEvidenceType(), "the strongest evidence wins, not the last one found")
	assert.EqualValues(t, 0, results[0].GetHopsWalked())
}

// TestTwoCulpritsAreBothKept is the other half: collapsing must be by culprit,
// not a cap of one.
func TestTwoCulpritsAreBothKept(t *testing.T) {
	src := &fakeSource{
		lines: map[string][]defectlineage.BlamedLine{
			key("P", "a.go"):  {line("x := 1", "FIRST")},
			key("C0", "a.go"): {line("other", "OLDER")},
			key("P", "b.go"):  {line("y := 2", "SECOND")},
			key("C1", "b.go"): {line("other", "OLDER")},
		},
		parents: map[string]string{"FIX": "P", "FIRST": "C0", "SECOND": "C1"},
	}
	fix := fixWith("x := 1")
	fix.LocationAnchors = append(fix.LocationAnchors, &defectlineagev1.LineageBlameAnchor{
		Path: "b.go", StartLine: 20, EndLine: 20, RemovedText: []string{"y := 2"},
	})

	_, results, err := newWalker(t).Walk(context.Background(), src, fix)
	require.NoError(t, err)
	require.Len(t, results, 2, "distinct culprits are distinct candidates")
}
