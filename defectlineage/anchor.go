// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage

import (
	"bufio"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/diffsize"
)

// Anchors turn a bug-fix pull request's diff into the locations a blame walk can
// ask a provider about. The diff is bytes you already hold, so this is a parse,
// not a fetch.
//
// It is a second parser beside [diffsize.ParseUnifiedDiff], which keeps the
// opposite half: that one reconstructs the ADDED lines to classify and count the
// post-change content, and discards the hunk headers, the old line numbers and
// the removed text — which is exactly what a blame target is made of.

// Kind is what a hunk's shape says about whether it can name an origin.
type Kind int

const (
	// KindUnclassified is the zero value, and deliberately not blameable. The
	// sibling evidence-type enum puts UNSPECIFIED at 0 for the same reason: a
	// value nobody set must never read as a real, actionable one, and here the
	// permissive answer is the dangerous one — a zero-value Anchor would
	// otherwise claim it can name a culprit.
	KindUnclassified Kind = iota
	// KindRemoval is a hunk that removed or replaced lines. Those lines existed
	// before the fix, so blame can name the commit that wrote them — the
	// strongest target.
	KindRemoval
	// KindAdditionOnly is a hunk that only added lines. There is no prior line to
	// blame; the enclosing region is weaker evidence and is not an anchor.
	KindAdditionOnly
	// KindTest is a hunk in a test file. A test states the intended behaviour, so
	// it is evidence that a fix happened, never the origin of the defect.
	KindTest
)

// Anchor is one blameable location: where the fix removed something, and what it
// removed.
type Anchor struct {
	// OldPath is the path BEFORE the change. Blame reads history, so a renamed
	// file must be asked about under the name the history knows.
	OldPath string
	// OldStartLine and OldLineCount come from the hunk header's `-start,count`.
	// They address the pre-change file, which is the revision blame is run
	// against.
	OldStartLine int
	OldLineCount int
	// RemovedText is the removed lines verbatim, without their leading marker.
	// Kept so a later match can be verified against what was actually there
	// rather than assumed from line numbers, which shift under any edit above
	// them. Bounded per hunk — a blame lookup reads the first lines to verify a
	// match and has no use for the thousandth — so it can be shorter than
	// RemovedLines.
	RemovedText []string
	// RemovedLines is how many lines the hunk actually removed, whether or not
	// their text was kept — the size of the change, which the kept text no longer
	// reports once the bound applies. Classification reads it rather than the
	// text length.
	RemovedLines int
	// RemovedStartLine and RemovedEndLine bound the hunk's FIRST contiguous run
	// of removed lines within the pre-change file — removed lines only, excluding
	// context. This is the region the syntax-tree comparison uses, NOT the hunk
	// span: a hunk's context routinely straddles a neighbouring declaration, and
	// a region spanning two declarations resolves to no single node, degrading
	// the fix to same-file coverage. Scoping to the first run keeps the region on
	// one change and aligned with the recovered candidate line, since both derive
	// from the first removed line. Both are 0 when the hunk removed nothing.
	RemovedStartLine int
	RemovedEndLine   int
	Kind             Kind
}

// Blameable reports whether this anchor can name an origin at all.
func (a Anchor) Blameable() bool { return a.Kind == KindRemoval }

// reasonOrgGlob labels a path dropped by a caller-configured glob rather than by
// the always-on vendor and generated filter.
const reasonOrgGlob = "org_glob"

// Options bound one extraction. The zero value is valid: it uses
// [DefaultMaxAnchors], excludes nothing beyond the always-on filter, and
// discards every event.
type Options struct {
	// ExcludePatterns are caller-configured globs that drop additional paths from
	// blame anchoring, layered on top of the always-on vendor and generated
	// filter. Applied before anything leaves this package, so an excluded path
	// never reaches a provider call and is never paid for.
	ExcludePatterns []string
	// MaxAnchors caps one fix's anchors. A single oversized change must not be
	// able to consume a provider budget on its own. Zero means
	// [DefaultMaxAnchors].
	MaxAnchors int
	// Observer receives the extraction's diagnostic events. Nil discards them.
	Observer Observer
}

func (o Options) obs() Observer { return orNop(o.Observer) }

