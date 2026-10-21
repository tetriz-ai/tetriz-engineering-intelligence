// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

func gapBlockedReading() Reading { return Reading{Status: StatusGapBlocked, Note: "gate closed"} }

func cellByKey(res *Result, stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel) *Cell {
	for _, c := range res.Cells {
		if c.Stage == stage && c.Level == level {
			return c
		}
	}
	return nil
}

// TestGapBlockedReading pins the fold-to-scorer contract for a closed gate: a
// gap-blocked term abstains the cell on the rule's own gap with that gap's sentence,
// the gap is carried on the cell only when it blocks, and G4's sentence names the
// check that closed it — Code/L5 on its enforcement (whatever the honoured flag),
// Review/L5 on its declaration secondary once enforcement resolves.
func TestGapBlockedReading(t *testing.T) {
	rs := rules.ForVersion("v1")
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	plan := aidlcv1.AidlcStage_AIDLC_STAGE_PLAN
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED

	tests := []struct {
		name      string
		stage     aidlcv1.AidlcStage
		level     aidlcv1.AidlcLevel
		set       func(rd *Readings)
		honoured  bool
		wantGap   aidlcv1.InstrumentationGap
		wantWords string
	}{
		{
			name:  "code L5 with nothing enforced names the enforcement, not honoured",
			stage: code, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L5,
			set: func(rd *Readings) {
				setUnenforced(rd, code)
			},
			wantGap: aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4, wantWords: "your organisation enforces in its agent setup",
		},
		{
			name:  "code L5 with nothing enforced names the enforcement, honoured",
			stage: code, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L5,
			set: func(rd *Readings) {
				setUnenforced(rd, code)
			},
			honoured: true,
			wantGap:  aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4, wantWords: "your organisation enforces in its agent setup",
		},
		{
			name:  "review L5's declaration check closed: ours when not honoured",
			stage: review, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L5,
			set: func(rd *Readings) {
				setEnforced(rd, review)
				setReviewPolicyGapBlocked(rd)
			},
			wantGap: aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4, wantWords: "Ours:",
		},
		{
			name:  "review L5's declaration check closed: yours when honoured, so nothing is declared",
			stage: review, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L5,
			set: func(rd *Readings) {
				setEnforced(rd, review)
				setReviewPolicyGapBlocked(rd)
			},
			honoured: true,
			wantGap:  aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4, wantWords: "Yours:",
		},
		{
			name:  "plan L3 with no tracker issue is G2, the organisation's: no issues in the window",
			stage: plan, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L3,
			set: func(rd *Readings) {
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS_HUMAN_REVIEWED, plan, gapBlockedReading())
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_MERGED_PRS, unspec, resolved(40))
			},
			wantGap: aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G2, wantWords: "Yours: no issues were created in your issue tracker",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rd := NewReadings()
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, unspec, withPeople(resolved(10), 10))
			tt.set(rd)
			res := Score(rs, rd, OrgRead{DeclarationsHonoured: tt.honoured}, nil)
			c := cellByKey(res, tt.stage, tt.level)
			if c == nil {
				t.Fatal("cell not scored")
			}
			if c.State != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE || c.abstain != abGap {
				t.Errorf("state %v kind %v, want abstain(gap)", c.State, c.abstain)
			}
			if c.Gap != tt.wantGap {
				t.Errorf("gap %v, want %v", c.Gap, tt.wantGap)
			}
			if !contains(c.Reason, tt.wantWords) {
				t.Errorf("reason %q does not carry %q", c.Reason, tt.wantWords)
			}
		})
	}
}

// enforcedL5Stages are the five stages whose L5 rule reads the stage's enforced
// practice; Product spec and Review also read the review-policy declaration.
var enforcedL5Stages = []aidlcv1.AidlcStage{
	aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC,
	aidlcv1.AidlcStage_AIDLC_STAGE_PLAN,
	aidlcv1.AidlcStage_AIDLC_STAGE_CODE,
	aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW,
	aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA,
}

