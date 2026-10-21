// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import "strings"

// maxRepoSegmentScan bounds the lookup for a known-repo segment in session
// paths. Repo names appear near the root of session paths (a CI workspace
// name at index 0, then the repo at index 1 at most). Scanning deeper risks
// matching mid-path references like "docs/repo-b/setup.md" and over-stripping.
const maxRepoSegmentScan = 3

// ScoreFileOverlap returns the containment of a session's edited files in a
// PR's changed files: |intersection| / |session|, the share of the session's
// work that landed in this PR. Returns 0 if either set is empty. Paths are
// normalized to repo-relative form before comparison so that session paths
// (e.g. "github.com/org/repo/server/foo.go") match PR paths (e.g.
// "server/foo.go").
func ScoreFileOverlap(sessionFiles, prFiles []string, knownRepos map[string]struct{}) float64 {
	intersection, sessionSize, _ := overlapCounts(sessionFiles, prFiles, knownRepos)
	return containmentFromCounts(intersection, sessionSize)
}

// overlapCounts returns the two normalized set sizes and their intersection,
// the numbers containmentFromCounts and jaccardFromCounts derive from, so
// normalization is defined once.
func overlapCounts(sessionFiles, prFiles []string, knownRepos map[string]struct{}) (intersection, sessionSize, prSize int) {
	if len(sessionFiles) == 0 || len(prFiles) == 0 {
		return 0, 0, 0
	}

	sessionSet := normalizePathSet(sessionFiles, knownRepos)
	prSet := normalizePathSet(prFiles, knownRepos)

	for f := range sessionSet {
		if _, ok := prSet[f]; ok {
			intersection++
		}
	}
	return intersection, len(sessionSet), len(prSet)
}

// containmentFromCounts is |A∩B| / |A|: the fraction of the session that landed
// in this PR. Zero when the session normalized to nothing.
func containmentFromCounts(intersection, sessionSize int) float64 {
	if sessionSize == 0 {
		return 0
	}
	return float64(intersection) / float64(sessionSize)
}

// jaccardFromCounts is |A∩B| / |A∪B| over the same normalized sets. The Jaccard
// shadow (see matchFileOverlap) counts the pairs it would admit. It never makes
// a match.
func jaccardFromCounts(intersection, sessionSize, prSize int) float64 {
	union := sessionSize + prSize - intersection
	if union <= 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// MatchedFiles returns the paths present in both sets, in their normalized
// repo-relative form (see normalizePath), in no fixed order.
func MatchedFiles(sessionFiles, prFiles []string, knownRepos map[string]struct{}) []string {
	if len(sessionFiles) == 0 || len(prFiles) == 0 {
		return nil
	}

	prSet := normalizePathSet(prFiles, knownRepos)
	sessionNorm := normalizePathSet(sessionFiles, knownRepos)

	var matched []string
	for f := range sessionNorm {
		if _, ok := prSet[f]; ok {
			matched = append(matched, f)
		}
	}
	return matched
}

// normalizePathSet builds a set of normalized file paths.
// Strips VCS module prefixes (e.g. "github.com/org/repo/"), leading slashes,
// and known-repo workspace prefixes so that session paths match repo-relative
// PR paths.
func normalizePathSet(paths []string, knownRepos map[string]struct{}) map[string]struct{} {
	set := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		np := normalizePath(p, knownRepos)
		if np == "" {
			continue // dropped by ephemeral/directory filter
		}
		set[np] = struct{}{}
	}
	return set
}

// normalizePath converts a file path to repo-relative form for comparison.
// Handles:
//   - "github.com/org/repo/server/foo.go"                    → "server/foo.go"
//   - "/Users/dev/go/src/github.com/org/repo/server/foo.go"  → "server/foo.go"
//   - "gitlab.com/org/repo/src/bar.go"                       → "src/bar.go"
//   - "/server/foo.go"                                       → "server/foo.go"
//   - "server/foo.go"                                        → "server/foo.go" (no-op)
//
// When knownRepos is non-empty, also handles workspace prefixes and
// shortened module paths by stripping everything through the first known-repo
// segment found within the first maxRepoSegmentScan path components:
//   - "handbook/scripts/X"                        → "scripts/X"     (repos: {handbook})
//   - "ci-handbook-workspace/handbook/scripts/X"  → "scripts/X"     (repos: {handbook})
//   - "ci-handbook-workspace/widgets/deploy/ci/X" → "deploy/ci/X"   (repos: {widgets})
//   - "github.com/widgets/server/foo.go"          → "server/foo.go" (repos: {widgets}; org segment elided)
//
// Returns "" for ephemeral paths (OS temp, screenshots, agent tool-results,
// binary/image assets, directories). normalizePathSet drops empty strings
// from the resulting set so these paths contribute neither to the numerator
// (via PR intersection) nor the denominator (session size).
func normalizePath(p string, knownRepos map[string]struct{}) string {
	p = strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if p == "" {
		return p
	}
	if isEphemeralPath(p) {
		return ""
	}

	// Strip VCS hosting prefix. When knownRepos is available we prefer it for
	// the tail-strip step so shortened "host/repo/..." paths (missing the org
	// segment, as some session extractors emit them) normalize correctly;
	// otherwise fall back to the plain "host/org/repo/..." 2-segment strip.
	for _, host := range []string{"github.com/", "gitlab.com/", "bitbucket.org/"} {
		if idx := strings.Index(p, host); idx >= 0 {
			tail := p[idx+len(host):]
			if stripped, ok := stripRepoPrefix(tail, knownRepos); ok {
				return stripped
			}
			parts := strings.SplitN(tail, "/", 3)
			if len(parts) == 3 && parts[2] != "" {
				return parts[2]
			}
			return p // incomplete module path — keep as-is
		}
	}

	p = strings.TrimLeft(p, "/")
	if stripped, ok := stripRepoPrefix(p, knownRepos); ok {
		return stripped
	}
	return p
}

