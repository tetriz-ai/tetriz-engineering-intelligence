// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage

// Observer receives the pipeline's diagnostic events: counters, distributions
// and the occasional explanatory line.
//
// Every method is advisory. An Observer never changes an attribution decision,
// and a slow or failing one must not change one either — the pipeline calls
// these inline, so an implementation that blocks makes tracing slower and
// nothing else.
//
// A nil Observer is valid and means "discard everything"; the pipeline never
// calls through a nil interface.
type Observer interface {
	// Count records one occurrence of a named event.
	Count(event string, labels map[string]string)
	// Observe records one sample of a named distribution.
	Observe(event string, value float64, labels map[string]string)
	// Debug records an explanatory event that is not a metric. Used where the
	// pipeline takes a path worth being able to explain after the fact.
	Debug(event string, fields map[string]any)
}

// nopObserver discards every event. It is the default, so an adopter that wants
// no instrumentation configures nothing.
type nopObserver struct{}

func (nopObserver) Count(string, map[string]string)            {}
func (nopObserver) Observe(string, float64, map[string]string) {}
func (nopObserver) Debug(string, map[string]any)               {}

// orNop returns o, or a discarding Observer when o is nil.
func orNop(o Observer) Observer {
	if o == nil {
		return nopObserver{}
	}
	return o
}