// setUnenforced is the fold's shape for a stage with no admin-pushed practice
// artifact enforced: all three enforcement reads gap-blocked.
func setUnenforced(rd *Readings, stage aidlcv1.AidlcStage) {
	for _, k := range []aidlcv1.SignalKey{
		aidlcv1.SignalKey_SIGNAL_KEY_ENFORCED_PRACTICE_ARTIFACTS_BY_STAGE,
		aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE,
		aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE,
	} {
		rd.Set(k, stage, gapBlockedReading())
	}
}

// setEnforced is a stage with an enforced practice that half its targets use.
func setEnforced(rd *Readings, stage aidlcv1.AidlcStage) {
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_ENFORCED_PRACTICE_ARTIFACTS_BY_STAGE, stage, resolved(1))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, stage, withPeople(resolved(10), 10))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, stage, withPeople(resolved(5), 5))
}

// setReviewPolicyGapBlocked is the review-policy fold's shape when declarations are not
// honoured, or are honoured with no review policy declared: both reads gap-blocked.
func setReviewPolicyGapBlocked(rd *Readings) {
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS, unspec, gapBlockedReading())
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS, unspec, gapBlockedReading())
}

// setReviewPolicyDeclared is the review-policy fold's shape for an honoured, declared
// policy: both reads resolve.
func setReviewPolicyDeclared(rd *Readings) {
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS, unspec, resolved(40))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS, unspec, withPeople(resolved(30), 6))
}

// TestG4_CopyFollowsTheBlockingCheck scores the shapes the fold produces
// and pins that the coverage block's G4 lines and the Review/L5 reason both name
// the check that closed the cell, and nothing else: an organisation that has
// declared an honoured review policy is never told to declare one.
func TestG4_CopyFollowsTheBlockingCheck(t *testing.T) {
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	l5 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L5
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	allUnenforced := "Your organisation enforces no practice in its agent setup for Product spec, Plan, Code, Review and Test & QA yet; once one is enforced, we read how widely it is followed."

	tests := []struct {
		name     string
		version  string
		set      func(rd *Readings)
		honoured bool
		// wantG4Lines are the G4 block lines, "list|sentence", in served order.
		wantG4Lines []string
		// reasonStage's L5 reason carries every reasonCarries substring.
		reasonStage   aidlcv1.AidlcStage
		reasonCarries []string
	}{
		{
			name:    "honoured and declared, nothing enforced: the enforcement line only",
			version: "v1",
			set: func(rd *Readings) {
				for _, st := range enforcedL5Stages {
					setUnenforced(rd, st)
				}
				setReviewPolicyDeclared(rd)
			},
			honoured:      true,
			wantG4Lines:   []string{"theirs|" + allUnenforced},
			reasonStage:   review,
			reasonCarries: []string{"Yours:", "your organisation enforces in its agent setup"},
		},
		{
			name:    "honoured and undeclared, enforced: the declare line only",
			version: "v1",
			set: func(rd *Readings) {
				for _, st := range enforcedL5Stages {
					setEnforced(rd, st)
				}
				setReviewPolicyGapBlocked(rd)
			},
			honoured:      true,
			wantG4Lines:   []string{"theirs|" + theirsG4UndeclaredSentence},
			reasonStage:   review,
			reasonCarries: []string{"Yours:", "not declared yet"},
		},
		{
			name:    "honoured, undeclared and nothing enforced: both lines, and the reason says both",
			version: "v1",
			set: func(rd *Readings) {
				for _, st := range enforcedL5Stages {
					setUnenforced(rd, st)
				}
				setReviewPolicyGapBlocked(rd)
			},
			honoured:      true,
			wantG4Lines:   []string{"theirs|" + allUnenforced, "theirs|" + theirsG4UndeclaredSentence},
			reasonStage:   review,
			reasonCarries: []string{"your organisation enforces in its agent setup", "declared review policy", "neither"},
		},
		{
			name:    "not honoured, enforced: the ours approvals line only",
			version: "v1",
			set: func(rd *Readings) {
				for _, st := range enforcedL5Stages {
					setEnforced(rd, st)
				}
				setReviewPolicyGapBlocked(rd)
			},
			wantG4Lines:   []string{"ours|" + oursG4NotHonouredSentence},
			reasonStage:   review,
			reasonCarries: []string{"Ours:", "approvals are not checked against your review policy"},
		},
	}
	g4Sentence := func(sentence string) bool {
		switch sentence {
		case oursG4NotHonouredSentence, oursG4UninstrumentedSentence, theirsG4UndeclaredSentence:
			return true
		}
		return strings.Contains(sentence, "enforces no practice in its agent setup")
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rd := NewReadings()
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, unspec, withPeople(resolved(10), 10))
			tt.set(rd)
			res := Score(rules.ForVersion(tt.version), rd, OrgRead{DeclarationsHonoured: tt.honoured}, nil)

			var got []string
			for _, it := range res.Profile.GetCoverageItems() {
				if !g4Sentence(it.GetSentence()) {
					continue
				}
				list := "ours"
				if it.GetList() == aidlcv1.MaturityCoverageList_MATURITY_COVERAGE_LIST_THEIRS {
					list = "theirs"
				}
				got = append(got, list+"|"+it.GetSentence())
			}
			if strings.Join(got, "\n") != strings.Join(tt.wantG4Lines, "\n") {
				t.Errorf("G4 lines\n got %q\nwant %q", got, tt.wantG4Lines)
			}

			c := cellByKey(res, tt.reasonStage, l5)
			if c == nil || c.Gap != aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4 {
				t.Fatalf("%s L5 is not a G4 abstain: %+v", stageKey(tt.reasonStage), c)
			}
			for _, w := range tt.reasonCarries {
				if !strings.Contains(c.Reason, w) {
					t.Errorf("reason %q does not carry %q", c.Reason, w)
				}
			}
		})
	}
}

