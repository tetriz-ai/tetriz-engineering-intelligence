// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"strings"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// ComputeMatchTier resolves the confidence tier for a single session→PR match
// row. Cursor sessions get HIGH on file-overlap because
// the producer reports definitive line-level evidence for it. Claude Code
// file-overlap stays MEDIUM — it is a narrow-window heuristic
// (TimeWindowAfterDays). Direct and branch
// matches are HIGH iff the session itself has "high" AI-attribution
// confidence (set by the session's producer). Anything else
// is MEDIUM.
//
// AI_SIGNATURE is never tiered here: a caller that attributes a pull request
// from a signature package scan of its body assigns that tier itself.
func ComputeMatchTier(method attributionv1.SessionToPRMatchMethod, ideType string, sessionConfidence string) attributionv1.PRMatchConfidenceTier {
	switch method {
	case attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF,
		attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH:
		if sessionConfidence == "high" {
			return attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_HIGH
		}
	case attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP:
		if sessionConfidence == "high" && IsCursorIDE(ideType) {
			return attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_HIGH
		}
	default:
		// AI_SIGNATURE is tiered by the aggregator (always HIGH), never here;
		// UNSPECIFIED / anything else floors to MEDIUM.
	}
	return attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_MEDIUM
}

// IsCursorIDE returns true for both the proto enum form ("IDE_TYPE_CURSOR")
// and the display form ("Cursor"), case-insensitively.
func IsCursorIDE(ideType string) bool {
	switch strings.ToLower(ideType) {
	case "cursor", "ide_type_cursor":
		return true
	}
	return false
}
