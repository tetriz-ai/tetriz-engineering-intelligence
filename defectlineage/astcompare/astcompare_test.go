// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

//go:build astcompare

package astcompare_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/astcompare"
)

const goSource = `package p

func Alpha(x int) int {
	y := x + 1
	return y
}

func Beta(x int) int {
	z := x * 2
	return z
}
`

func region(path string, start, end int) defectlineage.ChangedRegion {
	return defectlineage.ChangedRegion{Path: path, StartLine: start, EndLine: end}
}

// TestTheSameStatementIsAnExactNode: both sides changed the same element of the
// same function, which is the strongest context the comparison reports.
func TestTheSameStatementIsAnExactNode(t *testing.T) {
	v, err := astcompare.Comparator{}.Compare(context.Background(),
		region("a.go", 4, 4), []byte(goSource),
		region("a.go", 4, 4), []byte(goSource))
	require.NoError(t, err)
	assert.Equal(t, defectlineage.MatchExactNode, v.Level)
	assert.False(t, v.Degraded, "a measured match is never degraded")
	assert.NotEmpty(t, v.NodeType, "the verdict explains which node it matched")
	assert.Equal(t, "function_declaration", v.EnclosingType)
}

// TestADifferentStatementInTheSameFunctionIsSameEnclosing: one function, two
// elements.
func TestADifferentStatementInTheSameFunctionIsSameEnclosing(t *testing.T) {
	v, err := astcompare.Comparator{}.Compare(context.Background(),
		region("a.go", 4, 4), []byte(goSource),
		region("a.go", 5, 5), []byte(goSource))
	require.NoError(t, err)
	assert.Equal(t, defectlineage.MatchSameEnclosing, v.Level)
	assert.False(t, v.Degraded)
}

// TestADifferentFunctionIsSameFile: the same file, different declarations.
func TestADifferentFunctionIsSameFile(t *testing.T) {
	v, err := astcompare.Comparator{}.Compare(context.Background(),
		region("a.go", 4, 4), []byte(goSource),
		region("a.go", 9, 9), []byte(goSource))
	require.NoError(t, err)
	assert.Equal(t, defectlineage.MatchSameFile, v.Level)
	assert.False(t, v.Degraded, "a measured same-file answer is not a degrade")
}

// TestADifferentPathShortCircuits: no parsing is needed to know two files are
// not the same file.
func TestADifferentPathShortCircuits(t *testing.T) {
	v, err := astcompare.Comparator{}.Compare(context.Background(),
		region("a.go", 4, 4), []byte(goSource),
		region("b.go", 4, 4), []byte(goSource))
	require.NoError(t, err)
	assert.Equal(t, defectlineage.MatchDifferentFile, v.Level)
	assert.False(t, v.Degraded)
}

// TestAnUnsupportedGrammarDegrades: a language with no grammar must not be
// parsed with the wrong one, so the comparison reports coverage and says so.
func TestAnUnsupportedGrammarDegrades(t *testing.T) {
	v, err := astcompare.Comparator{}.Compare(context.Background(),
		region("a.rs", 1, 1), []byte("fn main() {}"),
		region("a.rs", 1, 1), []byte("fn main() {}"))
	require.NoError(t, err)
	assert.Equal(t, defectlineage.MatchSameFile, v.Level)
	assert.True(t, v.Degraded, "an unparsed file must be flagged as coverage")
}

// TestAnOutOfRangeRegionDegrades rather than keying on the whole file, which
// would fabricate an exact-node match from the root node.
func TestAnOutOfRangeRegionDegrades(t *testing.T) {
	v, err := astcompare.Comparator{}.Compare(context.Background(),
		region("a.go", 9999, 9999), []byte(goSource),
		region("a.go", 4, 4), []byte(goSource))
	require.NoError(t, err)
	assert.True(t, v.Degraded)
}

// TestACRLFFileDoesNotFalselyMatchExactly pins the carriage-return strip.
// Keeping the "\r" pushes the end column past the line's real content and
// selects the enclosing block, which reads as an exact-node match on every file
// stored with Windows line endings.
func TestACRLFFileDoesNotFalselyMatchExactly(t *testing.T) {
	crlf := []byte("package p\r\n\r\nfunc Alpha(x int) int {\r\n\ty := x + 1\r\n\treturn y\r\n}\r\n")
	v, err := astcompare.Comparator{}.Compare(context.Background(),
		region("a.go", 4, 4), crlf,
		region("a.go", 5, 5), crlf)
	require.NoError(t, err)
	assert.NotEqual(t, defectlineage.MatchExactNode, v.Level,
		"two different statements must not read as the same element")
}

// TestPythonParses proves the grammar set reaches beyond Go.
func TestPythonParses(t *testing.T) {
	py := []byte("def alpha(x):\n    y = x + 1\n    return y\n\ndef beta(x):\n    return x\n")
	v, err := astcompare.Comparator{}.Compare(context.Background(),
		region("a.py", 2, 2), py,
		region("a.py", 6, 6), py)
	require.NoError(t, err)
	assert.Equal(t, defectlineage.MatchSameFile, v.Level)
	assert.False(t, v.Degraded, "Python is a supported grammar, so this is measured")
}

func TestSupportedPath(t *testing.T) {
	for _, p := range []string{"a.go", "a.js", "a.jsx", "a.ts", "a.tsx", "a.py", "a.mjs"} {
		assert.True(t, astcompare.SupportedPath(p), p)
	}
	for _, p := range []string{"a.rs", "a.java", "a.md", "a"} {
		assert.False(t, astcompare.SupportedPath(p), p)
	}
}
