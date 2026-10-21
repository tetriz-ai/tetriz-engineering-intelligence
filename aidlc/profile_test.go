// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// TestCellDefinitions_CoverEveryRule pins that every cell the v1 ruleset scores has a
// definition to render: a rung with a verdict and no meaning is the gap this table closes.
func TestCellDefinitions_CoverEveryRule(t *testing.T) {
	rs := rules.ForVersion("v1")
	if rs == nil {
		t.Fatal("v1 ruleset not registered")
	}
	for key := range rs.Cells {
		if cellDefinition(key.Stage, key.Level) == "" {
			t.Errorf("%s/%s has no definition", stageKey(key.Stage), levelKey(key.Level))
		}
	}
	if got := len(rs.Cells); got != 45 {
		t.Fatalf("v1 scores %d cells, want 45", got)
	}
}

// TestSignalLabels_CoverEveryRuleSignal pins that every signal a v1 cell rule reads has a
// display label, so no term or evidence row falls back to a registry name.
func TestSignalLabels_CoverEveryRuleSignal(t *testing.T) {
	rs := rules.ForVersion("v1")
	for key, rule := range rs.Cells {
		for _, ck := range rule.Checks {
			for _, sig := range []aidlcv1.SignalKey{ck.Signal, ck.Denominator} {
				if sig == aidlcv1.SignalKey_SIGNAL_KEY_UNSPECIFIED {
					continue
				}
				if _, ok := signalLabels[sig]; !ok {
					t.Errorf("%s/%s reads %s, which has no display label", stageKey(key.Stage), levelKey(key.Level), sig)
				}
			}
		}
	}
	for _, l := range signalLabels {
		if strings.ContainsAny(l, "<>=—") {
			t.Errorf("label %q carries an operator or an em dash", l)
		}
	}
}

func codeRow(t *testing.T, rd *Readings) map[aidlcv1.AidlcLevel]*aidlcv1.MaturityCell {
	t.Helper()
	return stageCells(t, rd, aidlcv1.AidlcStage_AIDLC_STAGE_CODE)
}

func stageCells(t *testing.T, rd *Readings, stage aidlcv1.AidlcStage) map[aidlcv1.AidlcLevel]*aidlcv1.MaturityCell {
	t.Helper()
	res := Score(rules.ForVersion("v1"), rd, OrgRead{}, nil)
	cells := map[aidlcv1.AidlcLevel]*aidlcv1.MaturityCell{}
	for _, st := range res.Profile.GetStages() {
		if st.GetStage() != stage {
			continue
		}
		for _, c := range st.GetCells() {
			cells[c.GetLevel()] = c
		}
	}
	if len(cells) != 5 {
		t.Fatalf("%s row has %d cells, want 5", stageKey(stage), len(cells))
	}
	return cells
}

func wantTerms(t *testing.T, c *aidlcv1.MaturityCell, wants ...string) {
	t.Helper()
	terms := strings.Join(c.GetTerms(), "\n")
	for _, w := range wants {
		if !strings.Contains(terms, w) {
			t.Errorf("terms lack %q:\n%s", w, terms)
		}
	}
	if strings.Contains(terms, "—") {
		t.Errorf("terms carry an em dash, which reader copy never does:\n%s", terms)
	}
}

// TestProfileCells_CarryTermsAndEvidence scores a small readings fixture end to end and
// checks the wire cells explain themselves: a rule-decided cell names its share and the
// readings behind it, a control-gated cell names the control, an implied floor names the
// rung it was awarded under, and a cell with no reading keeps its reason with no terms invented.
func TestProfileCells_CarryTermsAndEvidence(t *testing.T) {
	rd := NewReadings()
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	// code L1: 40 AI-authored of 100 seat-linked delivery PRs by 6 people -> emerging, met by rule.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(40), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(100))
	// code L3: 30 human-reviewed of the 40 AI PRs, control rate 0.6 -> established, met by rule;
	// code L2 is then an implied floor under L3.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, withPeople(resolved(30), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(40))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0.6))
	// code L4: 5 low-turn of 40 -> not reached; complexity is context.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code, withPeople(resolved(5), 3))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_COMPLEXITY_SCORE, code, resolved(6.6))

	cells := codeRow(t, rd)
	for lv, c := range cells {
		if c.GetDefinition() == "" {
			t.Errorf("code/%s has no definition", levelKey(lv))
		}
	}

	l1 := cells[aidlcv1.AidlcLevel_AIDLC_LEVEL_L1]
	if l1.GetState() != aidlcv1.CellState_CELL_STATE_MET || l1.GetDecidedBy() != aidlcv1.DecidedBy_DECIDED_BY_RULE {
		t.Fatalf("code/L1 = %v/%v, want met by rule", l1.GetState(), l1.GetDecidedBy())
	}
	wantTerms(t, l1,
		"AI-authored merged feature and bugfix PRs: 40 of the 100 merged feature and bugfix PRs by people with a seat (40%).",
		"Emerging: more than a fifth of what was read, and no more than half.",
		"6 people did this, at or above the 2 this level asks for.")
	if got := len(l1.GetEvidence()); got != 3 {
		t.Errorf("code/L1 evidence has %d entries, want the numerator, the population and the people", got)
	}
	for _, e := range l1.GetEvidence() {
		if e.GetLabel() == "" || e.GetValue() == "" {
			t.Errorf("code/L1 evidence entry with an empty label or value: %v", e)
		}
	}

	l3 := cells[aidlcv1.AidlcLevel_AIDLC_LEVEL_L3]
	wantTerms(t, l3,
		"30 of the 40 AI-authored merged feature and bugfix PRs (75%).",
		"Established: more than half of what was read.",
		"Reviews are recorded on 60% of this stage's non-AI work, so a recorded review is a real signal here.")

	l4 := cells[aidlcv1.AidlcLevel_AIDLC_LEVEL_L4]
	if l4.GetState() != aidlcv1.CellState_CELL_STATE_NOT_REACHED {
		t.Fatalf("code/L4 = %v, want not reached", l4.GetState())
	}
	wantTerms(t, l4,
		"5 of the 40 AI-authored merged feature and bugfix PRs (13%).",
		"Not reached: even if everything that could not be read counted, this stays at or below a fifth.",
		"Change-complexity score of the low back-and-forth AI PRs is 6.6; the verdict does not use it.")

	l2 := cells[aidlcv1.AidlcLevel_AIDLC_LEVEL_L2]
	if l2.GetDecidedBy() != aidlcv1.DecidedBy_DECIDED_BY_IMPLIED_FLOOR {
		t.Fatalf("code/L2 decided by %v, want implied floor", l2.GetDecidedBy())
	}
	wantTerms(t, l2, "Counted because this stage reaches Supervised agentic, which includes this level.")
	if len(l2.GetTerms()) != 1 {
		t.Errorf("a floor with no share carries one sentence, got %v", l2.GetTerms())
	}

	l5 := cells[aidlcv1.AidlcLevel_AIDLC_LEVEL_L5]
	if l5.GetState() != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE || l5.GetReason() == "" {
		t.Fatalf("code/L5 = %v with reason %q, want an abstain with a reason", l5.GetState(), l5.GetReason())
	}
	if len(l5.GetTerms()) != 0 || len(l5.GetEvidence()) != 0 {
		t.Errorf("code/L5's enforced-practice signals have no reading, so nothing was read; terms=%v evidence=%v", l5.GetTerms(), l5.GetEvidence())
	}
}

