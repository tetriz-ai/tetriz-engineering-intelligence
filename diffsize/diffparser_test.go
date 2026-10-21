// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package diffsize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseUnifiedDiff(t *testing.T) {
	tests := []struct {
		name      string
		diff      string
		wantFiles []FileDiff
	}{
		{
			name: "single file with additions only",
			diff: `diff --git a/main.go b/main.go
index abc..def 100644
--- a/main.go
+++ b/main.go
@@ -1,3 +1,5 @@
 package main

+// NewHandler creates an HTTP handler.
+func NewHandler() {}
+
 func main() {}
`,
			wantFiles: []FileDiff{
				{
					Path:       "main.go",
					NewContent: "// NewHandler creates an HTTP handler.\nfunc NewHandler() {}\n\n",
					Additions:  3,
					Deletions:  0,
				},
			},
		},
		{
			name: "multiple files",
			diff: `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,1 +1,2 @@
 package main
+var x = 1
diff --git a/b.go b/b.go
--- a/b.go
+++ b/b.go
@@ -1,2 +1,1 @@
-package old
 package main
`,
			wantFiles: []FileDiff{
				{Path: "a.go", NewContent: "var x = 1\n", Additions: 1, Deletions: 0},
				{Path: "b.go", NewContent: "", Additions: 0, Deletions: 1},
			},
		},
		{
			name: "new file (--- /dev/null)",
			diff: `diff --git a/new.go b/new.go
new file mode 100644
--- /dev/null
+++ b/new.go
@@ -0,0 +1,2 @@
+package new
+var x = 1
`,
			wantFiles: []FileDiff{
				{Path: "new.go", NewContent: "package new\nvar x = 1\n", Additions: 2, Deletions: 0},
			},
		},
		{
			name: "deleted file (+++ /dev/null) is skipped",
			diff: `diff --git a/gone.go b/gone.go
deleted file mode 100644
--- a/gone.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package gone
-var x = 1
`,
			wantFiles: nil,
		},
		{
			name: "deleted followed by new file keeps only the new file",
			diff: `diff --git a/gone.go b/gone.go
--- a/gone.go
+++ /dev/null
@@ -1,1 +0,0 @@
-package gone
diff --git a/kept.go b/kept.go
--- /dev/null
+++ b/kept.go
@@ -0,0 +1,1 @@
+package kept
`,
			wantFiles: []FileDiff{
				{Path: "kept.go", NewContent: "package kept\n", Additions: 1, Deletions: 0},
			},
		},
		{
			// A diff with `diff --git`, `---` and `+++` headers but no `index`
			// line and no hunk context. The `+++ b/X` line is what starts a file.
			name: "per-file patch synthesis (no `index` line, no hunk context)",
			diff: `diff --git a/src/big.go b/src/big.go
--- a/src/big.go
+++ b/src/big.go
@@ -1,3 +1,5 @@
+added1
+added2
diff --git a/src/new.go b/src/new.go
--- a/src/new.go
+++ b/src/new.go
@@ -0,0 +1,2 @@
+new1
+new2
`,
			wantFiles: []FileDiff{
				{Path: "src/big.go", NewContent: "added1\nadded2\n", Additions: 2, Deletions: 0},
				{Path: "src/new.go", NewContent: "new1\nnew2\n", Additions: 2, Deletions: 0},
			},
		},
		{
			name: "GNU diff -u: a tab and a timestamp follow the path",
			diff: "--- a.go\t2026-10-01 10:00:00.000000000 +0000\n+++ a.go\t2026-10-01 10:05:00.000000000 +0000\n@@ -1 +1,2 @@\n package a\n+var A = 1\n",
			wantFiles: []FileDiff{
				{Path: "a.go", NewContent: "var A = 1\n", Additions: 1},
			},
		},
		{
			name: "svn: a tab and a revision follow the path",
			diff: "Index: a.go\n===================================================================\n--- a.go\t(revision 12)\n+++ a.go\t(working copy)\n@@ -1 +1,2 @@\n package a\n+var A = 1\n",
			wantFiles: []FileDiff{
				{Path: "a.go", NewContent: "var A = 1\n", Additions: 1},
			},
		},
		{
			name: "svn: revision after the new path too",
			diff: "--- a.go\t(revision 11)\n+++ a.go\t(revision 12)\n@@ -1 +1,2 @@\n package a\n+var A = 1\n",
			wantFiles: []FileDiff{
				{Path: "a.go", NewContent: "var A = 1\n", Additions: 1},
			},
		},
		{
			name: "hg: a tab and a date follow the b/ path",
			diff: "diff -r 1a2b3c4d5e6f a.go\n--- a/a.go\tThu Oct 01 10:00:00 2026 +0000\n+++ b/a.go\tThu Oct 01 10:05:00 2026 +0000\n@@ -1 +1,2 @@\n package a\n+var A = 1\n",
			wantFiles: []FileDiff{
				{Path: "a.go", NewContent: "var A = 1\n", Additions: 1},
			},
		},
		{
			name: "git: a path with a space ends with a tab",
			diff: "diff --git a/my file.go b/my file.go\n--- a/my file.go\t\n+++ b/my file.go\t\n@@ -1 +1,2 @@\n package a\n+var A = 1\n",
			wantFiles: []FileDiff{
				{Path: "my file.go", NewContent: "var A = 1\n", Additions: 1},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseUnifiedDiff([]byte(tt.diff))
			require.Len(t, got, len(tt.wantFiles), "file count")
			for i, want := range tt.wantFiles {
				assert.Equal(t, want.Path, got[i].Path, "file %d path", i)
				assert.Equal(t, want.NewContent, got[i].NewContent, "file %d NewContent", i)
				assert.Equal(t, want.Additions, got[i].Additions, "file %d Additions", i)
				assert.Equal(t, want.Deletions, got[i].Deletions, "file %d Deletions", i)
			}
		})
	}
}

