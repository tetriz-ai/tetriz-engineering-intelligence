// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// quickStartBlock returns the first go code block under the README's
// "## Quick start" heading.
func quickStartBlock(t *testing.T) string {
	t.Helper()
	readme, err := os.ReadFile("../README.md")
	require.NoError(t, err)
	s := string(readme)
	i := strings.Index(s, "\n## Quick start\n")
	require.GreaterOrEqual(t, i, 0, "README has a Quick start section")
	s = s[i:]
	start := strings.Index(s, "\n```go\n")
	require.GreaterOrEqual(t, start, 0, "Quick start has a go block")
	s = s[start+len("\n```go\n"):]
	end := strings.Index(s, "\n```\n")
	require.GreaterOrEqual(t, end, 0)
	return s[:end+1]
}

// funcBody prints the body of the named top-level function, comments dropped.
func funcBody(t *testing.T, path, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	require.NoError(t, err)
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == name {
			var b bytes.Buffer
			require.NoError(t, printer.Fprint(&b, fset, fn.Body))
			return b.String()
		}
	}
	t.Fatalf("%s has no func %s", path, name)
	return ""
}

// The README's quick start, examples/match-one-session and the package Example
// are one program, so none of them can drift from the others.
func TestQuickStartIsOneProgram(t *testing.T) {
	main, err := os.ReadFile("../examples/match-one-session/main.go")
	require.NoError(t, err)
	assert.Equal(t, string(main), quickStartBlock(t), "README quick start == examples/match-one-session/main.go")
	assert.Equal(t, funcBody(t, "../examples/match-one-session/main.go", "main"), funcBody(t, "example_test.go", "Example"),
		"Example's body == main's body")
}

// readmePrograms returns every go block in the README with the text block that
// follows it, which is the output the README shows for it.
func readmePrograms(t *testing.T) (programs, outputs []string) {
	t.Helper()
	readme, err := os.ReadFile("../README.md")
	require.NoError(t, err)
	blocks := regexp.MustCompile("(?s)\n```(\\w+)\n(.*?)\n```\n").FindAllStringSubmatch(string(readme), -1)
	for _, b := range blocks {
		switch {
		case b[1] == "go":
			programs = append(programs, b[2]+"\n")
			outputs = append(outputs, "")
		case b[1] == "text" && len(outputs) > 0 && outputs[len(outputs)-1] == "":
			outputs[len(outputs)-1] = b[2]
		}
	}
	return programs, outputs
}

// Every program in the README runs and prints exactly the output shown after it,
// so no README block can go stale. Each is built inside this module, the way the
// Quick start's go.mod resolves it once the module is fetched.
func TestEveryREADMEProgramRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("builds every README program")
	}
	programs, outputs := readmePrograms(t)
	require.GreaterOrEqual(t, len(programs), 7, "the README has its programs")
	// go test puts the go command that runs it first on PATH.
	goTool, err := exec.LookPath("go")
	require.NoError(t, err)
	for i, program := range programs {
		require.NotEmpty(t, outputs[i], "README program %d has an output block", i+1)
		dir, err := os.MkdirTemp("..", ".readme-program-")
		require.NoError(t, err)
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		require.NoError(t, os.WriteFile(filepath.Join(dir, "main.go"), []byte(program), 0o600))

		cmd := exec.Command(goTool, "run", "./"+filepath.Base(dir))
		cmd.Dir = ".."
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		require.NoError(t, cmd.Run(), "README program %d: %s", i+1, stderr.String())
		assert.Equal(t, outputs[i], strings.TrimRight(stdout.String(), "\n"), "README program %d", i+1)
	}
}