// TestProfileCells_StraddleKeepsShareTerm pins that a coverage abstain whose interval
// straddles a cut still shows the measured interval and names the line it spans.
func TestProfileCells_StraddleKeepsShareTerm(t *testing.T) {
	rd := NewReadings()
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	// 15 hits of 100 with 20 unread -> [0.15, 0.35] straddles the 0.20 cut.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withNumRemainder(withPeople(resolved(15), 6), 20))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(100))
	l1 := codeRow(t, rd)[aidlcv1.AidlcLevel_AIDLC_LEVEL_L1]
	if l1.GetState() != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE || l1.GetReason() == "" {
		t.Fatalf("code/L1 = %v (%q), want a coverage abstain with a reason", l1.GetState(), l1.GetReason())
	}
	wantTerms(t, l1,
		"15 of the 100 merged feature and bugfix PRs by people with a seat (15% to 35%). For 20 of those, we could not tell.",
		"Too much could not be read: that range crosses a fifth, the line this level is decided on.")
	if n := strings.Count(strings.Join(l1.GetTerms(), "\n"), "35%"); n != 1 {
		t.Errorf("the interval prints once, got %d times", n)
	}
}

// TestProfileCells_ControlFailedSaysSo pins that a cell the control abstained does not
// also print the reassuring control sentence.
func TestProfileCells_ControlFailedSaysSo(t *testing.T) {
	rd := NewReadings()
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, withPeople(resolved(30), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(40))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0))
	l3 := codeRow(t, rd)[aidlcv1.AidlcLevel_AIDLC_LEVEL_L3]
	// A failed control travels on the wire as a coverage abstain; the reason names it.
	if l3.GetState() != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE || !strings.Contains(l3.GetReason(), "Review is rare on this stage") {
		t.Fatalf("code/L3 = %v (%q), want the control-failed abstain", l3.GetState(), l3.GetReason())
	}
	wantTerms(t, l3, "No reviews are recorded on this stage's non-AI work, so a review on an AI change proves nothing here.")
	if strings.Contains(strings.Join(l3.GetTerms(), "\n"), "real signal") {
		t.Error("a control-failed cell must not print the passing control sentence")
	}
}

// TestProfileCells_EarlyAbstainsKeepTheirReading pins that an abstain decided by a
// measurement still shows it: a population under its floor names the count, and an
// instrumented-only zero names the zero.
func TestProfileCells_EarlyAbstainsKeepTheirReading(t *testing.T) {
	rd := NewReadings()
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	// 3 of 8 seat-linked delivery PRs: the artifact floor is 20, so the share cannot be read.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(3), 2))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(8))
	l1 := codeRow(t, rd)[aidlcv1.AidlcLevel_AIDLC_LEVEL_L1]
	if l1.GetAbstainKind() != aidlcv1.AbstainKind_ABSTAIN_KIND_POPULATION_FLOOR {
		t.Fatalf("code/L1 abstain = %v, want population floor", l1.GetAbstainKind())
	}
	wantTerms(t, l1, "Merged feature and bugfix PRs by people with a seat: 8 in the window. Needs at least 20 before a share can be read.")
	if len(l1.GetEvidence()) == 0 || l1.GetEvidence()[0].GetValue() != "8" {
		t.Errorf("population-floor evidence = %v, want the population count", l1.GetEvidence())
	}

	// product_spec L1 reads an instrumented-only presence signal; zero abstains, and says so.
	rd2 := NewReadings()
	ps := aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC
	rd2.Set(aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_HARNESS_SEAT_HOLDERS, ps, withPeople(resolved(0), 0))
	rd2.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, resolved(12))
	p1 := stageCells(t, rd2, ps)[aidlcv1.AidlcLevel_AIDLC_LEVEL_L1]
	if p1.GetAbstainKind() != aidlcv1.AbstainKind_ABSTAIN_KIND_INSTRUMENTED_ONLY {
		t.Fatalf("product_spec/L1 abstain = %v, want instrumented only", p1.GetAbstainKind())
	}
	wantTerms(t, p1, "People with a seat who used a product-spec artifact: none recorded in the window.")
}

// TestPctText pins the rounding rule: a rate that rounds onto a cut, or onto zero, while
// not being that number keeps the decimals that separate them, so a term never
// contradicts its verdict.
func TestPctText(t *testing.T) {
	rs := rules.ForVersion("v1")
	for _, tt := range []struct {
		in   float64
		want string
	}{
		{0.40, "40%"}, {0.204, "20.4%"}, {0.20, "20%"}, {0.499, "49.9%"}, {0.004, "0.4%"}, {0, "0%"}, {1, "100%"}, {0.996, "99.6%"}, {0.1788, "18%"},
	} {
		if got := pctText(rs, tt.in); got != tt.want {
			t.Errorf("pctText(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
	if got := intervalText(rs, 0.25, 0.252); got != "25%" {
		t.Errorf("intervalText collapses equal prints; got %q", got)
	}
	if got := pctText(rs, 0.20004); got != "20.004%" {
		t.Errorf("pctText keeps the decimals that separate a rate from a cut; got %q", got)
	}
	// The sensitive values follow the ruleset's cuts, not literals, and precision grows
	// until the rate is apart from the cut, whatever the cut is.
	quarter := &rules.RuleSet{BandCuts: rules.BandCuts{Emerging: 0.25, Established: 0.5}}
	if got := pctText(quarter, 0.2504); got != "25.04%" {
		t.Errorf("pctText under a 25%% cut = %q, want the decimals kept", got)
	}
	if got := pctText(rs, 50001.0/250000.0); got != "20.0004%" {
		t.Errorf("pctText(50001/250000) = %q, want 20.0004%%", got)
	}
	third := &rules.RuleSet{BandCuts: rules.BandCuts{Emerging: 1.0 / 3.0, Established: 0.5}}
	if got := pctText(third, 0.3334); got != "33.34%" {
		t.Errorf("pctText under a one-third cut = %q, want 33.34%%", got)
	}
	if got := pctText(third, 1.0/3.0); got != "33%" {
		t.Errorf("pctText at exactly the cut = %q, want 33%%", got)
	}
	// The threshold a verdict states is the scorer's cut, not a rounding of it.
	if got := fractionWord(third, 1.0/3.0); got != "33.3333%" {
		t.Errorf("fractionWord(1/3) = %q, want 33.3333%%", got)
	}
	halfPct := &rules.RuleSet{BandCuts: rules.BandCuts{Emerging: 0.205, Established: 0.5}}
	if got := fractionWord(halfPct, 0.205); got != "20.5%" {
		t.Errorf("fractionWord(0.205) = %q, want 20.5%%", got)
	}
	if got := pctText(halfPct, 0.207); got != "20.7%" {
		t.Errorf("pctText(0.207) under a 20.5%% cut = %q, want 20.7%% (a rate past the cut never prints as the cut's rounding)", got)
	}
}

// TestProfileCopy_NoOperatorsOrDashes sweeps every served string on a full grid: no
// term, definition or label carries a comparison operator or an em dash.
func TestProfileCopy_NoOperatorsOrDashes(t *testing.T) {
	rd := NewReadings()
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withNumRemainder(withPeople(resolved(40), 6), 30))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(100))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, withPeople(resolved(30), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(40))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0.6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code, withPeople(resolved(5), 3))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_COMPLEXITY_SCORE, code, resolved(6.6))
	res := Score(rules.ForVersion("v1"), rd, OrgRead{}, nil)
	for _, st := range res.Profile.GetStages() {
		for _, c := range st.GetCells() {
			for _, txt := range append([]string{c.GetDefinition(), c.GetReason()}, c.GetTerms()...) {
				if strings.ContainsAny(txt, "<>=—") {
					t.Errorf("%s/%s serves %q", stageKey(st.GetStage()), levelKey(c.GetLevel()), txt)
				}
			}
			for _, e := range c.GetEvidence() {
				if strings.ContainsAny(e.GetLabel(), "<>=—") {
					t.Errorf("%s/%s evidence label %q", stageKey(st.GetStage()), levelKey(c.GetLevel()), e.GetLabel())
				}
			}
		}
	}
}

