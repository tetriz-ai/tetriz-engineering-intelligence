// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"testing"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	kindUnspecified = attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_UNSPECIFIED
	kindAuthored    = attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED
	kindReviewed    = attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED

	tierUnspecified = attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_UNSPECIFIED
	tierMedium      = attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_MEDIUM
	tierHigh        = attributionv1.PRMatchConfidenceTier_PR_MATCH_CONFIDENCE_TIER_HIGH

	// testServicedSeq is an arbitrary non-zero request value; the merge must stamp
	// whatever it is handed, never derive one.
	testServicedSeq int32 = 4
)

func match(prKey string, opts ...func(*attributionv1.PRMatch)) *attributionv1.PRMatch {
	m := &attributionv1.PRMatch{
		PrKey:             prKey,
		PrNumber:          1,
		PrRepository:      "org/repo",
		ConfidenceTier:    tierMedium,
		ParticipationKind: kindAuthored,
		Method:            attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH,
	}
	for _, o := range opts {
		o(m)
	}
	return m
}

func withKind(k attributionv1.SessionParticipationKind) func(*attributionv1.PRMatch) {
	return func(m *attributionv1.PRMatch) { m.ParticipationKind = k }
}

func withTier(t attributionv1.PRMatchConfidenceTier) func(*attributionv1.PRMatch) {
	return func(m *attributionv1.PRMatch) { m.ConfidenceTier = t }
}

func withLines(added, removed int64) func(*attributionv1.PRMatch) {
	return func(m *attributionv1.PRMatch) { m.LinesAdded, m.LinesRemoved = added, removed }
}

func withMethod(mm attributionv1.SessionToPRMatchMethod) func(*attributionv1.PRMatch) {
	return func(m *attributionv1.PRMatch) { m.Method = mm }
}

// keyKinds indexes a merged result by pr_key for order-independent assertions.
func keyKinds(attr *attributionv1.PRAttribution) map[string]*attributionv1.PRMatch {
	out := make(map[string]*attributionv1.PRMatch, len(attr.GetMatches()))
	for _, m := range attr.GetMatches() {
		out[m.GetPrKey()] = m
	}
	return out
}

