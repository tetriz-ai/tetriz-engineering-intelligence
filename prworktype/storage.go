// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package prworktype

import (
	"database/sql"

	_ "github.com/lib/pq" // registers the "postgres" driver
)

// Postgres storage for classifications — the same table the Python orchestrator
// uses, so both write-paths are compatible. Optional: only used when a DSN is set.

const storageSchema = `
CREATE TABLE IF NOT EXISTS classifications (
    id            BIGSERIAL PRIMARY KEY,
    title         TEXT NOT NULL,
    branch        TEXT NOT NULL DEFAULT '',
    type_bucket   TEXT,
    type_method   TEXT NOT NULL,
    type_score    DOUBLE PRECISION,
    domain_bucket TEXT,
    domain_method TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);`

// Storage is the optional Postgres store for classifications.
type Storage struct{ db *sql.DB }

// OpenStorage connects and ensures the table exists.
func OpenStorage(dsn string) (*Storage, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	if _, err := db.Exec(storageSchema); err != nil {
		return nil, err
	}
	return &Storage{db: db}, nil
}

// Save persists one classification. A nil/empty bucket is stored as SQL NULL.
func (s *Storage) Save(title, branch string, c Classification) error {
	_, err := s.db.Exec(
		`INSERT INTO classifications
		   (title, branch, type_bucket, type_method, type_score, domain_bucket, domain_method)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		title, branch, nullStr(c.Type.Bucket), string(c.Type.Method), nullScore(c.Type),
		nullStr(c.Domain.Bucket), string(c.Domain.Method),
	)
	return err
}

// Close releases the underlying database handle.
func (s *Storage) Close() error { return s.db.Close() }

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullScore(a AxisResult) any {
	if a.Method != MethodEmbedding {
		return nil
	}
	return a.Score
}
