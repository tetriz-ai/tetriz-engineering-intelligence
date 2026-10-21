// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// Withdrawal is a stored match the reducer removed. A persister emits one of
// these per deletion AFTER its write commits, because a withdrawal that is
// reported but not persisted misleads worse than one that is silent.
type Withdrawal struct {
	SessionID string
	IDEType   string
	Match     *attributionv1.PRMatch
}

// Reduction is one batch folded into what was already stored.
type Reduction struct {
	// Attributions holds each session's merged attribution, in the order the
	// results were given. A persister writes these.
	Attributions []*attributionv1.PRAttribution
	// Sessions are the sessions those attributions belong to, same order.
	Sessions []*attributionv1.SessionEvidence
	// Withdrawals are matches removed from storage, to be reported once the
	// write commits.
	Withdrawals []Withdrawal
	// Reattributed counts sessions whose outstanding re-attribution request this
	// batch serviced.
	Reattributed int
}

// Reduce folds each freshly computed attribution into the session's stored one,
// adds-only, stamping the re-attribution request it serviced.
//
// A stored match is withdrawn only when this run fetched its pull request and the
// current rules refuse it. The inputs that decide it are read from the session
// itself, so two different persisters cannot drift on what makes a match
// withdrawable.
//
// The returned withdrawals become real only once the caller's write commits.
func Reduce(results []MatchResult) Reduction {
	out := Reduction{
		Attributions: make([]*attributionv1.PRAttribution, len(results)),
		Sessions:     make([]*attributionv1.SessionEvidence, len(results)),
	}
	for i, r := range results {
		// The request value read when the batch was loaded. Recorded as serviced,
		// so a newer request raised mid-batch is not mistaken for this one and
		// stays outstanding.
		servicedSeq := r.Session.GetReattributionSeq()
		if servicedSeq > r.Session.GetStoredAttribution().GetServicedReattributionSeq() {
			out.Reattributed++
		}

		merged, withdrawn := mergePRAttribution(
			r.Session.GetStoredAttribution(), r.Attribution, servicedSeq,
			retractionInputs{
				gated:           sessionIsGated(r.Session),
				evaluated:       r.CandidateCount > 0,
				producerStamped: producerCanStampEvidence(r.Session),
				possiblePRs:     r.PossiblePRs,
				// Built from the session's CURRENT references, which is what lets a
				// re-extraction that reclassifies a scrape make it withdrawable.
				refProvenanceNow: refProvenanceFromSession(r.Session),
			})

		for _, m := range withdrawn {
			out.Withdrawals = append(out.Withdrawals, Withdrawal{
				SessionID: r.Session.GetSessionId(),
				IDEType:   canonicalIDEType(r.Session.GetIdeType()),
				Match:     m,
			})
		}
		out.Attributions[i] = merged
		out.Sessions[i] = r.Session
	}
	return out
}
