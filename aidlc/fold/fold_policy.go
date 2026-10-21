// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// The policy and planning folds: the enforcement policy column (L5), the declared
// review policy's approval share, the Plan stage over session planning phases, Deploy
// over branch roles, and the declared-command sessions. Each emits its signals for the
// stages that read them; a stage whose gate is closed gets a gap-blocked reading,
// never a zero.

// gapBlocked is the reading for a rule whose gate the fold found closed: the scorer
// abstains on the rule's own gap with that gap's sentence.
func gapBlocked(note string) aidlc.Reading {
	return aidlc.Reading{Status: aidlc.StatusGapBlocked, Note: note}
}

// l5Stages are the stages that carry an enforcement-backed L5 rule.
var l5Stages = []aidlcv1.AidlcStage{
	aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC,
	aidlcv1.AidlcStage_AIDLC_STAGE_PLAN,
	aidlcv1.AidlcStage_AIDLC_STAGE_CODE,
	aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW,
	aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA,
}

// foldEnforcementL5 emits, per stage, the people under the stage's enforcement and the
// people among them who used an enforced artifact in the window. An enforcement
// belongs to the stage its installed copies serve; one whose copies are absent from
// the stage map (or that has no installed copy) belongs to a stage we cannot name, so
// while any such enforcement exists a stage without a named enforcement reads as
// unreadable rather than as having no policy. Expired and unplaceable enforcements
// open nothing. A stage with no live enforcement is gap-blocked on G4.
func foldEnforcementL5(rd *aidlc.Readings, seated []string, invs []HarnessInvocation, stageMap map[string]aidlcv1.AidlcStage, er EnforcementRead) {
	seatedSet := map[string]bool{}
	for _, p := range seated {
		seatedSet[p] = true
	}
	// used[artifactID] = people who invoked it in the window.
	used := map[string]peopleSet{}
	for _, inv := range invs {
		if used[inv.ArtifactID] == nil {
			used[inv.ArtifactID] = newPeopleSet()
		}
		used[inv.ArtifactID][inv.PersonID] = true
	}
	unresolved := newPeopleSet()
	for _, p := range er.UnresolvedUsagePersonIDs {
		unresolved[p] = true
	}

	type liveEnf struct {
		e      Enforcement
		stages map[aidlcv1.AidlcStage]bool
	}
	var live []liveEnf
	unknownStage := 0
	for _, e := range er.Enforcements {
		if e.Expired || e.Unplaceable || !isPractice(e.Type) {
			continue
		}
		stages := map[aidlcv1.AidlcStage]bool{}
		unclassified := false
		for _, id := range e.InstalledIDs {
			st, ok := stageMap[id]
			if !ok {
				unclassified = true
				continue
			}
			if st != aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED {
				stages[st] = true
			}
		}
		if len(stages) == 0 {
			if unclassified || len(e.InstalledIDs) == 0 {
				unknownStage++ // a policy exists whose stage we cannot read yet
			}
			continue // judged as serving no stage: opens nothing, and is not unknown
		}
		live = append(live, liveEnf{e: e, stages: stages})
	}

	for _, stage := range l5Stages {
		var mine []Enforcement
		for _, l := range live {
			if l.stages[stage] {
				mine = append(mine, l.e)
			}
		}
		if len(mine) == 0 {
			var r aidlc.Reading
			if unknownStage > 0 {
				r = noRun("an enforced practice is not mapped to a stage, so this stage's policy cannot be read")
			} else {
				r = gapBlocked("no admin-pushed practice artifact is enforced for this stage")
			}
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_ENFORCED_PRACTICE_ARTIFACTS_BY_STAGE, stage, r)
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, stage, r)
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, stage, r)
			continue
		}

		under := newPeopleSet()
		teamUnresolved := false
		for _, e := range mine {
			switch {
			case e.PersonID != nil && *e.PersonID != "":
				if seatedSet[*e.PersonID] {
					under[*e.PersonID] = true
				}
			case e.TeamID != nil && *e.TeamID != "":
				members, ok := er.TeamMembers[*e.TeamID]
				if !ok {
					teamUnresolved = true
					continue
				}
				for _, m := range members {
					if seatedSet[m] {
						under[m] = true
					}
				}
			default: // org-wide
				for p := range seatedSet {
					under[p] = true
				}
			}
		}
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_ENFORCED_PRACTICE_ARTIFACTS_BY_STAGE, stage,
			denominatorReading(float64(len(mine)), nil))
		if teamUnresolved {
			r := noRun("a team this stage's enforcement targets could not be resolved")
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, stage, r)
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, stage, r)
			continue
		}
		using := newPeopleSet()
		for _, e := range mine {
			for _, id := range e.InstalledIDs {
				for p := range used[id] {
					if under[p] {
						using[p] = true
					}
				}
			}
		}
		// People under the enforcement whose usage could not be resolved to any
		// artifact may have used an enforced one: the numerator's remainder.
		remainder := 0
		for p := range unresolved {
			if under[p] && !using[p] {
				remainder++
			}
		}
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, stage,
			denominatorReading(float64(len(under)), nil))
		n := len(using)
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, stage,
			withRemainder(numeratorReading(float64(n), iptr(n), nil), remainder))
	}
}

