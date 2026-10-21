// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package wiring

import (
	"context"
	"errors"
	"fmt"
	"time"

	connv1 "github.com/tetriz-ai/tetriz-connectors/gen/lineage/v1"
	connlineage "github.com/tetriz-ai/tetriz-connectors/sdk/lineage"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	dlv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// The connector framework calls into its host for three things: a work list to
// claim fixes from and submit results to, a blame walk, and a structural scorer.
// It ships none of them, and refuses to run an operation whose host service is
// absent. This file supplies all three from the engine.
//
// The two sides speak the same messages under different generated types, because
// each repository owns its own contract. Re-using one repository's generated
// types in the other would register the same message names twice in the global
// registry and panic before main runs, so the translation below is the price of
// the two repositories being independently buildable.

// BlameSink is the host work list the connector framework claims from.
//
// It needs a resolver as well as a store, because claiming a fix means handing
// the walk that fix's anchors, and anchors are parsed from a diff the engine
// does not own.
type BlameSink struct {
	store    defectlineage.Store
	resolver defectlineage.FixResolver
	lease    time.Duration
	anchors  defectlineage.Options
	maxFiles int
	cfg      defectlineage.Config
	corrob   defectlineage.CorroborationSource
}

// BlameSinkParams configure a [BlameSink].
type BlameSinkParams struct {
	// Store holds the work list and the candidate rows. Required.
	Store defectlineage.Store
	// Resolver supplies each claimed fix's diff and landing commit. Required.
	Resolver defectlineage.FixResolver
	// Lease is how long a claim is held. Zero means the engine's default.
	Lease time.Duration
	// Anchors bound how anchors are extracted from each fix's diff.
	Anchors defectlineage.Options
	// MaxChangedFiles is the ceiling above which a fix is skipped. Zero means the
	// engine's default.
	MaxChangedFiles int
	// Config calibrates the confidence formula a structural verdict completes.
	// The zero value means the engine's default.
	Config defectlineage.Config
	// Corroboration supplies the signals behind the corroboration term. Nil
	// leaves that term's candidate-specific bucket empty.
	Corroboration defectlineage.CorroborationSource
}

// NewBlameSink builds the host work list.
func NewBlameSink(p BlameSinkParams) (*BlameSink, error) {
	if p.Store == nil {
		return nil, fmt.Errorf("wiring: BlameSinkParams.Store is required")
	}
	if p.Resolver == nil {
		return nil, fmt.Errorf("wiring: BlameSinkParams.Resolver is required")
	}
	lease := p.Lease
	if lease == 0 {
		lease = defectlineage.DefaultLease
	}
	if err := p.Config.Validate(); err != nil {
		return nil, err
	}
	cfg := p.Config
	if cfg == (defectlineage.Config{}) {
		cfg = defectlineage.DefaultConfig()
	}
	return &BlameSink{
		store: p.Store, resolver: p.Resolver, lease: lease,
		anchors: p.Anchors, maxFiles: p.MaxChangedFiles,
		cfg: cfg, corrob: p.Corroboration,
	}, nil
}

// ListPendingLineageBlame claims a batch of fixes and returns each with the
// anchors its walk needs.
//
// A claimed fix with nothing to blame is settled here rather than returned. That
// is what keeps the promise the contract makes to the walk: every fix it
// receives has at least one anchor, so it never spends a provider call
// discovering there was nothing to do.
func (s *BlameSink) ListPendingLineageBlame(ctx context.Context, req *connv1.ListPendingLineageBlameRequest) (*connv1.ListPendingLineageBlameResponse, error) {
	claims, err := s.store.ClaimPendingBlame(ctx, req.GetOrganizationId(), req.GetConnectorId(), int(req.GetLimit()), s.lease)
	if err != nil {
		return nil, fmt.Errorf("claiming fixes: %w", err)
	}

	out := make([]*connv1.PendingLineageBlame, 0, len(claims))
	for _, claim := range claims {
		in, err := s.resolver.Resolve(ctx, claim.Ref)
		if err != nil {
			// A fix whose input is permanently absent is settled so it stops being
			// re-offered. Any other failure is left for the lease to retry.
			if errors.Is(err, defectlineage.ErrNoFixInput) {
				_, _ = s.store.Settle(ctx, claim.Ref, claim.ClaimToken,
					dlv1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_ANCHORS_EMPTY)
			}
			continue
		}
		request, terminal, ok := defectlineage.BuildClaim(claim.Ref, in, s.maxFiles, s.anchors)
		if !ok {
			_, _ = s.store.Settle(ctx, claim.Ref, claim.ClaimToken, terminal)
			continue
		}
		held, err := s.store.MarkPrepared(ctx, claim.Ref, claim.ClaimToken)
		if err != nil {
			continue
		}
		if !held {
			// The claim was lost between claiming and preparing — the lease
			// lapsed and another worker took it, or the fix reached a terminal
			// stage meanwhile. Handing it out anyway gives two workers the same
			// fix, and this one's submit is dropped on arrival, so the provider
			// call that produced it was spent for nothing.
			continue
		}
		out = append(out, toConnPending(request))
	}
	return &connv1.ListPendingLineageBlameResponse{Pending: out}, nil
}

// SubmitLineageBlame records one fix's walk outcome.
func (s *BlameSink) SubmitLineageBlame(ctx context.Context, req *connv1.SubmitLineageBlameRequest) (*connv1.SubmitLineageBlameResponse, error) {
	// The submit carries no connector or repository, so they are read back from
	// the fix's own lifecycle row rather than guessed. Guessing would file the
	// candidate rows under the wrong connector, and the scoring pass claims by
	// connector, so they would never be scored.
	ref, err := s.store.LookupFix(ctx, req.GetOrganizationId(), req.GetBugFixPrId())
	if err != nil {
		return nil, fmt.Errorf("resolving fix for submit: %w", err)
	}
	outcome := &dlv1.LineageBlameOutcome{
		OrganizationId: req.GetOrganizationId(),
		BugFixPrId:     req.GetBugFixPrId(),
		Status:         dlv1.DefectLineageState(req.GetStatus()),
		Results:        make([]*dlv1.LineageBlameResult, 0, len(req.GetResults())),
	}
	for _, r := range req.GetResults() {
		outcome.Results = append(outcome.Results, fromConnResult(r))
	}
	if err := s.store.SubmitBlame(ctx, ref, outcome); err != nil {
		return nil, fmt.Errorf("submitting blame: %w", err)
	}
	return &connv1.SubmitLineageBlameResponse{}, nil
}

// ListPendingStructuralScoring claims unscored candidates and pairs each fix's
// group with that fix's anchors.
func (s *BlameSink) ListPendingStructuralScoring(ctx context.Context, req *connv1.ListPendingStructuralScoringRequest) (*connv1.ListPendingStructuralScoringResponse, error) {
	groups, err := s.store.ClaimUnscored(ctx, req.GetOrganizationId(), req.GetConnectorId(), int(req.GetLimit()), s.lease)
	if err != nil {
		return nil, fmt.Errorf("claiming unscored candidates: %w", err)
	}

	out := make([]*connv1.PendingStructuralScoring, 0, len(groups))
	for _, g := range groups {
		in, err := s.resolver.Resolve(ctx, g.Ref)
		if err != nil {
			continue
		}
		anchors := defectlineage.ExtractAnchors(in, s.anchors)
		if len(anchors) == 0 {
			// Without the fix's own changed regions there is nothing to compare a
			// candidate against, so this fix is left for a pass that can read them.
			continue
		}
		pending := &connv1.PendingStructuralScoring{
			PlatformRepoId: g.Ref.PlatformRepoID,
			RepoFullName:   in.RepoFullName,
			FixCommitSha:   in.FixCommitSHA,
			FixAnchors:     make([]*connv1.LineageBlameAnchor, 0, len(anchors)),
			Candidates:     make([]*connv1.PendingStructuralCandidate, 0, len(g.Candidates)),
		}
		for _, a := range anchors {
			pending.FixAnchors = append(pending.FixAnchors, &connv1.LineageBlameAnchor{
				Path:        a.OldPath,
				StartLine:   int32(a.RemovedStartLine),
				EndLine:     int32(a.RemovedEndLine),
				RemovedText: a.RemovedText,
			})
		}
		for _, c := range g.Candidates {
			pending.Candidates = append(pending.Candidates, &connv1.PendingStructuralCandidate{
				CandidateLineageId: c.GetCandidateLineageId(),
				CandidateCommitSha: c.GetCandidateCommitSha(),
				CandidatePath:      c.GetCandidatePath(),
				CandidateLine:      c.GetCandidateLine(),
			})
		}
		out = append(out, pending)
	}
	return &connv1.ListPendingStructuralScoringResponse{Pending: out}, nil
}

// SubmitStructuralVerdict records one candidate's structural rung and writes its
// confidence score.
//
// The connector measures only the code-context term; the other four are read
// from what blame already stored, so a verdict arriving here completes the score
// rather than replacing it.
func (s *BlameSink) SubmitStructuralVerdict(ctx context.Context, req *connv1.SubmitStructuralVerdictRequest) (*connv1.SubmitStructuralVerdictResponse, error) {
	orgID := req.GetOrganizationId()
	row, err := s.store.GetCandidate(ctx, orgID, req.GetCandidateLineageId())
	if err != nil {
		if errors.Is(err, defectlineage.ErrCandidateNotFound) {
			// The row went away between the claim and the verdict. Recreating it is
			// not this path's business, and failing would make the provider retry a
			// write that can never land.
			return &connv1.SubmitStructuralVerdictResponse{}, nil
		}
		return nil, fmt.Errorf("reading candidate for verdict: %w", err)
	}

	in, err := s.resolver.Resolve(ctx, row.Ref)
	if err != nil {
		return nil, fmt.Errorf("resolving fix for verdict: %w", err)
	}

	level, known := defectlineage.ParseMatchLevel(req.GetMatchLevel())
	if !known {
		// An unreadable rung scores no context rather than a wrong one, and is
		// marked degraded so the stored derivation says the rung was not measured.
		level = defectlineage.MatchDifferentFile
	}

	var e float64
	var flags defectlineage.CorroborationFlags
	if s.corrob != nil {
		ci, cerr := s.corrob.For(ctx, row.Ref, &dlv1.PendingStructuralCandidate{
			CandidateLineageId: row.ID,
			CandidateCommitSha: row.CommitSHA,
		})
		if cerr == nil {
			e, flags = defectlineage.ScoreEWithFlags(ci, s.cfg)
		}
	}

	score, evidence := defectlineage.BuildScore(defectlineage.ScoreInputs{
		EvidenceType:  row.EvidenceType,
		CommittedAt:   row.CommittedAt,
		MergedAt:      in.MergedAt,
		Level:         level,
		Degraded:      req.GetDegraded() || !known,
		NodeType:      req.GetNodeType(),
		EnclosingType: req.GetEnclosingType(),
		Corroboration: e,
		Flags:         flags,
	}, s.cfg)
	if err := s.store.WriteScore(ctx, orgID, row.ID, score, evidence, req.GetCandidateLine()); err != nil {
		return nil, fmt.Errorf("writing score: %w", err)
	}
	if _, err := s.store.ResolveIfFullyScored(ctx, row.Ref); err != nil {
		return nil, fmt.Errorf("resolving fix after verdict: %w", err)
	}
	return &connv1.SubmitStructuralVerdictResponse{}, nil
}

// Walker adapts the engine's blame walk to the connector framework's contract.
type Walker struct {
	inner *defectlineage.Walker
}

// NewWalker wraps an engine walker.
func NewWalker(w *defectlineage.Walker) (*Walker, error) {
	if w == nil {
		built, err := defectlineage.NewWalker(defectlineage.WalkParams{})
		if err != nil {
			return nil, err
		}
		w = built
	}
	return &Walker{inner: w}, nil
}

// Walk runs the blame walk for one claimed fix.
func (w *Walker) Walk(ctx context.Context, src connlineage.Source, fix *connv1.PendingLineageBlame) (connv1.DefectLineageState, []*connv1.LineageBlameResult, error) {
	state, results, err := w.inner.Walk(ctx, sourceAdapter{src}, fromConnPending(fix))
	out := make([]*connv1.LineageBlameResult, 0, len(results))
	for _, r := range results {
		out = append(out, toConnResult(r))
	}
	return connv1.DefectLineageState(state), out, err
}

// ResolveCandidatePRs fills each result's candidate pull-request number.
func (w *Walker) ResolveCandidatePRs(ctx context.Context, results []*connv1.LineageBlameResult, resolve connlineage.CandidatePRResolver) {
	// The engine mutates its own result type, so the numbers are copied back onto
	// the caller's messages afterwards.
	mirror := make([]*dlv1.LineageBlameResult, len(results))
	for i, r := range results {
		mirror[i] = fromConnResult(r)
	}
	w.inner.ResolveCandidatePRs(ctx, mirror, defectlineage.CandidatePRResolver(resolve))
	for i, m := range mirror {
		results[i].CandidatePrNumber = m.GetCandidatePrNumber()
	}
}

// sourceAdapter presents the connector framework's blame port as the engine's.
type sourceAdapter struct{ inner connlineage.Source }

func (a sourceAdapter) Lines(ctx context.Context, revision, path string) ([]defectlineage.BlamedLine, error) {
	lines, err := a.inner.Lines(ctx, revision, path)
	if err != nil {
		return nil, err
	}
	out := make([]defectlineage.BlamedLine, 0, len(lines))
	for _, l := range lines {
		out = append(out, defectlineage.BlamedLine{
			Text: l.Text, OwnerSHA: l.OwnerSHA, CommittedAt: l.CommittedAt,
			AuthorName: l.AuthorName, AuthorEmail: l.AuthorEmail,
		})
	}
	return out, nil
}

func (a sourceAdapter) FirstParent(ctx context.Context, sha string) (string, error) {
	return a.inner.FirstParent(ctx, sha)
}

// Compile-time proof that the engine satisfies the two host contracts it claims.
var (
	_ connlineage.BlameSink = (*BlameSink)(nil)
	_ connlineage.Walker    = (*Walker)(nil)
)
