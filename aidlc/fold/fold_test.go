// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"fmt"
	"testing"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
)

// TestFold_EmptyFactsAbstainNeverZero pins that folding no facts at all yields no
// confident zero: the profile-facing readings abstain, the preconditions report no
// merged pull request, and breadth has no active engineers to read.
func TestFold_EmptyFactsAbstainNeverZero(t *testing.T) {
	r := Fold(Facts{})
	if r.Preconditions.MergedPRCount != 0 || r.Preconditions.PathCoverage != 1 {
		t.Errorf("preconditions = %+v, want 0 merged and a vacuous coverage of 1", r.Preconditions)
	}
	if r.Org.HasAdoptionCohort {
		t.Error("an empty fold claims an adoption cohort")
	}
	if r.Org.DeclarationsHonoured {
		t.Error("the zero DeclarationState is DeclarationsOff, which is not honoured")
	}
	// Observability with zero coverage is a gap, not a zero.
	if rd, ok := readingFor(r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_OBSERVABILITY_TOOL_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE); !ok || rd.Status != aidlc.StatusNoRunRecord {
		t.Errorf("observability reading = %+v, want no-run", rd)
	}
	// No branch roles: the release-branch signals are a no-run.
	if rd, ok := readingFor(r.Readings, aidlcv1.SignalKey_SIGNAL_KEY_SHIPPED_BRANCH_SEAT_LINKED_PRS_MERGED, aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY); !ok || rd.Status != aidlc.StatusNoRunRecord {
		t.Errorf("shipped-branch reading = %+v, want no-run", rd)
	}
}

// TestFold_AuthorMappingUnresolvedLeavesBreadthUnreported pins that an unreadable
// author mapping marks the active engineers unresolved rather than counting a
// partial set.
func TestFold_AuthorMappingUnresolvedLeavesBreadthUnreported(t *testing.T) {
	r := Fold(Facts{Attribution: Attribution{SessionPersonIDs: []string{"p1"}}, AuthorPersonIDsUnresolved: true})
	if !r.Org.ActiveEngineersUnresolved || r.Org.ActiveEngineers != nil {
		t.Errorf("org = %+v, want active engineers unresolved and unset", r.Org)
	}
	r = Fold(Facts{Attribution: Attribution{SessionPersonIDs: []string{"p1"}}})
	if r.Org.ActiveEngineersUnresolved || r.Org.ActiveEngineers.Len() != 1 {
		t.Errorf("org = %+v, want one active engineer", r.Org)
	}
}

// TestFoldReviewBots_UnsupportedSourceLeftOut pins BotDetectionUnsupported: a pull
// request whose source cannot flag a bot is in neither side of any bot share, even
// when its review records were captured.
func TestFoldReviewBots_UnsupportedSourceLeftOut(t *testing.T) {
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	prs := []MergedPR{
		{PRKey: "a", AuthorIdentityID: sptr("x")},
		{PRKey: "b", AuthorIdentityID: sptr("y"), BotDetectionUnsupported: true},
		{PRKey: "c", AuthorIdentityID: sptr("z"), BotDetectionUnsupported: true},
	}
	facts := map[string]PRReviewFacts{
		"a": {Captured: true, BotPresent: true},
		"b": {Captured: true},
		// c has no captured record: unsupported, so not even the unread remainder.
	}
	rd := aidlc.NewReadings()
	foldReviewBots(rd, prs, facts)
	den := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_RULE_APPLICABLE_PRS, review)
	if v := valueOf(t, den); v != 1 || remainderOf(den) != 0 {
		t.Errorf("rule-applicable = %v rem %d, want 1 (a) rem 0 (b and c left out)", v, remainderOf(den))
	}
	if v := valueOf(t, mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_BOT_PRESENT_PRS, review)); v != 1 {
		t.Errorf("bot present = %v, want 1", v)
	}
}

// TestPathCoverage pins the precondition input: the share of merged pull requests
// that carry a file list, and a vacuous 1.0 over none.
func TestPathCoverage(t *testing.T) {
	prs := []MergedPR{{HasFileList: true}, {HasFileList: true}, {HasFileList: true}, {}}
	if got, n := pathCoverage(prs); got != 0.75 || n != 4 {
		t.Errorf("pathCoverage = %v over %d, want 0.75 over 4", got, n)
	}
	if got, n := pathCoverage(nil); got != 1 || n != 0 {
		t.Errorf("pathCoverage(nil) = %v over %d, want 1 over 0", got, n)
	}
}

// TestFoldMerged_LowTurnIsOneToFourHumanTurns pins the code L4 count: an AI delivery
// pull request is low-turn at one to four human turns; five is not, and zero (no
// readable count) is the numerator's unread remainder.
func TestFoldMerged_LowTurnIsOneToFourHumanTurns(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	turns := map[string]int{"t0": 0, "t1": 1, "t4": 4, "t5": 5}
	var prs []MergedPR
	authored := map[string]struct{}{}
	for id := range turns {
		prs = append(prs, MergedPR{PRKey: id, AuthorIdentityID: sptr(id), WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE})
		authored[id] = struct{}{}
	}
	rd := aidlc.NewReadings()
	foldMergedChange(rd, prs, Attribution{AuthoredPRKeys: authored}, turns, nil, 1)
	low := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code)
	if v := valueOf(t, low); v != 2 || remainderOf(low) != 1 {
		t.Errorf("low-turn = %v rem %d, want 2 (t1 and t4) rem 1 (t0)", v, remainderOf(low))
	}
}

// TestFoldPresence_AmbientNeverCounts pins that installed configuration (a hook)
// serving a presence stage never counts toward it, however many people triggered it.
func TestFoldPresence_AmbientNeverCounts(t *testing.T) {
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
	var invs []HarnessInvocation
	for p := 0; p < 6; p++ {
		invs = append(invs, HarnessInvocation{PersonID: fmt.Sprintf("p%d", p), ArtifactID: "hook", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_HOOK})
	}
	rd := aidlc.NewReadings()
	foldPresence(rd, 10, invs, map[string]aidlcv1.AidlcStage{"hook": review})
	r := mustReading(t, rd, aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, review)
	if v := valueOf(t, r); v != 0 || remainderOf(r) != 0 {
		t.Errorf("review seat-holders = %v rem %d, want 0 rem 0: a hook is ambient", v, remainderOf(r))
	}
}

// TestIsObservabilityTool pins the operate L1 matcher: case-insensitive, on a whole
// token, and not on an unrelated tool or on a name that merely contains a listed one
// (temporal and contempo contain tempo).
func TestIsObservabilityTool(t *testing.T) {
	for name, want := range map[string]bool{
		"mcp__datadog__query": true,
		"Grafana":             true,
		"sentry_issue_lookup": true,
		"temporal_workflow":   false,
		"contempo":            false,
		"mcp__elasticsearch":  true,
		"tempo-query":         true,
		"Read":                false,
		"Bash":                false,
		"":                    false,
	} {
		if got := IsObservabilityTool(name); got != want {
			t.Errorf("IsObservabilityTool(%q) = %v, want %v", name, got, want)
		}
	}
}
