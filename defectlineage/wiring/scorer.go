// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package wiring

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	connv1 "github.com/tetriz-ai/tetriz-connectors/gen/lineage/v1"
	connlineage "github.com/tetriz-ai/tetriz-connectors/sdk/lineage"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
)

// cacheType names the bucket a host cache keeps structural verdicts in.
const cacheType = "structural_verdict"

// Scorer is the third host service the connector framework asks for: it measures
// how close each candidate's change sits to the fix's, and submits the verdict.
//
// It carries only the code-context signal. The other four terms of the
// confidence score are read from what blame already stored, so a verdict
// arriving at the sink completes a score rather than replacing it.
type Scorer struct {
	cmp defectlineage.Comparator
	obs defectlineage.Observer
}

// ScorerParams configure a [Scorer].
type ScorerParams struct {
	// Comparator measures the proximity. Nil means [defectlineage.CoverageComparator],
	// which answers by file coverage and marks every verdict degraded. For a
	// measured syntax-tree rung pass astcompare.Comparator, building with
	// `-tags astcompare`; it is not the default because it needs a C toolchain,
	// and a scorer that silently required one would defeat the tag. The real
	// syntax-tree one, since a host that asked for this service wants a measured
	// rung rather than file coverage.
	Comparator defectlineage.Comparator
	// Observer receives the scorer's diagnostic events. Nil discards them.
	Observer defectlineage.Observer
}

// NewScorer builds the host scorer.
func NewScorer(p ScorerParams) *Scorer {
	cmp := p.Comparator
	if cmp == nil {
		cmp = defectlineage.CoverageComparator{}
	}
	return &Scorer{cmp: cmp, obs: p.Observer}
}

// ScorePendingFix scores every candidate of one claimed fix and submits each
// verdict.
//
// It never returns an error, because one candidate it cannot measure is not a
// failure: the comparison degrades to file coverage, the verdict says so, and
// the score is still written. A fix that produced no verdict at all would be
// left unscored forever, so the degraded path is the one that keeps the pipeline
// moving.
func (s *Scorer) ScorePendingFix(
	ctx context.Context, sink connlineage.BlameSink, cache connlineage.Cache,
	provider, orgID string,
	fetch connlineage.FileFetcher, firstParent connlineage.FirstParentFn,
	culpritLines connlineage.CulpritAddedLinesFn,
	fix *connv1.PendingStructuralScoring,
) {
	// The fix's anchors address the revision before the fix, because the lines it
	// removed exist only there.
	fixRev, err := firstParent(ctx, fix.GetFixCommitSha())
	if err != nil || fixRev == "" {
		s.debug("scorer_anchor_revision_failed", map[string]any{
			"fix": fix.GetFixCommitSha(), "provider": provider,
		})
		// Without the anchor revision nothing can be measured, so every candidate
		// is submitted as coverage rather than left unscored.
		for _, c := range fix.GetCandidates() {
			s.submit(ctx, sink, orgID, c, degradedFor(fix, c), 0)
		}
		return
	}

	// The fix side is the same for every candidate of this fix, so each anchor's
	// file is fetched once.
	fixFiles := make(map[string][]byte, len(fix.GetFixAnchors()))
	for _, a := range fix.GetFixAnchors() {
		if _, seen := fixFiles[a.GetPath()]; seen {
			continue
		}
		content, _, ferr := fetch(ctx, fixRev, a.GetPath())
		if ferr != nil || len(content) == 0 {
			continue
		}
		fixFiles[a.GetPath()] = content
	}

	for _, c := range fix.GetCandidates() {
		verdict, line := s.scoreCandidate(ctx, cache, fix, c, fixRev, fixFiles, fetch, culpritLines)
		s.submit(ctx, sink, orgID, c, verdict, line)
	}
}

