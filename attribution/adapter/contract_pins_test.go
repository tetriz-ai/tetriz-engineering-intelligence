// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package adapter_test pins the input contract this module's adapters accept.
//
// A contract can change on the producing side silently, and the way it fails
// here is silent too. An unrecognised provenance class reads as unclassified,
// and an unrecognised editor name reads as ungated: both produce attributions
// that are wrong rather than absent.
//
// So the values are written out literally. If an upstream rename lands, this
// fails and names what moved, instead of the number quietly changing.
package adapter_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/adapter/localsession"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// upstreamIDETypes is the editor-name table localsession.IDEType translates,
// verbatim.
var upstreamIDETypes = map[string]string{
	"claude-code": "IDE_TYPE_CLAUDE_CODE",
	"cursor":      "IDE_TYPE_CURSOR",
	"copilot":     "IDE_TYPE_COPILOT",
	"antigravity": "IDE_TYPE_ANTIGRAVITY",
	"codex":       "IDE_TYPE_CODEX",
	"kiro":        "IDE_TYPE_KIRO",
}

// upstreamSignalSources is the extractor's SIGNAL_SOURCE constant set, verbatim.
var upstreamSignalSources = []string{
	"SIGNAL_SOURCE_SESSION_COMMAND",
	"SIGNAL_SOURCE_VENDOR_FIELD",
	"SIGNAL_SOURCE_SESSION_PROSE",
	"SIGNAL_SOURCE_LOCAL_GIT_WINDOWED",
	"SIGNAL_SOURCE_UNSPECIFIED",
}

func TestEveryUpstreamEditorTranslates(t *testing.T) {
	for local, want := range upstreamIDETypes {
		assert.Equal(t, want, localsession.IDEType(local),
			"the extractor emits %q; an untranslated value leaves the session ungated", local)
		// The upload table is an exact lookup, so no other spelling translates.
		assert.Equal(t, "IDE_TYPE_UNSPECIFIED", localsession.IDEType(want), "the enum name %q", want)
		assert.Equal(t, "IDE_TYPE_UNSPECIFIED", localsession.IDEType(strings.ToUpper(local)), "upper case %q", local)
	}
	assert.Equal(t, "IDE_TYPE_UNSPECIFIED", localsession.IDEType("emacs"), "an unknown editor stays unspecified")
}

// If the extractor gains a seventh editor, this does not fail — nothing here can
// see that. What it does catch is the opposite and likelier direction: a name
// this module claims to handle that the extractor does not emit, which would
// leave a live editor silently ungated.
func TestNoEditorIsClaimedThatUpstreamDoesNotEmit(t *testing.T) {
	for local := range upstreamIDETypes {
		require.NotEqual(t, "IDE_TYPE_UNSPECIFIED", localsession.IDEType(local),
			"this module claims to translate %q; if the extractor renamed it, "+
				"update both the map and this pin", local)
	}
}

// Every provenance class the extractor writes must parse into this module's
// enum. One that does not degrades to unclassified, which the engine treats as
// workspace-ambient — so a branch the session genuinely pushed would stop being
// able to carry a match, and nothing would say so.
func TestEveryUpstreamProvenanceClassParses(t *testing.T) {
	for _, s := range upstreamSignalSources {
		t.Run(s, func(t *testing.T) {
			v, ok := attributionv1.SignalSource_value[s]
			require.True(t, ok, "%q has no counterpart in this module's SignalSource", s)
			if s != "SIGNAL_SOURCE_UNSPECIFIED" {
				assert.NotZero(t, v, "%q must not collapse to the unspecified value", s)
			}
		})
	}
}

// And the reverse: every value in this module's enum must be one the extractor
// actually emits. A value here with no upstream producer is a class that can
// never arrive, which makes any rule keyed on it dead code.
func TestThisModuleDeclaresNoProvenanceClassUpstreamCannotProduce(t *testing.T) {
	upstream := map[string]struct{}{}
	for _, s := range upstreamSignalSources {
		upstream[s] = struct{}{}
	}
	for name := range attributionv1.SignalSource_value {
		_, ok := upstream[name]
		assert.Truef(t, ok, "this module declares %s, which the extractor does not emit", name)
	}
}

// connectorMetadataKeys are the keys vcs.Convert reads from Record.Metadata.
var connectorMetadataKeys = []string{
	"repo", "owner", "head_branch", "files_changed", "files_line_stats",
}

// A key the connector stops writing does not error — the field simply arrives
// empty, and a candidate missing its files or branch quietly matches less. This
// pin is the written record of which keys that applies to, so a change upstream
// has somewhere to land.
func TestVCSAdapterReadsTheDocumentedMetadataKeys(t *testing.T) {
	assert.ElementsMatch(t,
		[]string{"repo", "owner", "head_branch", "files_changed", "files_line_stats"},
		connectorMetadataKeys,
		"update adapter/vcs and this pin together")
}
