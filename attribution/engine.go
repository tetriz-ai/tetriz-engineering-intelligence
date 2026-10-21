// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package attribution links AI coding sessions to the pull requests they
// produced, as author or as reviewer, and explains why each link was made.
//
// The engine holds no storage. A caller supplies candidate pull requests and
// person-to-vendor identities through two small interfaces, and persists what
// Attribute returns. A session whose evidence is too thin abstains with a stated
// reason rather than guessing.
package attribution

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// taskTypeCodeReview routes a self-reviewed pull request to the REVIEWED bucket.
const taskTypeCodeReview = "code-review"

// Identity is one person's vendor accounts, as the caller resolves them.
type Identity struct {
	// AuthorIDs are the vendor user ids this person authors under (a GitHub
	// login or numeric id, a Bitbucket account id). Candidate pull requests are
	// fetched by these.
	AuthorIDs []string
	// ReviewerIdentityIDs are the ids this person appears under in a pull
	// request's reviewer list. Separate from AuthorIDs because vendors identify
	// a reviewer differently from an author.
	ReviewerIdentityIDs []string
}

// IdentityResolver maps a person to the vendor accounts they act under.
//
// The engine never guesses an identity: a person it cannot resolve abstains with
// VCS_NOT_LINKED, and a resolver error abstains with IDENTITY_ERROR, which is
// retried on the cooldown rather than treated as a settled answer.
type IdentityResolver interface {
	Resolve(ctx context.Context, orgID, personID string) (Identity, error)
}

// CandidateStore supplies the pull requests a session might have contributed to.
//
// Both methods are called once per person per batch, over a date range spanning
// all of that person's sessions in the batch, and both may return an empty
// slice. The store is responsible for its own tenant scoping: the engine passes
// orgID and otherwise assumes whatever it receives is already the right
// tenant's.
type CandidateStore interface {
	// ListAuthoredPRs returns candidates this person authored, with
	// ParticipationKind AUTHORED.
	ListAuthoredPRs(ctx context.Context, orgID string, authorIDs []string, from, to time.Time) ([]*attributionv1.PRCandidate, error)
	// ListReviewedPRs returns candidates this person reviewed, with
	// ParticipationKind REVIEWED. A pull request the person both wrote and
	// reviewed belongs in both lists: that is what credits a code-review session
	// over the person's own pull request as REVIEWED.
	ListReviewedPRs(ctx context.Context, orgID string, identityIDs []string, from, to time.Time) ([]*attributionv1.PRCandidate, error)
}

// MatchResult is one session's attribution, with the two facts a persister needs
// that the attribution itself cannot carry.
type MatchResult struct {
	Session     *attributionv1.SessionEvidence
	Attribution *attributionv1.PRAttribution

	// CandidateCount is how many candidates the signals ran over. Zero means
	// nothing was evaluated, and the reducer then never withdraws a stored match.
	CandidateCount int

	// PossiblePRs is every candidate key this run fetched for the person, before
	// any gate ran. The reducer needs it to tell a match the rules REFUSED from
	// one this run never examined.
	PossiblePRs map[string]struct{}
}

// personState holds one person's identity resolution for the batch.
type personState struct {
	identity Identity
	err      error
	// identityErr is true when err came from identity resolution itself, as
	// opposed to a later candidate-fetch error. The former gets an
	// IDENTITY_ERROR marker so it retries on the cooldown rather than
	// re-selecting at the batch head every run; the latter is left pending.
	identityErr bool
}

// Engine attributes AI coding sessions to pull requests.
//
// It holds no storage of its own. A caller supplies candidates and identities
// through the two interfaces, and persists what Attribute returns.
type Engine struct {
	params     Params
	candidates CandidateStore
	identities IdentityResolver
}

// New builds an Engine. Every Params field left at zero or set negative takes
// its DefaultParams value. Every positive field is kept.
func New(params Params, candidates CandidateStore, identities IdentityResolver) *Engine {
	return &Engine{params: params.withDefaults(), candidates: candidates, identities: identities}
}

