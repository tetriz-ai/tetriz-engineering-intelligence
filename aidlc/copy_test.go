// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"unicode"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// isCompleteSentence is the requirement every served title and move title must meet:
// starts with a capital letter, ends with a period, no double space, and at
// least four words — so a title can never again be a bare noun phrase (code/L1's old
// fallback, "Completions and snippets") or a template fragment ("Grow this until
// completions and snippets").
func isCompleteSentence(s string) bool {
	if s == "" || !strings.HasSuffix(s, ".") {
		return false
	}
	r := []rune(s)
	if !unicode.IsUpper(r[0]) {
		return false
	}
	if strings.Contains(s, "  ") {
		return false
	}
	return len(strings.Fields(s)) >= 4
}

// These tests pin the reader-facing copy: every abstain site's sentence with its
// composed numbers, every gap's sentence, and the four moves with the distance rule
// per grain. The wording rules the copy owes (no em dash, no operators, no
// identifiers) are swept over every served string in TestServedCopy_ReadsPlain.

// TestLevelTitles_NameEachLevel holds the level-label map to the five level names the
// AidlcLevel enum documents, compared case-insensitively: the enum's names are the
// display case ("Assisted"), the labels the mid-sentence case ("assisted").
func TestLevelTitles_NameEachLevel(t *testing.T) {
	levelNames := map[aidlcv1.AidlcLevel]string{
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1: "Assisted",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2: "Conversational",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3: "Supervised agentic",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4: "Autonomous",
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5: "Orchestrated",
	}
	if got, want := len(levelTitles), len(levelNames); got != want {
		t.Fatalf("levelTitles has %d entries, want %d (one per level)", got, want)
	}
	for lv, want := range levelNames {
		if got := levelTitle(lv); !strings.EqualFold(got, want) {
			t.Errorf("%s: level label %q, level name %q", levelKey(lv), got, want)
		}
	}
}

func reasonOf(t *testing.T, res *Result, stage aidlcv1.AidlcStage, level aidlcv1.AidlcLevel) string {
	t.Helper()
	for _, c := range res.Cells {
		if c.Stage == stage && c.Level == level {
			return c.Reason
		}
	}
	t.Fatalf("no cell %s/%s", stageKey(stage), levelKey(level))
	return ""
}

