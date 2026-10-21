// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package diffsize

import "testing"

func TestMatchAny(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		patterns []string
		want     bool
	}{
		{name: "empty patterns", path: "main.go", patterns: nil, want: false},
		{name: "empty path", path: "", patterns: []string{"*.go"}, want: false},
		{name: "basename match for codegen", path: "gen/foo.pb.go", patterns: []string{"*.pb.go"}, want: true},
		{name: "single-segment dir glob does not match deeper files", path: "vendor/x/y.go", patterns: []string{"vendor/*"}, want: false},
		{name: "wire_gen match basename", path: "internal/wire_gen.go", patterns: []string{"wire_gen.go"}, want: true},
		{name: "non-match", path: "src/main.go", patterns: []string{"*.pb.go", "wire_gen.go"}, want: false},
		{name: "multiple patterns one hits", path: "go.sum", patterns: []string{"*.lock", "go.sum"}, want: true},
		{name: "skips empty pattern entry", path: "a.go", patterns: []string{"", "*.go"}, want: true},
		{name: "invalid pattern is skipped", path: "a.go", patterns: []string{"["}, want: false},
		{name: "slash pattern not matched against basename", path: "wire_gen.go", patterns: []string{"internal/wire_gen.go"}, want: false},
		{name: "doublestar suffix glob matches at depth", path: "gen/deep/foo.pb.go", patterns: []string{"**/*.pb.go"}, want: true},
		{name: "doublestar prefix glob matches anything under dir", path: "gen/deep/foo.go", patterns: []string{"gen/**"}, want: true},
		{name: "doublestar prefix does not match outside dir", path: "other/foo.go", patterns: []string{"gen/**"}, want: false},
		{name: "single star within segment does not cross separator", path: "vendor/x/y.go", patterns: []string{"vendor/*.go"}, want: false},
		{name: "a slash inside a brace alternative skips the basename fallback", path: "dir/x.pb.go", patterns: []string{"{gen/**,*.pb.go}"}, want: false},
		{name: "the same alternative without a slash gets the fallback", path: "dir/x.pb.go", patterns: []string{"{*.pb.go,*.gen.go}"}, want: true},
		{name: "documented lock globs: package-lock.json at depth", path: "web/package-lock.json", patterns: []string{"*.pb.go", "go.sum", "package-lock.json", "*.lock"}, want: true},
		{name: "documented lock globs: yarn.lock by *.lock", path: "yarn.lock", patterns: []string{"*.pb.go", "go.sum", "package-lock.json", "*.lock"}, want: true},
		{name: "documented lock globs: Cargo.lock by *.lock", path: "Cargo.lock", patterns: []string{"*.pb.go", "go.sum", "package-lock.json", "*.lock"}, want: true},
		{name: "documented lock globs: poetry.lock at depth", path: "py/poetry.lock", patterns: []string{"*.pb.go", "go.sum", "package-lock.json", "*.lock"}, want: true},
		{name: "documented lock globs: go.sum", path: "go.sum", patterns: []string{"*.pb.go", "go.sum", "package-lock.json", "*.lock"}, want: true},
		{name: "documented lock globs: clock.go is not a lock file", path: "clock.go", patterns: []string{"*.pb.go", "go.sum", "package-lock.json", "*.lock"}, want: false},
		{name: "documented lock globs: block.go is not a lock file", path: "internal/block.go", patterns: []string{"*.pb.go", "go.sum", "package-lock.json", "*.lock"}, want: false},
		{name: "documented lock globs: deadlock.go is not a lock file", path: "sync/deadlock.go", patterns: []string{"*.pb.go", "go.sum", "package-lock.json", "*.lock"}, want: false},
		{name: "documented lock globs: lockfile.go is not a lock file", path: "lockfile.go", patterns: []string{"*.pb.go", "go.sum", "package-lock.json", "*.lock"}, want: false},
		{name: "the *lock* glob this replaces matches clock.go", path: "clock.go", patterns: []string{"*lock*"}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchAny(tt.path, tt.patterns); got != tt.want {
				t.Fatalf("MatchAny(%q, %v) = %v, want %v", tt.path, tt.patterns, got, tt.want)
			}
		})
	}
}

func TestPatternsForRepo(t *testing.T) {
	org := []string{"*.pb.go"}
	overrides := map[string][]string{
		"example/web":   {"*.gen.ts", "node_modules/*"},
		"example/empty": {},
	}

	tests := []struct {
		name string
		repo string
		want []string
	}{
		{name: "no override falls through to org", repo: "example/api", want: org},
		{name: "override replaces org", repo: "example/web", want: overrides["example/web"]},
		{name: "empty repo falls through", repo: "", want: org},
		{name: "an empty override clears the org patterns", repo: "example/empty", want: []string{}},
		{name: "the repo name is matched exactly", repo: "Example/Web", want: org},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PatternsForRepo(tt.repo, org, overrides)
			if len(got) != len(tt.want) {
				t.Fatalf("PatternsForRepo(%q) len = %d, want %d", tt.repo, len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("PatternsForRepo(%q)[%d] = %q, want %q", tt.repo, i, got[i], tt.want[i])
				}
			}
		})
	}
}