// TestProfileCells_CutBoundaryPrintsTheDecidingFigure pins that the share term is derived
// from the stored counts, not the four-decimal bounds, so a verdict decided a hair past a
// cut prints a figure past the cut.
func TestProfileCells_CutBoundaryPrintsTheDecidingFigure(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	// 20,004 of 100,000 -> lo 0.20004: emerging by rule, and the term must not say 20%.
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(20004), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(100000))
	l1 := codeRow(t, rd)[aidlcv1.AidlcLevel_AIDLC_LEVEL_L1]
	if l1.GetState() != aidlcv1.CellState_CELL_STATE_MET {
		t.Fatalf("code/L1 = %v, want met (emerging)", l1.GetState())
	}
	wantTerms(t, l1, "(20.004%). Emerging:")

	// 20,000 hits and one unread of 100,000 -> [0.20000, 0.20001] straddles the cut; the term
	// must show two distinct ends, not "20%" beside "spans the 20% line".
	rd2 := NewReadings()
	rd2.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withNumRemainder(withPeople(resolved(20000), 6), 1))
	rd2.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(100000))
	s1 := codeRow(t, rd2)[aidlcv1.AidlcLevel_AIDLC_LEVEL_L1]
	if s1.GetState() != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE {
		t.Fatalf("code/L1 = %v, want a straddle abstain", s1.GetState())
	}
	wantTerms(t, s1, "(20% to 20.001%)", "crosses a fifth")
}

// TestBandCuts_RenderExactly pins the precision contract fractionWord relies on: every
// registered ruleset declares its band cuts to at most four decimals of a percent, so
// the threshold a verdict sentence states is the scorer's boundary and not a rounding
// of it. A new version with a finer cut fails here and must widen the renderer first.
func TestBandCuts_RenderExactly(t *testing.T) {
	for _, v := range rules.Versions() {
		rs := rules.ForVersion(v)
		for name, cut := range map[string]float64{"emerging": rs.BandCuts.Emerging, "established": rs.BandCuts.Established} {
			scaled := cut * 1_000_000 // four decimals of a percent
			if math.Abs(scaled-math.Round(scaled)) > 1e-6 {
				t.Errorf("%s: the %s cut %v is finer than four decimals of a percent; fractionWord would round it", v, name, cut)
			}
			printed := fractionWord(rs, cut)
			if printed == "" || strings.ContainsAny(printed, "<>=—") {
				t.Errorf("%s: the %s cut renders as %q", v, name, printed)
			}
		}
	}
}

// TestSummarise_ThreeShapes pins the summary composer's three shapes against
// hand-built stage verdicts, so each shape's exact wording is pinned independently of
// any one ruleset's readings.
func TestSummarise_ThreeShapes(t *testing.T) {
	const (
		unspecified = aidlcv1.AidlcLevel_AIDLC_LEVEL_UNSPECIFIED
		l1          = aidlcv1.AidlcLevel_AIDLC_LEVEL_L1
		l2          = aidlcv1.AidlcLevel_AIDLC_LEVEL_L2
		l3          = aidlcv1.AidlcLevel_AIDLC_LEVEL_L3
	)
	var (
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
	tests := []struct {
		name   string
		stages []StageVerdict
		want   string
	}{
		{
			// Shape 1: a full grid, grouped by frontier (high to low), with one
			// stage established-and-emerging and one emerging-only stage that has
			// nothing established.
			name: "some stages established",
			stages: []StageVerdict{
				{Stage: productSpec, Frontier: unspecified, EmergingEdge: l3},
				{Stage: techSpec, Frontier: l3, EmergingEdge: unspecified},
				{Stage: plan, Frontier: unspecified, EmergingEdge: unspecified},
				{Stage: code, Frontier: l3, EmergingEdge: unspecified},
				{Stage: review, Frontier: l1, EmergingEdge: unspecified},
				{Stage: testQA, Frontier: l3, EmergingEdge: unspecified},
				{Stage: ciCD, Frontier: l2, EmergingEdge: l3},
				{Stage: deploy, Frontier: l1, EmergingEdge: unspecified},
				{Stage: operate, Frontier: l3, EmergingEdge: unspecified},
			},
			want: "In Tech spec, Code, Test & QA and Operate, an agent produces a complete piece of work and a person checks and approves it. " +
				"In CI/CD, a person and an assistant work the task out together in conversation, and the work is moving toward a way of working where an agent produces a complete piece of work and a person checks and approves it. " +
				"In Review and Deploy, a person does the work and an assistant helps on request. " +
				"Product spec has nothing established yet and is moving toward a way of working where an agent produces a complete piece of work and a person checks and approves it.",
		},
		{
			// Shape 2: a profile with an emerging edge but no frontier anywhere.
			name: "nothing established, something emerging",
			stages: []StageVerdict{
				{Stage: code, Frontier: unspecified, EmergingEdge: l3},
			},
			want: "No stage is established yet. Code is moving toward a way of working where an agent produces a complete piece of work and a person checks and approves it.",
		},
		{
			// Shape 3: a profile with no frontiers anywhere and no emerging edge.
			name:   "nothing established or emerging",
			stages: []StageVerdict{{Stage: code, Frontier: unspecified, EmergingEdge: unspecified}},
			want:   "None of your stages shows AI in a real share of its work over this window yet.",
		},
		{
			// Every stage abstained on every rung: nothing was measured, which is not a
			// finding about the organisation and must not read as "AI is barely used".
			name: "nothing measured anywhere",
			stages: []StageVerdict{
				{Stage: code, Frontier: unspecified, EmergingEdge: unspecified, AllAbstained: true},
				{Stage: review, Frontier: unspecified, EmergingEdge: unspecified, AllAbstained: true},
			},
			want: "We could not measure any stage yet, so this map has no reading for your organisation.",
		},
		{
			name: "two emerging-only stages at different levels read as one sentence each",
			stages: []StageVerdict{
				{Stage: code, Frontier: unspecified, EmergingEdge: l2},
				{Stage: review, Frontier: unspecified, EmergingEdge: l1},
			},
			want: "No stage is established yet. Code is moving toward a way of working where a person and an assistant work the task out together in conversation. Review is moving toward a way of working where a person does the work and an assistant helps on request.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarise(tt.stages)
			if got != tt.want {
				t.Errorf("summarise() =\n  %q\nwant\n  %q", got, tt.want)
			}
			if strings.ContainsAny(got, "0123456789") {
				t.Errorf("summarise() carries a digit: %q", got)
			}
		})
	}
}

// actorNounRe finds the "who" a practice sentence names — person, assistant, agent
// (singular or plural), reviewer or the organisation — as a regression guard between
// summaryLevelPractice and practicePhrase (below); it deliberately does not match
// stage-specific nouns (spec, plan, pipeline, release, …), only the actors.
var actorNounRe = regexp.MustCompile(`(?i)\b(persons?|people|assistants?|agents?|reviewers?|organisation'?s?)\b`)

// TestSummaryLevelPractice_ActorsGroundedInPracticePhrase pins a deliberately
// narrower parity guard: summaryLevelPractice (profile.go) is a level-generic table,
// not a literal reuse of practicePhrase (copy.go) — one summary clause names several
// stages at once, so it cannot repeat any one stage's own specific verb the way a
// per-cell phrase does, and full lexical identity between the two tables is not
// achievable.
// What must hold, so the two cannot silently drift onto different actors for the same
// rung, is narrower: every actor noun (person/assistant/agent/agents/organisation/
// reviewer) summaryLevelPractice names at a level is one practicePhrase itself uses
// somewhere at that same level, the level names at least one actor at all, and the
// five entries are pairwise distinct.
func TestSummaryLevelPractice_ActorsGroundedInPracticePhrase(t *testing.T) {
	// A copy-pasted entry (L1's text on L2, L3's on L4) would pass the actor check
	// below, since adjacent levels share actors.
	if len(summaryLevelPractice) != 5 {
		t.Fatalf("summaryLevelPractice has %d entries, want one per level (5)", len(summaryLevelPractice))
	}
	byText := map[string]aidlcv1.AidlcLevel{}
	for level, summary := range summaryLevelPractice {
		if other, dup := byText[summary]; dup {
			t.Errorf("levels %s and %s share one summary practice: %q", levelKey(other), levelKey(level), summary)
		}
		byText[summary] = level
	}
	for level, summary := range summaryLevelPractice {
		vocab := map[string]bool{}
		for _, byLevel := range practicePhrase {
			if p, ok := byLevel[level]; ok {
				for _, m := range actorNounRe.FindAllString(p, -1) {
					vocab[strings.ToLower(m)] = true
				}
			}
		}
		actors := actorNounRe.FindAllString(summary, -1)
		if len(actors) == 0 {
			t.Errorf("level %s: summaryLevelPractice names no actor at all: %q", levelKey(level), summary)
		}
		for _, a := range actors {
			if !vocab[strings.ToLower(a)] {
				t.Errorf("level %s: summaryLevelPractice names %q, which no stage's practicePhrase entry at that level uses — the two tables have drifted: %q",
					levelKey(level), a, summary)
			}
		}
	}
}

