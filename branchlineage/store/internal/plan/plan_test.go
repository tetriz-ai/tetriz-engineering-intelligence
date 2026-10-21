// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package plan

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
)

// TestDerive_QueuedKeyMustBeMergedAndPresent pins Derive's contract with the
// stores: Put never queues a key that is missing or not merged, so one reaching
// Derive is an error, never a silent skip.
func TestDerive_QueuedKeyMustBeMergedAndPresent(t *testing.T) {
	state := []branchlineage.PR{
		{Key: "MERGED", Repository: "acme/r", Number: 1, HeadBranch: "feature/a", BaseBranch: "develop", MergedAt: time.Date(2026, 4, 2, 9, 0, 0, 0, time.UTC)},
		{Key: "OPEN", Repository: "acme/r", Number: 2, HeadBranch: "feature/b", BaseBranch: "develop"},
	}
	for _, key := range []string{"MISSING", "OPEN"} {
		t.Run(key, func(t *testing.T) {
			_, err := Derive(state, []string{key})
			require.ErrorIs(t, err, branchlineage.ErrInvalidPR)
		})
	}
	ds, err := Derive(state, []string{"MERGED"})
	require.NoError(t, err)
	require.Len(t, ds, 1)
}
