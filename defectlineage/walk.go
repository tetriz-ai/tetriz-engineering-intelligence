// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// DefaultHopCap bounds the walk. Reaching it resolves to nothing, never the last
// commit walked to — that would be a confident wrong answer.
const DefaultHopCap = 3

// BlamedLine is one line of a file at a revision, tagged with the commit that
// owns it under blame and that commit's author. The author is captured here,
// trusting the provider's blame, rather than re-derived later from the commit.
type BlamedLine struct {
	Text        string
	OwnerSHA    string
	CommittedAt time.Time
	AuthorName  string
	AuthorEmail string
}

// Source is the walk's provider port.
//
// A provider whose blame is path-scoped and does not follow renames sees the
// path vanish across a rename and fails closed, producing no candidate, rather
// than resolving across it. Resolving across a rename — and the
// same-text-elsewhere ambiguity noted on [Walker.Walk] — would both need the
// owner commit's diff, which this walk does not consult.
type Source interface {
	// Lines returns the file at path as of revision, each line tagged with its
	// blame owner and author. An empty slice means the path does not exist at
	// that revision.
	Lines(ctx context.Context, revision, path string) ([]BlamedLine, error)
	// FirstParent returns sha's first-parent commit, or "" when sha is a root
	// commit.
	FirstParent(ctx context.Context, sha string) (string, error)
}

// CandidatePRResolver resolves a culprit commit to the provider pull-request
// number that introduced it. It returns (0, false) when the provider names no
// pull request for the commit.
//
// It never returns an error: resolution is best-effort, so a resolver reports a
// failure as "not resolved". A failed lookup must never fail the blame walk.
type CandidatePRResolver func(ctx context.Context, culpritSHA string) (prNumber int32, ok bool)

// WalkParams configure a [Walker]. The zero value is valid: it uses
// [DefaultHopCap] and discards every event.
type WalkParams struct {
	// HopCap bounds how many non-material commits one anchor may be walked
	// past. Zero means [DefaultHopCap]. A negative value is an error from
	// [NewWalker].
	HopCap int
	// Observer receives the walk's diagnostic events. Nil discards them.
	Observer Observer
}

// Walker runs the blame walk. It holds no per-fix state, so one Walker serves
// every fix and every provider concurrently.
type Walker struct {
	hopCap int
	obs    Observer
}

// NewWalker builds a Walker from p. It returns an error only for a negative
// HopCap, which would cap the walk below its own first step and silently
// resolve nothing.
func NewWalker(p WalkParams) (*Walker, error) {
	if p.HopCap < 0 {
		return nil, fmt.Errorf("defectlineage: HopCap %d is negative", p.HopCap)
	}
	hopCap := p.HopCap
	if hopCap == 0 {
		hopCap = DefaultHopCap
	}
	return &Walker{hopCap: hopCap, obs: orNop(p.Observer)}, nil
}

// Walk runs the blame walk for one claimed bug fix over a provider Source and
// assembles its outcome. A caller forwards the result to [Store.SubmitBlame].
//
// Step zero resolves the anchor: blame runs against the fix's FIRST PARENT,
// because the line the fix removed does not exist after the fix. Every anchor is
// then walked; the fix resolves when at least one anchor names a culprit, and is
// UNATTRIBUTED when all were blamed cleanly but none did. Provider failures
// surface as an error for the caller to map to a terminal state — the walk does
// not own the state machine.
func (w *Walker) Walk(ctx context.Context, src Source, fix *defectlineagev1.PendingLineageBlame) (defectlineagev1.DefectLineageState, []*defectlineagev1.LineageBlameResult, error) {
	anchorRev, err := src.FirstParent(ctx, fix.GetFixCommitSha())
	if err != nil {
		return defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_UNSPECIFIED, nil,
			fmt.Errorf("resolving anchor for fix %s: %w", fix.GetFixCommitSha(), err)
	}

	var results []*defectlineagev1.LineageBlameResult
	var firstAnchorErr error
	for _, a := range fix.GetLocationAnchors() {
		outcome, err := w.walkAnchor(ctx, src, anchorRev, a.GetPath(), a.GetRemovedText())
		if err != nil {
			// One anchor's provider error must not discard the culprits earlier
			// anchors already named — a fix resolves when ANY anchor names one.
			// Record the first error and leave this anchor unattributed; the loop
			// moves on, and the error only decides the outcome if NO anchor
			// resolves (below).
			if firstAnchorErr == nil {
				firstAnchorErr = fmt.Errorf("anchor %s:%d: %w", a.GetPath(), a.GetStartLine(), err)
			}
			w.obs.Debug("anchor_failed", map[string]any{
				"path": a.GetPath(), "start_line": a.GetStartLine(), "error": err.Error(),
			})
		} else if outcome != nil {
			outcome.StartLine = a.GetStartLine()
			results = append(results, outcome)
		}
	}

	results = strongestPerCulprit(results)

	if len(results) > 0 {
		// At least one anchor named a culprit. Surface firstAnchorErr alongside
		// the results — nil when every anchor was clean — rather than swallowing
		// it: the caller must be able to tell a partial resolve from a complete
		// one, because the failed anchor's culprit is not recovered once the fix
		// advances. What the caller does with that is its own policy; the walk's
		// job is to report both halves rather than present a partial answer as a
		// complete one.
		return defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_RESOLVED, results, firstAnchorErr
	}
	if firstAnchorErr != nil {
		// No culprit AND at least one anchor failed for a provider reason:
		// surface the error so the caller records a terminal failure, rather than
		// a false UNATTRIBUTED "we looked and found none".
		return defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_UNSPECIFIED, nil, firstAnchorErr
	}
	// Every anchor was blamed cleanly and none survived — a real answer, not a
	// failure.
	return defectlineagev1.DefectLineageState_DEFECT_LINEAGE_STATE_UNATTRIBUTED, nil, nil
}