// TestBottleneckCopy_NamesThePracticeNotTheVerdict holds every bottleneck kind
// (review reach, adoption reach, rung missed, ladder stalled) to a title that names a
// practice rather than restating the cell's verdict, and no constraint or move
// carrying the rule's own vocabulary ("read", "missed", "cut") or a bare level key.
// checkConstraintCopy (copy_test.go) is the same sweep
// TestTokenTest_ProfileServesNoInternalIdentifiers runs over every served bottleneck
// and move; this test additionally exercises every kind directly, including the
// no-Terms fixed sentences, which the token test's fixtures do not reach.
func TestBottleneckCopy_NamesThePracticeNotTheVerdict(t *testing.T) {
	mc := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	pp := aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	testQA := aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA
	l1, l4 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L1, aidlcv1.AidlcLevel_AIDLC_LEVEL_L4

	bottlenecks := []struct {
		name string
		b    Bottleneck
	}{
		{"review reach, with numbers", Bottleneck{ID: bottleneckIDReviewReach, HasMove: true, Terms: &MoveTerms{Hits: 140, Population: 320, Grain: mc}}},
		{"review reach, no numbers", Bottleneck{ID: bottleneckIDReviewReach, HasMove: true}},
		{"adoption reach, with numbers", Bottleneck{ID: bottleneckIDAdoptionReach, HasMove: true, Terms: &MoveTerms{Hits: 11, Population: 18, Grain: pp}}},
		{"adoption reach, no numbers", Bottleneck{ID: bottleneckIDAdoptionReach, HasMove: true}},
		{"rung missed (code/autonomous), with numbers", Bottleneck{ID: "rung_code_L4", Stage: code, Level: l4, HasMove: true, Terms: &MoveTerms{Hits: 53, Population: 304, Grain: mc}}},
		{"rung missed (code/autonomous), no numbers", Bottleneck{ID: "rung_code_L4", Stage: code, Level: l4, HasMove: true}},
		{"rung missed (review/autonomous worked example)", Bottleneck{ID: "rung_review_L4", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, Level: l4, HasMove: true, Terms: &MoveTerms{Hits: 210, Population: 1507, Grain: mc}}},
		{"ladder stalled, with numbers", Bottleneck{ID: "ladder_test_qa", Stage: testQA, Level: l1, HasMove: true, Terms: &MoveTerms{Hits: 3, Population: 20, Grain: mc}}},
		{"ladder stalled, no numbers", Bottleneck{ID: "ladder_test_qa", Stage: testQA, Level: l1, HasMove: true}},
	}

	for _, version := range rules.Versions() {
		rs := rules.ForVersion(version)
		for _, tc := range bottlenecks {
			t.Run(version+"/"+tc.name, func(t *testing.T) {
				title := bottleneckTitle(rs, tc.b)
				body := bottleneckBody(rs, tc.b)
				checkConstraintCopy(t, "title", title)
				checkConstraintCopy(t, "body", body)
				if title == "" {
					t.Fatal("title is empty")
				}
				// The old template composed the title as "<Stage> <levelKey> was read
				// and missed: ..." — the cell's own verdict sentence. checkConstraintCopy
				// already forbids "read"/"missed" and a bare level key, which is what
				// that template was built from, so a title that still passes has left
				// the verdict-sentence shape behind.
				if strings.Contains(title, " was ") && strings.Contains(title, ":") {
					t.Errorf("title %q still reads as a verdict sentence", title)
				}
				// A derived rung_ or ladder_ finding always names a real cell, so it never
				// falls back to the generic shape reserved for a cell no table carries.
				if strings.HasPrefix(tc.b.ID, bottleneckIDPrefixRung) || strings.HasPrefix(tc.b.ID, bottleneckIDPrefixLadder) {
					if _, ok := cellCopyFor(tc.b.Stage, tc.b.Level, tc.b.SecondaryMiss); !ok {
						t.Errorf("%s/%s has no cell copy", stageKey(tc.b.Stage), levelKey(tc.b.Level))
					}
					if fallback := fmt.Sprintf("%s's %s practice", stageTitle(tc.b.Stage), levelTitle(tc.b.Level)); practiceText(tc.b.Stage, tc.b.Level, tc.b.SecondaryMiss) == fallback {
						t.Errorf("practiceText fell back to %q", fallback)
					}
					if strings.HasPrefix(title, "Little of your ") || strings.Contains(moveTitle(rs, tc.b), "described below") {
						t.Errorf("title %q or move title %q has the generic fallback shape", title, moveTitle(rs, tc.b))
					}
				}
				if tc.b.HasMove {
					mTitle, mBody := moveTitle(rs, tc.b), moveBody(rs, tc.b)
					checkConstraintCopy(t, "move title", mTitle)
					checkConstraintCopy(t, "move body", mBody)
					if mTitle == "" || mBody == "" {
						t.Errorf("move has empty copy: title=%q body=%q", mTitle, mBody)
					}
				}
			})
		}
	}
}

// TestRungMissedCopy_WorkedExamples pins three worked examples verbatim: each
// constraint title and body and each move title and body, with the distances 9, 60
// and 115 from the growing-total rule (the smallest n with (hits+n)/(total+n)
// strictly above one in five). 28 of 172 sits exactly on a multiple of four short of
// the line, so n = 8 lands on one in five and only 9 clears it; 40 of 438 and 210 of
// 1,507 sit between multiples, and the last also pins a thousands separator.
func TestRungMissedCopy_WorkedExamples(t *testing.T) {
	rs := rules.ForVersion("v1")
	mc := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	l4 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L4
	tests := []struct {
		name                             string
		b                                Bottleneck
		title, body, moveTitle, moveBody string
		distance                         int64
	}{
		{
			name:      "Code at Autonomous, 28 of 172",
			b:         Bottleneck{ID: "rung_code_L4", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_CODE, Level: l4, HasMove: true, Terms: &MoveTerms{Hits: 28, Population: 172, Grain: mc}},
			title:     "Few AI-written pull requests merge with little back-and-forth.",
			body:      "Of the 172 AI-written pull requests merged in the last 90 days, 28 needed little back-and-forth before merging.",
			moveTitle: "Let agents take more pull requests to merge with little back-and-forth.",
			moveBody:  "Once more than one in five of these pull requests work this way, your map counts it as a practice your team is adopting. That is about 9 more such pull requests at today's volume.",
			distance:  9,
		},
		{
			name:      "Review at Autonomous, 40 of 438",
			b:         Bottleneck{ID: "rung_review_L4", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, Level: l4, HasMove: true, Terms: &MoveTerms{Hits: 40, Population: 438, Grain: mc}},
			title:     "A review bot clears few changes without a person re-reading them.",
			body:      "Your review bot looked at 438 merged pull requests. 40 of them merged on the bot's review alone.",
			moveTitle: "Let your review bot approve low-risk pull requests on its own.",
			moveBody:  "Once more than one in five of these pull requests work this way, your map counts it as a practice your team is adopting. That is about 60 more bot-cleared pull requests at today's volume.",
			distance:  60,
		},
		{
			name:      "Review at Autonomous, 210 of 1,507",
			b:         Bottleneck{ID: "rung_review_L4", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, Level: l4, HasMove: true, Terms: &MoveTerms{Hits: 210, Population: 1507, Grain: mc}},
			title:     "A review bot clears few changes without a person re-reading them.",
			body:      "Your review bot looked at 1,507 merged pull requests. 210 of them merged on the bot's review alone.",
			moveTitle: "Let your review bot approve low-risk pull requests on its own.",
			moveBody:  "Once more than one in five of these pull requests work this way, your map counts it as a practice your team is adopting. That is about 115 more bot-cleared pull requests at today's volume.",
			distance:  115,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bottleneckTitle(rs, tt.b); got != tt.title {
				t.Errorf("title =\n  %q\nwant\n  %q", got, tt.title)
			}
			if got := bottleneckBody(rs, tt.b); got != tt.body {
				t.Errorf("body =\n  %q\nwant\n  %q", got, tt.body)
			}
			if got := moveTitle(rs, tt.b); got != tt.moveTitle {
				t.Errorf("move title =\n  %q\nwant\n  %q", got, tt.moveTitle)
			}
			if got := moveBody(rs, tt.b); got != tt.moveBody {
				t.Errorf("move body =\n  %q\nwant\n  %q", got, tt.moveBody)
			}
			if got := itemsToClear(*tt.b.Terms, rs.BandCuts.Emerging); got != tt.distance {
				t.Errorf("distance = %d, want %d", got, tt.distance)
			}
		})
	}
}

