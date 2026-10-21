// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"google.golang.org/protobuf/proto"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// retractionInputs are the three facts the reducer cannot derive from the two
// match sets, and each carries a safety property that is lost if it is stubbed.
//
// The reducer sees two match sets, never the candidate universe — which is why
// retraction is one narrow carve-out rather than "withdraw anything the current
// rules would not produce". It cannot re-evaluate a gate.
type retractionInputs struct {
	// gated is whether the stricter producer rules govern this session. Without
	// it the reducer would withdraw matches for IDEs whose producer still emits
	// them, because the reducer has no IDE awareness of its own.
	gated bool
	// evaluated is whether the fresh result came from signals running over a
	// NON-EMPTY candidate set. An empty fresh set from zero candidates is absence
	// of evidence, not evidence of absence, and retracting on it is exactly the
	// shrinkage the union exists to prevent.
	evaluated bool
	// producerStamped is whether this session's producer can express the evidence
	// the current rules demand at all.
	//
	// Without it the reducer cannot separate "the signals were stamped and came
	// back ambient" — the only case that justifies deleting — from "this producer
	// cannot stamp yet", and it deletes on both. Only the first is a rules
	// rejection. The second is reachable whenever a producer records no source,
	// because gating keys on ide_type while the corroborators
	// (BranchSignal, a reference's SignalSource) arrive only from a producer new
	// enough to emit them: so an unstamped session's reviewer-side branch match is
	// unsatisfiable by construction, not unsupported by evidence.
	//
	// This makes the deletion depend on the session rather than on time: a
	// session becomes withdrawable exactly when its producer can express the
	// evidence, with no counter or per-deployment lever needed to hold it back.
	producerStamped bool
	// possiblePRs is every PR record key this run fetched, before any gate ran.
	// A stored match absent from the fresh set is withdrawn only when its pull
	// request is in here: refused by the rules, not merely unexamined.
	possiblePRs map[string]struct{}
	// refProvenanceNow maps (repo, number) to the strongest source naming that PR
	// in the session's CURRENT references.
	//
	// Read from the session rather than from a field on the match, because a
	// match's own provenance is its value at emission time: UNSPECIFIED on any
	// row written before provenance stamping, and never able to become
	// LOCAL_GIT_WINDOWED, since the producer emits nothing for an ambient
	// reference. The session's references, by contrast, are re-stamped when the
	// session is re-extracted, so an unstamped reference becomes withdrawable once
	// the session is re-extracted with provenance.
	refProvenanceNow map[prRef]attributionv1.SignalSource
}

// producerCanStampEvidence reports whether this session's producer emits the
// evidence the current rules require.
//
// Presence of either stamp is enough, and neither implies the other: a session may
// hold a branch and no reference, or a reference and no branch. Either stamp means
// the producer can express evidence. A session with neither is not refused for its
// work.
func producerCanStampEvidence(s *attributionv1.SessionEvidence) bool {
	if len(s.GetBranchSignals()) > 0 {
		return true
	}
	for _, r := range s.GetPrReferences() {
		if r.GetSource() != attributionv1.SignalSource_SIGNAL_SOURCE_UNSPECIFIED {
			return true
		}
	}
	return false
}

// refProvenanceFromSession builds the lookup over the SAME source set the matcher
// reads — pr_references plus the single-value scalar fields (pr_number,
// pr_repository) it falls back to.
//
// It reuses collectPRRefs rather than re-walking the fields, which is what keeps
// the two from diverging: a lookup omitting the scalar population would MISS for
// those rows, and a miss is not LOCAL_GIT_WINDOWED, so they would stay
// unwithdrawable forever — narrower than the producer's gate, which the mirror
// invariant forbids in that direction.
func refProvenanceFromSession(s *attributionv1.SessionEvidence) map[prRef]attributionv1.SignalSource {
	refs := collectPRRefs(s)
	if len(refs) == 0 {
		return nil
	}
	out := make(map[prRef]attributionv1.SignalSource, len(refs))
	for _, r := range refs {
		out[r.prRef] = r.source
	}
	return out
}

