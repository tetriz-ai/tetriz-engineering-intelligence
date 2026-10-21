// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"strings"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// TestReasonNoPopulation_ByGrain pins the no-population reason's wording by grain.
func TestReasonNoPopulation_ByGrain(t *testing.T) {
	rs := rules.ForVersion("v1")
	if r := reasonNoPopulation(rs, aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE); strings.Contains(r, "merged") || !strings.Contains(r, "no one") {
		t.Errorf("people grain: %q", r)
	}
	if r := reasonNoPopulation(rs, aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES); !strings.Contains(r, "was merged") {
		t.Errorf("merged-change grain: %q", r)
	}
}
