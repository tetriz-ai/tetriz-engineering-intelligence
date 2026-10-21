// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

import (
	"math/rand"
	"strings"
	"testing"
)

// naiveKeyword and naiveSection are the direct forms of textIndex's rules: a
// scan over every keyword and start, and the heading regex over the text up
// to the reference. TestTextIndex_MatchesTheDirectRules checks the index
// gives the same answers.
func naiveKeyword(text string, identStart int, kws [][]int, starts []int) string {
	var loc []int
	for _, k := range kws {
		if k[1] <= identStart {
			loc = k
		}
	}
	if loc == nil || keywordWordEnd(text, loc) < identStart-keywordLookback {
		return ""
	}
	// A sentence end inside a URL is not one.
	wordEnd := keywordWordEnd(text, loc)
	for _, m := range keywordStop.FindAllStringIndex(text[wordEnd:identStart], -1) {
		inURL := false
		for _, u := range urlPattern.FindAllStringIndex(text, -1) {
			end := u[1]
			for end > u[0] && strings.IndexByte(urlTrailingPunctuation, text[end-1]) >= 0 {
				end--
			}
			if wordEnd+m[0] >= u[0] && wordEnd+m[0] < end {
				inURL = true
			}
		}
		if !inURL {
			return ""
		}
	}
	for _, st := range starts {
		if st >= loc[1] && st < identStart {
			return ""
		}
	}
	kw := strings.TrimSuffix(strings.TrimSpace(text[loc[0]:loc[1]]), ":")
	return strings.ToLower(strings.Join(strings.Fields(kw), " "))
}

func naiveSection(body string, offset int) (Section, string) {
	if offset <= 0 || offset > len(body) {
		return SectionNone, ""
	}
	ms := headingPattern.FindAllStringSubmatchIndex(body[:offset], -1)
	if len(ms) == 0 {
		return SectionNone, ""
	}
	m := ms[len(ms)-1]
	return sectionOf(body[m[2]:m[3]])
}

func TestTextIndex_MatchesTheDirectRules(t *testing.T) {
	corpus := []string{
		"## Background\n\nPROJ-3 tried this.\n\n## Changes\n\nFixes PROJ-7.\n",
		"## Background:\n\nPROJ-3 tried this first.",
		"## Background ##\nPROJ-3",
		"## Fixes PROJ-1\nsee PROJ-2",
		"Fixes PROJ-1, see PROJ-2",
		"Fixes PROJ-1 and PROJ-2",
		"Fixed typo.\nPROJ-5 is next",
		"prefixes are documented for this PROJ-1",
		"Implements https://github.com/acme/web/issues/17 and refs acme/web#3",
		"# \n## x\n### Fixes:\nPROJ-9 #4",
	}
	words := []string{"Fixes ", "see ", "PROJ-1 ", "acme/web#2 ", "#3 ", "## Background\n", "## Changes\n", "## Fixes\n",
		". ", "\n\n", "\r", "\r\n", "; ", "! ", "? ", "   ## ", "## Background ##", "## Background:", "   ", "\t", "close: ", "https://github.com/acme/web/issues/4 ", "https://a.co/x(y). ", "https://a.co/p#3 ", "word ", "related to ", "xx", "#", "\n", "## "}
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 400; i++ {
		var b strings.Builder
		for j := rng.Intn(60); j > 0; j-- {
			b.WriteString(words[rng.Intn(len(words))])
		}
		corpus = append(corpus, b.String())
	}
	for _, text := range corpus {
		x := newTextIndex(text, true)
		kws, starts := keywordPattern.FindAllStringIndex(text, -1), identifierStarts(text, anyURLSpans(text))
		for pos := 0; pos <= len(text); pos++ {
			if got, want := x.keyword(pos), naiveKeyword(text, pos, kws, starts); got != want {
				t.Fatalf("keyword at %d in %q: %q, want %q", pos, text, got, want)
			}
			gs, gh := x.section(pos)
			ws, wh := naiveSection(text, pos)
			if gs != ws || gh != wh {
				t.Fatalf("section at %d in %q: %v %q, want %v %q", pos, text, gs, gh, ws, wh)
			}
		}
	}
}