func TestParseGitLabDiffs(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantFiles []FileDiff
	}{
		{
			name: "new file",
			input: `[
				{
					"old_path": "new.go",
					"new_path": "new.go",
					"new_file": true,
					"deleted_file": false,
					"diff": "@@ -0,0 +1,2 @@\n+package new\n+var x = 1\n"
				}
			]`,
			wantFiles: []FileDiff{
				{Path: "new.go", NewContent: "package new\nvar x = 1\n", Additions: 2, Deletions: 0},
			},
		},
		{
			name: "modified file with adds and deletes",
			input: `[
				{
					"old_path": "a.go",
					"new_path": "a.go",
					"diff": "@@ -1,2 +1,2 @@\n-package old\n+package new\n var x = 1\n"
				}
			]`,
			wantFiles: []FileDiff{
				{Path: "a.go", NewContent: "package new\n", Additions: 1, Deletions: 1},
			},
		},
		{
			name: "deleted file is skipped",
			input: `[
				{"old_path": "gone.go", "new_path": "gone.go", "deleted_file": true, "diff": "@@ -1 +0,0 @@\n-x\n"},
				{"old_path": "kept.go", "new_path": "kept.go", "diff": "@@ -0,0 +1 @@\n+y\n"}
			]`,
			wantFiles: []FileDiff{
				{Path: "kept.go", NewContent: "y\n", Additions: 1, Deletions: 0},
			},
		},
		{
			name:      "malformed JSON returns empty",
			input:     `not json`,
			wantFiles: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseGitLabDiffs([]byte(tt.input))
			require.Len(t, got, len(tt.wantFiles))
			for i, want := range tt.wantFiles {
				assert.Equal(t, want.Path, got[i].Path)
				assert.Equal(t, want.NewContent, got[i].NewContent)
				assert.Equal(t, want.Additions, got[i].Additions)
				assert.Equal(t, want.Deletions, got[i].Deletions)
			}
		})
	}
}

