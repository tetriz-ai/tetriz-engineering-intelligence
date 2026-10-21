// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// TestAnUnknownMergeTimeScoresNoRecency is the guard against the worst shape of
// this bug: an unknown date awarding the MAXIMUM recency. The gap is computed by
// subtraction, so a zero merge time yields a large negative gap, and the clamp
// that exists for a candidate dated after its fix would pull that to zero and
// decay it to 1.0.
func TestAnUnknownMergeTimeScoresNoRecency(t *testing.T) {
	committed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cfg := defectlineage.DefaultConfig()

	assert.Zero(t, defectlineage.ScoreR(&committed, time.Time{}, cfg),
		"an unknown merge time means no gap to measure, so no recency")
	assert.Zero(t, defectlineage.ScoreR(nil, time.Now(), cfg),
		"an unknown commit date means the same")

	// A candidate committed after its fix merged cannot have introduced what the
	// fix repaired. Clamping the negative gap to zero days scored it 1.0 — the
	// strongest recency there is — for being impossible. An inverted ordering is
	// evidence against this culprit, so it scores no recency at all.
	later := committed.Add(24 * time.Hour)
	assert.Zero(t, defectlineage.ScoreR(&later, committed, cfg),
		"a candidate dated after its fix is not a very recent one")
}

// TestAHalfSetConfigIsRejected: leaving Config zero means "use the defaults",
// but setting one field and not the rest zeroes every weight, which scores every
// candidate 0.000 with nothing to show for it.
func TestAHalfSetConfigIsRejected(t *testing.T) {
	require.NoError(t, defectlineage.Config{}.Validate(), "the zero value means defaults")
	require.NoError(t, defectlineage.DefaultConfig().Validate())

	require.Error(t, defectlineage.Config{RDecayDays: 30}.Validate(),
		"tuning one knob must not silently zero the weights")

	skewed := defectlineage.DefaultConfig()
	skewed.WeightL = 0.9
	require.Error(t, skewed.Validate(), "weights that do not sum to 1 can push a score outside [0,1]")

	noDecay := defectlineage.DefaultConfig()
	noDecay.RDecayDays = 0
	require.Error(t, noDecay.Validate(), "a non-positive decay leaves every recency at 0")
}

// TestAScoreStaysInRange over the whole ladder: every component is in [0,1] and
// the default weights sum to 1, so the result cannot leave the range.
func TestAScoreStaysInRange(t *testing.T) {
	cfg := defectlineage.DefaultConfig()
	committed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	merged := committed.Add(400 * 24 * time.Hour)

	for _, ev := range []defectlineagev1.DefectLineageEvidenceType{
		defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_UNSPECIFIED,
		defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_DIRECT,
		defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_AFTER_HOPS,
		defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_PATH_RECONSTRUCTION,
		defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_GUARD_PROXIMITY,
	} {
		for _, lv := range []defectlineage.MatchLevel{
			defectlineage.MatchDifferentFile, defectlineage.MatchSameFile,
			defectlineage.MatchSameEnclosing, defectlineage.MatchExactNode,
		} {
			score, evidence := defectlineage.BuildScore(defectlineage.ScoreInputs{
				EvidenceType: ev, CommittedAt: &committed, MergedAt: merged,
				Level: lv, Corroboration: 1.0,
			}, cfg)
			assert.GreaterOrEqual(t, score, 0.0, "%v/%v", ev, lv)
			assert.LessOrEqual(t, score, 1.0, "%v/%v", ev, lv)
			require.NotNil(t, evidence.GetCorroborationFlags(),
				"the derivation always carries its flag breakdown")
			assert.GreaterOrEqual(t, evidence.GetRecencyDays(), 0.0,
				"a culprit predates its fix, so the stored gap is never negative")
		}
	}
}