// TestLadderCopy_NamesOnlyWhatTheCellMeasured pins the stalled-stage copy's contract: it
// never asserts a whole-stage cause the cell rule never established (a stage can also
// carry a same-row instrumentation gap, e.g. "deploy and rollback events are not
// read," and a causal claim would tell the reader two incompatible things
// about one stage), and it names only the one thing this cell decided: the first step of
// the stage is not yet established.
func TestLadderCopy_NamesOnlyWhatTheCellMeasured(t *testing.T) {
	rs := rules.ForVersion("v1")
	mc := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	tests := []struct {
		name string
		b    Bottleneck
		want string
	}{
		{
			name: "without counts",
			b:    Bottleneck{ID: "ladder_deploy", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L1},
			want: "Across your deployment work, this practice is still rare: a person runs the release and asks the assistant about it.",
		},
		{
			name: "with counts",
			b:    Bottleneck{ID: "ladder_deploy", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L1, Terms: &MoveTerms{Hits: 1, Population: 30, Grain: mc}},
			want: "Across your deployment work, this practice is still rare: a person runs the release and asks the assistant about it. In the last 90 days, 1 of 30 pull requests followed it.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bottleneckBody(rs, tt.b)
			if got != tt.want {
				t.Errorf("ladder body =\n  %q\nwant\n  %q", got, tt.want)
			}
			if strings.Contains(got, "held back by how the work is done") {
				t.Errorf("ladder body asserts a whole-stage cause the cell never measured: %q", got)
			}
		})
	}
}

// TestCellCopies_CoverEveryLevelAStageCanFallShortOn pins that every (stage, level 1 to
// 5) cell, and both shares of the two two-share L5 cells, carries its own action and
// finding, so the generic fallback action never serves a reachable cell. Each action is
// an imperative (a known verb opens it) and each title stays within the copy caps
// unpunctuated, because the composers add the full stop.
func TestCellCopies_CoverEveryLevelAStageCanFallShortOn(t *testing.T) {
	imperatives := map[string]bool{}
	for _, v := range strings.Fields("Have Work Let Draft Ask Plan Get Run Walk Edit Write Debug Close") {
		imperatives[v] = true
	}
	check := func(where string, cc cellCopy) {
		t.Helper()
		first := strings.Fields(cc.action + " ")[0]
		if !imperatives[first] {
			t.Errorf("%s: action %q does not open with an imperative verb", where, cc.action)
		}
		for name, txt := range map[string]string{"action": cc.action, "finding": cc.finding} {
			if strings.HasSuffix(txt, ".") {
				t.Errorf("%s: %s %q carries its own full stop; the composer adds it", where, name, txt)
			}
			if n := len(strings.Fields(txt + ".")); n > titleMaxWords {
				t.Errorf("%s: %s %q has %d words, want at most %d", where, name, txt, n, titleMaxWords)
			}
			if m := rubricWords.FindString(txt); m != "" {
				t.Errorf("%s: %s %q carries the rubric word %q", where, name, txt, m)
			}
			if m := moveTitleBannedWords.FindString(txt); m != "" {
				t.Errorf("%s: %s %q leans on %q", where, name, txt, m)
			}
		}
	}
	for _, stage := range stageOrder {
		for _, level := range ladderOrder {
			if level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L5 {
				if ts, ok := twoShareCellCopies[stage]; ok {
					check(stageKey(stage)+"/L5 primary", ts.primary)
					check(stageKey(stage)+"/L5 secondary", ts.secondary)
					continue
				}
			}
			cc, ok := cellCopies[stage][level]
			if !ok {
				t.Errorf("%s/%s: no cellCopies entry", stageKey(stage), levelKey(level))
				continue
			}
			check(stageKey(stage)+"/"+levelKey(level), cc)
		}
	}
}

// TestMoveTitle_FallsBackToTheGenericAction pins the fallback for a cell no table
// carries: a short imperative that points at the practice the constraint body quotes.
func TestMoveTitle_FallsBackToTheGenericAction(t *testing.T) {
	rs := rules.ForVersion("v1")
	b := Bottleneck{ID: "rung_code_L2", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_CODE, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L2, HasMove: true}
	saved := cellCopies[b.Stage][b.Level]
	delete(cellCopies[b.Stage], b.Level)
	t.Cleanup(func() { cellCopies[b.Stage][b.Level] = saved })
	want := "Get more of your coding work done the way described below."
	got := moveTitle(rs, b)
	if got != want {
		t.Errorf("fallback move title =\n  %q\nwant\n  %q", got, want)
	}
	if n := len(strings.Fields(got)); n > 12 {
		t.Errorf("fallback move title has %d words, want at most 12", n)
	}
	if got, want := bottleneckTitle(rs, b), "Little of your coding work follows the practice below."; got != want {
		t.Errorf("fallback title =\n  %q\nwant\n  %q", got, want)
	}
}