// TestReasons_ReadInTheReadersWords drives each abstain path the scorer has and pins
// the sentence it attaches, with the floors and the window taken from the ruleset.
func TestReasons_ReadInTheReadersWords(t *testing.T) {
	rs := rules.ForVersion("v1")
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	seats := func(rd *Readings, n int) {
		rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(float64(n)), n))
	}
	tests := []struct {
		name  string
		rd    func() *Readings
		stage aidlcv1.AidlcStage
		level aidlcv1.AidlcLevel
		want  string
	}{
		{
			name: "a number could not be read (row absent)",
			rd: func() *Readings {
				rd := NewReadings()
				seats(rd, 10)
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(100))
				return rd // the AI-authored numerator is never set
			},
			stage: code, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
			want: reasonTermUnreadable,
		},
		{
			name: "population floor names the floor and the window",
			rd: func() *Readings {
				rd := NewReadings()
				seats(rd, 10)
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(3), 2))
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(8))
				return rd
			},
			stage: code, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
			want: "Fewer than 20 pieces of this work in the last 90 days, too few to call it a practice either way. This fills in as more of this work happens.",
		},
		{
			name: "people floor names the people floor",
			rd: func() *Readings {
				rd := NewReadings()
				seats(rd, 10)
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(60), 1))
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(180))
				return rd
			},
			stage: code, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
			want: "Enough work, but fewer than 2 people did it; a practice needs more than one contributor.", // code L1 asks for 2 people
		},
		{
			name: "no population names the window and whose it is",
			rd: func() *Readings {
				rd := NewReadings()
				seats(rd, 10)
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(0), 0))
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(0))
				return rd
			},
			stage: code, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
			want: "No AI-assisted work of this kind was merged in the last 90 days, so there is nothing to measure yet. Yours: this fills in once your team ships this kind of work with AI.",
		},
		{
			name: "unmeasurable population names the source",
			rd: func() *Readings {
				rd := NewReadings()
				seats(rd, 10)
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(0), 0))
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, withNumRemainder(resolved(0), 30))
				return rd
			},
			stage: code, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
			want: "We can see this work happened but cannot read enough of it to decide. Ours: merged feature and bugfix PRs by people with a seat are only partly captured.",
		},
		{
			name: "the range straddles the cut",
			rd: func() *Readings {
				rd := NewReadings()
				seats(rd, 10)
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(withNumRemainder(resolved(48), 120), 6))
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(180))
				return rd
			},
			stage: code, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
			want: reasonStraddle,
		},
		{
			name: "count exceeds total",
			rd: func() *Readings {
				rd := NewReadings()
				seats(rd, 10)
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(200), 6))
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(180))
				return rd
			},
			stage: code, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
			want: reasonReadingsDisagree,
		},
		{
			name: "control failed",
			rd: func() *Readings {
				rd := NewReadings()
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, withPeople(resolved(30), 6))
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(40))
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0))
				return rd
			},
			stage: code, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L3,
			want: reasonControlFailed,
		},
		{
			name: "control unreadable",
			rd: func() *Readings {
				rd := NewReadings()
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, withPeople(resolved(30), 6))
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(40))
				return rd
			},
			stage: code, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L3,
			want: reasonControlUnreadable,
		},
		{
			name: "instrumented only",
			rd: func() *Readings {
				rd := NewReadings()
				seats(rd, 12)
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_HARNESS_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC, withPeople(resolved(0), 0))
				return rd
			},
			stage: aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
			want: reasonInstrumentedOnly,
		},
		{
			name:  "judgment with no gap named",
			rd:    NewReadings,
			stage: aidlcv1.AidlcStage_AIDLC_STAGE_PLAN, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
			want: reasonJudgment,
		},
		{
			name:  "a not-measurable cell carries its gap's sentence",
			rd:    NewReadings,
			stage: aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L5,
			want: reasonForGap(aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G6, nil, g2BlockerNotMeasured),
		},
		{
			name:  "implied floor with nothing met above",
			rd:    NewReadings,
			stage: code, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L2,
			want: reasonImpliedFloorPending,
		},
		{
			// Plan L3's gate is the tracker: no issue in the window closes it on G2.
			name: "a gated cell whose gate is closed carries its gap's sentence",
			rd: func() *Readings {
				rd := NewReadings()
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PLANNED_AI_MERGED_PRS_HUMAN_REVIEWED, aidlcv1.AidlcStage_AIDLC_STAGE_PLAN, gapBlockedReading())
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_MERGED_PRS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, resolved(40))
				return rd
			},
			stage: aidlcv1.AidlcStage_AIDLC_STAGE_PLAN, level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L3,
			want: reasonForGap(aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G2, nil, g2BlockerNoIssues),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := Score(rs, tt.rd(), OrgRead{}, nil)
			if got := reasonOf(t, res, tt.stage, tt.level); got != tt.want {
				t.Errorf("reason = %q\nwant     %q", got, tt.want)
			}
		})
	}
}

// TestGapReasons_EveryGapHasOne pins that all nine gaps carry a sentence naming whose
// it is, that only G4 reads the G4 blockers and only G2 the G2 blocker, and that each
// G2 blocker names one owner — never "Yours and ours" for a case that is one of them.
func TestGapReasons_EveryGapHasOne(t *testing.T) {
	for g := aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G1; g <= aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G9; g++ {
		plain := reasonForGap(g, nil, g2BlockerNotMeasured)
		if plain == "" || !strings.HasPrefix(plain, "Ours") && !strings.HasPrefix(plain, "Yours") {
			t.Errorf("%s: %q does not say whose it is", g, plain)
		}
		if g != aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4 && plain != reasonForGap(g, []g4Blocker{g4BlockerUndeclared}, g2BlockerNotMeasured) {
			t.Errorf("%s changes with the G4 blockers; only G4 may", g)
		}
		if g != aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G2 && plain != reasonForGap(g, nil, g2BlockerNoIssues) {
			t.Errorf("%s changes with the G2 blocker; only G2 may", g)
		}
	}
	if got := reasonForGap(aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_UNSPECIFIED, nil, g2BlockerNotMeasured); got == "" {
		t.Error("an unspecified gap still needs a sentence")
	}

	g2 := aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G2
	for _, tt := range []struct {
		blocker g2Blocker
		prefix  string
	}{
		{g2BlockerNotMeasured, "Ours:"},
		{g2BlockerNoIssues, "Yours:"},
	} {
		if got := reasonForGap(g2, nil, tt.blocker); !strings.HasPrefix(got, tt.prefix) {
			t.Errorf("G2 blocker %d: %q does not start with %q", tt.blocker, got, tt.prefix)
		}
	}
}