func TestMergePRAttribution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		stored *attributionv1.PRAttribution
		fresh  *attributionv1.PRAttribution
		// in carries the run's fetched keys; zero means an ungated run that
		// examined nothing.
		in retractionInputs
		// assert runs against the merged result.
		assert func(t *testing.T, got *attributionv1.PRAttribution)
	}{
		{
			// Rule 1: a long-lived session already matched to an earlier PR picks
			// up a newly-merged one without losing the first.
			name:   "rule 1: union keeps the stored match and adds the fresh one",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("repo#PR-101#merged")}},
			fresh:  &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("repo#PR-102#merged")}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				byKey := keyKinds(got)
				require.Len(t, byKey, 2)
				assert.Contains(t, byKey, "repo#PR-101#merged")
				assert.Contains(t, byKey, "repo#PR-102#merged")
				// Stored order first, so the merge does not churn the stored value.
				assert.Equal(t, "repo#PR-101#merged", got.GetMatches()[0].GetPrKey())
			},
		},
		{
			// Rule 1, the drift case: the candidate set shrank between runs (an
			// unlinked identity, a narrowed window, a purged record), so the
			// recompute does not see a PR it matched before.
			name: "rule 1: a stored match the recompute does not see survives",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
				match("repo#PR-101#merged"), match("repo#PR-5670#merged"),
			}},
			fresh: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("repo#PR-101#merged")}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				assert.Len(t, got.GetMatches(), 2, "the dropped candidate must not be lost")
			},
		},
		{
			// Rule 1's carve-out. Keeping a kind-less match the recompute can no
			// longer complete would leave a store that backfills kinds re-selecting
			// this row on every run, forever — reconcileMatch can only fill the
			// kind in when fresh carries the same pr_key, and it never will.
			name: "rule 1 carve-out: an UNSPECIFIED stored match fresh cannot see is dropped",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
				match("repo#PR-1#merged", withKind(kindAuthored)),
				match("repo#PR-2#merged", withKind(kindUnspecified)),
			}},
			in: retractionInputs{possiblePRs: map[string]struct{}{"repo#PR-1#merged": {}, "repo#PR-2#merged": {}}},
			fresh: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
				match("repo#PR-1#merged", withKind(kindAuthored)),
			}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				byKey := keyKinds(got)
				require.Len(t, byKey, 1)
				assert.Contains(t, byKey, "repo#PR-1#merged", "an explicit kind is still shrinkage-protected")
				assert.NotContains(t, byKey, "repo#PR-2#merged")
			},
		},
		{
			// Declined: the drop does not ask whether this run
			// fetched the pull request, so a kind-less match the run never examined
			// is dropped too.
			name: "rule 1 carve-out, declined: an UNSPECIFIED stored match this run did not fetch is dropped too",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
				match("repo#PR-2#merged", withKind(kindUnspecified)),
			}},
			fresh: &attributionv1.PRAttribution{Unmatched: &attributionv1.PRUnmatched{
				Reason: attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_VCS_NOT_LINKED,
			}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				assert.NotContains(t, keyKinds(got), "repo#PR-2#merged")
				assert.NotNil(t, got.GetUnmatched())
			},
		},
		{
			// The carve-out's boundary: it must fire only on the un-completable case.
			// Fresh still sees this pr_key, so reconcileMatch can write the explicit
			// kind and the row converges by being completed rather than emptied.
			name:   "rule 1 carve-out: an UNSPECIFIED stored match fresh still sees is kept and completed",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withKind(kindUnspecified))}},
			fresh:  &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withKind(kindAuthored))}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				require.Len(t, got.GetMatches(), 1)
				assert.Equal(t, kindAuthored, got.GetMatches()[0].GetParticipationKind())
			},
		},
		{
			// Convergence, end to end: the row's only match was kind-less and is now
			// un-completable, so matches empties and the unmatched/cooldown path takes
			// over. Without the carve-out this row would never leave the batch.
			name:   "rule 1 carve-out: a sole un-completable UNSPECIFIED match converges to unmatched",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("gone", withKind(kindUnspecified))}},
			in:     retractionInputs{possiblePRs: map[string]struct{}{"gone": {}}},
			fresh: &attributionv1.PRAttribution{Unmatched: &attributionv1.PRUnmatched{
				Reason: attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR,
			}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				assert.Empty(t, got.GetMatches())
				require.NotNil(t, got.GetUnmatched(), "the retry path must take over")
				assert.Equal(t,
					attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR,
					got.GetUnmatched().GetReason())
			},
		},
		{
			// Rule 2: task_type routes a self-reviewed PR and a producer may
			// reclassify it on re-export, so the re-export that triggers
			// re-attribution can itself flip the kind and move the PR out of the
			// AUTHORED filter.
			name:   "rule 2: a stored AUTHORED is not downgraded by a fresh REVIEWED",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withKind(kindAuthored))}},
			fresh:  &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withKind(kindReviewed))}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				require.Len(t, got.GetMatches(), 1)
				assert.Equal(t, kindAuthored, got.GetMatches()[0].GetParticipationKind())
			},
		},
		{
			// The one-directional exception. A session that authored a PR is only
			// reviewer-visible while the PR is open, so its first attribution can
			// legitimately store REVIEWED; once the PR merges the recompute has the
			// merged author-side evidence AUTHORED requires, and that must land.
			name:   "rule 2: a stored REVIEWED is upgraded by a fresh AUTHORED",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withKind(kindReviewed))}},
			fresh:  &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withKind(kindAuthored))}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				require.Len(t, got.GetMatches(), 1)
				assert.Equal(t, kindAuthored, got.GetMatches()[0].GetParticipationKind())
			},
		},
		{
			// The exception's boundary: a genuine code review recomputes to REVIEWED
			// again post-merge, and must not be disturbed.
			name:   "rule 2: a stored REVIEWED survives a fresh REVIEWED",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withKind(kindReviewed))}},
			fresh:  &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withKind(kindReviewed))}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				require.Len(t, got.GetMatches(), 1)
				assert.Equal(t, kindReviewed, got.GetMatches()[0].GetParticipationKind())
			},
		},
		{
			// A kind-less recompute carries no authorship evidence, so it must not
			// clear a stored REVIEWED — only an explicit AUTHORED may replace it.
			name:   "rule 2: a stored REVIEWED is not cleared by a fresh UNSPECIFIED",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withKind(kindReviewed))}},
			fresh:  &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withKind(kindUnspecified))}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				require.Len(t, got.GetMatches(), 1)
				assert.Equal(t, kindReviewed, got.GetMatches()[0].GetParticipationKind())
			},
		},
		{
			// Rule 2's other half. A recompute exists to write an explicit kind
			// over UNSPECIFIED; a blanket keep-stored rule would pin it forever.
			name:   "rule 2: UNSPECIFIED loses to an explicit fresh kind",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withKind(kindUnspecified))}},
			fresh:  &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withKind(kindAuthored))}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				require.Len(t, got.GetMatches(), 1)
				assert.Equal(t, kindAuthored, got.GetMatches()[0].GetParticipationKind())
			},
		},
		{
			name:   "rule 3: HIGH is not downgraded by a MEDIUM recompute",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withTier(tierHigh))}},
			fresh:  &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withTier(tierMedium))}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				require.Len(t, got.GetMatches(), 1)
				assert.Equal(t, tierHigh, got.GetMatches()[0].GetConfidenceTier())
			},
		},
		{
			// The higher-tier observation is taken wholesale, so (method,
			// confidence, tier) stay mutually consistent — lifting a tier onto the
			// other entry's method would produce a record no scorer run could emit.
			name: "rule 3: a HIGH upgrade carries its own method",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
				match("k", withTier(tierMedium), withMethod(attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP)),
			}},
			fresh: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
				match("k", withTier(tierHigh), withMethod(attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF)),
			}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				require.Len(t, got.GetMatches(), 1)
				m := got.GetMatches()[0]
				assert.Equal(t, tierHigh, m.GetConfidenceTier())
				assert.Equal(t, attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF, m.GetMethod())
			},
		},
		{
			name:   "rule 3: UNSPECIFIED tier never beats a scored one",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withTier(tierMedium))}},
			fresh:  &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withTier(tierUnspecified))}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				require.Len(t, got.GetMatches(), 1)
				assert.Equal(t, tierMedium, got.GetMatches()[0].GetConfidenceTier())
			},
		},
		{
			name:   "rule 3: line counts take the max across observations",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withLines(100, 5))}},
			fresh:  &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k", withLines(40, 90))}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				require.Len(t, got.GetMatches(), 1)
				assert.EqualValues(t, 100, got.GetMatches()[0].GetLinesAdded())
				assert.EqualValues(t, 90, got.GetMatches()[0].GetLinesRemoved())
			},
		},
		{
			// Rule 4. A recompute that finds nothing sets unmatched, but the stored
			// matches survive the union — so unmatched must be dropped or the row
			// violates the proto's "populated only when matches is empty".
			name:   "rule 4: unmatched is cleared when matches survive",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k")}},
			fresh: &attributionv1.PRAttribution{Unmatched: &attributionv1.PRUnmatched{
				Reason: attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR,
			}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				assert.Len(t, got.GetMatches(), 1)
				assert.Nil(t, got.GetUnmatched(), "unmatched must not coexist with matches")
			},
		},
		{
			name:   "unmatched is preserved when there is genuinely no match",
			stored: nil,
			fresh: &attributionv1.PRAttribution{Unmatched: &attributionv1.PRUnmatched{
				Reason: attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_VCS_NOT_LINKED,
			}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				assert.Empty(t, got.GetMatches())
				require.NotNil(t, got.GetUnmatched())
				assert.Equal(t, attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_VCS_NOT_LINKED, got.GetUnmatched().GetReason())
			},
		},
		{
			name:   "first attribution passes the fresh result through",
			stored: nil,
			fresh:  &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{match("k")}},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				require.Len(t, got.GetMatches(), 1)
				assert.Equal(t, "k", got.GetMatches()[0].GetPrKey())
			},
		},
		{
			// A nil fresh result must still record the request as serviced, or the
			// row is re-selected every minute forever on a request already handled.
			name: "a nil fresh result keeps the stored matches and records the request",
			stored: &attributionv1.PRAttribution{
				Matches:                  []*attributionv1.PRMatch{match("k")},
				ServicedReattributionSeq: 6,
			},
			fresh: nil,
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				require.Len(t, got.GetMatches(), 1)
				assert.Equal(t, testServicedSeq, got.GetServicedReattributionSeq())
			},
		},
		{
			name: "a duplicate pr_key in a stored array collapses to one entry",
			stored: &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
				match("k", withLines(10, 1)), match("k", withLines(99, 9)),
			}},
			fresh: &attributionv1.PRAttribution{},
			assert: func(t *testing.T, got *attributionv1.PRAttribution) {
				assert.Len(t, got.GetMatches(), 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Ungated: this table covers the adds-only union, whose behaviour must be
			// untouched by retraction. Retraction has its own table below.
			got, _ := mergePRAttribution(tt.stored, tt.fresh, testServicedSeq, tt.in)
			require.NotNil(t, got)
			// Every attribution write records the request it serviced, whatever else
			// it does — otherwise the row is re-selected forever.
			assert.Equal(t, testServicedSeq, got.GetServicedReattributionSeq(),
				"an attribution write must record the request it serviced")
			tt.assert(t, got)
		})
	}
}

