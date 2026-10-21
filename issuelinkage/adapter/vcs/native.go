// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package vcs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
)

// closingIssueKeys reads GitHub's closingIssuesReferences off a pull-request
// payload: one "owner/repo#N" key per issue the pull request closes on merge.
// A node without a repository takes fallbackRepo; a node with neither, or
// with number 0, is skipped. extracted is false when the field is absent.
func closingIssueKeys(payload json.RawMessage, fallbackRepo string) (keys []issuelinkage.NativeRef, extracted bool, err error) {
	if len(payload) == 0 {
		return nil, false, nil
	}
	var p struct {
		Refs *struct {
			Nodes []numberedNode `json:"nodes"`
		} `json:"closingIssuesReferences"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, false, fmt.Errorf("parsing closingIssuesReferences: %w", err)
	}
	if p.Refs == nil {
		return nil, false, nil
	}
	return numberedKeys(p.Refs.Nodes, fallbackRepo, "#%d"), true, nil
}

// closingPRKeys reads GitHub's closedByPullRequestsReferences off an issue
// payload: one "owner/repo#PR-N" key per pull request GitHub reports as
// closing the issue.
func closingPRKeys(payload json.RawMessage, fallbackRepo string) (keys []issuelinkage.NativeRef, extracted bool, err error) {
	if len(payload) == 0 {
		return nil, false, nil
	}
	var p struct {
		Refs *struct {
			Nodes []numberedNode `json:"nodes"`
		} `json:"closedByPullRequestsReferences"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, false, fmt.Errorf("parsing closedByPullRequestsReferences: %w", err)
	}
	if p.Refs == nil {
		return nil, false, nil
	}
	return numberedKeys(p.Refs.Nodes, fallbackRepo, "#PR-%d"), true, nil
}

type numberedNode struct {
	Number     int `json:"number"`
	Repository struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
}

func numberedKeys(nodes []numberedNode, fallbackRepo, suffix string) []issuelinkage.NativeRef {
	out := make([]issuelinkage.NativeRef, 0, len(nodes))
	for _, n := range nodes {
		repo := n.Repository.NameWithOwner
		if repo == "" {
			repo = fallbackRepo
		}
		if repo == "" || n.Number == 0 {
			continue
		}
		out = append(out, issuelinkage.NativeRef{Key: repo + fmt.Sprintf(suffix, n.Number), Source: issuelinkage.VendorGitHub})
	}
	return out
}

var (
	// githubPRURLPattern matches a github.com pull-request URL. It is not
	// end-anchored, so a trailing path or fragment still matches; scheme and
	// host match in any case.
	githubPRURLPattern = regexp.MustCompile(`^(?i:https?://github\.com/)([\w.-]+)/([\w.-]+)/pull/(\d+)`)
	// gitlabMRURLPattern matches a merge-request URL on any host: the
	// attachment's source type already says it is GitLab, and the path, not
	// the host, is the repository.
	gitlabMRURLPattern = regexp.MustCompile(`^(?i:https?://[^/]+/)(.+?)/-/merge_requests/(\d+)`)
)

// linearPRKeys reads a Linear issue's attachments: one key per GitHub pull
// request ("owner/repo#PR-N") or GitLab merge request ("path#MR-N") attached.
// truncated is set when Linear returned only the first page.
func linearPRKeys(payload json.RawMessage) (keys []issuelinkage.NativeRef, extracted, truncated bool, err error) {
	if len(payload) == 0 {
		return nil, false, false, nil
	}
	var p struct {
		Attachments *struct {
			Nodes []struct {
				URL        string `json:"url"`
				SourceType string `json:"sourceType"`
			} `json:"nodes"`
			PageInfo struct {
				HasNextPage bool `json:"hasNextPage"`
			} `json:"pageInfo"`
		} `json:"attachments"`
	}
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, false, false, fmt.Errorf("parsing attachments: %w", err)
	}
	if p.Attachments == nil {
		return nil, false, false, nil
	}
	out := make([]issuelinkage.NativeRef, 0, len(p.Attachments.Nodes))
	for _, n := range p.Attachments.Nodes {
		if key := attachmentPRKey(n.SourceType, n.URL); key != "" {
			out = append(out, issuelinkage.NativeRef{Key: key, Source: issuelinkage.VendorLinear})
		}
	}
	return out, true, p.Attachments.PageInfo.HasNextPage, nil
}

