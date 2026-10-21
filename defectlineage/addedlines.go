// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage

import "strings"

// AddedLine is a line a diff added, with its 1-based number in the new file.
type AddedLine struct {
	Text    string
	NewLine int
}

// Added lines are the other half of the anchor parser, and they exist for one
// job: recovering which line a culprit commit actually changed.
//
// A provider's blame reports a line's position at the revision it was queried
// about, and that position drifts from the culprit's own revision. The
// structural comparison needs the culprit-revision line, so when blame cannot
// supply it the line is read from the culprit's own diff instead — the added
// lines of that commit are the lines it wrote.

// AddedLines extracts the added lines of a unified diff scoped to one file.
//
// It reads line numbers from each hunk header and counts the body down, the same
// way the anchor parser does, so a line whose own content begins with "++" is
// content rather than a mistaken file header.
func AddedLines(raw []byte) []AddedLine {
	return addedLines(string(raw), "")
}

// AddedLinesForPath extracts the added lines of newPath from a multi-file diff.
// A path that does not appear yields none.
func AddedLinesForPath(raw []byte, newPath string) []AddedLine {
	if newPath == "" {
		return nil
	}
	return addedLines(string(raw), newPath)
}

// addedLines walks a unified patch. When wantPath is empty every file's added
// lines are returned, which is the single-file case; otherwise only that path's.
func addedLines(raw, wantPath string) []AddedLine {
	var (
		out      []AddedLine
		inWanted = wantPath == ""
		newLine  int
		remOld   int
		remNew   int
	)
	sc := newDiffScanner(raw)
	for sc.Scan() {
		line := sc.Text()

		inBody := remOld > 0 || remNew > 0
		if inBody && !endsBodyEarly(line) {
			switch {
			case strings.HasPrefix(line, `\`):
				// "\ No newline at end of file" occupies no position.
			case strings.HasPrefix(line, "+"):
				if inWanted {
					out = append(out, AddedLine{Text: strings.TrimPrefix(line, "+"), NewLine: newLine})
				}
				newLine++
				remNew--
			case strings.HasPrefix(line, "-"):
				remOld--
			default:
				newLine++
				remOld--
				remNew--
			}
			continue
		}
		if inBody {
			// The body ended before its header said. Resynchronise on structure.
			remOld, remNew = 0, 0
		}

		switch {
		case strings.HasPrefix(line, "diff --git "):
			// A new file section. Until its own header names the path, a
			// path-scoped read is outside the file it wants.
			inWanted = wantPath == ""
		case strings.HasPrefix(line, "+++ "):
			inWanted = wantPath == "" ||
				trimDiffPath(strings.TrimPrefix(line, "+++ "), "b/") == wantPath
		case strings.HasPrefix(line, "@@"):
			start, oldCount, newCount, ok := parseAddedHeader(line)
			if !ok {
				remOld, remNew = 0, 0
				continue
			}
			newLine, remOld, remNew = start, oldCount, newCount
		}
	}
	return out
}

// parseAddedHeader reads a hunk header's new-side start and both side counts.
// The count is optional — git omits ",count" when it is 1 — so an absent count
// is one line, not zero.
func parseAddedHeader(line string) (newStart, oldCount, newCount int, ok bool) {
	minus := strings.Index(line, "-")
	plus := strings.Index(line, "+")
	if minus < 0 || plus < 0 || plus < minus {
		return 0, 0, 0, false
	}
	oldRange := line[minus+1 : plus]
	if i := strings.IndexAny(oldRange, " \t"); i >= 0 {
		oldRange = oldRange[:i]
	}
	newRange := line[plus+1:]
	if i := strings.IndexAny(newRange, " \t"); i >= 0 {
		newRange = newRange[:i]
	}
	_, oldCount, okOld := parseRange(oldRange, nopObserver{})
	newStart, newCount, okNew := parseRange(newRange, nopObserver{})
	if !okOld || !okNew {
		return 0, 0, 0, false
	}
	return newStart, oldCount, newCount, true
}
