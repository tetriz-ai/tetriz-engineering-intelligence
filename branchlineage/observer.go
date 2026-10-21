// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

// Observer receives the engine's diagnostic events: counters and
// distributions, for a caller that wants its own metrics.
//
// Every method is advisory. An Observer never changes a result, and the engine
// calls it inline, so a slow one makes the engine slower and nothing else. A nil
// Observer, the default, discards everything.
//
// Events: carrier_outcome (count, label outcome), edges_per_carrier (sample),
// walk_carriers_loaded and walk_routes (counts per walk level), walk_depth_reached
// and walk_peak_routes (one sample per walk), walk_depth_cap_hit (count, label
// repository), walk_route_cap_hit (count), walk_route_demand (sample on a route
// cap hit), invalidation (count, label reason).
type Observer interface {
	// Count records one occurrence of a named event.
	Count(event string, labels map[string]string)
	// Observe records one sample of a named distribution.
	Observe(event string, value float64, labels map[string]string)
}

// nopObserver discards every event.
type nopObserver struct{}

func (nopObserver) Count(string, map[string]string)            {}
func (nopObserver) Observe(string, float64, map[string]string) {}

// Option configures an engine call.
type Option func(*config)

// WithObserver sends the call's events to o. A nil o discards them.
func WithObserver(o Observer) Option {
	return func(c *config) {
		if o != nil {
			c.observer = o
		}
	}
}

type config struct {
	observer Observer
}

func newConfig(opts []Option) config {
	c := config{observer: nopObserver{}}
	for _, o := range opts {
		if o != nil {
			o(&c)
		}
	}
	return c
}
