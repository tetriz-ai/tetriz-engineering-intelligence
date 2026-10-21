// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package diffsize

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/bmatcuk/doublestar/v4"
	sccprocessor "github.com/boyter/scc/v3/processor"
	enry "github.com/go-enry/go-enry/v2"
)

// The diff formats AnalyzeDiff and AnalyzeDiffWithExcludes read.
const (
	// FormatUnified is a git unified diff.
	FormatUnified = "unified"
	// FormatGitLabJSON is GitLab's merge-request diffs JSON array.
	FormatGitLabJSON = "json"
)

// analysisVersion is written into EffectiveLOC.AnalysisVersion. It changes
// when the classification or counting output changes materially, so a caller
// that stores results can tell which ones to recompute. "v2" counts comment
// lines for languages scc names differently, through sccAliases.
const analysisVersion = "v2"

// sccSetup builds scc's language database once, on the first analysis, so a
// program that imports the package only for MatchAny does not pay for it.
var sccSetup sync.Once

// EffectiveLOC is the effective lines-of-code breakdown of one diff. The JSON
// tags are stable, so callers can store the value and read it back.
type EffectiveLOC struct {
	// TotalSourceLinesAdded sums SourceLinesAdded over ByLanguage.
	TotalSourceLinesAdded int64 `json:"total_source_lines_added"`
	// TotalCommentLinesAdded sums CommentLinesAdded over ByLanguage.
	TotalCommentLinesAdded int64 `json:"total_comment_lines_added"`
	// TotalBlankLinesAdded sums BlankLinesAdded over ByLanguage.
	TotalBlankLinesAdded int64 `json:"total_blank_lines_added"`
	// TotalGeneratedLinesAdded counts the added lines of files go-enry flags
	// as generated and not vendored.
	TotalGeneratedLinesAdded int64 `json:"total_generated_lines_added"`
	// TotalVendorLinesAdded counts the added lines of vendored files.
	TotalVendorLinesAdded int64 `json:"total_vendor_lines_added"`
	// TotalDocLinesAdded counts the added lines of documentation files.
	TotalDocLinesAdded int64 `json:"total_doc_lines_added"`
	// ByLanguage maps a go-enry language name, or "Other", to that language's
	// counts. Generated, vendored, documentation and excluded files are not in it.
	ByLanguage map[string]*LanguageDetail `json:"by_language"`
	// ExcludedFiles lists the paths that matched an exclude pattern or were
	// generated or vendored, in diff order.
	ExcludedFiles []string `json:"excluded_files,omitempty"`
	// AnalysisVersion is "v2". It changes when the counts would change for
	// the same diff.
	AnalysisVersion string `json:"analysis_version"`
}

// LanguageDetail is the per-language breakdown in EffectiveLOC.ByLanguage.
type LanguageDetail struct {
	// SourceLinesAdded is scc's code count over the added lines.
	SourceLinesAdded int64 `json:"source_lines_added"`
	// CommentLinesAdded is scc's comment count over the added lines.
	CommentLinesAdded int64 `json:"comment_lines_added"`
	// BlankLinesAdded is scc's blank count over the added lines.
	BlankLinesAdded int64 `json:"blank_lines_added"`
	// FilesChanged is the number of files in this language the diff touches,
	// counting a file it only removes lines from. Deleted files are not
	// counted.
	FilesChanged int `json:"files_changed"`
}

// AnalyzeDiff processes a single diff (raw bytes plus its format) and returns
// the effective LOC breakdown. It is AnalyzeDiffWithExcludes with no exclude
// patterns.
func AnalyzeDiff(raw []byte, format string) (*EffectiveLOC, error) {
	loc, err := AnalyzeDiffWithExcludes(raw, format, nil)
	if err != nil {
		return nil, fmt.Errorf("analyzing diff: %w", err)
	}
	return loc, nil
}

