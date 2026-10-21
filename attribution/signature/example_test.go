// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package signature_test

import (
	"fmt"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/signature"
)

func ExampleScanPRDescription() {
	a := signature.ScanPRDescription("Adds a token bucket.\n\nGenerated with Claude Code")
	fmt.Println(a.IsAIAttributed, a.Sources)
	// Output: true [claude-code]
}

func ExampleScanCommitMessage() {
	a := signature.ScanCommitMessage("fix: retry on 503\n\nCo-authored-by: Copilot <copilot@example.com>")
	fmt.Println(a.IsAIAttributed, a.Sources)
	// Output: true [copilot]
}
