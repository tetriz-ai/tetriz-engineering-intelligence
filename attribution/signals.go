// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"fmt"
	"math"
	"strings"
	"time"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

const (
	confidenceDirectPRRef = 1.0
	confidenceBranchMatch = 0.85

	// shadowJaccardThreshold is the looser rule the Jaccard shadow counts: a
	// candidate the file-overlap gate refused whose Jaccard similarity is at
	// least 0.10. It selects which refused pairs are counted, never which match.
	shadowJaccardThreshold = 0.10

	// shadowLogSampleSize bounds the pairs carried in the jaccard_shadow_admits
	// debug event. The counter carries the full count.
	shadowLogSampleSize = 25
)

// prMatchFromContext builds a PRMatch proto from a candidate with the given
// confidence and matched files.
//
// evidence is required rather than filled in by callers afterwards so the
// field cannot be silently forgotten on a new emission site: an absent value
// means "no evidence recorded", never "evidence was checked and found
// absent", and a method that quietly omitted it would make the two
// indistinguishable to every reader.
func prMatchFromContext(pr *attributionv1.PRCandidate, confidence float64, matchedFiles []string,
	evidence attributionv1.SessionToPRMatchEvidence,
) *attributionv1.PRMatch {
	return &attributionv1.PRMatch{
		PrKey:        pr.GetPrKey(),
		PrRepository: pr.GetPrRepository(),
		PrNumber:     pr.GetPrNumber(),
		Confidence:   math.Min(1, math.Max(0, confidence)),
		MatchedFiles: matchedFiles,
		LinesAdded:   pr.GetLinesAdded(),
		LinesRemoved: pr.GetLinesRemoved(),
		Evidence:     evidence,
	}
}

// matchDirectPRRef matches the session against every PR it explicitly
// references. Returns all matching PRs.
//
// For a gated session the reference must additionally be session-produced. This
// is the highest-confidence method, so it is where an ambient reference does the
// most damage: a PR URL read out of a nearby commit names a colleague's PR
// precisely, and attributing on it is the failure the gate exists to prevent.
// Gating BRANCH_MATCH while leaving this arm open would close the weaker route
// and leave the stronger one, so the stored match would keep feeding the wrong
// PR to every reader.
//
// Bounded above, by the same ceiling BRANCH_MATCH uses. That bound is about
// recency, not provenance, and the two are independent: the gate above decides
// whether a reference is the session's to claim, this decides whether the PR is
// near enough in time to be the work. Without it the ceiling would be whatever
// the batch's longest-running session happened to set, since candidates are
// fetched per *person* — so the same session could match or not depending only on
// who else shared its batch.
//
// No lower bound: a session may legitimately name a PR that merged before it
// opened, reviewing or building on already-merged work. An undated candidate is
// exempt from the ceiling too: a reviewer-side fetch can admit still-open PRs,
// which carry no merge date for a window to place.
func matchDirectPRRef(session *attributionv1.SessionEvidence, prs []*attributionv1.PRCandidate, params Params) []*attributionv1.PRMatch {
	if session == nil {
		return nil
	}
	refs := collectPRRefs(session)
	if len(refs) == 0 {
		return nil
	}
	gated := sessionIsGated(session)
	sessionRepo := strings.ToLower(session.GetRepoName())
	windowTo := sessionMatchAnchor(session, params).AddDate(0, 0, effectiveStrongSignalWindowDays(params))

	repos := knownRepoSet(prs)
	var out []*attributionv1.PRMatch
	for _, pr := range prs {
		if !mergedAt(pr).IsZero() && mergedAt(pr).After(windowTo) {
			continue
		}
		for _, ref := range refs {
			if gated && !refMayAttribute(ref, sessionRepo) {
				continue
			}
			// Repo compared case-insensitively, matching reviewerBranchEvidence:
			// producers disagree on repo casing ("Org/Repo" vs the connector's
			// "org/repo"), and a byte-exact compare would drop a reference naming
			// this exact PR on the highest-confidence arm. Not sameRepo — its
			// basename fallback would match the wrong owner.
			if pr.GetPrNumber() == ref.number && strings.EqualFold(pr.GetPrRepository(), ref.repo) {
				// Deliberately unsupported: this method attributes on its own
				// evidence — the reference itself — so it records no separate
				// evidence. Keeping it explicit is what lets a reader tell
				// this shape apart from a BRANCH_MATCH that lost its stamp.
				out = append(out, prMatchFromContext(pr, confidenceDirectPRRef,
					MatchedFiles(extractFilesEdited(session), pr.GetFilesChanged(), repos),
					attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_UNSPECIFIED))
				break // don't emit the same PR twice if it appears in refs more than once
			}
		}
	}
	return out
}

// prRef is the matcher's internal (repo, number) pair for DIRECT_PR_REF.
type prRef struct {
	repo   string
	number int32
}

