// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// identifiers returns each candidate's identifier, in the order found.
func identifiers(ex Extraction) []string {
	out := make([]string, 0, len(ex.Candidates))
	for _, c := range ex.Candidates {
		out = append(out, c.Identifier)
	}
	return out
}

// TestExtract_AnOverLongBodyIsCutAtALineBoundary: the cut must not run
// through a key. This body places "PROJ-123456" so that the byte limit falls
// after its "PROJ-12", which on a plain cut would leave a key that still
// matches and names a real, unrelated issue.
func TestExtract_AnOverLongBodyIsCutAtALineBoundary(t *testing.T) {
	const head = "Fixes PROJ-1\n"
	const tail = " PROJ-123456 and more"
	pad := strings.Repeat("z", MaxTextBytes-len(head)-len(" PROJ-12"))
	body := head + pad + tail
	require.Equal(t, " PROJ-12", body[MaxTextBytes-len(" PROJ-12"):MaxTextBytes],
		"the limit must fall inside the key for this test to mean anything")

	ex := ExtractPR(PR{Key: "p", Vendor: VendorGitHub, Repository: "acme/web", Body: body}, VendorAny, DefaultSignals())

	assert.True(t, ex.Truncated)
	assert.Equal(t, []string{"PROJ-1"}, identifiers(ex),
		"a key the cut ran through must not survive as a shorter key")
}

// TestExtract_AnOverLongLineWithNoBreakIsDroppedWhole: there is no safe place
// to cut a single line, so none is taken.
func TestExtract_AnOverLongLineWithNoBreakIsDroppedWhole(t *testing.T) {
	body := "PROJ-1 " + strings.Repeat("z", MaxTextBytes)

	ex := ExtractPR(PR{Key: "p", Vendor: VendorGitHub, Repository: "acme/web", Body: body}, VendorAny, DefaultSignals())

	assert.True(t, ex.Truncated)
	assert.Empty(t, ex.Candidates)
}

// TestExtract_ABodyAtTheLimitIsNotTruncated: the limit is inclusive, and a
// record under it reads exactly as it did before the limit existed.
func TestExtract_ABodyAtTheLimitIsNotTruncated(t *testing.T) {
	const ref = "Fixes PROJ-1\n"
	body := ref + strings.Repeat("z", MaxTextBytes-len(ref))
	require.Len(t, body, MaxTextBytes)

	ex := ExtractPR(PR{Key: "p", Vendor: VendorGitHub, Repository: "acme/web", Body: body}, VendorAny, DefaultSignals())

	assert.False(t, ex.Truncated)
	assert.Equal(t, []string{"PROJ-1"}, identifiers(ex))
}

// TestExtract_AnOverLongBranchIsReportedTruncated: every scanned field is
// clamped, not only the body.
func TestExtract_AnOverLongBranchIsReportedTruncated(t *testing.T) {
	ex := ExtractPR(PR{
		Key: "p", Vendor: VendorGitHub, Repository: "acme/web",
		HeadBranch: "eng-42-" + strings.Repeat("z", MaxTextBytes),
	}, VendorAny, DefaultSignals())

	assert.True(t, ex.Truncated)
}

// TestValidate_RejectsAnOversizedField: a record that would make the scan
// truncate is refused at the store door, naming the record and the field.
func TestValidate_RejectsAnOversizedField(t *testing.T) {
	long := strings.Repeat("z", MaxTextBytes+1)

	err := Validate([]PR{{Key: "acme/web#PR-1", Body: long}}, nil)
	require.ErrorIs(t, err, ErrInvalidRecord)
	assert.Contains(t, err.Error(), "acme/web#PR-1")
	assert.Contains(t, err.Error(), "body")

	err = Validate(nil, []Issue{{Key: "PROJ-1", Title: long}})
	require.ErrorIs(t, err, ErrInvalidRecord)
	assert.Contains(t, err.Error(), "PROJ-1")
	assert.Contains(t, err.Error(), "title")

	assert.NoError(t, Validate(
		[]PR{{Key: "acme/web#PR-1", Body: strings.Repeat("z", MaxTextBytes)}},
		[]Issue{{Key: "PROJ-1"}},
	))
}
