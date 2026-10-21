// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
)

// Facts is everything the folds read for one organisation over one window. A zero
// field is read as "nothing of this kind", or as unreadable where the field says so;
// it never errors.
type Facts struct {
	// Roster is the seat-holders' person ids: the denominator of every presence
	// level and the people an organisation-wide enforcement targets. Duplicates count
	// once and an empty id is skipped.
	Roster []string
	// Invocations are the roster's AI-harness artifact uses in the window. An
	// invocation by a person not on the Roster, or with an empty person id, is dropped.
	Invocations []HarnessInvocation
	// StageMap maps an artifact id to the lifecycle stage it serves.
	// AIDLC_STAGE_UNSPECIFIED is a definitive "serves no stage"; an artifact absent
	// from the map is one whose stage is unknown, and its users widen every
	// presence level as unread mass.
	StageMap map[string]aidlcv1.AidlcStage
	// MergedPRs are the pull requests the roster merged in the window. A PRKey that
	// appears more than once is one pull request: the first occurrence is read and the
	// rest are dropped.
	MergedPRs []MergedPR
	// Attribution is the session-to-pull-request link.
	Attribution Attribution
	// HumanTurns maps a PRKey to the sum of human turns across the sessions that
	// authored it. A pull request absent from the map, or mapped to zero, has no
	// readable count: it is unread on the low-turn levels.
	HumanTurns map[string]int
	// ReviewFacts maps a PRKey to its captured review records.
	ReviewFacts map[string]PRReviewFacts
	// Observability is the operate L1 reading over session tool inventories.
	Observability ObservabilityRead
	// Enforcement is the organisation's enforced practices.
	Enforcement EnforcementRead
	// Planning is the Plan stage's evidence.
	Planning PlanningRead
	// BranchRoles resolves release branches; nil means no branch carries a role, and
	// the release-branch levels read as unreadable.
	BranchRoles BranchRoles
	// DeclarationState and Declarations are the admin's declarations.
	DeclarationState DeclarationState
	Declarations     Declarations
	// AuthorPersonIDs maps a pull-request author identity to the person id the
	// roster and the sessions use, so a person seen through a session and a merge
	// counts once among the active engineers. Nil leaves merge authors out of the
	// active engineers.
	AuthorPersonIDs map[string]string
	// AuthorPersonIDsUnresolved marks that the author mapping could not be read;
	// breadth is then left unreported rather than computed over a partial set.
	AuthorPersonIDsUnresolved bool
}

// Result is the folded facts: the readings and the org-level reads the scorer
// takes, and the preconditions a profile is published under.
type Result struct {
	Readings      *aidlc.Readings
	Org           aidlc.OrgRead
	Preconditions aidlc.Preconditions
}

// Fold folds one organisation's facts into the readings, org-level reads and
// preconditions aidlc.Score and aidlc.CheckPreconditions take.
func Fold(f Facts) Result {
	f.Roster = distinctIDs(f.Roster)
	f.Invocations = onRoster(f.Invocations, f.Roster)
	f.MergedPRs = distinctPRs(f.MergedPRs)
	f.Attribution.AuthoredPRKeys = withoutEmptyKey(f.Attribution.AuthoredPRKeys)
	f.Attribution.RoutedPRKeys = withoutEmptyKey(f.Attribution.RoutedPRKeys)
	f.HumanTurns = withoutEmptyKey(f.HumanTurns)
	f.ReviewFacts = withoutEmptyKey(f.ReviewFacts)
	f.Planning.PlannedPRKeys = withoutEmptyKey(f.Planning.PlannedPRKeys)
	if _, ok := f.Planning.PlannerPersonIDs[""]; ok {
		planners := make(map[string]struct{}, len(f.Planning.PlannerPersonIDs))
		for p := range f.Planning.PlannerPersonIDs {
			if p != "" {
				planners[p] = struct{}{}
			}
		}
		f.Planning.PlannerPersonIDs = planners
	}
	rd := aidlc.NewReadings()

	foldPresence(rd, len(f.Roster), f.Invocations, f.StageMap)
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_OBSERVABILITY_TOOL_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE,
		observabilityReading(f.Observability.SeatHolders, f.Observability.People, f.Observability.Coverage))

	pathCov, count := pathCoverage(f.MergedPRs)
	org := foldMergedChange(rd, f.MergedPRs, f.Attribution, f.HumanTurns, f.ReviewFacts, pathCov)
	foldReviewBots(rd, f.MergedPRs, f.ReviewFacts)
	org.DeclarationsHonoured = f.DeclarationState != DeclarationsOff

	foldEnforcementL5(rd, f.Roster, f.Invocations, f.StageMap, f.Enforcement)
	foldReviewPolicy(rd, f.MergedPRs, f.Attribution, f.ReviewFacts, f.DeclarationState, f.Declarations)
	foldPlan(rd, f.MergedPRs, f.Attribution, f.ReviewFacts, f.Planning)
	foldDeploy(rd, f.MergedPRs, f.Attribution, f.ReviewFacts, f.BranchRoles)
	foldDeclaredCommands(rd, f.DeclarationState, f.Declarations, f.Observability.ToolSessions)

	if f.AuthorPersonIDsUnresolved {
		org.ActiveEngineersUnresolved = true
	} else {
		org.ActiveEngineers = activeEngineers(f.Attribution.SessionPersonIDs, f.MergedPRs, f.AuthorPersonIDs).toScore()
	}

	return Result{
		Readings:      rd,
		Org:           org,
		Preconditions: aidlc.Preconditions{PathCoverage: pathCov, MergedPRCount: count},
	}
}

// distinctIDs returns ids in first-seen order with duplicates and empty ids removed.
func distinctIDs(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id != "" && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// withoutEmptyKey returns m without its "" entry, copying only when there is one, so
// no join ever matches a pull request with an empty PRKey.
func withoutEmptyKey[V any](m map[string]V) map[string]V {
	if _, ok := m[""]; !ok {
		return m
	}
	out := make(map[string]V, len(m))
	for k, v := range m {
		if k != "" {
			out[k] = v
		}
	}
	return out
}

// onRoster keeps the invocations by a person on the roster.
func onRoster(invs []HarnessInvocation, roster []string) []HarnessInvocation {
	seated := make(map[string]bool, len(roster))
	for _, p := range roster {
		seated[p] = true
	}
	out := make([]HarnessInvocation, 0, len(invs))
	for _, inv := range invs {
		if seated[inv.PersonID] {
			out = append(out, inv)
		}
	}
	return out
}

// distinctPRs keeps the first pull request for each PRKey. Pull requests with an
// empty PRKey cannot be joined to anything and are each kept.
func distinctPRs(prs []MergedPR) []MergedPR {
	seen := make(map[string]bool, len(prs))
	out := make([]MergedPR, 0, len(prs))
	for _, pr := range prs {
		if pr.PRKey != "" {
			if seen[pr.PRKey] {
				continue
			}
			seen[pr.PRKey] = true
		}
		out = append(out, pr)
	}
	return out
}

// observabilityReading is the operate L1 count. Tool inventories are a declared
// subset, so the reading is declared-partial and the level abstains rather than
// deciding; a zero coverage is no reading at all, a gap rather than a zero.
func observabilityReading(holders, people int, coverage float64) aidlc.Reading {
	if coverage <= 0 {
		return noRun("no session carried a tool inventory to read observability use from")
	}
	return declaredPartialReading(float64(holders), iptr(people))
}
