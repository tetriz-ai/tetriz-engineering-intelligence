// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage

import (
	"fmt"
	"math"
	"time"

	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// Scoring ranks a fix's stored candidates by how likely each is to have
// introduced the defect. It is five component values, each in [0,1], and their
// weighted sum.
//
// Every function here is pure over already-stored candidate data plus a [Config],
// so recalibrating a value is a re-read, never a re-blame or a re-fetch. That
// separation is the point: blame is the expensive half you pay for once, and the
// formula is the half that changes repeatedly.

// Config is the calibration surface for the formula. The values are calibration
// outputs rather than preferences, so they live here instead of as literals in
// the scorers.
type Config struct {
	// Component weights of confidence = WeightL·L + WeightC·C + WeightD·D +
	// WeightR·R + WeightE·E.
	WeightL float64
	WeightC float64
	WeightD float64
	WeightR float64
	WeightE float64

	// The L directness ladder: blame evidence type to strength in [0,1]. Direct
	// blame is the strongest lineage and each weaker mechanism steps down.
	// Anchor share is deliberately not a factor — the count is not stored and is
	// unrecoverable without re-blaming.
	LadderDirect             float64
	LadderAfterHops          float64
	LadderPathReconstruction float64
	LadderGuardProximity     float64

	// The C proximity ladder: the candidate-vs-fix match level to strength in
	// [0,1]. The endpoints are fixed in [ScoreC] — an exact-node match is 1.0, a
	// different-file match 0.0 — so only the two intermediate rungs are
	// calibrated here.
	LadderSameFile      float64
	LadderSameEnclosing float64

	// DStatic is the deployment-compatibility factor: a fixed value every
	// candidate receives, so D contributes an equal constant and does not
	// discriminate between a fix's candidates. It is fixed because no deployment
	// evidence is read — a candidate is proven to be in the fix's base history
	// but not proven deployed.
	DStatic float64

	// RDecayDays is the decay time-constant, in days, of the recency factor
	// exp(-days / RDecayDays): at RDecayDays the factor is e⁻¹, about 0.37, not
	// one half — so it is a time-constant, not a half-life, which would be
	// RDecayDays·ln2. Named for the exponential it actually parameterises.
	RDecayDays float64

	// ECandidateWeight splits corroboration between the candidate-specific
	// bucket (this weight) and the defect-level bucket (1 minus this).
	// Candidate-specific signals point at this candidate and dominate;
	// defect-level signals only confirm a real defect and are identical across a
	// fix's candidates, so they cannot rank.
	ECandidateWeight float64
}

// DefaultConfig is a starting point to tune, not a claim of correctness. The
// intermediate ladder rungs, the recency decay constant and the E split are the
// values a calibration against labelled data is expected to move; the endpoints,
// direct blame at 1.0 and D at 0.60, are the reference anchors.
func DefaultConfig() Config {
	return Config{
		WeightL: 0.35,
		WeightC: 0.20,
		WeightD: 0.20,
		WeightR: 0.15,
		WeightE: 0.10,

		LadderDirect:             1.0,
		LadderAfterHops:          0.70,
		LadderPathReconstruction: 0.50,
		LadderGuardProximity:     0.25,

		LadderSameFile:      0.30,
		LadderSameEnclosing: 0.50,

		DStatic:    0.60,
		RDecayDays: 90,

		ECandidateWeight: 0.70,
	}
}

// ScoreL is lineage strength: how directly blame reached the candidate, keyed on
// the stored evidence type alone. It returns 0 for the unspecified marker and,
// as a deliberate safe zero, for any evidence type without a ladder rung — an
// unrecognised mechanism scores no strength rather than a wrong one.
func ScoreL(evidenceType defectlineagev1.DefectLineageEvidenceType, cfg Config) float64 {
	switch evidenceType {
	case defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_DIRECT:
		return cfg.LadderDirect
	case defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_AFTER_HOPS:
		return cfg.LadderAfterHops
	case defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_PATH_RECONSTRUCTION:
		return cfg.LadderPathReconstruction
	case defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_GUARD_PROXIMITY:
		return cfg.LadderGuardProximity
	default:
		return 0
	}
}

// ScoreC is code-context proximity: the candidate-vs-fix match level mapped to a
// value in [0,1]. The level is the structural fact a [Comparator] reports; this
// maps it through [Config] so the intermediate rungs stay calibratable without
// re-running the comparison. The endpoints are fixed — an exact-node match is
// the strongest context and a different-file match is none — and, as a
// deliberate safe zero mirroring [ScoreL], any unrecognised level scores 0
// rather than a wrong value.
func ScoreC(level MatchLevel, cfg Config) float64 {
	switch level {
	case MatchExactNode:
		return 1.0
	case MatchSameEnclosing:
		return cfg.LadderSameEnclosing
	case MatchSameFile:
		return cfg.LadderSameFile
	case MatchDifferentFile:
		return 0.0
	default:
		return 0.0
	}
}

// ScoreD is deployment compatibility — the static factor from [Config]; see
// [Config.DStatic] for why it is a constant and does not discriminate between
// candidates.
func ScoreD(cfg Config) float64 {
	return cfg.DStatic
}

// ScoreR is the recency fit exp(-days / RDecayDays) over the gap from the
// candidate's commit to the fix's merge. A same-time candidate scores about 1.0
// and older candidates decay.
//
// The candidate's commit date can be absent. With no gap to measure there is no
// recency signal, so it scores 0 rather than a fabricated 1.0. A candidate dated
// after the fix merged is anomalous, since a culprit predates its fix; its gap
// clamps to 0 so it scores at most 1.0.
func ScoreR(candidateCommittedAt *time.Time, fixMergedAt time.Time, cfg Config) float64 {
	// Either end of the gap missing means there is no gap. Without the zero-merge
	// arm the subtraction yields a large negative gap, the anomaly clamp below
	// pulls it to zero, and the decay returns 1.0 — the maximum, awarded for an
	// unknown date.
	if candidateCommittedAt == nil || fixMergedAt.IsZero() || cfg.RDecayDays <= 0 {
		return 0
	}
	days := fixMergedAt.Sub(*candidateCommittedAt).Hours() / 24
	if days < 0 {
		// The candidate was committed after the fix merged, so it cannot have
		// introduced what the fix repaired. Clamping to zero days would score it
		// 1.0 — the strongest recency there is — for being impossible. Score it
		// 0 instead: an inverted ordering is evidence against this culprit, not
		// evidence of a very recent one.
		return 0
	}
	return math.Exp(-days / cfg.RDecayDays)
}

// Combine is the confidence score: the weighted sum of the five component
// values, each already in [0,1]. With the default weights summing to 1.0 and
// every input bounded, the result is in [0,1]. The caller computes each
// component and passes them here, so the combiner stays a pure weighting with no
// knowledge of how a component was derived.
func Combine(l, c, d, r, e float64, cfg Config) float64 {
	return cfg.WeightL*l + cfg.WeightC*c + cfg.WeightD*d + cfg.WeightR*r + cfg.WeightE*e
}

// ScoreInputs is everything one candidate's confidence score is computed from:
// what blame stored about the candidate, when the fix landed, and what the
// structural comparison and the corroborating signals found.
type ScoreInputs struct {
	// EvidenceType is how blame reached the candidate, which sets L.
	EvidenceType defectlineagev1.DefectLineageEvidenceType
	// CommittedAt is the culprit's commit date. Nil leaves R at 0, because
	// there is no gap to measure.
	CommittedAt *time.Time
	// MergedAt is when the fix landed, the other end of the recency gap.
	MergedAt time.Time
	// Level is the structural proximity rung, which sets C.
	Level MatchLevel
	// Degraded records that Level is file coverage rather than a measured
	// proximity, so a reader never mistakes the two.
	Degraded bool
	// NodeType and EnclosingType explain the rung. Empty when the comparison
	// degraded.
	NodeType      string
	EnclosingType string
	// Corroboration is E, and Flags the per-signal breakdown behind it.
	Corroboration float64
	Flags         CorroborationFlags
}

// BuildScore computes a candidate's confidence and the derivation behind it.
//
// It is the single implementation, because the score is reached from two
// directions — this package's own scoring pass, and a provider adapter
// submitting a structural verdict — and a formula with two implementations is a
// formula that disagrees with itself the first time a weight changes.
func BuildScore(in ScoreInputs, cfg Config) (float64, *defectlineagev1.DefectScoreEvidence) {
	l := ScoreL(in.EvidenceType, cfg)
	c := ScoreC(in.Level, cfg)
	d := ScoreD(cfg)
	r := ScoreR(in.CommittedAt, in.MergedAt, cfg)

	evidence := &defectlineagev1.DefectScoreEvidence{
		LineageStrength:    l,
		CodeContext:        c,
		Deployment:         d,
		Recency:            r,
		Corroboration:      in.Corroboration,
		AstMatchLevel:      in.Level.String(),
		AstNodeType:        in.NodeType,
		AstEnclosingNode:   in.EnclosingType,
		DirectnessTier:     in.EvidenceType,
		RecencyDays:        recencyDays(in.CommittedAt, in.MergedAt),
		Degraded:           in.Degraded,
		CorroborationFlags: in.Flags.Proto(),
	}
	return Combine(l, c, d, r, in.Corroboration, cfg), evidence
}

// recencyDays is the gap R was computed over, in days, clamped at 0. A candidate
// dated after the fix merged is anomalous — a culprit predates its fix — and the
// clamp keeps the stored derivation from reporting a negative age.
func recencyDays(committedAt *time.Time, mergedAt time.Time) float64 {
	if committedAt == nil || mergedAt.IsZero() {
		return 0
	}
	days := mergedAt.Sub(*committedAt).Hours() / 24
	if days < 0 {
		return 0
	}
	return days
}

// Validate reports whether a Config can produce a usable score.
//
// It exists because a partially-set Config is silently catastrophic: a caller
// who tunes one knob leaves every weight and every ladder rung at zero, and the
// formula then returns 0.000 for every candidate with no error to show for it.
// The zero value itself is fine — a constructor reads it as "use the defaults" —
// so this rejects only the half-filled case.
func (c Config) Validate() error {
	if c == (Config{}) {
		return nil
	}
	sum := c.WeightL + c.WeightC + c.WeightD + c.WeightR + c.WeightE
	const epsilon = 1e-6
	if sum < epsilon {
		return fmt.Errorf("defectlineage: Config sets no weights, so every score would be 0 — leave it zero to use DefaultConfig, or set all five")
	}
	if math.Abs(sum-1) > epsilon {
		return fmt.Errorf("defectlineage: Config weights sum to %g, not 1, so a score can fall outside [0,1]", sum)
	}
	if c.RDecayDays <= 0 {
		return fmt.Errorf("defectlineage: Config.RDecayDays is %g; a non-positive decay constant leaves every recency at 0", c.RDecayDays)
	}
	ladder := c.LadderDirect + c.LadderAfterHops + c.LadderPathReconstruction +
		c.LadderGuardProximity + c.LadderSameFile + c.LadderSameEnclosing
	if ladder < epsilon {
		// Weights set but every rung left at zero is the other half of the same
		// mistake: L is 0 for every candidate however it was reached, so the
		// strongest and the weakest evidence score alike and the term the
		// formula leans on hardest contributes nothing. Silent, because the
		// weights still sum to 1.
		return fmt.Errorf("defectlineage: Config sets weights but leaves every ladder rung at 0, so evidence strength cannot affect a score — set the Ladder fields, or leave the whole Config zero to use DefaultConfig")
	}
	return nil
}