// AnalyzeDiffWithExcludes processes a single diff and returns the effective
// LOC breakdown. format is FormatUnified or FormatGitLabJSON. Any other
// format is an error, and so is GitLab JSON that does not parse or is null.
// excludePatterns are globs in MatchAny's syntax; files matching any pattern
// are listed in ExcludedFiles without contributing to any total. An invalid
// pattern is an error; an empty one is skipped. A GitLab entry flagged
// "too_large" or "collapsed" whose path no pattern matches is an error
// wrapping ErrDiffExcluded, because its diff is not in the response. Non-blank
// FormatUnified input with no "diff --git ", "--- " or "+++ " line is an error
// too, while blank input is an empty diff. A combined diff ("diff --cc" or
// "diff --combined", from a merge commit) is an error: diff the merge against
// its first parent instead.
//
// A file counts in its language's FilesChanged when the diff touches it,
// including a file it only removes lines from; SourceLinesAdded and the other
// line counts take added lines only. A NUL byte in an added line is read as a
// space, so the line and the file's language are kept.
//
// Per-file flow: exclude globs first, then go-enry classifies the
// file (generated, vendor, documentation, language) and scc counts code,
// comment and blank lines among the added lines, using scc's comment syntax
// for the file (see sccLanguage). Files go-enry flags as
// generated or vendored are excluded from the language totals but listed in
// ExcludedFiles and added to one counter: TotalVendorLinesAdded for a vendored
// file (generated or not), else TotalGeneratedLinesAdded. Documentation files
// are counted in TotalDocLinesAdded only, with no language breakdown.
//
// go-enry sees a file's path and its added lines, never the whole file. An
// edit to an existing generated file, whose generator banner sits outside the
// hunk, therefore counts as source unless its path matches an exclude
// pattern. So does a file marked only by a generator go-enry does not know.
func AnalyzeDiffWithExcludes(raw []byte, format string, excludePatterns []string) (*EffectiveLOC, error) {
	for _, p := range excludePatterns {
		if p != "" && !doublestar.ValidatePattern(p) {
			return nil, fmt.Errorf("invalid exclude pattern %q", p)
		}
	}

	var files []FileDiff
	switch format {
	case FormatGitLabJSON:
		var err error
		if files, err = parseGitLabDiffs(raw, excludePatterns); err != nil {
			return nil, err
		}
	case FormatUnified:
		if !looksLikeUnifiedDiff(raw) {
			return nil, fmt.Errorf("not a unified diff: no %q, %q or %q line", "diff --git ", "--- ", "+++ ")
		}
		if header, ok := combinedDiffHeader(raw); ok {
			return nil, fmt.Errorf("combined diff (%q) from a merge commit is not supported: diff the merge against its first parent, as in git diff <merge>^1 <merge>", header)
		}
		files = ParseUnifiedDiff(raw)
	default:
		return nil, fmt.Errorf("unknown diff format %q (want %q or %q)", format, FormatUnified, FormatGitLabJSON)
	}

	sccSetup.Do(sccprocessor.ProcessConstants)

	result := &EffectiveLOC{
		ByLanguage:      make(map[string]*LanguageDetail),
		AnalysisVersion: analysisVersion,
	}

	for _, f := range files {
		// An exclude glob short-circuits enry and scc: these paths never count.
		if MatchAny(f.Path, excludePatterns) {
			result.ExcludedFiles = append(result.ExcludedFiles, f.Path)
			continue
		}

		// go-enry and scc stop reading at a NUL byte, so it is read as a
		// space. content is the added lines only, so a generator banner
		// outside the hunk is not seen (see the function comment).
		content := []byte(strings.ReplaceAll(f.NewContent, "\x00", " "))
		isGenerated := enry.IsGenerated(f.Path, content)
		isVendor := enry.IsVendor(f.Path)
		isDoc := enry.IsDocumentation(f.Path)

		if isGenerated || isVendor {
			result.ExcludedFiles = append(result.ExcludedFiles, f.Path)
			if isVendor {
				result.TotalVendorLinesAdded += int64(f.Additions)
			} else {
				result.TotalGeneratedLinesAdded += int64(f.Additions)
			}
			continue
		}
		if isDoc {
			result.TotalDocLinesAdded += int64(f.Additions)
			continue
		}

		lang := enry.GetLanguage(f.Path, content)
		if lang == "" {
			lang = "Other"
		}

		job := &sccprocessor.FileJob{
			Filename: f.Path,
			Language: sccLanguage(lang),
			Content:  content,
			Bytes:    int64(len(content)),
		}
		sccprocessor.CountStats(job)

		detail, ok := result.ByLanguage[lang]
		if !ok {
			detail = &LanguageDetail{}
			result.ByLanguage[lang] = detail
		}
		detail.SourceLinesAdded += job.Code
		detail.CommentLinesAdded += job.Comment
		detail.BlankLinesAdded += job.Blank
		detail.FilesChanged++

		result.TotalSourceLinesAdded += job.Code
		result.TotalCommentLinesAdded += job.Comment
		result.TotalBlankLinesAdded += job.Blank
	}

	return result, nil
}