// excludeReason reports why a path is dropped from blame anchoring:
// [ReasonVendor] or [ReasonGeneratedPath] — the always-on filter, since every
// caller wants generated files out of defect blame, so it is correctness rather
// than policy — or "org_glob", the additive caller layer. It returns "" when the
// path is kept.
//
// A generated file whose only signal is a banner in its body is not visible from
// the path alone; the blame walk drops those from the file body it holds. The
// reason labels the drop counter, so over-exclusion at the path stage is
// measurable rather than a silent empty anchor set.
func (o Options) excludeReason(path string) string {
	if r := PathExcludeReason(path); r != "" {
		return r
	}
	if diffsize.MatchAny(path, o.ExcludePatterns) {
		return reasonOrgGlob
	}
	return ""
}

// excludesPath reports whether a path is dropped from blame anchoring. The scan
// sites use it only to suppress text accumulation; the definitive drop and its
// event are in collect.
func (o Options) excludesPath(path string) bool {
	return o.excludeReason(path) != ""
}

// DefaultMaxAnchors is the per-fix cap.
//
// Fifty is well above a normal bug fix and well below a refactor that touches
// everything. The cap is a budget bound, not a correctness one: a fix needing
// more than this is not a fix whose origin a line-level walk can name anyway.
const DefaultMaxAnchors = 50

// ExtractUnified reads a unified patch, the format most providers serve a pull
// request's diff as.
func ExtractUnified(raw []byte, opts Options) []Anchor {
	return collect(scanUnified(string(raw), opts), opts)
}

// gitLabDiffEntry is GitLab's per-file wrapper. Its `diff` is a unified body
// with no file headers, so the path comes from the entry and the hunks from the
// embedded text.
type gitLabDiffEntry struct {
	OldPath     string `json:"old_path"`
	NewPath     string `json:"new_path"`
	Diff        string `json:"diff"`
	DeletedFile bool   `json:"deleted_file"`
}

// ExtractAnchors reads a fix's diff with the parser its format names. It is the
// one place that decision is made, so a new format is added here rather than at
// every call site.
func ExtractAnchors(in FixInput, opts Options) []Anchor {
	if in.DiffFormat == DiffFormatGitLab {
		return ExtractGitLab(in.Diff, opts)
	}
	return ExtractUnified(in.Diff, opts)
}

// ExtractGitLab reads GitLab's JSON array of per-file diffs.
func ExtractGitLab(raw []byte, opts Options) []Anchor {
	var entries []gitLabDiffEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		// One malformed stored diff must not take down the batch it arrived in:
		// return no anchors, the same answer a change with nothing to blame
		// produces.
		opts.obs().Debug("malformed_gitlab_diffs_json", map[string]any{"error": err.Error()})
		return nil
	}
	var out []Anchor
	for _, e := range entries {
		if e.DeletedFile {
			// The whole file is gone. Every line "changed", which names the file's
			// own history rather than the defect, so it is noise at anchor
			// granularity.
			continue
		}
		path := e.OldPath
		if path == "" {
			path = e.NewPath
		}
		// Excluded here as well as in collect: the scan can then skip accumulating
		// text for a path already known to be unwanted, rather than building it and
		// dropping it.
		skip := opts.excludesPath(path)
		out = append(out, scanHunks(path, e.Diff, skip, opts.obs())...)
	}
	return collect(out, opts)
}