// approvalsUnobserved reports whether a pull request's approvals could not be read: no
// review row was captured for it and the vendor payload named no reviewer either.
func approvalsUnobserved(pr *MergedPR, facts map[string]PRReviewFacts) bool {
	f, ok := facts[pr.PRKey]
	if ok && f.ApprovalsUnread {
		return true
	}
	return (!ok || !f.Captured) && !pr.ReviewersKnown
}

// reviewPolicyDeclared reports whether decl carries at least one checkable review-
// policy rule (ApprovalHuman or ApprovalBot).
func reviewPolicyDeclared(decl Declarations) bool {
	for _, k := range decl.ReviewPolicy {
		if k == ApprovalHuman || k == ApprovalBot {
			return true
		}
	}
	return false
}

// foldReviewPolicy emits the declared review policy's coverage and approval share over
// the AI-authored merged pull requests, org-level (product spec L5 and review L5 read
// the same pair). Not honoured or not declared is gap-blocked on G4; unreadable is a
// no-run, so a failed read never reads as the organisation's omission. A rule of NONE
// declares no requirement the fold can check, so it covers nothing.
func foldReviewPolicy(rd *aidlc.Readings, prs []MergedPR, attr Attribution, facts map[string]PRReviewFacts, state DeclarationState, decl Declarations) {
	org := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	set := func(r aidlc.Reading) {
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS, org, r)
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS, org, r)
	}
	switch state {
	case DeclarationsOff:
		set(gapBlocked("the organisation's declarations are not honoured"))
		return
	case DeclarationsUnreadable:
		set(noRun("the admin declarations could not be read"))
		return
	case DeclarationsRead:
	}
	if !reviewPolicyDeclared(decl) {
		set(gapBlocked("no review policy is declared"))
		return
	}
	covered, met, unobserved, unjoined := 0, 0, 0, 0
	metPeople := newPeopleSet()
	for i := range prs {
		pr := &prs[i]
		kind, ok := decl.ReviewPolicy[pr.WorkType]
		if !ok || (kind != ApprovalHuman && kind != ApprovalBot) {
			continue
		}
		if pr.PRKey == "" {
			unjoined++ // AI authorship unread: possibly covered
			continue
		}
		if _, ai := attr.AuthoredPRKeys[pr.PRKey]; !ai {
			continue
		}
		covered++
		f := facts[pr.PRKey]
		switch {
		case kind == ApprovalHuman && f.HumanNonAuthorApproved, kind == ApprovalBot && f.BotApproved:
			met++
			metPeople.add(pr.AuthorIdentityID)
		case approvalsUnobserved(pr, facts):
			unobserved++
		}
	}
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS, org, denominatorReading(float64(covered), optRemainder(unjoined)))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS, org,
		withRemainder(numeratorReading(float64(met), metPeople.count(), nil), unobserved))
}