// sourcedPRRef is a PR the session referenced, with how that reference was
// obtained. The two travel together because they answer different questions: a
// reference says WHICH PR, and its source says whether this session is why we
// know about it.
type sourcedPRRef struct {
	prRef
	source attributionv1.SignalSource
}

// collectPRRefs returns the session's deduped PR references, each with the
// strongest source that named it, in first-seen order.
//
// One session can hold both a `gh pr create` receipt and a URL scraped out of a
// teammate's commit for the SAME PR. Dedup is on (repo, number), so the merge
// has to say which source survives, and it must be the strongest: keeping the
// weaker one would let a scrape shadow a receipt.
//
// The pr_number and pr_repository scalars are the fallback when pr_references is
// empty, and enter unclassified, like every other value with no source.
func collectPRRefs(s *attributionv1.SessionEvidence) []sourcedPRRef {
	idx := make(map[prRef]int)
	var out []sourcedPRRef
	keepStrongest := func(repo string, num int32, src attributionv1.SignalSource) {
		if repo == "" || num == 0 {
			return
		}
		key := prRef{repo: repo, number: num}
		if at, dup := idx[key]; dup {
			if branchSourceRank(src) > branchSourceRank(out[at].source) {
				out[at].source = src
			}
			return
		}
		idx[key] = len(out)
		out = append(out, sourcedPRRef{prRef: key, source: src})
	}

	for _, r := range s.GetPrReferences() {
		keepStrongest(r.GetPrRepository(), r.GetPrNumber(), r.GetSource())
	}
	if len(out) == 0 {
		keepStrongest(s.GetPrRepository(), s.GetPrNumber(),
			attributionv1.SignalSource_SIGNAL_SOURCE_UNSPECIFIED)
	}
	return out
}

// refMayAttribute reports whether a reference is admissible as the basis for an
// attribution, as opposed to merely scoping a search.
//
// One definition serves both the direct-reference method and the branch arm's
// reviewer corroboration, deliberately: a reference too weak to attribute on its
// own must not be strong enough to corroborate a branch, and two copies of that
// test would eventually disagree about which.
//
// Prose carries the extra repo gate because prose routinely quotes a PR the
// session was only reading about. An absent session repo refuses rather than
// passes — an unknown repo cannot clear a gate whose whole job is comparing one.
func refMayAttribute(ref sourcedPRRef, sessionRepoLower string) bool {
	if !ref.isSessionProduced() {
		return false
	}
	if ref.source == attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_PROSE {
		return sessionRepoLower != "" && sameRepo(sessionRepoLower, ref.repo)
	}
	return true
}

// isSessionProduced reports whether this reference is evidence about THIS
// session rather than something the workspace merely held.
//
// It is the bar a reference must clear to support an attribution, as opposed to
// merely scoping a search. A local-git read names a PR precisely and says
// nothing about the session — that is the scraped colleague URL — and an
// unclassified reference cannot be told apart from one.
//
// SESSION_PROSE qualifies but the caller must additionally repo-gate it: prose
// routinely quotes a PR the session was only reading about.
func (r sourcedPRRef) isSessionProduced() bool {
	return isSessionProducedSource(r.source)
}

// isSessionProducedSource reports whether a signal's source is the session's own
// account of its work rather than something the workspace merely held.
//
// Shared by the reference bar (isSessionProduced) and the branch gate so the two
// cannot drift on what "the session produced this" means. A local-git read and an
// unclassified value describe the workspace, not the session; everything else is
// the session speaking — a command it ran, a vendor field it set, prose it wrote.
// A prose reference still needs a repo gate at its own call site (prose quotes a
// PR the session only read about); a prose BRANCH does not, because the branch has
// already matched a candidate PR's exact head branch, a far tighter scope than a
// bare PR number.
func isSessionProducedSource(s attributionv1.SignalSource) bool {
	switch s {
	case attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND,
		attributionv1.SignalSource_SIGNAL_SOURCE_VENDOR_FIELD,
		attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_PROSE:
		return true
	case attributionv1.SignalSource_SIGNAL_SOURCE_LOCAL_GIT_WINDOWED,
		attributionv1.SignalSource_SIGNAL_SOURCE_UNSPECIFIED:
		return false
	}
	return false
}

// branchEvidence is what made a scoped candidate specific to THIS session, as
// opposed to any other session that ran on the same branch. files is the set the
// gate actually evaluated, so the emitted matched_files cannot drift from the
// evidence that admitted the match; it is empty on the reviewer arm, which
// corroborates on a reference rather than on edits.
type branchEvidence struct {
	kind  attributionv1.SessionToPRMatchEvidence
	files []string
}

// branchGateInputs is what both gate arms read: gathered once per session rather
// than re-derived per candidate.
type branchGateInputs struct {
	refs        []sourcedPRRef
	sessionRepo string
	threshold   float64
	ideType     string
}

