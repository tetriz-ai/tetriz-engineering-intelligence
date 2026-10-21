// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name  string
		path  string
		repos map[string]struct{}
		want  string
	}{
		{name: "already relative", path: "server/foo.go", want: "server/foo.go"},
		{name: "bare filename", path: "main.go", want: "main.go"},
		{name: "empty string", path: "", want: ""},
		{name: "whitespace only", path: "   ", want: ""},
		{name: "github module path", path: "github.com/org/repo/server/foo.go", want: "server/foo.go"},
		{name: "gitlab module path", path: "gitlab.com/org/repo/src/bar.go", want: "src/bar.go"},
		{name: "bitbucket module path", path: "bitbucket.org/team/proj/lib/baz.go", want: "lib/baz.go"},
		{name: "absolute path with github prefix", path: "/Users/dev/go/src/github.com/org/repo/server/foo.go", want: "server/foo.go"},
		{name: "leading slash stripped", path: "/server/foo.go", want: "server/foo.go"},
		{name: "leading slashes stripped", path: "///server/foo.go", want: "server/foo.go"},
		{name: "incomplete module path kept as-is", path: "github.com/org/repo", want: "github.com/org/repo"},
		{name: "module path with only org", path: "github.com/org", want: "github.com/org"},
		{name: "whitespace trimmed", path: "  server/foo.go  ", want: "server/foo.go"},
		{name: "backslashes normalized", path: "server\\pkg\\util.go", want: "server/pkg/util.go"},
		{name: "deeply nested module path", path: "github.com/acme/widgets/services/billing/invoice/invoice_test.go", want: "services/billing/invoice/invoice_test.go"},
		{
			name:  "repository segment stripped via knownRepos",
			path:  "handbook/scripts/evals/upload_dataset.py",
			repos: map[string]struct{}{"handbook": {}},
			want:  "scripts/evals/upload_dataset.py",
		},
		{
			name:  "CI workspace plus repository segment stripped",
			path:  "ci-handbook-workspace/handbook/scripts/evals/upload_dataset.py",
			repos: map[string]struct{}{"handbook": {}, "widgets": {}},
			want:  "scripts/evals/upload_dataset.py",
		},
		{
			name:  "workspace plus repo — first repo hit wins",
			path:  "ci-handbook-workspace/widgets/deploy/pipelines/nightly/Pipelinefile",
			repos: map[string]struct{}{"widgets": {}, "handbook": {}},
			want:  "deploy/pipelines/nightly/Pipelinefile",
		},
		{
			name:  "no known repo present — no stripping",
			path:  "unrelated/workspace/scripts/X",
			repos: map[string]struct{}{"handbook": {}},
			want:  "unrelated/workspace/scripts/X",
		},
		{
			name:  "known repo beyond scan window — not stripped",
			path:  "a/b/c/handbook/scripts/X",
			repos: map[string]struct{}{"handbook": {}},
			want:  "a/b/c/handbook/scripts/X",
		},
		{
			name:  "known repo is the last segment — keep path as-is",
			path:  "docs/handbook",
			repos: map[string]struct{}{"handbook": {}},
			want:  "docs/handbook",
		},
		{
			name:  "shortened github module path (org segment elided) stripped via knownRepos",
			path:  "github.com/widgets/services/billing/reporting.go",
			repos: map[string]struct{}{"widgets": {}},
			want:  "services/billing/reporting.go",
		},
		{
			name:  "shortened github module path without knownRepos falls back to the 2-segment strip",
			path:  "github.com/widgets/server/foo.go",
			repos: nil,
			want:  "foo.go",
		},
		{
			name:  "standard github module path with knownRepos still strips through repo",
			path:  "github.com/acme/widgets/server/foo.go",
			repos: map[string]struct{}{"widgets": {}},
			want:  "server/foo.go",
		},

		// Ephemeral-path filter: these should all collapse to "" so they
		// contribute to neither numerator nor denominator of Containment.
		{name: "os temp absolute", path: "/tmp/pr-body.md", want: ""},
		{name: "mac user temp", path: "/var/folders/99/x/T/TemporaryItems/NSIRD_screencaptureui/shot.png", want: ""},
		{name: "mac private temp", path: "/private/tmp/scratch/foo.ts", want: ""},
		{name: "screenshot png outside tmp", path: "docs/assets/overview.png", want: ""},
		{name: "agent tool-results output", path: "-Users-jdoe/1bcb9b2d/tool-results/by7j3fl09.txt", want: ""},
		{name: "trailing slash is a directory", path: "services/billing/", want: ""},
		// Extensionless paths are kept: a directory cannot be told from a real
		// extensionless source file (Dockerfile, Makefile, docs/handbook). Keeping
		// directory arguments out of files_edited is the producer's job.
		{name: "root Dockerfile kept", path: "Dockerfile", want: "Dockerfile"},
		{name: "nested Dockerfile kept", path: "deploy/release/Dockerfile", want: "deploy/release/Dockerfile"},
		{name: "Makefile kept", path: "Makefile", want: "Makefile"},
		{name: "extensionless file in docs kept", path: "docs/scripts/build", want: "docs/scripts/build"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, normalizePath(tt.path, tt.repos))
		})
	}
}

