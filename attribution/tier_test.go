// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"testing"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"

	"github.com/stretchr/testify/assert"
)

func TestComputeMatchTier(t *testing.T) {
	tests := []struct {
		name       string
		method     attributionv1.SessionToPRMatchMethod
		ide        string
		confidence string
		want       attributionv1.PRMatchConfidenceTier
	}{
		{
			name:       "direct pr ref with high confidence is HIGH",
			method:     attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF,
			confidence: "high",
			want:       attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_HIGH,
		},
		{
			name:       "branch with high confidence is HIGH",
			method:     attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH,
			confidence: "high",
			want:       attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_HIGH,
		},
		{
			name:       "cursor file overlap with high confidence is HIGH",
			method:     attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP,
			ide:        "IDE_TYPE_CURSOR",
			confidence: "high",
			want:       attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_HIGH,
		},
		{
			name:       "CC file overlap is MEDIUM",
			method:     attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP,
			ide:        "IDE_TYPE_CLAUDE_CODE",
			confidence: "high",
			want:       attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_MEDIUM,
		},
		{
			name:       "file overlap without high confidence is MEDIUM",
			method:     attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP,
			ide:        "IDE_TYPE_CURSOR",
			confidence: "none",
			want:       attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_MEDIUM,
		},
		{
			name:       "direct pr ref without high confidence is MEDIUM",
			method:     attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF,
			confidence: "none",
			want:       attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_MEDIUM,
		},
		{
			name: "unspecified method defaults to MEDIUM",
			want: attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_MEDIUM,
		},
		{
			name:       "cursor lowercase string is treated as Cursor IDE",
			method:     attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP,
			ide:        "Cursor",
			confidence: "high",
			want:       attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_HIGH,
		},
		{
			name:       "branch with none confidence ignores ide",
			method:     attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH,
			ide:        "IDE_TYPE_CURSOR",
			confidence: "none",
			want:       attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_MEDIUM,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ComputeMatchTier(tt.method, tt.ide, tt.confidence)
			assert.Equal(t, tt.want, got)
		})
	}
}