func newBranchGateInputs(session *attributionv1.SessionEvidence, params Params) branchGateInputs {
	return branchGateInputs{
		refs:        collectPRRefs(session),
		sessionRepo: strings.ToLower(session.GetRepoName()),
		threshold:   float64(params.MinConfidencePercent) / 100.0,
		ideType:     canonicalIDEType(session.GetIdeType()),
	}
}

// branchKey keys a candidate by (repo, branch), not branch alone: candidates span
// a person's PRs across repositories (the candidate fetch is scoped by author
// and date, not by repository), so two distinct PRs in different repos that reuse a branch name
// must each keep their own BRANCH_MATCH rather than collapsing to one.
type branchKey struct{ repo, branch string }

// branchCand is the candidate held for one branchKey: the PR whose merge time is
// nearest the session, so a reused branch name doesn't link it to all of them.
type branchCand struct {
	pr       *attributionv1.PRCandidate
	gap      time.Duration
	evidence branchEvidence
}

// matchBranch finds PRs whose head_branch matches a branch observed during the
// session (branch_signals and git_branches, or the git_branch scalar when neither
// carries one). Empty and dependency-bot head branches (dependabot/, renovate/)
// are skipped. Shared default branches such as main and develop are matched on
// purpose, because some teams push their work to them (see isGenericBranch). A
// re-attribution trigger that wants to skip them asks IsSharedDefaultBranch. When a
// branch name matches more than one candidate PR — serial reuse of the same
// name across PRs — only the PR whose merge is nearest the session in time is
// kept, so one session does not fan out across every PR that used the name.
// Returns at most one match per branch.
//
// For a gated session (see sessionIsGated) a branch only SCOPES the candidate
// set: the branch is a property of the workspace, so every session run there
// carries it and it cannot say which session did the work. Such a candidate must
// additionally clear a PR-specific gate — see branchEvidenceFor — and one that
// does not is dropped rather than emitted. An ungated session keeps the older
// rule where branch membership attributes on its own, and its matches record no
// evidence because none was evaluated.
//
// Evidence is evaluated BEFORE the nearest-merge dedup, and the order is
// load-bearing. Deduping first picks the nearest-merging PR per (repo, branch)
// and only then tests it, so a reused branch name whose nearer PR has no overlap
// would discard the farther PR the session demonstrably contributed to — and
// FILE_OVERLAP cannot recover it, its window being the shorter of the two.
func matchBranch(session *attributionv1.SessionEvidence, prs []*attributionv1.PRCandidate, params Params) []*attributionv1.PRMatch {
	if session == nil {
		return nil
	}
	branches := collectBranches(session)
	if len(branches) == 0 {
		// Nothing to scope by, and membership against an empty set would exclude
		// every candidate anyway. A branchless session is not thereby
		// unattributable — FILE_OVERLAP runs as its own signal and is untouched.
		return nil
	}

	sessionDate := sessionStartDate(session)
	anchor := sessionMatchAnchor(session, params)
	windowTo := anchor.AddDate(0, 0, effectiveStrongSignalWindowDays(params))
	repos := knownRepoSet(prs)
	sessionFiles := extractFilesEdited(session)
	gated := sessionIsGated(session)

	// Only gathered for a session whose candidates will actually be gated.
	var gate branchGateInputs
	if gated {
		gate = newBranchGateInputs(session, params)
	}

	bestByBranch := make(map[branchKey]branchCand)
	for _, pr := range prs {
		if mergedAt(pr).Before(sessionDate) || mergedAt(pr).After(windowTo) {
			continue
		}
		if isGenericBranch(pr.GetHeadBranch()) {
			continue
		}
		branchSrc, hit := branches[pr.GetHeadBranch()]
		if !hit {
			continue
		}
		// The scope is now fixed; from here the candidate must earn the match.
		var evidence branchEvidence
		if gated {
			var hasEvidence bool
			evidence, hasEvidence = branchEvidenceFor(pr, branchSrc, sessionFiles, gate.sessionRepo, gate.refs, repos, sessionDate, anchor, gate.threshold)
			// Counted on both arms, before the dedup drops the losers: the rejection
			// is otherwise invisible, and the sum of the two is the only way to tell
			// a gate that decided from one that never ran.
			outcome := branchGateCorroborated
			if !hasEvidence {
				outcome = branchGateNoEvidence
			}
			params.obs().Count("branch_gate_evaluations", map[string]string{
				"outcome": outcome, "ide_type": gate.ideType, "org_id": session.GetOrgId()})
			if !hasEvidence {
				continue
			}
		}
		gap := nearestEndDistance(sessionDate, anchor, mergedAt(pr))
		key := branchKey{repo: pr.GetPrRepository(), branch: pr.GetHeadBranch()}
		cur, ok := bestByBranch[key]
		switch {
		case !ok || gap < cur.gap:
			bestByBranch[key] = branchCand{pr: pr, gap: gap, evidence: evidence}
		case gap == cur.gap:
			// A tie does not imply a same-day merge: distance is to the nearer
			// end of the session, so two PRs straddling it symmetrically tie while
			// merging weeks apart. They are genuinely equally close, so resolve by
			// the lexicographically-smaller key — the winner must never depend on
			// slice order.
			if pr.GetPrKey() < cur.pr.GetPrKey() {
				bestByBranch[key] = branchCand{pr: pr, gap: gap, evidence: evidence}
			}
		}
	}

	out := make([]*attributionv1.PRMatch, 0, len(bestByBranch))
	for _, c := range bestByBranch {
		confidence := confidenceBranchMatch * timeDecay(sessionDate, anchor, mergedAt(c.pr))
		matchedFiles := c.evidence.files
		if !gated {
			matchedFiles = MatchedFiles(sessionFiles, c.pr.GetFilesChanged(), repos)
		}
		out = append(out, prMatchFromContext(c.pr, confidence, matchedFiles, c.evidence.kind))
	}
	return out
}