// TestMergePRAttribution_DoesNotMutateInputs pins that the merge never writes
// through to the caller's messages. The stored message belongs to the session
// the caller still holds and may write conditionally, so mutating it would
// corrupt what the caller compares against.
func TestMergePRAttribution_DoesNotMutateInputs(t *testing.T) {
	t.Parallel()

	stored := &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
		match("k", withKind(kindAuthored), withTier(tierMedium), withLines(10, 1)),
	}}
	fresh := &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
		match("k", withKind(kindReviewed), withTier(tierHigh), withLines(50, 5)),
	}}

	got, _ := mergePRAttribution(stored, fresh, testServicedSeq, retractionInputs{})

	require.Len(t, got.GetMatches(), 1)
	assert.Equal(t, kindAuthored, got.GetMatches()[0].GetParticipationKind())
	assert.Equal(t, tierHigh, got.GetMatches()[0].GetConfidenceTier())
	assert.EqualValues(t, 50, got.GetMatches()[0].GetLinesAdded())

	// The fresh entry won on tier, so it is the one at risk of being mutated.
	assert.Equal(t, kindReviewed, fresh.GetMatches()[0].GetParticipationKind(), "fresh input was mutated")
	assert.EqualValues(t, 50, fresh.GetMatches()[0].GetLinesAdded())
	assert.Equal(t, kindAuthored, stored.GetMatches()[0].GetParticipationKind(), "stored input was mutated")
	assert.EqualValues(t, 10, stored.GetMatches()[0].GetLinesAdded())
}

