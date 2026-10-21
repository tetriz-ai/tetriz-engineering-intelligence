// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package signature

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScanCommitMessage(t *testing.T) {
	tests := []struct {
		name           string
		message        string
		wantAttributed bool
		wantSources    []string
	}{
		{
			name:           "empty message",
			message:        "",
			wantAttributed: false,
		},
		{
			name:           "plain commit no AI",
			message:        "fix: update handler logic\n\nSigned-off-by: dev@example.com",
			wantAttributed: false,
		},
		{
			name:           "Claude Opus co-author with anthropic email",
			message:        "feat: add new endpoint\n\nCo-authored-by: Claude Opus 4.6 <noreply@anthropic.com>",
			wantAttributed: true,
			wantSources:    []string{SourceClaudeCode},
		},
		{
			name:           "Claude Sonnet co-author",
			message:        "fix: resolve bug\n\nCo-authored-by: Claude Sonnet 4.6 <noreply@anthropic.com>",
			wantAttributed: true,
			wantSources:    []string{SourceClaudeCode},
		},
		{
			name:           "Claude Opus 1M context variant",
			message:        "refactor: clean up\n\nCo-authored-by: Claude Opus 4.6 (1M context) <noreply@anthropic.com>",
			wantAttributed: true,
			wantSources:    []string{SourceClaudeCode},
		},
		{
			name:           "Claude co-author without anthropic email",
			message:        "feat: something\n\nCo-authored-by: Claude <someone@example.com>",
			wantAttributed: true,
			wantSources:    []string{SourceClaudeCode},
		},
		{
			name:           "GitHub Copilot co-author with noreply",
			message:        "feat: add feature\n\nCo-authored-by: Copilot <175728472+Copilot@users.noreply.github.com>",
			wantAttributed: true,
			wantSources:    []string{SourceCopilot},
		},
		{
			name:           "Cursor co-author",
			message:        "fix: update\n\nCo-authored-by: Cursor <cursor@example.com>",
			wantAttributed: true,
			wantSources:    []string{SourceCursor},
		},
		{
			name:           "CURSOR_SUMMARY in commit message",
			message:        "feat: add stuff\n\n<!-- CURSOR_SUMMARY -->\nSome summary\n<!-- /CURSOR_SUMMARY -->",
			wantAttributed: true,
			wantSources:    []string{SourceCursorBot},
		},
		{
			name:           "CURSOR_SUMMARY with extra whitespace",
			message:        "fix: thing\n\n<!--  CURSOR_SUMMARY  -->",
			wantAttributed: true,
			wantSources:    []string{SourceCursorBot},
		},
		{
			name:           "Codeium co-author",
			message:        "feat: add\n\nCo-authored-by: Codeium <codeium@example.com>",
			wantAttributed: true,
			wantSources:    []string{SourceCodeium},
		},
		{
			name:           "Windsurf co-author",
			message:        "fix: update\n\nCo-authored-by: Windsurf <windsurf@example.com>",
			wantAttributed: true,
			wantSources:    []string{SourceWindsurf},
		},
		{
			name:           "case insensitive co-authored-by",
			message:        "feat: add\n\nco-authored-by: claude opus 4.6 <noreply@anthropic.com>",
			wantAttributed: true,
			wantSources:    []string{SourceClaudeCode},
		},
		{
			name:           "CO-AUTHORED-BY uppercase",
			message:        "feat: add\n\nCO-AUTHORED-BY: COPILOT <copilot@example.com>",
			wantAttributed: true,
			wantSources:    []string{SourceCopilot},
		},
		{
			name:           "multiple AI tools in one commit",
			message:        "feat: collaborative\n\nCo-authored-by: Claude Opus 4.6 <noreply@anthropic.com>\nCo-authored-by: Copilot <175728472+Copilot@users.noreply.github.com>",
			wantAttributed: true,
			wantSources:    []string{SourceClaudeCode, SourceCopilot},
		},
		// False positive guards.
		{
			name:           "human co-author should not match",
			message:        "feat: add\n\nCo-authored-by: John Doe <john@example.com>",
			wantAttributed: false,
		},
		{
			name:           "AI or GPT in message should not match",
			message:        "feat: use AI to generate GPT prompts\n\nThis commit improves AI usage.",
			wantAttributed: false,
		},
		{
			name:           "word 'claude' in body but not co-author",
			message:        "fix: update claude's config file\n\nFixed a path issue.",
			wantAttributed: false,
		},
		{
			name:           "word 'copilot' in commit title but not co-author",
			message:        "docs: add copilot setup instructions",
			wantAttributed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ScanCommitMessage(tt.message)
			assert.Equal(t, tt.wantAttributed, result.IsAIAttributed, "IsAIAttributed")
			assert.Equal(t, tt.wantSources, result.Sources, "Sources")
		})
	}
}

