// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

//go:build defectlineagequeue

package queue_test

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/store/postgres"
)

const (
	// dsnEnv points the transport tests at a database. Unset, they skip, so
	// `go test ./...` stays green with no services running.
	dsnEnv = "DEFECTLINEAGE_TEST_DSN"

	// The identity a consumed event is filed under. Any valid organisation id
	// works; a fixed one keeps a failed run's rows findable.
	orgID       = "00000000-0000-4000-8000-0000000000ff"
	connectorID = "github"
	repoID      = "repo-e2e"
)

// openStore gives a test a Postgres store with the schema loaded. The schema is
// idempotent, so applying it on every run costs nothing and keeps a test from
// depending on the order it is run in.
func openStore(t *testing.T) (*postgres.Store, *sql.DB) {
	t.Helper()
	dsn := os.Getenv(dsnEnv)
	if dsn == "" {
		t.Skipf("set %s to run the transport tests against a database", dsnEnv)
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	require.NoError(t, db.Ping())
	t.Cleanup(func() { _ = db.Close() })

	schema, err := os.ReadFile("../store/postgres/schema.sql")
	require.NoError(t, err)
	_, err = db.Exec(string(schema))
	require.NoError(t, err)

	s, err := postgres.New(db)
	require.NoError(t, err)
	return s, db
}