// foldPlan emits the Plan stage's signals over the planning read and the merged pull
// requests. AI_MERGED_PRS is org-level (Deploy L4 reads it too). L3's gate is the
// tracker: no issue in the window is gap-blocked on G2. A planned pull request whose
// phases recorded no gate is L4's unread mass, so a set of planned pull requests with
// no recorded gate straddles rather than denies.
func foldPlan(rd *aidlc.Readings, prs []MergedPR, attr Attribution, facts map[string]PRReviewFacts, pl PlanningRead) {
	plan := aidlcv1.AidlcStage_AIDLC_STAGE_PLAN
	org := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED

	planners := len(pl.PlannerPersonIDs)
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PLANNING_PHASE_SEAT_HOLDERS, plan,
		withRemainder(numeratorReading(float64(planners), iptr(planners), nil), pl.ExcludedNullParent))

	var (
		ai, planned, plannedReviewed, plannedRevUnobserved, unattended, gateUnknown, unjoined int
		plannedPeople, reviewedPeople, unattendedPeople                                       = newPeopleSet(), newPeopleSet(), newPeopleSet()
	)
	for i := range prs {
		pr := &prs[i]
		if pr.PRKey == "" {
			unjoined++ // AI authorship unread: possibly in the AI population
			continue
		}
		if _, isAI := attr.AuthoredPRKeys[pr.PRKey]; !isAI {
			continue
		}
		ai++
		gate, isPlanned := pl.PlannedPRKeys[pr.PRKey]
		if !isPlanned {
			continue
		}
		planned++
		plannedPeople.add(pr.AuthorIdentityID)
		f := facts[pr.PRKey]
		switch {
		case f.HumanNonAuthorApproved:
			plannedReviewed++
			reviewedPeople.add(pr.AuthorIdentityID)
		case approvalsUnobserved(pr, facts):
			plannedRevUnobserved++
		}
		switch gate {
		case HitlUnattended:
			unattended++
			unattendedPeople.add(pr.AuthorIdentityID)
		case HitlUnknown:
			gateUnknown++
		case HitlAttended:
		}
	}

	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_MERGED_PRS, org, denominatorReading(float64(ai), optRemainder(unjoined)))
	if pl.PlannedPRKeysUnread {
		// The planned set is unread: every AI pull request may have been planned and
		// reviewed, and the planned population cannot be counted.
		if pl.IssuesInWindow == 0 {
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS_HUMAN_REVIEWED, plan,
				gapBlocked("no tracker issue was created in the window"))
		} else {
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS_HUMAN_REVIEWED, plan,
				withRemainder(numeratorReading(0, nil, nil), ai+unjoined))
		}
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PM_ISSUES_IN_WINDOW, plan, denominatorReading(float64(pl.IssuesInWindow), nil))
		unread := noRun("the planned pull requests could not be read")
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS, plan, unread)
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_UNATTENDED_PLANNED_AI_MERGED_PRS, plan, unread)
		return
	}
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PM_ISSUES_IN_WINDOW, plan, denominatorReading(float64(pl.IssuesInWindow), nil))
	if pl.IssuesInWindow == 0 {
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS_HUMAN_REVIEWED, plan,
			gapBlocked("no tracker issue was created in the window"))
	} else {
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS_HUMAN_REVIEWED, plan,
			withRemainder(numeratorReading(float64(plannedReviewed), reviewedPeople.count(), nil), plannedRevUnobserved))
	}
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS, plan, denominatorReading(float64(planned), nil))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_UNATTENDED_PLANNED_AI_MERGED_PRS, plan,
		withRemainder(numeratorReading(float64(unattended), unattendedPeople.count(), nil), gateUnknown))
}

