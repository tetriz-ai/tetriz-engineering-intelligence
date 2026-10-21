// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package diffsize turns a pull request's diff into effective lines of code:
// per-language added source, comment and blank lines and files changed, using
// go-enry for language and generated/vendored classification and scc for line
// counting. It reads git unified diffs and GitLab's merge-request diffs JSON.
package diffsize

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrDiffExcluded is returned, wrapped, by AnalyzeDiff and
// AnalyzeDiffWithExcludes when GitLab left a file's diff out of the response
// and no exclude pattern matches the file's path. The error names the file (or "entry <n>" when GitLab sent no path) and the
// flag. On "too_large" the diff cannot be retrieved, so score the PR as
// prcomplexity's DiffTooLarge. On "collapsed", fetch that file's diff from
// GitLab and analyse again.
var ErrDiffExcluded = errors.New("diffsize: GitLab excluded a file's diff")

// FileDiff represents a single file extracted from a raw diff. NewContent
// is the reconstructed content of added lines (each line already newline-
// terminated) suitable for language classification and scc counting.
type FileDiff struct {
	Path       string
	NewContent string
	Additions  int
	Deletions  int
}

// ParseUnifiedDiff parses a raw git unified diff (the output of git diff, and
// what code hosts serve as a pull request's diff) into per-file diffs. It also
// reads GNU diff -u, svn and hg unified diffs, whose header lines put a tab
// and a timestamp or revision after the path.
// Handles:
//   - Multi-file diffs (`diff --git` separators).
//   - New files (`--- /dev/null`).
//   - Deleted files (`+++ /dev/null`) — skipped since they have no new content.
//   - Leading `a/` and `b/` path prefixes stripped, and paths git C-quotes
//     (non-ASCII or special characters, a tab among them) unquoted. An
//     unquoted path ends at the first tab.
//   - Lines of any length.
//
// Inside a hunk, lines are read by the hunk header's line counts, so an added
// line whose content starts with "++" or a removed line whose content starts
// with "--" is counted as content, not taken for a file header. A hunk also
// ends at the first line that cannot be hunk content.
//
// The parser does not attempt to apply hunks; it collects only the added
// lines so that go-enry / scc operate on the post-change content. It does not
// read a combined diff ("diff --cc", from a merge commit), which
// AnalyzeDiff rejects.
func ParseUnifiedDiff(raw []byte) []FileDiff {
	var (
		files   []FileDiff
		current *fileBuilder
		hunk    hunkState
	)

	flush := func() {
		if current != nil {
			files = append(files, current.finish())
		}
	}

	forEachLine(string(raw), func(line string) {
		if hunk.active() && hunk.consume(line, current) {
			return
		}
		switch {
		case strings.HasPrefix(line, "+++ "):
			// A new file header ends the previous file, even when the new
			// one is `+++ /dev/null` (a deleted file).
			flush()
			path := headerPath(strings.TrimPrefix(line, "+++ "))
			if path == "/dev/null" {
				// Deleted file — ignore, no new content to analyze.
				current = nil
				return
			}
			current = &fileBuilder{diff: FileDiff{Path: path}}

		case current == nil:
			// Ignore headers that arrive before the first +++ (diff --git,
			// index, ---, etc.) or content between a /dev/null block and the
			// next file header.

		case strings.HasPrefix(line, "@@"):
			hunk.start(line)

		default:
			current.looseLine(line)
		}
	})

	flush()
	return files
}

// headerPath returns the path of a `+++ ` header line: unquoted when git
// C-quoted it, else cut at the first tab, trimmed, and without its `b/`
// prefix. GNU diff, svn and hg write a tab and a timestamp or revision after
// the path, and git a lone tab after a path with a space. git C-quotes a path
// that holds a tab, so an unquoted path never does.
func headerPath(s string) string {
	path := strings.TrimSpace(s)
	if strings.HasPrefix(path, `"`) {
		if unquoted, err := strconv.Unquote(path); err == nil {
			return strings.TrimPrefix(unquoted, "b/")
		}
	}
	path, _, _ = strings.Cut(path, "\t")
	return strings.TrimPrefix(strings.TrimSpace(path), "b/")
}

