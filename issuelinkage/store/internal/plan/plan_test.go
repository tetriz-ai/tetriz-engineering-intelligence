// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package plan

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
)

// TestNames: a row reads an identifier through an unresolved candidate,
// whatever the reason, or through a link's evidence, compared exactly.
func TestNames(t *testing.T) {
	unresolved := func(id, reason string) issuelinkage.Row {
		return issuelinkage.Row{Result: issuelinkage.Result{Unresolved: []issuelinkage.Unresolved{
			{Candidate: issuelinkage.Candidate{Identifier: id, CaseFolded: true}, Reason: reason},
		}}}
	}
	linked := issuelinkage.Row{Result: issuelinkage.Result{Links: []issuelinkage.Link{
		{Evidence: []issuelinkage.Candidate{{Identifier: "OPS-30"}}},
	}}}
	padded := issuelinkage.ExtractPR(issuelinkage.PR{Key: "WS", Vendor: issuelinkage.VendorGitHub,
		NativeIssueRefs: []issuelinkage.NativeRef{{Key: " OPS-12 ", Source: issuelinkage.VendorGitHub}}},
		issuelinkage.VendorAny, issuelinkage.DefaultSignals())
	paddedNative := issuelinkage.Row{Result: issuelinkage.Result{Unresolved: []issuelinkage.Unresolved{
		{Candidate: padded.Candidates[0], Reason: issuelinkage.ReasonNotFound},
	}}}
	tests := []struct {
		name   string
		row    issuelinkage.Row
		idents []string
		want   bool
	}{
		{"unresolved, same identifier", unresolved("OPS-12", issuelinkage.ReasonNotFound), []string{"OPS-12"}, true},
		{"unresolved, another identifier", unresolved("OPS-12", issuelinkage.ReasonNotFound), []string{"OPS-14"}, false},
		{"case differs, even folded", unresolved("OPS-12", issuelinkage.ReasonNotFound), []string{"ops-12"}, false},
		{"ambiguous", unresolved("OPS-20", issuelinkage.ReasonAmbiguous), []string{"OPS-20"}, true},
		{"link evidence", linked, []string{"OPS-30"}, true},
		{"link evidence, another identifier", linked, []string{"OPS-31"}, false},
		{"nothing", issuelinkage.Row{}, []string{"OPS-12"}, false},
		{"unresolved, a padded native key as extracted", paddedNative, []string{"OPS-12"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := map[string]bool{}
			for _, id := range tt.idents {
				set[id] = true
			}
			assert.Equal(t, tt.want, Names(tt.row, set))
		})
	}
}

// TestIssueIdents: a new or changed issue touches its new and previous
// identifiers; an unchanged one touches none.
func TestIssueIdents(t *testing.T) {
	stored := issuelinkage.Issue{Identifiers: []string{"OLD-1"}}
	is := issuelinkage.Issue{Identifiers: []string{"NEW-1"}}
	for _, tt := range []struct {
		name            string
		exists, changed bool
		want            map[string]bool
	}{
		{"new", false, true, map[string]bool{"NEW-1": true}},
		{"changed", true, true, map[string]bool{"NEW-1": true, "OLD-1": true}},
		{"unchanged", true, false, map[string]bool{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			set := map[string]bool{}
			IssueIdents(set, stored, tt.exists, tt.changed, is)
			assert.Equal(t, tt.want, set)
		})
	}
}
