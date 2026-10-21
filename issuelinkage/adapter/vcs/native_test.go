// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package vcs

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
)

func keysOf(refs []issuelinkage.NativeRef) []string {
	out := []string{}
	for _, r := range refs {
		out = append(out, r.Key)
	}
	return out
}

func TestClosingIssueKeys(t *testing.T) {
	tests := []struct {
		name, payload, fallback string
		want                    []string
		extracted               bool
	}{
		{"two issues across repositories", `{"closingIssuesReferences":{"nodes":[{"number":42,"repository":{"nameWithOwner":"acme/api"}},{"number":7,"repository":{"nameWithOwner":"acme/web"}}]}}`, "acme/web", []string{"acme/api#42", "acme/web#7"}, true},
		{"no repository falls back to the pull request's", `{"closingIssuesReferences":{"nodes":[{"number":3}]}}`, "acme/web", []string{"acme/web#3"}, true},
		{"field absent is not extracted", `{"number":1}`, "acme/web", []string{}, false},
		{"empty nodes is extracted with no keys", `{"closingIssuesReferences":{"nodes":[]}}`, "acme/web", []string{}, true},
		{"number zero skipped", `{"closingIssuesReferences":{"nodes":[{"number":0,"repository":{"nameWithOwner":"a/b"}}]}}`, "", []string{}, true},
		{"no repository and no fallback skipped", `{"closingIssuesReferences":{"nodes":[{"number":4}]}}`, "", []string{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, extracted, err := closingIssueKeys(json.RawMessage(tt.payload), tt.fallback)
			require.NoError(t, err)
			assert.Equal(t, tt.extracted, extracted)
			assert.Equal(t, tt.want, keysOf(refs))
			for _, r := range refs {
				assert.Equal(t, issuelinkage.VendorGitHub, r.Source)
			}
		})
	}
	_, _, err := closingIssueKeys(json.RawMessage(`{`), "")
	assert.Error(t, err)
}

func TestClosingPRKeys(t *testing.T) {
	refs, extracted, err := closingPRKeys(json.RawMessage(`{"closedByPullRequestsReferences":{"nodes":[{"number":141,"repository":{"nameWithOwner":"acme/api"}},{"number":9}]}}`), "acme/web")
	require.NoError(t, err)
	assert.True(t, extracted)
	assert.Equal(t, []string{"acme/api#PR-141", "acme/web#PR-9"}, keysOf(refs))
	_, extracted, err = closingPRKeys(json.RawMessage(`{"number":1}`), "acme/web")
	require.NoError(t, err)
	assert.False(t, extracted)
}

func TestLinearPRKeys(t *testing.T) {
	tests := []struct {
		name, payload        string
		want                 []string
		extracted, truncated bool
	}{
		{"github pull request", `{"attachments":{"nodes":[{"sourceType":"github","url":"https://github.com/acme/web/pull/42"}]}}`, []string{"acme/web#PR-42"}, true, false},
		{"scheme and host in any case", `{"attachments":{"nodes":[{"sourceType":"github","url":"HTTPS://GitHub.com/acme/web/pull/42"}]}}`, []string{"acme/web#PR-42"}, true, false},
		{"next page reported", `{"attachments":{"nodes":[{"sourceType":"github","url":"https://github.com/acme/web/pull/1"}],"pageInfo":{"hasNextPage":true}}}`, []string{"acme/web#PR-1"}, true, true},
		{"trailing path", `{"attachments":{"nodes":[{"sourceType":"github","url":"https://github.com/acme/web/pull/42/files"}]}}`, []string{"acme/web#PR-42"}, true, false},
		{"github issue skipped", `{"attachments":{"nodes":[{"sourceType":"github","url":"https://github.com/acme/web/issues/42"}]}}`, []string{}, true, false},
		{"github commit skipped", `{"attachments":{"nodes":[{"sourceType":"github","url":"https://github.com/acme/web/commit/deadbeef"}]}}`, []string{}, true, false},
		{"gitlab merge request", `{"attachments":{"nodes":[{"sourceType":"gitlab","url":"https://gitlab.com/acme/web/-/merge_requests/7"}]}}`, []string{"acme/web#MR-7"}, true, false},
		{"gitlab nested groups", `{"attachments":{"nodes":[{"sourceType":"gitlab","url":"https://gitlab.com/acme/team/web/-/merge_requests/12"}]}}`, []string{"acme/team/web#MR-12"}, true, false},
		{"self-hosted gitlab", `{"attachments":{"nodes":[{"sourceType":"gitlab","url":"https://gitlab.example.com/acme/web/-/merge_requests/3"}]}}`, []string{"acme/web#MR-3"}, true, false},
		{"gitlab issue skipped", `{"attachments":{"nodes":[{"sourceType":"gitlab","url":"https://gitlab.com/acme/web/-/issues/7"}]}}`, []string{}, true, false},
		{"other source skipped", `{"attachments":{"nodes":[{"sourceType":"figma","url":"https://example.com/file/abc"}]}}`, []string{}, true, false},
		{"absent is not extracted", `{"identifier":"ENG-1"}`, []string{}, false, false},
		{"empty is extracted", `{"attachments":{"nodes":[]}}`, []string{}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, extracted, truncated, err := linearPRKeys(json.RawMessage(tt.payload))
			require.NoError(t, err)
			assert.Equal(t, tt.want, keysOf(refs))
			assert.Equal(t, tt.extracted, extracted)
			assert.Equal(t, tt.truncated, truncated)
			for _, r := range refs {
				assert.Equal(t, issuelinkage.VendorLinear, r.Source)
			}
		})
	}
}

