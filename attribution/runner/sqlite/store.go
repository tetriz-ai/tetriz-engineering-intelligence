// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package sqlite is the persistent reference SessionSource.
//
// It keeps sessions and their attributions across restarts, and a batch that
// runs again converges rather than duplicating. It has one writer: run one
// worker per store, and do not open one file from two processes.
//
// The driver is pure Go, so the package builds and tests anywhere without cgo.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"time"

	"google.golang.org/protobuf/proto"
	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/runner/internal/reupload"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

const schema = `
CREATE TABLE IF NOT EXISTS sessions (
    org_id             TEXT    NOT NULL,
    session_id         TEXT    NOT NULL,
    -- The session's evidence, without its attribution or request marker. One
    -- blob rather than columns: this store persists the contract, and a column
    -- set would have to chase every field added to it.
    evidence           BLOB    NOT NULL,
    -- The stored attribution, and whether there is one: an attribution with
    -- no fields marshals to no bytes, which must not read back as none. Only
    -- WriteResults writes them.
    attribution        BLOB,
    attributed         INTEGER NOT NULL DEFAULT 0,
    -- The re-attribution request. Put raises it when the matcher's inputs
    -- change, and RequestReattribution raises it. WriteResults never touches it.
    reattribution_seq  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (org_id, session_id)
);
`

// Store is a SQLite-backed SessionSource.
type Store struct {
	db *sql.DB
	// Params sets the selection LoadPending applies (see attribution.IsPending):
	// RetryCooldownHours, MaxRetryDays, and MaxSessionSpanDays, which bounds the
	// last-activity anchor the retry budget runs from. Set MaxSessionSpanDays to
	// the engine's value. Zero fields take their defaults.
	Params attribution.Params

	now func() time.Time
}

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
	return &Store{db: db, now: func() time.Time { return time.Now().UTC() }}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle so a caller can run its own schema changes or queries.
func (s *Store) DB() *sql.DB { return s.db }

// Put adds sessions, or replaces ones the store holds, with the rules of
// memory.Store.Put: a held session keeps its attribution, its start and its
// request, and the request rises by one when an input the matcher reads changed.
func (s *Store) Put(ctx context.Context, orgID string, sessions ...*attributionv1.SessionEvidence) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning put: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, sess := range sessions {
		stored, err := get(ctx, tx, orgID, sess.GetSessionId())
		if err != nil {
			return err
		}
		next := sess
		if stored != nil {
			next = reupload.Merge(stored, sess)
		}
		if err := put(ctx, tx, orgID, next); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing put: %w", err)
	}
	return nil
}

