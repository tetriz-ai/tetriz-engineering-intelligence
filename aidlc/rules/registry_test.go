// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package rules

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// v1FrozenHash is the pinned hash of the released v1 ruleset. It must never move: a
// change to any layer, band cut, floor, window or threshold moves it, and a moved
// hash on a released version is a version-bump bug, not an edit. To release a v2,
// add its set and pin its own hash; never re-pin this one.
const v1FrozenHash = "ab2ef21c68461214dcadf8d699cb95ad9cd56530b14c13980a321f931ccb1fa3"

func TestForVersion(t *testing.T) {
	if ForVersion("v1") == nil {
		t.Fatal("v1 ruleset is not registered")
	}
	if ForVersion("does-not-exist") != nil {
		t.Fatal("an unknown version must resolve to nil, never a fallback set")
	}
	assert.Equal(t, []string{"v1"}, Versions())
}

// TestHashFrozen holds the released set's hash to its pin. Editing a released set
// moves the hash; the fix is a new version, not a re-pin.
func TestHashFrozen(t *testing.T) {
	got := ForVersion("v1").Hash()
	if got != v1FrozenHash {
		t.Fatalf("v1 ruleset hash moved: got %s, pinned %s.\n"+
			"A released set is immutable: ship the change as a new version and pin its own hash.", got, v1FrozenHash)
	}
}

// TestHashCoversEveryParameter proves each scalar parameter is serialised into the
// hash: editing one in place must move the digest.
func TestHashCoversEveryParameter(t *testing.T) {
	base := ForVersion("v1")
	edits := map[string]func(rs *RuleSet){
		"window":           func(rs *RuleSet) { rs.WindowDays = 30 },
		"established cut":  func(rs *RuleSet) { rs.BandCuts.Established = 0.6 },
		"emerging cut":     func(rs *RuleSet) { rs.BandCuts.Emerging = 0.25 },
		"people floor":     func(rs *RuleSet) { rs.PopulationFloors.People = 6 },
		"artifact floor":   func(rs *RuleSet) { rs.PopulationFloors.Artifacts = 21 },
		"path withholding": func(rs *RuleSet) { rs.PathCoverageWithholdThreshold = 0.7 },
		"bottleneck move":  func(rs *RuleSet) { rs.Bottlenecks = append([]BottleneckRule(nil), rs.Bottlenecks[1:]...) },
	}
	for name, edit := range edits {
		altered := *base
		edit(&altered)
		assert.NotEqualf(t, base.Hash(), computeHash(&altered), "changing the %s did not move the hash", name)
	}
	assert.Equal(t, base.Hash(), computeHash(base), "the hash must be a pure function of the set")
}

func TestAllFortyFiveCells(t *testing.T) {
	rs := ForVersion("v1")
	if len(rs.Cells) != 45 {
		t.Fatalf("expected 45 cell rules, got %d", len(rs.Cells))
	}
	for _, stage := range allStages() {
		for _, level := range allLevels() {
			if _, ok := rs.Cells[CellKey{Stage: stage, Level: level}]; !ok {
				t.Errorf("missing cell rule for (%v, %v)", stage, level)
			}
		}
	}
}

// TestCellKindCounts holds the rule-kind split: 36 prevalence, 2 implied-floor, 2
// judgment, 5 not-measurable.
func TestCellKindCounts(t *testing.T) {
	rs := ForVersion("v1")
	counts := map[RuleKind]int{}
	for _, c := range rs.Cells {
		counts[c.Kind]++
	}
	want := map[RuleKind]int{
		RuleKindPrevalence:    36,
		RuleKindImpliedFloor:  2,
		RuleKindJudgment:      2,
		RuleKindNotMeasurable: 5,
	}
	assert.Equal(t, want, counts)
}

// TestEveryCheckNamesRegisteredSignal is the registry invariant: every check on
// every cell rule names a signal that is registered in the signal layer.
func TestEveryCheckNamesRegisteredSignal(t *testing.T) {
	rs := ForVersion("v1")
	for key, cell := range rs.Cells {
		for _, ch := range cell.Checks {
			if ch.Signal != aidlcv1.SignalKey_SIGNAL_KEY_UNSPECIFIED {
				if _, ok := rs.Signals[ch.Signal]; !ok {
					t.Errorf("cell %v check reads unregistered signal %v", key, ch.Signal)
				}
			}
			if ch.Kind == CheckKindPrevalenceShare || ch.Kind == CheckKindSecondaryShare {
				if _, ok := rs.Signals[ch.Denominator]; !ok {
					t.Errorf("cell %v share check reads unregistered denominator %v", key, ch.Denominator)
				}
			}
		}
	}
}

