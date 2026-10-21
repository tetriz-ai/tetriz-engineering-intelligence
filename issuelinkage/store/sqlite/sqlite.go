// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package sqlite is the persistent reference issuelinkage.Store.
//
// It keeps records and rows across restarts. A Put commits in one
// transaction, so a crash leaves a batch either whole or absent. It has one
// writer: do not open one file from two processes.
//
// The driver is pure Go, so the package builds and tests anywhere without cgo.
// Text is stored as SQLite text, so invalid UTF-8 in a title or body comes
// back with each bad byte replaced by U+FFFD; the memory store returns the
// bytes as given.
package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/internal/plan"
)

const schema = `
CREATE TABLE IF NOT EXISTS prs (
    key        TEXT    NOT NULL PRIMARY KEY,
    vendor     TEXT    NOT NULL,
    repository TEXT    NOT NULL,
    number     INTEGER NOT NULL,
    -- RFC 3339 in UTC; NULL when not merged.
    merged_at  TEXT,
    -- The record's canonical JSON, compared whole on a put.
    record     BLOB    NOT NULL
);
CREATE INDEX IF NOT EXISTS prs_number ON prs (repository, number);

CREATE TABLE IF NOT EXISTS issues (
    key    TEXT NOT NULL PRIMARY KEY,
    vendor TEXT NOT NULL,
    record BLOB NOT NULL
);

CREATE TABLE IF NOT EXISTS issue_identifiers (
    identifier TEXT NOT NULL,
    key        TEXT NOT NULL,
    PRIMARY KEY (identifier, key)
);

-- One row per entity. side is 1 for a pull request, 2 for an issue.
CREATE TABLE IF NOT EXISTS mapping_rows (
    side        INTEGER NOT NULL,
    key         TEXT    NOT NULL,
    status      INTEGER NOT NULL,
    updated_at  TEXT,
    last_run_at TEXT,
    closer      TEXT    NOT NULL DEFAULT '',
    result      BLOB,
    PRIMARY KEY (side, key)
);
CREATE INDEX IF NOT EXISTS mapping_rows_status ON mapping_rows (status);
`

// Store is a SQLite-backed issuelinkage.Store.
type Store struct {
	db *sql.DB
}

var _ issuelinkage.Store = (*Store)(nil)

