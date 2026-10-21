// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// cellDefinitions is what each level means for each stage, in the reader's words.
// It is display copy, not a rule: no verdict reads it, so it lives beside the
// profile renderer rather than in the hashed ruleset, and a wording edit here needs
// no new ruleset version.
var cellDefinitions = map[aidlcv1.AidlcStage]map[aidlcv1.AidlcLevel]string{
	aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "A person writes the requirement; the AI helps phrase, tighten or critique it.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "The requirement is worked out in dialogue: the AI drafts sections and asks questions, the person decides scope and wording.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "An agent produces a complete spec or issue from a brief, and a person approves it before it becomes the spec of record.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "An agent turns raw input (a ticket, a transcript, a support thread) into an accepted spec unattended, and a person checks the result rather than co-writing it.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Spec production is chained into planning and tracking under the org's own policy, and specs are regenerated when their inputs change.",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "A person writes the design; the AI answers design questions on request.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "The design document is drafted in dialogue, section by section, the person deciding each fork.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "An agent drafts a full design document against a template or process, and a person reviews it and decides the open questions. An interactive design-doc workflow is exactly this level.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "An agent produces a design document that lands as the record with result-level review only, repeatedly and for more than one person.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Design records are generated and kept in sync with the code by coordinated agents under the org's policy; drift between design and implementation is detected, not discovered.",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_PLAN: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "A person keeps the plan; the AI suggests next steps when asked.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "The plan is built in conversation: plan mode, todo lists, the person approving each step.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "An agent produces a task breakdown and a person approves it before any of it runs. Plan approval is a routine gate rather than an occasional one.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "An agent breaks the work down and executes the breakdown unattended, resolving order and dependencies itself; a person checks the results. Draining a task breakdown to merged PRs is this level.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Sequencing across a whole programme (dependency resolution, re-planning when a slice fails) is handled by coordinated agents under the org's policy.",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_CODE: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "Completions and snippets. A person writes and decides every line.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "Changes are produced in chat and applied by a person, edit by edit.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "An agent makes a complete multi-file change; a person reviews the diff and approves before it merges.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "An agent takes a piece of work to a finished, merged change unattended; a person reviews the result at the pull request rather than steering the work.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Several agents coordinate a change across services or repositories, and the org's own policy decides which classes of change need a human author.",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "A reviewer asks the AI to explain a diff. Review itself is entirely human.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "A reviewer works through a change in dialogue with the AI, still writing the review.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "Automated review runs on changes as a matter of course and posts findings; humans read them and decide.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "Automated review gates the merge (its verdict blocks or clears without a human re-reading the change), and humans spot-check rather than read every one.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Review depth is tiered by risk class under the org's own written policy, with escalation to humans for the classes the org has named.",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "A person writes tests; the AI helps with a case or an assertion on request.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "Tests are written alongside the change in dialogue.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "An agent produces tests as part of making the change; a person reviews them with the diff.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "An agent runs the suite, diagnoses failures and fixes them unattended until it passes; a person checks the result.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "What gets tested is decided by risk and coverage policy across the pipeline, by coordinated agents.",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "A person maintains the pipeline; the AI helps read a build error.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "Pipeline configuration is edited in dialogue.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "An agent changes pipeline configuration or fixes a broken build; a person approves it.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "An agent responds to a CI failure and lands the fix unattended; a person checks the result.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "The pipeline adapts and self-heals under the org's policy, with agents coordinating.",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "Releases are manual; the AI answers questions about the process.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "A release is walked through conversationally, step by step.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "The release runs through a written, versioned procedure that an agent executes, with a person approving at the gate.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "An agent performs the release unattended once approved, and a person checks the outcome. Rollback may still be human.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Release, verification and rollback are decided by agents under the org's policy.",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "A person operates the system; the AI helps read a log or a stack trace.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "Debugging and health checks happen in dialogue with the AI.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "An agent investigates a problem and proposes a fix; a person decides and applies it.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "An agent detects, diagnoses and remediates unattended; a person reviews afterwards.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Detection, remediation and verification form a closed loop under the org's policy.",
	},
}

// cellDefinition returns the level's definition for the stage, empty for a cell the
// table does not know.
func cellDefinition(stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel) string {
	return cellDefinitions[stage][level]
}

// standIns are the cells whose rule measures a narrower, observable stand-in for the
// level the definition describes. No stream carries the orchestration the L5
// definitions name or the produced plan the Plan definitions name, so a rule that
// claimed them would be one nothing could check; these cells say what was measured
// instead. The definition served is the stand-in; every other cell serves its entry
// in cellDefinitions.
var standIns = map[aidlcv1.AidlcStage]map[aidlcv1.AidlcLevel]string{
	aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Product specs are produced under a practice your admins enforce and your people use, with approvals following your declared review policy. Measured as: people using the enforced practice, and approvals following the declared policy.",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_PLAN: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "Work is planned in an AI session and a person approves the result on merge. Measured as: planned in-session and approved on merge.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "Work is planned in an AI session and run without a per-step human gate. Measured as: planned in-session under an unattended permission mode.",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Planning runs under a practice your admins enforce and your people use. Measured as: people using the enforced planning practice.",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_CODE: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Code is written under a practice your admins enforce and your people use. Measured as: people using the enforced coding practice.",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Review runs under a practice your admins enforce and your people use, with approvals following your declared review policy. Measured as: people using the enforced review practice, and approvals following the declared policy.",
	},
	aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA: {
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Testing runs under a practice your admins enforce and your people use. Measured as: people using the enforced testing practice.",
	},
}

// cellDefinitionFor returns the definition served for a cell: the stand-in where the
// rule measures one, else the level's definition.
func cellDefinitionFor(stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel) string {
	if d, ok := standIns[stage][level]; ok {
		return d
	}
	return cellDefinition(stage, level)
}
