// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

import (
	"regexp"
	"sort"
	"strings"
)

// Candidate is one reference found on a pull request or issue, before it is
// resolved to the entity it names.
type Candidate struct {
	// Signal is the generator that found it.
	Signal SignalType `json:"signal"`
	// Identifier is the reference as the resolver reads it: an issue key
	// ("PROJ-42", "acme/web#7") on a pull request, a pull-request key
	// ("acme/web#PR-12", "group/project#MR-3") on an issue. The extractors
	// build it without surrounding white space, and the resolver and the
	// stores compare it exactly.
	Identifier string `json:"identifier"`
	// Field is where it was found.
	Field SourceField `json:"field"`
	// Heading is the matched heading text when Section is not SectionNone.
	Heading string `json:"heading"`
	// Intent is what the surrounding words say.
	Intent Intent `json:"intent"`
	// Namespace is the tool the identifier belongs to; VendorAny when unknown.
	Namespace Vendor `json:"namespace"`
	// Section classifies the heading a body reference sits under. A negative
	// heading makes the reference context; a positive heading is evidence
	// for a reader, and stage 2 does not weigh it.
	Section Section `json:"section"`
	// Keyword is the matched keyword before the reference, lower-cased, or
	// "conv_commit" for a conventional-commit scope; empty when none.
	Keyword string `json:"keyword"`
	// CaseFolded marks a key read from a branch name in lower or mixed case
	// and upper-cased, such as ENG-42 from "eng-42-limiter". Such a key may be
	// a word that only looks like one ("release/v1-2"), so when it does not
	// resolve it is reported but not retried.
	CaseFolded bool `json:"case_folded"`
	// Offset and Length pin the matched text in the scanned text, not in the
	// raw field: a no-break space there is a space and, in a body, every line
	// end is "\n" and comments, quotes, code and trailers are removed (code
	// and quoted lines are left empty). Offset is -1 for a branch, a
	// conventional-commit scope or a native link.
	Offset int `json:"offset"`
	Length int `json:"length"`
}

// Extraction is stage 1's output for one entity.
type Extraction struct {
	Candidates []Candidate `json:"candidates"`
	// TitleFlags lists the title's noise markers: "generic_title",
	// "draft_prefix", "revert". They are evidence for a reader; stage 2 does
	// not weigh them.
	TitleFlags []string `json:"title_flags"`
	// Truncated reports that a scanned field was longer than MaxTextBytes
	// and was cut, so the candidates may not be every reference the record
	// holds.
	Truncated bool `json:"truncated"`
}

// MaxTextBytes bounds one scanned field. The scan is linear, so a long field
// costs time and memory rather than correctness, but nothing in the engine
// bounds what a caller hands it. The limit sits above what every code host
// accepts in the first place — GitHub caps a body at 65,536 characters, Jira
// at 32,767 and GitLab at this same megabyte — so real records never reach
// it, and a caller that has concatenated or synthesised text gets a bounded
// scan rather than one that grows with its input.
const MaxTextBytes = 1 << 20

// clampText cuts text to MaxTextBytes, reporting whether it cut. It cuts at
// the last line break inside the limit, never mid-line, because cutting
// through a key would leave a shorter key that still matches: "PROJ-123"
// becomes "PROJ-12", a reference to a real and entirely unrelated issue. A
// field over the limit holding no line break is dropped whole: no cut in it
// is safe, and a megabyte on one line is not a record any code host wrote.
func clampText(text string) (string, bool) {
	if len(text) <= MaxTextBytes {
		return text, false
	}
	cut := text[:MaxTextBytes]
	nl := strings.LastIndexByte(cut, '\n')
	if nl < 0 {
		return "", true
	}
	return cut[:nl], true
}

// keyPattern matches a project-prefixed key: a capital letter, then 1 to 9
// capitals, digits or underscores, a hyphen, 1 to 7 digits.
var keyPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{1,9}-[0-9]{1,7}\b`)

// githubURLPattern matches github.com issue and pull-request URLs.
var githubURLPattern = regexp.MustCompile(`https?://github\.com/([\w.-]+)/([\w.-]+)/(issues|pull)/(\d+)`)

// hashRefPattern matches "owner/repo#N", a nested GitLab path
// "group/sub/project#N", and a bare "#N". Group 1 is the path, group 2 the
// number.
var hashRefPattern = regexp.MustCompile(`(?:([\w.-]+(?:/[\w.-]+)+))?#(\d+)\b`)

// branchUpperKeyPattern matches a key in a branch name. Unlike keyPattern it
// may be followed by an underscore ("PROJ-12_fix"); branchKeys checks that a
// letter or digit does not follow.
var branchUpperKeyPattern = regexp.MustCompile(`\b[A-Z][A-Z0-9_]{1,9}-[0-9]{1,7}`)