// Open opens (or creates) a store at path. Use ":memory:" for an ephemeral one.
func Open(path string) (*Store, error) {
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
	return &Store{db: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// Put implements issuelinkage.Store.
func (s *Store) Put(ctx context.Context, prs []issuelinkage.PR, issues []issuelinkage.Issue) (err error) {
	if err := issuelinkage.Validate(prs, issues); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning put: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var moved []string
	for _, p := range prs {
		var old issuelinkage.PR
		ok, err := recordIn(ctx, tx, "prs", p.Key, &old)
		if err != nil {
			return err
		}
		p = plan.KeepNativePR(old, ok, p)
		if plan.MergeMoved(old, ok, p) {
			moved = append(moved, p.Key)
		}
		enc, err := plan.EncodePR(p)
		if err != nil {
			return err
		}
		e := issuelinkage.Entity{Side: issuelinkage.SidePR, Key: p.Key}
		changed, err := recordChanged(ctx, tx, "prs", p.Key, enc)
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `
INSERT INTO prs (key, vendor, repository, number, merged_at, record) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (key) DO UPDATE SET vendor = excluded.vendor, repository = excluded.repository,
    number = excluded.number, merged_at = excluded.merged_at, record = excluded.record`,
			p.Key, string(p.Vendor), p.Repository, p.Number, timeText(p.MergedAt), enc); err != nil {
			return fmt.Errorf("writing pull request %s: %w", p.Key, err)
		}
		if err = touch(ctx, tx, e, p.UpdatedAt, changed); err != nil {
			return err
		}
	}
	idents := map[string]bool{}
	for _, is := range issues {
		var old issuelinkage.Issue
		ok, err := recordIn(ctx, tx, "issues", is.Key, &old)
		if err != nil {
			return err
		}
		is = plan.KeepNativeIssue(old, ok, is)
		enc, err := plan.EncodeIssue(is)
		if err != nil {
			return err
		}
		e := issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: is.Key}
		changed, err := recordChanged(ctx, tx, "issues", is.Key, enc)
		if err != nil {
			return err
		}
		plan.IssueIdents(idents, old, ok, changed, is)
		if _, err = tx.ExecContext(ctx, `
INSERT INTO issues (key, vendor, record) VALUES (?, ?, ?)
ON CONFLICT (key) DO UPDATE SET vendor = excluded.vendor, record = excluded.record`,
			is.Key, string(is.Vendor), enc); err != nil {
			return fmt.Errorf("writing issue %s: %w", is.Key, err)
		}
		if _, err = tx.ExecContext(ctx, `DELETE FROM issue_identifiers WHERE key = ?`, is.Key); err != nil {
			return fmt.Errorf("clearing identifiers of %s: %w", is.Key, err)
		}
		for _, id := range is.Identifiers {
			if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO issue_identifiers (identifier, key) VALUES (?, ?)`, id, is.Key); err != nil {
				return fmt.Errorf("writing identifier of %s: %w", is.Key, err)
			}
		}
		if err = touch(ctx, tx, e, is.UpdatedAt, changed); err != nil {
			return err
		}
	}
	if len(moved) > 0 {
		if err = rearmLinking(ctx, tx, moved); err != nil {
			return err
		}
	}
	if len(idents) > 0 {
		if err = rearmAwaiting(ctx, tx, idents); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing put: %w", err)
	}
	return nil
}

// rearmLinking re-arms every issue row whose result links one of prKeys.
func rearmLinking(ctx context.Context, tx *sql.Tx, prKeys []string) error {
	keys, err := rowKeys(ctx, tx, issuelinkage.SideIssue)
	if err != nil {
		return err
	}
	for _, k := range keys {
		row, _, err := readRow(ctx, tx, issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: k})
		if err != nil {
			return err
		}
		for _, pk := range prKeys {
			if plan.Links(row, pk) {
				if err := writeRow(ctx, tx, plan.Rearm(row)); err != nil {
					return err
				}
				break
			}
		}
	}
	return nil
}

// rearmAwaiting re-arms every pull-request row whose last result reads one of
// idents (see plan.Names).
func rearmAwaiting(ctx context.Context, tx *sql.Tx, idents map[string]bool) error {
	keys, err := rowKeys(ctx, tx, issuelinkage.SidePR)
	if err != nil {
		return err
	}
	for _, k := range keys {
		row, _, err := readRow(ctx, tx, issuelinkage.Entity{Side: issuelinkage.SidePR, Key: k})
		if err != nil {
			return err
		}
		if plan.Names(row, idents) {
			if err := writeRow(ctx, tx, plan.Rearm(row)); err != nil {
				return err
			}
		}
	}
	return nil
}

// rowKeys lists the keys of side's rows that hold a result.
func rowKeys(ctx context.Context, tx *sql.Tx, side issuelinkage.Side) ([]string, error) {
	rs, err := tx.QueryContext(ctx, `SELECT key FROM mapping_rows WHERE side = ? AND result IS NOT NULL ORDER BY key`, int(side))
	if err != nil {
		return nil, fmt.Errorf("listing rows: %w", err)
	}
	var keys []string
	for rs.Next() {
		var k string
		if err := rs.Scan(&k); err != nil {
			_ = rs.Close()
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		keys = append(keys, k)
	}
	if err := rs.Close(); err != nil {
		return nil, fmt.Errorf("listing rows: %w", err)
	}
	return keys, nil
}

func recordChanged(ctx context.Context, q querier, table, key string, enc []byte) (bool, error) {
	var old []byte
	err := q.QueryRowContext(ctx, `SELECT record FROM `+table+` WHERE key = ?`, key).Scan(&old)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading stored %s %s: %w", table, key, err)
	}
	return !bytes.Equal(old, enc), nil
}

func touch(ctx context.Context, tx *sql.Tx, e issuelinkage.Entity, updatedAt time.Time, changed bool) error {
	prev, exists, err := readRow(ctx, tx, e)
	if err != nil {
		return err
	}
	return writeRow(ctx, tx, plan.Touch(prev, exists, e, updatedAt, changed))
}

func readRow(ctx context.Context, q querier, e issuelinkage.Entity) (issuelinkage.Row, bool, error) {
	var (
		status           int
		updated, lastRun sql.NullString
		result           []byte
	)
	err := q.QueryRowContext(ctx, `SELECT status, updated_at, last_run_at, result FROM mapping_rows WHERE side = ? AND key = ?`,
		int(e.Side), e.Key).Scan(&status, &updated, &lastRun, &result)
	if errors.Is(err, sql.ErrNoRows) {
		return issuelinkage.Row{}, false, nil
	}
	if err != nil {
		return issuelinkage.Row{}, false, fmt.Errorf("reading row %s %s: %w", e.Side, e.Key, err)
	}
	return decodeRow(e, status, updated, lastRun, result)
}

func decodeRow(e issuelinkage.Entity, status int, updated, lastRun sql.NullString, result []byte) (issuelinkage.Row, bool, error) {
	row := issuelinkage.Row{Entity: e, Status: issuelinkage.Status(status)}
	var err error
	if row.UpdatedAt, err = parseTime(updated); err != nil {
		return row, false, err
	}
	if row.LastRunAt, err = parseTime(lastRun); err != nil {
		return row, false, err
	}
	if len(result) > 0 {
		if err := json.Unmarshal(result, &row.Result); err != nil {
			return row, false, fmt.Errorf("decoding result of %s %s: %w", e.Side, e.Key, err)
		}
	}
	return row, true, nil
}

func writeRow(ctx context.Context, tx *sql.Tx, row issuelinkage.Row) error {
	var result []byte
	if row.Result.Entity.Key != "" {
		b, err := json.Marshal(row.Result)
		if err != nil {
			return fmt.Errorf("encoding result of %s: %w", row.Entity.Key, err)
		}
		result = b
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO mapping_rows (side, key, status, updated_at, last_run_at, closer, result) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (side, key) DO UPDATE SET status = excluded.status, updated_at = excluded.updated_at,
    last_run_at = excluded.last_run_at, closer = excluded.closer, result = excluded.result`,
		int(row.Entity.Side), row.Entity.Key, int(row.Status), timeText(row.UpdatedAt), timeText(row.LastRunAt),
		row.Result.Closer, result); err != nil {
		return fmt.Errorf("writing row %s %s: %w", row.Entity.Side, row.Entity.Key, err)
	}
	return nil
}

// update reads e's row, applies f, and writes it back in one transaction.
// A missing row is skipped unless mustExist.
func (s *Store) update(ctx context.Context, e issuelinkage.Entity, mustExist bool, f func(issuelinkage.Row) (issuelinkage.Row, error)) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning update: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	row, ok, err := readRow(ctx, tx, e)
	if err != nil {
		return err
	}
	if !ok {
		if mustExist {
			return fmt.Errorf("%w: no row for %s %q", issuelinkage.ErrInvalidRecord, e.Side, e.Key)
		}
		return tx.Commit()
	}
	if row, err = f(row); err != nil {
		return err
	}
	if err = writeRow(ctx, tx, row); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("committing update: %w", err)
	}
	return nil
}