// foldDeploy emits the Deploy signals over branch roles: a release is a merge into a
// branch whose role is shipped. Every count is over seat-linked authors, the population
// deploy L2 divides by. A pull request with no repository cannot be placed and is the
// seat-linked denominator's remainder. The deploy baseline control is the review rate on
// non-AI pull requests into a shipped branch whose reviewer list was captured. The fold reads no deploy events, so L4's
// numerator is zero with every AI merge into a production branch as the remainder. An
// organisation with no shipped role anywhere has nothing to read: every signal is a
// no-run.
func foldDeploy(rd *aidlc.Readings, prs []MergedPR, attr Attribution, facts map[string]PRReviewFacts, roles BranchRoles) {
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY
	if roles == nil || !roles.Known() {
		r := noRun("no branch in this organisation is marked as one that ships, so its releases cannot be read")
		for _, k := range []aidlcv1.SignalKey{
			aidlcv1.SignalKey_SIGNAL_KEY_SHIPPED_BRANCH_SEAT_LINKED_PRS_MERGED,
			aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED,
			aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED_AFTER_REVIEW,
			aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_PRODUCTION_BRANCH_PRS_DEPLOYED,
			aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE,
		} {
			rd.Set(k, deploy, r)
		}
		return
	}
	var (
		seatLinked, roleUnread, aiShipped, aiReviewed, aiRevUnknown, productionCandidates, aiUnjoined int
		nonAiReviewed, nonAiRevKnown                                                                  int
		aiPeople, reviewedPeople                                                                      = newPeopleSet(), newPeopleSet()
	)
	for i := range prs {
		pr := &prs[i]
		if pr.RepositoryID == nil || *pr.RepositoryID == "" {
			if pr.AuthorSeatLinked {
				roleUnread++
			}
			continue
		}
		role := roles.RoleOf(*pr.RepositoryID, pr.BaseBranch)
		if !role.Shipped {
			continue
		}
		if pr.AuthorSeatLinked {
			seatLinked++
		}
		if pr.PRKey == "" {
			if pr.AuthorSeatLinked {
				aiUnjoined++ // AI authorship unread
			}
			continue
		}
		if _, ai := attr.AuthoredPRKeys[pr.PRKey]; !ai {
			if pr.ReviewersKnown { // the non-AI baseline control
				nonAiRevKnown++
				if humanReviewed(pr, facts) {
					nonAiReviewed++
				}
			}
			continue
		}
		if !pr.AuthorSeatLinked {
			continue // deploy L2 divides by seat-linked authors only
		}
		aiShipped++
		aiPeople.add(pr.AuthorIdentityID)
		if humanReviewed(pr, facts) {
			aiReviewed++
			reviewedPeople.add(pr.AuthorIdentityID)
		} else if !pr.ReviewersKnown {
			aiRevUnknown++
		}
		if roles.IsProduction(*pr.RepositoryID, pr.BaseBranch) {
			productionCandidates++
		}
	}
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SHIPPED_BRANCH_SEAT_LINKED_PRS_MERGED, deploy,
		denominatorReading(float64(seatLinked), optRemainder(roleUnread)))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED, deploy,
		withRemainder(numeratorReading(float64(aiShipped), aiPeople.count(), nil), aiUnjoined))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED_AFTER_REVIEW, deploy,
		withRemainder(numeratorReading(float64(aiReviewed), reviewedPeople.count(), nil), aiRevUnknown))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_PRODUCTION_BRANCH_PRS_DEPLOYED, deploy,
		withRemainder(numeratorReading(0, nil, nil), productionCandidates))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, deploy, controlReading(nonAiReviewed, nonAiRevKnown))
}

// foldDeclaredCommands emits the declared test and CI command signals. The population
// is the sessions with tool use; the fold reads no command outcomes, so the numerator
// is zero with the whole population as its remainder. Not honoured or not declared is
// gap-blocked (the cells name G1); unreadable is a no-run.
func foldDeclaredCommands(rd *aidlc.Readings, state DeclarationState, decl Declarations, toolSessions int) {
	emit := func(stage aidlcv1.AidlcStage, num, den aidlcv1.SignalKey, commands []string, what string) {
		var blocked *aidlc.Reading
		switch {
		case state == DeclarationsOff:
			blocked = ptr(gapBlocked("the organisation's declarations are not honoured"))
		case state == DeclarationsUnreadable:
			blocked = ptr(noRun("the admin declarations could not be read"))
		case len(commands) == 0:
			blocked = ptr(gapBlocked("no " + what + " commands are declared"))
		case toolSessions == 0:
			blocked = ptr(noRun("no session carried a tool inventory in the window"))
		}
		if blocked != nil {
			rd.Set(num, stage, *blocked)
			rd.Set(den, stage, *blocked)
			return
		}
		rd.Set(den, stage, denominatorReading(float64(toolSessions), nil))
		rd.Set(num, stage, withRemainder(numeratorReading(0, nil, nil), toolSessions))
	}
	emit(aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA,
		aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_TEST_COMMAND_GREEN_SESSIONS,
		aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_TEST_COMMAND_SESSIONS, decl.TestCommands, "test")
	emit(aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD,
		aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_CI_COMMAND_GREEN_SESSIONS,
		aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_CI_COMMAND_SESSIONS, decl.CiCommands, "CI")
}

func ptr(r aidlc.Reading) *aidlc.Reading { return &r }