func TestGitLabClosesIssues(t *testing.T) {
	tests := []struct {
		name, payload, fallback string
		want                    []string
		extracted               bool
	}{
		{"two issues", `[{"iid":17,"references":{"full":"acme/web#17"}},{"iid":42,"references":{"full":"acme/api#42"}}]`, "acme/web", []string{"acme/web#17", "acme/api#42"}, true},
		{"no references.full falls back", `[{"iid":7}]`, "acme/web", []string{"acme/web#7"}, true},
		{"close across projects", `[{"iid":9,"references":{"full":"acme/team/other#9"}}]`, "acme/web", []string{"acme/team/other#9"}, true},
		{"empty array is an authoritative none", `[]`, "acme/web", []string{}, true},
		{"external tracker items are not authoritative", `[{"id":"PROJ-123","title":"external"}]`, "acme/web", []string{}, false},
		{"iid zero skipped", `[{"iid":0,"references":{"full":"a/b#0"}}]`, "acme/web", []string{}, false},
		{"no path at all skipped", `[{"iid":5}]`, "", []string{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			refs, extracted, err := gitlabClosesIssues(json.RawMessage(tt.payload), tt.fallback)
			require.NoError(t, err)
			assert.Equal(t, tt.want, keysOf(refs))
			assert.Equal(t, tt.extracted, extracted)
		})
	}
}

func TestJiraDescription(t *testing.T) {
	tests := []struct{ name, raw, want string }{
		{"string", `"see PR #42"`, "see PR #42"},
		{"ADF", `{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Closes PROJ-7"}]},{"type":"paragraph","content":[{"type":"text","text":"see also acme/web#PR-42"}]}]}`, "Closes PROJ-7\nsee also acme/web#PR-42"},
		{"null", `null`, ""},
		{"absent", ``, ""},
		{"malformed ADF", `{"type":`, ""},
		{"heading", `{"type":"doc","content":[{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Background"}]},{"type":"paragraph","content":[{"type":"text","text":"PROJ-3 came first"}]}]}`, "## Background\nPROJ-3 came first"},
		{"cards give their URLs", `{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"Earlier attempt: "},{"type":"inlineCard","attrs":{"url":"https://github.com/acme/web/pull/5"}}]},{"type":"blockCard","attrs":{"url":"https://github.com/acme/web/pull/6"}},{"type":"embedCard","attrs":{"url":"https://github.com/acme/web/pull/7"}}]}`, "Earlier attempt: \nhttps://github.com/acme/web/pull/5\nhttps://github.com/acme/web/pull/6\nhttps://github.com/acme/web/pull/7"},
		{"malformed child skipped", `{"type":"doc","content":[1,{"type":"text","text":"kept"}]}`, "kept"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := jiraDescription(json.RawMessage(tt.raw))
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