// scanUnified walks a multi-file unified patch.
//
// It tracks how many lines of each hunk body remain, from the counts the hunk
// header declares, and only interprets a line as a file header when it is NOT
// inside one. That is not defensiveness — it is the only way to tell them apart.
// A removed line whose own content begins with "-- " becomes "--- " once the
// diff prepends its removal marker, and SQL comments look exactly like that, so
// a prefix test alone reads a migration's own comment as the start of a new
// file: the hunk yields no anchor and the path it was attributed to becomes
// whatever followed the dashes.
func scanUnified(raw string, opts Options) []Anchor {
	obs := opts.obs()
	var (
		out     []Anchor
		oldPath string
		skip    bool
		current *Anchor
		body    hunkBody
	)
	flush := func() {
		if current != nil {
			out = append(out, *current)
			current = nil
		}
	}

	sc := newDiffScanner(raw)
	for sc.Scan() {
		line := sc.Text()

		if current != nil && !body.done() {
			if !endsBodyEarly(line) {
				body.consume(line, current, skip)
				if body.done() {
					flush()
				}
				continue
			}
			// The body ended before its header said it would. Keep what was read
			// and let this line be dispatched as the structure it is.
			obs.Debug("hunk_body_short", map[string]any{"old_path": oldPath})
			flush()
			body = hunkBody{}
		}

		switch {
		case strings.HasPrefix(line, "diff --git "):
			// The section separator. Clearing the path here is what stops a file
			// whose own "---" line is missing from inheriting the previous file's
			// path and attributing its hunks to the wrong file.
			flush()
			oldPath, skip = "", false
		case strings.HasPrefix(line, "--- "):
			flush()
			oldPath = trimDiffPath(strings.TrimPrefix(line, "--- "), "a/")
			skip = oldPath != "" && opts.excludesPath(oldPath)
		case strings.HasPrefix(line, "+++ "):
			newPath := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
			if i := strings.IndexByte(newPath, '\t'); i >= 0 {
				newPath = newPath[:i]
			}
			if unquoteDiffPath(newPath) == "/dev/null" {
				// A whole-file deletion. Every line of the file is removed, so an
				// anchor over them names whoever wrote the file rather than whoever
				// introduced the defect. ExtractGitLab drops these on the vendor's
				// deleted_file flag; the unified parser has only this line to read
				// it from, and the two must agree or the same fix traces
				// differently per provider.
				flush()
				oldPath, skip = "", true
				break
			}
			// Otherwise read only to recover a path for an added file, whose old
			// side is /dev/null.
			if oldPath == "" {
				oldPath = trimDiffPath(strings.TrimPrefix(line, "+++ "), "b/")
				skip = oldPath != "" && opts.excludesPath(oldPath)
			}
		case strings.HasPrefix(line, "@@"):
			flush()
			if a, b, ok := parseHunkHeader(oldPath, line, obs); ok {
				current, body = &a, b
				if body.done() {
					flush()
				}
			}
		}
	}
	flush()
	warnIfTruncated(sc, obs)
	return out
}

// scanHunks walks a single file's hunk body, where the path is already known.
func scanHunks(oldPath, rawBody string, skip bool, obs Observer) []Anchor {
	var (
		out     []Anchor
		current *Anchor
		body    hunkBody
	)
	flush := func() {
		if current != nil {
			out = append(out, *current)
			current = nil
		}
	}
	sc := newDiffScanner(rawBody)
	for sc.Scan() {
		line := sc.Text()
		if current != nil && !body.done() {
			if !endsBodyEarly(line) {
				body.consume(line, current, skip)
				if body.done() {
					flush()
				}
				continue
			}
			obs.Debug("hunk_body_short", map[string]any{"old_path": oldPath})
			flush()
			body = hunkBody{}
		}
		if strings.HasPrefix(line, "@@") {
			flush()
			if a, b, ok := parseHunkHeader(oldPath, line, obs); ok {
				current, body = &a, b
				if body.done() {
					flush()
				}
			}
		}
	}
	flush()
	warnIfTruncated(sc, obs)
	return out
}

// newDiffScanner reads a diff line by line. Generated and minified files carry
// lines far past the scanner's default 64 KiB limit, and a diff that trips it
// stops for good — so the ceiling is raised here, once, and every caller checks
// the error afterwards.
func newDiffScanner(raw string) *bufio.Scanner {
	sc := bufio.NewScanner(strings.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), scanLineLimit)
	return sc
}

// endsBodyEarly reports a line that cannot occur inside a well-formed hunk body,
// and so proves the header's declared counts were wrong.
//
// Every body line carries a marker in column zero — a space for context, "-",
// "+", or the no-newline "\\" — so a line starting flush with "diff --git " or
// "@@" is structure, not content. That is what makes these two safe to
// resynchronise on when "--- " is not: a removed line whose own text begins
// "-- " produces "--- " legitimately, but nothing produces an unmarked "@@".
//
// Counts do lie. A stored diff can be truncated mid-body — GitLab's per-file
// diff is documented to truncate an oversized file — and without a
// resynchronisation point an over-declared count consumes the rest of the patch:
// the next file's header lines are eaten as body text, its real hunk is dropped,
// and this file's anchor keeps the garbage.
func endsBodyEarly(line string) bool {
	return strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "@@")
}

// scanLineLimit bounds one line. Beyond it the scanner stops for good, which is
// why the caller must look at its error rather than treat a short result as a
// small change.
const scanLineLimit = 8 * 1024 * 1024

// maxRemovedLinesPerHunk bounds what one hunk keeps.
//
// The exclusion and the anchor cap both run after the scan, so without a bound
// here a diff that deletes a large generated file is fully materialised in memory
// before anything decides it was never wanted. A blame lookup reads the first
// lines of a hunk to verify a match; it has no use for the thousandth.
const maxRemovedLinesPerHunk = 200