// TestG4_DeclaredOrganisationIsNeverToldToDeclare scores an honoured, declared
// organisation across every enforcement shape and pins that no coverage line and no
// cell reason asks it to declare a review policy.
func TestG4_DeclaredOrganisationIsNeverToldToDeclare(t *testing.T) {
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	for _, version := range rules.Versions() {
		for _, enforced := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s enforced=%v", version, enforced), func(t *testing.T) {
				rd := NewReadings()
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, unspec, withPeople(resolved(10), 10))
				for _, st := range enforcedL5Stages {
					if enforced {
						setEnforced(rd, st)
					} else {
						setUnenforced(rd, st)
					}
				}
				setReviewPolicyDeclared(rd)
				res := Score(rules.ForVersion(version), rd, OrgRead{DeclarationsHonoured: true}, nil)
				for _, it := range res.Profile.GetCoverageItems() {
					if strings.Contains(strings.ToLower(it.GetSentence()), "declare") {
						t.Errorf("coverage line asks a declared organisation to declare: %q", it.GetSentence())
					}
				}
				for _, c := range res.Cells {
					if strings.Contains(strings.ToLower(c.Reason), "declare") {
						t.Errorf("%s/%s reason asks a declared organisation to declare: %q", stageKey(c.Stage), levelKey(c.Level), c.Reason)
					}
				}
			})
		}
	}
}