// Force implements issuelinkage.Store.
func (s *Store) Force(ctx context.Context, entities ...issuelinkage.Entity) error {
	for _, e := range entities {
		if err := s.update(ctx, e, false, func(r issuelinkage.Row) (issuelinkage.Row, error) {
			r.Status = issuelinkage.StatusMarkedForForceReprocess
			return r, nil
		}); err != nil {
			return err
		}
	}
	return nil
}

// Write implements issuelinkage.Store.
func (s *Store) Write(ctx context.Context, r issuelinkage.Result, at time.Time) error {
	return s.update(ctx, r.Entity, true, func(row issuelinkage.Row) (issuelinkage.Row, error) {
		return plan.Write(row, r, at)
	})
}

// Rearm implements issuelinkage.Store.
func (s *Store) Rearm(ctx context.Context, e issuelinkage.Entity) error {
	return s.update(ctx, e, false, func(r issuelinkage.Row) (issuelinkage.Row, error) {
		return plan.Rearm(r), nil
	})
}

// PR implements issuelinkage.Store.
func (s *Store) PR(ctx context.Context, key string) (issuelinkage.PR, bool, error) {
	var p issuelinkage.PR
	ok, err := s.record(ctx, "prs", key, &p)
	return p, ok, err
}

// Issue implements issuelinkage.Store.
func (s *Store) Issue(ctx context.Context, key string) (issuelinkage.Issue, bool, error) {
	var is issuelinkage.Issue
	ok, err := s.record(ctx, "issues", key, &is)
	return is, ok, err
}

func (s *Store) record(ctx context.Context, table, key string, into any) (bool, error) {
	return recordIn(ctx, s.db, table, key, into)
}

func recordIn(ctx context.Context, q querier, table, key string, into any) (bool, error) {
	var b []byte
	err := q.QueryRowContext(ctx, `SELECT record FROM `+table+` WHERE key = ?`, key).Scan(&b)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading %s %s: %w", table, key, err)
	}
	if err := json.Unmarshal(b, into); err != nil {
		return false, fmt.Errorf("decoding %s %s: %w", table, key, err)
	}
	return true, nil
}