// looksLikeUnifiedDiff reports whether raw is blank or has at least one line
// that starts a unified diff's file header. A rename-only diff has its
// "diff --git " line, so it passes.
func looksLikeUnifiedDiff(raw []byte) bool {
	if len(bytes.TrimSpace(raw)) == 0 {
		return true
	}
	found := false
	forEachLine(string(raw), func(line string) {
		if strings.HasPrefix(line, "diff --git ") || strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ ") {
			found = true
		}
	})
	return found
}

// combinedDiffHeader returns the first "diff --cc " or "diff --combined "
// line of raw, the header git writes for each file of a merge commit's
// combined diff. A hunk line starts with a space, "+", "-" or "@", so such a
// line is always a header.
func combinedDiffHeader(raw []byte) (string, bool) {
	var header string
	forEachLine(string(raw), func(line string) {
		if header == "" && (strings.HasPrefix(line, "diff --cc ") || strings.HasPrefix(line, "diff --combined ")) {
			header = line
		}
	})
	return header, header != ""
}

// sccLanguage returns the name scc counts a file under. scc keys its comment
// syntax by its own language names, and a name it does not know gets no
// comment syntax, so every non-blank line counts as source. go-enry's name is
// kept when scc knows it. Otherwise sccAliases gives scc's name for the same
// language, and a name with no alias is passed on unchanged. scc's own
// detection from the file name is never used: for an extension two languages
// share, it can pick an unrelated language whose comment syntax turns code
// into comments.
func sccLanguage(enryLang string) string {
	if sccKnows(enryLang) {
		return enryLang
	}
	if alias, ok := sccAliases[enryLang]; ok {
		return alias
	}
	return enryLang
}

// sccAliases maps a go-enry language name scc does not know to scc's name for
// the same language. A pair belongs here only when scc's comment syntax is
// the language's own, so that no line of code reads as a comment.
// TestSccAliases_CountOneCommentAndOneCodeLine counts a comment line and a
// code line for every pair. A language whose scc counterpart reads code as a
// comment has no pair, and counts every non-blank line as source.
var sccAliases = map[string]string{
	"AIDL":               "Android Interface Definition Language",
	"AutoHotkey":         "AutoHotKey",
	"Awk":                "AWK",
	"BASIC":              "Basic",
	"Batchfile":          "Batch",
	"BitBake":            "Bitbake",
	"Blade":              "Blade template",
	"Cabal Config":       "Cabal",
	"Closure Templates":  "Closure Template",
	"Fortran Free Form":  "Fortran Modern",
	"FreeMarker":         "Freemarker Template",
	"Gherkin":            "Gherkin Specification",
	"Graphviz (DOT)":     "DOT",
	"HTML+ERB":           "Ruby HTML",
	"HTML+Razor":         "Razor",
	"Haml":               "HAML",
	"JSON with Comments": "JSONC",
	"Jai":                "JAI",
	"Java Properties":    "Properties File",
	"LLVM":               "LLVM IR",
	"Less":               "LESS",
	"Lex":                "LEX",
	"M4":                 "m4",
	"MoonBit":            "Moonbit",
	"Objective-C":        "Objective C",
	"Objective-C++":      "Objective C++",
	"Opa":                "Opalang",
	"PLSQL":              "PL/SQL",
	"PowerShell":         "Powershell",
	"Protocol Buffer":    "Protocol Buffers",
	"Pug":                "Jade",
	"Reason":             "ReasonML",
	"Rocq Prover":        "Coq",
	"SCSS":               "Sass",
	"SWIG":               "Swig",
	"Smarty":             "Smarty Template",
	"Standard ML":        "Standard ML (SML)",
	"Starlark":           "Bazel",
	"TSX":                "TypeScript",
	"Tcl":                "TCL",
	"Type Language":      "TL",
	"UnrealScript":       "Unreal Script",
	"UrWeb":              "Ur/Web",
	"VBA":                "Visual Basic for Applications",
	"VCL":                "Varnish Configuration",
	"Visual Basic .NET":  "Visual Basic",
	"WGSL":               "WebGPU Shading Language",
	"Xmake":              "XMake",
	"fish":               "Fish",
	"templ":              "Templ",
}

// sccKnows reports whether scc knows a language name.
func sccKnows(lang string) bool {
	if lang == "" {
		return false
	}
	sccprocessor.LanguageFeaturesMutex.Lock()
	defer sccprocessor.LanguageFeaturesMutex.Unlock()
	_, ok := sccprocessor.LanguageFeatures[lang]
	return ok
}
