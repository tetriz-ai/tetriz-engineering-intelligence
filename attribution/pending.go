// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package attribution

import (
	"time"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// IsPending reports whether a session is owed an attribution pass at now. It is
// the selection policy a SessionSource applies when it loads a batch. A session
// is pending when any of these holds:
//
//   - it has no stored attribution;
//   - its re-attribution request is newer than the one its attribution serviced;
//   - a stored match carries no participation kind, so a pass can fill it in;
//   - it abstained with VCS_NOT_LINKED or IDENTITY_ERROR, and its last attempt
//     is more than RetryCooldownHours ago;
//   - it abstained with NO_MATCHING_PR, its last attempt is more than
//     RetryCooldownHours ago, it has a start date, and its last-activity date
//     plus MaxRetryDays is not before the UTC date of now.
//
// Anything else is settled, including an abstention with any other reason. The
// cooldown compares the stored RFC3339 attempt time with now minus the cooldown
// as strings, as the stored value is written. Zero Params fields take their
// DefaultParams values.
func IsPending(s *attributionv1.SessionEvidence, p Params, now time.Time) bool {
	p = p.withDefaults()
	attr := s.GetStoredAttribution()
	if attr == nil {
		return true
	}
	if s.GetReattributionSeq() > attr.GetServicedReattributionSeq() {
		return true
	}
	for _, m := range attr.GetMatches() {
		if m.GetParticipationKind() == attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_UNSPECIFIED {
			return true
		}
	}
	u := attr.GetUnmatched()
	if u == nil {
		return false
	}
	cutoff := now.UTC().Add(-time.Duration(p.RetryCooldownHours) * time.Hour).Format(time.RFC3339)
	pastCooldown := u.GetLastAttemptedAt() < cutoff
	switch u.GetReason() {
	case attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_VCS_NOT_LINKED,
		attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_IDENTITY_ERROR:
		return pastCooldown
	case attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR:
		if !pastCooldown {
			return false
		}
		start := sessionStartDate(s)
		if start.IsZero() {
			// No start date, so no last-activity date to measure the budget from.
			return false
		}
		lastActivity := sessionMatchAnchor(s, p)
		la := lastActivity.UTC()
		lastDay := time.Date(la.Year(), la.Month(), la.Day(), 0, 0, 0, 0, time.UTC)
		n := now.UTC()
		nowDay := time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
		return !lastDay.AddDate(0, 0, p.MaxRetryDays).Before(nowDay)
	case attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_UNSPECIFIED:
		return false
	}
	return false
}
