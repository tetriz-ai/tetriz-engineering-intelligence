// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

func sptr(s string) *string { return &s }

func mustReading(t *testing.T, rd *aidlc.Readings, sig aidlcv1.SignalKey, stage aidlcv1.AidlcStage) aidlc.Reading {
	t.Helper()
	r, ok := readingFor(rd, sig, stage)
	if !ok {
		t.Fatalf("no reading for %v at %v", sig, stage)
	}
	return r
}

func valueOf(t *testing.T, r aidlc.Reading) float64 {
	t.Helper()
	if r.Status != aidlc.StatusResolved || r.Value == nil {
		t.Fatalf("reading not resolved: status %v note %q", r.Status, r.Note)
	}
	return *r.Value
}

func remainderOf(r aidlc.Reading) int {
	if r.RemainderValue == nil {
		return 0
	}
	return *r.RemainderValue
}

// TestFoldEnforcementL5 pins the L5 gate and its arithmetic: a stage with no live
// admin-pushed practice enforcement is gap-blocked; while an enforcement's stage is
// unclassified every gateless stage is unreadable, not gap-blocked; an expired or
// unplaceable enforcement opens nothing; the people under an org-wide enforcement are
// the whole roster, under a team one the seated members, and a team whose members could
// not be resolved makes the stage unreadable; a person under the enforcement whose usage
// resolved to nothing is the numerator's remainder, not a miss.
func TestFoldEnforcementL5(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	plan := aidlcv1.AidlcStage_AIDLC_STAGE_PLAN
	skill := aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL
	seated := []string{"p1", "p2", "p3", "p4"}
	stageMap := map[string]aidlcv1.AidlcStage{"a-code": code, "a-review": review, "a-none": aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED}
	invs := []HarnessInvocation{
		{PersonID: "p1", ArtifactID: "a-code", Type: skill},
		{PersonID: "p9", ArtifactID: "a-code", Type: skill}, // unseated: never under enforcement
		{PersonID: "p3", ArtifactID: "a-review", Type: skill},
	}

	tests := []struct {
		name  string
		er    EnforcementRead
		check func(t *testing.T, rd *aidlc.Readings)
	}{
		{
			name: "no enforcement anywhere gap-blocks every stage",
			er:   EnforcementRead{},
			check: func(t *testing.T, rd *aidlc.Readings) {
				for _, st := range l5Stages {
					if r := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, st); r.Status != aidlc.StatusGapBlocked {
						t.Errorf("%v: status %v, want gap-blocked", st, r.Status)
					}
				}
			},
		},
		{
			name: "org-wide code enforcement: all seated under it, one using, one unresolved remainder",
			er: EnforcementRead{
				Enforcements:             []Enforcement{{Type: skill, InstalledIDs: []string{"a-code"}}},
				UnresolvedUsagePersonIDs: []string{"p2", "p1", "p9"},
			},
			check: func(t *testing.T, rd *aidlc.Readings) {
				den := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, code)
				num := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, code)
				if v := valueOf(t, den); v != 4 {
					t.Errorf("under = %v, want 4 (the seated roster)", v)
				}
				if v := valueOf(t, num); v != 1 || num.People == nil || *num.People != 1 {
					t.Errorf("using = %v people %v, want 1/1 (p1; p9 is unseated)", v, num.People)
				}
				if rem := remainderOf(num); rem != 1 {
					t.Errorf("remainder = %d, want 1 (p2 unresolved and not already using; p1 already using; p9 unseated)", rem)
				}
				if r := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, review); r.Status != aidlc.StatusGapBlocked {
					t.Errorf("review with no enforcement: status %v, want gap-blocked", r.Status)
				}
			},
		},
		{
			name: "unclassified enforcement makes the gateless stages unreadable, not gap-blocked",
			er: EnforcementRead{
				Enforcements: []Enforcement{
					{Type: skill, InstalledIDs: []string{"a-code"}},
					{Type: skill, InstalledIDs: []string{"a-unknown"}},
				},
			},
			check: func(t *testing.T, rd *aidlc.Readings) {
				if r := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, review); r.Status != aidlc.StatusNoRunRecord {
					t.Errorf("review: status %v, want no-run while an enforcement's stage is unclassified", r.Status)
				}
				if v := valueOf(t, mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, code)); v != 4 {
					t.Errorf("code still reads: under = %v, want 4", v)
				}
			},
		},
		{
			name: "expired, unplaceable, no-stage and non-practice enforcements open nothing",
			er: EnforcementRead{
				Enforcements: []Enforcement{
					{Type: skill, Expired: true, InstalledIDs: []string{"a-code"}},
					{Type: skill, Unplaceable: true},
					{Type: skill, InstalledIDs: []string{"a-none"}},
					{Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_HOOK, InstalledIDs: []string{"a-code"}},
				},
			},
			check: func(t *testing.T, rd *aidlc.Readings) {
				if r := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, code); r.Status != aidlc.StatusGapBlocked {
					t.Errorf("code: status %v note %q, want gap-blocked (nothing live opens it)", r.Status, r.Note)
				}
			},
		},
		{
			name: "team and person targets: seated members only; an unresolved team makes the stage unreadable",
			er: EnforcementRead{
				Enforcements: []Enforcement{
					{Type: skill, TeamID: sptr("t1"), InstalledIDs: []string{"a-review"}},
					{Type: skill, PersonID: sptr("p4"), InstalledIDs: []string{"a-review"}},
					{Type: skill, PersonID: sptr("p9"), InstalledIDs: []string{"a-review"}},
					{Type: skill, TeamID: sptr("t-missing"), InstalledIDs: []string{"a-code"}},
				},
				TeamMembers: map[string][]string{"t1": {"p3", "p2", "p9"}},
			},
			check: func(t *testing.T, rd *aidlc.Readings) {
				den := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, review)
				if v := valueOf(t, den); v != 3 {
					t.Errorf("review under = %v, want 3 (p2, p3 via team; p4 by person; p9 unseated)", v)
				}
				if v := valueOf(t, mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, review)); v != 1 {
					t.Errorf("review using = %v, want 1 (p3)", v)
				}
				if r := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, code); r.Status != aidlc.StatusNoRunRecord {
					t.Errorf("code targeted at an unresolved team: status %v, want no-run", r.Status)
				}
				if v := valueOf(t, mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_ENFORCED_PRACTICE_ARTIFACTS_BY_STAGE, review)); v != 3 {
					t.Errorf("review enforcement count = %v, want 3", v)
				}
				if r := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, plan); r.Status != aidlc.StatusGapBlocked {
					t.Errorf("plan: status %v, want gap-blocked", r.Status)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rd := aidlc.NewReadings()
			foldEnforcementL5(rd, seated, invs, stageMap, tt.er)
			tt.check(t, rd)
		})
	}
}

