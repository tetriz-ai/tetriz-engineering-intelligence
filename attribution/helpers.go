// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"strings"
	"time"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// branch-gate outcome labels on the branch_gate_evaluations counter.
const (
	branchGateCorroborated = "corroborated"
	branchGateNoEvidence   = "no_evidence"
)

// effectiveStrongSignalWindowDays returns StrongSignalWindowAfterDays when it
// is configured, falling back to TimeWindowAfterDays when unset.
// Strong signals (branch, direct PR ref) use this widened window to tolerate
// normal review + merge cycles (typically 1–2 weeks).
func effectiveStrongSignalWindowDays(params Params) int {
	if d := params.StrongSignalWindowAfterDays; d > 0 {
		return d
	}
	return params.TimeWindowAfterDays
}

func extractFilesEdited(s *attributionv1.SessionEvidence) []string {
	if s == nil {
		return nil
	}
	return s.GetFilesEdited()
}

// anchorEffect classifies what the last-activity anchor did to one session's
// window, for coverage reporting. Pure; the counter that consumes it is in
// observeAnchorCoverage.
func anchorEffect(s *attributionv1.SessionEvidence, params Params) string {
	durationS := s.GetSessionDurationSeconds()
	switch {
	case durationS <= 0:
		return "unmeasurable"
	case durationS > int64(effectiveMaxSessionSpanDays(params))*secondsInADay:
		return "clamped"
	case sessionMatchAnchor(s, params).After(sessionStartDate(s)):
		return "extended"
	default:
		// Measurable, but too short to cross a calendar day — the anchor lands on
		// the start date, the same window a start-anchored session would get.
		return "none"
	}
}

// observeAnchorCoverage reports the anchor's effect across a batch.
func observeAnchorCoverage(sessions []*attributionv1.SessionEvidence, params Params, source string) {
	for _, s := range sessions {
		// Fold unrecognised editor strings into one bucket: the label is supplied
		// by the producer, and an unbounded label set would multiply series
		// without telling us anything the known editors do not.
		ide := ideTypeToSource(s.GetIdeType())
		if ide == "" {
			ide = "other"
		}
		params.obs().Count("anchor_effect", map[string]string{
			"effect": anchorEffect(s, params), "ide_type": ide, "source": source})
	}
}

// prsOpenedOrMergedOnOrBefore narrows candidates to those whose merge OR open
// date lands on or before the cutoff, so a pull request raised near the session
// but merged after a slow review is still scored without widening the window.
func prsOpenedOrMergedOnOrBefore(prs []*attributionv1.PRCandidate, cutoff time.Time) []*attributionv1.PRCandidate {
	out := make([]*attributionv1.PRCandidate, 0, len(prs))
	for _, pc := range prs {
		if pc == nil {
			continue
		}
		opened := openedAt(pc)
		if !mergedAt(pc).After(cutoff) || (!opened.IsZero() && !opened.After(cutoff)) {
			out = append(out, pc)
		}
	}
	return out
}

// EditorName folds an editor string to the bounded name the engine's metric
// labels use ("kiro", "claude-code", "cursor", ...), from the enum name or the
// display name, case-insensitively. It returns "" for an editor it does not know.
// It names a label only. The gate keys on the enum name (IDETypeIsGated).
func EditorName(ideType string) string { return ideTypeToSource(ideType) }

// ideTypeToSource folds an editor string into the bounded label set the
// coverage counters use, returning "" for anything unrecognised so the caller
// can bucket it. It accepts both the enum-name spelling and the display one,
// because the label must stay bounded whichever a producer sends. canonicalIDEType,
// which gates behaviour, accepts only the enum name.
func ideTypeToSource(ideType string) string {
	switch strings.ToLower(ideType) {
	case "claude code", "ide_type_claude_code":
		return "claude-code"
	case "cursor", "ide_type_cursor":
		return "cursor"
	case "copilot", "github copilot", "ide_type_copilot", "ide_type_github_copilot":
		return "copilot"
	case "windsurf", "ide_type_windsurf":
		return "windsurf"
	case "codeium", "ide_type_codeium":
		return "codeium"
	case "codex", "openai codex", "ide_type_codex":
		return "codex"
	case "kiro", "ide_type_kiro":
		return "kiro"
	default:
		return ""
	}
}

// buildUnmatchedAttribution records why a session reached no pull request, with
// the attempt time the retry cooldown reads.
func buildUnmatchedAttribution(reason attributionv1.SessionToPRUnmatchedReason, signalsEvaluated []string) *attributionv1.PRAttribution {
	return &attributionv1.PRAttribution{
		Unmatched: &attributionv1.PRUnmatched{
			Reason:           reason,
			LastAttemptedAt:  time.Now().UTC().Format(time.RFC3339),
			SignalsEvaluated: signalsEvaluated,
		},
	}
}
