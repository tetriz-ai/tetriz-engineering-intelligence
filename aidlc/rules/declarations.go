// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package rules

import (
	"sort"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// The declaration fields an organisation's admin can declare. A declaration opens
// the cells whose checks read a signal only that declaration can supply.
const (
	DeclarationTestCommands = "test_commands"
	DeclarationCiCommands   = "ci_commands"
	DeclarationReviewPolicy = "review_policy"
)

// declarationSignals maps each declaration field to the signals only that
// declaration can supply.
var declarationSignals = map[string][]aidlcv1.SignalKey{
	DeclarationTestCommands: {
		aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_TEST_COMMAND_SESSIONS,
		aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_TEST_COMMAND_GREEN_SESSIONS,
	},
	DeclarationCiCommands: {
		aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_CI_COMMAND_SESSIONS,
		aidlcv1.SignalKey_SIGNAL_KEY_DECLARED_CI_COMMAND_GREEN_SESSIONS,
	},
	DeclarationReviewPolicy: {
		aidlcv1.SignalKey_SIGNAL_KEY_POLICY_COVERED_AI_MERGED_PRS,
		aidlcv1.SignalKey_SIGNAL_KEY_POLICY_MET_AI_MERGED_PRS,
	},
}

// IsDeclarationSignal reports whether key is one of the signals only the named
// declaration field can supply: the per-signal form of DeclarationEffects, for a
// caller that knows which signal closed a cell and must say which declaration it
// was.
func IsDeclarationSignal(field string, key aidlcv1.SignalKey) bool {
	for _, k := range declarationSignals[field] {
		if k == key {
			return true
		}
	}
	return false
}

// DeclarationFields lists the declaration fields in a fixed order.
var DeclarationFields = []string{DeclarationTestCommands, DeclarationCiCommands, DeclarationReviewPolicy}

// DeclarationEffects returns, per declaration field, the cells of rs that a
// declaration of that field opens, derived from which cells read a
// declaration-supplied signal, so it can never drift from the rules. It is not part
// of the hash because it is derived from what is. rs must not be nil: a nil ruleset
// (such as ForVersion returns for an unknown version) panics.
func DeclarationEffects(rs *RuleSet) map[string][]CellKey {
	out := map[string][]CellKey{}
	for _, field := range DeclarationFields {
		want := map[aidlcv1.SignalKey]bool{}
		for _, k := range declarationSignals[field] {
			want[k] = true
		}
		var cells []CellKey
		for key, cell := range rs.Cells {
			for _, ch := range cell.Checks {
				if want[ch.Signal] || want[ch.Denominator] {
					cells = append(cells, key)
					break
				}
			}
		}
		sortCellKeys(cells)
		out[field] = cells
	}
	return out
}

func sortCellKeys(keys []CellKey) {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Stage != keys[j].Stage {
			return keys[i].Stage < keys[j].Stage
		}
		return keys[i].Level < keys[j].Level
	})
}