// withEvidence / withFiles express the two things a stored match can offer as
// evidence: the corroborator field, and — for a row with no evidence field —
// the file set matchBranch happened to store.
func withEvidence(s attributionv1.SessionToPRMatchEvidence) func(*attributionv1.PRMatch) {
	return func(m *attributionv1.PRMatch) { m.Evidence = s }
}

func withFiles(files ...string) func(*attributionv1.PRMatch) {
	return func(m *attributionv1.PRMatch) { m.MatchedFiles = files }
}

// TestMergePRAttribution_Retraction covers the one carve-out from the adds-only
// union: a stored match the current rules do not produce is withdrawn.
//
// The mirror invariant runs in BOTH directions, so both are pinned here. A
// predicate BROADER than the producer withdraws a live match on the next run; one
// NARROWER preserves an unsupported match forever. Cases are therefore paired —
// each withdrawal has a sibling that must survive, differing in one field.
func TestMergePRAttribution_Retraction(t *testing.T) {
	t.Parallel()
	const (
		branch = attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH
		direct = attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF
		files  = attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_FILE_OVERLAP
		prRefS = attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_PR_REFERENCE
	)
	ambient := map[prRef]attributionv1.SignalSource{
		{repo: "org/repo", number: 1}: attributionv1.SignalSource_SIGNAL_SOURCE_LOCAL_GIT_WINDOWED,
	}
	produced := map[prRef]attributionv1.SignalSource{
		{repo: "org/repo", number: 1}: attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND,
	}
	// examined names the candidate the stored fixtures use, so a rejection in these
	// cases is the GATE refusing a PR this run looked at — not the reducer skipping
	// a PR it never fetched, which is a different case with its own table below.
	examined := map[string]struct{}{"k": {}}
	live := retractionInputs{gated: true, evaluated: true, producerStamped: true, possiblePRs: examined}

	tests := []struct {
		name    string
		stored  *attributionv1.PRMatch
		in      retractionInputs
		wantKey bool // survives the merge
	}{
		{
			// A branch match with no corroborator at all.
			name:   "withdraws a branch match carrying no evidence",
			stored: match("k", withMethod(branch)),
			in:     live,
		},
		{
			name:    "keeps a branch match whose corroborator is recorded",
			stored:  match("k", withMethod(branch), withEvidence(files)),
			in:      live,
			wantKey: true,
		},
		{
			// The reviewer arm must be reachable too: a mirror covering only
			// authored matches would leave every stored reviewer-side branch-only
			// attribution permanently un-withdrawable.
			name:   "withdraws an unsupported reviewer-side branch match",
			stored: match("k", withMethod(branch), withKind(kindReviewed)),
			in:     live,
		},
		{
			name:    "keeps a reviewer-side branch match corroborated by a reference",
			stored:  match("k", withMethod(branch), withKind(kindReviewed), withEvidence(prRefS)),
			in:      live,
			wantKey: true,
		},
		{
			// Fallback, author arm only: the stored file set stands in for the
			// corroborator field a row without one cannot have.
			name:    "keeps an evidence-less authored match whose stored files clear the bar",
			stored:  match("k", withMethod(branch), withFiles("a.go", "b.go")),
			in:      live,
			wantKey: true,
		},
		{
			// A lone ubiquitous basename is too weak to corroborate, so bare
			// non-emptiness must not pass.
			name:   "withdraws an evidence-less authored match whose only file is a common basename",
			stored: match("k", withMethod(branch), withFiles("README.md")),
			in:     live,
		},
		{
			// The fallback is author-only: matchBranch stored files for every branch
			// candidate regardless of kind, so a reviewer row's incidental overlap
			// names no PR and cannot stand in.
			name:   "withdraws an evidence-less reviewer match despite stored files",
			stored: match("k", withMethod(branch), withKind(kindReviewed), withFiles("a.go", "b.go")),
			in:     live,
		},
		{
			// A URL scraped from a teammate's commit. Provenance is read from the
			// session's CURRENT references.
			name:   "withdraws a direct ref whose naming evidence is ambient",
			stored: match("k", withMethod(direct)),
			in:     retractionInputs{gated: true, evaluated: true, producerStamped: true, refProvenanceNow: ambient, possiblePRs: examined},
		},
		{
			name:    "keeps a direct ref the session itself produced",
			stored:  match("k", withMethod(direct)),
			in:      retractionInputs{gated: true, evaluated: true, producerStamped: true, refProvenanceNow: produced, possiblePRs: examined},
			wantKey: true,
		},
		{
			// Absent from the lookup is not ambient — a miss must not withdraw.
			name:    "keeps a direct ref absent from the provenance lookup",
			stored:  match("k", withMethod(direct)),
			in:      live,
			wantKey: true,
		},
		{
			// FILE_OVERLAP is not a shape the branch gate governs.
			name: "keeps a file-overlap match untouched",
			stored: match("k",
				withMethod(attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_FILE_OVERLAP)),
			in:      live,
			wantKey: true,
		},
		{
			// gated: the reducer has no IDE awareness, so without it a Kiro-scoped
			// rule would withdraw matches for IDEs whose producer still emits them.
			name:    "ungated session keeps an unsupported branch match",
			stored:  match("k", withMethod(branch)),
			in:      retractionInputs{gated: false, evaluated: true, possiblePRs: examined},
			wantKey: true,
		},
		{
			// evaluated: an empty fresh set from ZERO candidates is absence of
			// evidence, not evidence of absence — retracting on it is the exact
			// shrinkage the union exists to prevent.
			name:    "unevaluated run keeps an unsupported branch match",
			stored:  match("k", withMethod(branch)),
			in:      retractionInputs{gated: true, evaluated: false, producerStamped: true, possiblePRs: examined},
			wantKey: true,
		},
		{
			// producerStamped: a build too old to emit any evidence cannot be told
			// apart from one whose signals came back ambient, so its reviewer-side
			// branch match is unsatisfiable by construction — deleting it would
			// punish the producer's age, not apply a rules rejection.
			name:    "unstamped producer keeps an unsupported branch match",
			stored:  match("k", withMethod(branch)),
			in:      retractionInputs{gated: true, evaluated: true, producerStamped: false, possiblePRs: examined},
			wantKey: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stored := &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{tt.stored}}
			// Fresh sees nothing: withdrawal only reaches a match the recompute no
			// longer produces.
			fresh := buildUnmatchedAttribution(
				attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR, nil)

			got, _ := mergePRAttribution(stored, fresh, testServicedSeq, tt.in)
			require.NotNil(t, got)
			if tt.wantKey {
				require.Len(t, got.GetMatches(), 1, "match must survive")
				assert.Equal(t, "k", got.GetMatches()[0].GetPrKey())
				assert.Nil(t, got.GetUnmatched(), "a surviving match forbids unmatched")
				return
			}
			assert.Empty(t, got.GetMatches(), "match must be withdrawn")
			assert.NotNil(t, got.GetUnmatched(), "an emptied set restores the unmatched reason")
		})
	}
}

