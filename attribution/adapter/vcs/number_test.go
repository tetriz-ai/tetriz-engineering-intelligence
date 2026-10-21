// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package vcs_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/adapter/vcs"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

type candidates []*attributionv1.PRCandidate

func (c candidates) ListAuthoredPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return c, nil
}

func (candidates) ListReviewedPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, nil
}

type linked struct{}

func (linked) Resolve(context.Context, string, string) (attribution.Identity, error) {
	return attribution.Identity{AuthorIDs: []string{"a"}}, nil
}

// An ExternalID in neither recognised form gives pull-request number 0, so a
// session that names that pull request abstains.
func TestAnUnrecognisedExternalIDCannotBeNamed(t *testing.T) {
	day := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	session := &attributionv1.SessionEvidence{SessionId: "s", PersonId: "p", StartDate: timestamppb.New(day),
		PrReferences: []*attributionv1.PRReference{{PrNumber: 7, PrRepository: "acme/widgets"}}}
	run := func(externalID string) *attributionv1.PRAttribution {
		prs := vcs.Convert([]vcs.Record{{ExternalID: externalID, DataDate: day,
			Metadata: map[string]string{"repo": "acme/widgets"}}},
			attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED)
		results, err := attribution.New(attribution.Params{}, candidates(prs), linked{}).
			Attribute(context.Background(), "org", []*attributionv1.SessionEvidence{session})
		require.NoError(t, err)
		require.Len(t, results, 1)
		return results[0].Attribution
	}
	assert.Len(t, run("acme/widgets#PR-7#merged").GetMatches(), 1, "the recognised form matches")
	other := run("acme/widgets/pull/7")
	assert.Empty(t, other.GetMatches())
	assert.Equal(t, attributionv1.SessionToPRUnmatchedReason_SESSION_TO_PR_UNMATCHED_REASON_NO_MATCHING_PR, other.GetUnmatched().GetReason())
}