// branchKeyPattern matches a key in any case at the start of a branch name
// or of one of its "/" segments, as tools that lower-case branch names write
// it ("eng-42-limiter", "user/eng-42-limiter"). Group 1 is the key.
var branchKeyPattern = regexp.MustCompile(`(?i)(?:^|/)([a-z][a-z0-9_]{1,9}-[0-9]{1,7})`)

// branchKeys returns the keys re finds in a branch name whose next character
// is not an ASCII letter or digit, the rule (?:[^0-9A-Za-z]|$) states, checked
// after the match so the character is not consumed: "a/proj-1/proj-2" holds
// two. group is the submatch that holds the key.
func branchKeys(re *regexp.Regexp, branch string, group int) []string {
	var out []string
	for _, loc := range re.FindAllStringSubmatchIndex(branch, -1) {
		end := loc[2*group+1]
		if end < len(branch) && isASCIIAlnum(branch[end]) {
			continue
		}
		out = append(out, branch[loc[2*group]:end])
	}
	return out
}

func isASCIIAlnum(b byte) bool {
	return '0' <= b && b <= '9' || 'a' <= b && b <= 'z' || 'A' <= b && b <= 'Z'
}

// keywordPattern matches the keywords that set an intent: closing (fix,
// close, resolve), delivering (implement, deliver, complete, ship) and
// referring (see, ref, cc, related to, toward, part of), each followed by an
// optional colon and white space.
var keywordPattern = regexp.MustCompile(`(?i)\b(?:fix(?:e[sd])?|clos(?:e[sd]?)|resolv(?:e[sd]?)|implement(?:s|ed)?|deliver(?:s|ed)?|complete[sd]?|ship(?:s|ped)?|see|refs?|cc|related(?:[ \t]+to)?|towards?|part[ \t]+of):?\s+`)

// convCommitPattern matches a conventional-commit title whose scope holds a
// key: "fix(WEB-12): ...". Group 1 is the type, group 2 the scope.
var convCommitPattern = regexp.MustCompile(`^(fix|feat|chore|refactor|test|docs|perf|build|ci|revert)\(([^)]+)\):`)

// gitlabURLPattern matches gitlab.com and self-hosted gitlab.* issue and
// merge-request URLs: group path, kind, number.
var gitlabURLPattern = regexp.MustCompile(`https?://(?:gitlab\.com|gitlab\.[\w.-]+)/([\w./-]+?)/-/(issues|merge_requests)/(\d+)`)

// jiraCloudURLPattern matches a Jira Cloud browse URL.
var jiraCloudURLPattern = regexp.MustCompile(`https?://[\w.-]+\.atlassian\.net/browse/([A-Z][A-Z0-9_]{1,9}-[0-9]{1,7})`)

// linearURLPattern matches a Linear issue URL.
var linearURLPattern = regexp.MustCompile(`https?://linear\.app/[\w.-]+/issue/([A-Z][A-Z0-9_]{1,9}-[0-9]{1,7})`)

// namespacedURLSpans returns the [start, end) byte spans of every Jira, Linear,
// GitHub and GitLab URL in text. The key scan skips matches inside them, so a
// key in a URL keeps the URL's namespace instead of the bare-key one.
func namespacedURLSpans(text string) [][2]int {
	var spans [][2]int
	for _, re := range []*regexp.Regexp{jiraCloudURLPattern, linearURLPattern, githubURLPattern, gitlabURLPattern} {
		for _, loc := range re.FindAllStringIndex(text, -1) {
			spans = append(spans, [2]int{loc[0], loc[1]})
		}
	}
	return mergeSpans(spans)
}

// urlPattern matches any http or https URL, up to white space, a bracket or
// a comma; a balanced pair of parentheses is part of it, as in
// ".../wiki/Foo_(bar)".
var urlPattern = regexp.MustCompile(`https?://(?:[^\s<>()\[\],]|\([^\s<>()\[\],]*\))+`)

// urlTrailingPunctuation is what a URL does not end with, as in GitHub's
// autolinks: a sentence's full stop after a URL ends the sentence.
const urlTrailingPunctuation = "?!.,:;*_~"

// anyURLSpans returns the [start, end) byte spans of every URL in text,
// without trailing punctuation.
func anyURLSpans(text string) [][2]int {
	var spans [][2]int
	for _, loc := range urlPattern.FindAllStringIndex(text, -1) {
		end := loc[1]
		for end > loc[0] && strings.IndexByte(urlTrailingPunctuation, text[end-1]) >= 0 {
			end--
		}
		spans = append(spans, [2]int{loc[0], end})
	}
	return mergeSpans(spans)
}

func withinAnySpan(pos int, spans [][2]int) bool {
	// The spans are sorted and merged (see mergeSpans), so the one that can
	// hold pos is the last one starting at or before it.
	k := sort.Search(len(spans), func(k int) bool { return spans[k][0] > pos }) - 1
	return k >= 0 && pos < spans[k][1]
}