// TestFoldReviewPolicy pins the declared review policy's share: not honoured and not
// declared are gap-blocked, unreadable is a no-run, a NONE rule covers nothing, and a
// covered pull request with no captured review and no vendor reviewer is the
// numerator's remainder rather than a miss.
func TestFoldReviewPolicy(t *testing.T) {
	org := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	feature := aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE
	docs := aidlcv1.PRWorkType_PR_WORK_TYPE_DOCS
	chore := aidlcv1.PRWorkType_PR_WORK_TYPE_CHORE_DEPS
	prs := []MergedPR{
		{PRKey: "f1", WorkType: feature, AuthorIdentityID: sptr("a"), ReviewersKnown: true},
		{PRKey: "f2", WorkType: feature, AuthorIdentityID: sptr("b"), ReviewersKnown: true},
		{PRKey: "f3", WorkType: feature, AuthorIdentityID: sptr("c")}, // no reviewer list, no captured review: unobserved
		{PRKey: "f4", WorkType: feature, AuthorIdentityID: sptr("d")}, // not AI-authored: outside the population
		{PRKey: "d1", WorkType: docs, AuthorIdentityID: sptr("a"), ReviewersKnown: true},
		{PRKey: "c1", WorkType: chore, AuthorIdentityID: sptr("a"), ReviewersKnown: true},
	}
	attr := Attribution{AuthoredPRKeys: map[string]struct{}{"f1": {}, "f2": {}, "f3": {}, "d1": {}, "c1": {}}}
	facts := map[string]PRReviewFacts{
		"f1": {Captured: true, HumanNonAuthorApproved: true},
		"f2": {Captured: true, BotApproved: true},
		"d1": {Captured: true, BotApproved: true},
	}
	policy := Declarations{ReviewPolicy: map[aidlcv1.PRWorkType]ApprovalKind{
		feature: ApprovalHuman, docs: ApprovalBot, chore: ApprovalNone,
	}}

	tests := []struct {
		name       string
		state      DeclarationState
		decl       Declarations
		wantStatus aidlc.ResolveStatus
		wantCov    float64
		wantMet    float64
		wantRem    int
	}{
		{name: "not honoured is gap-blocked", state: DeclarationsOff, decl: policy, wantStatus: aidlc.StatusGapBlocked},
		{name: "unreadable is a no-run", state: DeclarationsUnreadable, decl: policy, wantStatus: aidlc.StatusNoRunRecord},
		{name: "nothing declared is gap-blocked", state: DeclarationsRead, decl: Declarations{}, wantStatus: aidlc.StatusGapBlocked},
		{name: "all NONE is gap-blocked", state: DeclarationsRead, decl: Declarations{ReviewPolicy: map[aidlcv1.PRWorkType]ApprovalKind{chore: ApprovalNone}}, wantStatus: aidlc.StatusGapBlocked},
		{name: "human and bot rules: covered 4, met 2, one unobserved", state: DeclarationsRead, decl: policy, wantStatus: aidlc.StatusResolved, wantCov: 4, wantMet: 2, wantRem: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rd := aidlc.NewReadings()
			foldReviewPolicy(rd, prs, attr, facts, tt.state, tt.decl)
			cov := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS, org)
			met := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS, org)
			if cov.Status != tt.wantStatus || met.Status != tt.wantStatus {
				t.Fatalf("statuses %v/%v, want %v", cov.Status, met.Status, tt.wantStatus)
			}
			if tt.wantStatus != aidlc.StatusResolved {
				return
			}
			if v := valueOf(t, cov); v != tt.wantCov {
				t.Errorf("covered = %v, want %v (f1 f2 f3 under HUMAN, d1 under BOT; c1 under NONE covers nothing; f4 is not AI)", v, tt.wantCov)
			}
			if v := valueOf(t, met); v != tt.wantMet {
				t.Errorf("met = %v, want %v (f1 human-approved, d1 bot-approved; f2 bot-approved under a HUMAN rule is a miss)", v, tt.wantMet)
			}
			if rem := remainderOf(met); rem != tt.wantRem {
				t.Errorf("remainder = %d, want %d (f3 unobserved)", rem, tt.wantRem)
			}
		})
	}
}