// branchEvidenceFor evaluates whether a branch-scoped candidate carries evidence
// specific to THIS session, returning what satisfied the gate.
//
// The two arms are gated symmetrically — neither attributes on branch alone —
// and differ only in which evidence class can satisfy them. A reviewer reads
// rather than edits, so a file test is the wrong instrument there; exempting the
// reviewer arm from ANY test would leave it branch-only and reproduce on the
// reviewer side exactly the sharing failure the author side is closing.
//
// The arm is keyed on the candidate's fetch-side kind, the only kind that exists
// at gate time — classifyParticipationKind runs later, over the emitted matches.
// A self-reviewed PR sits in both candidate sets under one record key, so it can
// clear the author arm on its AUTHORED copy and still be stored REVIEWED. That
// is intended: the person did author it, and the reviewer arm's PR-naming bar
// exists for candidates they did not author, not for the REVIEWED label.
// Anything not AUTHORED takes the reviewer arm, so an unexpected kind fails
// closed onto the stricter test.
//
// branchSrc is how the session came to be on this head branch. A source the
// session itself produced (it created, pushed, or narrated the branch) corroborates
// on its own — the session has proven it worked on the exact branch the PR merged,
// which is stronger evidence than an incidental file overlap and does not decay with
// the file count. Only a branch the WORKSPACE merely held (a reflog read, or an
// unclassified value) still has to earn the match through file overlap, because
// that source says nothing about what THIS session did.
func branchEvidenceFor(
	pr *attributionv1.PRCandidate,
	branchSrc attributionv1.SignalSource,
	sessionFiles []string,
	sessionRepo string,
	refs []sourcedPRRef,
	repos map[string]struct{},
	sessionDate time.Time,
	sessionAnchor time.Time,
	threshold float64,
) (branchEvidence, bool) {
	// A session-produced branch corroborates on both arms — the account is the
	// session's own, so it needs neither a file test (author) nor a naming
	// reference (reviewer). isSessionProducedSource is the same bar refMayAttribute
	// applies to a reference, reused so a branch and a reference cannot drift on
	// what "the session produced this" means.
	if isSessionProducedSource(branchSrc) {
		return branchEvidence{
			kind: attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_BRANCH_PROVENANCE,
		}, true
	}
	if pr.GetParticipationKind() == attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED {
		matched, _, ok := fileOverlapEvidence(sessionFiles, pr, sessionRepo, repos, sessionDate, sessionAnchor, threshold)
		if !ok {
			return branchEvidence{}, false
		}
		return branchEvidence{
			kind:  attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_FILE_OVERLAP,
			files: matched,
		}, true
	}
	return reviewerBranchEvidence(pr, sessionRepo, refs)
}

// reviewerBranchEvidence corroborates a reviewer-side candidate with a reference
// that NAMES this PR and was produced by this session.
//
// Naming the PR is necessary but not sufficient — the bar is isSessionProduced,
// which states why. Reusing that one bar rather than a looser local test is what
// keeps a reference from being too weak to attribute directly and strong enough
// to corroborate a branch.
// Selection compares the repo case-insensitively but otherwise exactly — NOT via
// sameRepo. Case-folding alone fixes the real producer disagreement ("Org/Repo"
// against the connector's "org/repo"), which would otherwise drop a reference
// naming this exact PR on the one arm that has no file evidence to fall back on.
// sameRepo would be wrong here: it falls back to BASENAME equality when either
// side carries no owner, so a bare "gadgets" reference would corroborate
// "orgB/gadgets" — turning a dropped match into a match on the wrong repository,
// which is the worse of the two failures. The prose gate below does use sameRepo,
// because there both sides are owner-qualified repo paths by construction.
func reviewerBranchEvidence(pr *attributionv1.PRCandidate, sessionRepo string, refs []sourcedPRRef) (branchEvidence, bool) {
	for _, ref := range refs {
		if ref.number != pr.GetPrNumber() || !strings.EqualFold(ref.repo, pr.GetPrRepository()) {
			continue
		}
		// The same bar the direct-reference method applies. Note the prose repo
		// gate inside it compares against the SESSION's repo, not the candidate's:
		// the selection above already tied the reference to pr.GetPrRepository(), so re-testing
		// that would be vacuous.
		if !refMayAttribute(ref, sessionRepo) {
			continue
		}
		return branchEvidence{
			kind: attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_PR_REFERENCE,
		}, true
	}
	return branchEvidence{}, false
}

