// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package rules

import (
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// buildGapRemediation declares each instrumentation gap's remediation class: what
// kind of work would let a cell the gap holds closed be read.
func buildGapRemediation() map[aidlcv1.InstrumentationGap]RemediationClass {
	return map[aidlcv1.InstrumentationGap]RemediationClass{
		aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G1: RemediationClassCapturedNotIngested,
		aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G2: RemediationClassCapturedNotClassified,
		aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G3: RemediationClassDerivableNotJoined,
		aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4: RemediationClassCapturedAndIngested,
		aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G5: RemediationClassDerivableNotJoined,
		aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G6: RemediationClassComputableNotBuilt,
		aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G7: RemediationClassGenuinelyUncaptured,
		aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G8: RemediationClassGenuinelyUncaptured,
		aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G9: RemediationClassCapturedNotIngested,
	}
}

// buildBottlenecks returns the active bottleneck derivations, in the order the
// ruleset hashes them. Every org-subject finding carries a move; the product-subject
// gap finding is not shown to the organisation, is for whoever runs the measurement,
// and carries none.
func buildBottlenecks() []BottleneckRule {
	return []BottleneckRule{
		{Kind: BottleneckKindReviewReach, Subject: "org", HasMove: true},
		{Kind: BottleneckKindAdoptionReach, Subject: "org", HasMove: true},
		{Kind: BottleneckKindRungMissed, Subject: "org", HasMove: true},
		{Kind: BottleneckKindLadderStalled, Subject: "org", HasMove: true},
		{Kind: BottleneckKindGapBlocks, Subject: "product", HasMove: false},
	}
}
