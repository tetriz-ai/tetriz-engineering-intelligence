// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage/store/sqlite"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) branchlineage.Store {
		s, err := sqlite.Open(":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}

// TestSurvivesARestart: pull requests, edges and the queue persist across a
// close and reopen, and the reopened store picks up where it left off.
func TestSurvivesARestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "lineage.db")
	at := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	prs := []branchlineage.PR{
		{Key: "L1", Repository: "acme/r", Number: 1, HeadBranch: "feature/a", BaseBranch: "develop", MergedAt: at, FirstCommitAt: at.Add(-time.Hour),
			MergeCommitAt: at.Add(-time.Second), LinesAdded: 3},
		{Key: "C1", Repository: "acme/r", Number: 2, HeadBranch: "develop", BaseBranch: "main", MergedAt: at.Add(time.Hour),
			HeadCommitAt: at.Add(-time.Second)},
	}

	s, err := sqlite.Open(path)
	require.NoError(t, err)
	_, err = s.Put(ctx, prs...)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	s, err = sqlite.Open(path)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	pending, err := s.Pending(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{"C1", "L1"}, pending, "the queue survives the restart")
	n, err := s.Derive(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	edges, err := s.Edges(ctx, "acme/r")
	require.NoError(t, err)
	require.Len(t, edges, 1)
	assert.Equal(t, "L1", edges[0].Carried)
	assert.Nil(t, edges[0].CarrierLines, "no diff data reads back as unknown, not zero")
	require.NotNil(t, edges[0].CarriedLines)
	assert.Equal(t, int64(3), *edges[0].CarriedLines)

	inv, err := s.Put(ctx, prs...)
	require.NoError(t, err)
	assert.Empty(t, inv.Carriers, "times read back equal: a replay after the restart changes nothing")
}