// mergeSpans sorts spans and merges the overlapping ones.
func mergeSpans(spans [][2]int) [][2]int {
	sort.Slice(spans, func(a, b int) bool { return spans[a][0] < spans[b][0] })
	var out [][2]int
	for _, sp := range spans {
		if n := len(out); n > 0 && sp[0] < out[n-1][1] {
			if sp[1] > out[n-1][1] {
				out[n-1][1] = sp[1]
			}
			continue
		}
		out = append(out, sp)
	}
	return out
}

// genericTitlePattern matches a title with no content of its own.
var genericTitlePattern = regexp.MustCompile(`(?i)^(?:wip|tmp|test|update|fix|bump|version|chore|refactor)$`)

// draftPrefixPattern matches a draft or work-in-progress title prefix.
var draftPrefixPattern = regexp.MustCompile(`(?i)^(?:wip|draft|\[wip\]|\[draft\])[\s:]`)

// revertPrefixPattern matches a revert title.
var revertPrefixPattern = regexp.MustCompile(`(?i)^(?:revert\s+["'\x60]|revert\([^)]*\):)`)

// headingPattern matches a Markdown heading, indented up to three spaces;
// group 1 is its text, without closing #s.
var headingPattern = regexp.MustCompile(`(?m)^ {0,3}#{1,6}[ \t]+(.+?)(?:[ \t]+#+)?[ \t]*$`)

// negativeHeadingText matches the headings whose references are context, not
// delivery.
var negativeHeadingText = regexp.MustCompile(`(?i)^(?:background|previous\s+work|context|references|inspired\s+by|docs|prior\s+art)\s*:?$`)

// positiveHeadingText matches the headings that name delivered work.
var positiveHeadingText = regexp.MustCompile(`(?i)^(?:tickets?\s+closed|fixes?|closes|resolves?|jira\s+ticket|linear\s+issue|ticket)\s*:?$`)

var (
	stripHTMLCommentsPattern = regexp.MustCompile(`(?s)<!--.*?-->`)
	stripBlockquotePattern   = regexp.MustCompile(`(?m)^ {0,3}>.*$`)
	stripTrailerPattern      = regexp.MustCompile(`(?im)^(?:signed-off-by|co-authored-by|reviewed-by|reported-by):\s.+$`)
)

// lineEnds turns CR CR LF, CRLF and lone CR line ends into LF, and a
// no-break space (U+00A0) into a space.
var lineEnds = strings.NewReplacer("\r\r\n", "\n", "\r\n", "\n", "\r", "\n", "\u00a0", " ")

// noBreakSpaces turns a title's no-break spaces into spaces.
var noBreakSpaces = strings.NewReplacer("\u00a0", " ")

// stripBody turns every line end into "\n" and every no-break space into a
// space, then removes HTML comments, quoted lines (">" after up to three
// spaces), fenced code blocks (``` or ~~~), indented code blocks and commit
// trailers before the scan, so quoted or commented-out references do not
// count. Inline code is kept.
func stripBody(body string) string {
	body = lineEnds.Replace(body)
	body = stripHTMLCommentsPattern.ReplaceAllString(body, "")
	body = stripBlockquotePattern.ReplaceAllString(body, "")
	body = stripCode(body)
	body = stripTrailerPattern.ReplaceAllString(body, "")
	return body
}

var (
	// listMarkerPattern matches a list item's marker at the start of a
	// line's text: a bullet or a number with "." or ")", then white space or
	// the line's end.
	listMarkerPattern = regexp.MustCompile(`^(?:[-*+]|[0-9]+[.)])(?:[ \t]|$)`)
	// thematicBreakPattern matches a thematic break, which is not a list item.
	thematicBreakPattern = regexp.MustCompile(`^(?:(?:\*[ \t]*){3,}|(?:-[ \t]*){3,}|(?:_[ \t]*){3,})$`)
	// blockHeadingPattern matches an ATX heading at the start of a line's text.
	blockHeadingPattern = regexp.MustCompile(`^#{1,6}(?:[ \t]|$)`)
)

// lineIndent returns the column a line's text starts at, a tab moving to
// the next multiple of four, and that text; the text is "" for a blank line.
func lineIndent(l string) (int, string) {
	col := 0
	for i := 0; i < len(l); i++ {
		switch l[i] {
		case ' ':
			col++
		case '\t':
			col += 4 - col%4
		default:
			return col, l[i:]
		}
	}
	return col, ""
}