// TestReasonForG4_NamesTheBlockingCheck pins G4's cell reason per blocker: each names
// whose it is and the fact that closed the cell, and a cell missing its enforcement
// and its declaration says both rather than implying either alone opens it.
func TestReasonForG4_NamesTheBlockingCheck(t *testing.T) {
	tests := []struct {
		name     string
		blockers []g4Blocker
		prefix   string
		carries  []string
		never    []string
	}{
		{"not honoured", []g4Blocker{g4BlockerNotHonoured}, "Ours:", []string{"approvals are not checked against your review policy"}, []string{"declare", "harness", "agent setup"}},
		{"undeclared", []g4Blocker{g4BlockerUndeclared}, "Yours:", []string{"not declared yet"}, []string{"harness", "agent setup"}},
		{"unenforced", []g4Blocker{g4BlockerUnenforced}, "Yours:", []string{"your organisation enforces in its agent setup", "how widely it is followed"}, []string{"declare", "harness", "cannot see how many people"}},
		{"unenforced and undeclared", []g4Blocker{g4BlockerUnenforced, g4BlockerUndeclared}, "Yours:", []string{"your organisation enforces in its agent setup", "declared review policy", "neither"}, []string{"once it is", "harness"}},
		{"unenforced and not honoured", []g4Blocker{g4BlockerUnenforced, g4BlockerNotHonoured}, "Yours and ours:", []string{"your organisation enforces in its agent setup", "approvals are not checked against your review policy"}, []string{"declare", "harness"}},
		{"uninstrumented", []g4Blocker{g4BlockerUninstrumented}, "Ours:", []string{"practices your organisation enforces and its review policy are not read"}, []string{"declare", "approvals are not checked"}},
	}
	seen := map[string]string{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := reasonForGap(aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_G4, tt.blockers, g2BlockerNotMeasured)
			if !strings.HasPrefix(got, tt.prefix) {
				t.Errorf("%q does not start with %q", got, tt.prefix)
			}
			for _, w := range tt.carries {
				if !strings.Contains(got, w) {
					t.Errorf("%q does not carry %q", got, w)
				}
			}
			for _, w := range tt.never {
				if strings.Contains(strings.ToLower(got), strings.ToLower(w)) {
					t.Errorf("%q must not carry %q", got, w)
				}
			}
			if other, dup := seen[got]; dup {
				t.Errorf("%q reads the same as %q's", got, other)
			}
			seen[got] = tt.name
		})
	}
}

