// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

//go:build astcompare

// Package astcompare measures how close two changed regions are by comparing
// their syntax trees, which is the C term in a defect-lineage score.
//
// It is behind the `astcompare` build tag because tree-sitter is a C library:
// without the tag the package is skipped entirely, so the engine builds from a
// fresh clone with no C toolchain and `CGO_ENABLED=0` keeps working. Pass
// `-tags astcompare` to build it, and a Comparator to the scorer to use it.
// Without one, the scorer answers by file coverage and marks every verdict
// degraded.

package astcompare

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/typescript/tsx"
	"github.com/smacker/go-tree-sitter/typescript/typescript"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
)

// ParserVersion identifies the comparator and its grammar set.
//
// It belongs in any cache key a caller builds over a verdict: a grammar upgrade,
// or a change here that can alter a rung, must invalidate cached verdicts rather
// than serve them stale. Bump it on any such change.
const ParserVersion = "astcompare-v1"

// Comparator measures structural proximity by parsing both revisions of a
// file and comparing the node each side changed.
//
// Source is never retained: the files are parsed in memory and discarded, and
// only the small verdict leaves. The zero value is ready to use, and one
// instance is safe for concurrent use because each comparison builds its own
// parser.
type Comparator struct{}

// Compare reports how close the candidate's change sits to the fix's.
//
// It degrades rather than failing whenever the syntax tree cannot decide — an
// unsupported grammar, a region that resolves to no element — and marks the
// result degraded so a reader never mistakes coverage for a measured match. The
// one hard error is a cancelled context, which aborts the run instead of
// quietly degrading every candidate.
// ComparatorVersion identifies this comparator and its grammar set, so a cached
// verdict is not served across an upgrade that could change it.
func (Comparator) ComparatorVersion() string { return ParserVersion }

func (Comparator) Compare(
	ctx context.Context,
	fix defectlineage.ChangedRegion, fixContent []byte,
	cand defectlineage.ChangedRegion, candContent []byte,
) (defectlineage.Verdict, error) {
	if fix.Path != cand.Path {
		return defectlineage.Verdict{Level: defectlineage.MatchDifferentFile}, nil
	}

	fixTree, fixOK, err := parse(ctx, fix.Path, fixContent)
	if err != nil {
		return defectlineage.Verdict{}, fmt.Errorf("comparing fix side: %w", err)
	}
	if fixOK {
		defer fixTree.Close()
	}
	candTree, candOK, err := parse(ctx, cand.Path, candContent)
	if err != nil {
		return defectlineage.Verdict{}, fmt.Errorf("comparing candidate side: %w", err)
	}
	if candOK {
		defer candTree.Close()
	}
	if !fixOK || !candOK {
		return degraded(), nil
	}

	fixNode := namedNodeForRegion(fixTree.RootNode(), fixContent, fix)
	candNode := namedNodeForRegion(candTree.RootNode(), candContent, cand)
	if fixNode == nil || candNode == nil {
		return degraded(), nil
	}

	fixEnc := enclosingDecl(fixNode)
	candEnc := enclosingDecl(candNode)
	v := defectlineage.Verdict{NodeType: candNode.Type(), EnclosingType: typeOrEmpty(candEnc)}

	switch {
	case fixEnc != nil && candEnc != nil &&
		declIdentity(fixEnc, fixContent) == declIdentity(candEnc, candContent):
		// The same enclosing declaration: exact when the changed elements match,
		// otherwise same-enclosing.
		if elementKey(fixNode, fixEnc, fixContent) == elementKey(candNode, candEnc, candContent) {
			v.Level = defectlineage.MatchExactNode
		} else {
			v.Level = defectlineage.MatchSameEnclosing
		}
	case fixEnc == nil && candEnc == nil:
		// Both changes are top-level, so there is no enclosing declaration and no
		// same-enclosing bucket: they either touch the same top-level element, or
		// different ones. A shared empty enclosing identity must not read as "same
		// enclosing", which would score two unrelated package-level declarations as
		// if they sat in one function.
		if elementKey(fixNode, nil, fixContent) == elementKey(candNode, nil, candContent) {
			v.Level = defectlineage.MatchExactNode
		} else {
			v.Level = defectlineage.MatchSameFile
		}
	default:
		// Different enclosing declarations, or one enclosed and one top-level.
		v.Level = defectlineage.MatchSameFile
	}
	return v, nil
}

// degraded is the same-file fallback, always flagged, for a comparison the
// syntax tree could not decide.
func degraded() defectlineage.Verdict {
	return defectlineage.Verdict{Level: defectlineage.MatchSameFile, Degraded: true}
}

// SupportedPath reports whether a grammar exists for this file's extension. A
// caller uses it to decide, before fetching a file, whether a structural verdict
// is possible at all or the candidate should go straight to coverage.
func SupportedPath(path string) bool {
	_, ok := languageForPath(path)
	return ok
}

