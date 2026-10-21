# diffsize

A diff's added-line count mixes code with comments, blank lines, lock files and generated output, and does not say which languages changed. `diffsize` turns a pull request's diff into effective lines of code per language, with generated and excluded files set aside, and `prcomplexity` scores from that output.

## Contents

- [What it does](#what-it-does) lists the entry points.
- [Usage](#usage) shows each entry point in a complete program.
- [Reference](#reference) gives the exact rules for counting, parsing and exclude globs.
- [Before you rely on it](#before-you-rely-on-it) lists where sizes can mislead, most harmful first.

## What it does

`AnalyzeDiff` and `AnalyzeDiffWithExcludes` read a git unified diff or GitLab's merge-request diffs JSON. `diffsize` names each file's language with go-enry, a Go port of GitHub Linguist's rules, and counts code, comment and blank lines with scc. It also parses diffs (`ParseUnifiedDiff`, `ParseGitLabDiffs`) and matches exclude globs (`MatchAny`, `PatternsForRepo`).

## Usage

### Count a diff's lines

When: you want a diff's effective lines of code per language, without a score.

```go
// Command count-loc counts the effective lines of code a two-file diff adds.
package main

import (
	"fmt"
	"log"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize"
)

const diff = `diff --git a/app/main.py b/app/main.py
--- /dev/null
+++ b/app/main.py
@@ -0,0 +1,4 @@
+# Entry point.
+def main():
+    print("hello")
+
diff --git a/poetry.lock b/poetry.lock
--- /dev/null
+++ b/poetry.lock
@@ -0,0 +1,1 @@
+lock data
`

func main() {
	loc, err := diffsize.AnalyzeDiffWithExcludes([]byte(diff), diffsize.FormatUnified, []string{"*.lock"})
	if err != nil {
		log.Fatal(err)
	}
	py := loc.ByLanguage["Python"]
	fmt.Println(py.SourceLinesAdded, py.CommentLinesAdded, py.BlankLinesAdded, py.FilesChanged, loc.ExcludedFiles)
}
```

```text
2 1 1 1 [poetry.lock]
```

Remember:

- Line counts take added lines only: removed lines are not in `EffectiveLOC`, and a deleted file adds nothing. `FilesChanged` counts every file in that language the diff touches, including one it only removes lines from.
- `ByLanguage` is keyed by go-enry's exact language name, or "Other" when go-enry cannot tell.

### Exclude files

When: your repositories hold generated or lock files that go-enry does not recognise. The patterns below are common choices.

```go
package main

import (
	"fmt"
	"log"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize"
)

const excludeDiff = `diff --git a/api/user.pb.go b/api/user.pb.go
--- a/api/user.pb.go
+++ b/api/user.pb.go
@@ -40,2 +40,3 @@
 	x := 1
+	y := 2
 	return x
diff --git a/go.sum b/go.sum
--- a/go.sum
+++ b/go.sum
@@ -1 +1,2 @@
 example.com/a v1.0.0/go.mod h1:x
+example.com/b v1.0.0/go.mod h1:y
diff --git a/web/package-lock.json b/web/package-lock.json
--- /dev/null
+++ b/web/package-lock.json
@@ -0,0 +1 @@
+{}
diff --git a/api/user.go b/api/user.go
--- a/api/user.go
+++ b/api/user.go
@@ -1 +1,2 @@
 package api
+var y = 2
`

func main() {
	// Patterns for the whole organisation, and one repository that replaces them.
	org := []string{"*.pb.go", "go.sum", "package-lock.json", "*.lock"}
	overrides := map[string][]string{"example/web": {"dist/**"}}

	globs := diffsize.PatternsForRepo("example/api", org, overrides)
	loc, err := diffsize.AnalyzeDiffWithExcludes([]byte(excludeDiff), diffsize.FormatUnified, globs)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(loc.ExcludedFiles, loc.ByLanguage["Go"].SourceLinesAdded)
	fmt.Println(diffsize.PatternsForRepo("example/web", org, overrides))
}
```

```text
[api/user.pb.go go.sum web/package-lock.json] 1
[dist/**]
```

Remember:

- The edit to `api/user.pb.go` does not show its generator banner, so go-enry counts it as Go source. Only the glob leaves it out.
- `MatchAny` tests one path against the same globs. It skips an invalid pattern, which `AnalyzeDiffWithExcludes` rejects.

### Parse a diff

When: you want each file's added content and line counts, without classifying or counting languages.

```go
package main

import (
	"fmt"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize"
)

const diff = `diff --git a/api/routes.go b/api/routes.go
--- a/api/routes.go
+++ b/api/routes.go
@@ -10,3 +10,4 @@
 	r.Get("/health", health)
-	r.Get("/v1/users", listUsers)
+	r.Get("/v2/users", listUsers)
+	r.Get("/v2/teams", listTeams)
 }
diff --git a/old/legacy.go b/old/legacy.go
--- a/old/legacy.go
+++ /dev/null
@@ -1,2 +0,0 @@
-package old
-var x = 1
`

func main() {
	for _, f := range diffsize.ParseUnifiedDiff([]byte(diff)) {
		fmt.Printf("%s +%d -%d\n%s", f.Path, f.Additions, f.Deletions, f.NewContent)
	}
}
```

```text
api/routes.go +2 -1
	r.Get("/v2/users", listUsers)
	r.Get("/v2/teams", listTeams)
```

Remember:

- A deleted file is skipped, because it adds nothing.
- `ParseUnifiedDiff` never fails. `AnalyzeDiff` is the call that rejects input that is not a diff.

More: the full API is on [pkg.go.dev](https://pkg.go.dev/github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize), and every example is listed in [examples/README.md](../examples/README.md).

## Reference

### AnalyzeDiffWithExcludes

`AnalyzeDiffWithExcludes(raw, format, globs)` returns an `EffectiveLOC`, and `AnalyzeDiff(raw, format)` is the same with no globs.

Each file goes through these steps in order. The first that applies decides where it counts.

| Step | Where the file's added lines count |
|---|---|
| matches an exclude glob | nowhere; listed in `ExcludedFiles` |
| vendored (go-enry's path rules) | `TotalVendorLinesAdded`; listed |
| generated (go-enry's path rules, and its content rules on the added lines) | `TotalGeneratedLinesAdded`; listed |
| documentation (go-enry's path rules) | `TotalDocLinesAdded` |
| anything else | its go-enry language, split by scc into source, comment and blank |

- **Comment syntax** is scc's for go-enry's name, or, when scc does not know that name, for scc's name for the same language from a reviewed alias list, as with TSX, Protocol Buffer or PowerShell. A language with no alias, and "Other", get no comment syntax, so every non-blank line counts as source. The key stays go-enry's.
- **Errors:** a format other than `FormatUnified` or `FormatGitLabJSON`, GitLab JSON that does not parse or is null, or an invalid glob. Non-blank `FormatUnified` input with no `diff --git `, `--- ` or `+++ ` line is an error too, and so is a combined diff (`diff --cc` or `diff --combined`, which git writes for a merge commit): diff the merge against its first parent instead. The result is then nil.
- **`ErrDiffExcluded`** wraps the error for a GitLab entry flagged `too_large` or `collapsed`, whose diff is not in the response, when no exclude glob matches its path. A flagged entry a glob matches is excluded like any other file: listed in `ExcludedFiles`, counted nowhere. On `too_large`, score the PR as `DiffTooLarge`. On `collapsed`, refetch that file's diff and analyse again when you can, and otherwise, as with a diff read from a store, score it as `DiffTooLarge`.
- **Files and lines:** a file counts in its language's `FilesChanged` when the diff touches it, including a file it only removes lines from. `SourceLinesAdded`, `CommentLinesAdded` and `BlankLinesAdded` count added lines only. A NUL byte in an added line is read as a space, since go-enry and scc stop reading at one.
- **`AnalysisVersion`** is "v2". It changes when the counts would change for the same diff.

### Parsing

`ParseUnifiedDiff(raw)` and `ParseGitLabDiffs(raw)` return each file's added content and its added and removed line counts.

- **Unified diffs:** deleted files are skipped, `b/` prefixes stripped, and C-quoted paths unquoted. An unquoted path ends at the first tab, so the headers of GNU `diff -u`, svn and hg, which put a tab and a timestamp or revision after the path, are read too. A combined diff (`diff --cc`) is not read. Lines inside a hunk are read by the hunk header's counts, so content that starts with `++` or `--` is content. Lines of any length are read.
- **GitLab JSON:** deleted files are skipped, and so are entries with no added or removed line (a pure rename, a mode change, a binary file). An entry with changed lines but no `new_path` or `old_path` is an error naming its position, such as `entry 0`. `ParseGitLabDiffs` returns nil for every GitLab input `AnalyzeDiff` rejects.
- **Other formats,** such as a GitHub files-API JSON response, are not read.

### Exclude globs

| Pattern | Matches |
|---|---|
| `*.pb.go` | that basename at any depth |
| `**/*.pb.go` | the suffix at any depth |
| `gen/**` | everything under `gen/` |
| `vendor/*` | one level under `vendor/` only, so write `vendor/**` for all of it |
| `path/to/file.go` | that path |

- **Syntax** is doublestar's: `filepath.Match` plus `**`.
- **A pattern without a slash** is also tried against the basename. A slash anywhere turns that off, even inside one brace alternative: `{gen/**,*.pb.go}` does not match `dir/x.pb.go`. Write the alternatives as separate patterns.
- **`MatchAny(path, globs)`** skips empty and invalid patterns. `AnalyzeDiffWithExcludes` rejects invalid ones. Check one with `doublestar.ValidatePattern`.
- **`PatternsForRepo`** returns the repository's override when `overrides` has its exact full name, even an empty one. Otherwise it returns the organisation's patterns. An override replaces them rather than adding to them.

## Before you rely on it

Check these before you put sizes in front of people. Each is pinned by the test named with it.

### Sizes

**Counts more than it should (can mislead):**

- An edit to an existing generated file, such as a `.pb.go`, `mock_*.go`, `wire_gen.go` or `_pb2.py` file, counts as source. The file is left out only when its path matches an exclude glob you pass. Pinned in `diffsize/diffsize_test.go`, `TestAnalyzeDiff_ModifiedGeneratedFileCountsAsSource`.
- A file marked generated only by a banner go-enry does not know counts as source. Add a glob for it. Pinned in `diffsize/diffsize_test.go`, `TestAnalyzeDiff_UnknownGeneratorBannerCountsAsSource`.
- Declined: a file the diff only removes lines from counts in its language's `FilesChanged`, though it adds no line. 3 added lines in one Go file plus 12 deletion-only Go files give `FilesChanged: 13`, which against the Go baseline in [prcomplexity's README](../prcomplexity/README.md#score-a-diff) scores 8.2 instead of 1.3. Stored baselines count files the same way, so they stay comparable. Pinned in `diffsize/diffsize_test.go`, `TestAnalyzeDiff_DeletionOnlyFileCountsInFilesChangedDeclined`.
- Comment lines count as source in a language with no comment syntax, such as Fortran, Modula-3, Java Server Pages, Classic ASP, Eiffel or "Other". Pinned in `diffsize/diffsize_test.go`, `TestAnalyzeDiff_NoAliasWhereSccReadsCodeAsComment`, `TestAnalyzeDiff_NoAliasCountsAllSource` and `TestAnalyzeDiff_OtherHasNoCommentSyntax`.

**Counts less than it should (less harmful):**

- Code in a folder go-enry treats as documentation, such as a top-level `docs/`, `examples/` or `samples/`, counts in `TotalDocLinesAdded`, not as source.
- First-party code under a path go-enry treats as vendored counts in `TotalVendorLinesAdded` and is listed in `ExcludedFiles`. The path rules include any folder named `third_party/`, `vendor/`, `cache/`, `env/`, `dist/` or `testdata/` at any depth, and `*.d.ts` files.

Both are pinned in `diffsize/diffsize_test.go`, `TestAnalyzeDiff_PathRulesCountCodeAsDocOrVendor`.