// ResolveCandidatePRs populates each result's CandidatePrNumber from the
// provider's commit-to-pull-request resolver, best-effort. It dedupes by culprit
// sha so a culprit reached from several anchors costs one provider call, and it
// caches a miss so a repeated culprit is not re-queried. A zero result — no pull
// request, or a failed lookup — leaves the field 0, which a caller reads as
// "fall back to the commit's stored link". A nil resolver, meaning a provider
// with no such endpoint, is a no-op.
func (w *Walker) ResolveCandidatePRs(ctx context.Context, results []*defectlineagev1.LineageBlameResult, resolve CandidatePRResolver) {
	if resolve == nil {
		return
	}
	seen := make(map[string]int32, len(results))
	for _, r := range results {
		sha := r.GetCulpritSha()
		if sha == "" {
			continue
		}
		num, cached := seen[sha]
		if !cached {
			if n, ok := resolve(ctx, sha); ok {
				num = n
			}
			seen[sha] = num
		}
		if num > 0 {
			r.CandidatePrNumber = num
		}
	}
}

// walkAnchor traces one anchor back to the commit that materially wrote the
// removed line. It is the whole hop walk, and it never returns a commit it is
// not sure of. A nil result means the anchor produced no candidate — ambiguous,
// absent, or capped, all fail-closed outcomes that are answers, not errors.
//
// It pins the removed line at the anchor by content, which must be unique there,
// then follows it back: at each revision the owner blame reports is material if
// and only if the line's content is absent at that owner's parent. If the
// content is still there — carried, or only reformatted — the walk hops to that
// occurrence in the parent and asks again.
//
// Every case it cannot decide fails closed: the line is absent or not unique at
// a revision, the path does not exist at a parent (a rename this blame cannot
// cross), or the hop cap is reached.
//
// One residual remains. When the parent holds exactly one occurrence of the
// removed text, the walk treats it as the carried line. If that occurrence is an
// unrelated line of the same text — a bare `return nil`, a lone brace — the walk
// follows the wrong ancestry. Deciding this needs the owner commit's own diff,
// which this walk does not consult; it is the one path here that can yield a
// wrong culprit rather than a miss.
func (w *Walker) walkAnchor(ctx context.Context, src Source, anchorRev, path string, removedText []string) (*defectlineagev1.LineageBlameResult, error) {
	target, ok := representativeLine(removedText)
	if !ok {
		return nil, nil // nothing blameable in this anchor
	}
	wantWS := normalizeWS(target)

	lines, err := src.Lines(ctx, anchorRev, path)
	if err != nil {
		return nil, fmt.Errorf("blame %s@%s: %w", path, anchorRev, err)
	}
	if v := ClassifyContent(path, headContent(lines)); v != VerdictNotGenerated {
		// A generated file whose only signal is a banner in its body is invisible
		// to the path-stage filter in anchor extraction, but its content is in
		// hand here at no extra fetch. Its diff is not authored code, so it must
		// not name a culprit; fail closed like any undecidable anchor. Counted
		// because the drop is otherwise invisible in the result.
		w.obs.Count("generated_anchor_skipped", map[string]string{"detector": v.String()})
		return nil, nil
	}
	matched := matchLines(lines, wantWS)
	if len(matched) != 1 {
		// Absent (0) or not unique (>1) at the anchor: no single line to
		// attribute. A duplicated removed line is ordinary — a bare return, a
		// closing brace — and picking one would be a coin-flip, so the walk
		// refuses.
		return nil, nil
	}
	idx := matched[0]
	w.obs.Count("anchor_walk", nil)

	for hops := 0; ; {
		owner := lines[idx]
		if owner.OwnerSHA == "" {
			// Blame did not cover this line, for instance because the path was
			// absent at this revision.
			return nil, nil
		}
		parent, err := src.FirstParent(ctx, owner.OwnerSHA)
		if err != nil {
			return nil, fmt.Errorf("first parent of %s: %w", owner.OwnerSHA, err)
		}
		if parent == "" {
			// The owner is a root commit; nothing precedes it, so it wrote the line.
			return resolved(owner, path, hops), nil
		}

		parentLines, err := src.Lines(ctx, parent, path)
		if err != nil {
			return nil, fmt.Errorf("blame %s@%s: %w", path, parent, err)
		}
		if len(parentLines) == 0 {
			// The path does not exist at the parent — the file was created here,
			// or a rename this blame cannot follow lies between. Either way
			// authorship is undecidable, so the walk fails closed rather than
			// claim the owner wrote the line.
			return nil, nil
		}

		parentMatches := matchLines(parentLines, wantWS)
		switch {
		case len(parentMatches) == 0:
			// The line's content is absent before the owner, so the owner
			// introduced it.
			return resolved(owner, path, hops), nil
		case len(parentMatches) > 1:
			// More than one occurrence in the parent: which one this line
			// descends from is ambiguous, so the walk refuses rather than guess
			// an ancestry.
			return nil, nil
		}

		if hops == w.hopCap {
			// Reached the cap still on a carried line. Resolving to the last
			// commit walked to would be a confident wrong answer, so this anchor
			// yields nothing.
			w.obs.Count("hop_cap_reached", nil)
			return nil, nil
		}
		hops++
		// The parent's single occurrence is the carried line; follow it there.
		lines = parentLines
		idx = parentMatches[0]
	}
}