// languageForPath returns the grammar for a path's extension. An unsupported
// extension is reported rather than parsed with the wrong grammar, so a caller
// degrades to coverage instead of reading a fabricated proximity.
func languageForPath(path string) (*sitter.Language, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return golang.GetLanguage(), true
	case ".js", ".jsx", ".mjs", ".cjs":
		return javascript.GetLanguage(), true
	case ".ts":
		return typescript.GetLanguage(), true
	case ".tsx":
		return tsx.GetLanguage(), true
	case ".py":
		return python.GetLanguage(), true
	default:
		return nil, false
	}
}

// parse builds a syntax tree for content under the grammar for path.
//
// It reports false for an unsupported extension so the caller degrades. A parse
// never fails on malformed source — tree-sitter is error-tolerant and yields a
// partial tree — so a known grammar always returns a tree.
func parse(ctx context.Context, path string, content []byte) (*sitter.Tree, bool, error) {
	lang, ok := languageForPath(path)
	if !ok {
		return nil, false, nil
	}
	p := sitter.NewParser()
	// The tree owns its own C state once parsing returns, so the parser is
	// released immediately. Without this, concurrent scoring accumulates C
	// parsers until the garbage collector happens to run their finalizers.
	defer p.Close()
	p.SetLanguage(lang)
	tree, err := p.ParseCtx(ctx, nil, content)
	if err != nil {
		// Parsing only errors on cancellation; surfacing it aborts a cancelled run
		// rather than silently degrading every candidate to coverage.
		return nil, false, fmt.Errorf("parsing %s: %w", path, err)
	}
	if tree == nil {
		return nil, false, nil
	}
	return tree, true, nil
}

// namedNodeForRegion returns the smallest named node spanning the region's
// changed lines, or nil when the region is out of bounds.
//
// It bounds the range by the start line's first non-whitespace column and the
// end line's length, not the whole-line margins, so the query matches the
// statement or declaration on those lines rather than the enclosing block: a
// range from column zero to the next line's column zero is wider than the node
// and would select its parent.
func namedNodeForRegion(root *sitter.Node, content []byte, r defectlineage.ChangedRegion) *sitter.Node {
	start, end, ok := regionPoints(content, r)
	if root == nil || !ok {
		return nil
	}
	n := root.NamedDescendantForPointRange(start, end)
	// A blank, whitespace-only or out-of-range region resolves to the whole-file
	// root rather than a real element. Returning nil makes the caller degrade,
	// instead of fabricating a rung from — or keying on the text of — the entire
	// file.
	if n == nil || nodeEqual(n, root) {
		return nil
	}
	return n
}

// regionPoints converts a 1-based inclusive line range into tree-sitter points:
// the start line's first non-whitespace column, to the end line's byte length.
// It reports false when the range falls outside the file.
func regionPoints(content []byte, r defectlineage.ChangedRegion) (sitter.Point, sitter.Point, bool) {
	if r.StartLine < 1 {
		return sitter.Point{}, sitter.Point{}, false
	}
	lines := strings.Split(string(content), "\n")
	if r.StartLine > len(lines) {
		return sitter.Point{}, sitter.Point{}, false
	}
	endLine := r.EndLine
	if endLine < r.StartLine {
		endLine = r.StartLine
	}
	if endLine > len(lines) {
		endLine = len(lines)
	}
	startText := lines[r.StartLine-1]
	startCol := len(startText) - len(strings.TrimLeft(startText, " \t"))
	start := sitter.Point{Row: uint32(r.StartLine - 1), Column: uint32(startCol)}
	// Strip a trailing carriage return before measuring the end column. Splitting
	// on "\n" keeps the "\r" of a CRLF line, which pushes the end column one byte
	// past the line's real content and selects the enclosing block instead of the
	// statement — a false exact-node match on every CRLF-stored file.
	endText := strings.TrimSuffix(lines[endLine-1], "\r")
	end := sitter.Point{Row: uint32(endLine - 1), Column: uint32(len(endText))}
	return start, end, true
}

// containerTypes are the named-declaration node types, across the supported
// grammars, that form an enclosing node: a function, method, class or named
// type. Anonymous constructs — a bare block, a struct literal, an arrow
// function — are deliberately excluded, so the enclosing node is the nearest
// named declaration and two changes in one function compare as same-enclosing.
var containerTypes = map[string]bool{
	// Go
	"function_declaration": true,
	"method_declaration":   true,
	"type_spec":            true,
	// JavaScript and TypeScript
	"class_declaration":      true,
	"method_definition":      true,
	"interface_declaration":  true,
	"type_alias_declaration": true,
	// Python
	"function_definition": true,
	"class_definition":    true,
}

