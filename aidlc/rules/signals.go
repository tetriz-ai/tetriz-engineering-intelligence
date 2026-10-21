// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package rules

import (
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

func sig(key aidlcv1.SignalKey, name, collector, population string, grain aidlcv1.ShareGrain,
	bound string, status SignalStatus, observes Observes,
) SignalDef {
	return SignalDef{
		Key: key, Name: name, Collector: collector, Population: population, Grain: grain,
		CoverageBound: bound, Status: status, Observes: observes,
	}
}

// buildSignals returns the signal layer: one entry per SignalKey. Called once by
// buildV1 at init.
func buildSignals() map[aidlcv1.SignalKey]SignalDef {
	mc := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	pp := aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE
	defs := []SignalDef{
		// ---- presence numerators (people grain; observe instrumented artifacts only) ----
		sig(aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_HARNESS_SEAT_HOLDERS,
			"product_spec_harness_seat_holders", "harness_usage", "seat-holders invoking a product-spec artifact",
			pp, "", SignalStatusLive, ObservesInstrumentedOnly),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_SPEC_DRAFTING_COMMAND_SEAT_HOLDERS,
			"spec_drafting_command_seat_holders", "harness_usage", "seat-holders typing a spec-drafting command",
			pp, "command_telemetry", SignalStatusAbstains, ObservesInstrumentedOnly),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_HARNESS_SEAT_HOLDERS,
			"tech_spec_harness_seat_holders", "harness_usage", "seat-holders invoking a design-function artifact",
			pp, "", SignalStatusLive, ObservesInstrumentedOnly),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS,
			"review_harness_seat_holders", "harness_usage", "seat-holders invoking a review artifact in-session",
			pp, "", SignalStatusLive, ObservesInstrumentedOnly),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_CI_TOOL_SEAT_HOLDERS_BY_FUNCTION,
			"ci_tool_seat_holders_by_function", "harness_usage", "seat-holders invoking a CI-function artifact",
			pp, "", SignalStatusLive, ObservesInstrumentedOnly),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_HARNESS_SEAT_HOLDERS,
			"deploy_harness_seat_holders", "harness_usage", "seat-holders invoking a deploy-function artifact",
			pp, "", SignalStatusLive, ObservesInstrumentedOnly),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_OBSERVABILITY_TOOL_SEAT_HOLDERS,
			"observability_tool_seat_holders", "session_signals", "seat-holders with observability vendor tools in sessions",
			pp, "tool_inventory_coverage", SignalStatusAbstains, ObservesInstrumentedOnly),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_DEBUGGING_ARTIFACT_SEAT_HOLDERS,
			"debugging_artifact_seat_holders", "harness_usage", "seat-holders invoking a debugging artifact",
			pp, "", SignalStatusLive, ObservesInstrumentedOnly),

		// ---- merged-change numerators (merged-change grain) ----
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_PRD_PRS_MERGED_AFTER_REVIEW,
			"ai_attributed_prd_prs_merged_after_review", "pull_requests", "AI-authored merged PRD PRs a human reviewed",
			mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_HARNESS_ROUTED_PRD_PRS_MERGED,
			"harness_routed_prd_prs_merged", "pull_requests", "harness-routed merged PRD PRs",
			mc, "routed_source_coverage", SignalStatusAbstains, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_DESIGN_DOC_PRS_MERGED,
			"ai_attributed_design_doc_prs_merged", "pull_requests", "AI-authored merged design-doc PRs",
			mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_DESIGN_DOC_PRS_MERGED_AFTER_REVIEW,
			"ai_attributed_design_doc_prs_merged_after_review", "pull_requests", "AI-authored merged design-doc PRs a human reviewed",
			mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_HARNESS_ROUTED_DESIGN_DOC_PRS_MERGED,
			"harness_routed_design_doc_prs_merged", "pull_requests", "harness-routed merged design-doc PRs",
			mc, "routed_source_coverage", SignalStatusAbstains, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS,
			"ai_attributed_merged_prs", "pull_requests", "AI-authored merged feature and bugfix PRs",
			mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED,
			"ai_attributed_merged_prs_human_reviewed", "pull_requests", "AI-authored merged delivery PRs a human reviewed",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN,
			"ai_delivery_prs_low_turn", "pull_requests", "AI-authored merged delivery PRs with <=4 human turns",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_BOT_PRESENT_PRS,
			"bot_present_prs", "review_bots", "merged PRs with a bot as reviewer of record",
			mc, "captured_share", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_BOT_CLEARED_NO_HUMAN,
			"bot_cleared_no_human", "review_bots", "merged PRs a bot reviewed with zero human reviews",
			mc, "captured_share", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_TOUCHING_TESTS,
			"ai_delivery_prs_touching_tests", "pull_requests", "AI-authored merged delivery PRs touching tests",
			mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_LOW_TURN_PRS_TOUCHING_TESTS,
			"ai_low_turn_prs_touching_tests", "pull_requests", "low-turn AI-authored merged delivery PRs touching tests",
			mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_CI_PRS_MERGED,
			"ai_attributed_ci_prs_merged", "pull_requests", "AI-authored merged CI-config PRs",
			mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_CI_PRS_MERGED_AFTER_REVIEW,
			"ai_attributed_ci_prs_merged_after_review", "pull_requests", "AI-authored merged CI-config PRs a human reviewed",
			mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_RELEASE_PRS_MERGED,
			"ai_attributed_release_prs_merged", "pull_requests", "AI-authored merged release PRs",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_RELEASE_PRS_MERGED_AFTER_REVIEW,
			"ai_attributed_release_prs_merged_after_review", "pull_requests", "AI-authored merged release PRs a human reviewed",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MONITORING_PRS_MERGED,
			"ai_attributed_monitoring_prs_merged", "pull_requests", "AI-authored merged monitoring-as-code PRs",
			mc, "path_coverage", SignalStatusLive, ObservesEverything),

		// ---- denominators ----
		sig(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS,
			"seat_holders", "roster", "the seat roster", pp, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_AGENT_UNIVERSE,
			"product_spec_agent_universe", "pull_requests", "AI-attributed merged PRD PRs", mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_SEAT_LINKED_ARTIFACTS,
			"tech_spec_seat_linked_artifacts", "pull_requests", "merged design-doc PRs whose author links to a seat", mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_AGENT_UNIVERSE,
			"tech_spec_agent_universe", "pull_requests", "AI-attributed merged design-doc PRs", mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS,
			"code_seat_linked_artifacts", "pull_requests", "merged feature/bugfix PRs whose author links to a seat", mc, "work_type_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE,
			"code_agent_universe", "pull_requests", "AI-attributed merged delivery PRs", mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_RULE_APPLICABLE_PRS,
			"review_rule_applicable_prs", "review_bots", "merged PRs the bot-review rule could be evaluated on", mc, "captured_share", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_BOT_SEEN_PRS,
			"review_bot_seen_prs", "review_bots", "merged PRs a bot did review", mc, "captured_share", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_TEST_QA_SEAT_LINKED_ARTIFACTS,
			"test_qa_seat_linked_artifacts", "pull_requests", "merged delivery PRs whose author links to a seat", mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_TEST_QA_AGENT_LOW_TURN_UNIVERSE,
			"test_qa_agent_low_turn_universe", "pull_requests", "unattended AI delivery whose file list is readable", mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_CI_CD_SEAT_LINKED_ARTIFACTS,
			"ci_cd_seat_linked_artifacts", "pull_requests", "merged CI-config PRs whose author links to a seat", mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_CI_CD_AGENT_UNIVERSE,
			"ci_cd_agent_universe", "pull_requests", "AI-attributed merged CI-config PRs", mc, "path_coverage", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_SEAT_LINKED_ARTIFACTS,
			"deploy_seat_linked_artifacts", "pull_requests", "merged release PRs whose author links to a seat", mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_AGENT_UNIVERSE,
			"deploy_agent_universe", "pull_requests", "AI-attributed merged release PRs", mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_OPERATE_SEAT_LINKED_ARTIFACTS,
			"operate_seat_linked_artifacts", "pull_requests", "merged monitoring-as-code PRs whose author links to a seat", mc, "path_coverage", SignalStatusLive, ObservesEverything),

		// ---- control and validity ----
		sig(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE,
			"non_ai_baseline_review_rate", "pull_requests", "how often a stage's non-agent work is reviewed",
			mc, "reviewers_known_share", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_COMPLEXITY_SCORE,
			"complexity_score", "pull_requests", "change complexity of low-turn delivery PRs",
			mc, "complexity_coverage", SignalStatusAbstains, ObservesEverything),

		// ---- the L5 policy column ----
		sig(aidlcv1.SignalKey_SIGNAL_KEY_ENFORCED_PRACTICE_ARTIFACTS_BY_STAGE,
			"enforced_practice_artifacts_by_stage", "harness_enforcement", "admin-pushed practice artifacts classified to the stage",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE,
			"seat_holders_under_enforcement_by_stage", "harness_enforcement", "seated people the stage's enforcements target",
			pp, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE,
			"seat_holders_using_enforced_artifacts_by_stage", "harness_enforcement", "seated people under the stage's enforcement who used an enforced artifact",
			pp, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS,
			"policy_covered_ai_merged_prs", "review_policy", "AI-authored merged PRs whose work type the declared review policy covers",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS,
			"policy_met_ai_merged_prs", "review_policy", "covered AI-authored merged PRs whose approvals meet the declared requirement",
			mc, "", SignalStatusLive, ObservesEverything),

		// ---- the Plan stage ----
		sig(aidlcv1.SignalKey_SIGNAL_KEY_PLANNING_PHASE_SEAT_HOLDERS,
			"planning_phase_seat_holders", "planning", "seated people with a planning phase in the window",
			pp, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_MERGED_PRS,
			"ai_merged_prs", "pull_requests", "AI-authored merged PRs of every work type",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS,
			"planned_ai_merged_prs", "planning", "AI-authored merged PRs whose authoring session had a planning phase",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS_HUMAN_REVIEWED,
			"planned_ai_merged_prs_human_reviewed", "planning", "planned AI-authored merged PRs a person who was not the author approved",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_UNATTENDED_PLANNED_AI_MERGED_PRS,
			"unattended_planned_ai_merged_prs", "planning", "planned AI-authored merged PRs whose planning ran without a per-step human gate",
			mc, "planning_gate_recorded", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_PM_ISSUES_IN_WINDOW,
			"pm_issues_in_window", "planning", "tracker issues created in the window",
			mc, "", SignalStatusLive, ObservesEverything),

		// ---- Deploy over release branches ----
		sig(aidlcv1.SignalKey_SIGNAL_KEY_SHIPPED_BRANCH_SEAT_LINKED_PRS_MERGED,
			"shipped_branch_seat_linked_prs_merged", "pull_requests", "merged PRs into a SHIPPED-role branch whose author links to a seat",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED,
			"ai_attributed_shipped_branch_prs_merged", "pull_requests", "AI-authored merged PRs by seat-linked authors into a SHIPPED-role branch",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED_AFTER_REVIEW,
			"ai_attributed_shipped_branch_prs_merged_after_review", "pull_requests", "AI-authored merged PRs into a SHIPPED-role branch a human reviewed",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_PRODUCTION_BRANCH_PRS_DEPLOYED,
			"ai_attributed_production_branch_prs_deployed", "pull_requests", "AI-authored merges into the production-role branch confirmed deployed",
			mc, "deploy_events", SignalStatusLive, ObservesEverything),

		// ---- declared commands ----
		sig(aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_TEST_COMMAND_SESSIONS,
			"declared_test_command_sessions", "declared_commands", "seated sessions with tool use, where a declared test command could have run",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_TEST_COMMAND_GREEN_SESSIONS,
			"declared_test_command_green_sessions", "declared_commands", "sessions in which a declared test command ran and the next outcome was green",
			mc, "tool_outcomes", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_CI_COMMAND_SESSIONS,
			"declared_ci_command_sessions", "declared_commands", "seated sessions with tool use, where a declared CI command could have run",
			mc, "", SignalStatusLive, ObservesEverything),
		sig(aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_CI_COMMAND_GREEN_SESSIONS,
			"declared_ci_command_green_sessions", "declared_commands", "sessions in which a declared CI command ran and the next outcome was green",
			mc, "tool_outcomes", SignalStatusLive, ObservesEverything),
	}

	out := make(map[aidlcv1.SignalKey]SignalDef, len(defs))
	for _, d := range defs {
		out[d.Key] = d
	}
	return out
}
