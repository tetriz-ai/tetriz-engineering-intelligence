// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package aidlc scores an organisation's AI development lifecycle maturity: a grid of
// nine lifecycle stages by five autonomy levels. Score takes the ruleset (package
// rules) and one organisation's Readings, evaluates each cell's checks in order,
// closes each stage downward, derives the frontier, the summary and the bottlenecks,
// and assembles an aidlcv1.MaturityProfile. Every cell is met with a band, not
// reached, or abstains with a stated reason.
//
// Package fold builds Readings from raw facts. CheckPreconditions decides whether a
// profile may be published. DeriveMovement reads movement across stored days.
package aidlc

import (
	"math"
	"sort"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// ResolveStatus is whether a signal produced a reading for this organisation.
type ResolveStatus int

const (
	// StatusResolved means the signal produced a value.
	StatusResolved ResolveStatus = iota
	// StatusNoRunRecord means nothing produced the signal at all. Abstains as a gap
	// (ABSTAIN_KIND_GAP).
	StatusNoRunRecord
	// StatusRowAbsent means the signal was read but produced no value for this
	// organisation. Abstains as row-absent.
	StatusRowAbsent
	// StatusGapBlocked means the signal was read and the cell's gate is closed by the
	// instrumentation gap the rule names: the organisation enforces no practice for
	// the stage (G4), has no tracker issues in the window (G2), or has declared nothing
	// the rule could read. The cell abstains on that gap with the gap's own reader-facing
	// sentence, never on "a number could not be read".
	StatusGapBlocked
)

// Coverage is how much of the population a reading saw, set by whoever builds the
// reading from the signal's bound (no bound: full; a source that declares a subset:
// declared partial; a bound that could not be read: unknown; zero: none; 0.99 or
// more: full; else partial). The scorer reads it; it does not recompute it.
type Coverage int

const (
	// CoverageFull means the whole population was seen.
	CoverageFull Coverage = iota
	// CoveragePartial means only part was seen; the reading is a lower bound and the
	// interval widens by the unseen share.
	CoveragePartial
	// CoverageDeclaredPartial means the source declares it reports a subset; the
	// interval widens by the whole unmeasured remainder.
	CoverageDeclaredPartial
	// CoverageNone means the reading stands for nothing.
	CoverageNone
	// CoverageUnknown means coverage could not be established; treated as weaker than
	// partial, so the interval widens by the whole remainder.
	CoverageUnknown
)

// Reading is one signal's runtime resolution for an organisation over the window:
// the value, the distinct-people count behind it, its coverage, and the two kinds of
// unread mass (a declared remainder within the population, and a coverage bound).
// What a share built on this reading counts (its grain) and what a zero can deny
// (its observes) come from the ruleset's SignalDef, not from here: the caller
// produces the numbers, the registry classifies the signal.
type Reading struct {
	// Status is whether the signal resolved.
	Status ResolveStatus
	// Value is the reading. Nil when unresolved. Every signal a share counts (a
	// numerator or a population) is a whole count; a fractional value there resolves as
	// row-absent. The two rates, the non-AI baseline review rate and the complexity
	// score, may be fractional.
	Value *float64
	// People is the distinct people behind the value, nil when the signal declares no
	// people column. A people-floor check on a nil people count abstains (the gate
	// does not silently vanish).
	People *int
	// Coverage is the reading's coverage band.
	Coverage Coverage
	// RemainderDeclared is set when the signal declares a remainder bound; a declared
	// remainder whose value is nil is a stale run and abstains.
	RemainderDeclared bool
	// RemainderValue is the count of population members the deciding term could not be
	// read over (the numerator's) or the population could not be enumerated (the
	// denominator's). Nil when declared-but-absent.
	RemainderValue *int
	// BoundValue is the coverage bound as a fraction (routed source coverage, path
	// coverage), used to widen the numerator's unread mass under partial coverage. Nil
	// for a declared-partial or unbounded reading.
	BoundValue *float64
	// Note is the plain-language reason a reading is unresolved. On a no-run reading
	// (StatusNoRunRecord) the scorer serves it as the cell's reason, "Ours: <note>.", as
	// one clause: internal whitespace is collapsed, trailing whitespace and periods are
	// trimmed, a note of only whitespace or periods gets the generic reason, and the length
	// is not capped. Write it as one plain lower-case clause with no other trailing
	// punctuation. On any other status it stays on the reading for the caller.
	Note string

	// malformed is set by resolve on a reading whose numbers cannot describe what it
	// claims (see invalidReading, resolveCount): such a reading abstains with a kind
	// downward closure never awards.
	malformed bool
}

func f64(v float64) *float64 { return &v }

// readingKey addresses a reading by signal and stage. Most signals are stage-unique,
// but the baseline control varies by stage and the seat roster is org-level, so the
// scorer resolves (signal, cell stage) and falls back to the org-level (signal,
// UNSPECIFIED) entry.
type readingKey struct {
	Signal aidlcv1.SignalKey
	Stage  aidlcv1.AidlcStage
}

// PeopleSet is a set of distinct person ids, handed to the scorer where a disclosure
// needs the membership, not just a count: breadth intersects a stage's presence
// people with OrgRead.ActiveEngineers to bound its numerator at the denominator.
type PeopleSet map[string]struct{}

// Len is the set's distinct-person count.
func (s PeopleSet) Len() int { return len(s) }

// intersectCount is how many of s's members are also in other — breadth's numerator.
func (s PeopleSet) intersectCount(other PeopleSet) int {
	n := 0
	for p := range s {
		if _, ok := other[p]; ok {
			n++
		}
	}
	return n
}

// Readings is one organisation's readings, keyed by (signal, stage). fold.Fold fills
// it from raw facts, or a caller sets each reading directly.
type Readings struct {
	m map[readingKey]Reading
	// stagePeople carries the presence fold's distinct people set per stage — the raw
	// material breadth intersects against OrgRead.ActiveEngineers. Only the presence
	// fold's stages ever carry an entry; every other stage's breadth stays
	// UNSPECIFIED (absent when the stage's people set is empty).
	stagePeople map[aidlcv1.AidlcStage]PeopleSet
}

// NewReadings returns an empty reading set.
func NewReadings() *Readings { return &Readings{m: map[readingKey]Reading{}} }

// SetStagePeople records the presence fold's distinct people set for a stage.
func (r *Readings) SetStagePeople(stage aidlcv1.AidlcStage, people PeopleSet) *Readings {
	if r.stagePeople == nil {
		r.stagePeople = map[aidlcv1.AidlcStage]PeopleSet{}
	}
	r.stagePeople[stage] = people
	return r
}

// StagePeople returns the stage's recorded people set, nil when none was set.
func (r *Readings) StagePeople(stage aidlcv1.AidlcStage) PeopleSet {
	return r.stagePeople[stage]
}

// Set records a reading for a signal at a stage. Use the UNSPECIFIED stage for an
// org-level signal (the seat roster).
func (r *Readings) Set(signal aidlcv1.SignalKey, stage aidlcv1.AidlcStage, rd Reading) *Readings {
	r.m[readingKey{Signal: signal, Stage: stage}] = rd
	return r
}

// Entry pairs one stored reading with the (signal, stage) key it was Set under: the
// shape a caller needs to serialise the whole set, since the scorer's fallback to the
// org-level entry is a scoring concern an export must not apply.
type Entry struct {
	Signal  aidlcv1.SignalKey
	Stage   aidlcv1.AidlcStage
	Reading Reading
}

// All returns every stored reading as (signal, stage, reading) triples, ordered by
// signal then stage for a deterministic export. It reflects exactly what Set wrote —
// no org-level fallback — since a caller serialising the raw collected set must not
// have a stage's absence silently filled from the org-level entry.
func (r *Readings) All() []Entry {
	out := make([]Entry, 0, len(r.m))
	for k, v := range r.m {
		out = append(out, Entry{Signal: k.Signal, Stage: k.Stage, Reading: v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Signal != out[j].Signal {
			return out[i].Signal < out[j].Signal
		}
		return out[i].Stage < out[j].Stage
	})
	return out
}

// resolve returns the reading for a signal at a stage, falling back to the org-level
// entry, then to a no-run-record reading so an unnamed signal reads as an unmeasured
// gap rather than a zero. A reading whose numbers cannot describe a count (see
// invalidReading) resolves as row-absent, so the cell abstains rather than scoring it.
func (r *Readings) resolve(signal aidlcv1.SignalKey, stage aidlcv1.AidlcStage) Reading {
	if rd, ok := r.m[readingKey{Signal: signal, Stage: stage}]; ok {
		return checked(rd)
	}
	if rd, ok := r.m[readingKey{Signal: signal, Stage: aidlcv1.AidlcStage_AIDLC_STAGE_UNSPECIFIED}]; ok {
		return checked(rd)
	}
	return Reading{Status: StatusNoRunRecord}
}

// maxExactCount is the largest count a float64 holds exactly (2^53).
const maxExactCount = 1 << 53

// resolveCount resolves a share's count term (a numerator or population): on top of
// resolve's checks, a value that is not a whole number, or is beyond 2^53 where a
// float64 stops holding every whole number, cannot be a count, so it resolves as
// malformed too.
func (r *Readings) resolveCount(signal aidlcv1.SignalKey, stage aidlcv1.AidlcStage) Reading {
	rd := r.resolve(signal, stage)
	if rd.Status == StatusResolved && rd.Value != nil {
		switch {
		case *rd.Value != math.Trunc(*rd.Value):
			return Reading{Status: StatusRowAbsent, Note: "a count is not a whole number", malformed: true}
		case *rd.Value > maxExactCount:
			return Reading{Status: StatusRowAbsent, Note: "a count is beyond 2^53", malformed: true}
		}
	}
	return rd
}

// checked passes a valid reading through and turns an invalid one into row-absent,
// keeping the reason in Note.
func checked(rd Reading) Reading {
	if note := invalidReading(rd); note != "" {
		return Reading{Status: StatusRowAbsent, Note: note, malformed: true}
	}
	return rd
}

// invalidReading names what makes a reading's numbers meaningless, or returns "": a
// value that is NaN, infinite or negative, a negative people count or remainder, or a
// coverage bound outside [0, 1].
func invalidReading(rd Reading) string {
	switch {
	case rd.Value != nil && (math.IsNaN(*rd.Value) || math.IsInf(*rd.Value, 0)):
		return "the value is not a finite number"
	case rd.Value != nil && *rd.Value < 0:
		return "the value is negative"
	case rd.People != nil && *rd.People < 0:
		return "the people count is negative"
	case rd.People != nil && *rd.People > maxExactCount:
		return "the people count is beyond 2^53"
	case rd.RemainderValue != nil && *rd.RemainderValue < 0:
		return "the remainder is negative"
	case rd.RemainderValue != nil && *rd.RemainderValue > maxExactCount:
		return "the remainder is beyond 2^53"
	case rd.BoundValue != nil && !(*rd.BoundValue >= 0 && *rd.BoundValue <= 1):
		return "the coverage bound is not between 0 and 1"
	default:
		return ""
	}
}
