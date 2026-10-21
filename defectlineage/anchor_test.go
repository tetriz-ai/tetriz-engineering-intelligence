// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage_test

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
)

func TestARemovalBecomesABlameableAnchor(t *testing.T) {
	diff := "diff --git a/pkg/a.go b/pkg/a.go\n" +
		"--- a/pkg/a.go\n+++ b/pkg/a.go\n" +
		"@@ -10,3 +10,3 @@\n" +
		" ctx := context.Background()\n" +
		"-x := 1\n" +
		"+x := 2\n" +
		" return x\n"

	got := defectlineage.ExtractUnified([]byte(diff), defectlineage.Options{})
	require.Len(t, got, 1)
	a := got[0]
	assert.Equal(t, "pkg/a.go", a.OldPath)
	assert.Equal(t, 10, a.OldStartLine)
	assert.Equal(t, []string{"x := 1"}, a.RemovedText)
	assert.Equal(t, 1, a.RemovedLines)
	assert.True(t, a.Blameable())
	assert.Equal(t, defectlineage.KindRemoval, a.Kind)
	assert.Equal(t, 11, a.RemovedStartLine, "the removed line sits after one context line")
	assert.Equal(t, 11, a.RemovedEndLine)
}

// TestAnAdditionOnlyHunkIsNotBlameable: there is no prior line to blame, so the
// hunk is evidence a fix happened, not evidence of where the defect came from.
func TestAnAdditionOnlyHunkIsNotBlameable(t *testing.T) {
	diff := "diff --git a/pkg/a.go b/pkg/a.go\n--- a/pkg/a.go\n+++ b/pkg/a.go\n" +
		"@@ -5,1 +5,2 @@\n x := 1\n+guard()\n"
	assert.Empty(t, defectlineage.ExtractUnified([]byte(diff), defectlineage.Options{}),
		"an added guard names no origin")
}

// TestATestFileIsNotBlameable: a test states the intended behaviour, so it is
// evidence that a fix happened and never the origin of the defect.
func TestATestFileIsNotBlameable(t *testing.T) {
	diff := "diff --git a/pkg/a_test.go b/pkg/a_test.go\n--- a/pkg/a_test.go\n+++ b/pkg/a_test.go\n" +
		"@@ -1,2 +1,2 @@\n-assert.Equal(t, 1, x)\n+assert.Equal(t, 2, x)\n"
	assert.Empty(t, defectlineage.ExtractUnified([]byte(diff), defectlineage.Options{}))
}

// A removed SQL comment must not be read as a file header.
func TestASQLCommentIsNotMistakenForAFileHeader(t *testing.T) {
	diff := "diff --git a/db/x.sql b/db/x.sql\n--- a/db/x.sql\n+++ b/db/x.sql\n" +
		"@@ -1,2 +1,2 @@\n" +
		"-- a dropped comment\n" +
		"+SELECT 2;\n"

	got := defectlineage.ExtractUnified([]byte(diff), defectlineage.Options{})
	require.Len(t, got, 1, "the hunk must still produce its anchor")
	assert.Equal(t, "db/x.sql", got[0].OldPath, "the path must not come from the comment text")
	assert.Equal(t, []string{"- a dropped comment"}, got[0].RemovedText)
}

// TestAnOverDeclaredHunkResynchronises covers a truncated stored diff. Without a
// resynchronisation point an over-declared count eats the next file's headers,
// drops its real hunk, and keeps the garbage on this one.
func TestAnOverDeclaredHunkResynchronises(t *testing.T) {
	diff := "diff --git a/pkg/a.go b/pkg/a.go\n--- a/pkg/a.go\n+++ b/pkg/a.go\n" +
		"@@ -1,99 +1,99 @@\n" + // claims 99 lines; supplies one
		"-truncated\n" +
		"diff --git a/pkg/b.go b/pkg/b.go\n--- a/pkg/b.go\n+++ b/pkg/b.go\n" +
		"@@ -7,2 +7,2 @@\n-y := 1\n+y := 2\n"

	got := defectlineage.ExtractUnified([]byte(diff), defectlineage.Options{})
	require.Len(t, got, 2, "the second file must survive the first's bad count")
	assert.Equal(t, "pkg/a.go", got[0].OldPath)
	assert.Equal(t, "pkg/b.go", got[1].OldPath)
	assert.Equal(t, []string{"y := 1"}, got[1].RemovedText)
}