func TestScanPRDescription(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantAttributed bool
		wantSources    []string
	}{
		{
			name:           "empty body",
			body:           "",
			wantAttributed: false,
		},
		{
			name:           "plain PR description",
			body:           "## Summary\nFixed a bug in the handler.\n\n## Test Plan\n- Manual testing",
			wantAttributed: false,
		},
		{
			name:           "Cursor Bugbot summary block",
			body:           "## Summary\nSome changes.\n\n<!-- CURSOR_SUMMARY -->\n---\n> Changes the retry policy.\n> Written by [Cursor Bugbot](https://cursor.com/dashboard?tab=bugbot) for commit abc123.\n<!-- /CURSOR_SUMMARY -->",
			wantAttributed: true,
			wantSources:    []string{SourceCursorBot},
		},
		{
			name:           "Written by Cursor Bugbot",
			body:           "Some PR description.\n\nWritten by Cursor Bugbot for commit abc123.",
			wantAttributed: true,
			wantSources:    []string{SourceCursorBot},
		},
		{
			name:           "Claude Code footer with emoji",
			body:           "## Summary\nAdded feature.\n\n🤖 Generated with [Claude Code](https://claude.com/claude-code)",
			wantAttributed: true,
			wantSources:    []string{SourceClaudeCode},
		},
		{
			name:           "Claude Code footer markdown link no emoji",
			body:           "## Summary\nFix bug.\n\nGenerated with [Claude Code](https://claude.com/claude-code)",
			wantAttributed: true,
			wantSources:    []string{SourceClaudeCode},
		},
		{
			name:           "Generated with Claude Code text",
			body:           "## Summary\nFix bug.\n\nGenerated with Claude Code",
			wantAttributed: true,
			wantSources:    []string{SourceClaudeCode},
		},
		{
			name:           "Generated by Copilot",
			body:           "This PR was generated by Copilot.\n\n## Changes\n- Updated handler",
			wantAttributed: true,
			wantSources:    []string{SourceCopilot},
		},
		{
			name:           "Copilot co-author in PR body",
			body:           "Fix feature\n\nCo-authored-by: Copilot <175728472+Copilot@users.noreply.github.com>",
			wantAttributed: true,
			wantSources:    []string{SourceCopilot},
		},
		{
			name:           "multiple tools in PR description",
			body:           "## Summary\n\n🤖 Generated with [Claude Code](https://claude.com/claude-code)\n\n<!-- CURSOR_SUMMARY -->\nSome summary\n<!-- /CURSOR_SUMMARY -->",
			wantAttributed: true,
			wantSources:    []string{SourceCursorBot, SourceClaudeCode},
		},
		// False positive guards.
		{
			name:           "mention of cursor in text should not match",
			body:           "Move the cursor to the right position before editing.",
			wantAttributed: false,
		},
		{
			name:           "mention of claude in text should not match",
			body:           "Talked to Claude about the design. He said it's fine.",
			wantAttributed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ScanPRDescription(tt.body)
			assert.Equal(t, tt.wantAttributed, result.IsAIAttributed, "IsAIAttributed")
			assert.Equal(t, tt.wantSources, result.Sources, "Sources")
		})
	}
}

