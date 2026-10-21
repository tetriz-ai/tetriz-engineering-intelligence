// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage

import (
	"context"
	"errors"
	"fmt"
	"time"

	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// ScoringProvider is what a scoring pass needs from a provider: the revision a
// fix's anchors are addressed in, and the file content both sides of the
// comparison read.
type ScoringProvider interface {
	// FirstParent returns sha's first-parent commit, or "" for a root commit.
	// The fix's anchors address its landing commit's first parent, because the
	// removed lines exist only before the fix.
	FirstParent(ctx context.Context, sha string) (string, error)
	// FileAt returns a file's content at a revision. A nil result with no error
	// means the file is absent or too large to parse, which degrades the
	// comparison rather than failing it.
	FileAt(ctx context.Context, revision, path string) ([]byte, error)
}

// VersionedComparator is implemented by a [Comparator] whose verdicts can
// change between releases — a different grammar set, a changed rung rule. A
// caller that caches verdicts keys them on this, so an upgrade invalidates what
// was cached rather than serving a verdict the current comparator would not
// give. A comparator that does not implement it is keyed by its type alone.
type VersionedComparator interface {
	ComparatorVersion() string
}

// CulpritLineResolver is the optional half of [ScoringProvider]: it says which
// lines a culprit commit added to a file.
//
// It exists because the blame walk cannot supply the line itself. Blame reports
// a line's position in the revision it was asked about, which drifts from the
// culprit's own revision, so [Walker.Walk] leaves CandidateLine at 0 rather than
// store a drifted number. Without this lookup the scoring pass has no region to
// measure, degrades every candidate to same-file coverage, and never calls the
// Comparator it was given — so a caller who supplies one gets the same answer as
// a caller who does not.
//
// A provider that does not implement it keeps the previous behaviour: every
// candidate the walk produced is scored degraded.
type CulpritLineResolver interface {
	// CulpritAddedLines returns the lines sha added to path, in file order, as
	// [AddedLines] parses them from that commit's own diff. An empty result
	// means the commit added none, which degrades the comparison rather than
	// failing it.
	CulpritAddedLines(ctx context.Context, sha, path string) ([]AddedLine, error)
}

// CorroborationSource supplies the corroborating signals [ScoreE] reads. They
// come from pull-request text this package does not own, so a caller that has
// none configures no source and E falls to its defect-level bucket.
type CorroborationSource interface {
	For(ctx context.Context, ref FixRef, candidate *defectlineagev1.PendingStructuralCandidate) (CorroborationInput, error)
}

// ScoringParams configure a [ScoringPass].
type ScoringParams struct {
	// Store holds the candidate rows to score. Required.
	Store Store
	// Resolver supplies each fix's diff and merge time, so the fix side of the
	// comparison and the recency gap can be built. Required.
	Resolver FixResolver
	// Comparator measures structural proximity. Nil means [CoverageComparator],
	// which answers by path alone and marks every same-file verdict degraded.
	Comparator Comparator
	// Config calibrates the formula. The zero value means [DefaultConfig].
	Config Config
	// Corroboration supplies the signals behind E. Nil leaves the
	// candidate-specific bucket empty.
	Corroboration CorroborationSource
	// Anchors bound how the fix's anchors are extracted.
	Anchors Options
	// Lease is how long a scored-candidate claim is held before the candidate is
	// offered again. Zero means [DefaultLease].
	Lease time.Duration
	// Observer receives the pass's diagnostic events. Nil discards them.
	Observer Observer
}

// ScoringPass turns stored candidates into confidence scores. It reads what
// blame already stored and never re-blames, which is what makes recalibrating
// the formula a re-read rather than a re-fetch.
type ScoringPass struct {
	store   Store
	resolve FixResolver
	cmp     Comparator
	cfg     Config
	lease   time.Duration
	corrob  CorroborationSource
	anchors Options
	obs     Observer
}

// NewScoringPass builds a ScoringPass, erroring when a required dependency is
// missing.
func NewScoringPass(p ScoringParams) (*ScoringPass, error) {
	if p.Store == nil {
		return nil, errors.New("defectlineage: ScoringParams.Store is required")
	}
	if p.Resolver == nil {
		return nil, errors.New("defectlineage: ScoringParams.Resolver is required")
	}
	cmp := p.Comparator
	if cmp == nil {
		cmp = CoverageComparator{}
	}
	if err := p.Config.Validate(); err != nil {
		return nil, err
	}
	cfg := p.Config
	if cfg == (Config{}) {
		cfg = DefaultConfig()
	}
	anchors := p.Anchors
	if anchors.Observer == nil {
		anchors.Observer = p.Observer
	}
	lease := p.Lease
	if lease == 0 {
		lease = DefaultLease
	}
	return &ScoringPass{
		store: p.Store, resolve: p.Resolver, cmp: cmp, cfg: cfg, lease: lease,
		corrob: p.Corroboration, anchors: anchors, obs: orNop(p.Observer),
	}, nil
}

// ScoringStats is what one scoring pass did.
type ScoringStats struct {
	// Scored is how many candidate rows were given a score.
	Scored int
	// Degraded is how many of those rested on file coverage rather than a
	// measured structural proximity — the number to watch, because a degraded
	// score reads like a weak match and means "could not tell".
	Degraded int
	// FixesResolved is how many fixes became terminal because their last
	// candidate was scored.
	FixesResolved int
	// Failed is how many candidates could not be scored this pass.
	Failed int
	// Settled is how many fixes were terminated because their scoring input is
	// permanently gone.
	Settled int
}

// Run scores up to limit of the connector's unscored candidates.
//
// One candidate's failure never abandons the batch: it is left unscored for a
// later pass, and the fix simply does not resolve yet.
func (s *ScoringPass) Run(ctx context.Context, orgID, connectorID string, limit int, provider ScoringProvider) (ScoringStats, error) {
	var stats ScoringStats
	groups, err := s.store.ClaimUnscored(ctx, orgID, connectorID, limit, s.lease)
	if err != nil {
		return stats, fmt.Errorf("claiming unscored candidates for %s: %w", connectorID, err)
	}

	for _, g := range groups {
		in, err := s.resolve.Resolve(ctx, g.Ref)
		if err != nil {
			if errors.Is(err, ErrNoFixInput) {
				// Permanently gone. A fix here is past the blame stages, so the
				// blame drain can no longer settle it and its candidates can never
				// be scored — without a terminal write it stays non-terminal
				// forever.
				if _, serr := s.store.SettleUnscorable(ctx, g.Ref,
					defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_ANCHORS_EMPTY); serr != nil {
					s.obs.Debug("settle_unscorable_failed", map[string]any{"fix": g.Ref.BugFixPRID, "error": serr.Error()})
				} else {
					s.obs.Count("fix_unscorable", map[string]string{"connector": g.Ref.ConnectorID})
					stats.Settled++
				}
				continue
			}
			s.obs.Debug("scoring_resolve_failed", map[string]any{"fix": g.Ref.BugFixPRID, "error": err.Error()})
			stats.Failed += len(g.Candidates)
			continue
		}
		// Re-parsed rather than stored: the diff is already held, and a stored
		// copy would drift from it.
		anchors := ExtractAnchors(in, s.anchors)
		fixRev, err := provider.FirstParent(ctx, in.FixCommitSHA)
		if err != nil {
			s.obs.Debug("scoring_first_parent_failed", map[string]any{"fix": g.Ref.BugFixPRID, "error": err.Error()})
			stats.Failed += len(g.Candidates)
			continue
		}

		stored, err := s.store.ListCandidates(ctx, g.Ref.OrganizationID, g.Ref.BugFixPRID)
		if err != nil {
			s.obs.Debug("scoring_read_failed", map[string]any{"fix": g.Ref.BugFixPRID, "error": err.Error()})
			stats.Failed += len(g.Candidates)
			continue
		}

		for _, cand := range g.Candidates {
			scored, degraded, err := s.scoreOne(ctx, g.Ref, in, anchors, fixRev, cand, stored, provider)
			if err != nil {
				s.obs.Debug("scoring_candidate_failed", map[string]any{
					"fix": g.Ref.BugFixPRID, "candidate": cand.GetCandidateLineageId(), "error": err.Error(),
				})
				stats.Failed++
				continue
			}
			if scored {
				stats.Scored++
			}
			if degraded {
				stats.Degraded++
			}
		}

		resolved, err := s.store.ResolveIfFullyScored(ctx, g.Ref)
		if err != nil {
			s.obs.Debug("resolve_if_scored_failed", map[string]any{"fix": g.Ref.BugFixPRID, "error": err.Error()})
			continue
		}
		if resolved {
			stats.FixesResolved++
		}
	}
	return stats, nil
}

// scoreOne computes and writes one candidate's confidence score.
func (s *ScoringPass) scoreOne(
	ctx context.Context, ref FixRef, in FixInput, anchors []Anchor, fixRev string,
	cand *defectlineagev1.PendingStructuralCandidate, stored []Candidate, provider ScoringProvider,
) (scored, degraded bool, err error) {
	row, ok := findCandidate(stored, cand.GetCandidateLineageId())
	if !ok {
		// The row went away between the claim and now — purged, or re-driven. It
		// is not this pass's business to recreate it.
		return false, false, nil
	}

	verdict := s.compare(ctx, anchors, fixRev, cand, provider)

	var e float64
	var flags CorroborationFlags
	if s.corrob != nil {
		ci, cerr := s.corrob.For(ctx, ref, cand)
		if cerr != nil {
			// Corroboration is the lowest-weighted term, so a lookup failure
			// degrades the score rather than failing the candidate.
			s.obs.Debug("corroboration_failed", map[string]any{"fix": ref.BugFixPRID, "error": cerr.Error()})
		} else {
			e, flags = ScoreEWithFlags(ci, s.cfg)
		}
	}

	score, evidence := BuildScore(ScoreInputs{
		EvidenceType:  row.EvidenceType,
		CommittedAt:   row.CommittedAt,
		MergedAt:      in.MergedAt,
		Level:         verdict.Level,
		Degraded:      verdict.Degraded,
		NodeType:      verdict.NodeType,
		EnclosingType: verdict.EnclosingType,
		Corroboration: e,
		Flags:         flags,
	}, s.cfg)
	if err := s.store.WriteScore(ctx, ref.OrganizationID, cand.GetCandidateLineageId(), score, evidence, cand.GetCandidateLine()); err != nil {
		return false, false, fmt.Errorf("writing score: %w", err)
	}
	if verdict.Degraded {
		s.obs.Count("score_degraded", map[string]string{"connector": ref.ConnectorID})
	}
	return true, verdict.Degraded, nil
}

// compare measures one candidate against the fix's anchors, taking the strongest
// rung any anchor yields.
//
// It never fails the candidate: a file it cannot read or a comparison that
// errors degrades to same-file coverage, which is the honest answer — "could not
// tell" — rather than a missing score.
func (s *ScoringPass) compare(
	ctx context.Context, anchors []Anchor, fixRev string,
	cand *defectlineagev1.PendingStructuralCandidate, provider ScoringProvider,
) Verdict {
	candPath := cand.GetCandidatePath()
	if candPath == "" {
		// Without a path there is nothing to locate.
		return degradedFor(anchors, candPath)
	}
	candLine := int(cand.GetCandidateLine())
	if candLine <= 0 {
		// The walk leaves the line at 0 on purpose; the culprit's own diff is
		// what says which lines it wrote.
		candLine = recoverCandidateLine(ctx, provider, cand.GetCandidateCommitSha(), candPath, anchors)
	}
	if candLine <= 0 {
		return degradedFor(anchors, candPath)
	}
	candContent, err := provider.FileAt(ctx, cand.GetCandidateCommitSha(), candPath)
	if err != nil || len(candContent) == 0 {
		return degradedFor(anchors, candPath)
	}

	best := Verdict{Level: MatchDifferentFile}
	measured := false
	for _, a := range anchors {
		if a.RemovedStartLine == 0 {
			continue
		}
		fixContent, ferr := provider.FileAt(ctx, fixRev, a.OldPath)
		if ferr != nil || len(fixContent) == 0 {
			continue
		}
		v, cerr := s.cmp.Compare(ctx,
			ChangedRegion{Path: a.OldPath, StartLine: a.RemovedStartLine, EndLine: a.RemovedEndLine}, fixContent,
			ChangedRegion{Path: candPath, StartLine: candLine, EndLine: candLine}, candContent)
		if cerr != nil {
			continue
		}
		measured = true
		if v.Level > best.Level {
			best = v
		}
	}
	if !measured {
		return degradedFor(anchors, candPath)
	}
	return best
}

// recoverCandidateLine finds the line in the culprit's own revision to measure
// the candidate at, from the lines that commit added to the file.
//
// It prefers an added line whose text matches one the fix removed, compared
// whitespace-insensitively like the walk's own matching. A culprit that touched
// several functions added lines in each, and the first of them is only the right
// region by luck — measuring there reports structural distance between the fix
// and an unrelated part of the same commit. Falling back to the first
// substantive added line keeps an answer where nothing matches, which is the
// walk's own choice of representative and so describes the same change.
//
// Zero means no line could be recovered, and the caller degrades.
func recoverCandidateLine(ctx context.Context, provider ScoringProvider, sha, path string, anchors []Anchor) int {
	resolver, ok := provider.(CulpritLineResolver)
	if !ok || sha == "" {
		return 0
	}
	added, err := resolver.CulpritAddedLines(ctx, sha, path)
	if err != nil || len(added) == 0 {
		return 0
	}

	removed := map[string]bool{}
	for _, a := range anchors {
		for _, t := range a.RemovedText {
			if n := normalizeWS(t); n != "" {
				removed[n] = true
			}
		}
	}

	fallback := 0
	for _, l := range added {
		n := normalizeWS(l.Text)
		if n == "" || l.NewLine <= 0 {
			continue
		}
		if removed[n] {
			return l.NewLine
		}
		if fallback == 0 {
			fallback = l.NewLine
		}
	}
	return fallback
}

// degradedFor is the fallback verdict when the comparison could not run: the
// same-file rung when the candidate touched a file the fix touched, and
// different-file otherwise. It is always marked degraded, so a reader never
// mistakes it for a measured match.
func degradedFor(anchors []Anchor, candPath string) Verdict {
	for _, a := range anchors {
		if a.OldPath == candPath {
			return Verdict{Level: MatchSameFile, Degraded: true}
		}
	}
	return Verdict{Level: MatchDifferentFile, Degraded: true}
}

func findCandidate(rows []Candidate, id string) (Candidate, bool) {
	for _, r := range rows {
		if r.ID == id {
			return r, true
		}
	}
	return Candidate{}, false
}