// TestBottleneckCopy_ExhaustiveStageLevelSweep holds every (stage, level) pair that is a
// prevalence rule (the only cells a rung_ or ladder_ finding can carry real Terms for,
// since only a prevalence rule's scoreShare sets Cell.Share) to the copy
// contract, rendered as both a rung_ and a ladder_
// finding: titles within 12 words, bodies within 60, and no rule vocabulary or bare
// level key (checkConstraintCopy). A practice phrase that is fine in one stage can leak
// vocabulary in another, so the contract is checked per cell rather than on samples.
func TestBottleneckCopy_ExhaustiveStageLevelSweep(t *testing.T) {
	type cell struct {
		version       string
		stage         aidlcv1.AidlcStage
		level         aidlcv1.AidlcLevel
		secondaryMiss bool
	}
	var cells []cell
	for _, version := range rules.Versions() {
		rs := rules.ForVersion(version)
		for key, rule := range rs.Cells {
			if rule.Kind != rules.RuleKindPrevalence {
				continue
			}
			cells = append(cells, cell{version: version, stage: key.Stage, level: key.Level})
			// A two-share cell is swept for both which-share-missed variants, since
			// practiceText's phrasing depends on it (Bottleneck.SecondaryMiss).
			for _, ck := range rule.Checks {
				if ck.Kind == rules.CheckKindSecondaryShare {
					cells = append(cells, cell{version: version, stage: key.Stage, level: key.Level, secondaryMiss: true})
				}
			}
		}
	}
	if len(cells) != 38 {
		t.Fatalf("swept %d cells, want the 36 prevalence rules and the 2 secondary shares", len(cells))
	}
	sort.Slice(cells, func(i, j int) bool {
		a, b := cells[i], cells[j]
		if a.stage != b.stage {
			return a.stage < b.stage
		}
		if a.level != b.level {
			return a.level < b.level
		}
		return !a.secondaryMiss && b.secondaryMiss
	})

	mc := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	for _, c := range cells {
		name := fmt.Sprintf("%s/%s/%s", c.version, stageKey(c.stage), levelKey(c.level))
		if c.secondaryMiss {
			name += "/secondary"
		}
		t.Run(name, func(t *testing.T) {
			rs := rules.ForVersion(c.version)
			// 10%, safely below every registered emerging cut; and a single hit, so the
			// "N of M ... work(s) this way" verb agreement is exercised for one and many.
			for _, terms := range []*MoveTerms{{Hits: 5, Population: 50, Grain: mc}, {Hits: 1, Population: 50, Grain: mc}} {
				for _, idPrefix := range []string{"rung_", "ladder_"} {
					b := Bottleneck{
						ID: idPrefix + stageKey(c.stage) + "_" + levelKey(c.level), Stage: c.stage, Level: c.level,
						HasMove: true, Terms: terms, SecondaryMiss: c.secondaryMiss,
					}
					title, body := bottleneckTitle(rs, b), bottleneckBody(rs, b)
					checkConstraintCopy(t, idPrefix+"title", title)
					checkConstraintCopy(t, idPrefix+"body", body)
					if title == "" || body == "" {
						t.Errorf("%s: empty title=%q body=%q", idPrefix, title, body)
					}
					mTitle, mBody := moveTitle(rs, b), moveBody(rs, b)
					checkConstraintCopy(t, idPrefix+"move title", mTitle)
					checkConstraintCopy(t, idPrefix+"move body", mBody)
					if mTitle == "" || mBody == "" {
						t.Errorf("%s: empty move title=%q body=%q", idPrefix, mTitle, mBody)
					}
					// Every served title and move title must be a complete sentence: never a
					// noun phrase or a template fragment.
					if !isCompleteSentence(title) {
						t.Errorf("%s: title is not a complete sentence: %q", idPrefix+"title", title)
					}
					if !isCompleteSentence(mTitle) {
						t.Errorf("%s: move title is not a complete sentence: %q", idPrefix+"move title", mTitle)
					}
					checkMoveTitleNamesThePractice(t, idPrefix+"move title", c.stage, mTitle)
					for _, s := range []string{title, body, mTitle, mBody} {
						checkSentenceMechanics(t, idPrefix, s)
					}
					checkCopyShape(t, idPrefix, title, body, mTitle, mBody)
					if _, ok := cellCopyFor(c.stage, c.level, c.secondaryMiss); !ok {
						t.Errorf("%s: no cellCopies entry, so the move title fell back to the generic action: %q", idPrefix, mTitle)
					}
				}
			}
		})
	}

	// The two cohort constraints carry no (stage, level), so they are swept once per
	// version, with and without numbers, for the same shorthand and mechanics.
	pp := aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE
	for _, version := range rules.Versions() {
		rs := rules.ForVersion(version)
		for _, b := range []Bottleneck{
			{ID: bottleneckIDReviewReach, HasMove: true, Terms: &MoveTerms{Hits: 40, Population: 180, Grain: mc}},
			{ID: bottleneckIDReviewReach, HasMove: true},
			{ID: bottleneckIDAdoptionReach, HasMove: true, Terms: &MoveTerms{Hits: 11, Population: 18, Grain: pp}},
			{ID: bottleneckIDAdoptionReach, HasMove: true},
		} {
			mTitle := moveTitle(rs, b)
			if !isCompleteSentence(mTitle) {
				t.Errorf("%s/%s: move title is not a complete sentence: %q", version, b.ID, mTitle)
			}
			checkMoveTitleNamesThePractice(t, version+"/"+b.ID+" move title", aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, mTitle)
			for _, s := range []string{bottleneckTitle(rs, b), bottleneckBody(rs, b), mTitle, moveBody(rs, b)} {
				checkConstraintCopy(t, version+"/"+b.ID, s)
				checkSentenceMechanics(t, version+"/"+b.ID, s)
			}
			checkCopyShape(t, version+"/"+b.ID, bottleneckTitle(rs, b), bottleneckBody(rs, b), mTitle, moveBody(rs, b))
		}
	}
}

// titleMaxWords and bodyMaxWords are the copy standard's caps: a card is scanned, not
// studied.
const (
	titleMaxWords = 12
	bodyMaxWords  = 60
)

// rubricWords are the rubric's own vocabulary, banned from all four texts of a
// constraint: the rule's verbs, "rung", and a level named by number ("L4", "level 4").
// checkConstraintCopy's narrower sweep already covers most of this; the shape check
// states the whole ban in one place.
var rubricWords = regexp.MustCompile(`(?i)\b(read|missed|miss|cut|cuts|rung|rungs|level \d)\b|\bL[1-5]\b`)

// checkCopyShape holds the four texts of one constraint to the copy standard: titles of
// at most titleMaxWords words ending in a full stop, bodies of at most bodyMaxWords, no
// rubric vocabulary, and a move that never repeats its finding's title or body.
func checkCopyShape(t *testing.T, where, title, body, mTitle, mBody string) {
	t.Helper()
	for name, s := range map[string]string{"title": title, "move title": mTitle} {
		if n := len(strings.Fields(s)); n > titleMaxWords {
			t.Errorf("%s %s has %d words, want at most %d: %q", where, name, n, titleMaxWords, s)
		}
		if !strings.HasSuffix(s, ".") {
			t.Errorf("%s %s does not end in a full stop: %q", where, name, s)
		}
	}
	for name, s := range map[string]string{"body": body, "move body": mBody} {
		if n := len(strings.Fields(s)); n > bodyMaxWords {
			t.Errorf("%s %s has %d words, want at most %d: %q", where, name, n, bodyMaxWords, s)
		}
	}
	for name, s := range map[string]string{"title": title, "body": body, "move title": mTitle, "move body": mBody} {
		if m := rubricWords.FindString(s); m != "" {
			t.Errorf("%s %s carries the rubric word %q: %q", where, name, m, s)
		}
	}
	for name, s := range map[string]string{"move title": mTitle, "move body": mBody} {
		if strings.Contains(s, title) || (body != "" && strings.Contains(s, body)) {
			t.Errorf("%s %s restates its finding: %q", where, name, s)
		}
	}
}

// moveTitleBannedWords are words a move title may not lean on: "real minority" reads as
// a reduction rather than "above one in five", "passive" is a label the page never
// defines, and a level or rung word names the rubric's position instead of the work.
var moveTitleBannedWords = regexp.MustCompile(`(?i)\b(real minority|passive|levels?|rungs?|assisted|conversational|supervised agentic|autonomous|orchestrated)\b`)

// checkMoveTitleNamesThePractice holds a move title to naming the work: never the
// stage label standing in for the practice ("Bring Code's practice to ..."), and none
// of moveTitleBannedWords.
func checkMoveTitleNamesThePractice(t *testing.T, where string, stage aidlcv1.AidlcStage, mTitle string) {
	t.Helper()
	if m := moveTitleBannedWords.FindString(mTitle); m != "" {
		t.Errorf("%s leans on %q: %q", where, m, mTitle)
	}
	if stage != aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED && strings.Contains(mTitle, stageTitle(stage)+"'s practice") {
		t.Errorf("%s names the stage label, not the practice: %q", where, mTitle)
	}
}

