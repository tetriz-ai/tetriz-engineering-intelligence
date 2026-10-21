// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prcomplexity

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestQuickStartMatches pins the quick start in three places: this package
// README's go block under "Quick start", examples/score-one-pr/main.go
// (the same bytes), and ExampleScoreComplexity (the same body as main, comments
// dropped).
func TestQuickStartMatches(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)## Quick start.*?```go\n(.*?)```").FindSubmatch(readme)
	if m == nil {
		t.Fatal("README.md: no go block under ## Quick start")
	}
	mainGo, err := os.ReadFile("../examples/score-one-pr/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m[1], mainGo) {
		t.Fatalf("README quick start and score-one-pr/main.go differ:\n%s\n---\n%s", m[1], mainGo)
	}

	mainBody := funcBody(t, "../examples/score-one-pr/main.go", "main")
	exampleBody := funcBody(t, "example_test.go", "ExampleScoreComplexity")
	if mainBody != exampleBody {
		t.Fatalf("ExampleScoreComplexity and main differ:\n%s\n---\n%s", exampleBody, mainBody)
	}
}

// TestCountLinesMatchesExample pins diffsize/README.md's "Count a diff's
// lines" program to examples/count-loc/main.go, byte for byte.
func TestCountLinesMatchesExample(t *testing.T) {
	readme, err := os.ReadFile("../diffsize/README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)### Count a diff's lines\n.*?```go\n(.*?)```").FindSubmatch(readme)
	if m == nil {
		t.Fatal("diffsize/README.md: no go block under ### Count a diff's lines")
	}
	mainGo, err := os.ReadFile("../examples/count-loc/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m[1], mainGo) {
		t.Fatalf("README's count-lines program and count-loc/main.go differ:\n%s\n---\n%s", m[1], mainGo)
	}
}

// TestREADMEBlocksMatchExamples pins the other README programs: each go block
// has the same top-level declarations and main body as an Example, and its
// text block is that Example's checked output.
func TestREADMEBlocksMatchExamples(t *testing.T) {
	for _, tc := range []struct{ readme, heading, exampleFile, example string }{
		{"README.md", "### Score a diff", "readme_example_test.go", "Example_scoreADiff"},
		{"README.md", "### Use with tetriz-connectors", "readme_example_test.go", "Example_useWithTetrizConnectors"},
		{"../diffsize/README.md", "### Count a diff's lines", "../diffsize/readme_example_test.go", "Example_quickStart"},
		{"../diffsize/README.md", "### Exclude files", "../diffsize/readme_example_test.go", "Example_excludeFiles"},
		{"README.md", "### Build a baseline", "readme_example_test.go", "Example_buildABaseline"},
	} {
		t.Run(tc.readme+" "+tc.heading, func(t *testing.T) {
			readme, err := os.ReadFile(tc.readme)
			if err != nil {
				t.Fatal(err)
			}
			re := regexp.MustCompile("(?s)" + regexp.QuoteMeta(tc.heading) + "\n.*?```go\n(.*?)```\\s*```text\n(.*?)```")
			m := re.FindSubmatch(readme)
			if m == nil {
				t.Fatalf("%s: no go and text block under %q", tc.readme, tc.heading)
			}
			block := filepath.Join(t.TempDir(), "main.go")
			if err := os.WriteFile(block, m[1], 0o600); err != nil {
				t.Fatal(err)
			}
			if got, want := funcBody(t, block, "main"), funcBody(t, tc.exampleFile, tc.example); got != want {
				t.Fatalf("README block and %s differ:\n%s\n---\n%s", tc.example, got, want)
			}
			for name, decl := range topDecls(t, block) {
				if other := topDecls(t, tc.exampleFile)[name]; other != decl {
					t.Fatalf("%s differs between the README and %s:\n%s\n---\n%s", name, tc.exampleFile, decl, other)
				}
			}
			if got, want := exampleOutput(t, tc.exampleFile, tc.example), strings.TrimSpace(string(m[2])); got != want {
				t.Fatalf("README output and %s's Output differ:\n%s\n---\n%s", tc.example, want, got)
			}
		})
	}
}

// topDecls prints each top-level const, var and func declaration in file, by
// name, without comments. main and the Example functions are left out: their
// bodies are compared on their own.
func topDecls(t *testing.T, file string) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			if fd.Name.Name == "main" || strings.HasPrefix(fd.Name.Name, "Example") {
				continue
			}
			var b bytes.Buffer
			if err := printer.Fprint(&b, fset, fd); err != nil {
				t.Fatal(err)
			}
			out["func "+fd.Name.Name] = b.String()
			continue
		}
		gd, ok := d.(*ast.GenDecl)
		if !ok || (gd.Tok != token.CONST && gd.Tok != token.VAR) {
			continue
		}
		for _, spec := range gd.Specs {
			vs := spec.(*ast.ValueSpec)
			var b bytes.Buffer
			if err := printer.Fprint(&b, fset, vs); err != nil {
				t.Fatal(err)
			}
			for _, n := range vs.Names {
				out[gd.Tok.String()+" "+n.Name] = b.String()
			}
		}
	}
	return out
}

// exampleOutput returns the text of the named Example's Output comment.
func exampleOutput(t *testing.T, file, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != name {
			continue
		}
		for _, cg := range f.Comments {
			if cg.Pos() > fd.Body.Lbrace && cg.End() < fd.Body.Rbrace {
				if text, found := strings.CutPrefix(cg.Text(), "Output:"); found {
					return strings.TrimSpace(text)
				}
			}
		}
	}
	t.Fatalf("%s: no Output comment in %s", file, name)
	return ""
}

// funcBody prints the body of the named function in file, without comments
// or the blank lines they leave.
func funcBody(t *testing.T, file, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0) // no parser.ParseComments: comments dropped
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == name {
			var b bytes.Buffer
			if err := printer.Fprint(&b, fset, fd.Body); err != nil {
				t.Fatal(err)
			}
			var lines []string
			for _, l := range strings.Split(b.String(), "\n") {
				if strings.TrimSpace(l) != "" {
					lines = append(lines, l)
				}
			}
			return strings.Join(lines, "\n")
		}
	}
	t.Fatalf("%s: no func %s", file, name)
	return ""
}
