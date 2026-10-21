// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package rules

import (
	"sort"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// registry is the map of immutable rule sets keyed by version. It is built once at
// package init and never mutated: a new version adds an entry, and an old one is
// never edited.
var registry = func() map[string]*RuleSet {
	m := map[string]*RuleSet{}
	for _, rs := range []*RuleSet{buildV1()} {
		if _, dup := m[rs.Version]; dup {
			panic("aidlc/rules: duplicate ruleset version " + rs.Version)
		}
		m[rs.Version] = rs
	}
	return m
}()

// ForVersion returns a copy of the ruleset for a version, or nil when no such version
// is registered. A caller handed an unknown version must treat nil as "this module
// cannot score that version" rather than falling back to another. Each call returns a
// fresh deep copy, so changing one never changes what another caller scores; a
// changed copy keeps the registered Hash, which then no longer describes it.
func ForVersion(version string) *RuleSet {
	rs := registry[version]
	if rs == nil {
		return nil
	}
	return rs.clone()
}

// clone deep-copies a ruleset: every map and slice is new, so no field of the copy
// shares memory with the registered set.
func (rs *RuleSet) clone() *RuleSet {
	out := *rs
	out.Signals = make(map[aidlcv1.SignalKey]SignalDef, len(rs.Signals))
	for k, v := range rs.Signals {
		out.Signals[k] = v
	}
	out.Cells = make(map[CellKey]CellRule, len(rs.Cells))
	for k, v := range rs.Cells {
		v.Checks = append([]Check(nil), v.Checks...)
		out.Cells[k] = v
	}
	out.Bottlenecks = append([]BottleneckRule(nil), rs.Bottlenecks...)
	out.GapRemediation = make(map[aidlcv1.InstrumentationGap]RemediationClass, len(rs.GapRemediation))
	for k, v := range rs.GapRemediation {
		out.GapRemediation[k] = v
	}
	return &out
}

// Versions lists every registered version, sorted.
func Versions() []string {
	out := make([]string, 0, len(registry))
	for v := range registry {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
