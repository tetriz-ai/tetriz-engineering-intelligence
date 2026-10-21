// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package sqlite_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/sqlite"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/storetest"
)

func TestConformance(t *testing.T) {
	storetest.RunConformance(t, func(t *testing.T) issuelinkage.Store {
		s, err := sqlite.Open(":memory:")
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })
		return s
	})
}

// TestReopen: records, rows and results survive closing the file.
func TestReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "linkage.db")
	s, err := sqlite.Open(path)
	require.NoError(t, err)
	require.NoError(t, s.Put(ctx,
		[]issuelinkage.PR{{Key: "P1", Vendor: issuelinkage.VendorGitHub, Repository: "acme/web", Number: 1, Body: "Fixes PROJ-1"}},
		[]issuelinkage.Issue{{Key: "I1", Vendor: issuelinkage.VendorJira, Identifiers: []string{"PROJ-1"}}}))
	_, err = issuelinkage.Run(ctx, s, time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC), issuelinkage.Config{})
	require.NoError(t, err)
	require.NoError(t, s.Close())

	s, err = sqlite.Open(path)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	r, ok, err := s.Row(ctx, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: "P1"})
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, issuelinkage.StatusProcessed, r.Status)
	require.Len(t, r.Result.Links, 1)
	assert.Equal(t, "I1", r.Result.Links[0].Issue)
}

// TestPutRollsBackWhenARowWriteFails: a failed row write fails the put, and
// none of the batch is stored.
func TestPutRollsBackWhenARowWriteFails(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "linkage.db")
	s, err := sqlite.Open(path)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	other, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	_, err = other.Exec(`DROP TABLE mapping_rows`)
	require.NoError(t, err)
	require.NoError(t, other.Close())

	err = s.Put(ctx, []issuelinkage.PR{{Key: "P1", Vendor: issuelinkage.VendorGitHub}}, nil)
	require.Error(t, err)
	_, ok, err := s.PR(ctx, "P1")
	require.NoError(t, err)
	assert.False(t, ok, "the pull request was rolled back with its row")
}

// TestPutRollsBackWhenAReArmWriteFails: when re-arming a pull request that
// waits for a put issue fails, the put fails and the issue is not stored.
func TestPutRollsBackWhenAReArmWriteFails(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "linkage.db")
	s, err := sqlite.Open(path)
	require.NoError(t, err)
	defer func() { _ = s.Close() }()
	require.NoError(t, s.Put(ctx, []issuelinkage.PR{{Key: "P1", Vendor: issuelinkage.VendorGitHub, Repository: "acme/web", Number: 1, Body: "Fixes OPS-12"}}, nil))
	_, err = issuelinkage.Run(ctx, s, time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC), issuelinkage.Config{})
	require.NoError(t, err)
	pe := issuelinkage.Entity{Side: issuelinkage.SidePR, Key: "P1"}
	r, _, err := s.Row(ctx, pe)
	require.NoError(t, err)
	require.Equal(t, issuelinkage.StatusPartiallyProcessed, r.Status)

	other, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	// The trigger fails the update that re-arms a pull-request row.
	_, err = other.Exec(fmt.Sprintf(`CREATE TRIGGER fail_rearm BEFORE UPDATE ON mapping_rows
WHEN NEW.side = %d AND NEW.status = %d BEGIN SELECT RAISE(ABORT, 're-arm down'); END`, int(issuelinkage.SidePR), int(issuelinkage.StatusToBeReprocessed)))
	require.NoError(t, err)
	require.NoError(t, other.Close())

	err = s.Put(ctx, nil, []issuelinkage.Issue{{Key: "I12", Vendor: issuelinkage.VendorJira, Identifiers: []string{"OPS-12"}}})
	require.ErrorContains(t, err, "re-arm down")
	_, ok, err := s.Issue(ctx, "I12")
	require.NoError(t, err)
	assert.False(t, ok, "the issue was rolled back with the re-arm")
	r, _, err = s.Row(ctx, pe)
	require.NoError(t, err)
	assert.Equal(t, issuelinkage.StatusPartiallyProcessed, r.Status)
}
