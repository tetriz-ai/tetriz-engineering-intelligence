// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

// Observer receives the engine's diagnostic events: counters, distributions and
// the occasional explanatory line.
//
// Every method is advisory. An Observer never changes a matching decision, and a
// slow or failing one must not change one either — the engine calls these inline,
// so an implementation that blocks makes matching slower and nothing else.
//
// A nil Observer is valid and means "discard everything"; the engine never calls
// through a nil interface.
//
// jaccard_shadow_evaluations (a count) and jaccard_shadow_admits (a debug event)
// report file-overlap pairs a looser Jaccard rule would admit; nothing acts on them.
type Observer interface {
	// Count records one occurrence of a named event.
	Count(event string, labels map[string]string)
	// Observe records one sample of a named distribution.
	Observe(event string, value float64, labels map[string]string)
	// Debug records an explanatory event that is not a metric. Used where the
	// engine takes a path worth being able to explain after the fact.
	Debug(event string, fields map[string]any)
}

// nopObserver discards every event. It is the default, so an adopter that wants
// no instrumentation configures nothing.
type nopObserver struct{}

func (nopObserver) Count(string, map[string]string)            {}
func (nopObserver) Observe(string, float64, map[string]string) {}
func (nopObserver) Debug(string, map[string]any)               {}

// obs returns the configured Observer, or a discarding one.
func (p Params) obs() Observer {
	if p.Observer == nil {
		return nopObserver{}
	}
	return p.Observer
}

// crossPlatformBasename returns the last path segment, accepting either
// separator. Session paths arrive from whichever platform the editor ran on, so
// a Windows-authored path must reduce the same way a POSIX one does.
func crossPlatformBasename(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[i+1:]
		}
	}
	return p
}
