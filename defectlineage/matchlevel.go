// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage

import "context"

// MatchLevel is how close a candidate's change sits to the fix's change in the
// syntax tree. It is the structural fact a [Comparator] reports, and the input
// [ScoreC] maps to a value.
type MatchLevel int

const (
	// MatchDifferentFile means the candidate changed a different file than the
	// fix. No structural relation.
	MatchDifferentFile MatchLevel = iota
	// MatchSameFile means the same file, but the changes sit in different
	// enclosing declarations.
	MatchSameFile
	// MatchSameEnclosing means the same enclosing declaration — a function,
	// method or type — but a different element within it.
	MatchSameEnclosing
	// MatchExactNode means the changes touch the same structural element.
	MatchExactNode
)

// String returns the stable wire label for the level, the form a structural
// verdict carries.
func (l MatchLevel) String() string {
	switch l {
	case MatchDifferentFile:
		return "different_file"
	case MatchSameFile:
		return "same_file"
	case MatchSameEnclosing:
		return "same_enclosing"
	case MatchExactNode:
		return "exact_node"
	default:
		return "unspecified"
	}
}

// ParseMatchLevel reads the wire label [MatchLevel.String] produces. An
// unrecognised label yields (MatchDifferentFile, false), so a caller can tell a
// real different-file verdict from an unreadable one instead of scoring a
// mistake. [ScoreC] treats both as no context.
func ParseMatchLevel(s string) (MatchLevel, bool) {
	switch s {
	case "different_file":
		return MatchDifferentFile, true
	case "same_file":
		return MatchSameFile, true
	case "same_enclosing":
		return MatchSameEnclosing, true
	case "exact_node":
		return MatchExactNode, true
	default:
		return MatchDifferentFile, false
	}
}

// ChangedRegion is one side of a structural comparison: a file and the line
// range within it that changed.
type ChangedRegion struct {
	Path      string
	StartLine int
	EndLine   int
}

// Verdict is a structural comparison's result. It carries no source by design:
// just the rung and the candidate-side node identities that explain it, so a
// stored score is auditable without re-fetching the file.
type Verdict struct {
	// Level is the proximity rung.
	Level MatchLevel
	// NodeType is the candidate's smallest changed node type.
	NodeType string
	// EnclosingType is the candidate's enclosing declaration type, empty when
	// the change sits at the top level.
	EnclosingType string
	// Degraded is true when the comparison could not use the syntax tree — an
	// unsupported grammar, or an unparseable region on either side — and fell
	// back to a same-file verdict. It marks the rung as coverage, not a measured
	// proximity, so a reader never mistakes a degrade for a real same-file match.
	Degraded bool
}

// Comparator measures the structural proximity of a candidate's change to a
// fix's change. It is the seam a syntax-tree implementation plugs into.
//
// Parsing source needs a grammar per language, and the grammars in wide use are
// C libraries, so an implementation that really parses costs a C toolchain to
// build. This package therefore defines the contract and ships
// [CoverageComparator], which needs no parser; the parsing implementation lives
// in the wiring module, where that build cost is opt-in.
//
// An implementation receives both files' bytes and must not retain them: only
// the [Verdict] leaves it.
type Comparator interface {
	Compare(ctx context.Context, fix ChangedRegion, fixContent []byte, cand ChangedRegion, candContent []byte) (Verdict, error)
}

// CoverageComparator reports proximity by path alone: same file, or different
// file. It needs no grammar and no parser, so it is the default when none is
// configured.
//
// A same-path answer is marked [Verdict.Degraded], because it is file coverage
// rather than a measured proximity. That keeps a score honest about what it
// rests on: a candidate scored through this comparator earns the same-file rung
// and never the two stronger ones.
type CoverageComparator struct{}

// Compare reports same-file when the two regions share a path, different-file
// otherwise. It never returns an error.
func (CoverageComparator) Compare(_ context.Context, fix ChangedRegion, _ []byte, cand ChangedRegion, _ []byte) (Verdict, error) {
	if fix.Path != cand.Path {
		return Verdict{Level: MatchDifferentFile}, nil
	}
	return Verdict{Level: MatchSameFile, Degraded: true}, nil
}
