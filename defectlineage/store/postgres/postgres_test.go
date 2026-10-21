// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package postgres_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/store/postgres"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/store/storetest"
)

// dsnEnv names the database these tests run against. They are skipped when it
// is unset, so `go test ./...` from a fresh clone stays green with no service —
// the in-memory store runs the same conformance suite unconditionally.
const dsnEnv = "DEFECTLINEAGE_TEST_DSN"

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		t.Skipf("set %s to run the PostgreSQL store tests", dsnEnv)
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })

	schema, err := os.ReadFile("schema.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(schema))
	require.NoError(t, err, "schema.sql must apply cleanly")
	return db
}

// TestConformance runs the rules every store follows, so the database-backed
// store and the in-memory one stay interchangeable rather than merely similar.
func TestConformance(t *testing.T) {
	db := openDB(t)
	storetest.RunConformance(t, func(t *testing.T) defectlineage.Store {
		s, err := postgres.New(db)
		require.NoError(t, err)
		return s
	})
}

// TestSchemaIsIdempotent pins that loading the schema again on a live database
// is a no-op. An operator who re-runs it on an existing deployment must not lose
// data or hit an error.
func TestSchemaIsIdempotent(t *testing.T) {
	db := openDB(t)
	schema, err := os.ReadFile("schema.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(schema))
	require.NoError(t, err, "a second load must succeed")

	var tables int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM information_schema.tables
		 WHERE table_schema = 'public'
		   AND table_name IN ('bugfix_pr_defect_lineage', 'defect_lineage_pipeline_state')`).Scan(&tables))
	assert.Equal(t, 2, tables)
}

// TestNilHandleIsRejected: a store built on no database can only fail at its
// first call, so it is refused where the mistake is actionable.
func TestNilHandleIsRejected(t *testing.T) {
	_, err := postgres.New(nil)
	require.Error(t, err)
}

// TestAnUnstorableIdentifierIsPermanent pins the classification a queue-driven
// caller depends on. This schema types its identifiers as uuid, so a non-uuid
// value fails on every redelivery; reporting it as transient would make the
// message retry forever.
func TestAnUnstorableIdentifierIsPermanent(t *testing.T) {
	db := openDB(t)
	s, err := postgres.New(db)
	require.NoError(t, err)

	err = s.Enqueue(context.Background(), defectlineage.FixRef{
		OrganizationID: "not-a-uuid",
		ConnectorID:    "github",
		PlatformRepoID: "r1",
		BugFixPRID:     "also-not-a-uuid",
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, defectlineage.ErrUnprocessable,
		"a value the schema cannot represent must be reported as permanent")
}

// TestATransientFailureStaysTransient is the other half: a closed connection
// must NOT be classified permanent, or a queue would drop events during a
// database restart.
func TestATransientFailureStaysTransient(t *testing.T) {
	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		t.Skipf("set %s to run the PostgreSQL store tests", dsnEnv)
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	require.NoError(t, db.Close()) // the failure a restart produces

	s, err := postgres.New(db)
	require.NoError(t, err)
	err = s.Enqueue(context.Background(), defectlineage.FixRef{
		OrganizationID: "00000000-0000-4000-8000-000000000009",
		ConnectorID:    "github", PlatformRepoID: "r1",
		BugFixPRID: "00000000-0000-4000-8000-00000000000a",
	})
	require.Error(t, err)
	assert.NotErrorIs(t, err, defectlineage.ErrUnprocessable,
		"a connection failure must stay retryable")
}
