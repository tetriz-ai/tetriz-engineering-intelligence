// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

//go:build astcompare

// The measured syntax-tree rung is opt-in, so the test that asserts one builds
// with the tag that supplies it. Without the tag the scorer answers by file
// coverage, which this test is not about.

package wiring_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	connv1 "github.com/tetriz-ai/tetriz-connectors/gen/lineage/v1"
	connsdk "github.com/tetriz-ai/tetriz-connectors/sdk"
	connlineage "github.com/tetriz-ai/tetriz-connectors/sdk/lineage"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/astcompare"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/store/postgres"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/wiring"
)

const (
	dsnEnv      = "DEFECTLINEAGE_TEST_DSN"
	orgID       = "00000000-0000-4000-8000-0000000000ff"
	connectorID = "github"
	repoID      = "repo-e2e"
)

// The file the fix repaired. The bug was introduced by CULPRIT, carried
// unchanged through a whitespace-only reformat, and repaired by the fix.
const fileAtAnchor = `package p

func Divide(a, b int) int {
	return a / b
}
`

func openStore(t *testing.T) (*postgres.Store, *sql.DB) {
	t.Helper()
	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		t.Skipf("set %s to run the host-contract tests", dsnEnv)
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })

	schema, err := os.ReadFile("../store/postgres/schema.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(schema))
	require.NoError(t, err)

	s, err := postgres.New(db)
	require.NoError(t, err)
	return s, db
}

// fakeProvider is the provider half: a blame answer per revision and a
// first-parent chain, standing in for a real VCS.
type fakeProvider struct{}

func (fakeProvider) Lines(_ context.Context, rev, path string) ([]connlineage.BlamedLine, error) {
	owner := map[string]string{"PARENT": "REFORMAT", "C1": "CULPRIT", "C0": "OLDER"}[rev]
	if owner == "" {
		return nil, nil
	}
	if rev == "C0" {
		// Before the culprit the line simply is not there, which is what makes
		// CULPRIT the commit that introduced it.
		return []connlineage.BlamedLine{{Text: "	return a", OwnerSHA: "OLDER"}}, nil
	}
	text := "	return a / b"
	if rev == "PARENT" {
		// The reformat only re-indented the line, so it is the last writer without
		// having authored the behaviour.
		text = "        return a / b"
	}
	return []connlineage.BlamedLine{{
		Text: text, OwnerSHA: owner,
		CommittedAt: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		AuthorName:  "A Dev", AuthorEmail: "dev@example.com",
	}}, nil
}

func (fakeProvider) FirstParent(_ context.Context, sha string) (string, error) {
	return map[string]string{"FIXCOMMIT": "PARENT", "REFORMAT": "C1", "CULPRIT": "C0"}[sha], nil
}

const culpritDiff = "@@ -3,1 +3,2 @@\n func Divide(a, b int) int {\n+\treturn a / b\n"

const fixDiff = `diff --git a/pkg/math.go b/pkg/math.go
--- a/pkg/math.go
+++ b/pkg/math.go
@@ -3,3 +3,6 @@ package p
 func Divide(a, b int) int {
-        return a / b
+	if b == 0 {
+		return 0
+	}
+	return a / b
 }
`

