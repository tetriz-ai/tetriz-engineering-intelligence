// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package diffsize_test

import (
	"fmt"
	"log"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize"
)

const sampleDiff = `diff --git a/server/handler.go b/server/handler.go
--- a/server/handler.go
+++ b/server/handler.go
@@ -1,3 +1,7 @@
 package server
 
+// Health reports that the server is up.
+func Health() string {
+	return "ok"
+}
+
 func Start() {}
diff --git a/go.sum b/go.sum
--- a/go.sum
+++ b/go.sum
@@ -1 +1,2 @@
 example.com/a v1.0.0 h1:abc=
+example.com/b v1.0.0 h1:def=
`

func ExampleAnalyzeDiff() {
	loc, err := diffsize.AnalyzeDiff([]byte(sampleDiff), diffsize.FormatUnified)
	if err != nil {
		log.Fatal(err)
	}
	g := loc.ByLanguage["Go"]
	fmt.Println(g.SourceLinesAdded, g.CommentLinesAdded, g.BlankLinesAdded, g.FilesChanged)
	// Output: 3 1 1 1
}

func ExampleAnalyzeDiffWithExcludes() {
	loc, err := diffsize.AnalyzeDiffWithExcludes([]byte(sampleDiff), diffsize.FormatUnified, []string{"go.sum"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(loc.ExcludedFiles, len(loc.ByLanguage))

	_, err = diffsize.AnalyzeDiffWithExcludes([]byte(sampleDiff), diffsize.FormatUnified, []string{"[go.sum"})
	fmt.Println(err)
	// Output:
	// [go.sum] 1
	// invalid exclude pattern "[go.sum"
}

func ExampleParseUnifiedDiff() {
	for _, f := range diffsize.ParseUnifiedDiff([]byte(sampleDiff)) {
		fmt.Println(f.Path, f.Additions, f.Deletions)
	}
	// Output:
	// server/handler.go 5 0
	// go.sum 1 0
}

func ExampleParseGitLabDiffs() {
	raw := `[{"old_path": "app.py", "new_path": "app.py", "diff": "@@ -1 +1,2 @@\n-print(1)\n+print(2)\n+print(3)\n"}]`
	for _, f := range diffsize.ParseGitLabDiffs([]byte(raw)) {
		fmt.Printf("%s +%d -%d %q\n", f.Path, f.Additions, f.Deletions, f.NewContent)
	}
	// Output: app.py +2 -1 "print(2)\nprint(3)\n"
}

func ExampleMatchAny() {
	patterns := []string{"*.pb.go", "gen/**"}
	for _, path := range []string{"api/v1/user.pb.go", "gen/client/x.go", "api/user.go"} {
		fmt.Println(path, diffsize.MatchAny(path, patterns))
	}
	// Output:
	// api/v1/user.pb.go true
	// gen/client/x.go true
	// api/user.go false
}

func ExamplePatternsForRepo() {
	org := []string{"*.pb.go", "go.sum", "package-lock.json", "*.lock"}
	overrides := map[string][]string{"example/web": {"dist/**"}}
	fmt.Println(diffsize.PatternsForRepo("example/api", org, overrides))
	fmt.Println(diffsize.PatternsForRepo("example/web", org, overrides))
	// Output:
	// [*.pb.go go.sum package-lock.json *.lock]
	// [dist/**]
}
