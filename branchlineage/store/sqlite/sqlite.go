// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package sqlite is the persistent reference branchlineage.Store.
//
// It keeps pull requests, edges and the derivation queue across restarts. A Put
// commits in one transaction, and each carrier's derivation replaces its edge set
// in one transaction, so a crash leaves a carrier either fully re-derived or still
// queued. It has one writer: do not open one file from two processes.
//
// The driver is pure Go, so the package builds and tests anywhere without cgo.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage/store/internal/plan"
)

const schema = `
CREATE TABLE IF NOT EXISTS prs (
    key              TEXT    NOT NULL PRIMARY KEY,
    repository       TEXT    NOT NULL,
    number           INTEGER NOT NULL,
    head_branch      TEXT    NOT NULL,
    base_branch      TEXT    NOT NULL,
    -- Times are RFC 3339 text in UTC; NULL is the zero time (not merged, unknown).
    opened_at        TEXT,
    merged_at        TEXT,
    first_commit_at  TEXT,
    head_commit_at   TEXT,
    merge_commit_at  TEXT,
    lines_added      INTEGER NOT NULL,
    lines_deleted    INTEGER NOT NULL,
    cross_repository INTEGER NOT NULL,
    -- derived marks a pull request whose edge set is current; pending queues it.
    derived          INTEGER NOT NULL DEFAULT 0,
    pending          INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS prs_repository ON prs (repository);
CREATE INDEX IF NOT EXISTS prs_pending ON prs (pending) WHERE pending = 1;

-- One row per live carrier-to-carried edge. A carrier's set is replaced whole.
CREATE TABLE IF NOT EXISTS edges (
    carrier        TEXT    NOT NULL,
    carried        TEXT    NOT NULL,
    repository     TEXT    NOT NULL,
    source_branch  TEXT    NOT NULL,
    target_branch  TEXT    NOT NULL,
    -- NULL when the pull request had no diff data.
    carrier_lines  INTEGER,
    carried_lines  INTEGER,
    PRIMARY KEY (carrier, carried)
);
CREATE INDEX IF NOT EXISTS edges_repository ON edges (repository);
`

// Store is a SQLite-backed branchlineage.Store.
type Store struct {
	db   *sql.DB
	opts []branchlineage.Option
}

var _ branchlineage.Store = (*Store)(nil)

// Open opens (or creates) a store at path. Use ":memory:" for an ephemeral one.
// opts (an Observer) apply to every derivation.
func Open(path string, opts ...branchlineage.Option) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("opening sqlite at %s: %w", path, err)
	}
	// One connection: the store has one writer, and ":memory:" is per connection.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}
	return &Store{db: db, opts: opts}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// Put implements branchlineage.Store.