// attachmentPRKey maps one attachment to a pull-request key, or "" when it
// is not a pull request. The URL shape decides: a GitHub attachment can also
// be an issue, a commit or a comment.
func attachmentPRKey(sourceType, rawURL string) string {
	switch sourceType {
	case "github":
		if m := githubPRURLPattern.FindStringSubmatch(rawURL); m != nil {
			return fmt.Sprintf("%s/%s#PR-%s", m[1], m[2], m[3])
		}
	case "gitlab":
		if m := gitlabMRURLPattern.FindStringSubmatch(rawURL); m != nil {
			return fmt.Sprintf("%s#MR-%s", m[1], m[2])
		}
	}
	return ""
}

// gitlabClosesIssues reads GitLab's closes_issues array: one "path#iid" key
// per issue the merge request closes. The path comes from references.full,
// which is right for a close across projects; without it, fallbackPath. An
// item with no iid, or no path, is skipped. extracted is false when items
// were present but none gave a key, such as an external tracker's issues.
func gitlabClosesIssues(raw json.RawMessage, fallbackPath string) (keys []issuelinkage.NativeRef, extracted bool, err error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	var items []struct {
		IID        int `json:"iid"`
		References struct {
			Full string `json:"full"`
		} `json:"references"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, false, fmt.Errorf("parsing closes_issues: %w", err)
	}
	out := make([]issuelinkage.NativeRef, 0, len(items))
	for _, it := range items {
		if it.IID == 0 {
			continue
		}
		path := fallbackPath
		if i := strings.IndexByte(it.References.Full, '#'); i > 0 {
			path = it.References.Full[:i]
		}
		if path == "" {
			continue
		}
		out = append(out, issuelinkage.NativeRef{Key: fmt.Sprintf("%s#%d", path, it.IID), Source: issuelinkage.VendorGitLab})
	}
	return out, len(items) == 0 || len(out) > 0, nil
}

// adfNode is one node of an Atlassian Document Format tree, as far as text
// extraction needs it.
type adfNode struct {
	Type    string            `json:"type"`
	Text    string            `json:"text"`
	Attrs   adfAttrs          `json:"attrs"`
	Content []json.RawMessage `json:"content"`
}

type adfAttrs struct {
	URL string `json:"url"`
}

// flattenADF returns an ADF document as text, one line per text leaf: a
// heading becomes a Markdown heading ("## " and its text), and an inline,
// block or embedded card becomes its URL. A malformed document gives "", and
// a malformed child is skipped.
func flattenADF(raw json.RawMessage) string {
	var root adfNode
	if err := json.Unmarshal(raw, &root); err != nil {
		return ""
	}
	var b strings.Builder
	walkADF(&root, &b)
	return b.String()
}

func line(b *strings.Builder, s string) {
	if s == "" {
		return
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(s)
}

func walkADF(n *adfNode, b *strings.Builder) {
	switch n.Type {
	case "text":
		line(b, n.Text)
	case "inlineCard", "blockCard", "embedCard":
		line(b, n.Attrs.URL)
	case "heading":
		var h strings.Builder
		for _, raw := range n.Content {
			var child adfNode
			if json.Unmarshal(raw, &child) == nil {
				h.WriteString(child.Text)
			}
		}
		if t := strings.TrimSpace(h.String()); t != "" {
			line(b, "## "+t)
		}
		return
	}
	for _, raw := range n.Content {
		var child adfNode
		if err := json.Unmarshal(raw, &child); err != nil {
			continue
		}
		walkADF(&child, b)
	}
}

// jiraDescription reads a Jira description: a string (Server and Data
// Center) or an ADF document (Cloud).
func jiraDescription(raw json.RawMessage) (string, error) {
	desc := bytes.TrimSpace(raw)
	if len(desc) == 0 || string(desc) == "null" {
		return "", nil
	}
	if desc[0] == '"' {
		var s string
		if err := json.Unmarshal(desc, &s); err != nil {
			return "", fmt.Errorf("parsing description string: %w", err)
		}
		return s, nil
	}
	return flattenADF(desc), nil
}
