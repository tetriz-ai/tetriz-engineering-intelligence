// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"strings"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// gatedIDETypes lists the IDE types the stricter attribution rules apply to:
// a branch narrows the candidate PRs but never attributes on its own, and the
// reducer may withdraw a stored match those rules do not produce.
//
// Kiro is listed because on its sessions a branch match is overwhelmingly the
// only thing behind an attribution, and the branch is shared by every session
// run in that workspace, so unrelated sessions claim the same PR. Listing
// another IDE asserts the same is true there, which is a claim its own data has
// to support.
//
// A constant rather than config: adding an IDE is a reviewed decision backed by
// that IDE's measurements. These rules delete stored matches, and removing an IDE
// from the list stops further withdrawals but restores none, so widen it one IDE
// at a time and watch the runner's matches_withdrawn count first. The list applies
// to every org at once.
//
// Keyed by the evidence contract's enum name, spelled literally because this
// package does not carry the editor enum. canonicalIDEType only upper-cases and
// trims, so "ide_type_kiro" is gated and "kiro" is not: a producer sends the enum
// name, as the localsession adapter does.
var gatedIDETypes = map[string]struct{}{
	"IDE_TYPE_KIRO": {},
}

// sessionIsGated reports whether the stricter rules govern this session.
//
// Fails open — an unknown or absent ide_type is NOT gated — so an editor this
// package does not recognise keeps attributing as an ungated editor rather than
// silently losing its attributions. The error falls on an extra match, never on a
// match silently withdrawn from a user who was never in scope.
func sessionIsGated(session *attributionv1.SessionEvidence) bool {
	if session == nil {
		return false
	}
	return IDETypeIsGated(session.GetIdeType())
}

// IDETypeIsGated reports whether the stricter branch rules apply to an editor,
// keyed on the enum name ("IDE_TYPE_KIRO"), case- and space-insensitively. Code
// that displays matches should use it to decide which branch-only links to hide.
// Fails open: an empty or unknown value, including the kebab-case "kiro", is not
// gated.
func IDETypeIsGated(ideType string) bool {
	if ideType == "" {
		return false
	}
	_, gated := gatedIDETypes[canonicalIDEType(ideType)]
	return gated
}

// canonicalIDEType folds ide_type to the form gatedIDETypes is keyed by: the
// enum name, case- and space-normalised. It is not a spelling map, so "kiro"
// becomes "KIRO" and is not gated (fail-open, per sessionIsGated).
func canonicalIDEType(ideType string) string {
	return strings.ToUpper(strings.TrimSpace(ideType))
}
