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