// put writes one whole row.
func put(ctx context.Context, tx *sql.Tx, orgID string, sess *attributionv1.SessionEvidence) error {
	evidence := proto.CloneOf(sess)
	evidence.StoredAttribution = nil
	evidence.ReattributionSeq = 0
	blob, err := proto.Marshal(evidence)
	if err != nil {
		return fmt.Errorf("marshalling session %s: %w", sess.GetSessionId(), err)
	}
	var attr []byte
	attributed := sess.GetStoredAttribution() != nil
	if attributed {
		if attr, err = proto.Marshal(sess.GetStoredAttribution()); err != nil {
			return fmt.Errorf("marshalling attribution of %s: %w", sess.GetSessionId(), err)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO sessions (org_id, session_id, evidence, attribution, attributed, reattribution_seq)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT (org_id, session_id) DO UPDATE SET
		     evidence = excluded.evidence,
		     attribution = excluded.attribution,
		     attributed = excluded.attributed,
		     reattribution_seq = excluded.reattribution_seq`,
		orgID, sess.GetSessionId(), blob, attr, attributed, sess.GetReattributionSeq(),
	); err != nil {
		return fmt.Errorf("putting session %s: %w", sess.GetSessionId(), err)
	}
	return nil
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// get reads one session, or nil when absent.
func get(ctx context.Context, q queryer, orgID, sessionID string) (*attributionv1.SessionEvidence, error) {
	var blob, attr []byte
	var attributed bool
	var seq int32
	err := q.QueryRowContext(ctx,
		`SELECT evidence, attribution, attributed, reattribution_seq FROM sessions WHERE org_id = ? AND session_id = ?`,
		orgID, sessionID).Scan(&blob, &attr, &attributed, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading session %s: %w", sessionID, err)
	}
	return decode(sessionID, blob, attr, attributed, seq)
}

func decode(sessionID string, blob, attr []byte, attributed bool, seq int32) (*attributionv1.SessionEvidence, error) {
	var sess attributionv1.SessionEvidence
	if err := proto.Unmarshal(blob, &sess); err != nil {
		return nil, fmt.Errorf("unmarshalling session %s: %w", sessionID, err)
	}
	if attributed {
		sess.StoredAttribution = &attributionv1.PRAttribution{}
		if err := proto.Unmarshal(attr, sess.StoredAttribution); err != nil {
			return nil, fmt.Errorf("unmarshalling attribution of %s: %w", sessionID, err)
		}
	}
	sess.ReattributionSeq = seq
	return &sess, nil
}

// Get reads one session back, or nil when absent.
func (s *Store) Get(ctx context.Context, orgID, sessionID string) (*attributionv1.SessionEvidence, error) {
	return get(ctx, s.db, orgID, sessionID)
}

// RequestReattribution raises each session's re-attribution request by one, so
// the next run attributes it again. A run stamps the value it read, so a request
// raised while a batch runs stays outstanding rather than being swallowed by it.
func (s *Store) RequestReattribution(ctx context.Context, orgID string, sessionIDs ...string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning reattribution request: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, id := range sessionIDs {
		if _, err := tx.ExecContext(ctx,
			`UPDATE sessions SET reattribution_seq = reattribution_seq + 1
			 WHERE org_id = ? AND session_id = ?`, orgID, id); err != nil {
			return fmt.Errorf("requesting reattribution for %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing reattribution request: %w", err)
	}
	return nil
}

// LoadPending returns up to limit sessions attribution.IsPending selects under
// the store's Params, oldest start date first, undated last, then by session id.
func (s *Store) LoadPending(ctx context.Context, orgID string, limit int) ([]*attributionv1.SessionEvidence, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT session_id, evidence, attribution, attributed, reattribution_seq FROM sessions WHERE org_id = ?`, orgID)
	if err != nil {
		return nil, fmt.Errorf("selecting sessions: %w", err)
	}
	defer func() { _ = rows.Close() }()

	now := s.now()
	var out []*attributionv1.SessionEvidence
	for rows.Next() {
		var id string
		var blob, attr []byte
		var attributed bool
		var seq int32
		if err := rows.Scan(&id, &blob, &attr, &attributed, &seq); err != nil {
			return nil, fmt.Errorf("scanning session: %w", err)
		}
		sess, err := decode(id, blob, attr, attributed, seq)
		if err != nil {
			return nil, err
		}
		if attribution.IsPending(sess, s.Params, now) {
			out = append(out, sess)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sessions: %w", err)
	}
	sort.Slice(out, func(i, j int) bool { return reupload.OldestFirst(out[i], out[j]) })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// WriteResults stores each merged attribution in one transaction, so the whole
// batch lands or none of it does, and returns how many rows it wrote. It writes
// the attribution only: the evidence and the request stay as stored, so a request
// raised while the batch ran stays ahead of the value the attribution serviced,
// and the session is selected again. A row deleted since the load is not written
// and counts as unpersisted.
func (s *Store) WriteResults(ctx context.Context, orgID string, r attribution.Reduction, _ time.Time) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("beginning write: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	persisted := 0
	for i, sess := range r.Sessions {
		attr, err := proto.Marshal(r.Attributions[i])
		if err != nil {
			return 0, fmt.Errorf("marshalling attribution of %s: %w", sess.GetSessionId(), err)
		}
		res, err := tx.ExecContext(ctx,
			`UPDATE sessions SET attribution = ?, attributed = 1 WHERE org_id = ? AND session_id = ?`,
			attr, orgID, sess.GetSessionId())
		if err != nil {
			return 0, fmt.Errorf("writing session %s: %w", sess.GetSessionId(), err)
		}
		if n, _ := res.RowsAffected(); n > 0 {
			persisted++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing write: %w", err)
	}
	return persisted, nil
}