// stripCode blanks fenced and indented code blocks, reading lines as
// CommonMark does, with list items:
//
//   - A fence opens at a line whose text starts with a run of three or more
//     backticks or tildes, indented at most three columns past the enclosing
//     list item's text; a backtick run followed by another backtick on the
//     line is inline code, not a fence. It closes at a line made of the same
//     character, a run at least as long, with only white space after it,
//     also indented at most three columns past that text. A fence that
//     never closes runs to the end of the body, or of its list item: a
//     line with text indented less than the item's text ends both.
//   - An indented code block is a run of lines indented four or more columns
//     past the enclosing list item's text (zero outside a list), that starts
//     at the body's start or after a blank line, a heading, a thematic break
//     or a fence, with the blank lines inside it.
//   - A list item starts at a line whose text starts with a marker ("-", "*",
//     "+", "1." or "1)" and white space, or the line's end), and its text
//     starts at the column after the marker's white space. A line after a
//     blank line indented less than that ends it, as does a heading, a fence
//     or another marker indented less; a line with no blank line before it
//     continues it. An item whose first line holds only the marker ends at a
//     blank line after it, and such a marker does not interrupt a paragraph
//     outside a list. The rest of a marker's line is read for a fence opener.
//
// So "- Changes:\n\n    Fixes PROJ-1" keeps the reference, which is the
// item's text, and "Notes:\n\n    Fixes PROJ-1" blanks it.
func stripCode(body string) string {
	lines := strings.Split(body, "\n")
	cols := make([]int, len(lines))
	texts := make([]string, len(lines))
	for i, l := range lines {
		cols[i], texts[i] = lineIndent(l)
	}
	var items []int // the text column of each open list item, innermost last
	popTo := func(col int) {
		for len(items) > 0 && items[len(items)-1] > col {
			items = items[:len(items)-1]
		}
	}
	base := func() int {
		if len(items) == 0 {
			return 0
		}
		return items[len(items)-1]
	}
	// fence blanks the fence opened at line i and returns its last line.
	fence := func(i int, text string) int {
		end := fenceEnd(cols, texts, i, text, base())
		for k := i; k <= end; k++ {
			lines[k] = ""
		}
		return end
	}
	opens, inCode, emptyItem := true, false, false
	for i := 0; i < len(lines); i++ {
		col, text := cols[i], texts[i]
		if text == "" {
			if inCode {
				lines[i] = ""
			}
			if emptyItem {
				// An item that holds only its marker ends at a blank line.
				items = items[:len(items)-1]
			}
			opens, emptyItem = true, false
			continue
		}
		emptyItem = false
		if opens {
			popTo(col)
		}
		if (inCode || opens) && col-base() >= 4 {
			inCode, opens = true, false
			lines[i] = ""
			continue
		}
		inCode = false
		if col-base() >= 4 {
			opens = false
			continue // a paragraph's continuation
		}
		isFence := fenceOpener(text)
		marker := !thematicBreakPattern.MatchString(text) && listMarkerPattern.MatchString(text)
		if marker && markerOnly(text) && !opens && len(items) == 0 {
			marker = false // an empty item does not interrupt a paragraph
		}
		heading := blockHeadingPattern.MatchString(text)
		if isFence || marker || heading || thematicBreakPattern.MatchString(text) {
			popTo(col)
		}
		opens = false
		switch {
		case isFence:
			i, opens = fence(i, text), true
		case marker:
			textCol, rest := itemText(col, text)
			items = append(items, textCol)
			switch {
			case rest == "":
				emptyItem = markerOnly(text)
			case fenceOpener(rest):
				i, opens = fence(i, rest), true
			}
		case heading || thematicBreakPattern.MatchString(text):
			opens = true
		}
	}
	return strings.Join(lines, "\n")
}

// fenceRun returns the character and length of the run of backticks or
// tildes text starts with; the length is 0 for neither.
func fenceRun(text string) (byte, int) {
	if text == "" || (text[0] != '`' && text[0] != '~') {
		return 0, 0
	}
	n := 0
	for n < len(text) && text[n] == text[0] {
		n++
	}
	return text[0], n
}

// fenceOpener reports whether text opens a fence: a run of three or more
// backticks with no backtick after it, or of three or more tildes.
func fenceOpener(text string) bool {
	c, n := fenceRun(text)
	return n >= 3 && (c == '~' || !strings.Contains(text[n:], "`"))
}

// fenceEnd returns the last line of the fence whose opener is text, on line
// open, inside a list item whose text starts at column base: the closing
// line; else the line before the first line with text indented less than
// base; else the body's last line.
func fenceEnd(cols []int, texts []string, open int, opener string, base int) int {
	c, n := fenceRun(opener)
	for k := open + 1; k < len(texts); k++ {
		if texts[k] != "" && cols[k] < base {
			return k - 1
		}
		if kc, kn := fenceRun(texts[k]); kc == c && kn >= n && cols[k]-base < 4 && strings.Trim(texts[k][kn:], " \t") == "" {
			return k
		}
	}
	return len(texts) - 1
}

