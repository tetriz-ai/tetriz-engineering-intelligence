// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Command trace-one-bug-fix runs the whole defect-lineage pipeline over one bug
// fix, in process, with no database and no network.
//
// The history it blames against is deliberate: the line the fix repaired was
// written by CULPRIT, then re-indented by REFORMAT. A reformat is the line's
// last writer without having authored its behaviour, so the walk must step past
// it — which is what the output shows.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/store/memory"
	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// The fix: it guards the division the old line did unguarded.
const fixDiff = `diff --git a/pkg/math.go b/pkg/math.go
--- a/pkg/math.go
+++ b/pkg/math.go
@@ -3,3 +3,6 @@ package p
 func Divide(a, b int) int {
-	return a / b
+	if b == 0 {
+		return 0
+	}
 	return a / b
 }
`

// The file as it stood just before the fix, and before the culprit.
const (
	atParent  = "package p\n\nfunc Divide(a, b int) int {\n        return a / b\n}\n"
	atCulprit = "package p\n\nfunc Divide(a, b int) int {\n\treturn a / b\n}\n"
)

// history stands in for a provider's blame.
type history struct{}

func (history) Lines(_ context.Context, rev, _ string) ([]defectlineage.BlamedLine, error) {
	committed := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	switch rev {
	case "PARENT": // the reformat re-indented the line
		return []defectlineage.BlamedLine{{
			Text: "        return a / b", OwnerSHA: "REFORMAT", CommittedAt: committed,
			AuthorName: "R Eformat", AuthorEmail: "reformat@example.com",
		}}, nil
	case "C1": // before the reformat, the culprit's own text
		return []defectlineage.BlamedLine{{
			Text: "\treturn a / b", OwnerSHA: "CULPRIT", CommittedAt: committed,
			AuthorName: "A Dev", AuthorEmail: "dev@example.com",
		}}, nil
	case "C2": // before the culprit, the line does not exist
		return []defectlineage.BlamedLine{{Text: "\treturn a", OwnerSHA: "OLDER"}}, nil
	}
	return nil, nil
}

func (history) FirstParent(_ context.Context, sha string) (string, error) {
	return map[string]string{"FIXCOMMIT": "PARENT", "REFORMAT": "C1", "CULPRIT": "C2"}[sha], nil
}

func (history) FileAt(_ context.Context, rev, _ string) ([]byte, error) {
	if rev == "CULPRIT" {
		return []byte(atCulprit), nil
	}
	return []byte(atParent), nil
}

func main() {
	ctx := context.Background()
	store := memory.New()

	ref := defectlineage.FixRef{
		OrganizationID: "acme", ConnectorID: "github",
		PlatformRepoID: "repo-1", BugFixPRID: "pr-42",
	}

	// 1. A merged bug fix needs tracing. This writes a row and calls no provider,
	//    which is what stops a burst of fixes becoming a burst of provider calls.
	if err := store.Enqueue(ctx, ref); err != nil {
		log.Fatal(err)
	}

	// The caller owns pull requests and diffs, so it supplies them.
	resolver := defectlineage.FixResolverFunc(func(context.Context, defectlineage.FixRef) (defectlineage.FixInput, error) {
		return defectlineage.FixInput{
			RepoFullName: "acme/widgets", FixCommitSHA: "FIXCOMMIT",
			Diff: []byte(fixDiff), DiffFormat: defectlineage.DiffFormatUnified,
			ChangedFiles: 1, MergedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		}, nil
	})

	// 2. Blame each region the fix removed back to the commit that wrote it.
	drain, err := defectlineage.NewDrain(defectlineage.DrainParams{Store: store, Resolver: resolver})
	if err != nil {
		log.Fatal(err)
	}
	blamed, err := drain.RunBlame(ctx, ref.OrganizationID, ref.ConnectorID, 10, history{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("blamed: claimed=%d resolved=%d\n", blamed.Claimed, blamed.Resolved)

	// 3. Rank the candidates. The comparator here answers by path alone, so the
	//    code-context term is coverage and the score says so; pass
	//    wiring.ASTComparator for a measured rung.
	pass, err := defectlineage.NewScoringPass(defectlineage.ScoringParams{Store: store, Resolver: resolver})
	if err != nil {
		log.Fatal(err)
	}
	scored, err := pass.Run(ctx, ref.OrganizationID, ref.ConnectorID, 10, history{})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("scored: candidates=%d degraded=%d fixes_resolved=%d\n",
		scored.Scored, scored.Degraded, scored.FixesResolved)

	// 4. The rows a user would query.
	candidates, err := store.ListCandidates(ctx, ref.OrganizationID, ref.BugFixPRID)
	if err != nil {
		log.Fatal(err)
	}
	for _, c := range candidates {
		fmt.Printf("\ncandidate %s by %s\n", c.CommitSHA, c.AuthorEmail)
		fmt.Printf("  reached by:  %s\n", shortEvidence(c.EvidenceType))
		fmt.Printf("  confidence:  %.3f\n", *c.ConfidenceScore)
		fmt.Printf("  context:     %s (degraded: %t)\n",
			c.ScoreEvidence.GetAstMatchLevel(), c.ScoreEvidence.GetDegraded())
		fmt.Printf("  age at fix:  %.0f days\n", c.ScoreEvidence.GetRecencyDays())
	}

	stage, outcome, _ := store.Stage(ref.OrganizationID, ref.BugFixPRID)
	fmt.Printf("\nfix %s: %s / %s\n", ref.BugFixPRID, shortStage(stage), shortOutcome(outcome))
}

func shortEvidence(e defectlineagev1.DefectLineageEvidenceType) string {
	return trimPrefix(e.String(), "DEFECT_LINEAGE_EVIDENCE_TYPE_")
}

func shortStage(s defectlineagev1.DefectLineageStage) string {
	return trimPrefix(s.String(), "DEFECT_LINEAGE_STAGE_")
}

func shortOutcome(o defectlineagev1.DefectLineageTerminalOutcome) string {
	return trimPrefix(o.String(), "DEFECT_LINEAGE_TERMINAL_OUTCOME_")
}

func trimPrefix(s, prefix string) string {
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):]
	}
	return s
}