// TestItemsToClear pins the distance rule per grain: for merged changes every new
// item joins the total too, so 53 of 304 at a 0.20 cut needs 10, 3 of 20 needs 2 (a
// fixed total would have said 5), and 140 of 320 at a 0.50 cut needs 41; for people the
// roster is fixed, so 3 of 20 at 0.20 needs 2 more people (4 of 20 is exactly the cut
// and does not clear; 5 of 20 does). An exact multiple of the cut is not above it, as
// the scorer's strict band test says.
func TestItemsToClear(t *testing.T) {
	mc := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	pp := aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE
	tests := []struct {
		name  string
		terms MoveTerms
		cut   float64
		want  int64
	}{
		{"53 of 304 needs 10 at a fifth", MoveTerms{Hits: 53, Population: 304, Grain: mc}, 0.20, 10},
		{"3 of 20 needs 2 at a fifth", MoveTerms{Hits: 3, Population: 20, Grain: mc}, 0.20, 2},
		{"140 of 320 needs 41 at half", MoveTerms{Hits: 140, Population: 320, Grain: mc}, 0.50, 41},
		{"57 of 289 needs 2 at a fifth (58 of 290 is exactly the cut)", MoveTerms{Hits: 57, Population: 289, Grain: mc}, 0.20, 2},
		{"already above needs none", MoveTerms{Hits: 100, Population: 200, Grain: mc}, 0.20, 0},
		{"people: 3 of 20 needs 2 (4 of 20 is exactly the cut)", MoveTerms{Hits: 3, Population: 20, Grain: pp}, 0.20, 2},
		{"people: 4 of 20 needs 1", MoveTerms{Hits: 4, Population: 20, Grain: pp}, 0.20, 1},
		{"people: the roster caps the answer", MoveTerms{Hits: 0, Population: 1, Grain: pp}, 0.50, 1},
		{"zero total", MoveTerms{Hits: 0, Population: 0, Grain: mc}, 0.20, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := itemsToClear(tt.terms, tt.cut); got != tt.want {
				t.Errorf("itemsToClear = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestPeopleToPassHalf pins the adoption move's count: 11 of 18 passive needs 3 to fall
// below half; 9 of 18 needs 1; 10 of 19 needs 1; 8 of 18 is already under.
func TestPeopleToPassHalf(t *testing.T) {
	tests := []struct {
		passive, roster, want int64
	}{{11, 18, 3}, {9, 18, 1}, {10, 19, 1}, {8, 18, 0}, {1, 1, 1}, {0, 0, 0}}
	for _, tt := range tests {
		if got := peopleToPassHalf(MoveTerms{Hits: tt.passive, Population: tt.roster}); got != tt.want {
			t.Errorf("peopleToPassHalf(%d of %d) = %d, want %d", tt.passive, tt.roster, got, tt.want)
		}
	}
}

// TestMoves_ComposedFromTheNumbers pins the four moves' prose from the numbers on the
// bottleneck, and the fixed sentence when a bottleneck carries none.
func TestMoves_ComposedFromTheNumbers(t *testing.T) {
	rs := rules.ForVersion("v1")
	mc := aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES
	pp := aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE
	tests := []struct {
		name string
		b    Bottleneck
		want string
	}{
		{
			name: "review reach",
			b:    Bottleneck{ID: bottleneckIDReviewReach, Terms: &MoveTerms{Hits: 140, Population: 320, Grain: mc}},
			want: "Once more than half of your AI-written pull requests carry a recorded review, review becomes the usual gate for AI-written code. That is about 41 more reviewed pull requests at today's volume, since each one also adds to the total.",
		},
		{
			name: "review reach with unread reviewer lists counts from the recorded floor",
			b:    Bottleneck{ID: bottleneckIDReviewReach, Terms: &MoveTerms{Hits: 5, Population: 30, Unobserved: 3, Grain: mc}},
			want: "Once more than half of your AI-written pull requests carry a recorded review, review becomes the usual gate for AI-written code. That is about 21 more reviewed pull requests at today's volume, since each one also adds to the total.",
		},
		{
			name: "review reach with no numbers states the impact alone",
			b:    Bottleneck{ID: bottleneckIDReviewReach},
			want: "Once more than half of your AI-written pull requests carry a recorded review, review becomes the usual gate for AI-written code.",
		},
		{
			name: "adoption reach, a roster of 20 or more, names the people still needed",
			b:    Bottleneck{ID: bottleneckIDAdoptionReach, Terms: &MoveTerms{Hits: 14, Population: 24, Grain: pp}},
			want: "Once more than half of your people have an AI-written merge, AI use is no longer limited to a few. That is about 3 more people.",
		},
		{
			name: "adoption reach under 20 people leaves the distance out",
			b:    Bottleneck{ID: bottleneckIDAdoptionReach, Terms: &MoveTerms{Hits: 11, Population: 18, Grain: pp}},
			want: "Once more than half of your people have an AI-written merge, AI use is no longer limited to a few.",
		},
		{
			name: "ladder stalled",
			b:    Bottleneck{ID: "ladder_test_qa", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L2, Terms: &MoveTerms{Hits: 3, Population: 20, Grain: mc}},
			want: "Once more than one in five of these pull requests work this way, your map counts it as a practice your team is adopting. That is about 2 more such pull requests at today's volume.",
		},
		{
			name: "ladder stalled with no numbers states the impact alone",
			b:    Bottleneck{ID: "ladder_test_qa", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L2},
			want: "Once this is common in your testing work, your map counts it as a practice your team is adopting.",
		},
		{
			name: "rung missed",
			b:    Bottleneck{ID: "rung_code_L4", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_CODE, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L4, Terms: &MoveTerms{Hits: 53, Population: 304, Grain: mc}},
			want: "Once more than one in five of these pull requests work this way, your map counts it as a practice your team is adopting. That is about 10 more such pull requests at today's volume.",
		},
		{
			name: "rung missed, a single item needed, agrees in number",
			b:    Bottleneck{ID: "rung_code_L4", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_CODE, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L4, Terms: &MoveTerms{Hits: 57, Population: 289, Grain: mc}},
			want: "Once more than one in five of these pull requests work this way, your map counts it as a practice your team is adopting. That is about 2 more such pull requests at today's volume.",
		},
		{
			name: "rung missed over people counts people",
			b:    Bottleneck{ID: "rung_plan_L5", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_PLAN, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L5, Terms: &MoveTerms{Hits: 3, Population: 40, Grain: pp}},
			want: "Once more than one in five of these people work this way, your map counts it as a practice your team is adopting. That is about 6 more people.",
		},
		{
			name: "rung missed with no numbers states the impact alone",
			b:    Bottleneck{ID: "rung_code_L4", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_CODE, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L4},
			want: "Once this is common in your coding work, your map counts it as a practice your team is adopting.",
		},
		{
			name: "rung missed: code, 28 of 172 (a distance of 9; 8 lands exactly on the line)",
			b:    Bottleneck{ID: "rung_code_L4", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_CODE, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L4, Terms: &MoveTerms{Hits: 28, Population: 172, Grain: mc}},
			want: "Once more than one in five of these pull requests work this way, your map counts it as a practice your team is adopting. That is about 9 more such pull requests at today's volume.",
		},
		{
			name: "rung missed: review, 40 of 438 (a distance of 60)",
			b:    Bottleneck{ID: "rung_review_L4", Stage: aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, Level: aidlcv1.AidlcLevel_AIDLC_LEVEL_L4, Terms: &MoveTerms{Hits: 40, Population: 438, Grain: mc}},
			want: "Once more than one in five of these pull requests work this way, your map counts it as a practice your team is adopting. That is about 60 more bot-cleared pull requests at today's volume.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := moveBody(rs, tt.b); got != tt.want {
				t.Errorf("moveBody =\n  %q\nwant\n  %q", got, tt.want)
			}
		})
	}
}

// TestServedCopy_ReadsPlain sweeps every served string on a grid that fires every
// bottleneck kind: no reason, term, title, body or move carries an operator, an em dash
// or an internal identifier such as a snake_case stage key.
func TestServedCopy_ReadsPlain(t *testing.T) {
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(20), 20))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(180), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(520))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, withPeople(resolved(40), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(180))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0.55))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code, withPeople(resolved(35), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_HARNESS_SEAT_HOLDERS, deploy, withPeople(resolved(1), 1))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED, deploy, withPeople(resolved(1), 1))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SHIPPED_BRANCH_SEAT_LINKED_PRS_MERGED, deploy, resolved(30))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, deploy, resolved(0.6))
	res := Score(rules.ForVersion("v1"), rd, OrgRead{HasAdoptionCohort: true, AdoptionDenominatorPeople: 18, AdoptionPassivePeople: 11}, nil)
	if len(res.Profile.GetRecommendations()) == 0 {
		t.Fatal("the fixture should fire at least one move")
	}
	check := func(where, txt string) {
		if strings.ContainsAny(txt, "<>=—") {
			t.Errorf("%s serves %q", where, txt)
		}
		for _, id := range []string{"test_qa", "ci_cd", "product_spec", "tech_spec", "review_reach", "adoption_reach"} {
			if strings.Contains(txt, id) {
				t.Errorf("%s serves an identifier %q in %q", where, id, txt)
			}
		}
	}
	for _, st := range res.Profile.GetStages() {
		check("stage abstain", st.GetAbstainReason())
		for _, c := range st.GetCells() {
			for _, txt := range append([]string{c.GetDefinition(), c.GetReason()}, c.GetTerms()...) {
				check(stageKey(st.GetStage())+"/"+levelKey(c.GetLevel()), txt)
			}
		}
	}
	for _, b := range res.Profile.GetBottlenecks() {
		check("bottleneck "+b.GetId(), b.GetTitle())
		check("bottleneck "+b.GetId(), b.GetBody())
	}
	for _, r := range res.Profile.GetRecommendations() {
		check("move "+r.GetId(), r.GetTitle())
		check("move "+r.GetId(), r.GetBody())
		if r.GetBody() == "" || r.GetTitle() == "" {
			t.Errorf("move %s has empty copy", r.GetId())
		}
	}
	for _, b := range res.Bottlenecks {
		check("internal "+b.ID, bottleneckTitle(res.rs, b))
		check("internal "+b.ID, bottleneckBody(res.rs, b))
		check("internal "+b.ID, moveBody(res.rs, b))
	}
	for i, item := range res.Profile.GetCoverageItems() {
		check(fmt.Sprintf("coverage_items[%d]", i), item.GetSentence())
	}
}

