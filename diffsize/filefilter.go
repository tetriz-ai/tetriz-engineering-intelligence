// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package diffsize

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// MatchAny reports whether path matches any of the supplied patterns.
//
// Patterns use doublestar.Match syntax: filepath.Match plus `**`, which
// matches across path separators.
//
// Supported forms:
//   - "*.pb.go"                 — basename glob (matches any depth)
//   - "**/*.pb.go"              — recursive glob on suffix
//   - "gen/**"                  — recursive glob on prefix
//   - "vendor/*"                — single-segment path glob
//   - "path/to/specific.file"   — literal path
//
// Patterns are tested against the full path first; if the pattern contains
// no separator, the basename is also tested so short forms like "*.pb.go"
// hit files at any depth without having to write "**/*.pb.go". A separator
// anywhere in the pattern, even inside one brace alternative, turns the
// basename test off for the whole pattern: "{gen/**,*.pb.go}" does not match
// "dir/x.pb.go". Write the alternatives as separate patterns instead.
//
// An empty pattern list or an empty path returns false. Empty and invalid
// patterns are skipped: they match nothing. AnalyzeDiffWithExcludes reports an
// invalid pattern as an error; doublestar.ValidatePattern checks one.
func MatchAny(path string, patterns []string) bool {
	if len(patterns) == 0 || path == "" {
		return false
	}
	base := filepath.Base(path)
	for _, p := range patterns {
		if p == "" {
			continue
		}
		if ok, err := doublestar.Match(p, path); err == nil && ok {
			return true
		}
		// Basename fallback for separator-free globs like "*.pb.go", so they
		// match at any depth without "**/".
		if strings.ContainsAny(p, "/\\") {
			continue
		}
		if ok, err := doublestar.Match(p, base); err == nil && ok {
			return true
		}
	}
	return false
}

// PatternsForRepo returns the effective exclude patterns for a repository.
// If the repository has an entry in overrides, that list is returned: an
// override replaces the org-level patterns rather than adding to them.
// Otherwise orgPatterns is returned. repoFullName is the key overrides uses,
// such as "owner/name"; it is matched exactly.
func PatternsForRepo(repoFullName string, orgPatterns []string, overrides map[string][]string) []string {
	if override, ok := overrides[repoFullName]; ok {
		return override
	}
	return orgPatterns
}
