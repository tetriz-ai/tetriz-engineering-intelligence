// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package vcs

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
)

func TestConvert_PerVendor(t *testing.T) {
	merged := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		rec  Record
		want branchlineage.PR
	}{
		{
			name: "GitHub: base_ref_name, the fork flag and open date from the payload",
			rec: Record{
				ExternalID: "acme/widgets#PR-42#merged",
				DataDate:   merged,
				Metadata: map[string]string{
					"repo": "widgets", "owner": "acme", "head_branch": "develop", "base_ref_name": "main",
					"files_line_stats": `[{"additions":40,"deletions":10},{"additions":2,"deletions":1}]`,
				},
				Payload: json.RawMessage(`{"createdAt":"2026-03-08T09:00:00Z","isCrossRepository":true}`),
			},
			want: branchlineage.PR{
				Key: "acme/widgets#PR-42#merged", Repository: "acme/widgets", Number: 42,
				HeadBranch: "develop", BaseBranch: "main", OpenedAt: time.Date(2026, 3, 8, 9, 0, 0, 0, time.UTC),
				MergedAt: merged, LinesAdded: 42, LinesDeleted: 11, CrossRepository: true,
			},
		},
		{
			name: "GitLab: target_branch, the fork flag and first commit from metadata",
			rec: Record{
				ExternalID: "mr-1234!7#merged",
				DataDate:   merged,
				Metadata: map[string]string{
					"repo": "acme/widgets", "head_branch": "integration", "target_branch": "main",
					"is_cross_repository": "true", "first_commit_at": "2026-03-01T08:00:00Z",
				},
				Payload: json.RawMessage(`{"created_at":"2026-03-02T09:00:00Z"}`),
			},
			want: branchlineage.PR{
				Key: "mr-1234!7#merged", Repository: "acme/widgets", Number: 7,
				HeadBranch: "integration", BaseBranch: "main", OpenedAt: time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC),
				MergedAt: merged, FirstCommitAt: time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC), CrossRepository: true,
			},
		},
		{
			name: "Bitbucket: destination_branch and created_on",
			rec: Record{
				ExternalID: "acme/widgets#PR-9#merged",
				DataDate:   merged,
				Metadata:   map[string]string{"repo": "acme/widgets", "head_branch": "release/2.0", "destination_branch": "main"},
				Payload:    json.RawMessage(`{"created_on":"2026-03-03T09:00:00Z"}`),
			},
			want: branchlineage.PR{
				Key: "acme/widgets#PR-9#merged", Repository: "acme/widgets", Number: 9,
				HeadBranch: "release/2.0", BaseBranch: "main", OpenedAt: time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC),
				MergedAt: merged,
			},
		},
		{
			name: "the head and merge commit times from metadata",
			rec: Record{
				ExternalID: "acme/widgets#PR-11#merged",
				DataDate:   merged,
				Metadata: map[string]string{
					"repo": "acme/widgets", "head_branch": "develop", "base_ref_name": "main",
					"head_commit_at": "2026-03-09T07:30:00Z", "merge_commit_at": "2026-03-10T06:29:58.5Z",
				},
			},
			want: branchlineage.PR{
				Key: "acme/widgets#PR-11#merged", Repository: "acme/widgets", Number: 11,
				HeadBranch: "develop", BaseBranch: "main", MergedAt: merged,
				HeadCommitAt:  time.Date(2026, 3, 9, 7, 30, 0, 0, time.UTC),
				MergeCommitAt: time.Date(2026, 3, 10, 6, 29, 58, 500000000, time.UTC),
			},
		},
		{
			name: "unreadable fields are left at zero, and a zero date is not merged",
			rec: Record{
				ExternalID: "acme/widgets#PR-x#opened",
				Metadata: map[string]string{"repo": "acme/widgets", "first_commit_at": "yesterday", "files_line_stats": "12",
					"head_commit_at": "soon", "merge_commit_at": "2026-03-10"},
				Payload: json.RawMessage(`not json`),
			},
			want: branchlineage.PR{Key: "acme/widgets#PR-x#opened", Repository: "acme/widgets"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Convert([]Record{tt.rec})
			require.Len(t, got, 1)
			assert.Equal(t, tt.want, got[0])
			assert.Equal(t, !tt.want.MergedAt.IsZero(), got[0].Merged())
		})
	}
}

func TestParseNumber(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want int32
	}{
		{name: "the common form", key: "acme/widgets#PR-42#merged", want: 42},
		{name: "a merge-request key", key: "mr-1234!7#merged", want: 7},
		{name: "no number is zero", key: "acme/widgets", want: 0},
		{name: "a malformed merge-request key is zero", key: "mr-1234#merged", want: 0},
		{name: "a negative number is rejected", key: "a#PR--3#m", want: 0},
		{name: "trailing characters are rejected", key: "a#PR-7x#m", want: 0},
		{name: "zero is not a number", key: "a#PR-0#m", want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, ParseNumber(tt.key))
		})
	}
}

// TestConvert_FeedsTheDeriver: converted records derive the same edge a
// hand-built history does.
func TestConvert_FeedsTheDeriver(t *testing.T) {
	at := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	prs := Convert([]Record{
		{ExternalID: "acme/widgets#PR-1#merged", DataDate: at, Metadata: map[string]string{"repo": "acme/widgets", "head_branch": "feature/a", "base_ref_name": "develop"}},
		{ExternalID: "acme/widgets#PR-2#merged", DataDate: at.Add(time.Hour), Metadata: map[string]string{"repo": "acme/widgets", "head_branch": "develop", "base_ref_name": "main"}},
	})
	g, err := branchlineage.Derive(prs)
	require.NoError(t, err)
	require.Len(t, g.Edges(), 1)
	assert.Equal(t, "acme/widgets#PR-2#merged", g.Edges()[0].Carrier)
	assert.Equal(t, "acme/widgets#PR-1#merged", g.Edges()[0].Carried)
}
