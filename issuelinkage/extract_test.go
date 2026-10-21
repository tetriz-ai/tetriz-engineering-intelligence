// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package issuelinkage

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// allSignals runs every generator for every vendor, so a test reads one
// generator's output without the default set's per-vendor choices.
func allSignals() SignalSet {
	s := SignalSet{}
	for _, v := range []Vendor{VendorGitHub, VendorGitLab, VendorBitbucket, VendorJira, VendorLinear} {
		s[v] = []SignalType{SignalText, SignalNative}
	}
	return s
}

func prText(title, body, branch string) Extraction {
	return ExtractPR(PR{Key: "p", Vendor: VendorGitHub, Title: title, Body: body, HeadBranch: branch}, VendorAny, allSignals())
}

func TestExtract(t *testing.T) {
	tests := []struct {
		name     string
		got      Extraction
		wantIDs  []string
		intents  []Intent
		keywords []string
		headings []string
	}{
		{name: "key in a title after a closing keyword", got: prText("Fixes PROJ-42", "", ""),
			wantIDs: []string{"PROJ-42"}, intents: []Intent{IntentFixes}, keywords: []string{"fixes"}},
		{name: "github issue URL in a body", got: prText("", "See https://github.com/acme/web/issues/17", ""),
			wantIDs: []string{"acme/web#17"}},
		{name: "key in a branch name", got: prText("", "", "feature/WEB-91-onboarding"),
			wantIDs: []string{"WEB-91"}},
		{name: "implements", got: prText("Implements PROJ-1", "", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentImplements}, keywords: []string{"implements"}},
		{name: "delivers", got: prText("Delivers PROJ-1", "", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentImplements}, keywords: []string{"delivers"}},
		{name: "completes", got: prText("Completes PROJ-1", "", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentImplements}, keywords: []string{"completes"}},
		{name: "ships", got: prText("Ships PROJ-1", "", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentImplements}, keywords: []string{"ships"}},
		{name: "see", got: prText("See PROJ-1", "", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentReferenceOnly}, keywords: []string{"see"}},
		{name: "refs", got: prText("Refs PROJ-1", "", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentReferenceOnly}, keywords: []string{"refs"}},
		{name: "cc", got: prText("cc PROJ-1", "", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentReferenceOnly}, keywords: []string{"cc"}},
		{name: "related to", got: prText("Related to PROJ-1", "", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentReferenceOnly}, keywords: []string{"related to"}},
		{name: "towards", got: prText("Towards PROJ-1", "", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentReferenceOnly}, keywords: []string{"towards"}},
		{name: "part of", got: prText("Part of PROJ-1", "", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentReferenceOnly}, keywords: []string{"part of"}},
		{name: "a colon after the keyword", got: prText("Fixes: PROJ-1", "", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}, keywords: []string{"fixes"}},
		{name: "the nearest keyword wins", got: prText("", "Fixes PROJ-1, see PROJ-2", ""),
			wantIDs: []string{"PROJ-1", "PROJ-2"}, intents: []Intent{IntentFixes, IntentReferenceOnly}, keywords: []string{"fixes", "see"}},
		{name: "a keyword does not reach past another identifier", got: prText("", "Fixes PROJ-1 and PROJ-2", ""),
			wantIDs: []string{"PROJ-1", "PROJ-2"}, intents: []Intent{IntentFixes, IntentBare}, keywords: []string{"fixes", ""}},
		{name: "a keyword sets a URL's intent", got: prText("", "Implements https://github.com/acme/web/issues/17", ""),
			wantIDs: []string{"acme/web#17"}, intents: []Intent{IntentImplements}, keywords: []string{"implements"}},
		{name: "a keyword sets a hash reference's intent", got: prText("", "See acme/web#17", ""),
			wantIDs: []string{"acme/web#17"}, intents: []Intent{IntentReferenceOnly}, keywords: []string{"see"}},
		{name: "a URL under a negative heading is context", got: prText("", "## Background\n\nFixes https://github.com/acme/web/issues/7", ""),
			wantIDs: []string{"acme/web#7"}, intents: []Intent{IntentReferencesContext}, keywords: []string{"fixes"}},
		{name: "a hash reference under a negative heading is context", got: prText("", "## Background\n\nFixes acme/web#7", ""),
			wantIDs: []string{"acme/web#7"}, intents: []Intent{IntentReferencesContext}, keywords: []string{"fixes"}},
		{name: "a later heading ends a Background section", got: prText("", "## Background\n\nPROJ-3 tried this.\n\n## Changes\n\nFixes PROJ-7.\n", ""),
			wantIDs: []string{"PROJ-3", "PROJ-7"}, intents: []Intent{IntentReferencesContext, IntentFixes}, keywords: []string{"", "fixes"}},
		{name: "a keyword is matched whole, not from the middle of a word", got: prText("", "prefixes are documented for this PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentBare}, keywords: []string{""}},
		{name: "a keyword word ending 30 bytes before the reference counts", got: prText("", "Fixes xxxxxxxxxxxxxxxxxxxxxxxxxxxx PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}, keywords: []string{"fixes"}},
		{name: "a keyword word ending 31 bytes before the reference does not", got: prText("", "Fixes xxxxxxxxxxxxxxxxxxxxxxxxxxxxx PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentBare}, keywords: []string{""}},
		{name: "a lower-case key starting a branch name", got: prText("", "", "eng-42-limiter"),
			wantIDs: []string{"ENG-42"}},
		{name: "a lower-case key starting a branch segment", got: prText("", "", "user/eng-42-limiter"),
			wantIDs: []string{"ENG-42"}},
		{name: "a key-shaped word inside a branch segment is not a key", got: prText("", "", "PROJ-1/fix-utf-8"),
			wantIDs: []string{"PROJ-1"}},
		{name: "tilde fences and quotes without a space are stripped", got: prText("", "~~~\nPROJ-9\n~~~\n>PROJ-8 quoted\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "comments are stripped before fences", got: prText("", "<!-- ``` -->\nFixes PROJ-1\n```", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a Background heading with a colon is context", got: prText("", "## Background:\n\nPROJ-3 tried this first.", ""),
			wantIDs: []string{"PROJ-3"}, intents: []Intent{IntentReferencesContext}},
		{name: "a Background heading with closing hashes is context", got: prText("", "## Background ##\n\nPROJ-3 tried this.", ""),
			wantIDs: []string{"PROJ-3"}, intents: []Intent{IntentReferencesContext}},
		{name: "a keyword does not reach past a sentence end", got: prText("", "Fixed typo.\nPROJ-5 is next", ""),
			wantIDs: []string{"PROJ-5"}, intents: []Intent{IntentBare}, keywords: []string{""}},
		{name: "a keyword does not reach past a blank line", got: prText("", "Closes\n\nPROJ-5", ""),
			wantIDs: []string{"PROJ-5"}, intents: []Intent{IntentBare}, keywords: []string{""}},
		{name: "a keyword reaches across one line break", got: prText("", "Closes\nPROJ-5", ""),
			wantIDs: []string{"PROJ-5"}, intents: []Intent{IntentFixes}, keywords: []string{"closes"}},
		{name: "a URL fragment is not a reference", got: prText("", "See https://docs.example.com/page#3 and Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "an indented quote is stripped", got: prText("", "  > Fixes PROJ-9\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a CRLF Background heading with a colon is context", got: prText("", "## Background:\r\nPROJ-3 tried this.", ""),
			wantIDs: []string{"PROJ-3"}, intents: []Intent{IntentReferencesContext}, headings: []string{"Background"}},
		{name: "a CRLF Background heading with closing hashes is context", got: prText("", "## Background ##\r\n\r\nPROJ-3", ""),
			wantIDs: []string{"PROJ-3"}, intents: []Intent{IntentReferencesContext}, headings: []string{"Background"}},
		{name: "a CRLF blank line stops a keyword", got: prText("", "Closes\r\n\r\nPROJ-5", ""),
			wantIDs: []string{"PROJ-5"}, intents: []Intent{IntentBare}, keywords: []string{""}},
		{name: "a lone CR is a line end", got: prText("", "## Background\rPROJ-3", ""),
			wantIDs: []string{"PROJ-3"}, intents: []Intent{IntentReferencesContext}, headings: []string{"Background"}},
		{name: "an indented quote of four spaces is code", got: prText("", "    > Fixes PROJ-9\n\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a quote indented three spaces is stripped", got: prText("", "   > Fixes PROJ-9\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a tab-indented line after a blank line is code", got: prText("", "Steps:\n\n\tFixes PROJ-9\n\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "an indented line inside a paragraph is text", got: prText("", "Steps:\n    Fixes PROJ-9", ""),
			wantIDs: []string{"PROJ-9"}, intents: []Intent{IntentFixes}},
		{name: "a quote indented four spaces inside a paragraph is text", got: prText("", "Steps:\n    > Fixes PROJ-9", ""),
			wantIDs: []string{"PROJ-9"}, intents: []Intent{IntentFixes}},
		{name: "a comment opened on a quote line is stripped whole", got: prText("", "> <!--\nFixes PROJ-1\n-->\nFixes PROJ-2", ""),
			wantIDs: []string{"PROJ-2"}, intents: []Intent{IntentFixes}},
		{name: "a heading indented three spaces", got: prText("", "   ## Background\nPROJ-3", ""),
			wantIDs: []string{"PROJ-3"}, intents: []Intent{IntentReferencesContext}, headings: []string{"Background"}},
		{name: "a line indented four spaces is not a heading", got: prText("", "Text\n    ## Background\nPROJ-3", ""),
			wantIDs: []string{"PROJ-3"}, intents: []Intent{IntentBare}, headings: []string{""}},
		{name: "Backgrounds is not a context heading", got: prText("", "## Backgrounds\nPROJ-3", ""),
			wantIDs: []string{"PROJ-3"}, intents: []Intent{IntentBare}, headings: []string{""}},
		{name: "a positive heading is recorded", got: prText("", "## Ticket\nPROJ-3", ""),
			wantIDs: []string{"PROJ-3"}, intents: []Intent{IntentBare}, headings: []string{"Ticket"}},
		{name: "a semicolon stops a keyword", got: prText("", "Fixes the login; PROJ-5 next", ""),
			wantIDs: []string{"PROJ-5"}, intents: []Intent{IntentBare}},
		{name: "an exclamation mark stops a keyword", got: prText("", "Fixes it! PROJ-5 next", ""),
			wantIDs: []string{"PROJ-5"}, intents: []Intent{IntentBare}},
		{name: "a question mark stops a keyword", got: prText("", "Fixes it? PROJ-5 next", ""),
			wantIDs: []string{"PROJ-5"}, intents: []Intent{IntentBare}},
		{name: "a white-space-only line is a blank line", got: prText("", "Closes\n \t\nPROJ-5", ""),
			wantIDs: []string{"PROJ-5"}, intents: []Intent{IntentBare}},
		{name: "a stop before the keyword does not cancel it", got: prText("", "e.g. fixes PROJ-1 and v1.2 fixes PROJ-2", ""),
			wantIDs: []string{"PROJ-1", "PROJ-2"}, intents: []Intent{IntentFixes, IntentFixes}},
		{name: "the lookback counts from the keyword word, not the space after it", got: prText("", "Fixes"+strings.Repeat(" ", 200)+"PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentBare}, keywords: []string{""}},
		{name: "a two-word keyword does not span a blank line", got: prText("", "Related\n\nto PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentBare}},
		{name: "a two-word keyword spans spaces", got: prText("", "Related   to PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentReferenceOnly}, keywords: []string{"related to"}},
		{name: "a URL fragment does not cancel a keyword", got: prText("", "Fixes https://localhost/p#3 PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a URL ends at a comma", got: prText("", "See https://docs.example.com/page,acme/web#3", ""),
			wantIDs: []string{"acme/web#3"}},
		{name: "a reference after a URL", got: prText("", "See https://docs.example.com/page acme/web#3", ""),
			wantIDs: []string{"acme/web#3"}},
		{name: "a leading underscore is not a key", got: prText("", "_PROJ-1 and PROJ-2", ""),
			wantIDs: []string{"PROJ-2"}},
		{name: "a key with an underscore in a branch", got: prText("", "", "my_proj-12-x"),
			wantIDs: []string{"MY_PROJ-12"}},
		{name: "a list item's indented continuation is text", got: prText("", "- Changes:\n\n    - Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}, keywords: []string{"fixes"}},
		{name: "an ordered item's indented paragraph is text", got: prText("", "1. Release notes\n\n    Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}, keywords: []string{"fixes"}},
		{name: "a tab-indented item paragraph is text", got: prText("", "* item\n\n\tCloses PROJ-2", ""),
			wantIDs: []string{"PROJ-2"}, intents: []Intent{IntentFixes}, keywords: []string{"closes"}},
		{name: "a line indented four past an item's text is code", got: prText("", "- a\n\n      Fixes PROJ-9\n\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a paragraph after a blank line ends a list", got: prText("", "- a\n\nText\n\n    Fixes PROJ-9\n\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a thematic break is not a list item", got: prText("", "* * *\n\n    Fixes PROJ-9\n\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "an indented line after a heading is code", got: prText("", "## Notes\n    Fixes PROJ-9\n\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}, headings: []string{""}},
		{name: "a key followed by an underscore in a branch", got: prText("", "", "PROJ-12_fix"),
			wantIDs: []string{"PROJ-12"}},
		{name: "a lower-case key followed by an underscore in a branch", got: prText("", "", "proj-12_fix"),
			wantIDs: []string{"PROJ-12"}},
		{name: "a branch starting with an underscore holds no key", got: prText("", "", "_proj-1-x"),
			wantIDs: nil},
		{name: "eight digits in a branch are not a key", got: prText("", "", "PROJ-12345678"),
			wantIDs: nil},
		{name: "a key followed by a letter in a branch is not a key", got: prText("", "", "PROJ-12x"),
			wantIDs: nil},
		{name: "an upper-case key inside a branch segment followed by an underscore", got: prText("", "", "fix-PROJ-12_x"),
			wantIDs: []string{"PROJ-12"}},
		{name: "a key in each branch segment", got: prText("", "", "user/proj-1/proj-2"),
			wantIDs: []string{"PROJ-1", "PROJ-2"}},
		{name: "a fence shown in indented code opens no fence", got: prText("", "Example:\n\n    ```\n\nFixes PROJ-1\n\n```js\nx\n```", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "inline backticks open no fence", got: prText("", "Run ```make``` and\nFixes PROJ-1\n```", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a fence indented to a list item's text is code", got: prText("", "1. Steps:\n\n    ```\n    Fixes PROJ-9\n    ```\n\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a fence line indented four columns does not close a fence", got: prText("", "```\n    ```\nFixes PROJ-9\n```\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "an unclosed fence runs to the end of the body", got: prText("", "Fixes PROJ-1\n```\nFixes PROJ-2", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a fence indented four columns inside a paragraph is text", got: prText("", "Steps:\n    ```\nFixes PROJ-1\n```", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "an item whose marker is followed by more than four spaces", got: prText("", "1.      code\n\n    Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "fenced code inside a list is code", got: prText("", "- Steps:\n  ```\n  Fixes PROJ-9\n  ```\n- Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a dot inside a URL does not stop a keyword", got: prText("", "Fixes https://docs.example.com/p#3 PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}, keywords: []string{"fixes"}},
		{name: "a URL ending at a comma does not stop a keyword", got: prText("", "Fixes https://example.com/a,PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}, keywords: []string{"fixes"}},
		{name: "a sentence end after a URL stops a keyword", got: prText("", "Fixes https://example.com/a. PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentBare}, keywords: []string{""}},
		{name: "a URL holds balanced parentheses", got: ExtractPR(PR{Key: "p", Vendor: VendorGitHub, Repository: "acme/web", Body: "See https://en.wikipedia.org/wiki/Foo_(bar)#3 done"}, VendorAny, allSignals()),
			wantIDs: nil},
		{name: "two CRs are a blank line", got: prText("", "Closes\r\rPROJ-5", ""),
			wantIDs: []string{"PROJ-5"}, intents: []Intent{IntentBare}, keywords: []string{""}},
		{name: "part of does not span a blank line", got: prText("", "Part\n\nof PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentBare}, keywords: []string{""}},
		{name: "a white-space-only line opens indented code", got: prText("", "Text\n  \n    Fixes PROJ-9\n\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "an indented lazy quote line is quoted", got: prText("", "> quote\n    Fixes PROJ-9\n\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "an indented line after a fence is code", got: prText("", "text\n```\ncode\n```\n    Fixes PROJ-9\n\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a no-break space is a space", got: prText("", "Fixes\u00a0PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}, keywords: []string{"fixes"}},
		{name: "a no-break space in a title is a space", got: prText("Fixes\u00a0PROJ-1", "", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}, keywords: []string{"fixes"}},
		{name: "CR CR LF is one line end", got: prText("", "Closes\r\r\nPROJ-5", ""),
			wantIDs: []string{"PROJ-5"}, intents: []Intent{IntentFixes}, keywords: []string{"closes"}},
		{name: "CRLF and LF mixed in one body", got: prText("", "## Background\r\nPROJ-3\n\n## Changes\r\nFixes PROJ-7\n", ""),
			wantIDs: []string{"PROJ-3", "PROJ-7"}, intents: []Intent{IntentReferencesContext, IntentFixes}, headings: []string{"Background", ""}},
		{name: "a keyword reaches across one CRLF", got: prText("", "Closes\r\nPROJ-5", ""),
			wantIDs: []string{"PROJ-5"}, intents: []Intent{IntentFixes}, keywords: []string{"closes"}},
		{name: "a tab-indented line at the body's start is code", got: prText("", "\tFixes PROJ-9\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a tab-indented line inside a paragraph is text", got: prText("", "Steps:\n\tFixes PROJ-9", ""),
			wantIDs: []string{"PROJ-9"}, intents: []Intent{IntentFixes}},
		{name: "a shorter run does not close a fence", got: prText("", "````\nFixes PROJ-9\n```\nFixes PROJ-9\n````\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a fence line with an info string does not close a fence", got: prText("", "```\nFixes PROJ-9\n```js\nFixes PROJ-9\n```\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a tilde run does not close a backtick fence", got: prText("", "```\nFixes PROJ-9\n~~~\nFixes PROJ-9\n```\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a tilde opener may hold a backtick", got: prText("", "~~~ `x`\nFixes PROJ-9\n~~~\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a shorter tilde run does not close a tilde fence", got: prText("", "~~~~\nFixes PROJ-9\n~~~\nFixes PROJ-9\n~~~~~\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a fence that opens a list item is code", got: prText("", "- ```\n  Fixes PROJ-9\n  ```\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "an unclosed fence after text is code", got: prText("", "text\n```\nFixes PROJ-9\n", ""),
			wantIDs: nil},
		{name: "an empty list item followed by a blank line ends", got: prText("", "- \n\n    Fixes PROJ-9", ""),
			wantIDs: nil},
		{name: "an opener holding a backtick is inline code, not a fence", got: prText("", "```npm i``` first\nFixes PROJ-1\n```", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a less indented line ends a fence inside a list item", got: prText("", "- a\n  ```\n  x\nFixes PROJ-1\n  ```", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a marker may end its line", got: prText("", "-\n  x\n\n    Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "an empty marker after a paragraph is not an item", got: prText("", "Text\n-\n  x\n\n    Fixes PROJ-9\n\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "an indented line after an item's fence is code", got: prText("", "- ```\n  x\n  ```\n      Fixes PROJ-9\n\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a tab after two spaces inside an item is the item's text", got: prText("", "- a\n\n  \tFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a space, a tab and two spaces past an item's text is code", got: prText("", "- a\n\n \t  Fixes PROJ-9", ""),
			wantIDs: nil},
		{name: "a question mark ending a URL stops a keyword", got: prText("", "Fixes https://a.co/x? PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentBare}},
		{name: "a full stop and an underscore ending a URL stop a keyword", got: prText("", "Fixes https://a.co/x._ PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentBare}},
		{name: "a marker followed by four spaces", got: prText("", "-    a\n\n      Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "an indented code block continues without a blank line", got: prText("", "Text\n\n    code\n    Fixes PROJ-9\n\nFixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "an ordered item with a parenthesis", got: prText("", "1) a\n\n    Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a tab after a marker", got: prText("", "-\ta\n\n        Fixes PROJ-9", ""),
			wantIDs: nil},
		{name: "an empty item's text starts one column past the marker", got: prText("", "-  \n  x\n\n      Fixes PROJ-9", ""),
			wantIDs: nil},
		{name: "a marker indented less ends the deeper items", got: prText("", "- a\n  - b\n    - c\n  1.    d\n\n      Fixes PROJ-9", ""),
			wantIDs: nil},
		{name: "a line at an item's text column stays in the item", got: prText("", "- a\n  1.   b\n\n       Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "two deep: text column", got: prText("", "- a\n  - b\n\n      Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "two deep: code", got: prText("", "- a\n  - b\n\n        Fixes PROJ-9", ""),
			wantIDs: nil},
		{name: "three deep: text column", got: prText("", "- a\n  - b\n    - c\n\n        Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "three deep: code", got: prText("", "- a\n  - b\n    - c\n\n          Fixes PROJ-9", ""),
			wantIDs: nil},
		{name: "a 10. item's text", got: prText("", "10. a\n\n     Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a 10. item's code", got: prText("", "10. a\n\n         Fixes PROJ-9", ""),
			wantIDs: nil},
		{name: "a 100. item's text", got: prText("", "100. a\n\n      Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a 100. item's code", got: prText("", "100. a\n\n          Fixes PROJ-9", ""),
			wantIDs: nil},
		{name: "a tilde fence inside a list is code", got: prText("", "- Steps:\n  ~~~\n  Fixes PROJ-9\n  ~~~\n- Fixes PROJ-1", ""),
			wantIDs: []string{"PROJ-1"}, intents: []Intent{IntentFixes}},
		{name: "a Markdown link's URL fragment is not a reference", got: ExtractPR(PR{Key: "p", Vendor: VendorGitHub, Repository: "acme/web", Body: "[x](https://a.co/p#3)"}, VendorAny, allSignals()),
			wantIDs: nil},
		{name: "an unbalanced parenthesis ends a URL", got: ExtractPR(PR{Key: "p", Vendor: VendorGitHub, Repository: "acme/web", Body: "See https://a.co/x)#3 done"}, VendorAny, allSignals()),
			wantIDs: []string{"acme/web#3"}},
		{name: "issue title", got: ExtractIssue(Issue{Key: "i", Vendor: VendorJira, Title: "Closes WEB-7"}, allSignals()),
			wantIDs: []string{"WEB-7"}, intents: []Intent{IntentFixes}, keywords: []string{"closes"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Len(t, tt.got.Candidates, len(tt.wantIDs))
			for i, want := range tt.wantIDs {
				c := tt.got.Candidates[i]
				assert.Equal(t, want, c.Identifier)
				assert.Equal(t, SignalText, c.Signal)
				if i < len(tt.intents) {
					assert.Equal(t, tt.intents[i], c.Intent)
				}
				if i < len(tt.keywords) {
					assert.Equal(t, tt.keywords[i], c.Keyword)
				}
				if i < len(tt.headings) {
					assert.Equal(t, tt.headings[i], c.Heading)
				}
			}
		})
	}
}

// TestExtract_BareKeyNamespace: bare keys (title, branch, conventional-commit
// scope) take the caller's namespace; a URL keeps its own.
func TestExtract_BareKeyNamespace(t *testing.T) {
	tests := []struct {
		name   string
		pr     PR
		ns     Vendor
		wantID string
		want   Vendor
	}{
		{"bare title key takes linear", PR{Title: "Fixes ENG-42"}, VendorLinear, "ENG-42", VendorLinear},
		{"bare branch key takes jira", PR{HeadBranch: "feature/WEB-91-x"}, VendorJira, "WEB-91", VendorJira},
		{"conventional-commit scope takes linear", PR{Title: "fix(ENG-7): retry"}, VendorLinear, "ENG-7", VendorLinear},
		{"no namespace leaves any", PR{Title: "Fixes ENG-42"}, VendorAny, "ENG-42", VendorAny},
		{"linear URL keeps linear", PR{Body: "see https://linear.app/team/issue/ENG-7/x"}, VendorJira, "ENG-7", VendorLinear},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.pr.Key, tt.pr.Vendor = "p", VendorGitHub
			found := false
			for _, c := range ExtractPR(tt.pr, tt.ns, allSignals()).Candidates {
				if c.Identifier == tt.wantID && c.Namespace == tt.want {
					found = true
				}
			}
			assert.True(t, found, "candidate %q in namespace %q", tt.wantID, tt.want)
		})
	}
}

// TestExtract_URLKeyIsNotAlsoABareKey: a key inside a Jira URL is emitted once,
// in Jira's namespace, not again in the bare-key namespace.
func TestExtract_URLKeyIsNotAlsoABareKey(t *testing.T) {
	ex := ExtractPR(PR{Key: "p", Vendor: VendorGitHub, Body: "Fixes https://example.atlassian.net/browse/ENG-7"}, VendorLinear, allSignals())
	var ns []Vendor
	for _, c := range ex.Candidates {
		if c.Identifier == "ENG-7" {
			ns = append(ns, c.Namespace)
		}
	}
	assert.Equal(t, []Vendor{VendorJira}, ns)
}

func TestExtract_PatternBank(t *testing.T) {
	has := func(ex Extraction, id string, ns Vendor) bool {
		for _, c := range ex.Candidates {
			if c.Identifier == id && c.Namespace == ns {
				return true
			}
		}
		return false
	}
	t.Run("jira cloud URL", func(t *testing.T) {
		assert.True(t, has(prText("", "Fixes https://example.atlassian.net/browse/WEB-99 today", ""), "WEB-99", VendorJira))
	})
	t.Run("linear URL", func(t *testing.T) {
		assert.True(t, has(prText("", "see https://linear.app/team/issue/ENG-7/x for context", ""), "ENG-7", VendorLinear))
	})
	t.Run("gitlab issue URL; a merge-request URL is skipped", func(t *testing.T) {
		ex := prText("", "https://gitlab.com/acme/team/web/-/issues/5 and https://gitlab.com/acme/web/-/merge_requests/6", "")
		assert.True(t, has(ex, "acme/team/web#5", VendorGitLab))
		assert.False(t, has(ex, "acme/web#6", VendorGitLab))
	})
	t.Run("a key with an underscore", func(t *testing.T) {
		assert.True(t, has(prText("", "Fixes MY_PROJ-12", ""), "MY_PROJ-12", VendorAny))
	})
	t.Run("a key with an underscore in a Jira URL", func(t *testing.T) {
		ex := ExtractPR(PR{Key: "p", Vendor: VendorGitHub, Body: "Fixes https://x.atlassian.net/browse/MY_PROJ-12"}, VendorLinear, allSignals())
		require.Len(t, ex.Candidates, 1)
		assert.Equal(t, VendorJira, ex.Candidates[0].Namespace)
		assert.Equal(t, IntentFixes, ex.Candidates[0].Intent)
	})
	t.Run("a key with an underscore in a Linear URL", func(t *testing.T) {
		assert.True(t, has(prText("", "see https://linear.app/team/issue/MY_ENG-7/x", ""), "MY_ENG-7", VendorLinear))
	})
	t.Run("hash references on a GitLab pull request are GitLab issues", func(t *testing.T) {
		ex := ExtractPR(PR{Key: "mr-1!1", Vendor: VendorGitLab, Repository: "grp/proj", Body: "Closes #7\n\nAlso closes grp/sub/proj#8"}, VendorAny, allSignals())
		assert.True(t, has(ex, "grp/proj#7", VendorGitLab))
		assert.True(t, has(ex, "grp/sub/proj#8", VendorGitLab))
	})
	t.Run("hash references", func(t *testing.T) {
		ex := ExtractPR(PR{Key: "p", Vendor: VendorGitHub, Repository: "acme/web", Body: "Fixes #12 and acme/api#3"}, VendorAny, allSignals())
		assert.True(t, has(ex, "acme/web#12", VendorGitHub))
		assert.True(t, has(ex, "acme/api#3", VendorGitHub))
		ex = ExtractPR(PR{Key: "p", Vendor: VendorGitHub, Body: "Fixes #12"}, VendorAny, allSignals())
		assert.Empty(t, ex.Candidates, "a bare #N needs the repository")
	})
	t.Run("conventional-commit scope", func(t *testing.T) {
		for title, want := range map[string]Intent{
			"fix(WEB-12): retry on 429": IntentFixes,
			"feat(WEB-12): add retry":   IntentImplements,
			"docs(WEB-12): explain":     IntentReferenceOnly,
			"chore(WEB-12): bump":       IntentReferenceOnly,
		} {
			found := false
			for _, c := range prText(title, "", "").Candidates {
				if c.Identifier == "WEB-12" && c.Keyword == "conv_commit" {
					found = true
					assert.Equal(t, want, c.Intent, title)
				}
			}
			assert.True(t, found, title)
		}
	})
	t.Run("negative heading", func(t *testing.T) {
		ex := prText("", "## Background\n\nThis was kicked off by WEB-7", "")
		require.NotEmpty(t, ex.Candidates)
		c := ex.Candidates[0]
		assert.Equal(t, SectionNegative, c.Section)
		assert.Equal(t, "Background", c.Heading)
		assert.Equal(t, IntentReferencesContext, c.Intent)
	})
	t.Run("positive heading", func(t *testing.T) {
		ex := prText("", "## Fixes\n\nWEB-7", "")
		require.NotEmpty(t, ex.Candidates)
		assert.Equal(t, SectionPositive, ex.Candidates[0].Section)
	})
	t.Run("title flags", func(t *testing.T) {
		for _, tt := range []struct{ title, want string }{
			{"WIP", "generic_title"}, {"WIP: add foo", "draft_prefix"}, {"Revert \"Add foo\"", "revert"},
		} {
			assert.Contains(t, prText(tt.title, "", "").TitleFlags, tt.want, tt.title)
		}
	})
	t.Run("strips comments, fenced code, quotes and trailers", func(t *testing.T) {
		body := "Fixes PROJ-1\n\n<!-- ignore PROJ-99 -->\n\n```go\n// also ignore PROJ-100\n```\n\n> quoted PROJ-200\n\nSigned-off-by: someone PROJ-300"
		ids := map[string]bool{}
		for _, c := range prText("", body, "").Candidates {
			ids[c.Identifier] = true
		}
		assert.Equal(t, map[string]bool{"PROJ-1": true}, ids)
	})
}

func TestBareKeyNamespace(t *testing.T) {
	tests := []struct {
		name     string
		trackers []Vendor
		want     Vendor
	}{
		{"jira only", []Vendor{VendorJira}, VendorJira},
		{"linear only", []Vendor{VendorLinear}, VendorLinear},
		{"both is any", []Vendor{VendorJira, VendorLinear}, VendorAny},
		{"neither is any", []Vendor{VendorGitHub}, VendorAny},
		{"none is any", nil, VendorAny},
		{"jira with a non-key tracker stays jira", []Vendor{VendorJira, VendorGitHub}, VendorJira},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, BareKeyNamespace(tt.trackers))
		})
	}
}

func TestNativeNamespace(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		source Vendor
		want   Vendor
	}{
		{"gitlab merge-request path key", "acme/web#MR-7", VendorLinear, VendorGitLab},
		{"nested gitlab path", "acme/team/web#MR-12", VendorGitLab, VendorGitLab},
		{"github pull-request key", "acme/web#PR-42", VendorGitHub, VendorGitHub},
		{"github issue key", "acme/web#8", VendorGitHub, VendorGitHub},
		{"gitlab issue key, source decides", "acme/web#17", VendorGitLab, VendorGitLab},
		{"unknown source defaults to github", "acme/web#17", VendorAny, VendorGitHub},
		{"unrecognised shape", "ENG-42", VendorGitLab, VendorAny},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, nativeNamespace(tt.key, tt.source))
		})
	}
}

// TestExtract_NativeLinks: each native key is a closing claim from the native
// link field, sorted by key.
func TestExtract_NativeLinks(t *testing.T) {
	ex := ExtractPR(PR{Key: "p", Vendor: VendorGitHub, NativeIssueRefs: []NativeRef{
		{Key: "acme/web#42", Source: VendorGitHub}, {Key: "acme/api#3", Source: VendorGitHub},
	}}, VendorAny, DefaultSignals())
	require.Len(t, ex.Candidates, 2)
	assert.Equal(t, "acme/api#3", ex.Candidates[0].Identifier)
	for _, c := range ex.Candidates {
		assert.Equal(t, SignalNative, c.Signal)
		assert.Equal(t, FieldNativeLink, c.Field)
		assert.Equal(t, IntentFixes, c.Intent)
	}
}

// TestDefaultSignals pins which generators run per vendor by default.
func TestDefaultSignals(t *testing.T) {
	s := DefaultSignals()
	assert.True(t, s.Allows(VendorGitHub, SignalNative))
	assert.True(t, s.Allows(VendorGitHub, SignalText))
	assert.True(t, s.Allows(VendorGitLab, SignalNative))
	assert.True(t, s.Allows(VendorLinear, SignalNative))
	assert.False(t, s.Allows(VendorBitbucket, SignalNative))
	assert.False(t, s.Allows(VendorJira, SignalNative))
	assert.False(t, s.Allows(Vendor("other"), SignalText), "an unlisted vendor runs nothing")
}

// TestExtract_ABareHashOnAnIssueIsSkippedLimit pins an inherent limit: an
// issue has no repository of its own, so a bare "#7" in it names nothing the
// engine can resolve and is not a candidate. The URL beside it still is.
func TestExtract_ABareHashOnAnIssueIsSkippedLimit(t *testing.T) {
	ex := ExtractIssue(Issue{Key: "i", Vendor: VendorGitHub, Body: "Fixed by https://github.com/acme/web/pull/7 and #7"}, allSignals())
	require.Len(t, ex.Candidates, 1)
	assert.Equal(t, "acme/web#PR-7", ex.Candidates[0].Identifier)
}

// TestExtract_WhatTheScanSkipsLimit pins what the text scan does not read.
// Each record also holds a reference the scan does read, so an extractor that
// read nothing would fail.
func TestExtract_WhatTheScanSkipsLimit(t *testing.T) {
	ids := func(ex Extraction) []string {
		var out []string
		for _, c := range ex.Candidates {
			out = append(out, c.Identifier)
		}
		return out
	}
	t.Run("a heading is a Markdown heading, so a plain Background line is not one", func(t *testing.T) {
		ex := ExtractIssue(Issue{Key: "i", Vendor: VendorJira, Body: "Background\nPROJ-1 came first"}, allSignals())
		require.Len(t, ex.Candidates, 1)
		assert.Equal(t, SectionNone, ex.Candidates[0].Section)
	})
	t.Run("a key-shaped word is a candidate", func(t *testing.T) {
		assert.Equal(t, []string{"UTF-8", "SHA-256"}, ids(prText("", "Encode as UTF-8 and hash with SHA-256", "")))
	})
	t.Run("a prefix past ten characters is not a key", func(t *testing.T) {
		assert.Equal(t, []string{"PROJ-1"}, ids(prText("", "ABCDEFGHIJK-1 and PROJ-1", "")))
	})
	t.Run("a GitLab !N merge-request reference is not scanned", func(t *testing.T) {
		ex := ExtractIssue(Issue{Key: "i", Vendor: VendorGitLab, Body: "Fixed by !12, see https://gitlab.com/acme/web/-/issues/3"}, allSignals())
		assert.Equal(t, []string{"acme/web#3"}, ids(ex))
	})
	t.Run("only github.com URLs are GitHub URLs", func(t *testing.T) {
		ex := prText("", "Fixes https://github.example.com/acme/web/issues/7 and https://github.com/acme/web/issues/8", "")
		assert.Equal(t, []string{"acme/web#8"}, ids(ex))
	})
}

// TestExtract_NegationIsAKeywordLimit pins a consequence of keyword sets: the
// keyword counts whatever words surround it, so a negated one still sets the
// intent.
func TestExtract_NegationIsAKeywordLimit(t *testing.T) {
	for text, want := range map[string]Intent{
		"This does not fix PROJ-3":        IntentFixes,
		"Fixed typo, unrelated to PROJ-2": IntentFixes,
		"Don't close PROJ-4 yet":          IntentFixes,
	} {
		ex := prText("", text, "")
		require.Len(t, ex.Candidates, 1, text)
		assert.Equal(t, want, ex.Candidates[0].Intent, text)
	}
}

// TestExtract_AHashNumberInProseIsAReferenceLimit pins what a "#N" outside a
// URL means: on a pull request with a repository it names that repository's
// issue N, even in prose such as "attempt #2".
func TestExtract_AHashNumberInProseIsAReferenceLimit(t *testing.T) {
	ex := ExtractPR(PR{Key: "p", Vendor: VendorGitHub, Repository: "acme/web", Body: "This is attempt #2 at the fix"}, VendorAny, allSignals())
	require.Len(t, ex.Candidates, 1)
	assert.Equal(t, "acme/web#2", ex.Candidates[0].Identifier)
}

// TestExtract_ASchemeLessURLFragmentIsAReferenceLimit pins what a fragment
// on a URL with no scheme means: the URL is not recognised, so its "path#N"
// is a candidate.
func TestExtract_ASchemeLessURLFragmentIsAReferenceLimit(t *testing.T) {
	ex := prText("", "Closes PROJ-9 per docs.example.com/page#3", "")
	var ids []string
	for _, c := range ex.Candidates {
		ids = append(ids, c.Identifier)
	}
	assert.Equal(t, []string{"PROJ-9", "docs.example.com/page#3"}, ids)
}

// TestExtract_OffsetsIndexTheScannedText pins what a body candidate's Offset
// and Length index: the scanned text, after line ends are normalised and
// comments, quotes and code are removed, not the raw body.
func TestExtract_OffsetsIndexTheScannedText(t *testing.T) {
	for _, tt := range []struct {
		name, body string
		offset     int
	}{
		{"CRLF", "Fixes\r\nPROJ-1", 6},
		{"comment", "<!-- PROJ-9 -->Fixes PROJ-1", 6},
		{"indented code", "    code\n   \nFixes PROJ-1", 8},
		{"no-break space", "Fixes\u00a0PROJ-1", 6},
		{"after a fence", "```\nx\n```\nFixes PROJ-1", 9},
		{"after a quote", "> q\nFixes PROJ-1", 7},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ex := prText("", tt.body, "")
			require.Len(t, ex.Candidates, 1)
			c := ex.Candidates[0]
			assert.Equal(t, tt.offset, c.Offset)
			assert.Equal(t, "PROJ-1", stripBody(tt.body)[c.Offset:c.Offset+c.Length])
		})
	}
}

// TestExtract_TitleOffsetsIndexTheScannedTitle: a title's offsets index the
// title with no-break spaces read as spaces.
func TestExtract_TitleOffsetsIndexTheScannedTitle(t *testing.T) {
	title := "Fixes\u00a0PROJ-1"
	ex := prText(title, "", "")
	require.Len(t, ex.Candidates, 1)
	c := ex.Candidates[0]
	assert.Equal(t, 6, c.Offset)
	assert.Equal(t, "PROJ-1", noBreakSpaces.Replace(title)[c.Offset:c.Offset+c.Length])
}

// TestExtract_TitleFlagsReadNoBreakSpacesAsSpaces: the title flags read the
// title as the scan does.
func TestExtract_TitleFlagsReadNoBreakSpacesAsSpaces(t *testing.T) {
	assert.Equal(t, []string{"draft_prefix"}, prText("WIP\u00a0thing", "", "").TitleFlags)
	assert.Equal(t, []string{"revert"}, prText("Revert\u00a0\"x\"", "", "").TitleFlags)
}

// TestExtract_LargeBodiesFinishInTime bounds the scan on large bodies: 64 KiB
// of references on one heading line, and 1 MiB each of URLs followed by a
// "#N", of nested list items, of fences and of URLs with parentheses. The
// bound catches a quadratic scan, which takes minutes on these inputs; it
// is deliberately far above the measurement, so a slow runner is not a
// failure. Every body is sized just under MaxTextBytes, the largest the
// engine will scan, so the test measures the real worst case whole.
func TestExtract_LargeBodiesFinishInTime(t *testing.T) {
	headingLine := "## " + strings.Repeat("PROJ-1 ", 64*1024/7)
	unit := "https://a.co/x #1 "
	urls := strings.Repeat(unit, 1024*1024/len(unit))
	var nested strings.Builder
	for i := 0; ; i++ {
		line := strings.Repeat(" ", (i%200)*2) + "- PROJ-1\n"
		if nested.Len()+len(line) > MaxTextBytes {
			break
		}
		nested.WriteString(line)
	}
	for name, body := range map[string]string{
		"64 KiB heading line":   headingLine,
		"1 MiB of URLs":         urls,
		"1 MiB of nested lists": nested.String(),
		"1 MiB of fences":       strings.Repeat("```\nPROJ-1\n```\nPROJ-2\n", 1<<20/22),
		"1 MiB of parentheses":  strings.Repeat("https://a.co/(x)(y) PROJ-1 ", 1<<20/27),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			start := time.Now()
			ex := ExtractPR(PR{Key: "p", Vendor: VendorGitHub, Repository: "acme/web", Body: body}, VendorAny, DefaultSignals())
			assert.NotEmpty(t, ex.Candidates)
			assert.False(t, ex.Truncated, "the body must be scanned whole for the bound to mean anything")
			assert.Less(t, time.Since(start), largeBodyBound)
		})
	}
}