// Attribute runs identity resolution, candidate fetch and match computation in
// turn. The caller loads the batch and stores the result.
func (e *Engine) Attribute(ctx context.Context, orgID string, sessions []*attributionv1.SessionEvidence) ([]MatchResult, error) {
	if len(sessions) == 0 {
		return nil, nil
	}
	observeAnchorCoverage(sessions, e.params, "engine")

	byPerson := groupByPerson(sessions)
	identities, err := e.resolveIdentities(ctx, orgID, byPerson)
	if err != nil {
		return nil, fmt.Errorf("resolving identities: %w", err)
	}
	candidates := e.fetchCandidates(ctx, orgID, byPerson, identities)
	results, err := e.computeAttributions(orgID, byPerson, identities, candidates)
	if err != nil {
		return nil, fmt.Errorf("computing attributions: %w", err)
	}
	return results, nil
}

func groupByPerson(sessions []*attributionv1.SessionEvidence) map[string][]*attributionv1.SessionEvidence {
	byPerson := make(map[string][]*attributionv1.SessionEvidence)
	for _, s := range sessions {
		byPerson[s.GetPersonId()] = append(byPerson[s.GetPersonId()], s)
	}
	return byPerson
}

// resolveIdentities resolves every person in the batch concurrently. A
// per-person failure is captured rather than cancelling siblings, and the map is
// pre-populated so a worker that dies before writing leaves a usable zero value
// rather than a nil dereference in the next phase.
func (e *Engine) resolveIdentities(ctx context.Context, orgID string, byPerson map[string][]*attributionv1.SessionEvidence) (map[string]*personState, error) {
	identities := make(map[string]*personState, len(byPerson))
	for personID := range byPerson {
		identities[personID] = &personState{}
	}

	g := newErrGroup()
	g.setLimit(e.params.IdentityResolveConcurrency)
	for personID := range byPerson {
		g.Go(func() error {
			id, resolveErr := e.identities.Resolve(ctx, orgID, personID)
			state := identities[personID]
			state.identity = id
			state.err = resolveErr
			state.identityErr = resolveErr != nil
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("waiting: %w", err)
	}
	return identities, nil
}

// fetchCandidates reads each resolved person's candidates.
//
// Author-side and reviewer-side are one completeness unit per person: a snapshot
// written from author-only candidates is effectively terminal, because a later
// run only revisits a matched session when its inputs drift — never because this
// fetch recovered. So a failure on either side records the error and skips the
// person entirely, leaving the sessions pending, rather than degrading to a
// partial candidate set that would lose the other side's matches for good.
func (e *Engine) fetchCandidates(
	ctx context.Context,
	orgID string,
	byPerson map[string][]*attributionv1.SessionEvidence,
	identities map[string]*personState,
) map[string][]*attributionv1.PRCandidate {
	out := make(map[string][]*attributionv1.PRCandidate, len(byPerson))

	for personID, personSessions := range byPerson {
		state := identities[personID]
		if state.err != nil {
			continue
		}
		if len(state.identity.AuthorIDs) == 0 && len(state.identity.ReviewerIdentityIDs) == 0 {
			continue
		}

		from, to := sessionDateRange(personSessions, e.params)
		// Fetch on the wider strong-signal window so the direct-reference and
		// branch signals see pull requests that merged well after the batch's last
		// session activity. File overlap narrows the set again at match time.
		to = to.AddDate(0, 0, effectiveStrongSignalWindowDays(e.params))

		var authored []*attributionv1.PRCandidate
		if len(state.identity.AuthorIDs) > 0 {
			prs, err := e.candidates.ListAuthoredPRs(ctx, orgID, state.identity.AuthorIDs, from, to)
			if err != nil {
				state.err = fmt.Errorf("listing authored candidates: %w", err)
				e.reportSkipped(orgID, personID, skipCandidateFetchError, state.err, personSessions)
				continue
			}
			authored = stampKind(prs, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED)
		}

		var reviewed []*attributionv1.PRCandidate
		if len(state.identity.ReviewerIdentityIDs) > 0 {
			prs, err := e.candidates.ListReviewedPRs(ctx, orgID, state.identity.ReviewerIdentityIDs, from, to)
			if err != nil {
				state.err = fmt.Errorf("listing reviewed candidates: %w", err)
				e.reportSkipped(orgID, personID, skipCandidateFetchError, state.err, personSessions)
				continue
			}
			// A self-reviewed pull request flows through in BOTH sets. The match
			// computation then routes each pair to REVIEWED or AUTHORED on the
			// session's own task type.
			reviewed = stampKind(prs, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED)
		}

		e.params.obs().Observe("candidate_prs_per_person", float64(len(authored)), map[string]string{"side": "author"})
		e.params.obs().Observe("candidate_prs_per_person", float64(len(reviewed)), map[string]string{"side": "reviewer"})

		out[personID] = append(authored, reviewed...)
	}
	return out
}

// Reasons a person's sessions were left pending, as reported to the Observer.
const (
	skipCandidateFetchError = "candidate_fetch_error"
	skipIdentityError       = "identity_error"
)

// reportSkipped tells the Observer that a person's sessions were left pending
// with no result: an error that leaves no result must still be visible, or a
// store that fails every run starves those sessions without a trace.
func (e *Engine) reportSkipped(orgID, personID, reason string, err error, sessions []*attributionv1.SessionEvidence) {
	ids := make([]string, 0, len(sessions))
	for _, s := range sessions {
		ids = append(ids, s.GetSessionId())
	}
	e.params.obs().Count("person_skipped", map[string]string{"reason": reason})
	e.params.obs().Debug("person_skipped", map[string]any{
		"org_id":      orgID,
		"person_id":   personID,
		"reason":      reason,
		"error":       err.Error(),
		"session_ids": ids,
	})
}

// stampKind marks which side a candidate was fetched from, writing the kind onto
// the candidate the store returned. A store that already stamped the kind keeps
// its value, so a caller joining both sides itself is not overridden. A store
// must therefore return its own candidate objects per person and per call: an
// object shared between people, or between batches running at once, is marked
// by whichever fetch reached it first. A nil candidate panics.
func stampKind(prs []*attributionv1.PRCandidate, kind attributionv1.SessionParticipationKind) []*attributionv1.PRCandidate {
	for _, pr := range prs {
		if pr.GetParticipationKind() == attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_UNSPECIFIED {
			pr.ParticipationKind = kind
		}
	}
	return prs
}

func (e *Engine) computeAttributions(
	orgID string,
	byPerson map[string][]*attributionv1.SessionEvidence,
	identities map[string]*personState,
	candidates map[string][]*attributionv1.PRCandidate,
) ([]MatchResult, error) {
	results := make([]MatchResult, 0, totalSessions(byPerson))
	var mu sync.Mutex

	g := newErrGroup()
	g.setLimit(e.params.ComputeMatchConcurrency)
	for personID, personSessions := range byPerson {
		state := identities[personID]
		if state.err != nil {
			if !state.identityErr {
				// A candidate-fetch error is transient: leave the sessions pending
				// so they retry, rather than recording an answer.
				continue
			}
			// Identity resolution itself failed. Record the marker so these
			// sessions obey the retry cooldown instead of re-selecting at the head
			// of every batch.
			g.Go(func() error {
				local := make([]MatchResult, 0, len(personSessions))
				var kept []*attributionv1.SessionEvidence
				for _, s := range personSessions {
					// A session that already holds real matches keeps them: an
					// identity outage must not replace a good attribution with an
					// unmatched marker. Leave it pending instead.
					if len(s.GetStoredAttribution().GetMatches()) > 0 {
						kept = append(kept, s)
						continue
					}
					local = append(local, MatchResult{
						Session: s,
						Attribution: buildUnmatchedAttribution(
							attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_IDENTITY_ERROR, nil),
					})
				}
				if len(kept) > 0 {
					e.reportSkipped(orgID, personID, skipIdentityError, state.err, kept)
				}
				mu.Lock()
				results = append(results, local...)
				mu.Unlock()
				return nil
			})
			continue
		}

		prs, hasPRs := candidates[personID]
		g.Go(func() error {
			local := make([]MatchResult, 0, len(personSessions))
			if !hasPRs {
				for _, s := range personSessions {
					local = append(local, MatchResult{
						Session: s,
						Attribution: buildUnmatchedAttribution(
							attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_VCS_NOT_LINKED, nil),
					})
				}
			} else {
				candKeys := possiblePRSet(prs)
				for _, s := range personSessions {
					attr := e.computeMatches(s, prs)
					e.params.obs().Observe("matches_per_session", float64(len(attr.GetMatches())), nil)
					for _, m := range attr.GetMatches() {
						e.params.obs().Count("signal_hits", map[string]string{"method": m.GetMethod().String()})
					}
					local = append(local, MatchResult{
						Session: s, Attribution: attr,
						CandidateCount: len(prs), PossiblePRs: candKeys,
					})
				}
			}
			mu.Lock()
			results = append(results, local...)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("waiting: %w", err)
	}
	return results, nil
}

// possiblePRSet indexes the fetched candidates by key.
func possiblePRSet(prs []*attributionv1.PRCandidate) map[string]struct{} {
	if len(prs) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(prs))
	for _, pc := range prs {
		if pc != nil {
			out[pc.GetPrKey()] = struct{}{}
		}
	}
	return out
}

func totalSessions(byPerson map[string][]*attributionv1.SessionEvidence) int {
	n := 0
	for _, s := range byPerson {
		n += len(s)
	}
	return n
}

// sessionDateRange is the batch's outer window: earliest start to latest anchor.
func sessionDateRange(sessions []*attributionv1.SessionEvidence, params Params) (time.Time, time.Time) {
	if len(sessions) == 0 {
		return time.Time{}, time.Time{}
	}
	earliest := sessionStartDate(sessions[0])
	latest := sessionMatchAnchor(sessions[0], params)
	for _, s := range sessions[1:] {
		if sessionStartDate(s).Before(earliest) {
			earliest = sessionStartDate(s)
		}
		if anchor := sessionMatchAnchor(s, params); anchor.After(latest) {
			latest = anchor
		}
	}
	return earliest, latest
}

// MatchesDistribution reports the mean and p99 matches per session in a batch.
// The p99 index is int(0.99n), clamped to the last element, so for a batch of
// 100 sessions or fewer it is the maximum.
func MatchesDistribution(results []MatchResult) (float64, int) {
	if len(results) == 0 {
		return 0, 0
	}
	counts := make([]int, len(results))
	var total int
	for i, r := range results {
		n := len(r.Attribution.GetMatches())
		counts[i] = n
		total += n
	}
	mean := float64(total) / float64(len(results))
	sort.Ints(counts)
	idx := int(float64(len(counts)) * 0.99)
	if idx >= len(counts) {
		idx = len(counts) - 1
	}
	return mean, counts[idx]
}

// computeMatches runs the signal cascade over a session's candidates and returns
// one entry per distinct pull request it links to.
//
// Signals run in descending tier order and the first to claim a key wins. A
// later signal matching the same pull request is recorded in SignalsEvaluated
// rather than replacing the match, so the strongest evidence is what the match
// carries and the weaker corroboration is still visible.
//
// File overlap sees a narrower candidate set than the strong signals: only
// author-side candidates, and only those inside the tighter window. Reviewers
// read rather than edit, so overlap on a reviewed pull request is usually
// unrelated parallel work, and counting it would inflate review attribution with
// false positives.
//
// Each match carries an explicit participation kind, routed by:
//
//	not author                                      -> REVIEWED
//	author + self-review    + task type code-review -> REVIEWED
//	author + self-review    + any other task type   -> AUTHORED
//	author + no self-review + any task type         -> AUTHORED
//
// The code-review carve-out exists because engineers run an agent over their own
// pull request as a pre-merge review pass, and that is a review.
func (e *Engine) computeMatches(session *attributionv1.SessionEvidence, prs []*attributionv1.PRCandidate) *attributionv1.PRAttribution {
	narrowCutoff := sessionMatchAnchor(session, e.params).AddDate(0, 0, e.params.TimeWindowAfterDays)
	narrowAuthorPRs := filterByKind(prsOpenedOrMergedOnOrBefore(prs, narrowCutoff),
		attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED)

	signals := []struct {
		method     attributionv1.SessionToPRMatchMethod
		candidates []*attributionv1.PRCandidate
		fn         func(*attributionv1.SessionEvidence, []*attributionv1.PRCandidate, Params) []*attributionv1.PRMatch
	}{
		{attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF, prs, matchDirectPRRef},
		{attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH, prs, matchBranch},
		{attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP, narrowAuthorPRs, matchFileOverlap},
	}

	authorPRKeys := make(map[string]struct{}, len(prs))
	reviewerPRKeys := make(map[string]struct{}, len(prs))
	for _, pc := range prs {
		if pc == nil {
			continue
		}
		switch pc.GetParticipationKind() {
		case attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED:
			authorPRKeys[pc.GetPrKey()] = struct{}{}
		case attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED:
			reviewerPRKeys[pc.GetPrKey()] = struct{}{}
		default:
			// UNSPECIFIED joins neither set.
		}
	}
	isCodeReviewTask := session.GetTaskType() == taskTypeCodeReview

	byPRKey := make(map[string]*attributionv1.PRMatch, len(prs))
	evaluated := make([]string, 0, len(signals))

	ideType := session.GetIdeType()
	confidence := session.GetAiAttributionConfidence()

	for _, sig := range signals {
		evaluated = append(evaluated, sig.method.String())
		for _, m := range sig.fn(session, sig.candidates, e.params) {
			if existing, ok := byPRKey[m.GetPrKey()]; ok {
				existing.SignalsEvaluated = append(existing.SignalsEvaluated, sig.method.String())
				continue
			}
			m.Method = sig.method
			m.SignalsEvaluated = append([]string{}, evaluated...)
			m.ConfidenceTier = ComputeMatchTier(sig.method, ideType, confidence)
			m.ParticipationKind = classifyParticipationKind(m.GetPrKey(), authorPRKeys, reviewerPRKeys, isCodeReviewTask)
			byPRKey[m.GetPrKey()] = m
		}
	}

	if len(byPRKey) == 0 {
		return buildUnmatchedAttribution(
			attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR,
			evaluated,
		)
	}

	// Map order: the matches come back in no fixed order. A caller that prints or
	// compares them sorts them itself.
	matches := make([]*attributionv1.PRMatch, 0, len(byPRKey))
	for _, m := range byPRKey {
		matches = append(matches, m)
	}
	return &attributionv1.PRAttribution{Matches: matches}
}

// classifyParticipationKind resolves a match's kind from the session's
// membership across both candidate sets and its task type. Defaults to AUTHORED
// when the key is in neither set, so a match never carries UNSPECIFIED.
func classifyParticipationKind(
	prKey string,
	authorPRKeys map[string]struct{},
	reviewerPRKeys map[string]struct{},
	isCodeReviewTask bool,
) attributionv1.SessionParticipationKind {
	_, isAuthor := authorPRKeys[prKey]
	_, isReviewer := reviewerPRKeys[prKey]
	switch {
	case isAuthor && isReviewer && isCodeReviewTask:
		return attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED
	case !isAuthor && isReviewer:
		return attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED
	default:
		return attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED
	}
}

func filterByKind(prs []*attributionv1.PRCandidate, kind attributionv1.SessionParticipationKind) []*attributionv1.PRCandidate {
	out := prs[:0:0]
	for _, pc := range prs {
		if pc.GetParticipationKind() == kind {
			out = append(out, pc)
		}
	}
	return out
}

// Breakdown counts a batch by what each session resolved to.
type Breakdown struct {
	MatchedDirectPRRef int
	MatchedBranch      int
	MatchedFileOverlap int

	UnmatchedNoMatchingPR int
	UnmatchedVCSNotLinked int
	UnmatchedIdentityErr  int
}

// Summarise counts a batch's results by match method and abstention reason.
//
// A session contributing to several pull requests counts once per match, so the
// matched totals can exceed the number of sessions. The abstention counts are
// per session, because a session abstains once.
func Summarise(results []MatchResult) Breakdown {
	var b Breakdown
	for _, r := range results {
		if matches := r.Attribution.GetMatches(); len(matches) > 0 {
			for _, m := range matches {
				switch m.GetMethod() {
				case attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF:
					b.MatchedDirectPRRef++
				case attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH:
					b.MatchedBranch++
				case attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP:
					b.MatchedFileOverlap++
				default:
				}
			}
			continue
		}
		switch r.Attribution.GetUnmatched().GetReason() {
		case attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR:
			b.UnmatchedNoMatchingPR++
		case attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_VCS_NOT_LINKED:
			b.UnmatchedVCSNotLinked++
		case attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_IDENTITY_ERROR:
			b.UnmatchedIdentityErr++
		default:
		}
	}
	return b
}