// TestFoldPlan pins the Plan signals: the planner count carries the unattributable
// phases as its remainder; AI_MERGED_PRS is org-level; L3 is gap-blocked on G2 when
// the tracker has no issue in the window; a planned pull request whose gate is unknown
// is L4's remainder.
func TestFoldPlan(t *testing.T) {
	plan := aidlcv1.AidlcStage_AIDLC_STAGE_PLAN
	org := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	prs := []MergedPR{
		{PRKey: "a1", AuthorIdentityID: sptr("a"), ReviewersKnown: true},
		{PRKey: "a2", AuthorIdentityID: sptr("b"), ReviewersKnown: true},
		{PRKey: "a3", AuthorIdentityID: sptr("c")},
		{PRKey: "a4", AuthorIdentityID: sptr("d"), ReviewersKnown: true},
		{PRKey: "h1", AuthorIdentityID: sptr("e"), ReviewersKnown: true}, // not AI
	}
	attr := Attribution{AuthoredPRKeys: map[string]struct{}{"a1": {}, "a2": {}, "a3": {}, "a4": {}}}
	facts := map[string]PRReviewFacts{"a1": {Captured: true, HumanNonAuthorApproved: true}}
	base := PlanningRead{
		PlannerPersonIDs:   map[string]struct{}{"a": {}, "b": {}},
		PlannedPRKeys:      map[string]HitlState{"a1": HitlAttended, "a2": HitlUnattended, "a3": HitlUnknown, "h1": HitlUnattended},
		ExcludedNullParent: 2,
	}

	t.Run("tracker has issues", func(t *testing.T) {
		pl := base
		pl.IssuesInWindow = 7
		rd := aidlc.NewReadings()
		foldPlan(rd, prs, attr, facts, pl)
		planners := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_PLANNING_PHASE_SEAT_HOLDERS, plan)
		if v := valueOf(t, planners); v != 2 || remainderOf(planners) != 2 {
			t.Errorf("planners = %v rem %d, want 2 rem 2", v, remainderOf(planners))
		}
		if v := valueOf(t, mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_AI_MERGED_PRS, org)); v != 4 {
			t.Errorf("AI merged = %v, want 4 (org-level)", v)
		}
		if v := valueOf(t, mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS, plan)); v != 3 {
			t.Errorf("planned = %v, want 3 (a1 a2 a3; h1 is planned but not AI)", v)
		}
		l3 := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS_HUMAN_REVIEWED, plan)
		if v := valueOf(t, l3); v != 1 || remainderOf(l3) != 1 {
			t.Errorf("planned+approved = %v rem %d, want 1 (a1) rem 1 (a3 unobserved)", v, remainderOf(l3))
		}
		l4 := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_UNATTENDED_PLANNED_AI_MERGED_PRS, plan)
		if v := valueOf(t, l4); v != 1 || remainderOf(l4) != 1 {
			t.Errorf("unattended = %v rem %d, want 1 (a2) rem 1 (a3 gate unknown)", v, remainderOf(l4))
		}
		if v := valueOf(t, mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_PM_ISSUES_IN_WINDOW, plan)); v != 7 {
			t.Errorf("issues = %v, want 7", v)
		}
	})
	t.Run("no tracker issue gap-blocks L3 only", func(t *testing.T) {
		rd := aidlc.NewReadings()
		foldPlan(rd, prs, attr, facts, base)
		if r := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS_HUMAN_REVIEWED, plan); r.Status != aidlc.StatusGapBlocked {
			t.Errorf("L3 numerator status %v, want gap-blocked", r.Status)
		}
		if v := valueOf(t, mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_UNATTENDED_PLANNED_AI_MERGED_PRS, plan)); v != 1 {
			t.Errorf("L4 still reads: unattended = %v, want 1", v)
		}
	})
}