// TestAMissingOldPathHeaderDoesNotInheritThePreviousFile: clearing the path at
// the section separator is what stops a file's hunks being attributed to the
// file before it.
func TestAMissingOldPathHeaderDoesNotInheritThePreviousFile(t *testing.T) {
	diff := "diff --git a/pkg/a.go b/pkg/a.go\n--- a/pkg/a.go\n+++ b/pkg/a.go\n" +
		"@@ -1,2 +1,2 @@\n-x := 1\n+x := 2\n" +
		"diff --git a/pkg/b.go b/pkg/b.go\n" + // no --- line
		"@@ -5,2 +5,2 @@\n-y := 1\n+y := 2\n"

	got := defectlineage.ExtractUnified([]byte(diff), defectlineage.Options{})
	for _, a := range got {
		assert.NotEqual(t, 5, a.OldStartLine, "b.go's hunk must not be filed under a.go")
	}
}

// The removed range stops at the first context line.
func TestTheRemovedRangeCoversOnlyTheFirstContiguousRun(t *testing.T) {
	diff := "diff --git a/pkg/a.go b/pkg/a.go\n--- a/pkg/a.go\n+++ b/pkg/a.go\n" +
		"@@ -10,7 +10,7 @@\n" +
		"-first\n" +
		"-second\n" +
		" context\n" +
		" more context\n" +
		"-far away\n" +
		"+replacement\n" +
		" tail\n"

	got := defectlineage.ExtractUnified([]byte(diff), defectlineage.Options{})
	require.Len(t, got, 1)
	assert.Equal(t, 10, got[0].RemovedStartLine)
	assert.Equal(t, 11, got[0].RemovedEndLine, "the run stops at the context line")
	assert.Equal(t, 3, got[0].RemovedLines, "every removed line still counts toward the size")
}

// TestAnAbsentHunkCountMeansOneLine: git omits ",count" when it is 1, and
// reading the absent count as zero would produce an empty range matching nothing.
func TestAnAbsentHunkCountMeansOneLine(t *testing.T) {
	diff := "diff --git a/pkg/a.go b/pkg/a.go\n--- a/pkg/a.go\n+++ b/pkg/a.go\n" +
		"@@ -42 +42 @@\n-x := 1\n+x := 2\n"
	got := defectlineage.ExtractUnified([]byte(diff), defectlineage.Options{})
	require.Len(t, got, 1)
	assert.Equal(t, 42, got[0].OldStartLine)
	assert.Equal(t, 1, got[0].OldLineCount)
}

// TestANegativeHunkCountYieldsNoAnchor: accepting one lets the body-length
// countdown be satisfied at the wrong moment, which is how a bad header takes
// the rest of the patch with it.
func TestANegativeHunkCountYieldsNoAnchor(t *testing.T) {
	diff := "diff --git a/pkg/a.go b/pkg/a.go\n--- a/pkg/a.go\n+++ b/pkg/a.go\n" +
		"@@ -1,-3 +1,2 @@\n-x := 1\n+x := 2\n"
	assert.Empty(t, defectlineage.ExtractUnified([]byte(diff), defectlineage.Options{}))
}