// markerOnly reports whether a marker line holds nothing after its marker.
func markerOnly(text string) bool {
	return strings.TrimRight(text[markerLen(text):], " \t") == ""
}

// markerLen returns the length of the list marker text starts with.
func markerLen(text string) int {
	if text[0] == '-' || text[0] == '*' || text[0] == '+' {
		return 1
	}
	return len(text) - len(strings.TrimLeft(text, "0123456789")) + 1
}

// itemText returns the column a list item's text starts at, past the marker
// and its white space, or one column past the marker when that white space
// is wider than four columns or ends the line; and the item's text on the
// marker's line, "" when the line ends or holds indented code.
func itemText(col int, text string) (int, string) {
	m := markerLen(text)
	c := col + m
	w := 0
	i := m
	for ; i < len(text) && (text[i] == ' ' || text[i] == '\t'); i++ {
		if text[i] == '\t' {
			w += 4 - (c+w)%4
		} else {
			w++
		}
	}
	if i == len(text) || w > 4 {
		return c + 1, ""
	}
	return c + w, text[i:]
}

// sectionOf classifies a heading's text.
func sectionOf(text string) (Section, string) {
	switch {
	case negativeHeadingText.MatchString(text):
		return SectionNegative, strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), ":"))
	case positiveHeadingText.MatchString(text):
		return SectionPositive, strings.TrimSpace(strings.TrimSuffix(text, ":"))
	}
	return SectionNone, ""
}

// classifyTitle returns the title's noise flags.
func classifyTitle(title string) []string {
	var flags []string
	t := strings.TrimSpace(title)
	if t == "" {
		return flags
	}
	if genericTitlePattern.MatchString(t) {
		flags = append(flags, "generic_title")
	}
	if draftPrefixPattern.MatchString(t) {
		flags = append(flags, "draft_prefix")
	}
	if revertPrefixPattern.MatchString(t) {
		flags = append(flags, "revert")
	}
	return flags
}

// intentForKeyword maps the keyword before a key to its intent.
func intentForKeyword(kw string) Intent {
	if kw == "" {
		return IntentBare
	}
	a := strings.ToLower(kw)
	switch {
	case strings.HasPrefix(a, "fix") || strings.HasPrefix(a, "clos") || strings.HasPrefix(a, "resolv"):
		return IntentFixes
	case strings.HasPrefix(a, "implement") || strings.HasPrefix(a, "deliver") || strings.HasPrefix(a, "complete") || strings.HasPrefix(a, "ship"):
		return IntentImplements
	case strings.HasPrefix(a, "see") || strings.HasPrefix(a, "ref") || strings.HasPrefix(a, "cc") || strings.HasPrefix(a, "related") || strings.HasPrefix(a, "toward") || strings.HasPrefix(a, "part"):
		return IntentReferenceOnly
	}
	return IntentBare
}

// keywordLookback is how far before a reference a keyword's word may end.
const keywordLookback = 30

// keywordStop matches what ends a keyword's reach: a sentence end or a blank
// line between the keyword and the reference.
var keywordStop = regexp.MustCompile(`[.;!?]|\n[ \t]*\n`)

// keywordWordEnd is where a keyword match's word ends, before the white
// space the match takes after it.
func keywordWordEnd(text string, loc []int) int {
	return loc[0] + len(strings.TrimRight(text[loc[0]:loc[1]], " \t\r\n"))
}

// textIndex holds one scanned text's keyword, identifier and heading
// offsets, found once, so each reference is classified by a binary search
// instead of a rescan.
type textIndex struct {
	text     string
	kws      [][]int  // keywordPattern matches, in order
	starts   []int    // identifier match starts, sorted
	headings [][]int  // headingPattern matches, in order
	urls     [][2]int // anyURLSpans
}

func newTextIndex(text string, headings bool) *textIndex {
	urls := anyURLSpans(text)
	x := &textIndex{text: text, kws: keywordPattern.FindAllStringIndex(text, -1), starts: identifierStarts(text, urls), urls: urls}
	sort.Ints(x.starts)
	if headings {
		x.headings = headingPattern.FindAllStringSubmatchIndex(text, -1)
	}
	return x
}

// keyword returns the keyword nearest before identStart, lower-cased, or ""
// when its word does not end within keywordLookback bytes of it, a sentence end
// outside a URL or a blank line lies between the two, or another identifier
// starts between them:
// "Fixes PROJ-1, see PROJ-2" gives PROJ-2 "see". A keyword is always matched
// whole, over the whole text.
func (x *textIndex) keyword(identStart int) string {
	// The last keyword ending at or before identStart.
	i := sort.Search(len(x.kws), func(i int) bool { return x.kws[i][1] > identStart }) - 1
	if i < 0 {
		return ""
	}
	loc := x.kws[i]
	wordEnd := keywordWordEnd(x.text, loc)
	if wordEnd < identStart-keywordLookback {
		return ""
	}
	for _, m := range keywordStop.FindAllStringIndex(x.text[wordEnd:identStart], -1) {
		if !withinAnySpan(wordEnd+m[0], x.urls) {
			return ""
		}
	}
	// Any identifier starting in [keyword end, identStart) cancels it.
	j := sort.SearchInts(x.starts, loc[1])
	if j < len(x.starts) && x.starts[j] < identStart {
		return ""
	}
	kw := strings.TrimSpace(x.text[loc[0]:loc[1]])
	kw = strings.TrimSuffix(kw, ":")
	return strings.ToLower(strings.Join(strings.Fields(kw), " "))
}

