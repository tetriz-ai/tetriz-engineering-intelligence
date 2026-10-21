// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/store/memory"
	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

const drainDiff = `diff --git a/pkg/math.go b/pkg/math.go
--- a/pkg/math.go
+++ b/pkg/math.go
@@ -3,3 +3,6 @@ package p
 func Divide(a, b int) int {
-	return a / b
+	if b == 0 {
+		return 0
+	}
+	return a / b
 }
`

func drainRef() defectlineage.FixRef {
	return defectlineage.FixRef{
		OrganizationID: "org-1", ConnectorID: "github",
		PlatformRepoID: "repo-1", BugFixPRID: "fix-1",
	}
}

func goodInput() defectlineage.FixInput {
	return defectlineage.FixInput{
		RepoFullName: "acme/widgets", FixCommitSHA: "FIXCOMMIT",
		Diff: []byte(drainDiff), DiffFormat: defectlineage.DiffFormatUnified,
		ChangedFiles: 1, MergedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}
}

// drainSource blames the anchor straight to CULPRIT.
type drainSource struct{ err error }

func (d drainSource) Lines(_ context.Context, rev, _ string) ([]defectlineage.BlamedLine, error) {
	if d.err != nil {
		return nil, d.err
	}
	switch rev {
	case "PARENT":
		return []defectlineage.BlamedLine{{
			Text: "\treturn a / b", OwnerSHA: "CULPRIT",
			CommittedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
			AuthorName:  "A Dev", AuthorEmail: "dev@example.com",
		}}, nil
	case "EARLIER":
		return []defectlineage.BlamedLine{{Text: "\treturn a", OwnerSHA: "OLDER"}}, nil
	}
	return nil, nil
}

func (drainSource) FirstParent(_ context.Context, sha string) (string, error) {
	return map[string]string{"FIXCOMMIT": "PARENT", "CULPRIT": "EARLIER"}[sha], nil
}

func newDrain(t *testing.T, store defectlineage.Store, r defectlineage.FixResolver) *defectlineage.Drain {
	t.Helper()
	d, err := defectlineage.NewDrain(defectlineage.DrainParams{Store: store, Resolver: r})
	require.NoError(t, err)
	return d
}

func TestDrainClaimsWalksAndStores(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	ref := drainRef()
	require.NoError(t, s.Enqueue(ctx, ref))

	d := newDrain(t, s, defectlineage.FixResolverFunc(
		func(context.Context, defectlineage.FixRef) (defectlineage.FixInput, error) {
			return goodInput(), nil
		}))

	stats, err := d.RunBlame(ctx, ref.OrganizationID, ref.ConnectorID, 10, drainSource{})
	require.NoError(t, err)
	assert.Equal(t, 1, stats.Claimed)
	assert.Equal(t, 1, stats.Resolved)

	got, err := s.ListCandidates(ctx, ref.OrganizationID, ref.BugFixPRID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "CULPRIT", got[0].CommitSHA)
	assert.Nil(t, got[0].ConfidenceScore, "the drain never scores — rescoring must not force a re-blame")
}

// TestDrainSettlesAFixWithNothingToBlame: an oversized change and a diff with no
// removal are both answers, and settling them before any provider call is the
// point of the pre-check.
func TestDrainSettlesAFixWithNothingToBlame(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name  string
		input defectlineage.FixInput
	}{
		{"oversized", func() defectlineage.FixInput {
			in := goodInput()
			in.ChangedFiles = 10_000
			return in
		}()},
		{"nothing removed", func() defectlineage.FixInput {
			in := goodInput()
			in.Diff = []byte("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,1 +1,2 @@\n x := 1\n+guard()\n")
			return in
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := memory.New()
			ref := drainRef()
			require.NoError(t, s.Enqueue(ctx, ref))

			d := newDrain(t, s, defectlineage.FixResolverFunc(
				func(context.Context, defectlineage.FixRef) (defectlineage.FixInput, error) {
					return tc.input, nil
				}))
			stats, err := d.RunBlame(ctx, ref.OrganizationID, ref.ConnectorID, 10, drainSource{})
			require.NoError(t, err)
			assert.Equal(t, 1, stats.Settled)
			assert.Zero(t, stats.Resolved)

			stage, _, _ := s.Stage(ref.OrganizationID, ref.BugFixPRID)
			assert.Equal(t, defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED, stage,
				"it is terminal without a provider call")
		})
	}
}

// TestDrainLeavesATransientFailureForALaterPass. A provider blip must not bury
// the fix: the claim lapses and the next pass retries it.
func TestDrainLeavesATransientFailureForALaterPass(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	ref := drainRef()
	require.NoError(t, s.Enqueue(ctx, ref))

	d := newDrain(t, s, defectlineage.FixResolverFunc(
		func(context.Context, defectlineage.FixRef) (defectlineage.FixInput, error) {
			return goodInput(), nil
		}))
	stats, err := d.RunBlame(ctx, ref.OrganizationID, ref.ConnectorID, 10, drainSource{err: errors.New("provider down")})
	require.NoError(t, err, "one fix's failure never fails the pass")
	assert.Equal(t, 1, stats.Failed)

	stage, _, _ := s.Stage(ref.OrganizationID, ref.BugFixPRID)
	assert.NotEqual(t, defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED, stage,
		"a transient failure must not be recorded as terminal")

	// A later pass, once the lease lapsed, succeeds.
	again, err := s.ClaimPendingBlame(ctx, ref.OrganizationID, ref.ConnectorID, 10, 0)
	require.NoError(t, err)
	assert.Len(t, again, 1, "the fix is retried")
}

// TestDrainSettlesAPermanentlyMissingInput: ErrNoFixInput is documented as
// permanent, so re-offering the fix forever would be the wrong answer.
func TestDrainSettlesAPermanentlyMissingInput(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	ref := drainRef()
	require.NoError(t, s.Enqueue(ctx, ref))

	d := newDrain(t, s, defectlineage.FixResolverFunc(
		func(context.Context, defectlineage.FixRef) (defectlineage.FixInput, error) {
			return defectlineage.FixInput{}, defectlineage.ErrNoFixInput
		}))
	stats, err := d.RunBlame(ctx, ref.OrganizationID, ref.ConnectorID, 10, drainSource{})
	require.NoError(t, err)
	assert.Equal(t, 1, stats.Settled)

	stage, _, _ := s.Stage(ref.OrganizationID, ref.BugFixPRID)
	assert.Equal(t, defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_RESOLVED, stage)
}

func TestNewDrainRequiresItsDependencies(t *testing.T) {
	_, err := defectlineage.NewDrain(defectlineage.DrainParams{})
	require.Error(t, err, "a drain with no store can only fail at the first fix")

	_, err = defectlineage.NewDrain(defectlineage.DrainParams{Store: memory.New()})
	require.Error(t, err, "and one with no resolver cannot build a claim")
}

// timestamp is the protobuf form of t, for the test fixtures above.
func timestamp(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t) }
