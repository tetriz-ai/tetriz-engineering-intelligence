// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issueattribution

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

// TestQuickStartIsTheExample pins the quick start in three places: the go
// block under "## Quick start", the example's main.go (the same bytes), and
// Example_quickStart (the same main body, and the README's output as its
// Output).
func TestQuickStartIsTheExample(t *testing.T) {
	const mainGo = "../examples/measure-issue-ai-share/main.go"
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)## Quick start\n.*?```go\n(.*?)```.*?```text\n(.*?)```").FindSubmatch(readme)
	if m == nil {
		t.Fatal("README.md: no go and text block under the quick start")
	}
	program, err := os.ReadFile(mainGo)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m[1], program) {
		t.Fatalf("README block and %s differ:\n%s\n---\n%s", mainGo, m[1], program)
	}
	if got, want := funcBody(t, mainGo, "main"), funcBody(t, "readme_example_test.go", "Example_quickStart"); got != want {
		t.Fatalf("Example_quickStart and main differ:\n%s\n---\n%s", want, got)
	}
	if got, want := exampleOutput(t, "readme_example_test.go", "Example_quickStart"), strings.TrimSpace(string(m[2])); got != want {
		t.Fatalf("README output and Example_quickStart's Output differ:\n%s\n---\n%s", want, got)
	}
}

// TestREADMEBlocksMatchExamples pins each Usage program: its main body is an
// Example's body, and its text block is that Example's checked output.
func TestREADMEBlocksMatchExamples(t *testing.T) {
	for _, tc := range []struct{ heading, example string }{
		{"### Leave out weak links", "Example_linkFloor"},
		{"### Per repository and per week", "Example_byRepositoryAndWeek"},
	} {
		t.Run(tc.heading, func(t *testing.T) {
			readme, err := os.ReadFile("README.md")
			if err != nil {
				t.Fatal(err)
			}
			re := regexp.MustCompile("(?s)" + regexp.QuoteMeta(tc.heading) + "\n.*?```go\n(.*?)```\\s*```text\n(.*?)```")
			m := re.FindSubmatch(readme)
			if m == nil {
				t.Fatalf("README.md: no go and text block under %q", tc.heading)
			}
			block := filepath.Join(t.TempDir(), "main.go")
			if err := os.WriteFile(block, m[1], 0o600); err != nil {
				t.Fatal(err)
			}
			if got, want := funcBody(t, block, "main"), funcBody(t, "readme_example_test.go", tc.example); got != want {
				t.Fatalf("README block and %s differ:\n%s\n---\n%s", tc.example, got, want)
			}
			if len(topDecls(t, block)) != 0 {
				t.Fatalf("README block under %q declares more than main", tc.heading)
			}
			if got, want := exampleOutput(t, "readme_example_test.go", tc.example), strings.TrimSpace(string(m[2])); got != want {
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