// section returns the class of the nearest Markdown heading before offset,
// of any kind, and its text: a later heading such as "## Changes" ends a
// Background section. A heading on the reference's own line counts only as
// far as the text before the reference.
func (x *textIndex) section(offset int) (Section, string) {
	if offset <= 0 || offset > len(x.text) {
		return SectionNone, ""
	}
	i := sort.Search(len(x.headings), func(i int) bool { return x.headings[i][0] >= offset }) - 1
	for ; i >= 0; i-- {
		m := x.headings[i]
		if m[1] <= offset {
			return sectionOf(x.text[m[2]:m[3]])
		}
		// The reference sits on the heading's line: read the line up to it.
		if text, ok := headingUpTo(x.text, m[0], offset); ok {
			return sectionOf(text)
		}
	}
	return SectionNone, ""
}

// headingUpTo reads the heading line starting at start as headingPattern
// would read text[start:offset], without a regex pass over the line, so many
// references on one heading line cost no more than one: it returns the
// heading text, and false when the cut line is not a heading.
func headingUpTo(text string, start, offset int) (string, bool) {
	i := start
	for i < offset && i-start < 3 && text[i] == ' ' {
		i++
	}
	h := i
	for i < offset && text[i] == '#' {
		i++
	}
	if i == h || i-h > 6 {
		return "", false
	}
	rest := text[i:offset]
	if len(rest) < 2 || (rest[0] != ' ' && rest[0] != '\t') {
		return "", false
	}
	g := strings.TrimLeft(rest, " \t")
	if g == "" {
		// Only white space: the regex takes one blank as the text.
		return rest[len(rest)-1:], true
	}
	g = strings.TrimRight(g, " \t")
	if trimmed := strings.TrimRight(g, "#"); trimmed != g && trimmed != "" && strings.TrimRight(trimmed, " \t") != trimmed {
		g = strings.TrimRight(trimmed, " \t")
	}
	return g, true
}

// identifierStarts returns the start of every identifier match in text, the
// boundaries textIndex.keyword does not look across. A "#N" inside a URL is
// not one, since it is not a candidate. urls is anyURLSpans(text).
func identifierStarts(text string, urls [][2]int) []int {
	var out []int
	for _, re := range []*regexp.Regexp{keyPattern, jiraCloudURLPattern, linearURLPattern, githubURLPattern, gitlabURLPattern, hashRefPattern} {
		for _, loc := range re.FindAllStringIndex(text, -1) {
			if re == hashRefPattern && withinAnySpan(loc[0], urls) {
				continue
			}
			out = append(out, loc[0])
		}
	}
	return out
}

// convCommitIntent maps a conventional-commit type to the intent its scope
// key takes: fix closes, feat delivers, any other type refers.
func convCommitIntent(typ string) Intent {
	switch typ {
	case "fix":
		return IntentFixes
	case "feat":
		return IntentImplements
	}
	return IntentReferenceOnly
}

// scanInput is what one text scan reads.
type scanInput struct {
	side       Side
	vendor     Vendor
	title      string
	body       string
	branch     string
	repository string
	bareKeys   Vendor
}