// gitLabDiffEntry mirrors a single element of the GitLab
// GET /projects/:id/merge_requests/:iid/diffs JSON response. Only the fields
// the parser reads are declared; unknown fields are ignored.
type gitLabDiffEntry struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	Diff        string `json:"diff"`
	NewFile     bool   `json:"new_file"`
	DeletedFile bool   `json:"deleted_file"`
	RenamedFile bool   `json:"renamed_file"`
	TooLarge    bool   `json:"too_large"`
	Collapsed   bool   `json:"collapsed"`
}

// ParseGitLabDiffs parses the GitLab JSON-array diff format:
//
//	[{"old_path": "...", "new_path": "...", "diff": "@@ -1,3 +1,5 @@\n...", ...}, ...]
//
// The inline `diff` string is a unified-diff body (without file headers) so
// additions / deletions are extracted line-by-line the same way as
// ParseUnifiedDiff. Deleted files (`deleted_file=true`) are skipped, and so
// are entries with no added or removed line (a pure rename, a mode change, a
// binary file), since a unified diff has no hunk for them either.
//
// Returns nil when the JSON fails to parse, when it is null or holds a null
// entry, when an entry that is not deleted has GitLab's "too_large" or
// "collapsed" flag, since its diff is not in the response, and when an entry
// that is not deleted has added or removed lines but no path. AnalyzeDiff and
// AnalyzeDiffWithExcludes report each of these cases as an error, the flags
// as ErrDiffExcluded unless an exclude pattern matches the flagged path.
func ParseGitLabDiffs(raw []byte) []FileDiff {
	files, err := parseGitLabDiffs(raw, nil)
	if err != nil {
		return nil
	}
	return files
}

// parseGitLabDiffs is ParseGitLabDiffs with the error returned. A flagged
// entry whose path matches excludePatterns is returned with no lines, so the
// caller lists it as excluded, instead of failing the diff.
func parseGitLabDiffs(raw []byte, excludePatterns []string) ([]FileDiff, error) {
	var entries []*gitLabDiffEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, fmt.Errorf("parsing GitLab diffs JSON: %w", err)
	}
	if entries == nil {
		return nil, errors.New("parsing GitLab diffs JSON: null, not an array")
	}

	files := make([]FileDiff, 0, len(entries))
	for i, e := range entries {
		if e == nil {
			return nil, fmt.Errorf("parsing GitLab diffs JSON: entry %d is null", i)
		}
		if e.DeletedFile {
			continue
		}
		path := e.NewPath
		if path == "" {
			path = e.OldPath
		}
		// The flags come before the empty-path skip: a left-out diff is
		// reported even when GitLab sends no path for it, unless an exclude
		// pattern drops the file anyway.
		name := strconv.Quote(path)
		if path == "" {
			name = fmt.Sprintf("entry %d", i)
		}
		if (e.TooLarge || e.Collapsed) && MatchAny(path, excludePatterns) {
			files = append(files, FileDiff{Path: path})
			continue
		}
		switch {
		case e.TooLarge:
			return nil, fmt.Errorf("%w: %s is too_large", ErrDiffExcluded, name)
		case e.Collapsed:
			return nil, fmt.Errorf("%w: %s is collapsed", ErrDiffExcluded, name)
		}

		// The embedded `diff` is a unified-diff body — scan the same way
		// as ParseUnifiedDiff but without file headers.
		fb := &fileBuilder{diff: FileDiff{Path: path}}
		var hunk hunkState
		forEachLine(e.Diff, func(line string) {
			if hunk.active() && hunk.consume(line, fb) {
				return
			}
			if strings.HasPrefix(line, "@@") {
				hunk.start(line)
				return
			}
			fb.looseLine(line)
		})

		fd := fb.finish()
		if fd.Additions == 0 && fd.Deletions == 0 {
			continue
		}
		if path == "" {
			// Lines with no file to put them in: a language and a file count
			// cannot be given, so the entry is reported, not dropped.
			return nil, fmt.Errorf("parsing GitLab diffs JSON: %s has changed lines but no new_path or old_path", name)
		}
		files = append(files, fd)
	}
	return files, nil
}

