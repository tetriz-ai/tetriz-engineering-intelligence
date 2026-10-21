// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prworktype

import "strings"

// Layer 0 — deterministic type-axis rules.
// A known dependency-bot author → chore-deps; a cherry-pick marker → CherryPick.
// Everything else returns "" (escalate). No domain rules.

var botAuthors = map[string]bool{
	"dependabot[bot]": true,
	"renovate[bot]":   true,
}

var cherryMarkers = []string{"cherry-pick", "cherrypick"}

// classifyTypeRule returns a type slug if a Layer-0 rule fires, else "".
func classifyTypeRule(title, branch, authorLogin string) string {
	if botAuthors[strings.ToLower(strings.TrimSpace(authorLogin))] {
		return "chore-deps"
	}
	text := strings.ToLower(title + " " + branch)
	for _, m := range cherryMarkers {
		if strings.Contains(text, m) {
			return CherryPick
		}
	}
	return ""
}