// TestMergePRAttribution_FreshEvidenceDisplacesUnsupported pins the replace-rather-
// than-reconcile rule.
//
// reconcileMatch keeps the STORED entry on an equal tier, and a stored
// DIRECT_PR_REF carries flat confidence 1.0 with no decay — so without this the row
// keeps the scraped URL it was re-opened to remove, looking repaired and still
// wrong.
func TestMergePRAttribution_FreshEvidenceDisplacesUnsupported(t *testing.T) {
	t.Parallel()
	ambient := map[prRef]attributionv1.SignalSource{
		{repo: "org/repo", number: 1}: attributionv1.SignalSource_SIGNAL_SOURCE_LOCAL_GIT_WINDOWED,
	}
	stored := &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
		match("k", withMethod(attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF)),
	}}
	fresh := &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
		match("k",
			withMethod(attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH),
			withEvidence(attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_FILE_OVERLAP)),
	}}

	got, _ := mergePRAttribution(stored, fresh, testServicedSeq,
		retractionInputs{
			gated: true, evaluated: true, producerStamped: true, refProvenanceNow: ambient,
			possiblePRs: map[string]struct{}{"k": {}},
		})

	require.Len(t, got.GetMatches(), 1)
	assert.Equal(t, attributionv1.SessionToPRMatchEvidence_SESSION_TO_PR_MATCH_EVIDENCE_FILE_OVERLAP,
		got.GetMatches()[0].GetEvidence(), "the corroborated fresh match must replace the ambient stored one")
	assert.Equal(t, attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH,
		got.GetMatches()[0].GetMethod())
}

