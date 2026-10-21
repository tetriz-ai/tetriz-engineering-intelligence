// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/store/memory"
)

// spyComparator records that it was asked, and at which candidate region.
type spyComparator struct {
	calls    int
	candLine int
}

func (s *spyComparator) Compare(
	_ context.Context, _ defectlineage.ChangedRegion, _ []byte,
	cand defectlineage.ChangedRegion, _ []byte,
) (defectlineage.Verdict, error) {
	s.calls++
	s.candLine = cand.StartLine
	return defectlineage.Verdict{Level: defectlineage.MatchExactNode}, nil
}

// scoringSource is a scripted blame history that also answers the culprit's own
// added lines, so it satisfies both ScoringProvider and CulpritLineResolver.
type scoringSource struct {
	files   map[string]map[string][]defectlineage.BlamedLine
	parents map[string]string
	added   map[string][]defectlineage.AddedLine
}

func (s scoringSource) Lines(_ context.Context, rev, path string) ([]defectlineage.BlamedLine, error) {
	return s.files[rev][path], nil
}

func (s scoringSource) FirstParent(_ context.Context, sha string) (string, error) {
	return s.parents[sha], nil
}

func (s scoringSource) FileAt(_ context.Context, rev, path string) ([]byte, error) {
	var b strings.Builder
	for _, l := range s.files[rev][path] {
		b.WriteString(l.Text + "\n")
	}
	return []byte(b.String()), nil
}

func (s scoringSource) CulpritAddedLines(_ context.Context, sha, _ string) ([]defectlineage.AddedLine, error) {
	return s.added[sha], nil
}

// providerWithoutLines answers the required half of ScoringProvider and nothing
// more, so it does not satisfy CulpritLineResolver.
type providerWithoutLines struct{ inner scoringSource }

func (p providerWithoutLines) Lines(ctx context.Context, rev, path string) ([]defectlineage.BlamedLine, error) {
	return p.inner.Lines(ctx, rev, path)
}

func (p providerWithoutLines) FirstParent(ctx context.Context, sha string) (string, error) {
	return p.inner.FirstParent(ctx, sha)
}

func (p providerWithoutLines) FileAt(ctx context.Context, rev, path string) ([]byte, error) {
	return p.inner.FileAt(ctx, rev, path)
}

const fixDiff = "diff --git a/m.go b/m.go\n--- a/m.go\n+++ b/m.go\n" +
	"@@ -2,1 +2,1 @@\n-return a / b\n+return safe(a, b)\n"

func newScoringSource() scoringSource {
	return scoringSource{
		files: map[string]map[string][]defectlineage.BlamedLine{
			"PARENT": {"m.go": {
				{Text: "package p", OwnerSHA: "OLD"},
				{Text: "return a / b", OwnerSHA: "CULPRIT"},
			}},
			"CULPRIT": {"m.go": {
				{Text: "package p", OwnerSHA: "OLD"},
				{Text: "func unrelated() {}", OwnerSHA: "CULPRIT"},
				{Text: "return a / b", OwnerSHA: "CULPRIT"},
			}},
			"CP": {"m.go": {{Text: "package p", OwnerSHA: "OLD"}}},
		},
		parents: map[string]string{"FIX": "PARENT", "CULPRIT": "CP"},
		// The culprit touched two places. The first added line is the unrelated
		// one, so taking it would measure the wrong region.
		added: map[string][]defectlineage.AddedLine{"CULPRIT": {
			{Text: "func unrelated() {}", NewLine: 2},
			{Text: "return a / b", NewLine: 3},
		}},
	}
}

func runPass(t *testing.T, provider defectlineage.ScoringProvider, cmp defectlineage.Comparator) defectlineage.ScoringStats {
	t.Helper()
	ctx := context.Background()
	store := memory.New()
	ref := defectlineage.FixRef{OrganizationID: "o", ConnectorID: "c", PlatformRepoID: "r", BugFixPRID: "f1"}
	require.NoError(t, store.Enqueue(ctx, ref))

	resolver := defectlineage.FixResolverFunc(func(context.Context, defectlineage.FixRef) (defectlineage.FixInput, error) {
		return defectlineage.FixInput{
			FixCommitSHA: "FIX", Diff: []byte(fixDiff),
			DiffFormat: defectlineage.DiffFormatUnified, MergedAt: time.Now(),
		}, nil
	})

	drain, err := defectlineage.NewDrain(defectlineage.DrainParams{Store: store, Resolver: resolver})
	require.NoError(t, err)
	src, ok := provider.(defectlineage.Source)
	require.True(t, ok)
	bs, err := drain.RunBlame(ctx, "o", "c", 10, src)
	require.NoError(t, err)
	require.Equal(t, 1, bs.Resolved, "the walk must name a culprit for this to test anything")

	pass, err := defectlineage.NewScoringPass(defectlineage.ScoringParams{
		Store: store, Resolver: resolver, Comparator: cmp,
	})
	require.NoError(t, err)
	stats, err := pass.Run(ctx, "o", "c", 10, provider)
	require.NoError(t, err)
	return stats
}

// TestTheComparatorIsAskedWhenTheProviderCanRecoverTheLine: the walk leaves the
// culprit line at 0 on purpose, so without a way to recover it the scoring pass
// degraded every candidate and never called the Comparator it was handed — a
// caller who supplied one got the same answer as a caller who did not.
func TestTheComparatorIsAskedWhenTheProviderCanRecoverTheLine(t *testing.T) {
	spy := &spyComparator{}
	stats := runPass(t, newScoringSource(), spy)

	assert.Positive(t, spy.calls, "the comparator must be asked")
	assert.Equal(t, 1, stats.Scored)
	assert.Zero(t, stats.Degraded, "a measured verdict is not degraded")

	// The culprit added two lines; the second is the one the fix removed. Taking
	// the first would measure an unrelated function in the same commit.
	assert.Equal(t, 3, spy.candLine,
		"the line matching the removed text is measured, not merely the first added line")
}

// TestAProviderThatCannotRecoverTheLineStillDegrades: the lookup is optional, so
// a provider that does not implement it keeps the previous behaviour rather than
// failing.
func TestAProviderThatCannotRecoverTheLineStillDegrades(t *testing.T) {
	spy := &spyComparator{}
	stats := runPass(t, providerWithoutLines{inner: newScoringSource()}, spy)

	assert.Zero(t, spy.calls, "nothing to measure, so nothing is asked")
	assert.Equal(t, 1, stats.Scored)
	assert.Equal(t, 1, stats.Degraded)
}