// Ephemeral-path match kinds. Used by ephemeralPatterns so every noise rule
// can live in a single table rather than one table per string operation.
type ephemeralMatchKind int

const (
	ephemeralPrefix    ephemeralMatchKind = iota // strings.HasPrefix, case kept
	ephemeralSubstring                           // strings.Contains, case kept
	ephemeralSuffix                              // strings.HasSuffix against lowercased
)

// ephemeralPatterns holds every "drop from attribution" rule. We filter only
// unambiguous noise: OS temp dirs (agents write scratch files there), macOS
// screenshot and tool-results machinery, and binary/image extensions.
// Extensionless paths are intentionally NOT filtered here: legitimate source
// files like Dockerfile, Makefile, scripts/build and docs/handbook would be
// false positives. Keeping directory arguments out of files_edited is the
// producer's job.
var ephemeralPatterns = []struct {
	kind ephemeralMatchKind
	pat  string
}{
	{ephemeralPrefix, "tmp/"},
	{ephemeralPrefix, "/tmp/"},
	{ephemeralPrefix, "var/folders/"},
	{ephemeralPrefix, "/var/folders/"},
	{ephemeralPrefix, "private/tmp/"},
	{ephemeralPrefix, "/private/tmp/"},
	{ephemeralPrefix, "private/var/"},
	{ephemeralPrefix, "/private/var/"},

	{ephemeralSubstring, "/TemporaryItems/"},
	{ephemeralSubstring, "/tool-results/"},

	{ephemeralSuffix, ".png"},
	{ephemeralSuffix, ".jpg"},
	{ephemeralSuffix, ".jpeg"},
	{ephemeralSuffix, ".gif"},
	{ephemeralSuffix, ".svg"},
	{ephemeralSuffix, ".ico"},
	{ephemeralSuffix, ".webp"},
	{ephemeralSuffix, ".pdf"},
	{ephemeralSuffix, ".mov"},
	{ephemeralSuffix, ".mp4"},
	{ephemeralSuffix, ".zip"},
	{ephemeralSuffix, ".tar"},
	{ephemeralSuffix, ".gz"},
	{ephemeralSuffix, ".tgz"},
}

// isEphemeralPath reports whether p should be dropped from attribution. A
// trailing slash is the one structural directory signal we trust; everything
// else is table-driven via ephemeralPatterns.
func isEphemeralPath(p string) bool {
	if strings.HasSuffix(p, "/") {
		return true
	}
	lower := strings.ToLower(p)
	for _, c := range ephemeralPatterns {
		switch c.kind {
		case ephemeralPrefix:
			if strings.HasPrefix(p, c.pat) {
				return true
			}
		case ephemeralSubstring:
			if strings.Contains(p, c.pat) {
				return true
			}
		case ephemeralSuffix:
			if strings.HasSuffix(lower, c.pat) {
				return true
			}
		}
	}
	return false
}

// stripRepoPrefix strips everything up to and including the first known-repo
// segment found within the first maxRepoSegmentScan components. Returns
// (path, false) if no known-repo segment applies — callers decide the fallback.
// The scan window bounds how deep we look into the path so mid-path references
// like "docs/repo-b/setup.md" don't get over-stripped.
func stripRepoPrefix(p string, knownRepos map[string]struct{}) (string, bool) {
	if len(knownRepos) == 0 || p == "" {
		return p, false
	}
	segs := strings.Split(p, "/")
	scanUntil := min(maxRepoSegmentScan, len(segs))
	for i := range scanUntil {
		if _, ok := knownRepos[segs[i]]; ok && i+1 < len(segs) {
			return strings.Join(segs[i+1:], "/"), true
		}
	}
	return p, false
}