var (
	// countVerb finds "<hits> of <population> <items> [(<pct>)] [already] <verb>".
	countVerb = regexp.MustCompile(`(\d[\d,]*) of [\d,]+ \S+ (?:\([^)]*\) )?(?:already )?(work|works|reach|reaches)\b`)
	// afterColon finds the word opening the clause after a colon.
	afterColon = regexp.MustCompile(`: ([A-Z][\w&/]*)`)
)

// checkSentenceMechanics catches two composition slips: a verb that
// disagrees with the count before it ("1 of 20 people already work"), and a practice
// phrase capitalised after a colon ("looks like this: A person ...") — only a stage
// name, a proper noun, may open a clause there.
func checkSentenceMechanics(t *testing.T, where, s string) {
	t.Helper()
	for _, m := range countVerb.FindAllStringSubmatch(s, -1) {
		singular := m[1] == "1"
		if singularVerb := strings.HasSuffix(m[2], "s"); singular != singularVerb {
			t.Errorf("%s: verb %q disagrees with the count %q in %q", where, m[2], m[1], s)
		}
	}
	for _, m := range afterColon.FindAllStringSubmatch(s, -1) {
		proper := false
		for _, st := range stageTitles {
			if strings.HasPrefix(st, m[1]) {
				proper = true
			}
		}
		if !proper {
			t.Errorf("%s: %q is capitalised after a colon in %q", where, m[1], s)
		}
	}
}

// TestCellDefinition_AgreesWithPracticePhrase holds the two surfaces one page renders
// for a stand-in cell to the same practice: the stage cell's definition
// (cellDefinitionFor) and the constraint copy's phrase (practiceText). A page that
// served the level's own Plan L3 definition beside the stand-in phrase would
// contradict itself.
func TestCellDefinition_AgreesWithPracticePhrase(t *testing.T) {
	for stage, levels := range standIns {
		for level, want := range levels {
			if got := cellDefinitionFor(stage, level); got != want {
				t.Errorf("%s/%s: cell definition = %q, want the stand-in %q", stageKey(stage), levelKey(level), got, want)
			}
		}
	}
	plan, l3 := aidlcv1.AidlcStage_AIDLC_STAGE_PLAN, aidlcv1.AidlcLevel_AIDLC_LEVEL_L3
	def := strings.ToLower(cellDefinitionFor(plan, l3))
	phrase := strings.ToLower(practiceText(plan, l3, false))
	for _, shared := range []string{"session", "approves the result", "merge"} {
		if !strings.Contains(def, shared) || !strings.Contains(phrase, shared) {
			t.Errorf("plan/L3: definition %q and phrase %q do not both describe %q", def, phrase, shared)
		}
	}
	for _, definitionOnly := range []string{"task breakdown", "before any of it runs"} {
		if strings.Contains(def, definitionOnly) || strings.Contains(phrase, definitionOnly) {
			t.Errorf("plan/L3: definition %q or phrase %q carries the level definition's own %q", def, phrase, definitionOnly)
		}
	}
}

// TestCellCopies_AgreeWithEveryStandIn holds the constraint copy (cellCopies) to the
// stand-in a rule measures, as TestCellDefinition_AgreesWithPracticePhrase does for
// the practice phrase: a move or finding that names the level definition's own
// practice beside a cell scored on a different one would contradict the page.
func TestCellCopies_AgreeWithEveryStandIn(t *testing.T) {
	plan := aidlcv1.AidlcStage_AIDLC_STAGE_PLAN
	l3, l4 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L3, aidlcv1.AidlcLevel_AIDLC_LEVEL_L4
	required := map[[2]int32][]string{
		{int32(plan), int32(l3)}: {"session", "merge"},
		{int32(plan), int32(l4)}: {"without a person approving"},
	}
	rubricOnly := []string{"break", "task", "before any"}
	for stage, levels := range standIns {
		for level := range levels {
			cc, ok := cellCopyFor(stage, level, false)
			if !ok {
				t.Errorf("%s/%s: a stand-in cell has no constraint copy", stageKey(stage), levelKey(level))
				continue
			}
			text := strings.ToLower(cc.action + " " + cc.finding)
			for _, want := range required[[2]int32{int32(stage), int32(level)}] {
				if !strings.Contains(text, want) {
					t.Errorf("%s/%s: copy %q does not describe the stand-in (missing %q)", stageKey(stage), levelKey(level), text, want)
				}
			}
			if stage == plan && (level == l3 || level == l4) {
				for _, bad := range rubricOnly {
					if strings.Contains(text, bad) {
						t.Errorf("%s/%s: copy %q carries the rubric's own %q", stageKey(stage), levelKey(level), text, bad)
					}
				}
			}
		}
	}
}

// TestTwoShareClause_OnlyAppliesAtL5 pins practiceText's level guard: twoShareClauses
// is keyed on stage alone with no level check of its own, so without the guard it
// would win at every level: a rung_ or ladder_ finding on review or product-spec at
// L1-L4 would be grounded in "a review practice the organisation enforces...", which
// no rule measures at those levels. practiceText checks level == L5 before consulting
// the map; this pins that an L1-L4 finding on either two-share stage never quotes it.
func TestTwoShareClause_OnlyAppliesAtL5(t *testing.T) {
	rs := rules.ForVersion("v1")
	mc := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	cases := []struct {
		stage aidlcv1.AidlcStage
		level aidlcv1.AidlcLevel
	}{
		{aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, aidlcv1.AidlcLevel_AIDLC_LEVEL_L1},
		{aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, aidlcv1.AidlcLevel_AIDLC_LEVEL_L3},
		{aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC, aidlcv1.AidlcLevel_AIDLC_LEVEL_L1},
		{aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC, aidlcv1.AidlcLevel_AIDLC_LEVEL_L3},
		{aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC, aidlcv1.AidlcLevel_AIDLC_LEVEL_L4},
	}
	banned := []string{"organisation enforces", "declared policy", "declared review policy"}
	for _, tc := range cases {
		b := Bottleneck{ID: "rung_x", Stage: tc.stage, Level: tc.level, HasMove: true, Terms: &MoveTerms{Hits: 5, Population: 50, Grain: mc}}
		for _, got := range []string{bottleneckTitle(rs, b), bottleneckBody(rs, b), moveTitle(rs, b), moveBody(rs, b)} {
			for _, phrase := range banned {
				if strings.Contains(got, phrase) {
					t.Errorf("%s/%s: wrongly quoted the two-share L5 phrase %q in %q", stageKey(tc.stage), levelKey(tc.level), phrase, got)
				}
			}
		}
	}
}

// TestPracticePhrase_CoversAllFortyFiveCells pins the phrase table's
// requirement: one verb-led phrase per (stage, level), for all 45 cells — either in
// practicePhrase directly, or (product-spec/L5, review/L5) in twoShareClauses, which
// practiceText consults first at L5. A cell missing here would fall back to
// practiceText's "Stage's Level practice" stand-in, which is exactly the bare
// stage/level coordinate the whole table exists to avoid.
func TestPracticePhrase_CoversAllFortyFiveCells(t *testing.T) {
	for _, stage := range stageOrder {
		for _, level := range ladderOrder {
			if level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L5 {
				if _, ok := twoShareClauses[stage]; ok {
					continue
				}
			}
			if p, ok := practicePhrase[stage][level]; !ok || strings.TrimSpace(p) == "" {
				t.Errorf("%s/%s: no practicePhrase entry", stageKey(stage), levelKey(level))
			}
		}
	}
}

