// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
)

func TestAddedLinesCarryTheirNewFileNumbers(t *testing.T) {
	diff := []byte("@@ -10,3 +10,5 @@\n" +
		" ctx := context.Background()\n" +
		"-old := 1\n" +
		"+first := 1\n" +
		"+second := 2\n" +
		" return nil\n")

	got := defectlineage.AddedLines(diff)
	require.Len(t, got, 2)
	assert.Equal(t, "first := 1", got[0].Text)
	assert.Equal(t, 11, got[0].NewLine, "after one context line at 10")
	assert.Equal(t, "second := 2", got[1].Text)
	assert.Equal(t, 12, got[1].NewLine)
}

// A removed line whose content begins with "--", or an added line beginning
// "++", is content. Counting the body down is what tells them from headers.
func TestAddedLinesReadContentNotHeaders(t *testing.T) {
	diff := []byte("@@ -1,2 +1,2 @@\n-- a dropped comment\n++ an added marker\n")
	got := defectlineage.AddedLines(diff)
	require.Len(t, got, 1)
	assert.Equal(t, "+ an added marker", got[0].Text)
	assert.Equal(t, 1, got[0].NewLine)
}

func TestAddedLinesForPathSelectsOneFile(t *testing.T) {
	diff := []byte("diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n" +
		"@@ -1,1 +1,2 @@\n x := 1\n+added_in_a := 1\n" +
		"diff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n" +
		"@@ -5,1 +5,2 @@\n y := 1\n+added_in_b := 2\n")

	a := defectlineage.AddedLinesForPath(diff, "a.go")
	require.Len(t, a, 1)
	assert.Equal(t, "added_in_a := 1", a[0].Text)
	assert.Equal(t, 2, a[0].NewLine)

	b := defectlineage.AddedLinesForPath(diff, "b.go")
	require.Len(t, b, 1)
	assert.Equal(t, "added_in_b := 2", b[0].Text)
	assert.Equal(t, 6, b[0].NewLine)

	assert.Empty(t, defectlineage.AddedLinesForPath(diff, "c.go"), "an absent path yields none")
	assert.Empty(t, defectlineage.AddedLinesForPath(diff, ""), "so does no path")
}

func TestAnAbsentCountMeansOneLine(t *testing.T) {
	got := defectlineage.AddedLines([]byte("@@ -42 +42 @@\n-old\n+new\n"))
	require.Len(t, got, 1)
	assert.Equal(t, 42, got[0].NewLine)
}

func TestAnUnparseableHeaderYieldsNothing(t *testing.T) {
	assert.Empty(t, defectlineage.AddedLines([]byte("@@ not a range @@\n+added\n")))
	assert.Empty(t, defectlineage.AddedLines([]byte("")))
}