// TestParseUnifiedDiff_LongLine proves a line longer than any scanner buffer
// neither stops the parse nor drops the files after it.
func TestParseUnifiedDiff_LongLine(t *testing.T) {
	long := strings.Repeat("x", 9*1024*1024)
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -0,0 +1,1 @@\n+" + long +
		"\ndiff --git a/b.go b/b.go\n--- /dev/null\n+++ b/b.go\n@@ -0,0 +1,2 @@\n+package b\n+var x = 1\n"
	got := ParseUnifiedDiff([]byte(diff))
	require.Len(t, got, 2)
	assert.Equal(t, 1, got[0].Additions)
	assert.Len(t, got[0].NewContent, len(long)+1)
	assert.Equal(t, "b.go", got[1].Path)
	assert.Equal(t, 2, got[1].Additions)

	gl := `[{"new_path":"a.go","diff":"@@ -0,0 +1,2 @@\n+` + long + `\n+y\n"}]`
	files := ParseGitLabDiffs([]byte(gl))
	require.Len(t, files, 1)
	assert.Equal(t, 2, files[0].Additions)
}

// TestParseUnifiedDiff_HeaderLookalikesInsideHunks proves content lines that
// start like file headers are read by the hunk's line counts: an added line
// whose content starts with "++" is an addition, and a removed line whose
// content starts with "--" is a deletion.
func TestParseUnifiedDiff_HeaderLookalikesInsideHunks(t *testing.T) {
	diff := "diff --git a/a.c b/a.c\n--- a/a.c\n+++ b/a.c\n@@ -1,2 +1,4 @@\n+int x;\n+++ y;\n+++\n--- old comment\n-select 1;\n+select 2;\n"
	got := ParseUnifiedDiff([]byte(diff))
	require.Len(t, got, 1)
	assert.Equal(t, "a.c", got[0].Path)
	assert.Equal(t, 4, got[0].Additions)
	assert.Equal(t, 2, got[0].Deletions)
	assert.Equal(t, "int x;\n++ y;\n++\nselect 2;\n", got[0].NewContent)

	gl := `[{"new_path":"a.c","diff":"@@ -1,1 +1,2 @@\n+++ y;\n--- old\n+z\n"}]`
	files := ParseGitLabDiffs([]byte(gl))
	require.Len(t, files, 1)
	assert.Equal(t, 2, files[0].Additions)
	assert.Equal(t, 1, files[0].Deletions)
}

// TestParseUnifiedDiff_QuotedPath proves a path git C-quotes (non-ASCII or
// special characters) is unquoted.
func TestParseUnifiedDiff_QuotedPath(t *testing.T) {
	diff := "diff --git \"a/caf\\303\\251.go\" \"b/caf\\303\\251.go\"\n--- /dev/null\n+++ \"b/caf\\303\\251.go\"\n@@ -0,0 +1,1 @@\n+package x\n" +
		"diff --git \"a/tab\\there.go\" \"b/tab\\there.go\"\n--- /dev/null\n+++ \"b/tab\\there.go\"\n@@ -0,0 +1,1 @@\n+package y\n"
	got := ParseUnifiedDiff([]byte(diff))
	require.Len(t, got, 2)
	assert.Equal(t, "café.go", got[0].Path)
	assert.Equal(t, "tab\there.go", got[1].Path)
}

// TestParseGitLabDiffs_EmptyDiffSkipped proves an entry with no added or
// removed lines (a pure rename, a mode change, a binary file) is not a
// changed file, as in a unified diff.
func TestParseGitLabDiffs_EmptyDiffSkipped(t *testing.T) {
	got := ParseGitLabDiffs([]byte(`[
		{"old_path": "a.go", "new_path": "b.go", "renamed_file": true, "diff": ""},
		{"old_path": "logo.png", "new_path": "logo.png", "diff": "Binary files differ\n"},
		{"old_path": "c.go", "new_path": "c.go", "diff": "@@ -1 +0,0 @@\n-x\n"}
	]`))
	require.Len(t, got, 1)
	assert.Equal(t, "c.go", got[0].Path)
	assert.Equal(t, 1, got[0].Deletions)
}

