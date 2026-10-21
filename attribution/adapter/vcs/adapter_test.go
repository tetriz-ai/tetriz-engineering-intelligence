// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package vcs

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

const authored = attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED

func TestParseNumber(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want int32
	}{
		{name: "the common form", key: "acme/widgets#PR-42#merged", want: 42},
		{name: "a merge-request key", key: "mr-1234!7#merged", want: 7},
		{name: "a repository name containing a dash", key: "acme/my-widgets#PR-9#merged", want: 9},
		{name: "no number is zero, not an error", key: "acme/widgets", want: 0},
		{name: "a malformed merge-request key is zero", key: "mr-1234#merged", want: 0},
		{name: "empty is zero", key: "", want: 0},
		{name: "a negative number is rejected", key: "a#PR--3#m", want: 0},
		{name: "trailing characters are rejected", key: "a#PR-7x#m", want: 0},
		{name: "a merge-request iid with trailing characters is rejected", key: "mr-1!7x#m", want: 0},
		{name: "zero is not a number", key: "a#PR-0#m", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseNumber(tt.key))
		})
	}
}

func TestConvertReadsTheIndexedFields(t *testing.T) {
	merged := time.Date(2026, time.March, 10, 12, 0, 0, 0, time.UTC)
	got := Convert([]Record{{
		ExternalID: "acme/widgets#PR-42#merged",
		DataDate:   merged,
		Metadata: map[string]string{
			"repo":             "acme/widgets",
			"head_branch":      "feat/rate-limiter",
			"files_changed":    `["limiter/bucket.go","limiter/window.go"]`,
			"files_line_stats": `[{"additions":40,"deletions":10},{"additions":2,"deletions":1}]`,
		},
		Payload: json.RawMessage(`{"body":"adds a limiter","createdAt":"2026-03-08T09:00:00Z"}`),
	}}, authored)

	require.Len(t, got, 1)
	c := got[0]
	assert.Equal(t, "acme/widgets#PR-42#merged", c.GetPrKey())
	assert.Equal(t, "acme/widgets", c.GetPrRepository())
	assert.Equal(t, int32(42), c.GetPrNumber())
	assert.Equal(t, "feat/rate-limiter", c.GetHeadBranch())
	assert.Equal(t, []string{"limiter/bucket.go", "limiter/window.go"}, c.GetFilesChanged())
	assert.Equal(t, int64(42), c.GetLinesAdded())
	assert.Equal(t, int64(11), c.GetLinesRemoved())
	assert.Equal(t, "adds a limiter", c.GetBody())
	assert.Equal(t, merged, c.GetMergedAt().AsTime())
	assert.Equal(t, authored, c.GetParticipationKind())
}

func TestBareRepositoryIsRejoinedWithItsOwner(t *testing.T) {
	tests := []struct {
		name string
		meta map[string]string
		want string
	}{
		{name: "bare repo gains its owner", meta: map[string]string{"repo": "widgets", "owner": "acme"}, want: "acme/widgets"},
		{name: "a qualified repo is left alone", meta: map[string]string{"repo": "acme/widgets", "owner": "acme"}, want: "acme/widgets"},
		{name: "no owner leaves it bare rather than inventing one", meta: map[string]string{"repo": "widgets"}, want: "widgets"},
		{name: "no repo is empty", meta: map[string]string{"owner": "acme"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Convert([]Record{{ExternalID: "x#PR-1#merged", Metadata: tt.meta}}, authored)
			assert.Equal(t, tt.want, got[0].GetPrRepository())
		})
	}
}

// An unmerged pull request must stay undated. Dating it to the epoch would place
// it before every session and drop it from the branch signal entirely, with
// nothing to show that it happened.
func TestAnUnmergedPullRequestStaysUndated(t *testing.T) {
	got := Convert([]Record{{ExternalID: "acme/widgets#PR-7#opened", Metadata: map[string]string{"repo": "acme/widgets"}}}, authored)
	require.Len(t, got, 1)
	assert.Nil(t, got[0].GetMergedAt(), "no merge date must stay unset, not become 1970")
}

// A record with an unreadable field still matches on the fields that did parse.
// Refusing the whole record would lose a branch or direct-reference match that
// never needed the broken one.
func TestAnUnparseableFieldIsDroppedNotFatal(t *testing.T) {
	got := Convert([]Record{{
		ExternalID: "acme/widgets#PR-42#merged",
		Metadata: map[string]string{
			"repo":             "acme/widgets",
			"head_branch":      "feat/thing",
			"files_changed":    `{not an array}`,
			"files_line_stats": `also not`,
		},
		Payload: json.RawMessage(`{broken`),
	}}, authored)

	require.Len(t, got, 1)
	c := got[0]
	assert.Nil(t, c.GetFilesChanged())
	assert.Zero(t, c.GetLinesAdded())
	assert.Empty(t, c.GetBody())
	assert.Equal(t, "feat/thing", c.GetHeadBranch(), "the fields that parsed must survive")
	assert.Equal(t, int32(42), c.GetPrNumber())
}

func TestOpenDateReadsEveryVendorSpelling(t *testing.T) {
	for _, key := range []string{"createdAt", "created_at", "created_on"} {
		t.Run(key, func(t *testing.T) {
			payload := json.RawMessage(`{"` + key + `":"2026-03-08T09:30:00Z"}`)
			got := Convert([]Record{{ExternalID: "x#PR-1#merged", Payload: payload}}, authored)
			require.NotNil(t, got[0].GetOpenedAt())
			// Normalised to the calendar day, matching the granularity the merge
			// date is compared at.
			assert.Equal(t, time.Date(2026, time.March, 8, 0, 0, 0, 0, time.UTC), got[0].GetOpenedAt().AsTime())
		})
	}
}