// signalKeyToken, gapIDToken and ofNCellsPhrase find the internal tokens a reader must
// never see. stageEnumTokens is deliberately the four multi-word stage keys only
// (product_spec, tech_spec, test_qa, ci_cd): the single-word keys (plan, code, review,
// deploy, operate) double as ordinary English (bottleneckBody's "Recorded review
// reaches..." legitimately carries "review"), so they cannot be swept as identifiers
// without flagging copy that already reads plain.
var (
	signalKeyToken  = regexp.MustCompile(`SIGNAL_KEY_[A-Z0-9_]+`)
	gapIDToken      = regexp.MustCompile(`\bG[1-9]\b`)
	ofNCellsPhrase  = regexp.MustCompile(`of \d+ cells`)
	stageEnumTokens = []string{"product_spec", "tech_spec", "test_qa", "ci_cd"}
	ruleIDPrefixes  = []string{"rung_", "ladder_", "gap_"}
)

// checkNoInternalTokens fails where against every internal token: a stage enum key, a
// signal key, a gap id, a rule id, or an "of N cells" phrase.
func checkNoInternalTokens(t *testing.T, where, txt string) {
	t.Helper()
	if txt == "" {
		return
	}
	for _, tok := range stageEnumTokens {
		if strings.Contains(txt, tok) {
			t.Errorf("%s serves the stage enum key %q in %q", where, tok, txt)
		}
	}
	if m := signalKeyToken.FindString(txt); m != "" {
		t.Errorf("%s serves the signal key %q in %q", where, m, txt)
	}
	if m := gapIDToken.FindString(txt); m != "" {
		t.Errorf("%s serves the gap id %q in %q", where, m, txt)
	}
	for _, pfx := range ruleIDPrefixes {
		if strings.Contains(txt, pfx) {
			t.Errorf("%s serves the rule id prefix %q in %q", where, pfx, txt)
		}
	}
	if m := ofNCellsPhrase.FindString(txt); m != "" {
		t.Errorf("%s serves the retired total-cells phrase %q in %q", where, m, txt)
	}
}