// normalizeWS collapses a line to its whitespace-insensitive form. A reformat
// that only re-indents a line changes its bytes without changing its meaning,
// and no blame endpoint offers a whitespace-insensitive flag, so the comparison
// strips whitespace itself.
func normalizeWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// representativeLine is the line the walk blames for an anchor: the first
// removed line that carries code. An anchor may remove several lines, but one
// candidate is attributed per anchor.
//
// Comment and punctuation-only lines are passed over because a fix that
// rewrites a stanza ordinarily removes the comment above the buggy line as
// well. Blaming the comment names whoever last reworded it, which is a
// confident wrong author on a common input — the one outcome this package
// prefers a miss to. If every removed line is a comment or a brace, the first
// of them is still returned: the removal is real and a comment's author is a
// better answer than none, where no code line competes with it.
func representativeLine(removedText []string) (string, bool) {
	first := ""
	found := false
	for _, l := range removedText {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if !found {
			first, found = l, true
		}
		if !isCommentOrPunctuation(l) {
			return l, true
		}
	}
	return first, found
}

// commentPrefixes open a line comment in the languages this package sees. A
// generous set rather than a per-language lookup: misreading a code line as a
// comment only moves the blame to the next removed line, while missing one
// returns to naming the comment's author.
var commentPrefixes = []string{"//", "#", "--", "/*", "*", "<!--", ";", "%", "\"\"\""}

// isCommentOrPunctuation reports whether a line carries no code: a comment, or
// only brackets, braces and separators (a lone "}" or ")," that a reformat
// moves freely).
func isCommentOrPunctuation(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return true
	}
	for _, p := range commentPrefixes {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return strings.IndexFunc(t, func(r rune) bool {
		return !strings.ContainsRune("{}()[]<>,;:.", r)
	}) < 0
}

// generatedHeadBytes bounds how much of a blamed file the generated-content
// check reads. A generated banner sits at the top of a file by convention, so a
// leading byte prefix is enough, and bounding by bytes rather than line count
// means a minified file's single very long leading line contributes at most this
// prefix instead of being copied in full for every anchor.
const generatedHeadBytes = 64 * 1024

