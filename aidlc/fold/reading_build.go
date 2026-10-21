// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
)

// coverageFor maps a measured coverage fraction to the aidlc.Coverage band: a zero is
// none, a fraction at or above 0.99 is full (rounding noise is not partiality), and
// anything between is partial. A fold that has no coverage bound at all passes
// aidlc.CoverageFull directly and never calls this.
func coverageFor(fraction float64) aidlc.Coverage {
	switch {
	case fraction <= 0:
		return aidlc.CoverageNone
	case fraction >= 0.99:
		return aidlc.CoverageFull
	default:
		return aidlc.CoveragePartial
	}
}

// numeratorReading builds a resolved numerator reading. value is the count of
// matching items; people is the distinct people behind it (nil for a numerator whose
// cell names no people floor). boundFraction, when non-nil, is the coverage measure
// that bounds the read (path coverage, captured share): it sets the coverage band and
// is carried as BoundValue so the scorer widens the numerator's unread mass by the
// unseen share. A nil boundFraction is an unbounded read at full coverage.
func numeratorReading(value float64, people *int, boundFraction *float64) aidlc.Reading {
	r := aidlc.Reading{Status: aidlc.StatusResolved, Value: f64(value), People: zeroPeople(value, people)}
	if boundFraction == nil {
		r.Coverage = aidlc.CoverageFull
		return r
	}
	r.Coverage = coverageFor(*boundFraction)
	// BoundValue widens the numerator only under partial coverage (the scorer reads it
	// on the CoveragePartial branch); carrying it on a full read is harmless and keeps
	// the reading self-describing.
	r.BoundValue = boundFraction
	return r
}

// declaredPartialReading builds a resolved reading whose source reports only a
// subset: the routed-source, command-telemetry and observability-tool reads, too thin
// for a verdict. Declared-partial widens the numerator by the whole unmeasured
// remainder, so the interval straddles the cut and the cell abstains rather than
// denying: the "abstains" status the ruleset marks these signals with, produced by the
// reading rather than asserted.
func declaredPartialReading(value float64, people *int) aidlc.Reading {
	return aidlc.Reading{
		Status:   aidlc.StatusResolved,
		Value:    f64(value),
		People:   zeroPeople(value, people),
		Coverage: aidlc.CoverageDeclaredPartial,
	}
}

// zeroPeople is the people count behind a numerator: a numerator that counted nothing
// was done by nobody, a measured zero, while a count with no author identity behind it
// stays unknown (nil).
func zeroPeople(value float64, people *int) *int {
	if people == nil && value == 0 {
		return iptr(0)
	}
	return people
}

// denominatorReading builds a resolved denominator reading (no people floor reads a
// denominator's people count). remainder, when non-nil, is the count of population
// members that could not be enumerated — a declared remainder the scorer folds into
// the population and the unread mass.
func denominatorReading(value float64, remainder *int) aidlc.Reading {
	r := aidlc.Reading{Status: aidlc.StatusResolved, Value: f64(value)}
	if remainder != nil {
		r.RemainderDeclared = true
		r.RemainderValue = remainder
	}
	return r
}

// noRun is the reading for a signal no fold could produce a value for: a term the
// facts genuinely do not carry. It abstains as a gap, never as a zero.
func noRun(note string) aidlc.Reading {
	return aidlc.Reading{Status: aidlc.StatusNoRunRecord, Note: note}
}

func f64(v float64) *float64 { return &v }
func iptr(v int) *int        { return &v }
