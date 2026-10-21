// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package airoi

import (
	"context"
	"fmt"
	"time"
)

// The four boundary interfaces an adopter implements. The engine (measure /
// classes / cascade / dollars) is pure; everything that touches data lives behind
// these. A production deployment backs them with its own data systems (SQL source,
// AI-attribution, quality reader, Postgres snapshots); an adopter supplies their own.

// SubjectCounts are the window-level cohort sizes.
type SubjectCounts struct {
	AIChangeCount    int
	TotalChangeCount int
}

// SourceReader supplies the per-(work-type, band, cohort) aggregates and the
// subject counts. The adopter owns the query, scoping, and PERCENTILE_CONT medians;
// `aiPRKeys` is the AI cohort membership that splits AI from baseline, and `bounds`
// are the complexity-band cutoffs to bin by.
type SourceReader interface {
	LoadBandStats(ctx context.Context, subjectID string, from, to time.Time, aiPRKeys []string, bounds BandBounds) ([]BandStat, error)
	LoadSubjectCounts(ctx context.Context, subjectID string, from, to time.Time, aiPRKeys []string) (SubjectCounts, error)
}

// AICohortProvider returns the AI-attributed PR keys for a subject/window (the
// product-wide L1∪L2 membership internally). Optional — pass keys directly if you
// resolve them elsewhere.
type AICohortProvider interface {
	AIAttributedPRKeys(ctx context.Context, subjectID string, from, to time.Time) ([]string, error)
}

// QualityScoreReader supplies the mean quality score (e.g. authored-session
// overallScore) per AI PR key — the signal the WITHIN_ORG_LEVERAGE rung bands on.
type QualityScoreReader interface {
	MeanQualityScoreByPRKey(ctx context.Context, subjectID string, aiPRKeys []string, from, to time.Time) (map[string]float64, error)
}

// Snapshot is one immutable dated measurement row.
type Snapshot struct {
	SubjectID   string
	AsOfDate    time.Time
	WindowDays  int32
	Basis       BaselineBasis
	Measurement *Measurement
}

// SnapshotStore persists measurements and reads the prior rung. LatestBefore is
// deliberately distinct from Latest so a recompute reads the day before the one it
// is overwriting (the cascade's prior-rung read).
type SnapshotStore interface {
	Latest(ctx context.Context, subjectID string) (*Snapshot, error)
	LatestBefore(ctx context.Context, subjectID string, asOf time.Time) (*Snapshot, error)
	Upsert(ctx context.Context, snap Snapshot) error
}

// NewFrame assembles a SubjectFrame from a reader's band stats + counts.
func NewFrame(bandStats []BandStat, counts SubjectCounts) *SubjectFrame {
	return &SubjectFrame{
		BandStats:        bandStats,
		AIChangeCount:    counts.AIChangeCount,
		TotalChangeCount: counts.TotalChangeCount,
	}
}

// ComputeMeasurement reads the FROM_YOUR_DATA frame and runs the methodology math.
// The simplest entrypoint: AI-vs-non-AI over the window, no cascade/leverage.
func ComputeMeasurement(ctx context.Context, reader SourceReader, subjectID string, from, to time.Time, aiPRKeys []string, cfg Config) (*Measurement, error) {
	bandStats, err := reader.LoadBandStats(ctx, subjectID, from, to, aiPRKeys, cfg.BandBounds)
	if err != nil {
		return nil, fmt.Errorf("loading band stats for %s: %w", subjectID, err)
	}
	counts, err := reader.LoadSubjectCounts(ctx, subjectID, from, to, aiPRKeys)
	if err != nil {
		return nil, fmt.Errorf("loading subject counts for %s: %w", subjectID, err)
	}
	return Measure(NewFrame(bandStats, counts), cfg), nil
}

// ComputeSubjectCascade runs the full basis cascade: it builds the FROM_YOUR_DATA
// frame, optionally builds the WITHIN_ORG_LEVERAGE frame (when a QualityScoreReader
// yields a usable high/low quality split), reads the prior rung, and applies the
// one-way rule. `quality` and `store` may be nil (no leverage rung / no prior).
func ComputeSubjectCascade(ctx context.Context, reader SourceReader, quality QualityScoreReader, store SnapshotStore, subjectID string, from, to time.Time, aiPRKeys []string, asOf time.Time, cfg Config) (CascadeResult, error) {
	fydStats, err := reader.LoadBandStats(ctx, subjectID, from, to, aiPRKeys, cfg.BandBounds)
	if err != nil {
		return CascadeResult{}, fmt.Errorf("loading band stats for %s: %w", subjectID, err)
	}
	counts, err := reader.LoadSubjectCounts(ctx, subjectID, from, to, aiPRKeys)
	if err != nil {
		return CascadeResult{}, fmt.Errorf("loading subject counts for %s: %w", subjectID, err)
	}
	fyd := NewFrame(fydStats, counts)

	leverage, err := buildLeverageFrame(ctx, reader, quality, subjectID, from, to, aiPRKeys, counts, cfg)
	if err != nil {
		return CascadeResult{}, err
	}

	var prior *PriorRung
	if store != nil {
		snap, err := store.LatestBefore(ctx, subjectID, asOf)
		if err != nil {
			return CascadeResult{}, fmt.Errorf("reading prior rung for %s: %w", subjectID, err)
		}
		if snap != nil {
			prior = &PriorRung{Basis: snap.Basis}
			if snap.Measurement != nil {
				prior.MixShift = snap.Measurement.MixShift
			}
		}
	}

	return Cascade(fyd, leverage, prior, cfg), nil
}

// buildLeverageFrame builds the WITHIN_ORG_LEVERAGE frame, or nil when there is no
// quality reader or no usable high/low split. It bands the AI PR keys by quality
// terciles, reads each band's AI cohort cells, and relabels high→AI / low→baseline.
func buildLeverageFrame(ctx context.Context, reader SourceReader, quality QualityScoreReader, subjectID string, from, to time.Time, aiPRKeys []string, counts SubjectCounts, cfg Config) (*SubjectFrame, error) {
	if quality == nil {
		return nil, nil
	}
	scores, err := quality.MeanQualityScoreByPRKey(ctx, subjectID, aiPRKeys, from, to)
	if err != nil {
		return nil, fmt.Errorf("loading quality scores for %s: %w", subjectID, err)
	}
	lowKeys, highKeys := BucketPRsByBand(scores)
	if len(lowKeys) == 0 || len(highKeys) == 0 {
		return nil, nil // no usable leverage split
	}
	highCells, err := aiCellsForKeys(ctx, reader, subjectID, from, to, highKeys, cfg)
	if err != nil {
		return nil, err
	}
	lowCells, err := aiCellsForKeys(ctx, reader, subjectID, from, to, lowKeys, cfg)
	if err != nil {
		return nil, err
	}
	return BuildLeverageFrame(highCells, lowCells, counts.AIChangeCount, counts.TotalChangeCount), nil
}

// aiCellsForKeys reads band stats scoped to a set of AI PR keys and keeps only the
// AI-cohort cells — the band's signal for the leverage comparison.
func aiCellsForKeys(ctx context.Context, reader SourceReader, subjectID string, from, to time.Time, keys []string, cfg Config) ([]BandStat, error) {
	stats, err := reader.LoadBandStats(ctx, subjectID, from, to, keys, cfg.BandBounds)
	if err != nil {
		return nil, fmt.Errorf("loading leverage band stats for %s: %w", subjectID, err)
	}
	out := make([]BandStat, 0, len(stats))
	for _, bs := range stats {
		if bs.Cohort == CohortAI {
			out = append(out, bs)
		}
	}
	return out, nil
}