func (s *Store) Put(ctx context.Context, prs ...branchlineage.PR) (inv branchlineage.Invalidation, err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return inv, fmt.Errorf("beginning put: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	keys := make([]string, 0, len(prs))
	for _, p := range prs {
		keys = append(keys, p.Key)
	}
	versions, err := queryPRs(ctx, tx, "key", keys)
	if err != nil {
		return inv, err
	}
	inRepos, err := queryPRs(ctx, tx, "repository", plan.Repositories(prs, plain(versions)))
	if err != nil {
		return inv, err
	}
	derived := map[string]bool{}
	for _, l := range [][]storedPR{versions, inRepos} {
		for _, p := range l {
			derived[p.Key] = p.derived
		}
	}
	pl, err := plan.Put(append(plain(inRepos), plain(versions)...), func(k string) bool { return derived[k] }, prs, s.opts...)
	if err != nil {
		return inv, err
	}
	for _, p := range prs {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO prs (key, repository, number, head_branch, base_branch, opened_at, merged_at, first_commit_at,
                 head_commit_at, merge_commit_at, lines_added, lines_deleted, cross_repository)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (key) DO UPDATE SET
    repository = excluded.repository, number = excluded.number,
    head_branch = excluded.head_branch, base_branch = excluded.base_branch,
    opened_at = excluded.opened_at, merged_at = excluded.merged_at,
    first_commit_at = excluded.first_commit_at, head_commit_at = excluded.head_commit_at,
    merge_commit_at = excluded.merge_commit_at, lines_added = excluded.lines_added,
    lines_deleted = excluded.lines_deleted, cross_repository = excluded.cross_repository`,
			p.Key, p.Repository, p.Number, p.HeadBranch, p.BaseBranch, timeText(p.OpenedAt), timeText(p.MergedAt),
			timeText(p.FirstCommitAt), timeText(p.HeadCommitAt), timeText(p.MergeCommitAt), p.LinesAdded, p.LinesDeleted, boolInt(p.CrossRepository)); err != nil {
			return inv, fmt.Errorf("writing pull request %s: %w", p.Key, err)
		}
	}
	for _, k := range pl.DropEdges {
		if _, err = tx.ExecContext(ctx, `DELETE FROM edges WHERE carrier = ?`, k); err != nil {
			return inv, fmt.Errorf("dropping edges of %s: %w", k, err)
		}
	}
	for _, k := range pl.Forget {
		if _, err = tx.ExecContext(ctx, `UPDATE prs SET derived = 0, pending = 0 WHERE key = ?`, k); err != nil {
			return inv, fmt.Errorf("forgetting %s: %w", k, err)
		}
	}
	for _, k := range pl.Queue {
		if _, err = tx.ExecContext(ctx, `UPDATE prs SET pending = 1 WHERE key = ?`, k); err != nil {
			return inv, fmt.Errorf("queueing %s: %w", k, err)
		}
	}
	if err = tx.Commit(); err != nil {
		return inv, fmt.Errorf("committing put: %w", err)
	}
	return pl.Invalidation, nil
}

// Pending implements branchlineage.Store.
func (s *Store) Pending(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key FROM prs WHERE pending = 1 ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("listing pending: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			return nil, fmt.Errorf("reading pending: %w", err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading pending: %w", err)
	}
	return out, nil
}

// Derive implements branchlineage.Store.
func (s *Store) Derive(ctx context.Context) (int, error) {
	keys, err := s.Pending(ctx)
	if err != nil || len(keys) == 0 {
		return 0, err
	}
	pending, err := queryPRs(ctx, s.db, "key", keys)
	if err != nil {
		return 0, err
	}
	repos := map[string]struct{}{}
	var state []branchlineage.PR
	for _, p := range pending {
		if p.Repository == "" {
			state = append(state, p.PR)
			continue
		}
		repos[p.Repository] = struct{}{}
	}
	repoList := make([]string, 0, len(repos))
	for r := range repos {
		repoList = append(repoList, r)
	}
	sort.Strings(repoList)
	inRepos, err := queryPRs(ctx, s.db, "repository", repoList)
	if err != nil {
		return 0, err
	}
	state = append(state, plain(inRepos)...)
	ds, err := plan.Derive(state, keys, s.opts...)
	if err != nil {
		return 0, err
	}
	for _, d := range ds {
		if err := s.replace(ctx, d); err != nil {
			return 0, err
		}
	}
	return len(ds), nil
}

// replace writes one carrier's whole edge set and marks it derived, in one
// transaction.
func (s *Store) replace(ctx context.Context, d branchlineage.Derivation) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning derive of %s: %w", d.Carrier, err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.ExecContext(ctx, `DELETE FROM edges WHERE carrier = ?`, d.Carrier); err != nil {
		return fmt.Errorf("clearing edges of %s: %w", d.Carrier, err)
	}
	for _, e := range d.Edges {
		if _, err = tx.ExecContext(ctx, `
INSERT INTO edges (carrier, carried, repository, source_branch, target_branch, carrier_lines, carried_lines)
VALUES (?, ?, ?, ?, ?, ?, ?)`,
			e.Carrier, e.Carried, e.Repository, e.Source, e.Target, nullInt(e.CarrierLines), nullInt(e.CarriedLines)); err != nil {
			return fmt.Errorf("writing edge %s -> %s: %w", e.Carrier, e.Carried, err)
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE prs SET derived = 1, pending = 0 WHERE key = ?`, d.Carrier); err != nil {
		return fmt.Errorf("marking %s derived: %w", d.Carrier, err)
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing derive of %s: %w", d.Carrier, err)
	}
	return nil
}

// EdgesFrom implements branchlineage.EdgeSource.
func (s *Store) EdgesFrom(ctx context.Context, carriers []string) ([]branchlineage.Edge, error) {
	var out []branchlineage.Edge
	for _, chunk := range chunks(carriers) {
		es, err := s.queryEdges(ctx, `carrier IN (`+placeholders(len(chunk))+`)`, toArgs(chunk)...)
		if err != nil {
			return nil, err
		}
		out = append(out, es...)
	}
	return out, nil
}

// Edges implements branchlineage.Store.
func (s *Store) Edges(ctx context.Context, repository string) ([]branchlineage.Edge, error) {
	return s.queryEdges(ctx, `repository = ?`, repository)
}

func (s *Store) queryEdges(ctx context.Context, where string, args ...any) ([]branchlineage.Edge, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT carrier, carried, repository, source_branch, target_branch, carrier_lines, carried_lines
FROM edges WHERE `+where+` ORDER BY carrier, carried`, args...)
	if err != nil {
		return nil, fmt.Errorf("reading edges: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []branchlineage.Edge
	for rows.Next() {
		var e branchlineage.Edge
		var carrierLines, carriedLines sql.NullInt64
		if err := rows.Scan(&e.Carrier, &e.Carried, &e.Repository, &e.Source, &e.Target, &carrierLines, &carriedLines); err != nil {
			return nil, fmt.Errorf("reading edge: %w", err)
		}
		e.CarrierLines = intPtr(carrierLines)
		e.CarriedLines = intPtr(carriedLines)
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading edges: %w", err)
	}
	return out, nil
}

// storedPR is a pull request with its derived flag.
type storedPR struct {
	branchlineage.PR
	derived bool
}

type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// queryPRs reads the pull requests whose column (key or repository) is in values.
func queryPRs(ctx context.Context, q querier, column string, values []string) ([]storedPR, error) {
	if column != "key" && column != "repository" {
		return nil, errors.New("sqlite: unknown lookup column " + column)
	}
	var out []storedPR
	for _, chunk := range chunks(values) {
		rows, err := q.QueryContext(ctx, `
SELECT key, repository, number, head_branch, base_branch, opened_at, merged_at, first_commit_at,
       head_commit_at, merge_commit_at, lines_added, lines_deleted, cross_repository, derived
FROM prs WHERE `+column+` IN (`+placeholders(len(chunk))+`)`, toArgs(chunk)...)
		if err != nil {
			return nil, fmt.Errorf("reading pull requests: %w", err)
		}
		got, err := scanPRs(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
	}
	return out, nil
}

func scanPRs(rows *sql.Rows) (out []storedPR, err error) {
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var p storedPR
		var opened, merged, first, head, mergeCommit sql.NullString
		var cross, derived int
		if err := rows.Scan(&p.Key, &p.Repository, &p.Number, &p.HeadBranch, &p.BaseBranch, &opened, &merged, &first,
			&head, &mergeCommit, &p.LinesAdded, &p.LinesDeleted, &cross, &derived); err != nil {
			return nil, fmt.Errorf("reading pull request: %w", err)
		}
		if p.OpenedAt, err = parseTime(opened); err != nil {
			return nil, err
		}
		if p.MergedAt, err = parseTime(merged); err != nil {
			return nil, err
		}
		if p.FirstCommitAt, err = parseTime(first); err != nil {
			return nil, err
		}
		if p.HeadCommitAt, err = parseTime(head); err != nil {
			return nil, err
		}
		if p.MergeCommitAt, err = parseTime(mergeCommit); err != nil {
			return nil, err
		}
		p.CrossRepository = cross != 0
		p.derived = derived != 0
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("reading pull requests: %w", err)
	}
	return out, nil
}

func plain(ps []storedPR) []branchlineage.PR {
	out := make([]branchlineage.PR, 0, len(ps))
	for _, p := range ps {
		out = append(out, p.PR)
	}
	return out
}

func timeText(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s sql.NullString) (time.Time, error) {
	if !s.Valid {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("reading stored time %q: %w", s.String, err)
	}
	return t, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullInt(p *int64) any {
	if p == nil {
		return nil
	}
	return *p
}

func intPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

// chunkSize keeps each IN list under SQLite's bound-parameter limit.
const chunkSize = 500

func chunks(values []string) [][]string {
	var out [][]string
	for start := 0; start < len(values); start += chunkSize {
		out = append(out, values[start:min(start+chunkSize, len(values))])
	}
	return out
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func toArgs(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
