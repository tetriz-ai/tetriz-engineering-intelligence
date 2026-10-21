// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package vcs_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/adapter/vcs"
)

func TestEntityKey(t *testing.T) {
	for in, want := range map[string]string{
		"acme/web#PR-42#merged":                "acme/web#PR-42",
		"mr-1!1#snapshot":                      "mr-1!1",
		"10001#created":                        "10001",
		"acme/web#ISSUE-7#closed":              "acme/web#ISSUE-7",
		"5f0c2a9e-0000-4000-8000-000000000001": "5f0c2a9e-0000-4000-8000-000000000001",
		"acme/web#PR-42":                       "acme/web#PR-42",
	} {
		assert.Equal(t, want, vcs.EntityKey(in), in)
	}
}

func TestPRs(t *testing.T) {
	merged := time.Date(2026, 7, 7, 23, 0, 0, 0, time.UTC)
	t.Run("github lifecycle records fold into one", func(t *testing.T) {
		payload := `{"number":42,"title":"Fix login","body":"Fixes PROJ-1","mergedAt":"2026-07-07T23:00:00Z","updatedAt":"2026-07-08T00:00:00Z","headRefName":"feature/x",
			"closingIssuesReferences":{"nodes":[{"number":7}]}}`
		got := vcs.PRs(issuelinkage.VendorGitHub, []vcs.Record{
			{ExternalID: "acme/web#PR-42#opened", Metadata: map[string]string{"repo": "acme/web"}, Payload: json.RawMessage(`{"number":42,"title":"old"}`)},
			{ExternalID: "acme/web#PR-42#snapshot", Metadata: map[string]string{"repo": "acme/web", "head_branch": "feature/PROJ-1"}, Payload: json.RawMessage(payload)},
			{ExternalID: "acme/web#PR-42#merged", DataDate: merged, Metadata: map[string]string{"repo": "acme/web"}, Payload: json.RawMessage(`{"number":42,"title":"later"}`)},
		})
		require.Len(t, got, 1)
		p := got[0]
		assert.Equal(t, "acme/web#PR-42", p.Key)
		assert.Equal(t, "acme/web", p.Repository)
		assert.Equal(t, 42, p.Number)
		assert.Equal(t, "Fix login", p.Title, "the snapshot supplies the content")
		assert.Equal(t, "Fixes PROJ-1", p.Body)
		assert.Equal(t, "feature/PROJ-1", p.HeadBranch)
		assert.True(t, p.MergedAt.Equal(merged))
		assert.Equal(t, []issuelinkage.NativeRef{{Key: "acme/web#7", Source: issuelinkage.VendorGitHub}}, p.NativeIssueRefs)
	})
	t.Run("gitlab", func(t *testing.T) {
		got := vcs.PRs(issuelinkage.VendorGitLab, []vcs.Record{{
			ExternalID: "mr-1!1#merged", DataDate: merged,
			Metadata: map[string]string{"repo": "acme/web", "closes_issues": `[{"iid":17,"references":{"full":"acme/web#17"}}]`},
			Payload:  json.RawMessage(`{"iid":1,"title":"t","description":"closes #17","source_branch":"feature/y","merged_at":"2026-07-07T23:00:00Z"}`),
		}})
		require.Len(t, got, 1)
		assert.Equal(t, "mr-1!1", got[0].Key)
		assert.Equal(t, 1, got[0].Number)
		assert.Equal(t, "closes #17", got[0].Body)
		assert.Equal(t, "feature/y", got[0].HeadBranch)
		assert.Equal(t, []issuelinkage.NativeRef{{Key: "acme/web#17", Source: issuelinkage.VendorGitLab}}, got[0].NativeIssueRefs)
	})
	t.Run("bitbucket body falls back to summary.raw; the merge date comes from the record", func(t *testing.T) {
		got := vcs.PRs(issuelinkage.VendorBitbucket, []vcs.Record{
			{ExternalID: "ws/repo#PR-300#merged", DataDate: merged, Metadata: map[string]string{"repo": "ws/repo"},
				Payload: json.RawMessage(`{"id":300,"title":"t","summary":{"raw":"see PROJ-9"},"source":{"branch":{"name":"feature/z"}}}`)},
		})
		require.Len(t, got, 1)
		assert.Equal(t, "see PROJ-9", got[0].Body)
		assert.Equal(t, 300, got[0].Number)
		assert.Equal(t, "feature/z", got[0].HeadBranch)
		assert.True(t, got[0].MergedAt.Equal(merged))
	})
	t.Run("the merge time comes from any merged record, whatever the order", func(t *testing.T) {
		got := vcs.PRs(issuelinkage.VendorGitHub, []vcs.Record{
			{ExternalID: "acme/web#PR-7#merged", Metadata: map[string]string{"repo": "acme/web"}, Payload: json.RawMessage(`{"number":7,"mergedAt":"2026-06-01T00:00:00Z"}`)},
			{ExternalID: "acme/web#PR-7#opened", Metadata: map[string]string{"repo": "acme/web"}, Payload: json.RawMessage(`{"number":7}`)},
		})
		require.Len(t, got, 1)
		assert.True(t, got[0].MergedAt.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)), got[0].MergedAt)
	})
	t.Run("the earliest merged record is kept, whatever the order", func(t *testing.T) {
		got := vcs.PRs(issuelinkage.VendorBitbucket, []vcs.Record{
			{ExternalID: "ws/repo#PR-1#merged", DataDate: time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC), Payload: json.RawMessage(`{"id":1}`)},
			{ExternalID: "ws/repo#PR-1#merged", DataDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), Payload: json.RawMessage(`{"id":1}`)},
		})
		require.Len(t, got, 1)
		assert.True(t, got[0].MergedAt.Equal(time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)), got[0].MergedAt)
	})
	t.Run("a GitHub key gives the repository when the metadata has none", func(t *testing.T) {
		got := vcs.PRs(issuelinkage.VendorGitHub, []vcs.Record{{ExternalID: "acme/web#PR-3#opened", Payload: json.RawMessage(`{"number":3,"body":"Fixes #9"}`)}})
		require.Len(t, got, 1)
		assert.Equal(t, "acme/web", got[0].Repository)
	})
	t.Run("a malformed payload leaves fields at zero", func(t *testing.T) {
		got := vcs.PRs(issuelinkage.VendorGitHub, []vcs.Record{{ExternalID: "acme/web#PR-1#opened", Payload: json.RawMessage(`{"body":`)}})
		require.Len(t, got, 1)
		assert.Equal(t, "", got[0].Body)
	})
}

