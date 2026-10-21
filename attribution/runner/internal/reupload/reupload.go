// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package reupload holds the rules both reference stores share: what a session
// they already hold becomes when it is put again, and the order they load in.
package reupload

import (
	"slices"
	"time"

	"google.golang.org/protobuf/proto"

	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// maxDurationSeconds caps a duration before it is added to a start, so a
// corrupt value cannot overflow the date arithmetic. Ten years.
const maxDurationSeconds = 3650 * 24 * 60 * 60

// Merge returns the session to store when incoming replaces stored.
//
// The incoming evidence replaces the stored evidence, except for what identifies
// the session and what only other writers change. The person and org are kept
// as first stored, and so are the start date and start instant. The last
// activity is the later of the two, ignoring an unset one. The stored
// attribution is kept, whatever incoming carries: only an attribution write
// changes it. The re-attribution request is the stored value, raised by one
// when an input the matcher reads differs (InputsChanged); incoming's own value
// is ignored.
func Merge(stored, incoming *attributionv1.SessionEvidence) *attributionv1.SessionEvidence {
	next := proto.CloneOf(incoming)
	next.PersonId = stored.GetPersonId()
	next.OrgId = stored.GetOrgId()
	next.StoredAttribution = proto.CloneOf(stored.GetStoredAttribution())
	next.StartDate = proto.CloneOf(stored.GetStartDate())
	next.StartedAt = proto.CloneOf(stored.GetStartedAt())
	if s := stored.GetLastActivityAt(); s != nil && (next.GetLastActivityAt() == nil || s.AsTime().After(next.GetLastActivityAt().AsTime())) {
		next.LastActivityAt = proto.CloneOf(s)
	}
	next.ReattributionSeq = stored.GetReattributionSeq()
	if InputsChanged(stored, incoming) {
		next.ReattributionSeq++
	}
	return next
}

// InputsChanged reports whether incoming differs from stored in an input the
// matcher reads: the pull-request references, the branches, the branch
// provenance, the files edited, the single pull-request number, repository and
// branch, or the number of days the session's duration spans from its stored
// start. Any difference counts, a reordering included.
func InputsChanged(stored, incoming *attributionv1.SessionEvidence) bool {
	if !slices.EqualFunc(stored.GetPrReferences(), incoming.GetPrReferences(), func(a, b *attributionv1.PRReference) bool { return proto.Equal(a, b) }) ||
		!slices.Equal(stored.GetGitBranches(), incoming.GetGitBranches()) ||
		!slices.EqualFunc(stored.GetBranchSignals(), incoming.GetBranchSignals(), func(a, b *attributionv1.BranchSignal) bool { return proto.Equal(a, b) }) ||
		!slices.Equal(stored.GetFilesEdited(), incoming.GetFilesEdited()) ||
		stored.GetPrNumber() != incoming.GetPrNumber() ||
		stored.GetPrRepository() != incoming.GetPrRepository() ||
		stored.GetGitBranch() != incoming.GetGitBranch() {
		return true
	}
	base := anchorBase(stored)
	return spanDays(base, stored.GetSessionDurationSeconds()) != spanDays(base, incoming.GetSessionDurationSeconds())
}

// anchorBase is the stored start instant, or the stored start date when the
// instant is unknown.
func anchorBase(s *attributionv1.SessionEvidence) time.Time {
	if t := s.GetStartedAt(); t != nil {
		return t.AsTime().UTC()
	}
	return s.GetStartDate().AsTime().UTC()
}

// spanDays is how many calendar days (UTC) base plus the duration lies past
// base. A negative duration counts as zero.
func spanDays(base time.Time, seconds int64) int {
	if seconds < 0 {
		seconds = 0
	}
	seconds = min(seconds, maxDurationSeconds)
	end := base.Add(time.Duration(seconds) * time.Second)
	day := func(t time.Time) time.Time { return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC) }
	return int(day(end).Sub(day(base)).Hours() / 24)
}

// OldestFirst orders a before b by start date, then by session id. A session
// with no start date comes after every dated one.
func OldestFirst(a, b *attributionv1.SessionEvidence) bool {
	switch ad, bd := a.GetStartDate(), b.GetStartDate(); {
	case ad == nil && bd != nil:
		return false
	case ad != nil && bd == nil:
		return true
	case ad != nil && !ad.AsTime().Equal(bd.AsTime()):
		return ad.AsTime().Before(bd.AsTime())
	}
	return a.GetSessionId() < b.GetSessionId()
}