// ScanReviewBody re-uses the same regex set as ScanPRDescription —
// review bodies and PR descriptions share the AI tool footer vocabulary.
// This test is intentionally a thin variant of TestScanPRDescription.
func TestScanReviewBody(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantAttributed bool
		wantSources    []string
	}{
		{
			name:           "empty body",
			body:           "",
			wantAttributed: false,
		},
		{
			name:           "human review",
			body:           "LGTM, please add a unit test before merging.",
			wantAttributed: false,
		},
		{
			name:           "Claude Code footer with emoji",
			body:           "Fixed the off-by-one.\n\n🤖 Generated with [Claude Code](https://claude.com/claude-code)",
			wantAttributed: true,
			wantSources:    []string{SourceClaudeCode},
		},
		{
			name:           "Generated by Copilot in review",
			body:           "Generated by Copilot",
			wantAttributed: true,
			wantSources:    []string{SourceCopilot},
		},
		{
			name:           "Cursor co-author in review body",
			body:           "Co-authored-by: Cursor <cursor@example.com>",
			wantAttributed: true,
			wantSources:    []string{SourceCursor},
		},
		{
			name:           "Codeium co-author",
			body:           "Co-authored-by: Codeium <codeium@example.com>",
			wantAttributed: true,
			wantSources:    []string{SourceCodeium},
		},
		// False-positive guards — these mirror the PR description guards.
		{
			name:           "mention of cursor in text should not match",
			body:           "Move your cursor before the close brace.",
			wantAttributed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ScanReviewBody(tt.body)
			assert.Equal(t, tt.wantAttributed, result.IsAIAttributed, "IsAIAttributed")
			assert.Equal(t, tt.wantSources, result.Sources, "Sources")
		})
	}
}

func TestMergeAttributions(t *testing.T) {
	tests := []struct {
		name           string
		attrs          []AIAttribution
		wantAttributed bool
		wantSources    []string
	}{
		{
			name:           "no attributions",
			attrs:          nil,
			wantAttributed: false,
		},
		{
			name: "single attribution",
			attrs: []AIAttribution{
				{IsAIAttributed: true, Sources: []string{SourceClaudeCode}},
			},
			wantAttributed: true,
			wantSources:    []string{SourceClaudeCode},
		},
		{
			name: "merge deduplicates",
			attrs: []AIAttribution{
				{IsAIAttributed: true, Sources: []string{SourceClaudeCode, SourceCopilot}},
				{IsAIAttributed: true, Sources: []string{SourceCopilot, SourceCursorBot}},
			},
			wantAttributed: true,
			wantSources:    []string{SourceClaudeCode, SourceCopilot, SourceCursorBot},
		},
		{
			name: "merge with empty",
			attrs: []AIAttribution{
				{IsAIAttributed: false},
				{IsAIAttributed: true, Sources: []string{SourceCopilot}},
			},
			wantAttributed: true,
			wantSources:    []string{SourceCopilot},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MergeAttributions(tt.attrs...)
			assert.Equal(t, tt.wantAttributed, result.IsAIAttributed, "IsAIAttributed")
			assert.Equal(t, tt.wantSources, result.Sources, "Sources")
		})
	}
}

func TestDeduplicateSources(t *testing.T) {
	assert.Nil(t, DeduplicateSources(nil))
	assert.Nil(t, DeduplicateSources([]string{}))
	assert.Equal(t, []string{"a", "b"}, DeduplicateSources([]string{"a", "b", "a"}))
	assert.Equal(t, []string{"x"}, DeduplicateSources([]string{"x", "x", "x"}))
	assert.Nil(t, DeduplicateSources([]string{"", " ", "  "}))
}