// TestEverySignalIsRegistered holds every SignalKey in the vocabulary to a
// registration, and every registration to its own key.
func TestEverySignalIsRegistered(t *testing.T) {
	rs := ForVersion("v1")
	for n := range aidlcv1.SignalKey_name {
		key := aidlcv1.SignalKey(n)
		if key == aidlcv1.SignalKey_SIGNAL_KEY_UNSPECIFIED {
			continue
		}
		d, ok := rs.Signals[key]
		if !ok {
			t.Errorf("signal %v is in the vocabulary but not registered", key)
			continue
		}
		assert.Equal(t, key, d.Key)
	}
	assert.Len(t, rs.Signals, len(aidlcv1.SignalKey_name)-1)
}

// TestEverySignalHasCollector holds every registered signal to naming the fold that
// produces it.
func TestEverySignalHasCollector(t *testing.T) {
	rs := ForVersion("v1")
	for key, d := range rs.Signals {
		switch d.Status {
		case SignalStatusLive, SignalStatusAbstains:
			if strings.TrimSpace(d.Collector) == "" {
				t.Errorf("signal %v (%s) is %v but names no collector", key, d.Name, d.Status)
			}
		default:
			t.Errorf("signal %v (%s) carries status %v", key, d.Name, d.Status)
		}
	}
}

// TestNoAmbientSignalReachesRung holds that no signal read from installed
// configuration (a hook, a rules file) may decide a level: such usage says what the
// organisation installed, not how its people worked.
func TestNoAmbientSignalReachesRung(t *testing.T) {
	rs := ForVersion("v1")
	for key, cell := range rs.Cells {
		for _, ch := range cell.Checks {
			if ch.Signal == aidlcv1.SignalKey_SIGNAL_KEY_UNSPECIFIED {
				continue
			}
			if name := rs.Signals[ch.Signal].Name; strings.HasPrefix(name, "ambient_") {
				t.Errorf("cell %v reads ambient signal %s", key, name)
			}
		}
	}
}

// TestGapRemediationCoversNamedGaps holds every gap a cell names to a declared
// remediation class.
func TestGapRemediationCoversNamedGaps(t *testing.T) {
	rs := ForVersion("v1")
	for key, cell := range rs.Cells {
		if cell.Gap != aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_UNSPECIFIED {
			if _, ok := rs.GapRemediation[cell.Gap]; !ok {
				t.Errorf("cell %v names gap %v with no declared remediation class", key, cell.Gap)
			}
		}
	}
}

// TestDeclarationEffects pins which cells each declaration opens.
func TestDeclarationEffects(t *testing.T) {
	L := func(s aidlcv1.AidlcStage, l aidlcv1.AidlcLevel) CellKey { return CellKey{Stage: s, Level: l} }
	got := DeclarationEffects(ForVersion("v1"))
	assert.Equal(t, []CellKey{L(aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA, aidlcv1.AidlcLevel_AIDLC_LEVEL_L4)}, got[DeclarationTestCommands])
	assert.Equal(t, []CellKey{L(aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD, aidlcv1.AidlcLevel_AIDLC_LEVEL_L4)}, got[DeclarationCiCommands])
	assert.Equal(t, []CellKey{
		L(aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC, aidlcv1.AidlcLevel_AIDLC_LEVEL_L5),
		L(aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW, aidlcv1.AidlcLevel_AIDLC_LEVEL_L5),
	}, got[DeclarationReviewPolicy])
	assert.True(t, IsDeclarationSignal(DeclarationReviewPolicy, aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS))
	assert.False(t, IsDeclarationSignal(DeclarationReviewPolicy, aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_CI_COMMAND_SESSIONS))
	assert.False(t, IsDeclarationSignal("not_a_field", aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS))
}

// TestBandCutsAndFloors pins the numeric parameters the scorer and the copy read.
func TestBandCutsAndFloors(t *testing.T) {
	rs := ForVersion("v1")
	require.NotNil(t, rs)
	assert.Equal(t, BandCuts{Established: 0.50, Emerging: 0.20, EmergingUncalibrated: true}, rs.BandCuts)
	assert.Equal(t, PopulationFloors{People: 5, Artifacts: 20}, rs.PopulationFloors)
	assert.Equal(t, 90, rs.WindowDays)
	assert.Equal(t, 0.80, rs.PathCoverageWithholdThreshold)
}

func allStages() []aidlcv1.AidlcStage {
	return []aidlcv1.AidlcStage{
		aidlcv1.AidlcStage_AIDLC_STAGE_PRODUCT_SPEC,
		aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC,
		aidlcv1.AidlcStage_AIDLC_STAGE_PLAN,
		aidlcv1.AidlcStage_AIDLC_STAGE_CODE,
		aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW,
		aidlcv1.AidlcStage_AIDLC_STAGE_TEST_QA,
		aidlcv1.AidlcStage_AIDLC_STAGE_CI_CD,
		aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY,
		aidlcv1.AidlcStage_AIDLC_STAGE_OPERATE,
	}
}

func allLevels() []aidlcv1.AidlcLevel {
	return []aidlcv1.AidlcLevel{
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L1,
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L2,
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L3,
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L4,
		aidlcv1.AidlcLevel_AIDLC_LEVEL_L5,
	}
}