// Trackers implements issuelinkage.Store.
func (s *Store) Trackers(ctx context.Context) ([]issuelinkage.Vendor, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT vendor FROM issues ORDER BY vendor`)
	if err != nil {
		return nil, fmt.Errorf("listing trackers: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var out []issuelinkage.Vendor
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scanning tracker: %w", err)
		}
		out = append(out, issuelinkage.Vendor(v))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing trackers: %w", err)
	}
	return out, nil
}

// Eligible implements issuelinkage.Store.
func (s *Store) Eligible(ctx context.Context, now time.Time, w issuelinkage.Window) ([]issuelinkage.Entity, error) {
	statuses := []issuelinkage.Status{
		issuelinkage.StatusYetToProcess, issuelinkage.StatusToBeReprocessed,
		issuelinkage.StatusMarkedForForceReprocess, issuelinkage.StatusPartiallyProcessed,
	}
	args := make([]any, len(statuses))
	for i, st := range statuses {
		args[i] = int(st)
	}
	q := `SELECT side, key, status, updated_at, last_run_at FROM mapping_rows WHERE status IN (?` +
		strings.Repeat(", ?", len(statuses)-1) + `)`
	rs, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("listing candidate rows: %w", err)
	}
	defer func() { _ = rs.Close() }()
	var rows []issuelinkage.Row
	for rs.Next() {
		var (
			side, status     int
			key              string
			updated, lastRun sql.NullString
		)
		if err := rs.Scan(&side, &key, &status, &updated, &lastRun); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		row, _, err := decodeRow(issuelinkage.Entity{Side: issuelinkage.Side(side), Key: key}, status, updated, lastRun, nil)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("listing candidate rows: %w", err)
	}
	return plan.Eligible(rows, now, w), nil
}

// Row implements issuelinkage.Store.
func (s *Store) Row(ctx context.Context, e issuelinkage.Entity) (issuelinkage.Row, bool, error) {
	return readRow(ctx, s.db, e)
}

// Rows implements issuelinkage.Store.
func (s *Store) Rows(ctx context.Context) ([]issuelinkage.Row, error) {
	rs, err := s.db.QueryContext(ctx, `SELECT side, key, status, updated_at, last_run_at, result FROM mapping_rows ORDER BY side, key`)
	if err != nil {
		return nil, fmt.Errorf("listing rows: %w", err)
	}
	defer func() { _ = rs.Close() }()
	var out []issuelinkage.Row
	for rs.Next() {
		var (
			side, status     int
			key              string
			updated, lastRun sql.NullString
			result           []byte
		)
		if err := rs.Scan(&side, &key, &status, &updated, &lastRun, &result); err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		row, _, err := decodeRow(issuelinkage.Entity{Side: issuelinkage.Side(side), Key: key}, status, updated, lastRun, result)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("listing rows: %w", err)
	}
	return out, nil
}

// IssuesByIdentifier implements issuelinkage.Lookup.
func (s *Store) IssuesByIdentifier(ctx context.Context, ident string) ([]issuelinkage.Ref, error) {
	return s.refs(ctx, `SELECT i.key, i.vendor FROM issue_identifiers d JOIN issues i ON i.key = d.key
WHERE d.identifier = ? ORDER BY i.key`, ident)
}

// PRsByNumber implements issuelinkage.Lookup.
func (s *Store) PRsByNumber(ctx context.Context, repository string, number int) ([]issuelinkage.Ref, error) {
	return s.refs(ctx, `SELECT key, vendor FROM prs WHERE repository = ? AND number = ? ORDER BY key`, repository, number)
}

func (s *Store) refs(ctx context.Context, q string, args ...any) ([]issuelinkage.Ref, error) {
	rs, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("looking up: %w", err)
	}
	defer func() { _ = rs.Close() }()
	var out []issuelinkage.Ref
	for rs.Next() {
		var key, vendor string
		if err := rs.Scan(&key, &vendor); err != nil {
			return nil, fmt.Errorf("scanning lookup: %w", err)
		}
		out = append(out, issuelinkage.Ref{Key: key, Vendor: issuelinkage.Vendor(vendor)})
	}
	if err := rs.Err(); err != nil {
		return nil, fmt.Errorf("looking up: %w", err)
	}
	return out, nil
}

// MergedAt implements issuelinkage.Lookup.
func (s *Store) MergedAt(ctx context.Context, prKeys []string) (map[string]time.Time, error) {
	out := map[string]time.Time{}
	for _, k := range prKeys {
		var at sql.NullString
		err := s.db.QueryRowContext(ctx, `SELECT merged_at FROM prs WHERE key = ?`, k).Scan(&at)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading merge time of %s: %w", k, err)
		}
		t, err := parseTime(at)
		if err != nil {
			return nil, err
		}
		if !t.IsZero() {
			out[k] = t
		}
	}
	return out, nil
}

// Closers implements issuelinkage.Lookup.
func (s *Store) Closers(ctx context.Context, issueKeys []string) (map[string]string, error) {
	out := map[string]string{}
	for _, k := range issueKeys {
		var closer string
		err := s.db.QueryRowContext(ctx, `SELECT closer FROM mapping_rows WHERE side = ? AND key = ?`,
			int(issuelinkage.SideIssue), k).Scan(&closer)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("reading closer of %s: %w", k, err)
		}
		if closer != "" {
			out[k] = closer
		}
	}
	return out, nil
}

func timeText(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s sql.NullString) (time.Time, error) {
	if !s.Valid || s.String == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing stored time %q: %w", s.String, err)
	}
	return t, nil
}
