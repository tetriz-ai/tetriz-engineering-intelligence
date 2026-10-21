// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// foldReviewBots emits the review-stage automation signals over the merged PRs,
// joined to their captured review facts. Every bot share runs over the captured and
// rule-applicable subset (a PR whose source cannot flag a bot is left out of both
// sides); the coverage of that subset is carried through the denominators, so a
// thin-capture organisation's share is a weak claim, not a measured absence.
func foldReviewBots(rd *aidlc.Readings, prs []MergedPR, facts map[string]PRReviewFacts) {
	review := aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW

	var (
		ruleApplicableCaptured   int // REVIEW_RULE_APPLICABLE_PRS: captured ∧ rule-applicable
		ruleApplicableUncaptured int // rule-applicable PRs with no captured review row — the denominator's unknown remainder
		botPresent               int // BOT_PRESENT_PRS / REVIEW_BOT_SEEN_PRS: + a bot review row
		botClearedNoHuman        int // BOT_CLEARED_NO_HUMAN: + zero human review rows
		botClearedPeople         = newPeopleSet()
	)

	for i := range prs {
		pr := &prs[i]
		if pr.BotDetectionUnsupported {
			continue // rule not applicable: a zero bot count here is not evidence of no bots
		}
		f, ok := facts[pr.PRKey]
		if !ok || !f.Captured {
			ruleApplicableUncaptured++ // rule-applicable but unobserved — bounds the share, never a zero
			continue
		}
		ruleApplicableCaptured++
		if !f.BotPresent {
			continue
		}
		botPresent++
		if f.HumanRows == 0 {
			botClearedNoHuman++
			botClearedPeople.add(pr.AuthorIdentityID)
		}
	}

	// The rule-applicable population is the captured PRs plus the uncaptured ones whose
	// bot presence is unobserved; carrying the uncaptured count as the denominator's
	// remainder widens the L3 share to a [captured_share, 1] interval so a thin-capture
	// org cannot read as a confident 100% bot-reviewed.
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_RULE_APPLICABLE_PRS, review,
		denominatorReading(float64(ruleApplicableCaptured), optRemainder(ruleApplicableUncaptured)))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_BOT_PRESENT_PRS, review,
		numeratorReading(float64(botPresent), nil, nil))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_REVIEW_BOT_SEEN_PRS, review,
		denominatorReading(float64(botPresent), nil))
	rd.Set(aidlcv1.SignalKey_SIGNAL_KEY_BOT_CLEARED_NO_HUMAN, review,
		numeratorReading(float64(botClearedNoHuman), botClearedPeople.count(), nil))
}
