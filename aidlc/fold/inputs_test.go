// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"fmt"
	"testing"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// TestFold_DuplicatePRKeysCountOnce pins that a PRKey listed more than once is one pull
// request: ten keys four times each are ten merged pull requests, not forty.
func TestFold_DuplicatePRKeysCountOnce(t *testing.T) {
	var prs []MergedPR
	authored := map[string]struct{}{}
	for k := 0; k < 10; k++ {
		key := fmt.Sprintf("pr-%d", k)
		author := fmt.Sprintf("person-%d", k)
		if k < 8 {
			authored[key] = struct{}{}
		}
		for dup := 0; dup < 4; dup++ {
			prs = append(prs, MergedPR{
				PRKey: key, AuthorIdentityID: &author, AuthorSeatLinked: true, BaseBranch: "main",
				WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE, Paths: []string{"src/app/handler.go"}, HasFileList: true,
			})
		}
	}
	r := Fold(Facts{MergedPRs: prs, Attribution: Attribution{AuthoredPRKeys: authored}})
	if r.Preconditions.MergedPRCount != 10 {
		t.Errorf("merged pull requests = %d, want 10", r.Preconditions.MergedPRCount)
	}
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	if v := valueOf(t, mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code)); v != 10 {
		t.Errorf("code seat-linked = %v, want 10", v)
	}
	if v := valueOf(t, mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code)); v != 8 {
		t.Errorf("code AI-authored = %v, want 8", v)
	}
}

// reviewPresence folds review-artifact invocations by the given people over a roster.
func reviewPresence(t *testing.T, roster, invokers []string) (holders, seats float64, people int) {
	t.Helper()
	var invs []HarnessInvocation
	for _, p := range invokers {
		invs = append(invs, HarnessInvocation{PersonID: p, ArtifactID: "rev-skill", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL})
	}
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	r := Fold(Facts{Roster: roster, Invocations: invs, StageMap: map[string]aidlcv1.AidlcStage{"rev-skill": review}})
	holders = valueOf(t, mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, review))
	seats = valueOf(t, mustReading(t, r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED))
	return holders, seats, r.Readings.StagePeople(review).Len()
}

// TestFold_PresenceIsReadOverTheRoster pins the roster as the presence population:
// invocations by people off the roster are dropped, a duplicated roster id counts once,
// and an empty id is never a person.
func TestFold_PresenceIsReadOverTheRoster(t *testing.T) {
	var roster []string
	for i := 0; i < 10; i++ {
		roster = append(roster, fmt.Sprintf("r%d", i))
	}
	if h, s, _ := reviewPresence(t, roster, []string{"x1", "x2", "x3", "x4", "x5", "x6"}); h != 0 || s != 10 {
		t.Errorf("off-roster invokers: holders %v of %v, want 0 of 10", h, s)
	}
	if h, s, _ := reviewPresence(t, []string{"a", "a", "a", "a", "a"}, []string{"a"}); h != 1 || s != 1 {
		t.Errorf("duplicated roster: holders %v of %v, want 1 of 1", h, s)
	}
	if h, s, p := reviewPresence(t, append([]string{""}, roster...), []string{"", "r1"}); h != 1 || s != 10 || p != 1 {
		t.Errorf("empty id: holders %v of %v, stage people %d, want 1 of 10 and 1", h, s, p)
	}
}

// TestPathStages_IgnoreCase pins that the stage rules read paths case-insensitively:
// Infra/main.TF is a CI/CD path, and Src/Handler_Test.go touches tests.
func TestPathStages_IgnoreCase(t *testing.T) {
	if !pathStages([]string{"Infra/main.TF"}, "main", "")[aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD] {
		t.Error("Infra/main.TF is not read as CI/CD")
	}
	if !touchesTests([]string{"Src/Handler_Test.go"}) {
		t.Error("Src/Handler_Test.go is not read as a test")
	}
}