// TestMergePRAttribution_UnexaminedCandidateIsNeverWithdrawn is the data-loss
// guard, and it is the case a per-person candidate COUNT cannot express.
//
// A stored match is withdrawn when the fresh set does not contain it — but
// absence has two causes and only one is a rules rejection. If the PR was fetched
// and the gate refused it, withdrawing is correct. If the PR was never fetched,
// nothing evaluated it, and deleting on that basis is the candidate-shrinkage the
// union exists to prevent.
//
// The count cannot separate them: it stays above zero whenever the person has ANY
// other candidate. Three ordinary situations produce exactly that — an operator
// narrowing the candidate window, a store purging old candidate records, and one
// identity arm unlinked while the other stays linked. None is an error path, and
// each would otherwise delete a user's data silently.
func TestMergePRAttribution_UnexaminedCandidateIsNeverWithdrawn(t *testing.T) {
	t.Parallel()
	branch := attributionv1.SessionToPRMatchMethod_SESSION_TO_PR_MATCH_METHOD_BRANCH_MATCH
	stored := &attributionv1.PRAttribution{Matches: []*attributionv1.PRMatch{
		match("purged-or-out-of-window", withMethod(branch)),
	}}
	fresh := buildUnmatchedAttribution(
		attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR, nil)

	// The person still has candidates, so the count says "we evaluated" — but not
	// this PR. Every other retraction condition is satisfied, so the key check is
	// the only thing standing between this row and deletion.
	got, _ := mergePRAttribution(stored, fresh, testServicedSeq, retractionInputs{
		gated:           true,
		evaluated:       true,
		producerStamped: true,
		possiblePRs:     map[string]struct{}{"some-other-pr-this-run-did-fetch": {}},
	})
	require.Len(t, got.GetMatches(), 1, "a PR this run never fetched was not refused by the rules")
	assert.Equal(t, "purged-or-out-of-window", got.GetMatches()[0].GetPrKey())

	// Same fixture, same everything, except the run DID fetch it: now the absence
	// from fresh is the gate's refusal, and withdrawal is correct.
	got, _ = mergePRAttribution(stored, fresh, testServicedSeq, retractionInputs{
		gated:           true,
		evaluated:       true,
		producerStamped: true,
		possiblePRs:     map[string]struct{}{"purged-or-out-of-window": {}},
	})
	assert.Empty(t, got.GetMatches(), "a PR the gate examined and refused is withdrawn")
}