// headContent assembles a newline-delimited byte prefix from the leading blamed
// lines, capped at generatedHeadBytes, as the input the generated-file detector
// reads. The buffer grows naturally rather than pre-reserving the cap, so a
// small file — the common case — allocates only its own size.
func headContent(lines []BlamedLine) []byte {
	var b []byte
	for _, l := range lines {
		remaining := generatedHeadBytes - len(b)
		if remaining <= 0 {
			break
		}
		t := l.Text
		if len(t) > remaining {
			t = t[:remaining]
		}
		b = append(b, t...)
		if len(b) >= generatedHeadBytes {
			break
		}
		b = append(b, '\n')
	}
	return b
}

// matchLines returns the indices of every line whose whitespace-normalized text
// equals wantWS.
func matchLines(lines []BlamedLine, wantWS string) []int {
	var out []int
	for i, l := range lines {
		if normalizeWS(l.Text) == wantWS {
			out = append(out, i)
		}
	}
	return out
}

// resolved builds a candidate for the owner reached after hops steps, carrying
// the author the blame reported.
func resolved(owner BlamedLine, path string, hops int) *defectlineagev1.LineageBlameResult {
	evidence := defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_DIRECT
	if hops > 0 {
		evidence = defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_BLAME_AFTER_HOPS
	}
	var committedAt *timestamppb.Timestamp
	if !owner.CommittedAt.IsZero() {
		committedAt = timestamppb.New(owner.CommittedAt)
	}
	var author *defectlineagev1.LineageBlameAuthor
	if owner.AuthorName != "" || owner.AuthorEmail != "" {
		author = &defectlineagev1.LineageBlameAuthor{
			Name:  owner.AuthorName,
			Email: owner.AuthorEmail,
		}
	}
	return &defectlineagev1.LineageBlameResult{
		Path:         path,
		CulpritSha:   owner.OwnerSHA,
		CommittedAt:  committedAt,
		HopsWalked:   int32(hops),
		EvidenceType: evidence,
		Author:       author,
		// CulpritPath is the anchor path: a path-scoped blame fails closed across
		// renames, so the culprit touched the same path. CulpritLine is left 0 —
		// blame reports only the queried-revision position, which drifts from the
		// culprit's own revision, so the scorer derives the line from the
		// culprit's diff, or degrades, rather than trust a drifted number.
		CulpritPath: path,
	}
}

// strongestPerCulprit collapses the results to one per culprit commit, keeping
// the strongest evidence each was reached by.
//
// Several anchors naming one culprit is the ordinary case for a multi-hunk fix.
// A store keys a candidate on (fix, culprit commit), so those collapse to one
// row there anyway — but on whichever result it wrote last, which means a
// culprit reached directly by one anchor and after a hop by another would be
// stored at whichever strength happened to come later in the diff. That is a
// confidence difference decided by hunk order rather than by evidence, so the
// choice is made here, deliberately, instead.
func strongestPerCulprit(results []*defectlineagev1.LineageBlameResult) []*defectlineagev1.LineageBlameResult {
	if len(results) < 2 {
		return results
	}
	best := make(map[string]*defectlineagev1.LineageBlameResult, len(results))
	order := make([]string, 0, len(results))
	for _, r := range results {
		sha := r.GetCulpritSha()
		prev, seen := best[sha]
		if !seen {
			best[sha] = r
			order = append(order, sha)
			continue
		}
		if strongerEvidence(r, prev) {
			best[sha] = r
		}
	}
	out := make([]*defectlineagev1.LineageBlameResult, 0, len(order))
	for _, sha := range order {
		out = append(out, best[sha])
	}
	return out
}

// strongerEvidence reports whether a was reached more directly than b. The
// evidence enum is declared strongest first, so a lower non-zero value wins;
// at equal strength the shorter walk does.
func strongerEvidence(a, b *defectlineagev1.LineageBlameResult) bool {
	av, bv := a.GetEvidenceType(), b.GetEvidenceType()
	switch {
	case av == defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_UNSPECIFIED:
		return false
	case bv == defectlineagev1.DefectLineageEvidenceType_DEFECT_LINEAGE_EVIDENCE_TYPE_UNSPECIFIED:
		return true
	case av != bv:
		return av < bv
	default:
		return a.GetHopsWalked() < b.GetHopsWalked()
	}
}