// hunkBody counts down the lines a hunk header declared, so the scanner knows
// where the body ends without guessing from content. oldCursor is the pre-change
// file line number of the next old-side line, a context or removed line,
// advanced as the body is consumed, so a removed line can be recorded at its
// real line number for the tight removed-line range.
type hunkBody struct {
	oldRemaining, newRemaining int
	oldCursor                  int
}

func (h hunkBody) done() bool { return h.oldRemaining <= 0 && h.newRemaining <= 0 }

// consume applies one body line. skip suppresses only the text accumulation —
// the counts still advance, because the body's length is what tells the scanner
// where it ends.
func (h *hunkBody) consume(line string, a *Anchor, skip bool) {
	switch {
	case strings.HasPrefix(line, `\`):
		// "\ No newline at end of file" annotates the line before it and occupies
		// no position on either side.
	case strings.HasPrefix(line, "-"):
		if !skip && len(a.RemovedText) < maxRemovedLinesPerHunk {
			a.RemovedText = append(a.RemovedText, strings.TrimPrefix(line, "-"))
		}
		// Record the removed line at its real pre-change line number. The range
		// covers the FIRST contiguous run of removed lines only: a hunk can merge
		// two edits separated by a few context lines, and a range spanning that gap
		// would straddle a neighbouring declaration and degrade the syntax-tree
		// match — the very failure the tight range exists to avoid. The first run
		// is also the one the candidate line is recovered from, since the walk's
		// representative line is the first removed line, so the compared region and
		// that line describe the same change. Contiguity is oldCursor == prev+1: a
		// context line advances oldCursor and so breaks the run; an added line does
		// not.
		switch {
		case a.RemovedLines == 0:
			a.RemovedStartLine = h.oldCursor
			a.RemovedEndLine = h.oldCursor
		case h.oldCursor == a.RemovedEndLine+1:
			a.RemovedEndLine = h.oldCursor
		}
		a.RemovedLines++
		h.oldCursor++
		h.oldRemaining--
	case strings.HasPrefix(line, "+"):
		h.newRemaining--
	default:
		// A context line, which both sides count. An empty line is context too:
		// some tools strip the trailing space that would otherwise mark it.
		h.oldCursor++
		h.oldRemaining--
		h.newRemaining--
	}
}

// warnIfTruncated reports a scan that stopped early. Without it a diff
// containing one oversized line yields a short anchor set that is
// indistinguishable from a small change, and every file after that line is
// silently absent.
func warnIfTruncated(sc *bufio.Scanner, obs Observer) {
	if err := sc.Err(); err != nil {
		obs.Debug("diff_scan_truncated", map[string]any{"error": err.Error()})
	}
}

// parseHunkHeader reads `@@ -start,count +start,count @@` into an anchor's
// old-side range.
//
// The count is optional: git omits `,count` when it is 1, so `-42` means one
// line at 42. Reading the absent count as zero would produce an empty range that
// matches nothing.
func parseHunkHeader(oldPath, line string, obs Observer) (Anchor, hunkBody, bool) {
	i := strings.Index(line, "-")
	if oldPath == "" || i < 0 {
		return Anchor{}, hunkBody{}, false
	}
	rest := line[i+1:]
	if j := strings.IndexAny(rest, " \t"); j >= 0 {
		rest = rest[:j]
	}
	start, count, ok := parseRange(rest, obs)
	if !ok {
		return Anchor{}, hunkBody{}, false
	}
	// The new-side range bounds the body too: a body ends when both sides are
	// consumed.
	newCount := 0
	if j := strings.Index(line, "+"); j >= 0 {
		newRest := line[j+1:]
		if k := strings.IndexAny(newRest, " \t"); k >= 0 {
			newRest = newRest[:k]
		}
		if _, c, okNew := parseRange(newRest, obs); okNew {
			newCount = c
		}
	}
	return Anchor{OldPath: oldPath, OldStartLine: start, OldLineCount: count},
		hunkBody{oldRemaining: count, newRemaining: newCount, oldCursor: start}, true
}

// parseRange reads a hunk header's `start,count`. The count is optional: git
// omits `,count` when it is 1, so `-42` means one line at 42 — reading the
// absent count as zero would produce an empty range that matches nothing.
//
// A range that will not parse is reported rather than dropped: it means a hunk
// produced no anchor, which is a blame target lost, and the whole point of this
// parser is that such a loss must not be silent.
func parseRange(raw string, obs Observer) (start, count int, ok bool) {
	startStr, countStr, hasCount := strings.Cut(raw, ",")
	start, err := strconv.Atoi(startStr)
	if err != nil {
		obs.Debug("unparseable_hunk_range", map[string]any{"range": raw, "error": err.Error()})
		return 0, 0, false
	}
	count = 1
	if hasCount {
		c, cErr := strconv.Atoi(countStr)
		if cErr != nil {
			obs.Debug("unparseable_hunk_count", map[string]any{"range": raw, "error": cErr.Error()})
			return 0, 0, false
		}
		// A negative count is corruption, and accepting one lets done() be
		// satisfied at the wrong moment — which is how a bad header takes the rest
		// of the patch with it.
		if c < 0 {
			obs.Debug("negative_hunk_count", map[string]any{"range": raw})
			return 0, 0, false
		}
		count = c
	}
	return start, count, true
}

// trimDiffPath strips the diff's path prefix and yields "" for the /dev/null
// side.
func trimDiffPath(path, prefix string) string {
	path = strings.TrimSpace(path)
	// A tab-separated timestamp follows the path in a classic patch.
	if i := strings.IndexByte(path, '\t'); i >= 0 {
		path = path[:i]
	}
	path = unquoteDiffPath(path)
	if path == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(path, prefix)
}

// unquoteDiffPath undoes git's core.quotePath encoding, which is on by default:
// a path holding a byte outside ASCII, a quote or a backslash is printed
// double-quoted with C-style escapes ("a/caf\303\251.go"). Left encoded, the
// path never matches the file the blame source knows, and the walk fails closed
// with nothing to say why. A string that does not parse is returned unchanged,
// so an ordinary path keeps its exact bytes.
func unquoteDiffPath(path string) string {
	if len(path) < 2 || path[0] != '"' || path[len(path)-1] != '"' {
		return path
	}
	// Git writes the bytes of the path, so the octal escapes are bytes rather
	// than runes. Unquote one escape at a time and keep the bytes as they come.
	var b strings.Builder
	rest := path[1 : len(path)-1]
	for len(rest) > 0 {
		c, multibyte, tail, err := strconv.UnquoteChar(rest, '"')
		if err != nil {
			return path
		}
		if multibyte {
			b.WriteRune(c)
		} else {
			b.WriteByte(byte(c))
		}
		rest = tail
	}
	return b.String()
}

// collect classifies, excludes, deduplicates and caps.
//
// Order matters and is the point of the slice: everything that removes work
// happens before anything that costs a provider call, so an excluded or
// duplicated anchor is never paid for rather than being paid for and then
// discarded.
func collect(raw []Anchor, opts Options) []Anchor {
	obs := opts.obs()
	maxAnchors := opts.MaxAnchors
	if maxAnchors <= 0 {
		maxAnchors = DefaultMaxAnchors
	}

	out := make([]Anchor, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, a := range raw {
		if a.OldPath == "" {
			continue
		}
		if reason := opts.excludeReason(a.OldPath); reason != "" {
			obs.Count("anchor_path_excluded", map[string]string{"reason": reason})
			continue
		}
		a.Kind = classify(a)
		if !a.Blameable() {
			continue
		}
		// Deduplicated on (path, old start line). Adjacent hunks share context
		// lines, so two of them routinely address the same line — and blaming it
		// twice would count one piece of evidence twice in the agreement measure.
		key := a.OldPath + ":" + strconv.Itoa(a.OldStartLine)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, a)
		if len(out) == maxAnchors {
			// Reported because the return shape cannot say it: a capped set and a
			// small change look identical to a caller counting anchors.
			obs.Debug("anchor_cap_reached", map[string]any{"cap": maxAnchors, "old_path": a.OldPath})
			break
		}
	}
	return out
}

// classify decides what a hunk's shape allows.
func classify(a Anchor) Kind {
	if isTestPath(a.OldPath) {
		return KindTest
	}
	if a.RemovedLines == 0 {
		return KindAdditionOnly
	}
	return KindRemoval
}

// testPathMarkers are the path shapes that mean "test" across common languages.
// Matched on the path rather than the content because the decision is about what
// the file IS, and a test that happens to contain no assertions is still a test.
var testPathMarkers = []string{"_test.", ".test.", ".spec.", "/tests/", "/test/", "/testdata/", "/__tests__/"}

func isTestPath(path string) bool {
	lower := strings.ToLower(path)
	for _, m := range testPathMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return strings.HasPrefix(lower, "test/") || strings.HasPrefix(lower, "tests/")
}