// fakeRoles is a BranchRoles over a fixed (repo, branch) -> role table.
type fakeRoles struct {
	roles      map[string]BranchRole
	production map[string]bool
	known      bool
}

func (f fakeRoles) RoleOf(repo, branch string) BranchRole { return f.roles[repo+"/"+branch] }
func (f fakeRoles) IsProduction(repo, branch string) bool { return f.production[repo+"/"+branch] }
func (f fakeRoles) Known() bool                           { return f.known }

// TestFoldDeploy pins Deploy over the derived roles: no SHIPPED role anywhere is a
// no-run on every signal; a pull request with no repository is the denominator's
// remainder; the L4 numerator is zero with every AI merge into a production branch as
// its remainder.
func TestFoldDeploy(t *testing.T) {
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY
	prs := []MergedPR{
		{PRKey: "r1", RepositoryID: sptr("R"), BaseBranch: "release/1", AuthorSeatLinked: true, AuthorIdentityID: sptr("a"), SubmittedReview: true, ReviewersKnown: true},
		{PRKey: "r2", RepositoryID: sptr("R"), BaseBranch: "release/1", AuthorSeatLinked: true, AuthorIdentityID: sptr("b")},
		{PRKey: "m1", RepositoryID: sptr("R"), BaseBranch: "main", AuthorSeatLinked: true, AuthorIdentityID: sptr("a"), ReviewersKnown: true},
		{PRKey: "f1", RepositoryID: sptr("R"), BaseBranch: "feature/x", AuthorSeatLinked: true, AuthorIdentityID: sptr("c"), ReviewersKnown: true},
		{PRKey: "n1", BaseBranch: "release/1", AuthorSeatLinked: true, AuthorIdentityID: sptr("d")},                                                // no repository: unplaceable
		{PRKey: "h1", RepositoryID: sptr("R"), BaseBranch: "release/1", AuthorSeatLinked: true, AuthorIdentityID: sptr("e"), ReviewersKnown: true}, // not AI
	}
	attr := Attribution{AuthoredPRKeys: map[string]struct{}{"r1": {}, "r2": {}, "m1": {}, "f1": {}, "n1": {}}}
	roles := fakeRoles{
		roles:      map[string]BranchRole{"R/release/1": {Shipped: true}, "R/main": {Shipped: true, AdminAdded: true}},
		production: map[string]bool{"R/main": true},
		known:      true,
	}

	t.Run("roles known", func(t *testing.T) {
		rd := aidlc.NewReadings()
		foldDeploy(rd, prs, attr, map[string]PRReviewFacts{}, roles)
		den := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_SHIPPED_BRANCH_SEAT_LINKED_PRS_MERGED, deploy)
		if v := valueOf(t, den); v != 4 || remainderOf(den) != 1 {
			t.Errorf("shipped seat-linked = %v rem %d, want 4 (r1 r2 m1 h1) rem 1 (n1)", v, remainderOf(den))
		}
		if v := valueOf(t, mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED, deploy)); v != 3 {
			t.Errorf("AI shipped = %v, want 3 (r1 r2 m1)", v)
		}
		l3 := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED_AFTER_REVIEW, deploy)
		if v := valueOf(t, l3); v != 1 || remainderOf(l3) != 1 {
			t.Errorf("AI shipped after review = %v rem %d, want 1 (r1) rem 1 (r2 reviewers unknown)", v, remainderOf(l3))
		}
		l4 := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_PRODUCTION_BRANCH_PRS_DEPLOYED, deploy)
		if v := valueOf(t, l4); v != 0 || remainderOf(l4) != 1 {
			t.Errorf("deployed = %v rem %d, want 0 rem 1 (m1 into the production branch, unconfirmed)", v, remainderOf(l4))
		}
	})
	t.Run("no SHIPPED role anywhere", func(t *testing.T) {
		rd := aidlc.NewReadings()
		foldDeploy(rd, prs, attr, map[string]PRReviewFacts{}, fakeRoles{})
		if r := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_SHIPPED_BRANCH_SEAT_LINKED_PRS_MERGED, deploy); r.Status != aidlc.StatusNoRunRecord {
			t.Errorf("status %v, want no-run", r.Status)
		}
	})
}