// TestAGeneratedPathIsExcluded keeps regenerated content out before it costs a
// provider call.
func TestAGeneratedPathIsExcluded(t *testing.T) {
	diff := "diff --git a/vendor/x/a.go b/vendor/x/a.go\n--- a/vendor/x/a.go\n+++ b/vendor/x/a.go\n" +
		"@@ -1,2 +1,2 @@\n-x := 1\n+x := 2\n"
	assert.Empty(t, defectlineage.ExtractUnified([]byte(diff), defectlineage.Options{}),
		"a vendored path names no culprit")
}

// TestACallerGlobExcludesAPath layers the caller's own exclusions on top of the
// always-on filter.
func TestACallerGlobExcludesAPath(t *testing.T) {
	diff := "diff --git a/internal/x.go b/internal/x.go\n--- a/internal/x.go\n+++ b/internal/x.go\n" +
		"@@ -1,2 +1,2 @@\n-x := 1\n+x := 2\n"
	opts := defectlineage.Options{ExcludePatterns: []string{"internal/**"}}
	assert.Empty(t, defectlineage.ExtractUnified([]byte(diff), opts))
	assert.Len(t, defectlineage.ExtractUnified([]byte(diff), defectlineage.Options{}), 1,
		"and the same diff is kept without the glob")
}

// TestAdjacentHunksOnOneLineAreDeduplicated: adjacent hunks share context lines
// and routinely address the same line, and blaming it twice would count one
// piece of evidence twice.
func TestAdjacentHunksOnOneLineAreDeduplicated(t *testing.T) {
	diff := "diff --git a/pkg/a.go b/pkg/a.go\n--- a/pkg/a.go\n+++ b/pkg/a.go\n" +
		"@@ -10,2 +10,2 @@\n-x := 1\n+x := 2\n" +
		"@@ -10,2 +10,2 @@\n-x := 1\n+x := 3\n"
	assert.Len(t, defectlineage.ExtractUnified([]byte(diff), defectlineage.Options{}), 1)
}

// TestTheAnchorCapBounds one fix's provider budget.
func TestTheAnchorCapBounds(t *testing.T) {
	diff := "diff --git a/pkg/a.go b/pkg/a.go\n--- a/pkg/a.go\n+++ b/pkg/a.go\n"
	for i := 1; i <= 10; i++ {
		diff += "@@ -" + strconv.Itoa(i*10) + ",2 +" + strconv.Itoa(i*10) + ",2 @@\n-x := 1\n+x := 2\n"
	}
	got := defectlineage.ExtractUnified([]byte(diff), defectlineage.Options{MaxAnchors: 3})
	assert.Len(t, got, 3)
}

// TestGitLabDiffsAreRead covers the other stored shape: a JSON array whose
// entries carry the path, with a header-less unified body.
func TestGitLabDiffsAreRead(t *testing.T) {
	raw := `[{"old_path":"pkg/a.go","new_path":"pkg/a.go","diff":"@@ -3,2 +3,2 @@\n-x := 1\n+x := 2\n"}]`
	got := defectlineage.ExtractGitLab([]byte(raw), defectlineage.Options{})
	require.Len(t, got, 1)
	assert.Equal(t, "pkg/a.go", got[0].OldPath)
	assert.Equal(t, []string{"x := 1"}, got[0].RemovedText)
}

// TestADeletedGitLabFileIsSkipped: every line "changed", which names the file's
// own history rather than the defect.
func TestADeletedGitLabFileIsSkipped(t *testing.T) {
	raw := `[{"old_path":"pkg/a.go","new_path":"pkg/a.go","deleted_file":true,"diff":"@@ -1,2 +1,0 @@\n-x := 1\n-y := 2\n"}]`
	assert.Empty(t, defectlineage.ExtractGitLab([]byte(raw), defectlineage.Options{}))
}

// TestMalformedGitLabJSONYieldsNoAnchors: one bad stored diff must not take down
// the batch it arrived in.
func TestMalformedGitLabJSONYieldsNoAnchors(t *testing.T) {
	assert.Empty(t, defectlineage.ExtractGitLab([]byte("{not json"), defectlineage.Options{}))
}