// botBranchPrefixes are the dependency-bot head-branch namespaces filtered by isGenericBranch.
var botBranchPrefixes = []string{"dependabot/", "renovate/"}

// IsGenericBranch reports whether a head branch can never produce a BRANCH_MATCH:
// an empty name or a dependency-bot branch (dependabot/, renovate/). Shared
// default branches such as main are not generic here. IsSharedDefaultBranch
// answers that separate question.
func IsGenericBranch(branch string) bool { return isGenericBranch(branch) }

// isGenericBranch reports whether a head-branch carries no attribution signal.
// Empty branches and bot-owned namespaces (dependabot/, renovate/) are always
// suppressed. Common default branch names (main, master, develop, …) are
// intentionally NOT filtered: orgs vary in whether engineers push features
// directly to those branches (e.g. GitFlow where develop is active), and a
// false-negative (dropped real attribution) is worse than the rare false-positive
// of matching a release-promotion PR on a shared branch name.
func isGenericBranch(branch string) bool {
	b := strings.ToLower(strings.TrimSpace(branch))
	if b == "" {
		return true
	}
	for _, p := range botBranchPrefixes {
		if strings.HasPrefix(b, p) {
			return true
		}
	}
	return false
}

// branchSourceRank orders SignalSource strongest-first for the branch merge:
// a session's own record outranks the workspace's, so a command beats a vendor
// field beats prose beats a local-git read, and an unclassified value ranks
// last.
//
// Every value is enumerated with no default arm, deliberately. The exhaustive
// linter treats a default as covering the enum, so a default would let the next
// SignalSource land silently at rank 0 — barred from supporting an attribution
// — on the very field whose purpose is ranking sources. Without one, adding a
// value fails the build until someone decides where it sits.
func branchSourceRank(s attributionv1.SignalSource) int {
	switch s {
	case attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND:
		return 4
	case attributionv1.SignalSource_SIGNAL_SOURCE_VENDOR_FIELD:
		return 3
	case attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_PROSE:
		return 2
	case attributionv1.SignalSource_SIGNAL_SOURCE_LOCAL_GIT_WINDOWED:
		return 1
	case attributionv1.SignalSource_SIGNAL_SOURCE_UNSPECIFIED:
		return 0
	}
	return 0
}

// collectBranches returns the deduped branches to match against, each with the
// strongest source that named it.
//
// The source is load-bearing: matchBranch reads it, and a session-produced source
// (a branch the session created, pushed, or narrated) corroborates the match on
// its own — see branchEvidenceFor. So the merge must keep the STRONGEST source per
// branch, or a corroborating command source could be shadowed by a workspace read.
//
// The three producers MERGE rather than take precedence, and that is the whole
// point of the function. Making a non-empty branch_signals win outright would
// turn a producer that stamps only some branches — writing branch_signals for
// some and git_branches for others — from a harmless source downgrade into
// silent loss of the branches it did not stamp. Merging fails the other way: a
// stray unstamped name enters at UNSPECIFIED, which can scope a search but cannot itself
// corroborate a match, so the cost is a slightly wider search, never a fabricated
// link.
//
// The single-value git_branch scalar stays a fallback for when neither repeated field
// carries anything, which is the only population that has nothing else.
func collectBranches(s *attributionv1.SessionEvidence) map[string]attributionv1.SignalSource {
	set := make(map[string]attributionv1.SignalSource)
	keepStrongest := func(name string, src attributionv1.SignalSource) {
		if name == "" {
			return
		}
		if existing, seen := set[name]; seen && branchSourceRank(existing) >= branchSourceRank(src) {
			return
		}
		set[name] = src
	}

	for _, b := range s.GetBranchSignals() {
		keepStrongest(b.GetName(), b.GetSource())
	}
	// Carries no source, so it enters unclassified — never as a claim that the
	// branch was ambient, which is a different statement we cannot make.
	for _, b := range s.GetGitBranches() {
		keepStrongest(b, attributionv1.SignalSource_SIGNAL_SOURCE_UNSPECIFIED)
	}
	if len(set) == 0 {
		keepStrongest(s.GetGitBranch(), attributionv1.SignalSource_SIGNAL_SOURCE_UNSPECIFIED)
	}
	return set
}

