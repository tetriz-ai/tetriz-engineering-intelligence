// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package diffsize_test

import (
	"fmt"
	"log"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize"
)

// diff and Example_quickStart are the README's "Count a diff's lines" program
// (prcomplexity/readme_test.go checks).
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

func Example_quickStart() {
	loc, err := diffsize.AnalyzeDiffWithExcludes([]byte(diff), diffsize.FormatUnified, []string{"*.lock"})
	if err != nil {
		log.Fatal(err)
	}
	py := loc.ByLanguage["Python"]
	fmt.Println(py.SourceLinesAdded, py.CommentLinesAdded, py.BlankLinesAdded, py.FilesChanged, loc.ExcludedFiles)
	// Output: 2 1 1 1 [poetry.lock]
}

// excludeDiff and Example_excludeFiles are the README's "Exclude files"
// program (prcomplexity/readme_test.go checks).
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

func Example_excludeFiles() {
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
	// Output:
	// [api/user.pb.go go.sum web/package-lock.json] 1
	// [dist/**]
}