// scoreCandidate measures one candidate, returning the verdict and the
// culprit-revision line it was measured at. A zero line means recovery failed,
// and the sink leaves the stored line unchanged rather than zeroing it.
func (s *Scorer) scoreCandidate(
	ctx context.Context, cache connlineage.Cache,
	fix *connv1.PendingStructuralScoring, c *connv1.PendingStructuralCandidate,
	fixRev string, fixFiles map[string][]byte,
	fetch connlineage.FileFetcher, culpritLines connlineage.CulpritAddedLinesFn,
) (defectlineage.Verdict, int32) {
	path := c.GetCandidatePath()
	if path == "" {
		return degradedFor(fix, c), 0
	}

	line := c.GetCandidateLine()
	if line <= 0 {
		// The provider's blame reported only the queried revision's position,
		// which drifts. The culprit's own diff says which lines it wrote.
		line = recoverLine(ctx, culpritLines, c.GetCandidateCommitSha(), path, removedTextOf(fix))
	}
	if line <= 0 {
		return degradedFor(fix, c), 0
	}

	candContent, candKey, err := fetch(ctx, c.GetCandidateCommitSha(), path)
	if err != nil || len(candContent) == 0 {
		return degradedFor(fix, c), line
	}

	best := defectlineage.Verdict{Level: defectlineage.MatchDifferentFile}
	measured := false
	for _, a := range fix.GetFixAnchors() {
		fixContent, ok := fixFiles[a.GetPath()]
		if !ok || a.GetStartLine() <= 0 {
			continue
		}
		key := verdictKey(s.comparatorVersion(), candKey, fixRev, a, c, line)
		if v, hit := s.cached(ctx, cache, key); hit {
			if v.Level > best.Level {
				best = v
			}
			measured = true
			continue
		}
		v, cerr := s.cmp.Compare(ctx,
			defectlineage.ChangedRegion{Path: a.GetPath(), StartLine: int(a.GetStartLine()), EndLine: int(a.GetEndLine())}, fixContent,
			defectlineage.ChangedRegion{Path: path, StartLine: int(line), EndLine: int(line)}, candContent)
		if cerr != nil {
			continue
		}
		s.store(ctx, cache, key, v)
		measured = true
		if v.Level > best.Level {
			best = v
		}
	}
	if !measured {
		return degradedFor(fix, c), line
	}
	return best, line
}

// recoverLine finds the line in the culprit's own revision to measure at, from
// the lines that commit added to path.
//
// It prefers an added line whose text matches one the fix removed, compared
// whitespace-insensitively like the walk's own matching. A culprit that touched
// several functions added lines in each, and the first of them is the right
// region only by luck — measuring there reports structural distance between the
// fix and an unrelated part of the same commit. The first substantive added line
// remains the fallback, which is the walk's own choice of representative.
//
// Zero means nothing could be recovered, and the caller degrades.
func recoverLine(ctx context.Context, culpritLines connlineage.CulpritAddedLinesFn, sha, path string, removed map[string]bool) int32 {
	if culpritLines == nil {
		return 0
	}
	added, err := culpritLines(ctx, sha, path)
	if err != nil || len(added) == 0 {
		return 0
	}
	var fallback int32
	for _, a := range added {
		n := strings.Join(strings.Fields(a.Text), " ")
		if n == "" || a.NewLine <= 0 {
			continue
		}
		if removed[n] {
			return int32(a.NewLine)
		}
		if fallback == 0 {
			fallback = int32(a.NewLine)
		}
	}
	return fallback
}

// removedTextOf collects the fix's removed lines, whitespace-normalised, so
// recoverLine can match the culprit's added lines against them.
func removedTextOf(fix *connv1.PendingStructuralScoring) map[string]bool {
	out := map[string]bool{}
	for _, a := range fix.GetFixAnchors() {
		for _, t := range a.GetRemovedText() {
			if n := strings.Join(strings.Fields(t), " "); n != "" {
				out[n] = true
			}
		}
	}
	return out
}

// degradedFor is the fallback when the comparison could not run: same-file when
// the candidate touched a file the fix touched, different-file otherwise, always
// flagged so a reader never mistakes it for a measured match.
func degradedFor(fix *connv1.PendingStructuralScoring, c *connv1.PendingStructuralCandidate) defectlineage.Verdict {
	for _, a := range fix.GetFixAnchors() {
		if a.GetPath() == c.GetCandidatePath() {
			return defectlineage.Verdict{Level: defectlineage.MatchSameFile, Degraded: true}
		}
	}
	return defectlineage.Verdict{Level: defectlineage.MatchDifferentFile, Degraded: true}
}

func (s *Scorer) submit(ctx context.Context, sink connlineage.BlameSink, orgID string,
	c *connv1.PendingStructuralCandidate, v defectlineage.Verdict, line int32) {
	if _, err := sink.SubmitStructuralVerdict(ctx, &connv1.SubmitStructuralVerdictRequest{
		OrganizationId:     orgID,
		CandidateLineageId: c.GetCandidateLineageId(),
		MatchLevel:         v.Level.String(),
		NodeType:           v.NodeType,
		EnclosingType:      v.EnclosingType,
		Degraded:           v.Degraded,
		CandidateLine:      line,
	}); err != nil {
		s.debug("scorer_submit_failed", map[string]any{
			"candidate": c.GetCandidateLineageId(), "error": err.Error(),
		})
	}
}

