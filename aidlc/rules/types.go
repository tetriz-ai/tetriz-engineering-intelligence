// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package rules is the AIDLC maturity ruleset: the signals, the forty-five cell
// rules and the bottleneck rules the scorer in the parent aidlc package reads, plus
// the band cuts, the population floors, the window and one hash over all of it.
//
// The registry keys every signal on the aidlcv1.SignalKey enum.
package rules

import (
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// SignalStatus is a registered signal's readiness: whether a rule that reads it can
// produce a verdict, abstains on thin coverage, or is blocked on a gap.
type SignalStatus int

const (
	// SignalStatusUnspecified is the zero value; a registered signal never carries it.
	SignalStatusUnspecified SignalStatus = iota
	// SignalStatusLive means the signal reads at trusted coverage and a rule on it
	// can decide a cell.
	SignalStatusLive
	// SignalStatusAbstains means the signal exists but its coverage is too thin for a
	// verdict to stand, so a rule reading it abstains rather than fires.
	SignalStatusAbstains
	// SignalStatusBlocked means the signal a cell needs is not collected at all. No
	// registered signal carries it; it exists so a later ruleset can register one.
	SignalStatusBlocked
)

// Observes records what a signal can see, so a zero reading is read correctly.
type Observes int

const (
	// ObservesEverything is the default: a zero is a measured absence.
	ObservesEverything Observes = iota
	// ObservesInstrumentedOnly means a zero bounds only what was instrumented; the
	// chat-only form leaves no trace, so a zero never denies. With nothing unread the
	// cell abstains instrumented-only; with unread hits it abstains on coverage when the
	// widened share straddles a cut, and instrumented-only otherwise, after the floors. A
	// non-zero count on these signals is a measured share and can read not reached; only
	// a zero never denies.
	ObservesInstrumentedOnly
	// ObservesAutomatedOnly means a zero means nobody chose the artifact by hand
	// while installed configuration serving the stage fired, so the cell abstains.
	ObservesAutomatedOnly
)

// RemediationClass is which of six kinds of work unblocks a cell an instrumentation
// gap holds closed, from "the caller holds the data and passes no fact" to "no source
// records it at all".
type RemediationClass int

const (
	// RemediationClassUnspecified is the zero value.
	RemediationClassUnspecified RemediationClass = iota
	// RemediationClassCapturedAndIngested is class 1: the caller holds the data but
	// passes no fact to Fold for it.
	RemediationClassCapturedAndIngested
	// RemediationClassCapturedNotIngested is class 2: the source emits the data, but
	// the caller holds only counters, not the per-event records a fact needs.
	RemediationClassCapturedNotIngested
	// RemediationClassCapturedNotClassified is class 3: the caller holds the event but
	// not a classification of what it did.
	RemediationClassCapturedNotClassified
	// RemediationClassDerivableNotJoined is class 4: the caller holds both sides but
	// does not join them.
	RemediationClassDerivableNotJoined
	// RemediationClassComputableNotBuilt is class 5: the caller holds every input, but
	// nothing computes the fact from them.
	RemediationClassComputableNotBuilt
	// RemediationClassGenuinelyUncaptured is class 6: no source records the event, so
	// a new source is needed.
	RemediationClassGenuinelyUncaptured
)

// SignalDef is one registered signal's static binding: the fold that produces it,
// the population it is measured over, its grain, its coverage bound, its status and
// what it observes.
type SignalDef struct {
	// Key is the vocabulary member the registry keys on.
	Key aidlcv1.SignalKey
	// Name is the signal's snake_case name.
	Name string
	// Collector is the fold that produces the reading.
	Collector string
	// Population is what the signal is measured over, in plain words.
	Population string
	// Grain is what a share built on this signal (as a denominator) counts.
	Grain aidlcv1.ShareGrain
	// CoverageBound names the coverage measure that bounds the reading, empty where
	// the read is unbounded.
	CoverageBound string
	// Status is the signal's readiness.
	Status SignalStatus
	// Observes is what a zero reading can and cannot deny.
	Observes Observes
}

// CheckKind is one of the kinds of check a cell rule's ordered list carries.
type CheckKind int

const (
	// CheckKindUnspecified is the zero value.
	CheckKindUnspecified CheckKind = iota
	// CheckKindPrevalenceShare is a share of a signal over a population, banded on
	// the coverage interval. Exactly one per prevalence rule, and it sets the band.
	CheckKindPrevalenceShare
	// CheckKindPeopleFloor is the numerator's distinct-people floor: at least N
	// people did the thing, or the cell abstains rather than crediting one person's
	// automation.
	CheckKindPeopleFloor
	// CheckKindBaselineControl is a control on a stage's non-AI review rate: a review
	// on AI work evidences approval only where review is a live practice. An
	// unresolvable or zero control abstains; neither denies.
	CheckKindBaselineControl
	// CheckKindContext is a context-only reading attached to the cell (the
	// complexity reading on code L4): reported, never a gate.
	CheckKindContext
	// CheckKindSecondaryShare is a second share whose band composes with the
	// primary's as the lower of the two (review L5 and product spec L5: the enforced
	// practice's usage share and the declared review policy's approval share). Its
	// gate closing abstains the cell like the primary's does.
	CheckKindSecondaryShare
)

// Check is one entry in a cell rule's ordered check list. The scorer evaluates the
// checks in order and never interprets a cell by name.
type Check struct {
	// Kind is the check kind.
	Kind CheckKind
	// Signal is the signal this check reads: the numerator for a share, the control
	// term for a control, the validity signal for a context reading. Unset for a
	// people floor, which reads the share's own numerator people count.
	Signal aidlcv1.SignalKey
	// Denominator is the population a share divides by; set only on a share check.
	Denominator aidlcv1.SignalKey
	// PeopleMin is the minimum distinct people a people-floor check requires.
	PeopleMin int
}

// RuleKind is how a cell is decided. The scorer branches on it before reading any
// signal.
type RuleKind int

const (
	// RuleKindUnspecified is the zero value; a registered cell never carries it.
	RuleKindUnspecified RuleKind = iota
	// RuleKindPrevalence is a deterministic share rule with ordered checks (36
	// cells).
	RuleKindPrevalence
	// RuleKindImpliedFloor carries no rule of its own; downward closure awards it
	// when a higher level on the stage is met, else it abstains on its gap (2 cells:
	// code L2, review L2).
	RuleKindImpliedFloor
	// RuleKindJudgment is decided by a judgment with cited evidence, which this
	// scorer does not make, so it abstains as judgment (2 cells: plan L1, test and
	// QA L1).
	RuleKindJudgment
	// RuleKindNotMeasurable abstains on a named instrumentation gap (5 cells).
	RuleKindNotMeasurable
)

// CellKey is the (stage, level) coordinate the cell-rule map is keyed on.
type CellKey struct {
	Stage aidlcv1.AidlcStage
	Level aidlcv1.AidlcLevel
}

// CellRule is one of the forty-five cells' rule: its kind, its ordered checks (for
// a prevalence rule), and the instrumentation gap it abstains on when one holds it
// closed.
type CellRule struct {
	Stage aidlcv1.AidlcStage
	Level aidlcv1.AidlcLevel
	// Kind is how the cell is decided.
	Kind RuleKind
	// Checks are the ordered checks a prevalence rule evaluates; empty otherwise.
	Checks []Check
	// Gap is the instrumentation gap a not-measurable cell abstains on, or the gap
	// that blocks a prevalence rule whose gate the fold found closed. Unspecified
	// otherwise.
	Gap aidlcv1.InstrumentationGap
}

// BandCuts are the two prevalence cut points. Established is the definitional
// "most"; emerging is the uncalibrated "demonstrated but not established" floor.
type BandCuts struct {
	// Established is the share above which a band is established (0.50).
	Established float64
	// Emerging is the share above which a band is emerging (0.20).
	Emerging float64
	// EmergingUncalibrated records that the emerging cut has no cohort behind it,
	// so a cell it decides owes its reader the boundary disclosure.
	EmergingUncalibrated bool
}

// PopulationFloors are the smallest populations a share may be banded over, by
// grain. Below the floor a share is a small-sample artifact and the cell abstains:
// it neither awards nor denies.
type PopulationFloors struct {
	// People is the floor for a people-grain denominator (5).
	People int
	// Artifacts is the floor for a merged-change denominator (20).
	Artifacts int
}

// BottleneckKind is which of the grid derivations raised a bottleneck. The detector
// for each kind reads the scored grid (and, for two, an org-level read).
type BottleneckKind int

const (
	// BottleneckKindUnspecified is the zero value.
	BottleneckKindUnspecified BottleneckKind = iota
	// BottleneckKindReviewReach fires when recorded review reaches under half of a
	// large AI-authored code population. Id "review_reach", org subject.
	BottleneckKindReviewReach
	// BottleneckKindAdoptionReach fires when at least half the people who merged
	// work show no AI-authored merge. Id "adoption_reach", org subject.
	BottleneckKindAdoptionReach
	// BottleneckKindRungMissed fires for the first not-reached level on a stage that
	// has at least one met level. Id "rung_<stage>_<level>", org subject.
	BottleneckKindRungMissed
	// BottleneckKindLadderStalled fires for a stage read end to end with nothing met.
	// Id "ladder_<stage>", org subject, top two by miss count.
	BottleneckKindLadderStalled
	// BottleneckKindGapBlocks fires for the instrumentation gap blocking the most
	// cells. Id "gap_<gid>", product subject: not shown to the organisation; for
	// whoever runs the measurement.
	BottleneckKindGapBlocks
)

// BottleneckRule is one bottleneck derivation: its kind (the detector the scorer
// runs) and whether it produces a move. The detectors live in the aidlc package;
// this layer declares which are active and whether each carries a move.
type BottleneckRule struct {
	// Kind is the detector.
	Kind BottleneckKind
	// Subject is "org" or "product"; a product finding is not shown to the
	// organisation, and is for whoever runs the measurement.
	Subject string
	// HasMove is whether this kind produces a recommendation.
	HasMove bool
}

// RuleSet is one immutable version of the whole ruleset: the three layers, the band
// cuts, the population floors, the window, the precondition threshold, the gap
// remediation classes, and one hash over all of it. A released set is never edited;
// the hash-frozen test fails a change.
type RuleSet struct {
	// Version is the ruleset version this set answers for ("v1").
	Version string
	// WindowDays is the measurement window (90).
	WindowDays int
	// BandCuts and PopulationFloors are the scorer's numeric parameters.
	BandCuts         BandCuts
	PopulationFloors PopulationFloors
	// PathCoverageWithholdThreshold is the share of merged pull requests that must
	// carry file paths for a profile to be published: below it, five of nine stages
	// read paths that are not there, so the profile is withheld rather than served
	// misleading. It decides whether an organisation gets a map at all, so it is part
	// of the hash.
	PathCoverageWithholdThreshold float64
	// Signals is the signal layer, keyed on the vocabulary.
	Signals map[aidlcv1.SignalKey]SignalDef
	// Cells is the forty-five cell rules, keyed on (stage, level).
	Cells map[CellKey]CellRule
	// Bottlenecks are the active bottleneck derivations, in order.
	Bottlenecks []BottleneckRule
	// GapRemediation declares each instrumentation gap's remediation class.
	GapRemediation map[aidlcv1.InstrumentationGap]RemediationClass

	// hash is computed at package init over the canonical serialisation of every
	// field above; unexported so no caller can set it and the frozen test can trust
	// it is derived.
	hash string
}

// Hash is the digest over this ruleset's canonical serialisation. It is computed
// once at package init; a released set's hash never moves, so a change to a
// released set is a new version, not an edit. A RuleSet built outside this package
// has an empty hash.
func (rs *RuleSet) Hash() string { return rs.hash }
