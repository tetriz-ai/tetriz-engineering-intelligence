// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package memory_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/store/memory"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/store/storetest"
	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// TestConformance runs the rules every store follows. It needs no service, so
// it runs on every machine and in CI, which is what keeps the contract honest
// between releases of the database-backed store.
func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(*testing.T) defectlineage.Store { return memory.New() })
}

// TestTwoWorkersClaimDisjointBatches pins the property the lease exists for.
// Two workers draining one store must never be handed the same fix, or both
// would walk it and pay a provider twice for one answer.
func TestTwoWorkersClaimDisjointBatches(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	orgID := uuid.NewString()

	const fixes = 40
	for i := 0; i < fixes; i++ {
		require.NoError(t, s.Enqueue(ctx, defectlineage.FixRef{
			OrganizationID: orgID, ConnectorID: "github",
			PlatformRepoID: "r1", BugFixPRID: uuid.NewString(),
		}))
	}

	var mu sync.Mutex
	seen := map[string]int{}
	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			claims, err := s.ClaimPendingBlame(ctx, orgID, "github", 10, time.Minute)
			assert.NoError(t, err)
			mu.Lock()
			defer mu.Unlock()
			for _, c := range claims {
				seen[c.Ref.BugFixPRID]++
			}
		}()
	}
	wg.Wait()

	assert.Len(t, seen, fixes, "every fix is claimed exactly once across the workers")
	for id, n := range seen {
		assert.Equal(t, 1, n, "fix %s was handed out more than once", id)
	}
}

// TestALapsedLeaseIsReoffered ages the clock rather than sleeping, so the test
// states the rule without waiting for it.
func TestALapsedLeaseIsReoffered(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	now := time.Now().UTC()
	s.SetClock(func() time.Time { return now })

	ref := defectlineage.FixRef{
		OrganizationID: uuid.NewString(), ConnectorID: "github",
		PlatformRepoID: "r1", BugFixPRID: uuid.NewString(),
	}
	require.NoError(t, s.Enqueue(ctx, ref))

	first, err := s.ClaimPendingBlame(ctx, ref.OrganizationID, "github", 10, 15*time.Minute)
	require.NoError(t, err)
	require.Len(t, first, 1)

	// Still inside the lease: the fix is held.
	now = now.Add(5 * time.Minute)
	held, err := s.ClaimPendingBlame(ctx, ref.OrganizationID, "github", 10, 15*time.Minute)
	require.NoError(t, err)
	assert.Empty(t, held, "a live claim is not re-offered")

	// Past it: the worker is presumed gone and the fix comes back.
	now = now.Add(11 * time.Minute)
	again, err := s.ClaimPendingBlame(ctx, ref.OrganizationID, "github", 10, 15*time.Minute)
	require.NoError(t, err)
	require.Len(t, again, 1, "a lapsed claim is re-offered")
	assert.NotEqual(t, first[0].ClaimToken, again[0].ClaimToken)
	assert.EqualValues(t, 2, again[0].AttemptCount, "the retry depth is observable")
}

// TestStageReportsWhereAFixGotTo covers the convenience read a first run uses in
// place of a SQL query.
func TestStageReportsWhereAFixGotTo(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	ref := defectlineage.FixRef{
		OrganizationID: uuid.NewString(), ConnectorID: "github",
		PlatformRepoID: "r1", BugFixPRID: uuid.NewString(),
	}

	_, _, found := s.Stage(ref.OrganizationID, ref.BugFixPRID)
	assert.False(t, found, "an unknown fix is reported as unknown")

	require.NoError(t, s.Enqueue(ctx, ref))
	stage, outcome, found := s.Stage(ref.OrganizationID, ref.BugFixPRID)
	require.True(t, found)
	assert.Equal(t, defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_PENDING, stage)
	assert.Equal(t, defectlineagev1.DefectLineageTerminalOutcome_DEFECT_LINEAGE_TERMINAL_OUTCOME_UNSPECIFIED, outcome)
}