// matchFileOverlap scores each candidate through fileOverlapEvidence and emits
// every PR that clears the MinConfidence threshold — not just the single best —
// within the asymmetric time window.
//
// A refused candidate is then scored by Jaccard similarity (the Jaccard shadow).
// A pair the looser rule would admit is counted as jaccard_shadow_evaluations,
// and the session's admitted pairs are reported once as the jaccard_shadow_admits
// debug event. The shadow persists nothing and emits no match.
func matchFileOverlap(session *attributionv1.SessionEvidence, prs []*attributionv1.PRCandidate, params Params) []*attributionv1.PRMatch {
	sessionFiles := extractFilesEdited(session)
	if len(sessionFiles) == 0 {
		return nil
	}

	sessionDate := sessionStartDate(session)
	anchor := sessionMatchAnchor(session, params)
	windowTo := anchor.AddDate(0, 0, params.TimeWindowAfterDays)
	repos := knownRepoSet(prs)
	threshold := float64(params.MinConfidencePercent) / 100.0
	// Confine overlap to the session's own repo. normalizePath strips the repo
	// prefix so paths compare repo-relative, which means a shared basename
	// (README.md, main.go) would otherwise overlap a PR in a *different* repo
	// the person also owns. Compare the full owner/repo (not the leaf) so two
	// same-named repos under different owners — a fork vs upstream, orgA/gadgets
	// vs orgB/gadgets — don't collide. Inert unless the session carries an
	// owner-qualified repo_name: a project_name-only session is left to the
	// caller, which can vet it against the repositories it knows, and
	// its folder basename (e.g. "gadgets-checkout" for repo "gadgets") is too
	// unreliable to gate on here.
	sessionRepo := strings.ToLower(session.GetRepoName())

	var out []*attributionv1.PRMatch
	var shadowSample []string
	shadowAdmitted := 0
	for _, pr := range prs {
		// In window if EITHER the merged date OR the opened date lands in
		// [sessionDate, windowTo]. Keying on opened too (not merged alone) admits a
		// PR raised near the session but merged later after slow review, without
		// widening the window itself. The opened arm requires opened >= sessionDate
		// (a PR opened before the session is only the session's via the merged arm),
		// and the open date is zero on reviewer-side candidates and records without
		// a payload, where only the merged arm applies.
		mergedInWindow := !mergedAt(pr).Before(sessionDate) && !mergedAt(pr).After(windowTo)
		openedInWindow := !openedAt(pr).IsZero() &&
			!openedAt(pr).Before(sessionDate) && !openedAt(pr).After(windowTo)
		if !mergedInWindow && !openedInWindow {
			continue
		}
		matched, score, ok := fileOverlapEvidence(sessionFiles, pr, sessionRepo, repos, sessionDate, anchor, threshold)
		if !ok {
			if jaccard, admits := jaccardShadowAdmits(sessionFiles, pr, sessionRepo, repos); admits {
				params.obs().Count("jaccard_shadow_evaluations", map[string]string{"org_id": session.GetOrgId()})
				shadowAdmitted++
				if len(shadowSample) < shadowLogSampleSize {
					shadowSample = append(shadowSample,
						fmt.Sprintf("%s@%.3f", pr.GetPrKey(), jaccard))
				}
			}
			continue
		}
		// Stamped even though this method's name already implies it: leaving the
		// field empty here would make it mean "no evidence" on one method and
		// "not populated" on another, forcing every reader to be method-aware.
		out = append(out, prMatchFromContext(pr, score, matched,
			attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_FILE_OVERLAP))
	}
	if shadowAdmitted > 0 {
		params.obs().Debug("jaccard_shadow_admits", map[string]any{
			"org_id":         session.GetOrgId(),
			"session_id":     session.GetSessionId(),
			"admitted_pairs": shadowAdmitted,
			"sampled_pairs":  shadowSample,
		})
	}
	return out
}

// fileOverlapEvidence reports whether the session's edits land in this PR's diff,
// returning the matched set and the time-decayed score that cleared the bar.
//
// This is the single definition of "the session's edits landed in this PR":
// matchFileOverlap emits on it, and matchBranch's author arm corroborates on it.
// A bare non-empty intersection is deliberately NOT that bar — a lone
// coincidental file, especially a ubiquitous basename like README.md, would
// satisfy it, and on a shared feature branch unrelated sessions routinely touch
// one common file. Keeping one definition is what stops the two callers drifting
// into admitting different overlaps.
//
// Callers apply their own time window first; the windows differ by design and
// that difference is the branch arm's whole remaining value over this one.
func fileOverlapEvidence(
	sessionFiles []string,
	pr *attributionv1.PRCandidate,
	sessionRepo string,
	repos map[string]struct{},
	sessionDate time.Time,
	sessionAnchor time.Time,
	threshold float64,
) ([]string, float64, bool) {
	if len(sessionFiles) == 0 || len(pr.GetFilesChanged()) == 0 {
		return nil, 0, false
	}
	if crossRepo(sessionRepo, pr.GetPrRepository()) {
		return nil, 0, false
	}
	score := ScoreFileOverlap(sessionFiles, pr.GetFilesChanged(), repos) * timeDecay(sessionDate, sessionAnchor, decayDate(pr))
	if score < threshold {
		return nil, 0, false
	}
	// Only now (threshold passed) compute the matched set. Kept after the score
	// gate so sub-threshold candidates pay no extra path-normalization.
	matched := MatchedFiles(sessionFiles, pr.GetFilesChanged(), repos)
	if !meetsMinIntersection(matched) {
		return nil, 0, false
	}
	return matched, score, true
}