// TestDecidedCellCarriesNoGap proves the gap is metadata for a blocked cell only: a
// code L5 whose enforcement gate is open and whose share bands is decided with the
// gap left unspecified, so a decided cell is never listed as gap-blocked.
func TestDecidedCellCarriesNoGap(t *testing.T) {
	rs := rules.ForVersion("v1")
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(10), 10))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, code, resolved(10))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, code, withPeople(resolved(7), 7))
	res := Score(rs, rd, OrgRead{}, nil)
	c := cellByKey(res, code, aidlcv1.AidlcLevel_AIDLC_LEVEL_L5)
	if c.State != aidlcv1.CellState_CELL_STATE_MET || c.Band != aidlcv1.PrevalenceBand_PREVALENCE_BAND_ESTABLISHED {
		t.Fatalf("code L5: state %v band %v, want met(established)", c.State, c.Band)
	}
	if c.Gap != aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_UNSPECIFIED {
		t.Errorf("a decided cell carries gap %v, want unspecified", c.Gap)
	}
}

// TestSecondaryShareComposesLower pins the review L5 rule: the enforcement usage
// share and the declared policy's approval share compose as the lower band; a
// secondary share below the emerging cut denies the cell whatever the first read; a
// gap-blocked secondary abstains the cell on G4; a straddling secondary abstains on
// coverage.
func TestSecondaryShareComposesLower(t *testing.T) {
	rs := rules.ForVersion("v1")
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED

	tests := []struct {
		name    string
		covered Reading
		met     Reading
		want    string
	}{
		{name: "both established", covered: resolved(40), met: withPeople(resolved(30), 5), want: "met(established)"},
		{name: "primary established, policy emerging composes emerging", covered: resolved(40), met: withPeople(resolved(12), 5), want: "met(emerging)"},
		{name: "policy below the cut denies", covered: resolved(40), met: withPeople(resolved(4), 2), want: "not_reached"},
		{name: "policy gap-blocked abstains on the gap", covered: gapBlockedReading(), met: gapBlockedReading(), want: "abstain(gap)"},
		{name: "policy straddling abstains on coverage", covered: resolved(40), met: withPeople(withNumRemainder(resolved(6), 30), 5), want: "abstain(coverage)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rd := NewReadings()
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, unspec, withPeople(resolved(10), 10))
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, review, resolved(10))
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, review, withPeople(resolved(8), 8))
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS, unspec, tt.covered)
			rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS, unspec, tt.met)
			res := Score(rs, rd, OrgRead{}, nil)
			if got := vector(res)["review/L5"]; got != tt.want {
				t.Errorf("review/L5: got %s, want %s", got, tt.want)
			}
		})
	}
}

