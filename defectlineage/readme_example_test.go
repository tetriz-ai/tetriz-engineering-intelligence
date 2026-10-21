// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage_test

import (
	"context"
	"fmt"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	dlv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// readmeHistory is the README quick start's stand-in provider, kept here so the
// example in the README is executed rather than only read.
type readmeHistory struct{}

func (readmeHistory) Lines(_ context.Context, rev, _ string) ([]defectlineage.BlamedLine, error) {
	switch rev {
	case "PARENT":
		return []defectlineage.BlamedLine{{Text: "return a / b", OwnerSHA: "CULPRIT"}}, nil
	case "EARLIER":
		return []defectlineage.BlamedLine{{Text: "return a", OwnerSHA: "OLDER"}}, nil
	}
	return nil, nil
}

func (readmeHistory) FirstParent(_ context.Context, sha string) (string, error) {
	return map[string]string{"FIX": "PARENT", "CULPRIT": "EARLIER"}[sha], nil
}

// Example_quickStart is the README's quick start. Its output is the README's
// output, so a change that breaks one breaks the other.
func Example_quickStart() {
	diff := []byte("diff --git a/m.go b/m.go\n--- a/m.go\n+++ b/m.go\n" +
		"@@ -3,1 +3,4 @@\n-return a / b\n+if b == 0 {\n+\treturn 0\n+}\n+return a / b\n")

	anchors := defectlineage.ExtractUnified(diff, defectlineage.Options{})
	fix := &dlv1.PendingLineageBlame{FixCommitSha: "FIX"}
	for _, a := range anchors {
		fix.LocationAnchors = append(fix.LocationAnchors, &dlv1.LineageBlameAnchor{
			Path: a.OldPath, StartLine: int32(a.OldStartLine), RemovedText: a.RemovedText,
		})
	}

	walker, _ := defectlineage.NewWalker(defectlineage.WalkParams{})
	state, results, err := walker.Walk(context.Background(), readmeHistory{}, fix)
	if err != nil {
		panic(err)
	}
	fmt.Println(state)
	for _, r := range results {
		fmt.Println(r.GetCulpritSha(), r.GetEvidenceType())
	}
	// Output:
	// DEFECT_LINEAGE_STATE_RESOLVED
	// CULPRIT DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_DIRECT
}