// allStageKeys is every stageKey() output, all nine (unlike stageEnumTokens above).
// levelKeyToken finds a bare level key (L1..L5) as a standalone token. allStageKeys is
// safe to sweep ONLY against the summary: it is composed directly from stage/level
// enums by summarise(), which only ever emits
// stageTitle's capitalised form and levelTitle's word, so a lowercase key can only
// appear there if the composer regresses to the raw enum. A single-word stage key
// ("code", "review", "plan", "deploy", "operate") doubles as ordinary English
// elsewhere ("Recorded review reaches...", "A review bot clears..."), so sweeping it
// there would flag correct copy. levelKeyToken has no such collision — a bare "L4" is
// never legitimate English — so it is swept everywhere a constraint or move renders
// (checkConstraintCopy below), not only the summary.
var (
	allStageKeys  = []string{"product_spec", "tech_spec", "plan", "code", "review", "test_qa", "ci_cd", "deploy", "operate"}
	levelKeyToken = regexp.MustCompile(`\bL[1-5]\b`)
)

// bottleneckRuleVocab matches rule vocabulary that constraint and move copy must not
// use — "was read and missed", "read on N and clear it", "the cut for emerging is
// above" — as whole-word, case-insensitive tokens. A constraint or move
// states what a reader can restate about their organisation, never the words the rule
// was decided with (read, missed, cut, L4).
// This is scoped to constraint and move copy only: the cell-reason/definition surface
// uses "read" and "cut" in their ordinary senses ("cannot read enough of it to
// decide", "a fifth of what was read") and is deliberately out of this sweep. It bans
// only the bare root forms, not every word built on them: the review copy says
// "without a person re-reading", a different word, not the rule's verdict vocabulary.
var bottleneckRuleVocab = regexp.MustCompile(`(?i)\b(read|missed|miss|cut|cuts)\b`)

// checkConstraintCopy is the strict sweep for a served constraint or move string: every
// check checkNoInternalTokens runs, plus a bare level key and the rule's own
// vocabulary — neither belongs in a reader-facing sentence.
func checkConstraintCopy(t *testing.T, where, txt string) {
	t.Helper()
	if txt == "" {
		return
	}
	checkNoInternalTokens(t, where, txt)
	if m := levelKeyToken.FindString(txt); m != "" {
		t.Errorf("%s serves the bare level key %q in %q", where, m, txt)
	}
	if m := bottleneckRuleVocab.FindString(txt); m != "" {
		t.Errorf("%s serves the rule's own vocabulary %q in %q", where, m, txt)
	}
}

// checkSummaryComposer is the strict sweep for the summary: every
// stageKey() output (not just the four unambiguous ones) plus a bare level key. It
// closes the gap the four-key sweep leaves open — a regression to
// fmt.Sprintf("%s at %s", stageKey(stage), levelKey(level)) for a single-word stage
// (e.g. "code at L3") would print no multi-word key and no SIGNAL_KEY_/gap/rule token,
// so checkNoInternalTokens alone would not catch it.
func checkSummaryComposer(t *testing.T, where, txt string) {
	t.Helper()
	if txt == "" {
		return
	}
	for _, tok := range allStageKeys {
		if strings.Contains(txt, tok) {
			t.Errorf("%s serves the stage enum key %q in %q", where, tok, txt)
		}
	}
	if m := levelKeyToken.FindString(txt); m != "" {
		t.Errorf("%s serves the bare level key %q in %q", where, m, txt)
	}
}

