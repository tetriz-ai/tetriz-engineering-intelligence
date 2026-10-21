// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package rules

import (
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// The forty-five cell rules: 36 prevalence, 2 implied-floor, 2 judgment and 5
// not-measurable. A few prevalence rules read a stand-in narrower than the level
// definition, because nothing observable carries the definition itself: the L5
// cells read an admin-enforced practice and its use, Plan L3 and L4 read planning
// in an AI session, and Deploy reads merges into a branch whose role is shipped.
// Evidence that has no source the fold reads (deploy events, declared-command
// outcomes, a per-step planning gate) is reported as unread mass, so those cells
// abstain on coverage rather than deny.

// prevalence builds a prevalence cell rule. control adds the non-AI baseline review
// control; validity attaches the complexity context reading. peopleMin 0 omits the
// people floor.
func prevalence(stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel,
	num, den aidlcv1.SignalKey, peopleMin int, control, validity bool,
) CellRule {
	checks := []Check{{Kind: CheckKindPrevalenceShare, Signal: num, Denominator: den}}
	if peopleMin > 0 {
		checks = append(checks, Check{Kind: CheckKindPeopleFloor, PeopleMin: peopleMin})
	}
	if control {
		checks = append(checks, Check{
			Kind:   CheckKindBaselineControl,
			Signal: aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE,
		})
	}
	if validity {
		checks = append(checks, Check{
			Kind:   CheckKindContext,
			Signal: aidlcv1.SignalKey_SIGNAL_KEY_COMPLEXITY_SCORE,
		})
	}
	return CellRule{Stage: stage, Level: level, Kind: RuleKindPrevalence, Checks: checks}
}

func impliedFloor(stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel, gap aidlcv1.InstrumentationGap) CellRule {
	return CellRule{Stage: stage, Level: level, Kind: RuleKindImpliedFloor, Gap: gap}
}

func judgment(stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel, gap aidlcv1.InstrumentationGap) CellRule {
	return CellRule{Stage: stage, Level: level, Kind: RuleKindJudgment, Gap: gap}
}

func notMeasurable(stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel, gap aidlcv1.InstrumentationGap) CellRule {
	return CellRule{Stage: stage, Level: level, Kind: RuleKindNotMeasurable, Gap: gap}
}

// gated returns a prevalence rule that names the gap blocking it when its gate is
// closed: the fold reports the gate's reading as gap-blocked and the scorer abstains
// on this gap with the gap's own sentence. A decided cell carries no gap.
func gated(r CellRule, gap aidlcv1.InstrumentationGap) CellRule {
	r.Gap = gap
	return r
}

// withSecondary adds a second share whose band composes with the primary's as the
// lower of the two.
func withSecondary(r CellRule, num, den aidlcv1.SignalKey) CellRule {
	r.Checks = append(r.Checks, Check{Kind: CheckKindSecondaryShare, Signal: num, Denominator: den})
	return r
}

// buildCells returns the 45-cell rule map. Called once by buildV1 at init.
func buildCells() map[CellKey]CellRule {
	const (
		L1 = aidlcv1.AidlcLevel_AIDLC_LEVEL_L1
		L2 = aidlcv1.AidlcLevel_AIDLC_LEVEL_L2
		L3 = aidlcv1.AidlcLevel_AIDLC_LEVEL_L3
		L4 = aidlcv1.AidlcLevel_AIDLC_LEVEL_L4
		L5 = aidlcv1.AidlcLevel_AIDLC_LEVEL_L5
	)
	const (
		productSpec = aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC
		techSpec    = aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC
		plan        = aidlcv1.AidlcStage_AIDLC_STAGE_PLAN
		code        = aidlcv1.AidlcStage_AIDLC_STAGE_CODE
		review      = aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
		testQA      = aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA
		ciCD        = aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD
		deploy      = aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY
		operate     = aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE
	)
	var (
		seatHolders = aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS
		noGap       = aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_UNSPECIFIED
		g1          = aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G1
		g2          = aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G2
		g3          = aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G3
		g4          = aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4
		g6          = aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G6
		g7          = aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G7
		g8          = aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G8

		enforcedUsing = aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE
		enforcedUnder = aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE
		policyMet     = aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS
		policyCovered = aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS
	)
	// enforcementL5 is the L5 rule every G4-gated stage takes: the people using an
	// admin-pushed practice artifact for the stage over the people that enforcement
	// targets. The enforcement is the gate: without one the fold reports the total
	// as gap-blocked on G4.
	enforcementL5 := func(stage aidlcv1.AidlcStage) CellRule {
		return gated(prevalence(stage, L5, enforcedUsing, enforcedUnder, 2, false, false), g4)
	}

	rules := []CellRule{
		// product spec
		prevalence(productSpec, L1, aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_HARNESS_SEAT_HOLDERS, seatHolders, 2, false, false),
		prevalence(productSpec, L2, aidlcv1.SignalKey_SIGNAL_KEY_SPEC_DRAFTING_COMMAND_SEAT_HOLDERS, seatHolders, 2, false, false),
		prevalence(productSpec, L3, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_PRD_PRS_MERGED_AFTER_REVIEW, aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_AGENT_UNIVERSE, 2, true, false),
		prevalence(productSpec, L4, aidlcv1.SignalKey_SIGNAL_KEY_HARNESS_ROUTED_PRD_PRS_MERGED, aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_AGENT_UNIVERSE, 2, false, false),
		// the risk-class rule: enforced-practice usage and the declared review policy's approvals
		withSecondary(enforcementL5(productSpec), policyMet, policyCovered),

		// tech spec
		prevalence(techSpec, L1, aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_HARNESS_SEAT_HOLDERS, seatHolders, 2, false, false),
		prevalence(techSpec, L2, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_DESIGN_DOC_PRS_MERGED, aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_SEAT_LINKED_ARTIFACTS, 2, true, false),
		prevalence(techSpec, L3, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_DESIGN_DOC_PRS_MERGED_AFTER_REVIEW, aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_AGENT_UNIVERSE, 2, true, false),
		prevalence(techSpec, L4, aidlcv1.SignalKey_SIGNAL_KEY_HARNESS_ROUTED_DESIGN_DOC_PRS_MERGED, aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_AGENT_UNIVERSE, 2, true, false),
		notMeasurable(techSpec, L5, g6),

		// plan: planning phases as the count, the tracker's issues as L3's gate
		judgment(plan, L1, noGap),
		prevalence(plan, L2, aidlcv1.SignalKey_SIGNAL_KEY_PLANNING_PHASE_SEAT_HOLDERS, seatHolders, 2, false, false),
		gated(prevalence(plan, L3, aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS_HUMAN_REVIEWED, aidlcv1.SignalKey_SIGNAL_KEY_AI_MERGED_PRS, 2, false, false), g2),
		prevalence(plan, L4, aidlcv1.SignalKey_SIGNAL_KEY_UNATTENDED_PLANNED_AI_MERGED_PRS, aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS, 2, false, false),
		enforcementL5(plan),

		// code
		prevalence(code, L1, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, 2, false, false),
		impliedFloor(code, L2, g3),
		prevalence(code, L3, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, 2, true, false),
		prevalence(code, L4, aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, 2, false, true),
		enforcementL5(code),

		// review
		prevalence(review, L1, aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, seatHolders, 2, false, false),
		impliedFloor(review, L2, noGap),
		prevalence(review, L3, aidlcv1.SignalKey_SIGNAL_KEY_BOT_PRESENT_PRS, aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_RULE_APPLICABLE_PRS, 0, false, false),
		prevalence(review, L4, aidlcv1.SignalKey_SIGNAL_KEY_BOT_CLEARED_NO_HUMAN, aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_BOT_SEEN_PRS, 2, false, false),
		withSecondary(enforcementL5(review), policyMet, policyCovered),

		// test & QA
		judgment(testQA, L1, noGap),
		prevalence(testQA, L2, aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_TOUCHING_TESTS, aidlcv1.SignalKey_SIGNAL_KEY_TEST_QA_SEAT_LINKED_ARTIFACTS, 2, false, false),
		prevalence(testQA, L3, aidlcv1.SignalKey_SIGNAL_KEY_AI_LOW_TURN_PRS_TOUCHING_TESTS, aidlcv1.SignalKey_SIGNAL_KEY_TEST_QA_AGENT_LOW_TURN_UNIVERSE, 2, false, false),
		// the declared test commands open L4; their outcomes are unread mass, so it abstains on G1
		gated(prevalence(testQA, L4, aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_TEST_COMMAND_GREEN_SESSIONS, aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_TEST_COMMAND_SESSIONS, 2, false, false), g1),
		enforcementL5(testQA),

		// CI/CD
		prevalence(ciCD, L1, aidlcv1.SignalKey_SIGNAL_KEY_CI_TOOL_SEAT_HOLDERS_BY_FUNCTION, seatHolders, 2, false, false),
		prevalence(ciCD, L2, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_CI_PRS_MERGED, aidlcv1.SignalKey_SIGNAL_KEY_CI_CD_SEAT_LINKED_ARTIFACTS, 2, false, false),
		prevalence(ciCD, L3, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_CI_PRS_MERGED_AFTER_REVIEW, aidlcv1.SignalKey_SIGNAL_KEY_CI_CD_AGENT_UNIVERSE, 2, true, false),
		gated(prevalence(ciCD, L4, aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_CI_COMMAND_GREEN_SESSIONS, aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_CI_COMMAND_SESSIONS, 2, false, false), g1),
		notMeasurable(ciCD, L5, g1),

		// deploy: a release is a merge into a branch whose role is shipped
		prevalence(deploy, L1, aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_HARNESS_SEAT_HOLDERS, seatHolders, 2, false, false),
		prevalence(deploy, L2, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED, aidlcv1.SignalKey_SIGNAL_KEY_SHIPPED_BRANCH_SEAT_LINKED_PRS_MERGED, 2, true, false),
		prevalence(deploy, L3, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED_AFTER_REVIEW, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED, 2, true, false),
		gated(prevalence(deploy, L4, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_PRODUCTION_BRANCH_PRS_DEPLOYED, aidlcv1.SignalKey_SIGNAL_KEY_AI_MERGED_PRS, 2, false, false), g7),
		notMeasurable(deploy, L5, g7),

		// operate
		prevalence(operate, L1, aidlcv1.SignalKey_SIGNAL_KEY_OBSERVABILITY_TOOL_SEAT_HOLDERS, seatHolders, 2, false, false),
		prevalence(operate, L2, aidlcv1.SignalKey_SIGNAL_KEY_DEBUGGING_ARTIFACT_SEAT_HOLDERS, seatHolders, 2, false, false),
		prevalence(operate, L3, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MONITORING_PRS_MERGED, aidlcv1.SignalKey_SIGNAL_KEY_OPERATE_SEAT_LINKED_ARTIFACTS, 2, false, false),
		notMeasurable(operate, L4, g8),
		notMeasurable(operate, L5, g8),
	}

	out := make(map[CellKey]CellRule, len(rules))
	for _, r := range rules {
		out[CellKey{Stage: r.Stage, Level: r.Level}] = r
	}
	return out
}