// scanText runs the text generator over one entity. Each scanned field is
// clamped to MaxTextBytes first, so the work the scan does is bounded by the
// limit rather than by what the caller handed it.
func scanText(in scanInput) Extraction {
	rawTitle, titleCut := clampText(in.title)
	rawBody, bodyCut := clampText(in.body)
	branchText, branchCut := clampText(in.branch)
	title := noBreakSpaces.Replace(rawTitle)
	out := Extraction{TitleFlags: classifyTitle(title), Truncated: titleCut || bodyCut || branchCut}
	body := stripBody(rawBody)
	add := func(c Candidate) {
		c.Signal = SignalText
		out.Candidates = append(out.Candidates, c)
	}
	titleField, bodyField := FieldPRTitle, FieldPRBody
	if in.side == SideIssue {
		titleField, bodyField = FieldIssueTitle, FieldIssueBody
	}

	// A "#N" or "path#N" reference names an issue on the code host the
	// record lives on.
	hashNS := VendorGitHub
	if in.vendor == VendorGitLab {
		hashNS = VendorGitLab
	}
	scan := func(text string, field SourceField) {
		if text == "" {
			return
		}
		isBody := field == FieldPRBody || field == FieldIssueBody
		idx := newTextIndex(text, isBody)
		// at classifies one match: its keyword, intent and section. A body
		// match under a negative heading is context whatever its keyword.
		at := func(start int) (string, Intent, Section, string) {
			kw := idx.keyword(start)
			intent := intentForKeyword(kw)
			section, heading := SectionNone, ""
			if isBody {
				section, heading = idx.section(start)
				if section == SectionNegative {
					intent = IntentReferencesContext
				}
			}
			return kw, intent, section, heading
		}
		urlSpans := namespacedURLSpans(text)
		urls := anyURLSpans(text)
		for _, loc := range keyPattern.FindAllStringIndex(text, -1) {
			start, end := loc[0], loc[1]
			if withinAnySpan(start, urlSpans) {
				continue
			}
			kw, intent, section, heading := at(start)
			add(Candidate{
				Identifier: text[start:end], Field: field, Heading: heading, Intent: intent,
				Namespace: in.bareKeys, Section: section, Keyword: kw, Offset: start, Length: end - start,
			})
		}
		for _, u := range []struct {
			re *regexp.Regexp
			ns Vendor
		}{{jiraCloudURLPattern, VendorJira}, {linearURLPattern, VendorLinear}} {
			for _, loc := range u.re.FindAllStringSubmatchIndex(text, -1) {
				start := loc[0]
				kw, intent, section, heading := at(start)
				add(Candidate{
					Identifier: text[loc[2]:loc[3]], Field: field, Heading: heading, Intent: intent, Namespace: u.ns,
					Section: section, Keyword: kw, Offset: start, Length: loc[1] - start,
				})
			}
		}
		for _, loc := range gitlabURLPattern.FindAllStringSubmatchIndex(text, -1) {
			start := loc[0]
			if text[loc[4]:loc[5]] == "merge_requests" {
				continue
			}
			kw, intent, section, heading := at(start)
			add(Candidate{
				Identifier: text[loc[2]:loc[3]] + "#" + text[loc[6]:loc[7]], Field: field, Heading: heading, Intent: intent,
				Namespace: VendorGitLab, Section: section, Keyword: kw, Offset: start, Length: loc[1] - start,
			})
		}
		for _, loc := range githubURLPattern.FindAllStringSubmatchIndex(text, -1) {
			owner, repo, kind, num := text[loc[2]:loc[3]], text[loc[4]:loc[5]], text[loc[6]:loc[7]], text[loc[8]:loc[9]]
			ident := owner + "/" + repo + "#" + num
			if kind == "pull" {
				ident = owner + "/" + repo + "#PR-" + num
			}
			kw, intent, section, heading := at(loc[0])
			add(Candidate{
				Identifier: ident, Field: field, Heading: heading, Intent: intent, Namespace: VendorGitHub,
				Section: section, Keyword: kw, Offset: loc[0], Length: loc[1] - loc[0],
			})
		}
		for _, loc := range hashRefPattern.FindAllStringSubmatchIndex(text, -1) {
			start := loc[0]
			if withinAnySpan(start, urls) {
				// A URL's fragment, such as page#3, names no issue.
				continue
			}
			num := text[loc[4]:loc[5]]
			var ident string
			switch {
			case loc[2] != -1:
				ident = text[loc[2]:loc[3]] + "#" + num
			case in.repository != "":
				ident = in.repository + "#" + num
			default:
				continue
			}
			kw, intent, section, heading := at(start)
			add(Candidate{
				Identifier: ident, Field: field, Heading: heading, Intent: intent, Namespace: hashNS,
				Section: section, Keyword: kw, Offset: start, Length: loc[1] - start,
			})
		}
	}

	scan(title, titleField)
	scan(body, bodyField)
	if in.side == SidePR && rawTitle != "" {
		if m := convCommitPattern.FindStringSubmatch(rawTitle); m != nil {
			scope := strings.TrimSpace(m[2])
			if keyPattern.MatchString(scope) {
				add(Candidate{
					Identifier: scope, Field: titleField, Intent: convCommitIntent(m[1]), Namespace: in.bareKeys,
					Section: SectionNone, Keyword: "conv_commit", Offset: -1,
				})
			}
		}
	}
	if in.side == SidePR && branchText != "" {
		seen := map[string]bool{}
		branch := func(raw string) {
			key := strings.ToUpper(raw)
			if seen[key] {
				return
			}
			seen[key] = true
			add(Candidate{
				Identifier: key, Field: FieldBranch, Intent: IntentBare, Namespace: in.bareKeys,
				Section: SectionNone, Offset: -1, CaseFolded: key != raw,
			})
		}
		for _, k := range branchKeys(branchUpperKeyPattern, branchText, 0) {
			branch(k)
		}
		for _, k := range branchKeys(branchKeyPattern, branchText, 1) {
			branch(k)
		}
	}
	return out
}