// tokenSweepBaseReadings is the token sweep's fixture: product_spec, tech_spec and ci_cd
// each established at L1, plus TestFixtureA_RichCodeReviewStalledDeploy's code and
// deploy pattern so a move fires.
func tokenSweepBaseReadings() *Readings {
	unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
	productSpec := aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC
	techSpec := aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC
	ciCD := aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD
	code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
	deploy := aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY

	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, unspec, withPeople(resolved(20), 20))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_PRODUCT_SPEC_HARNESS_SEAT_HOLDERS, productSpec, withPeople(resolved(15), 5))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_TECH_SPEC_HARNESS_SEAT_HOLDERS, techSpec, withPeople(resolved(15), 5))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CI_TOOL_SEAT_HOLDERS_BY_FUNCTION, ciCD, withPeople(resolved(15), 5))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS, code, withPeople(resolved(180), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_SEAT_LINKED_ARTIFACTS, code, resolved(520))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED, code, withPeople(resolved(40), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE, code, resolved(180))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, code, resolved(0.55))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_DELIVERY_PRS_LOW_TURN, code, withPeople(resolved(35), 6))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_DEPLOY_HARNESS_SEAT_HOLDERS, deploy, withPeople(resolved(1), 1))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED, deploy, withPeople(resolved(1), 1))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SHIPPED_BRANCH_SEAT_LINKED_PRS_MERGED, deploy, resolved(30))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_NON_AI_BASELINE_REVIEW_RATE, deploy, resolved(0.6))
	return rd
}

// TestTokenTest_ProfileServesNoInternalIdentifiers sweeps every string
// assembleProfile renders (summary, stage definitions and abstain reasons, cell
// reasons, terms and evidence labels, bottleneck and move titles and bodies) for a
// stage enum key, a SIGNAL_KEY_ constant, a gap id, a rule id, or "of N cells"; the
// summary gets the additional strict sweep (all nine stage keys, a bare level key)
// that only it can pass safely. The fixture adds a two-share cell and a gated cell
// whose gate is closed, so the secondary-share terms and a gap sentence are swept too.
func TestTokenTest_ProfileServesNoInternalIdentifiers(t *testing.T) {
	tests := []struct {
		name    string
		version string
		extra   func(rd *Readings) // layers readings onto the shared base
	}{
		{
			name:    "v1",
			version: "v1",
			extra: func(rd *Readings) {
				review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW
				code := aidlcv1.AidlcStage_AIDLC_STAGE_CODE
				unspec := aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED
				// review L5 composes two shares (enforcement usage, declared-policy
				// approval) as the lower band.
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, review, resolved(10))
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, review, withPeople(resolved(8), 8))
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS, unspec, resolved(40))
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS, unspec, withPeople(resolved(30), 5))
				// code L5 is a gated prevalence rule; a closed gate exercises its gap
				// sentence.
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_UNDER_ENFORCEMENT_BY_STAGE, code, gapBlockedReading())
				rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS_USING_ENFORCED_ARTIFACTS_BY_STAGE, code, gapBlockedReading())
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rd := tokenSweepBaseReadings()
			if tt.extra != nil {
				tt.extra(rd)
			}
			res := Score(rules.ForVersion(tt.version), rd, OrgRead{HasAdoptionCohort: true, AdoptionDenominatorPeople: 18, AdoptionPassivePeople: 11}, nil)
			p := res.Profile
			if len(p.GetRecommendations()) == 0 {
				t.Fatalf("%s: the fixture should fire at least one move", tt.version)
			}
			// Pins that the fixture's summary keeps naming a multi-word stage, the case the
			// strict summary sweep exists to catch; a fixture narrowed to single-word
			// stages would silently stop exercising it.
			if !strings.Contains(p.GetSummary(), "Product spec") {
				t.Fatalf("%s: fixture summary %q does not name Product spec", tt.version, p.GetSummary())
			}

			checkNoInternalTokens(t, tt.version+" summary", p.GetSummary())
			checkSummaryComposer(t, tt.version+" summary", p.GetSummary())
			for _, st := range p.GetStages() {
				checkNoInternalTokens(t, tt.version+" "+stageKey(st.GetStage())+" what_this_stage_is", st.GetWhatThisStageIs())
				checkNoInternalTokens(t, tt.version+" "+stageKey(st.GetStage())+" abstain_reason", st.GetAbstainReason())
				for _, c := range st.GetCells() {
					where := tt.version + " " + stageKey(st.GetStage()) + "/" + levelKey(c.GetLevel())
					checkNoInternalTokens(t, where+" definition", c.GetDefinition())
					checkNoInternalTokens(t, where+" reason", c.GetReason())
					for _, term := range c.GetTerms() {
						checkNoInternalTokens(t, where+" term", term)
					}
					for _, e := range c.GetEvidence() {
						checkNoInternalTokens(t, where+" evidence label", e.GetLabel())
					}
				}
			}
			for _, b := range p.GetBottlenecks() {
				checkConstraintCopy(t, tt.version+" bottleneck "+b.GetId()+" title", b.GetTitle())
				checkConstraintCopy(t, tt.version+" bottleneck "+b.GetId()+" body", b.GetBody())
			}
			for _, r := range p.GetRecommendations() {
				checkConstraintCopy(t, tt.version+" move "+r.GetId()+" title", r.GetTitle())
				checkConstraintCopy(t, tt.version+" move "+r.GetId()+" body", r.GetBody())
			}
			for i, item := range p.GetCoverageItems() {
				checkNoInternalTokens(t, fmt.Sprintf("%s coverage_items[%d]", tt.version, i), item.GetSentence())
			}
			// Result.Bottlenecks carries every subject, product included, composed
			// with the same title, body and move copy the profile renders; the sweep
			// covers it so a product-subject (gap_) constraint, dropped from the
			// org-facing profile above, is held to the same copy bar.
			for _, b := range res.Bottlenecks {
				checkConstraintCopy(t, tt.version+" internal "+b.ID+" title", bottleneckTitle(res.rs, b))
				checkConstraintCopy(t, tt.version+" internal "+b.ID+" body", bottleneckBody(res.rs, b))
				checkConstraintCopy(t, tt.version+" internal "+b.ID+" move_body", moveBody(res.rs, b))
			}
		})
	}
}