// comparatorVersion identifies the comparator a cached verdict came from. A
// comparator that declares a version supplies one; otherwise its type stands in,
// which still separates a coverage verdict from a measured one.
func (s *Scorer) comparatorVersion() string {
	if v, ok := s.cmp.(defectlineage.VersionedComparator); ok {
		return v.ComparatorVersion()
	}
	return fmt.Sprintf("%T", s.cmp)
}

// verdictKey identifies one comparison. It includes the comparator version, so a
// grammar upgrade or a change that can alter a rung invalidates what was cached
// rather than serving it stale.
func verdictKey(comparatorVersion, candContentKey, fixRev string, a *connv1.LineageBlameAnchor,
	c *connv1.PendingStructuralCandidate, line int32) string {
	return strings.Join([]string{
		comparatorVersion, fixRev, a.GetPath(),
		fmt.Sprint(a.GetStartLine()), fmt.Sprint(a.GetEndLine()),
		c.GetCandidateCommitSha(), candContentKey, c.GetCandidatePath(), fmt.Sprint(line),
	}, "\x00")
}

// cachedVerdict is the stored shape. It carries no source, only the rung and the
// node identities that explain it.
type cachedVerdict struct {
	Level         string `json:"level"`
	NodeType      string `json:"node_type"`
	EnclosingType string `json:"enclosing_type"`
	Degraded      bool   `json:"degraded"`
}

func (s *Scorer) cached(ctx context.Context, cache connlineage.Cache, key string) (defectlineage.Verdict, bool) {
	if cache == nil {
		return defectlineage.Verdict{}, false
	}
	got, err := cache.GetMany(ctx, cacheType, []string{key})
	if err != nil || len(got) == 0 {
		return defectlineage.Verdict{}, false
	}
	raw, ok := got[key]
	if !ok {
		return defectlineage.Verdict{}, false
	}
	var cv cachedVerdict
	if err := json.Unmarshal(raw, &cv); err != nil {
		return defectlineage.Verdict{}, false
	}
	level, known := defectlineage.ParseMatchLevel(cv.Level)
	if !known {
		// An unreadable cached rung is treated as a miss, so the comparison is
		// redone rather than scored from a value nothing can interpret.
		return defectlineage.Verdict{}, false
	}
	return defectlineage.Verdict{
		Level: level, NodeType: cv.NodeType,
		EnclosingType: cv.EnclosingType, Degraded: cv.Degraded,
	}, true
}

func (s *Scorer) store(ctx context.Context, cache connlineage.Cache, key string, v defectlineage.Verdict) {
	if cache == nil {
		return
	}
	raw, err := json.Marshal(cachedVerdict{
		Level: v.Level.String(), NodeType: v.NodeType,
		EnclosingType: v.EnclosingType, Degraded: v.Degraded,
	})
	if err != nil {
		return
	}
	// Best effort: a cache that will not take the verdict costs a re-comparison,
	// never a wrong score.
	_ = cache.PutMany(ctx, cacheType, map[string]json.RawMessage{key: raw})
}

// AddedLinesFromSingleFileDiff extracts the added lines of a diff scoped to one
// file.
func (*Scorer) AddedLinesFromSingleFileDiff(raw []byte) []connlineage.AddedLine {
	return toConnAddedLines(defectlineage.AddedLines(raw))
}

// AddedLinesForPath extracts the added lines of newPath from a multi-file diff.
func (*Scorer) AddedLinesForPath(raw []byte, newPath string) []connlineage.AddedLine {
	return toConnAddedLines(defectlineage.AddedLinesForPath(raw, newPath))
}

func toConnAddedLines(in []defectlineage.AddedLine) []connlineage.AddedLine {
	out := make([]connlineage.AddedLine, 0, len(in))
	for _, a := range in {
		out = append(out, connlineage.AddedLine{Text: a.Text, NewLine: a.NewLine})
	}
	return out
}

func (s *Scorer) debug(event string, fields map[string]any) {
	if s.obs != nil {
		s.obs.Debug(event, fields)
	}
}

// Compile-time proof that this satisfies the third host contract.
var _ connlineage.Scorer = (*Scorer)(nil)