// nativeCandidates turns vendor-native links into candidates, sorted by key:
// each is a closing claim. A key is trimmed of white space here, once, so
// the resolver and the stores' re-arm read the same identifier.
func nativeCandidates(refs []NativeRef) []Candidate {
	sorted := make([]NativeRef, 0, len(refs))
	for _, r := range refs {
		r.Key = strings.TrimSpace(r.Key)
		sorted = append(sorted, r)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	out := make([]Candidate, 0, len(sorted))
	for _, r := range sorted {
		out = append(out, Candidate{
			Signal: SignalNative, Identifier: r.Key, Field: FieldNativeLink, Intent: IntentFixes,
			Namespace: nativeNamespace(r.Key, r.Source), Section: SectionNone, Offset: -1,
		})
	}
	return out
}

// nativeNamespace infers a native key's namespace from its shape: "#MR-" is
// GitLab; a "/" and a "#" is GitHub, unless GitLab recorded the link, since
// GitLab issue keys share GitHub's "path#N" shape; anything else is unknown.
// The source is not the namespace itself: a Linear attachment points at a
// code host.
func nativeNamespace(key string, source Vendor) Vendor {
	if strings.Contains(key, "#MR-") {
		return VendorGitLab
	}
	if strings.Contains(key, "/") && strings.Contains(key, "#") {
		if source == VendorGitLab {
			return VendorGitLab
		}
		return VendorGitHub
	}
	return VendorAny
}

// BareKeyNamespace picks the namespace for bare keys ("ENG-42") on pull
// requests from the trackers in use: Jira alone gives jira, Linear alone gives
// linear, both or neither give VendorAny, which resolves against any tracker.
// Trackers that do not mint bare keys (GitHub, GitLab issues) are ignored.
func BareKeyNamespace(trackers []Vendor) Vendor {
	var jira, linear bool
	for _, v := range trackers {
		switch namespaceForTracker(v) {
		case VendorJira:
			jira = true
		case VendorLinear:
			linear = true
		}
	}
	switch {
	case jira && !linear:
		return VendorJira
	case linear && !jira:
		return VendorLinear
	}
	return VendorAny
}

// namespaceForTracker is the bare-key namespace an issue's own tracker gives.
func namespaceForTracker(v Vendor) Vendor {
	if v == VendorJira || v == VendorLinear {
		return v
	}
	return VendorAny
}

// SignalSet says which signal types run for records from each vendor. A
// vendor it does not list runs none.
type SignalSet map[Vendor][]SignalType

// DefaultSignals runs text signals for every vendor, and native signals for
// GitHub (closing references both ways), GitLab (the closes_issues metadata,
// when the record carries it) and Linear (pull-request attachments).
// Bitbucket and Jira record no native link that adapter/vcs reads.
func DefaultSignals() SignalSet {
	return SignalSet{
		VendorGitHub:    {SignalText, SignalNative},
		VendorGitLab:    {SignalText, SignalNative},
		VendorBitbucket: {SignalText},
		VendorJira:      {SignalText},
		VendorLinear:    {SignalText, SignalNative},
	}
}

// Allows reports whether t runs for records from v.
func (s SignalSet) Allows(v Vendor, t SignalType) bool {
	for _, x := range s[v] {
		if x == t {
			return true
		}
	}
	return false
}

// ExtractPR runs stage 1 over a pull request. bareKeys is the namespace bare
// keys take (see BareKeyNamespace); signals says which generators run.
func ExtractPR(pr PR, bareKeys Vendor, signals SignalSet) Extraction {
	var out Extraction
	if signals.Allows(pr.Vendor, SignalText) {
		out = scanText(scanInput{
			side: SidePR, vendor: pr.Vendor, title: pr.Title, body: pr.Body, branch: pr.HeadBranch,
			repository: pr.Repository, bareKeys: bareKeys,
		})
	}
	if signals.Allows(pr.Vendor, SignalNative) {
		out.Candidates = append(out.Candidates, nativeCandidates(pr.NativeIssueRefs)...)
	}
	return out
}

// ExtractIssue runs stage 1 over an issue. Bare keys take the issue's own
// tracker as their namespace.
func ExtractIssue(is Issue, signals SignalSet) Extraction {
	var out Extraction
	if signals.Allows(is.Vendor, SignalText) {
		out = scanText(scanInput{
			side: SideIssue, vendor: is.Vendor, title: is.Title, body: is.Body, bareKeys: namespaceForTracker(is.Vendor),
		})
	}
	if signals.Allows(is.Vendor, SignalNative) {
		out.Candidates = append(out.Candidates, nativeCandidates(is.NativePRRefs)...)
	}
	return out
}