// storedMatchLacksEvidence reports whether a stored match carries no evidence the
// current rules would accept.
//
// One clause covers both arms and does not branch on participation_kind: a
// populated evidence field IS the claim, because the producer cannot emit a match
// without one. Only a match with no evidence field falls back to the single
// piece of evidence it does carry.
//
// That fallback is author-only on purpose. A reviewer-side match may carry
// matched_files without naming a pull request, so only author-side matches fall
// back to it; crediting a reviewer-side one would preserve the branch-only
// reviewer attribution the gate refuses. An ABSENT kind counts as non-author, which is right for the
// evidence-less matches this targets.
//
// The fallback applies meetsMinIntersection's own test rather than bare
// emptiness: an evidence-less match whose matched_files is exactly ["README.md"] would
// otherwise read as supported, which is the precise coincidence the gate exists
// to exclude. It is a pure function of the stored array, so it needs no
// recomputation.
func storedMatchLacksEvidence(m *attributionv1.PRMatch) bool {
	if m.GetEvidence() != attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_UNSPECIFIED {
		return false
	}
	if m.GetParticipationKind() == attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED {
		return !meetsMinIntersection(m.GetMatchedFiles())
	}
	return true
}

// storedMatchIsWithdrawable reports whether a stored match is one the current
// rules do not produce — the single carve-out from the adds-only union.
//
// Two shapes qualify, differing only in which gate they fail. A BRANCH_MATCH with
// no corroborator mirrors what matchBranch refuses on either arm. And a
// DIRECT_PR_REF with no evidence whose naming reference is ambient in the
// session's current references is unsupported on identical grounds — a scraped
// URL is a scope, not an attribution, and without this clause a URL scraped from
// a teammate's commit is withdrawable by nothing.
//
// The two conditions on the direct arm are independent and both required: the
// absent evidence field says we never had one, and the lookup says the naming
// evidence is ambient now. A genuine `gh pr create` receipt fails both.
//
// The lookup is keyed on (repo, number), not pr_key — pr_key is the caller's
// record key and lives in a different keyspace, so keying on it would miss every
// row silently.
func storedMatchIsWithdrawable(m *attributionv1.PRMatch, in retractionInputs) bool {
	if !in.gated || !in.evaluated || !in.producerStamped {
		return false
	}
	// Only a PR this run actually looked at can be rejected by the rules. One it
	// never fetched is unexamined, and treating unexamined as refused is how a
	// narrowed window or a purged partition turns into deleted data.
	if _, examined := in.possiblePRs[m.GetPrKey()]; !examined {
		return false
	}
	switch m.GetMethod() {
	case attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH:
		return storedMatchLacksEvidence(m)
	case attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF:
		if m.GetEvidence() != attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_UNSPECIFIED {
			return false
		}
		key := prRef{repo: m.GetPrRepository(), number: m.GetPrNumber()}
		return in.refProvenanceNow[key] == attributionv1.SignalSource_SIGNAL_SOURCE_LOCAL_GIT_WINDOWED
	case attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP,
		attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_AI_SIGNATURE,
		attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_UNSPECIFIED:
		// Never withdrawn here. These methods rest on neither a branch nor a
		// reference, so nothing this gate re-derives can contradict them, and an
		// unrecognised method is not evidence that the rules refused it.
		return false
	}
	return false
}