// enclosingDecl returns the nearest ancestor, or the node itself, that is a
// named declaration — or nil when the change sits at the top level.
func enclosingDecl(n *sitter.Node) *sitter.Node {
	for cur := n; cur != nil; cur = cur.Parent() {
		if containerTypes[cur.Type()] {
			return cur
		}
	}
	return nil
}

// declIdentity is a declaration's stable identity across revisions: its type
// plus its declared name. A nil node, meaning a top-level change, is the empty
// string, so two top-level changes in one file compare equal at this level.
func declIdentity(n *sitter.Node, content []byte) string {
	if n == nil {
		return ""
	}
	name, ok := declName(n, content)
	if !ok {
		return n.Type()
	}
	return n.Type() + "/" + name
}

// elementKey identifies the changed element within its enclosing declaration,
// finer than the enclosing itself. It is the nearest named sub-element between
// the change and the enclosing node, by name; for a bare statement with no named
// sub-element, it is the change's structural position within the enclosing. enc
// may be nil for a top-level change, so the walk is nil-safe.
//
// Position rather than node text, because text keying fails in both directions:
// two distinct statements with identical text would collide into a false
// exact-node match, and a statement whose text the fix edited would differ
// across revisions and miss the exact-node match it deserves. Position keeps
// distinct siblings distinct and matches the same slot across revisions.
func elementKey(n *sitter.Node, enc *sitter.Node, content []byte) string {
	for cur := n; cur != nil && !nodeEqual(cur, enc); cur = cur.Parent() {
		if namedElementTypes[cur.Type()] {
			if name, ok := declName(cur, content); ok {
				return cur.Type() + "/" + name
			}
		}
	}
	return "pos:" + structuralPath(n, enc)
}

// namedElementTypes are the node types whose name is a stable cross-revision
// identity: the containers, plus finer named members such as struct fields,
// class fields, package-level declarations and variable declarators. Only these
// key by name; every other node — a call, an expression statement, an if — keys
// by structural position, so two calls to the same function are not mistaken for
// one element because they share a callee.
var namedElementTypes = map[string]bool{
	// Containers, also in containerTypes.
	"function_declaration":   true,
	"method_declaration":     true,
	"type_spec":              true,
	"class_declaration":      true,
	"method_definition":      true,
	"interface_declaration":  true,
	"type_alias_declaration": true,
	"function_definition":    true,
	"class_definition":       true,
	// Finer named members.
	"field_declaration":       true, // Go struct field
	"var_spec":                true, // Go var
	"const_spec":              true, // Go const
	"field_definition":        true, // JavaScript and TypeScript class field
	"public_field_definition": true, // TypeScript class field
	"variable_declarator":     true, // JavaScript and TypeScript variable
}

// structuralPath is the child-index path from boundary, exclusive, down to n. A
// nil boundary walks to the file root. It gives a change a position-based
// identity that is stable across revisions of one element and distinct between
// sibling elements.
func structuralPath(n *sitter.Node, boundary *sitter.Node) string {
	var parts []string
	for cur := n; cur != nil && !nodeEqual(cur, boundary); cur = cur.Parent() {
		parent := cur.Parent()
		if parent == nil {
			break
		}
		parts = append(parts, cur.Type()+"#"+strconv.Itoa(namedChildIndex(parent, cur)))
	}
	// Reverse into top-down order, so the path reads from the enclosing toward
	// the leaf.
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return strings.Join(parts, "/")
}

// namedChildIndex is child's index among parent's named children, or -1 when it
// is not among them.
func namedChildIndex(parent, child *sitter.Node) int {
	for i := 0; i < int(parent.NamedChildCount()); i++ {
		if nodeEqual(parent.NamedChild(i), child) {
			return i
		}
	}
	return -1
}

// nodeEqual is a nil-safe identity check. The binding's own Equal dereferences
// its argument, so comparing directly panics when either side is nil — which
// happens for a top-level change, whose enclosing declaration is nil.
func nodeEqual(a, b *sitter.Node) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(b)
}

// declName extracts a declaration's name: the "name" field when the grammar has
// one, otherwise the first identifier-like direct child, since grammars differ on
// whether the name is a field or a positional child.
func declName(n *sitter.Node, content []byte) (string, bool) {
	if n == nil {
		return "", false
	}
	if name := n.ChildByFieldName("name"); name != nil {
		return name.Content(content), true
	}
	for i := 0; i < int(n.NamedChildCount()); i++ {
		c := n.NamedChild(i)
		switch c.Type() {
		case "identifier", "type_identifier", "field_identifier", "property_identifier", "dotted_name":
			return c.Content(content), true
		}
	}
	return "", false
}

func typeOrEmpty(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return n.Type()
}

// Compile-time proof that this satisfies the engine's comparator contract.
var _ defectlineage.Comparator = Comparator{}