// crossRepo reports whether the pair is confined out: the session and the PR are
// in repos both known to differ. Inert when either side is unknown — a PR whose
// metadata omits "repo" (pr.GetPrRepository() == "") must still reach scoring
// rather than be dropped for lacking a repo.
//
// sessionRepoLower must already be lowercased — sameRepo lowercases only the PR
// side.
func crossRepo(sessionRepoLower, prRepo string) bool {
	return sessionRepoLower != "" && prRepo != "" && !sameRepo(sessionRepoLower, prRepo)
}

// sameRepo reports whether an owner-qualified session repo (lowercased
// "org/repo") and a PR repo refer to the same repository. When both carry an
// owner segment it compares the last two path segments so a GitLab nested
// subgroup PR path ("group/sub/repo") matches the session's git-remote last-2
// ("sub/repo"), while owner-distinct repos ("orgA/gadgets" vs "orgB/gadgets" — both
// already two segments) stay distinct. If the PR repo lacks an owner (metadata
// without "owner") it falls back to basename equality — the most precise
// comparison still possible.
func sameRepo(sessionRepoLower, prRepo string) bool {
	pr := strings.ToLower(prRepo)
	if strings.Contains(sessionRepoLower, "/") && strings.Contains(pr, "/") {
		return lastTwoSegments(sessionRepoLower) == lastTwoSegments(pr)
	}
	return crossPlatformBasename(sessionRepoLower) == crossPlatformBasename(pr)
}