// mergePRAttribution folds a freshly computed attribution into the stored one,
// adds-only. Re-running attribution on an already-matched session makes the write
// a replacement rather than the only snapshot, and a replacement can lose data.
//
// Scoring itself is deterministic — timeDecay reads two fixed dates — so a
// recompute never re-scores an old match differently. What can shrink is the
// *candidate set*, and a shrunken recompute would silently drop matches it no
// longer sees: an unlinked or re-merged VCS identity removes its PRs, narrowing
// StrongSignalWindowAfterDays drops a match found at a wider gap, and a store may
// purge old candidate records. Union with the stored matches is what keeps those.
//
// Rules, each load-bearing:
//
//   - Union by pr_key. A stored match absent from fresh is kept — unless its
//     participation_kind is UNSPECIFIED, which is dropped instead, whether or not
//     this run fetched its pull request, and is not returned with the withdrawals.
//     reconcileMatch can only fill the kind in when fresh carries the same pr_key,
//     and a store that re-selects rows holding a kind-less match (to backfill the
//     kind, as IsPending does) would otherwise select the row on every run.
//   - participation_kind on conflict: UNSPECIFIED loses to an explicit kind;
//     between two explicit kinds the stored one wins, except that AUTHORED
//     replaces REVIEWED. Keeping stored guards against a kind flip: the
//     session's task_type is what routes a self-reviewed PR to AUTHORED vs
//     REVIEWED, a producer may reclassify it on re-export, and the re-export that
//     triggers re-attribution would otherwise move the PR out of a reader's
//     AUTHORED filter.
//     The AUTHORED exception is one-directional for the same reason it is safe:
//     AUTHORED is only ever produced for a PR that merged *and* is authored by
//     this person, whereas REVIEWED is reachable from the author's own review
//     comments while the PR is still open — so an upgrade rests on strictly more
//     evidence, while the downgrade it still blocks rests on less. That rests on
//     an invariant this exception makes load-bearing: every match's pr_key is in
//     at least one of classifyParticipationKind's two key sets — a self-reviewed
//     PR's key is in both — so the neither-set case is unreachable and every
//     default-to-AUTHORED return therefore has isAuthor true. A key that could
//     reach that arm from neither set would carry no authorship evidence at all,
//     and would overwrite a real stored REVIEWED. Letting UNSPECIFIED lose is
//     equally required: a recompute exists to write an explicit kind over
//     UNSPECIFIED, and a blanket keep-stored rule would pin it forever.
//   - The higher confidence_tier wins, and the winning entry is taken wholesale.
//     Tier is derived from (method, ide_type, ai_attribution_confidence) by the
//     scorer, so lifting a tier onto the other entry's method would produce a
//     record that no scorer run could have produced.
//   - lines_added / lines_removed take the max across observations: a later sync
//     may carry more complete file stats.
//   - unmatched is cleared whenever the merged matches are non-empty. The proto
//     states unmatched is populated only when matches is empty, and a recompute
//     that finds nothing sets unmatched while the stored matches survive the
//     union — so without this the row would violate its own invariant.
//
// serviced_reattribution_seq on the result is always servicedSeq — the request
// value the caller read when it loaded the batch, not whatever the session
// carries now. Recording what was actually serviced (rather than "no request
// outstanding") is what makes a request raised mid-computation survive: the
// session's reattribution_seq stays ahead of this value and it is selected again.
//
// The result's match order is stable — stored order, then newly-found matches in
// fresh order — so an unchanged merge does not churn the stored value.
// The second return is every stored match this merge dropped as unproducible.
// It is returned rather than counted here because the deletion is not real until
// the row is written: counted at the drop, a rolled-back or short-counted write
// would still move the only counter guarding an irreversible write, and a write
// outage would look like a withdrawal. The caller owns both the counting and the
// per-row record a repair would need.
func mergePRAttribution(
	stored, fresh *attributionv1.PRAttribution,
	servicedSeq int32,
	in retractionInputs,
) (*attributionv1.PRAttribution, []*attributionv1.PRMatch) {
	// A nil fresh result carries no new information, but the attempt still
	// happened — return the stored matches with the serviced marker advanced, never
	// the stored message verbatim, or the row would be re-selected on every run
	// for a request that has been serviced.
	if fresh == nil {
		return normalizeAttribution(stored.GetMatches(), stored.GetUnmatched(), servicedSeq), nil
	}
	if stored == nil || len(stored.GetMatches()) == 0 {
		// Nothing to preserve. Still stamp the serviced marker and enforce the
		// matches/unmatched invariant so the two entry paths agree.
		return normalizeAttribution(fresh.GetMatches(), fresh.GetUnmatched(), servicedSeq), nil
	}

	// Needed before the stored pass: whether fresh still sees a pr_key decides
	// whether a stored UNSPECIFIED-kind match can still be completed, and so
	// whether keeping it would strand the row.
	freshKeys := make(map[string]struct{}, len(fresh.GetMatches()))
	for _, f := range fresh.GetMatches() {
		if f != nil {
			freshKeys[f.GetPrKey()] = struct{}{}
		}
	}

	merged := make([]*attributionv1.PRMatch, 0, len(stored.GetMatches())+len(fresh.GetMatches()))
	byKey := make(map[string]int, len(stored.GetMatches()))
	// Entries kept but known unsupported, so fresh evidence REPLACES rather than
	// reconciles them. Marked on withdrawable, not on the branch clause alone: a
	// stored DIRECT_PR_REF carries flat confidence 1.0 with no decay, so an
	// unmarked ambient one wins reconcile's tie against the fresh corroborated
	// match and the row keeps the scraped URL it was re-opened to remove.
	lacksEvidenceAt := make(map[int]struct{})
	var withdrawn []*attributionv1.PRMatch
	for _, m := range stored.GetMatches() {
		if m == nil {
			continue
		}
		if _, dup := byKey[m.GetPrKey()]; dup {
			// A stored array should carry one entry per pr_key; if an older
			// write violated that, keep the first and drop the rest rather
			// than propagating the duplicate.
			continue
		}
		if m.GetParticipationKind() == attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_UNSPECIFIED {
			if _, stillFound := freshKeys[m.GetPrKey()]; !stillFound {
				// Not completable by this run, so it is dropped — see the union rule
				// above. It is not reported as a withdrawal.
				continue
			}
		}
		withdrawable := storedMatchIsWithdrawable(m, in)
		if withdrawable {
			if _, stillFound := freshKeys[m.GetPrKey()]; !stillFound {
				// The carve-out: a match the current rules do not produce, which the
				// recompute does not see either. Withdrawn rather than preserved —
				// shrinkage protection is for a match the recompute lost sight of,
				// not for one the rules do not support.
				//
				// Handed to the caller rather than counted here: this is the only
				// place the deletion is observable at all (every other counter in
				// this package reads the fresh attribution, which by definition does
				// not contain this key), but it is not yet a deletion until the row
				// is written.
				withdrawn = append(withdrawn, m)
				continue
			}
		}
		if withdrawable {
			lacksEvidenceAt[len(merged)] = struct{}{}
		}
		byKey[m.GetPrKey()] = len(merged)
		merged = append(merged, m)
	}

	for _, f := range fresh.GetMatches() {
		if f == nil {
			continue
		}
		idx, seen := byKey[f.GetPrKey()]
		if !seen {
			byKey[f.GetPrKey()] = len(merged)
			merged = append(merged, f)
			continue
		}
		if _, unsupported := lacksEvidenceAt[idx]; unsupported {
			// Fresh evidence displaces an unsupported stored match outright, even
			// on an equal tier — reconcile keeps the stored entry on a tie, which
			// would preserve the uncorroborated match forever behind a PR the
			// recompute now matches properly.
			merged[idx] = f
			delete(lacksEvidenceAt, idx)
			continue
		}
		merged[idx] = reconcileMatch(merged[idx], f)
	}

	return normalizeAttribution(merged, fresh.GetUnmatched(), servicedSeq), withdrawn
}

