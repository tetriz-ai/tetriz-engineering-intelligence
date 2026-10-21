// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prworktype

import "testing"

func TestClassifyTypeRule(t *testing.T) {
	tests := []struct {
		name   string
		title  string
		branch string
		author string
		want   string
	}{
		{name: "dependabot author", author: "dependabot[bot]", want: "chore-deps"},
		{name: "renovate author", author: "renovate[bot]", want: "chore-deps"},
		{name: "author case/space insensitive", author: "  Dependabot[bot] ", want: "chore-deps"},
		{name: "cherry-pick in title", title: "Cherry-pick #42 to release", want: CherryPick},
		{name: "cherrypick in branch", branch: "cherrypick/hotfix", want: CherryPick},
		{name: "ordinary PR escalates", title: "add login page", branch: "feat/login", author: "alice", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyTypeRule(tc.title, tc.branch, tc.author); got != tc.want {
				t.Fatalf("classifyTypeRule(%q,%q,%q) = %q, want %q", tc.title, tc.branch, tc.author, got, tc.want)
			}
		})
	}
}

func TestValidBucket(t *testing.T) {
	tests := []struct {
		axis, bucket string
		want         bool
	}{
		{"type", "bugfix", true},
		{"type", "chore-deps", true},
		{"type", Uncategorised, true},
		{"type", CherryPick, true},
		{"type", "infra-devops", false}, // a domain bucket is not a type
		{"type", "nonsense", false},
		{"domain", "ai-ml", true},
		{"domain", "security-compliance", true},
		{"domain", "bugfix", false}, // a type bucket is not a domain
		{"domain", Uncategorised, false},
		{"other", "bugfix", false},
	}
	for _, tc := range tests {
		t.Run(tc.axis+"/"+tc.bucket, func(t *testing.T) {
			if got := validBucket(tc.axis, tc.bucket); got != tc.want {
				t.Fatalf("validBucket(%q,%q) = %v, want %v", tc.axis, tc.bucket, got, tc.want)
			}
		})
	}
}