// TestTheEngineSatisfiesTheConnectorHostContract drives the whole pipeline
// through the interfaces tetriz-connectors declares, against a real database.
//
// Nothing here calls the engine's own API: every step goes through
// sdk/lineage.BlameSink and sdk/lineage.Walker, which is what a connector run
// would do.
func TestTheEngineSatisfiesTheConnectorHostContract(t *testing.T) {
	ctx := context.Background()
	store, db := openStore(t)

	var fixID string
	require.NoError(t, db.QueryRow("SELECT gen_random_uuid()").Scan(&fixID))
	ref := defectlineage.FixRef{
		OrganizationID: orgID, ConnectorID: connectorID,
		PlatformRepoID: repoID, BugFixPRID: fixID,
	}
	require.NoError(t, store.Enqueue(ctx, ref))

	resolver := defectlineage.FixResolverFunc(func(_ context.Context, r defectlineage.FixRef) (defectlineage.FixInput, error) {
		return defectlineage.FixInput{
			RepoFullName: "acme/widgets",
			FixCommitSHA: "FIXCOMMIT",
			Diff:         []byte(fixDiff),
			DiffFormat:   defectlineage.DiffFormatUnified,
			ChangedFiles: 1,
			MergedAt:     time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		}, nil
	})

	sink, err := wiring.NewBlameSink(wiring.BlameSinkParams{Store: store, Resolver: resolver})
	require.NoError(t, err)
	walker, err := wiring.NewWalker(nil)
	require.NoError(t, err)

	// The connector framework refuses to run an operation whose host service is
	// nil, so proving these three fields accept our types is the contract.
	host := connsdk.HostServices{
		BlameSink:     sink,
		LineageWalker: walker,
		DefectScorer:  wiring.NewScorer(wiring.ScorerParams{Comparator: astcompare.Comparator{}}),
	}
	require.True(t, host.Has(connsdk.HostBlameSink), "the sink must register as a host service")
	require.True(t, host.Has(connsdk.HostLineageWalker), "the walker must register as a host service")
	require.True(t, host.Has(connsdk.HostDefectScorer), "and so must the scorer")
	assert.ElementsMatch(t,
		[]connsdk.HostService{connsdk.HostBlameSink, connsdk.HostLineageWalker, connsdk.HostDefectScorer},
		host.Provided(), "every lineage host service the framework asks for is supplied")
	assert.Empty(t, connsdk.HostServices{}.Missing(nil))

	// 1. The connector claims its work.
	claimed, err := host.BlameSink.ListPendingLineageBlame(ctx, &connv1.ListPendingLineageBlameRequest{
		OrganizationId: orgID, ConnectorId: connectorID, Limit: 10,
	})
	require.NoError(t, err)
	pending := findPending(claimed.GetPending(), fixID)
	require.NotNil(t, pending, "the enqueued fix must be claimable through the contract")
	require.NotEmpty(t, pending.GetLocationAnchors(), "a claimed fix always carries anchors")
	assert.Equal(t, "pkg/math.go", pending.GetLocationAnchors()[0].GetPath())
	assert.Equal(t, "FIXCOMMIT", pending.GetFixCommitSha())

	// 2. The connector walks it against its provider.
	state, results, err := host.LineageWalker.Walk(ctx, fakeProvider{}, pending)
	require.NoError(t, err)
	require.Equal(t, connv1.DefectLineageState_DEFECT_LINEAGE_STATE_RESOLVED, state)
	require.Len(t, results, 1)
	assert.Equal(t, "CULPRIT", results[0].GetCulpritSha(),
		"the whitespace-only reformat must not be blamed")
	// tetriz-connectors reserves 1, 2 and 4 in this enum, so BLAME_AFTER_HOPS has
	// no name on its side and the value is asserted by number. The numbering is
	// shared, so the value still round-trips; what a connectors-side reader
	// cannot do is name it. See the note in the pull request that added this.
	const blameAfterHops = connv1.DefectLineageEvidenceType(2)
	assert.Equal(t, blameAfterHops, results[0].GetEvidenceType(),
		"the engine reports how it reached the culprit, in the shared numbering")

	// 3. The connector resolves the culprit's pull request, best effort.
	host.LineageWalker.ResolveCandidatePRs(ctx, results, func(_ context.Context, sha string) (int32, bool) {
		if sha == "CULPRIT" {
			return 21, true
		}
		return 0, false
	})
	assert.EqualValues(t, 21, results[0].GetCandidatePrNumber())

	// 4. The connector submits the outcome.
	_, err = host.BlameSink.SubmitLineageBlame(ctx, &connv1.SubmitLineageBlameRequest{
		OrganizationId: orgID, BugFixPrId: fixID, Status: state, Results: results,
	})
	require.NoError(t, err)

	// 5. The connector claims the unscored candidate for structural scoring.
	scoring, err := host.BlameSink.ListPendingStructuralScoring(ctx, &connv1.ListPendingStructuralScoringRequest{
		OrganizationId: orgID, ConnectorId: connectorID, Limit: 10,
	})
	require.NoError(t, err)
	group := findScoring(scoring.GetPending(), "acme/widgets")
	require.NotNil(t, group, "a stored candidate must appear for scoring")
	require.NotEmpty(t, group.GetFixAnchors(), "the fix's own regions ride along")
	require.NotEmpty(t, group.GetCandidates())

	// 6. The connector hands the fix to the host scorer, which measures the
	//    proximity and submits the verdict through the sink itself.
	fetch := func(_ context.Context, _, path string) ([]byte, string, error) {
		return []byte(fileAtAnchor), "blob-" + path, nil
	}
	firstParent := func(_ context.Context, sha string) (string, error) {
		return map[string]string{"FIXCOMMIT": "PARENT", "CULPRIT": "C1"}[sha], nil
	}
	// The culprit's own diff is where its line comes from when blame reported
	// only the queried revision's position.
	culpritLines := func(_ context.Context, _, _ string) ([]connlineage.AddedLine, error) {
		return host.DefectScorer.AddedLinesFromSingleFileDiff(
			[]byte(culpritDiff)), nil
	}
	host.DefectScorer.ScorePendingFix(ctx, host.BlameSink, nil, connectorID, orgID,
		fetch, firstParent, culpritLines, group)

	// 7. The row a user queries now carries the culprit and its score.
	got, err := store.ListCandidates(ctx, orgID, fixID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	c := got[0]
	assert.Equal(t, "CULPRIT", c.CommitSHA)
	assert.Equal(t, "dev@example.com", c.AuthorEmail)
	assert.EqualValues(t, 21, c.CandidatePRNumber)
	require.NotNil(t, c.ConfidenceScore)
	assert.Greater(t, *c.ConfidenceScore, 0.0)
	assert.LessOrEqual(t, *c.ConfidenceScore, 1.0, "a confidence stays in [0,1]")
	require.NotNil(t, c.ScoreEvidence)
	assert.Equal(t, "exact_node", c.ScoreEvidence.GetAstMatchLevel())
	assert.False(t, c.ScoreEvidence.GetDegraded(), "a measured rung is not degraded")
	assert.InDelta(t, 31.0, c.ScoreEvidence.GetRecencyDays(), 1.0,
		"the recency gap is the culprit commit to the fix merge")

	// 8. The fix reached a terminal state, which is what stops it being re-offered.
	var stage, outcome string
	require.NoError(t, db.QueryRow(
		`SELECT stage, COALESCE(terminal_outcome, '') FROM defect_lineage_pipeline_state
		 WHERE organization_id = $1 AND bug_fix_pr_id = $2 AND deleted_at IS NULL`,
		orgID, fixID).Scan(&stage, &outcome))
	assert.Equal(t, "DEFECT_LINEAGE_STAGE_RESOLVED", stage)
	assert.Equal(t, "DEFECT_LINEAGE_TERMINAL_OUTCOME_SCORED_OK", outcome)
}

func findPending(ps []*connv1.PendingLineageBlame, fixID string) *connv1.PendingLineageBlame {
	for _, p := range ps {
		if p.GetBugFixPrId() == fixID {
			return p
		}
	}
	return nil
}

func findScoring(ps []*connv1.PendingStructuralScoring, repo string) *connv1.PendingStructuralScoring {
	for _, p := range ps {
		if p.GetRepoFullName() == repo {
			return p
		}
	}
	return nil
}