// TestParseUnifiedDiff_HunkEndsWhenItsCountIsUsed proves a hunk stops taking
// added lines once its new-line count is used: a `+++` line after it is the
// next file's header.
func TestParseUnifiedDiff_HunkEndsWhenItsCountIsUsed(t *testing.T) {
	diff := "--- a/a.go\n+++ b/a.go\n@@ -1,2 +1,1 @@\n+x\n+++ b/b.go\n@@ -0,0 +1 @@\n+y\n"
	got := ParseUnifiedDiff([]byte(diff))
	require.Len(t, got, 2)
	assert.Equal(t, "a.go", got[0].Path)
	assert.Equal(t, 1, got[0].Additions)
	assert.Equal(t, "b.go", got[1].Path)
}

// TestParseUnifiedDiff_CRLF proves a diff with Windows line endings parses as
// the same diff with Unix ones: no carriage return in paths or content.
func TestParseUnifiedDiff_CRLF(t *testing.T) {
	unix := "diff --git a/a.go b/a.go\n--- /dev/null\n+++ b/a.go\n@@ -0,0 +1,2 @@\n+package a\n+var x = 1\n"
	crlf := strings.ReplaceAll(unix, "\n", "\r\n")
	assert.Equal(t, ParseUnifiedDiff([]byte(unix)), ParseUnifiedDiff([]byte(crlf)))
}

// TestParseUnifiedDiff_NoNewlineMarkerStaysInHunk proves the "\ No newline at
// end of file" marker belongs to the hunk: the lines after it are still read
// by the hunk's counts, so an added "++b" stays content.
func TestParseUnifiedDiff_NoNewlineMarkerStaysInHunk(t *testing.T) {
	diff := "diff --git a/f.txt b/f.txt\n--- a/f.txt\n+++ b/f.txt\n@@ -1 +1,2 @@\n-a\n\\ No newline at end of file\n+++b\n+c\n"
	got := ParseUnifiedDiff([]byte(diff))
	require.Len(t, got, 1)
	assert.Equal(t, "f.txt", got[0].Path)
	assert.Equal(t, 2, got[0].Additions)
	assert.Equal(t, 1, got[0].Deletions)
	assert.Equal(t, "++b\nc\n", got[0].NewContent)
}

// TestParseUnifiedDiff_NegativeHunkCountOpensNoHunk proves a hunk header with
// a negative count opens no hunk, so a `+++ ` line after it is a file header.
func TestParseUnifiedDiff_NegativeHunkCountOpensNoHunk(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,-1 +1,1 @@\n+++ b/x.go\n+y\n"
	got := ParseUnifiedDiff([]byte(diff))
	require.Len(t, got, 2)
	assert.Equal(t, "a.go", got[0].Path)
	assert.Equal(t, 0, got[0].Additions)
	assert.Equal(t, "x.go", got[1].Path)
	assert.Equal(t, 1, got[1].Additions)
}

// TestParseUnifiedDiff_HunkEndsAtContextAfterOneSideIsUsed proves a context
// line ends the hunk once either side's count is used up, so a later "--- "
// header is not taken for a removed line.
func TestParseUnifiedDiff_HunkEndsAtContextAfterOneSideIsUsed(t *testing.T) {
	diff := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,3 +1,1 @@\n ctx\n ctx2\n--- foo\n"
	files := ParseUnifiedDiff([]byte(diff))
	if len(files) != 1 || files[0].Deletions != 0 || files[0].Additions != 0 {
		t.Fatalf("got %+v, want one file with no additions or deletions", files)
	}
}

// TestParseGitLabDiffs_OldPathFallback proves an entry with an empty new_path
// is counted under its old_path.
func TestParseGitLabDiffs_OldPathFallback(t *testing.T) {
	raw := `[{"old_path": "a.go", "new_path": "", "diff": "@@ -1,1 +1,2 @@\n package a\n+var x = 1\n"}]`
	files := ParseGitLabDiffs([]byte(raw))
	if len(files) != 1 || files[0].Path != "a.go" || files[0].Additions != 1 {
		t.Fatalf("got %+v, want a.go with one addition", files)
	}
	result, err := AnalyzeDiff([]byte(raw), FormatGitLabJSON)
	if err != nil {
		t.Fatal(err)
	}
	if d := result.ByLanguage["Go"]; d == nil || d.FilesChanged != 1 {
		t.Fatalf("ByLanguage = %v, want Go with one file", result.ByLanguage)
	}
}
