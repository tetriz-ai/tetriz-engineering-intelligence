// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

// Params are the tunable thresholds of the matching cascade.
//
// DefaultParams returns the values this engine was calibrated on. They are
// defaults, not universal constants: each was picked against one dataset, and
// an adopter whose repositories, review latency or work mix differ should
// expect to retune. Every field states what moves when it does.
type Params struct {
	// TimeWindowAfterDays is how many days a pull request may merge AFTER a
	// session and still be considered by the file-overlap signal. Kept tight
	// because file overlap is the weakest signal: a pull request that drifted
	// long after the session is likely unrelated work that happens to share
	// scaffolding files. Raising it widens the false-positive surface.
	TimeWindowAfterDays int

	// StrongSignalWindowAfterDays is the same bound for the strong signals,
	// direct reference and branch match. Safe to widen: a session on branch X
	// and a pull request merged from branch X are the same work even when review
	// is slow, because the branch name itself is the signal. Defaults to 30,
	// like every unset field (see DefaultParams).
	StrongSignalWindowAfterDays int

	// MinConfidencePercent is the floor for a file-overlap score, as a
	// percentage converted to [0.0, 1.0] at use. The score is the share of the
	// session's edited files that the pull request changed, time-decayed, and a
	// score at or above the floor matches. Higher values cut false positives but
	// drop sessions whose edits spread over several pull requests.
	MinConfidencePercent int

	// RetryCooldownHours is the minimum gap between re-attribution attempts for
	// an unmatched session, so a run does not re-evaluate the same sessions
	// every tick. Applied by IsPending, which both reference stores call.
	// Lowering it raises read load without much benefit: pull requests rarely
	// appear between runs.
	RetryCooldownHours int

	// MaxRetryDays is how long after a session's last observed activity an
	// unmatched session is retried when the reason is "no matching pull
	// request". After it, the session is permanently unmatched unless a
	// re-attribution request reopens it. "Not linked" and "identity error"
	// abstentions retry on the cooldown indefinitely. Applied by IsPending.
	MaxRetryDays int

	// MaxSessionSpanDays caps how far a session's last-activity anchor may sit
	// past its start date, bounding every window measured from that anchor. Not
	// cosmetic: each extra day widens the candidate fetch for the whole person
	// and admits more chances for a reused branch name to link a session to
	// unrelated work, so a session left open for months must not grow an
	// unbounded matching window.
	MaxSessionSpanDays int

	// IdentityResolveConcurrency caps concurrent identity lookups during the
	// resolution phase, keeping fan-out below a resolver's rate budget while
	// still overlapping latency across the batch.
	IdentityResolveConcurrency int

	// ComputeMatchConcurrency caps concurrent in-memory match computations.
	// The signals are pure CPU over already-fetched candidates, so this governs
	// CPU headroom rather than IO.
	ComputeMatchConcurrency int

	// Observer receives diagnostic counters and explanatory events. Nil discards
	// them. It never influences a matching decision.
	Observer Observer
}

// DefaultParams returns the calibrated defaults.
//
// The strong-signal window and the retry budget default to 30 days.
func DefaultParams() Params {
	return Params{
		TimeWindowAfterDays:         3,
		StrongSignalWindowAfterDays: 30,
		MinConfidencePercent:        30,
		RetryCooldownHours:          12,
		MaxRetryDays:                30,
		MaxSessionSpanDays:          30,
		IdentityResolveConcurrency:  10,
		ComputeMatchConcurrency:     10,
	}
}

// withDefaults fills every zero or negative field from DefaultParams and keeps
// every positive field the caller set, so a caller can tune one threshold without restating the rest.
// A zero or negative value therefore always means "the default". A caller who
// wants no strong-signal widening sets StrongSignalWindowAfterDays equal to
// TimeWindowAfterDays.
func (p Params) withDefaults() Params {
	d := DefaultParams()
	fill := func(v *int, def int) {
		if *v == 0 {
			*v = def
		}
	}
	fill(&p.TimeWindowAfterDays, d.TimeWindowAfterDays)
	fill(&p.StrongSignalWindowAfterDays, d.StrongSignalWindowAfterDays)
	fill(&p.MinConfidencePercent, d.MinConfidencePercent)
	fill(&p.RetryCooldownHours, d.RetryCooldownHours)
	fill(&p.MaxRetryDays, d.MaxRetryDays)
	fill(&p.MaxSessionSpanDays, d.MaxSessionSpanDays)
	fill(&p.IdentityResolveConcurrency, d.IdentityResolveConcurrency)
	fill(&p.ComputeMatchConcurrency, d.ComputeMatchConcurrency)
	return p
}

// defaultMaxSessionSpanDays bounds the anchor when MaxSessionSpanDays is unset,
// mirroring effectiveStrongSignalWindowDays' fallback. A zero bound would clamp
// every anchor back onto the start date, which would make an unset value behave
// as a silent kill switch for the anchor rather than as "no configured bound".
const defaultMaxSessionSpanDays = 30

// effectiveMaxSessionSpanDays returns MaxSessionSpanDays when configured,
// falling back to defaultMaxSessionSpanDays.
func effectiveMaxSessionSpanDays(p Params) int {
	if p.MaxSessionSpanDays > 0 {
		return p.MaxSessionSpanDays
	}
	return defaultMaxSessionSpanDays
}