// TestAllUnobservedStraddleNamesItself pins the reason for a straddle in which nothing
// was observed, the shape of evidence the fold has no source for (Deploy L4's deploy
// events, the declared-command outcomes): the cell abstains on coverage and says the
// evidence is not recorded, rather than "sits on the line".
func TestAllUnobservedStraddleNamesItself(t *testing.T) {
	rs := rules.ForVersion("v1")
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, unspec, withPeople(resolved(10), 10))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_MERGED_PRS, unspec, resolved(40))
	// 30 of 40 AI merges went to the production branch, none confirmed deployed.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_PRODUCTION_BRANCH_PRS_DEPLOYED, deploy, withNumRemainder(resolved(0), 30))
	res := Score(rs, rd, OrgRead{}, nil)
	c := cellByKey(res, deploy, aidlcv1.AidlcLevel_AIDLC_LEVEL_L4)
	if got := vector(res)["deploy/L4"]; got != "abstain(coverage)" {
		t.Fatalf("deploy/L4: got %s, want abstain(coverage)", got)
	}
	if c.Reason != reasonAllUnobserved {
		t.Errorf("reason %q, want the all-unobserved sentence", c.Reason)
	}

	// A straddle with hits keeps the plain straddle sentence.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_PRODUCTION_BRANCH_PRS_DEPLOYED, deploy, withPeople(withNumRemainder(resolved(5), 25), 3))
	res = Score(rs, rd, OrgRead{}, nil)
	if c := cellByKey(res, deploy, aidlcv1.AidlcLevel_AIDLC_LEVEL_L4); c.Reason != reasonStraddle {
		t.Errorf("reason %q, want the straddle sentence", c.Reason)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestSecondaryMissBottleneck_GroundsInTheShareThatFailed pins the risk-class rule's
// copy end to end: review/L5's enforcement-usage share (primary) reads established at
// 8 of 10, but its declared-policy-approval share (secondary) reads 4 of 40, below the
// emerging cut, the "policy below the cut denies" case of
// TestSecondaryShareComposesLower. The rung_review_L5 bottleneck's Terms come from the
// secondary share, since the primary already cleared every cut, and its copy names the
// secondary's practice (the declared review policy), not the primary's.
func TestSecondaryMissBottleneck_GroundsInTheShareThatFailed(t *testing.T) {
	rs := rules.ForVersion("v1")
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, unspec, withPeople(resolved(10), 10))
	// L1, L3 and L4 established, so the stage has a met rung and L5's miss is a real
	// rung_ finding (deriveBottlenecks requires anyMet).
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, review, withPeople(resolved(9), 9))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_BOT_PRESENT_PRS, review, resolved(90))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_RULE_APPLICABLE_PRS, review, resolved(100))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_BOT_CLEARED_NO_HUMAN, review, withPeople(resolved(90), 20))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_BOT_SEEN_PRS, review, resolved(100))
	// L5: primary (enforcement usage) established at 8 of 10; secondary (declared
	// policy approval) not reached at 4 of 40, TestSecondaryShareComposesLower's
	// "policy below the cut denies" case.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, review, resolved(10))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, review, withPeople(resolved(8), 8))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS, unspec, resolved(40))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS, unspec, withPeople(resolved(4), 2))

	res := Score(rs, rd, OrgRead{}, nil)
	if got := vector(res)["review/L5"]; got != "not_reached" {
		t.Fatalf("review/L5 = %s, want not_reached", got)
	}
	c := cellByKey(res, review, aidlcv1.AidlcLevel_AIDLC_LEVEL_L5)
	if !c.SecondaryMiss {
		t.Fatal("expected Cell.SecondaryMiss on review/L5")
	}
	if c.Share == nil || c.Share.Hits != 8 || c.Share.Population != 10 {
		t.Fatalf("primary share = %+v, want 8 of 10 (the enforcement-usage read, unchanged)", c.Share)
	}
	if c.Share2 == nil || c.Share2.Hits != 4 || c.Share2.Population != 40 {
		t.Fatalf("secondary share = %+v, want 4 of 40 (the declared-policy read)", c.Share2)
	}

	var found *Bottleneck
	for i := range res.Bottlenecks {
		if res.Bottlenecks[i].ID == "rung_review_L5" {
			found = &res.Bottlenecks[i]
		}
	}
	if found == nil {
		t.Fatal("expected a rung_review_L5 bottleneck")
	}
	if !found.SecondaryMiss {
		t.Error("Bottleneck.SecondaryMiss should carry the cell's SecondaryMiss")
	}
	if found.Terms == nil || found.Terms.Hits != 4 || found.Terms.Population != 40 {
		t.Fatalf("Terms = %+v, want the secondary share's 4 of 40 — the primary (8 of 10) already cleared the cut and must not be quoted as the finding", found.Terms)
	}

	body := bottleneckBody(rs, *found)
	if !strings.Contains(body, "declared policy") {
		t.Errorf("body should ground in the secondary (declared-policy) clause, got %q", body)
	}
	if strings.Contains(body, "8 of 10") || strings.Contains(body, "organisation enforces") {
		t.Errorf("body must not quote the primary share or its clause once the secondary is what missed: %q", body)
	}
	if !strings.Contains(body, "4 of 40") {
		t.Errorf("body should quote the secondary share's real numbers, got %q", body)
	}
	moveB := moveBody(rs, *found)
	if strings.Contains(moveB, "0 more") {
		t.Errorf("move must not say 0 more once grounded in the share that actually missed: %q", moveB)
	}
}