func TestIssues(t *testing.T) {
	tests := []struct {
		name      string
		vendor    issuelinkage.Vendor
		rec       vcs.Record
		key       string
		ident     string
		body      string
		native    []issuelinkage.NativeRef
		truncated bool
	}{
		{name: "github", vendor: issuelinkage.VendorGitHub,
			rec: vcs.Record{ExternalID: "acme/web#ISSUE-7#snapshot", Payload: json.RawMessage(`{"number":7,"title":"t","body":"closed by #7","repository":{"nameWithOwner":"acme/web"},"closedByPullRequestsReferences":{"nodes":[{"number":9}]}}`)},
			key: "acme/web#ISSUE-7", ident: "acme/web#7", body: "closed by #7",
			native: []issuelinkage.NativeRef{{Key: "acme/web#PR-9", Source: issuelinkage.VendorGitHub}}},
		{name: "gitlab", vendor: issuelinkage.VendorGitLab,
			rec: vcs.Record{ExternalID: "issue-1!120#opened", Payload: json.RawMessage(`{"iid":120,"title":"t","description":"fixed by !12","references":{"full":"acme/checkout#120"}}`)},
			key: "issue-1!120", ident: "acme/checkout#120", body: "fixed by !12"},
		{name: "jira ADF", vendor: issuelinkage.VendorJira,
			rec: vcs.Record{ExternalID: "10001#created", Metadata: map[string]string{"issue_key": "WORK-1"},
				Payload: json.RawMessage(`{"id":"10001","key":"WORK-1","fields":{"summary":"t","updated":"2026-07-07T02:00:00.000+0000","description":{"type":"doc","content":[{"type":"text","text":"see acme/web#PR-42"}]}}}`)},
			key: "10001", ident: "WORK-1", body: "see acme/web#PR-42"},
		{name: "jira key from metadata", vendor: issuelinkage.VendorJira,
			rec: vcs.Record{ExternalID: "10002#snapshot", Metadata: map[string]string{"issue_key": "WORK-2"}, Payload: json.RawMessage(`{"fields":{"description":"plain"}}`)},
			key: "10002", ident: "WORK-2", body: "plain"},
		{name: "linear", vendor: issuelinkage.VendorLinear,
			rec: vcs.Record{ExternalID: "5f0c2a9e-0000-4000-8000-000000000001", Payload: json.RawMessage(`{"id":"5f0c2a9e-0000-4000-8000-000000000001","identifier":"ENG-42","title":"t","description":"Fixed by https://github.com/acme/web/pull/42",
				"attachments":{"nodes":[{"sourceType":"github","url":"https://github.com/acme/web/pull/42"}],"pageInfo":{"hasNextPage":true}}}`)},
			key: "5f0c2a9e-0000-4000-8000-000000000001", ident: "ENG-42", body: "Fixed by https://github.com/acme/web/pull/42",
			native: []issuelinkage.NativeRef{{Key: "acme/web#PR-42", Source: issuelinkage.VendorLinear}}, truncated: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vcs.Issues(tt.vendor, []vcs.Record{tt.rec})
			require.Len(t, got, 1)
			is := got[0]
			assert.Equal(t, tt.key, is.Key)
			assert.Equal(t, []string{tt.ident}, is.Identifiers)
			assert.Equal(t, tt.body, is.Body)
			assert.Equal(t, tt.native, is.NativePRRefs)
			assert.Equal(t, tt.truncated, is.NativeRefsTruncated)
		})
	}
}

// TestNativeListNilMeansNotFetched: an absent native field gives nil, which a
// store reads as "keep what you hold"; a present, empty one gives an empty
// list.
func TestNativeListNilMeansNotFetched(t *testing.T) {
	absent := vcs.PRs(issuelinkage.VendorGitHub, []vcs.Record{{ExternalID: "acme/web#PR-1#opened", Payload: json.RawMessage(`{"number":1}`)}})
	assert.Nil(t, absent[0].NativeIssueRefs)
	empty := vcs.PRs(issuelinkage.VendorGitHub, []vcs.Record{{ExternalID: "acme/web#PR-1#opened", Payload: json.RawMessage(`{"number":1,"closingIssuesReferences":{"nodes":[]}}`)}})
	assert.NotNil(t, empty[0].NativeIssueRefs)
	assert.Empty(t, empty[0].NativeIssueRefs)
	external := vcs.PRs(issuelinkage.VendorGitLab, []vcs.Record{{ExternalID: "mr-1!1#opened", Metadata: map[string]string{"repo": "acme/web", "closes_issues": `[{"id":"PROJ-1"}]`}}})
	assert.Nil(t, external[0].NativeIssueRefs, "an external tracker's items are not an authoritative none")
	linear := vcs.Issues(issuelinkage.VendorLinear, []vcs.Record{{ExternalID: "u", Payload: json.RawMessage(`{"identifier":"ENG-1"}`)}})
	assert.Nil(t, linear[0].NativePRRefs)
}