// lastTwoSegments returns the final two "/"-delimited segments of s joined by
// "/" (or s unchanged when it has fewer than two), the canonical repo key both
// sides reduce to before comparison.
func lastTwoSegments(s string) string {
	parts := strings.Split(s, "/")
	if len(parts) <= 2 {
		return s
	}
	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

// commonBasenames are files so ubiquitous across repos that a lone overlap on
// one is no evidence a session contributed to a PR — a session and an unrelated
// PR routinely both touch a README, a manifest, or a package marker. Hand-
// curated project-marker / lockfile / entrypoint files for Go, JS/TS, Python
// and Rust. A lone overlap on a file in this list is never evidence; a lone
// overlap on any other file is. Not exhaustive by design: a ubiquitous file
// missing from the list lets a single coincidental overlap on it count.
var commonBasenames = map[string]struct{}{
	"readme.md": {}, "readme": {}, "readme.rst": {}, "readme.txt": {},
	"index.ts": {}, "index.js": {}, "index.tsx": {}, "index.jsx": {}, "index.mjs": {}, "index.html": {},
	"makefile": {}, "dockerfile": {}, ".dockerignore": {}, ".gitignore": {}, ".editorconfig": {},
	"main.go": {}, "go.mod": {}, "go.sum": {},
	"package.json": {}, "package-lock.json": {}, "pnpm-lock.yaml": {}, "yarn.lock": {}, "tsconfig.json": {},
	"__init__.py": {}, "setup.py": {}, "conftest.py": {}, "requirements.txt": {},
	"mod.rs": {}, "lib.rs": {}, "cargo.toml": {},
}

// jaccardShadowAdmits reports whether the looser Jaccard rule would admit a pair
// the file-overlap gate refused, and the Jaccard it scored. It keeps the gate's
// repo confinement and its minimum-intersection rule, and relaxes only the score.
func jaccardShadowAdmits(
	sessionFiles []string,
	pr *attributionv1.PRCandidate,
	sessionRepo string,
	repos map[string]struct{},
) (float64, bool) {
	if crossRepo(sessionRepo, pr.GetPrRepository()) {
		return 0, false
	}
	intersection, sessionSize, prSize := overlapCounts(sessionFiles, pr.GetFilesChanged(), repos)
	if intersection == 0 {
		return 0, false
	}
	jaccard := jaccardFromCounts(intersection, sessionSize, prSize)
	if jaccard < shadowJaccardThreshold {
		return jaccard, false
	}
	if !meetsMinIntersection(MatchedFiles(sessionFiles, pr.GetFilesChanged(), repos)) {
		return jaccard, false
	}
	return jaccard, true
}

// meetsMinIntersection guards a file-overlap match against coincidental overlap on
// ubiquitous files: it requires at least one overlapping file that is NOT a
// ubiquitous cross-repo basename (commonBasenames). A lone common file never
// qualifies — and neither does an overlap consisting ENTIRELY of common files,
// however many: two unrelated sessions both touching only package.json + index.ts
// share no real work. One specific
// (non-ubiquitous) file, alone or alongside others, is the evidence bar.
//
// Load-bearing for a DESTRUCTIVE path, not only match quality: storedMatchLacksEvidence
// (merge.go) reads this to decide whether an evidence-less match is WITHDRAWABLE. FILE_OVERLAP
// matches are never withdrawn (merge.go returns false for that method), so tightening
// this cannot retroactively delete a file-overlap match. The only withdrawal effect is
// that an evidence-unspecified AUTHORED BRANCH_MATCH whose stored overlap is
// entirely ubiquitous files reads as evidence-lacking and becomes withdrawable on
// re-examine — a bounded, correct cleanup of exactly the coincidence this gate excludes.
// Treat further tightening (or widening commonBasenames) as a data-deletion change.
func meetsMinIntersection(matched []string) bool {
	switch len(matched) {
	case 0:
		return false
	case 1:
		_, common := commonBasenames[strings.ToLower(crossPlatformBasename(matched[0]))]
		return !common
	default:
		// >=2 files: require at least one non-ubiquitous file. An overlap made up
		// ENTIRELY of ubiquitous basenames (e.g. index.ts + package.json) is
		// coincidence, not evidence. Kept as a switch so a future coincidence
		// class (generated files, high-author-churn files, …) plugs in as another
		// case or an additional disqualifier in this scan.
		for _, f := range matched {
			if _, common := commonBasenames[strings.ToLower(crossPlatformBasename(f))]; !common {
				return true
			}
		}
		return false
	}
}

// knownRepoSet extracts repo short-names from PR contexts. PR `Repo` is
// "org/repo-name" (GitHub/Bitbucket) or similar for GitLab — we only need the
// trailing component, which is what appears as a segment in session paths.
// An empty set (zero PRs, or all PRs with blank Repo) means normalizePath
// falls back to its existing behavior.
func knownRepoSet(prs []*attributionv1.PRCandidate) map[string]struct{} {
	if len(prs) == 0 {
		return nil
	}
	repos := make(map[string]struct{}, len(prs))
	for _, pr := range prs {
		r := pr.GetPrRepository()
		if slash := strings.LastIndex(r, "/"); slash >= 0 && slash+1 < len(r) {
			r = r[slash+1:]
		}
		if r != "" {
			repos[r] = struct{}{}
		}
	}
	return repos
}

// timeDecayFloor is the lowest factor timeDecay returns.
const timeDecayFloor = 0.75

// decayDate is the date a candidate's time decay is measured to: its merge, or
// its open date while it has not merged. A still-open pull request reaches the
// file-overlap signal through its open date, and decaying from that same date
// keeps the score inside [0, 1] instead of measuring to a date that is not set.
func decayDate(pr *attributionv1.PRCandidate) time.Time {
	if m := mergedAt(pr); !m.IsZero() {
		return m
	}
	return openedAt(pr)
}

// timeDecay reduces confidence by the day gap between the session and the PR
// merge. Returns 1.0 for same-day, decaying by 0.05 per day, floored at 0.75. An
// undated PR (prDate zero) gets the floor: there is no gap to measure.
//
// The gap is measured to the NEARER end of the session — its start or its last
// activity — because a session is an interval, not an instant, and a PR merged
// close to either end is closely related. Measuring from one end alone penalises
// the other: from the start, a long session's late PR decays to the floor even
// though the work had just stopped; from the anchor, its early PR decays instead,
// which is how a marginal file-overlap match near the start can be pushed under
// MinConfidencePercent.
//
// Because the start is always one of the two candidates, this can only ever
// return a value greater than or equal to a start-anchored decay — widening the
// window can raise a session's confidence but never lower it.
func timeDecay(sessionStart, sessionAnchor, prDate time.Time) float64 {
	if prDate.IsZero() {
		return timeDecayFloor
	}
	days := nearestEndDistance(sessionStart, sessionAnchor, prDate).Hours() / 24
	decay := 1.0 - 0.05*float64(int(days))
	return math.Min(1, math.Max(decay, timeDecayFloor))
}

// nearestEndDistance returns how far a PR merge sits from the session, measured
// to whichever end — start or last activity — is closer.
//
// Shared by timeDecay and matchBranch's reused-branch dedup so the two cannot
// disagree about which candidate is "nearest". They must be one function: the
// dedup keeps a single PR per (repo, branch) and the scorer then rates it, so a
// dedup ranking on a different metric can discard the candidate that would have
// scored highest and store a strictly worse match in its place.
func nearestEndDistance(sessionStart, sessionAnchor, prDate time.Time) time.Duration {
	fromStart := prDate.Sub(sessionStart)
	if fromStart < 0 {
		fromStart = -fromStart
	}
	fromAnchor := prDate.Sub(sessionAnchor)
	if fromAnchor < 0 {
		fromAnchor = -fromAnchor
	}
	// Sub saturates at the Duration bounds, and negating the minimum overflows
	// back to it; a saturated gap is "as far as can be measured".
	if fromStart < 0 {
		fromStart = math.MaxInt64
	}
	if fromAnchor < 0 {
		fromAnchor = math.MaxInt64
	}
	return min(fromStart, fromAnchor)
}
