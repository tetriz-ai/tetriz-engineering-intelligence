// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package memory

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

const org = "org-1"

func session(id string, at time.Time) *attributionv1.SessionEvidence {
	return &attributionv1.SessionEvidence{
		SessionId: id, PersonId: "p1", OrgId: org,
		StartDate: timestamppb.New(at),
	}
}

func TestPutAndGetCopyRatherThanAlias(t *testing.T) {
	s := New()
	now := time.Now().UTC()
	put := session("s1", now)
	s.Put(org, put)

	// Mutating what was handed in must not reach the store, and mutating what
	// comes out must not either. A store that aliases its caller's proto lets an
	// unrelated goroutine change a session mid-batch.
	put.PersonId = "someone-else"
	got := s.Get(org, "s1")
	require.NotNil(t, got)
	assert.Equal(t, "p1", got.GetPersonId(), "Put must copy")

	got.PersonId = "mutated"
	again := s.Get(org, "s1")
	assert.Equal(t, "p1", again.GetPersonId(), "Get must copy")
}

func TestGetUnknownReturnsNil(t *testing.T) {
	s := New()
	assert.Nil(t, s.Get(org, "absent"))
	assert.Nil(t, s.Get("other-org", "s1"))
}

// LoadPending returns at most limit sessions, the oldest, and a later call
// returns the rest: nothing is held back between calls.
func TestLoadPendingReturnsUpToTheLimit(t *testing.T) {
	ctx := context.Background()
	s := New()
	base := time.Now().UTC().Truncate(24 * time.Hour)
	s.Put(org, session("s3", base.AddDate(0, 0, 2)), session("s1", base), session("s2", base.AddDate(0, 0, 1)))

	first, err := s.LoadPending(ctx, org, 2)
	require.NoError(t, err)
	assert.Equal(t, []string{"s1", "s2"}, []string{first[0].GetSessionId(), first[1].GetSessionId()})
	again, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	assert.Len(t, again, 3, "an unwritten session is offered again")
}

func TestLoadPendingReturnsOldestFirst(t *testing.T) {
	ctx := context.Background()
	s := New()
	base := time.Now().UTC().Truncate(24 * time.Hour)
	s.Put(org, session("newest", base.AddDate(0, 0, 2)))
	s.Put(org, session("oldest", base))
	s.Put(org, session("middle", base.AddDate(0, 0, 1)))

	got, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, []string{"oldest", "middle", "newest"},
		[]string{got[0].GetSessionId(), got[1].GetSessionId(), got[2].GetSessionId()},
		"a backlog must drain in order rather than starving its tail")
}

func TestWriteResultsStoresTheAttribution(t *testing.T) {
	ctx := context.Background()
	s := New()
	now := time.Now().UTC()
	s.Put(org, session("s1", now))

	loaded, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	require.Len(t, loaded, 1)

	attr := &attributionv1.PRAttribution{
		Matches: []*attributionv1.PRMatch{{PrKey: "acme/widgets#PR-1#merged", PrNumber: 1, ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED}},
	}
	persisted, err := s.WriteResults(ctx, org, attribution.Reduction{
		Sessions: loaded, Attributions: []*attributionv1.PRAttribution{attr},
	}, time.Now().UTC())
	require.NoError(t, err)
	assert.Equal(t, 1, persisted)

	stored := s.Get(org, "s1")
	require.Len(t, stored.GetStoredAttribution().GetMatches(), 1)

	// Settled: not offered again.
	after, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	assert.Empty(t, after)
}

func TestVanishedSessionCountsAsUnpersisted(t *testing.T) {
	ctx := context.Background()
	s := New()
	now := time.Now().UTC()
	s.Put(org, session("s1", now))

	loaded, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)

	// Purged mid-batch. Not an error, but it must not be counted as written —
	// the caller needs the short count to see a starving session.
	s.mu.Lock()
	delete(s.byOrg[org], "s1")
	s.mu.Unlock()

	persisted, err := s.WriteResults(ctx, org, attribution.Reduction{
		Sessions:     loaded,
		Attributions: []*attributionv1.PRAttribution{{}},
	}, time.Now().UTC())
	require.NoError(t, err)
	assert.Zero(t, persisted, "a vanished row is not persisted")
}

func TestAbstentionRetriesOnlyAfterCooldown(t *testing.T) {
	ctx := context.Background()
	s := New()
	s.Params.RetryCooldownHours = 1
	clock := time.Now().UTC()
	s.now = func() time.Time { return clock }

	s.Put(org, session("s1", clock))
	loaded, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	require.Len(t, loaded, 1)

	abstained := buildAbstention(clock)
	_, err = s.WriteResults(ctx, org, attribution.Reduction{
		Sessions: loaded, Attributions: []*attributionv1.PRAttribution{abstained},
	}, clock)
	require.NoError(t, err)

	immediate, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	assert.Empty(t, immediate, "an abstention waits out its cooldown")

	clock = clock.Add(2 * time.Hour)
	later, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	assert.Len(t, later, 1, "an abstention is not final")
}

func TestReattributionRequestReopensASettledSession(t *testing.T) {
	ctx := context.Background()
	s := New()
	now := time.Now().UTC()
	s.Put(org, session("s1", now))

	loaded, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	_, err = s.WriteResults(ctx, org, attribution.Reduction{
		Sessions: loaded,
		Attributions: []*attributionv1.PRAttribution{{
			Matches: []*attributionv1.PRMatch{{PrKey: "acme/widgets#PR-1#merged", ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED}},
		}},
	}, now)
	require.NoError(t, err)

	settled, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	require.Empty(t, settled)

	// A request: the session is owed another pass.
	s.RequestReattribution(org, "s1")

	after, err := s.LoadPending(ctx, org, 10)
	require.NoError(t, err)
	assert.Len(t, after, 1, "a raised request must reopen a settled session")
}

func buildAbstention(at time.Time) *attributionv1.PRAttribution {
	return &attributionv1.PRAttribution{
		Unmatched: &attributionv1.PRUnmatched{
			Reason:          attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR,
			LastAttemptedAt: at.UTC().Format(time.RFC3339),
		},
	}
}