// fileBuilder accumulates one file's added lines and counts.
type fileBuilder struct {
	diff  FileDiff
	added []string
}

func (b *fileBuilder) add(content string) {
	b.added = append(b.added, content)
	b.diff.Additions++
}

func (b *fileBuilder) remove() {
	b.diff.Deletions++
}

// looseLine handles a line outside any hunk. A diff without hunk headers
// still has its `+` and `-` lines counted; `+++` and `---` lines there are
// file headers.
func (b *fileBuilder) looseLine(line string) {
	switch {
	case strings.HasPrefix(line, "+++"), strings.HasPrefix(line, "---"):
		// File header, not content.
	case strings.HasPrefix(line, "+"):
		b.add(line[1:])
	case strings.HasPrefix(line, "-"):
		b.remove()
	}
}

func (b *fileBuilder) finish() FileDiff {
	b.diff.NewContent = strings.Join(b.added, "\n")
	if len(b.added) > 0 {
		b.diff.NewContent += "\n"
	}
	return b.diff
}

// hunkState tracks the old and new line counts left in the current hunk.
type hunkState struct {
	oldLeft, newLeft int
}

func (h *hunkState) active() bool { return h.oldLeft > 0 || h.newLeft > 0 }

// start reads a hunk header, "@@ -a[,b] +c[,d] @@". A header it cannot read
// leaves no hunk active, so the lines after it are read as loose lines.
func (h *hunkState) start(line string) {
	*h = hunkState{}
	fields := strings.Fields(line)
	if len(fields) < 3 || fields[0] != "@@" {
		return
	}
	oldCount, okOld := hunkCount(fields[1], "-")
	newCount, okNew := hunkCount(fields[2], "+")
	if okOld && okNew {
		h.oldLeft, h.newLeft = oldCount, newCount
	}
}

// hunkCount returns the line count of one side of a hunk header: "-a,b" has
// b lines, "-a" has one.
func hunkCount(field, sign string) (int, bool) {
	if !strings.HasPrefix(field, sign) {
		return 0, false
	}
	_, count, found := strings.Cut(field[1:], ",")
	if !found {
		return 1, true
	}
	n, err := strconv.Atoi(count)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// consume reads one line inside a hunk into b and reports whether it was hunk
// content. A line that cannot be hunk content ends the hunk and is not
// consumed. An empty line is a context line whose single space was stripped.
func (h *hunkState) consume(line string, b *fileBuilder) bool {
	switch {
	case strings.HasPrefix(line, "+"):
		if h.newLeft > 0 {
			h.newLeft--
			b.add(line[1:])
			return true
		}
	case strings.HasPrefix(line, "-"):
		if h.oldLeft > 0 {
			h.oldLeft--
			b.remove()
			return true
		}
	case line == "" || strings.HasPrefix(line, " "):
		if h.oldLeft > 0 && h.newLeft > 0 {
			h.oldLeft--
			h.newLeft--
			return true
		}
	case strings.HasPrefix(line, `\`):
		// "\ No newline at end of file" belongs to the hunk and counts no line.
		return true
	}
	*h = hunkState{}
	return false
}

// forEachLine calls fn for each line of s, split as bufio.ScanLines splits
// (one trailing carriage return dropped, no empty line after a final newline)
// but with no limit on line length.
func forEachLine(s string, fn func(string)) {
	for len(s) > 0 {
		line := s
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			line, s = s[:i], s[i+1:]
		} else {
			s = ""
		}
		fn(strings.TrimSuffix(line, "\r"))
	}
}