// TestReasonMalformed_SaysSuppliedButInvalid pins the malformed reason: the number was
// supplied, and is not a valid count. It is distinct from the not-supplied reason.
func TestReasonMalformed_SaysSuppliedButInvalid(t *testing.T) {
	if reasonMalformed != "Ours: one of the numbers this level needs was supplied but is not a valid count." {
		t.Errorf("reasonMalformed = %q", reasonMalformed)
	}
	if reasonMalformed == reasonTermUnreadable {
		t.Error("the malformed reason repeats the not-supplied reason")
	}
}

// TestReasonFor_NoRunNoteIsServed pins that a no-run reading's note is the cell's reason,
// in the caller's words, and that a no-run reading with no note keeps the generic one.
func TestReasonFor_NoRunNoteIsServed(t *testing.T) {
	if got := reasonFor(Reading{Status: StatusNoRunRecord, Note: "the planned pull requests could not be read"}); got != "Ours: the planned pull requests could not be read." {
		t.Errorf("noted no-run reason = %q", got)
	}
	if got := reasonFor(Reading{Status: StatusNoRunRecord}); got != reasonTermUnreadable {
		t.Errorf("unnoted no-run reason = %q, want the generic one", got)
	}
	if got := reasonFor(Reading{Status: StatusRowAbsent, Note: "x"}); got != reasonTermUnreadable {
		t.Errorf("row-absent reason = %q, want the generic one: only a no-run's note is served", got)
	}
}

// TestReasonFor_NoteIsOneClause pins how a note is served: whitespace and newlines
// collapse, a trailing period or space is dropped so the sentence ends once, and a note of
// only whitespace gets the generic reason.
func TestReasonFor_NoteIsOneClause(t *testing.T) {
	for note, want := range map[string]string{
		"ends with a period.": "Ours: ends with a period.",
		"spans\ntwo   lines ": "Ours: spans two lines.",
		"trailing dots .. ":   "Ours: trailing dots.",
		" ":                   reasonTermUnreadable,
		"\n\t":                reasonTermUnreadable,
	} {
		if got := reasonFor(Reading{Status: StatusNoRunRecord, Note: note}); got != want {
			t.Errorf("note %q served as %q, want %q", note, got, want)
		}
	}
}

// TestZeroOverOneUnreadPerson pins the zero-over-unread rule at a single unread person,
// with a hand-built ruleset whose people floor is 1: 0 counted of 3 seats with 1 unread
// widens to [0, 1/3], which straddles the emerging cut, so review L1 abstains on
// coverage, neither instrumented-only nor not reached.
func TestZeroOverOneUnreadPerson(t *testing.T) {
	rs := rules.ForVersion("v1")
	rs.PopulationFloors.People = 1
	rd := NewReadings()
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED, withPeople(resolved(3), 3))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_HARNESS_SEAT_HOLDERS, aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, withNumRemainder(withPeople(resolved(0), 0), 1))
	if got := vector(Score(rs, rd, OrgRead{}, nil))["review/L1"]; got != "abstain(coverage)" {
		t.Errorf("review/L1 = %s, want abstain(coverage)", got)
	}
}