// TestFoldDeclaredCommands pins the declared-command cells: off and undeclared are
// gap-blocked, unreadable is a no-run, no tool session is a no-run, and a declared
// command reads the tool sessions as the population with the numerator all remainder.
func TestFoldDeclaredCommands(t *testing.T) {
	testQA := aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA
	ciCD := aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD
	both := Declarations{TestCommands: []string{"go test ./..."}, CiCommands: []string{"make ci"}}
	tests := []struct {
		name     string
		state    DeclarationState
		decl     Declarations
		sessions int
		wantTest aidlc.ResolveStatus
		wantCI   aidlc.ResolveStatus
	}{
		{name: "off", state: DeclarationsOff, decl: both, sessions: 10, wantTest: aidlc.StatusGapBlocked, wantCI: aidlc.StatusGapBlocked},
		{name: "unreadable", state: DeclarationsUnreadable, decl: both, sessions: 10, wantTest: aidlc.StatusNoRunRecord, wantCI: aidlc.StatusNoRunRecord},
		{name: "only test declared", state: DeclarationsRead, decl: Declarations{TestCommands: []string{"pytest"}}, sessions: 10, wantTest: aidlc.StatusResolved, wantCI: aidlc.StatusGapBlocked},
		{name: "declared but no tool session", state: DeclarationsRead, decl: both, sessions: 0, wantTest: aidlc.StatusNoRunRecord, wantCI: aidlc.StatusNoRunRecord},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rd := aidlc.NewReadings()
			foldDeclaredCommands(rd, tt.state, tt.decl, tt.sessions)
			ts := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_TEST_COMMAND_GREEN_SESSIONS, testQA)
			cs := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_CI_COMMAND_GREEN_SESSIONS, ciCD)
			if ts.Status != tt.wantTest || cs.Status != tt.wantCI {
				t.Fatalf("statuses test=%v ci=%v, want %v/%v", ts.Status, cs.Status, tt.wantTest, tt.wantCI)
			}
			if tt.wantTest == aidlc.StatusResolved {
				den := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_TEST_COMMAND_SESSIONS, testQA)
				if valueOf(t, den) != float64(tt.sessions) || valueOf(t, ts) != 0 || remainderOf(ts) != tt.sessions {
					t.Errorf("test: den %v num %v rem %d, want %d / 0 / %d", *den.Value, *ts.Value, remainderOf(ts), tt.sessions, tt.sessions)
				}
			}
		})
	}
}