// reconcileMatch resolves two observations of the same pr_key. The higher-tier
// observation is taken wholesale so (method, confidence, tier) stay mutually
// consistent; participation_kind and the line counts are then reconciled on top
// per mergePRAttribution's rules.
func reconcileMatch(stored, fresh *attributionv1.PRMatch) *attributionv1.PRMatch {
	winner := stored
	if tierRank(fresh.GetConfidenceTier()) > tierRank(stored.GetConfidenceTier()) {
		winner = fresh
	}
	// Clone rather than mutate: winner aliases an entry in the caller's stored or
	// fresh message, and the stored one is the in-memory session row the caller
	// still reads from after this returns.
	out, ok := proto.Clone(winner).(*attributionv1.PRMatch)
	if !ok {
		// Unreachable — proto.Clone returns the same concrete type. Fall back to
		// the winner unmerged rather than nil-panic the whole batch.
		return winner
	}

	// Stored kind wins unless it never carried one, or unless the fresh run has
	// the strictly stronger evidence AUTHORED requires. See mergePRAttribution's
	// participation_kind rule for why the exception runs one way only.
	kind := stored.GetParticipationKind()
	switch {
	case kind == attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_UNSPECIFIED:
		kind = fresh.GetParticipationKind()
	case kind == attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED &&
		fresh.GetParticipationKind() == attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED:
		kind = attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED
	}
	out.ParticipationKind = kind

	out.LinesAdded = max(stored.GetLinesAdded(), fresh.GetLinesAdded())
	out.LinesRemoved = max(stored.GetLinesRemoved(), fresh.GetLinesRemoved())
	return out
}

// tierRank orders the confidence tiers for comparison. UNSPECIFIED ranks lowest
// so a match without a tier never beats a scored one. A reader may
// count UNSPECIFIED as MEDIUM for display; that does not apply here — this is
// a "which observation is better evidence" ordering, not a display default.
func tierRank(t attributionv1.PRMatchConfidenceTier) int {
	switch t {
	case attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_HIGH:
		return 2
	case attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_MEDIUM:
		return 1
	default:
		return 0
	}
}

// normalizeAttribution enforces the whole-message invariants: unmatched is set
// only when there are no matches, and the serviced marker records the request this
// write actually serviced.
func normalizeAttribution(matches []*attributionv1.PRMatch, unmatched *attributionv1.PRUnmatched, servicedSeq int32) *attributionv1.PRAttribution {
	out := &attributionv1.PRAttribution{Matches: matches, ServicedReattributionSeq: servicedSeq}
	if len(matches) == 0 {
		out.Unmatched = unmatched
	}
	return out
}