func TestScoreFileOverlap(t *testing.T) {
	tests := []struct {
		name         string
		sessionFiles []string
		prFiles      []string
		repos        map[string]struct{}
		want         float64
	}{
		{
			name:         "exact match",
			sessionFiles: []string{"src/handler.go", "src/handler_test.go"},
			prFiles:      []string{"src/handler.go", "src/handler_test.go"},
			want:         1.0,
		},
		{
			name:         "partial overlap",
			sessionFiles: []string{"src/handler.go", "src/handler_test.go"},
			prFiles:      []string{"src/handler.go", "README.md"},
			want:         0.5, // Containment: |∩| / |session| = 1/2
		},
		{
			name:         "no overlap",
			sessionFiles: []string{"src/handler.go"},
			prFiles:      []string{"README.md"},
			want:         0.0,
		},
		{
			name:         "empty session files",
			sessionFiles: nil,
			prFiles:      []string{"README.md"},
			want:         0.0,
		},
		{
			name:         "empty pr files",
			sessionFiles: []string{"src/handler.go"},
			prFiles:      nil,
			want:         0.0,
		},
		{
			name:         "both empty",
			sessionFiles: nil,
			prFiles:      nil,
			want:         0.0,
		},
		{
			name:         "duplicate files in session are deduplicated",
			sessionFiles: []string{"src/handler.go", "src/handler.go"},
			prFiles:      []string{"src/handler.go"},
			want:         1.0,
		},
		{
			name:         "case sensitive paths",
			sessionFiles: []string{"src/Handler.go"},
			prFiles:      []string{"src/handler.go"},
			want:         0.0,
		},
		{
			name:         "session files with Go module prefix match PR repo-relative paths",
			sessionFiles: []string{"github.com/acme/widgets/server/foo.go", "github.com/acme/widgets/web/bar.ts"},
			prFiles:      []string{"server/foo.go", "README.md"},
			want:         0.5, // Containment: 1 overlap / 2 session files
		},
		{
			name:         "mixed prefixed and bare paths",
			sessionFiles: []string{"github.com/org/repo/src/a.go", "src/b.go"},
			prFiles:      []string{"src/a.go", "src/b.go"},
			want:         1.0,
		},
		{
			name: "CI workspace plus repository prefix matches via knownRepos",
			sessionFiles: []string{
				"ci-handbook-workspace/handbook/scripts/evals/upload_dataset.py",
				"ci-handbook-workspace/handbook/datasets/evals/labels.csv",
			},
			prFiles: []string{
				"scripts/evals/upload_dataset.py",
				"datasets/evals/labels.csv",
			},
			repos: map[string]struct{}{"handbook": {}, "widgets": {}},
			want:  1.0,
		},
		{
			name:         "repository prefix matches via knownRepos",
			sessionFiles: []string{"handbook/scripts/evals/upload_dataset.py"},
			prFiles:      []string{"scripts/evals/upload_dataset.py"},
			repos:        map[string]struct{}{"handbook": {}},
			want:         1.0,
		},
		{
			name:         "small session into large PR — containment succeeds where Jaccard fails",
			sessionFiles: []string{"a.go", "b.go", "c.go", "d.go"},
			prFiles: []string{
				"a.go", "b.go", "x.go", "y.go", "z.go",
				"m.go", "n.go", "o.go", "p.go", "q.go",
				"r.go", "s.go", "t.go", "u.go",
			},
			// Jaccard would be 2/(4+14-2)=0.125 (fails 30% threshold).
			// Containment is 2/4 = 0.5 (passes).
			want: 0.5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ScoreFileOverlap(tt.sessionFiles, tt.prFiles, tt.repos)
			assert.InDelta(t, tt.want, got, 0.001)
		})
	}
}

func TestMatchedFiles(t *testing.T) {
	tests := []struct {
		name         string
		sessionFiles []string
		prFiles      []string
		repos        map[string]struct{}
		want         []string
	}{
		{
			name:         "returns intersection",
			sessionFiles: []string{"a.go", "b.go", "c.go"},
			prFiles:      []string{"b.go", "c.go", "d.go"},
			want:         []string{"b.go", "c.go"},
		},
		{
			name:         "no overlap returns nil",
			sessionFiles: []string{"a.go"},
			prFiles:      []string{"b.go"},
			want:         nil,
		},
		{
			name:         "empty inputs return nil",
			sessionFiles: nil,
			prFiles:      []string{"a.go"},
			want:         nil,
		},
		{
			name:         "Go module prefix stripped for matching",
			sessionFiles: []string{"github.com/acme/widgets/server/handler.go", "github.com/acme/widgets/web/app.ts"},
			prFiles:      []string{"server/handler.go", "docs/README.md"},
			want:         []string{"server/handler.go"},
		},
		{
			name:         "workspace plus repo prefix stripped via knownRepos",
			sessionFiles: []string{"ci-handbook-workspace/handbook/scripts/X.py"},
			prFiles:      []string{"scripts/X.py"},
			repos:        map[string]struct{}{"handbook": {}},
			want:         []string{"scripts/X.py"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchedFiles(tt.sessionFiles, tt.prFiles, tt.repos)
			assert.ElementsMatch(t, tt.want, got)
		})
	}
}