// TestPracticePhrase_MatchesStandIn pins standInPhrases' override: practicePhrase
// alone describes each level's own definition, so a cell whose rule measures a
// narrower stand-in (definitions.go's standIns) would, without the override, quote
// materially different text: Plan L3 measures "planned in an AI session, approved on
// merge", not "a task breakdown... approved before any of it runs". For every (stage,
// level) standIns carries, this pins the phrase practiceText serves against a keyword
// the stand-in's own text names and, where the base practicePhrase entry says
// something materially different, against a keyword the served phrase must not carry,
// so a regression that drops the override is caught by content, not a snapshot.
func TestPracticePhrase_MatchesStandIn(t *testing.T) {
	type expect struct {
		mustContain    []string
		mustNotContain []string
	}
	// One entry per (stage, level) standIns carries, except product-spec/L5 and
	// review/L5: two-share cells practiceText resolves through twoShareClauses before
	// either practice table.
	cases := map[aidlcv1.AidlcStage]map[aidlcv1.AidlcLevel]expect{
		aidlcv1.AidlcStage_AIDLC_STAGE_PLAN: {
			aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: {
				mustContain:    []string{"session"},
				mustNotContain: []string{"task breakdown", "before any of it runs"},
			},
			aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: {
				mustContain:    []string{"session"},
				mustNotContain: []string{"breaks the work down"},
			},
			aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: {
				mustContain: []string{"enforce"},
			},
		},
		aidlcv1.AidlcStage_AIDLC_STAGE_CODE: {
			aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: {mustContain: []string{"enforce"}},
		},
		aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA: {
			aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: {mustContain: []string{"enforce"}},
		},
	}
	for stage, levels := range cases {
		for level, exp := range levels {
			if _, ok := standIns[stage][level]; !ok {
				t.Fatalf("%s/%s: test case has no matching standIns entry; keep this test in sync with definitions.go", stageKey(stage), levelKey(level))
			}
			got := strings.ToLower(practiceText(stage, level, false))
			for _, want := range exp.mustContain {
				if !strings.Contains(got, want) {
					t.Errorf("%s/%s: phrase %q does not mention %q, the stand-in's own practice", stageKey(stage), levelKey(level), got, want)
				}
			}
			for _, unwanted := range exp.mustNotContain {
				if strings.Contains(got, unwanted) {
					t.Errorf("%s/%s: phrase %q wrongly carries %q, which the rule does not measure", stageKey(stage), levelKey(level), got, unwanted)
				}
			}
		}
	}

	// Confirm standIns carries no substitute this test forgot to check.
	for stage, levels := range standIns {
		for level := range levels {
			if level == aidlcv1.AidlcLevel_AIDLC_LEVEL_L5 {
				if _, ok := twoShareClauses[stage]; ok {
					continue
				}
			}
			if _, ok := cases[stage][level]; !ok {
				t.Errorf("%s/%s: standIns carries a substitute this test does not check", stageKey(stage), levelKey(level))
			}
		}
	}

	// The base entry still describes the level's own definition, so it is the
	// override, not an edit to the base table, that serves the stand-in.
	if got := practicePhrase[aidlcv1.AidlcStage_AIDLC_STAGE_PLAN][aidlcv1.AidlcLevel_AIDLC_LEVEL_L3]; !strings.Contains(got, "before any of it runs") {
		t.Errorf("plan/L3 base phrase = %q, want the level definition's own practice", got)
	}
}

// TestProfileCells_PeopleShareHidesCountsBelowFloor// TestProfileCells_PeopleShareHidesCountsBelowFloor pins the small-group rule: the
// smallest awardable people population renders its band and universe, and no count or
// percentage anywhere on the cell; the same level at a 40-person org renders its
// counts.
func TestProfileCells_PeopleShareHidesCountsBelowFloor(t *testing.T) {
	rs := rules.ForVersion("v1")
	stage := aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC
	cellAt := func(hits, population int) *aidlcv1.MaturityCell {
		rd := NewReadings()
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_HARNESS_SEAT_HOLDERS, stage, withPeople(resolved(float64(hits)), hits))
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, stage, resolved(float64(population)))
		for _, st := range Score(rs, rd, OrgRead{}, nil).Profile.GetStages() {
			if st.GetStage() != stage {
				continue
			}
			for _, c := range st.GetCells() {
				if c.GetLevel() == aidlcv1.AidlcLevel_AIDLC_LEVEL_L1 {
					return c
				}
			}
		}
		t.Fatal("no product_spec L1 cell")
		return nil
	}
	text := func(c *aidlcv1.MaturityCell) string {
		parts := append([]string{}, c.GetTerms()...)
		for _, e := range c.GetEvidence() {
			parts = append(parts, e.GetLabel()+"="+e.GetValue())
		}
		return strings.Join(parts, "\n")
	}

	small := cellAt(4, rs.PopulationFloors.People)
	if small.GetState() != aidlcv1.CellState_CELL_STATE_MET {
		t.Fatalf("smallest awardable population: state = %v, want met", small.GetState())
	}
	got := text(small)
	if len(small.GetTerms()) == 0 || strings.ContainsAny(got, "0123456789%") {
		t.Errorf("small people share leaks a count or percentage:\n%s", got)
	}
	for _, e := range small.GetEvidence() {
		if strings.ContainsAny(e.GetValue(), "0123456789%") {
			t.Errorf("evidence value %q is numeric for a hidden people share", e.GetValue())
		}
	}
	if !strings.Contains(got, "more than half") {
		t.Errorf("small people share lacks its band:\n%s", got)
	}

	big := cellAt(30, 40)
	if got := text(big); !strings.Contains(got, "30 of the 40") || !strings.Contains(got, "75%") {
		t.Errorf("a 40-person org renders its counts, got:\n%s", got)
	}

	// Below the population floor the cell abstains before a share is read, and still
	// must not print the group's size: only the floor it asks for.
	tiny := cellAt(1, 3)
	if tiny.GetState() != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE {
		t.Fatalf("population 3: state = %v, want abstains", tiny.GetState())
	}
	got = text(tiny)
	if len(tiny.GetTerms()) == 0 || strings.Contains(got, "3") {
		t.Errorf("a 3-person population leaks its size:\n%s", got)
	}
	for _, e := range tiny.GetEvidence() {
		if strings.ContainsAny(e.GetValue(), "0123456789%") {
			t.Errorf("evidence value %q is numeric for a small people population", e.GetValue())
		}
	}
}

// TestBottleneckCopy_HiddenPeopleShareCarriesNoNumber holds every constraint text over
// a people share under the floor to the band and the universe.
func TestBottleneckCopy_HiddenPeopleShareCarriesNoNumber(t *testing.T) {
	rs := rules.ForVersion("v1")
	pp := aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE
	stage := aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC
	l1 := aidlcv1.AidlcLevel_AIDLC_LEVEL_L1
	bs := []Bottleneck{
		{ID: bottleneckIDAdoptionReach, Terms: &MoveTerms{Hits: 10, Population: 18, Grain: pp}},
		{ID: bottleneckIDAdoptionReach, Terms: &MoveTerms{Hits: 9, Population: 18, Grain: pp}},
		{ID: "rung_product_spec_L1", Stage: stage, Level: l1, Terms: &MoveTerms{Hits: 2, Population: 18, Grain: pp}},
		{ID: "ladder_product_spec", Stage: stage, Level: l1, Terms: &MoveTerms{Hits: 1, Population: 18, Grain: pp}},
	}
	for _, b := range bs {
		for name, s := range map[string]string{
			"title": bottleneckTitle(rs, b), "body": bottleneckBody(rs, b),
			"move title": moveTitle(rs, b), "move body": moveBody(rs, b),
		} {
			if strings.ContainsAny(strings.ReplaceAll(s, "last 90 days", ""), "0123456789%") {
				t.Errorf("%s %s carries a number: %q", b.ID, name, s)
			}
		}
	}
	// The title already carries the band; the body must not say it again.
	if got := bottleneckBody(rs, bs[0]); strings.Contains(got, "Most") || strings.Contains(got, "no AI-written merge") {
		t.Errorf("hidden adoption body restates its title: %q", got)
	}
	if got := bottleneckBody(rs, bs[2]); !strings.Contains(got, "fewer than one in five of your people followed this practice") {
		t.Errorf("rung body = %q", got)
	}
}
