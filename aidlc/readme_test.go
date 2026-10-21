// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"regexp"
	"strings"
	"testing"
)

// readmeProgram returns the go block under a README heading and the text block after it.
func readmeProgram(t *testing.T, readme []byte, heading string) (program, output []byte) {
	t.Helper()
	m := regexp.MustCompile("(?s)\n" + regexp.QuoteMeta(heading) + "\n.*?```go\n(.*?)```.*?```text\n(.*?)```").FindSubmatch(readme)
	if m == nil {
		t.Fatalf("README.md: no go block and text block under %s", heading)
	}
	return m[1], m[2]
}

// exampleOutput returns the last text block of an example's README, the output the
// examples runner holds the example to.
func exampleOutput(t *testing.T, name string) []byte {
	t.Helper()
	readme, err := os.ReadFile("../examples/" + name + "/README.md")
	if err != nil {
		t.Fatal(err)
	}
	blocks := regexp.MustCompile("(?s)```text\n(.*?)```").FindAllSubmatch(readme, -1)
	if len(blocks) == 0 {
		t.Fatalf("examples/%s/README.md: no text block", name)
	}
	return blocks[len(blocks)-1][1]
}

// TestREADMEProgramsMatch pins every program in this package's README to code that runs:
// the quick start, "Withhold a profile" and "Read movement" are byte for byte their
// examples' main.go, with those examples' outputs; the quick start's main is also
// ExampleScore's body. "Fold raw facts" has fold's Example body as its main, and that
// Example's checked output.
func TestREADMEProgramsMatch(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	for heading, name := range map[string]string{
		"## Quick start":         "score-maturity-readings",
		"### Withhold a profile": "withhold-a-thin-window",
		"### Read movement":      "track-movement",
	} {
		program, output := readmeProgram(t, readme, heading)
		mainGo, err := os.ReadFile("../examples/" + name + "/main.go")
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(program, mainGo) {
			t.Errorf("README %s and examples/%s/main.go differ:\n%s\n---\n%s", heading, name, program, mainGo)
		}
		if want := exampleOutput(t, name); !bytes.Equal(output, want) {
			t.Errorf("README %s output is not examples/%s's:\n%s\n---\n%s", heading, name, output, want)
		}
	}

	if got, want := funcBody(t, "../examples/score-maturity-readings/main.go", nil, "main"), funcBody(t, "example_test.go", nil, "ExampleScore"); got != want {
		t.Errorf("ExampleScore and the quick start's main differ:\n%s\n---\n%s", want, got)
	}

	program, output := readmeProgram(t, readme, "### Fold raw facts")
	if got, want := funcBody(t, "README.md", program, "main"), funcBody(t, "fold/example_test.go", nil, "Example"); got != want {
		t.Errorf("README Fold raw facts main and fold's Example differ:\n%s\n---\n%s", got, want)
	}
	if want := exampleOutputComment(t, "fold/example_test.go"); string(output) != want {
		t.Errorf("README Fold raw facts output is not fold's Example output:\n%s\n---\n%s", output, want)
	}
}

// exampleOutputComment returns the lines of the "// Output:" comment in an Example file.
func exampleOutputComment(t *testing.T, file string) string {
	t.Helper()
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	_, after, ok := strings.Cut(string(src), "\t// Output:\n")
	if !ok {
		t.Fatalf("%s: no Output comment", file)
	}
	var b strings.Builder
	for _, line := range strings.Split(after, "\n") {
		if !strings.HasPrefix(line, "\t// ") {
			break
		}
		b.WriteString(strings.TrimPrefix(line, "\t// ") + "\n")
	}
	return b.String()
}

// funcBody prints a function's body with comments and blank lines dropped. src, when
// non-nil, is parsed instead of reading file.
func funcBody(t *testing.T, file string, src []byte, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	var source any
	if src != nil {
		source = src
	}
	f, err := parser.ParseFile(fset, file, source, 0) // no parser.ParseComments: comments dropped
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
