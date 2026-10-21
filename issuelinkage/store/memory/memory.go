// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package memory is an in-process issuelinkage.Store, for tests and for a run
// over records already in memory. It keeps nothing across restarts and has
// one writer; reads may run alongside it.
package memory

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/issuelinkage/store/internal/plan"
)

// Store holds records and rows in memory.
type Store struct {
	mu     sync.RWMutex
	prs    map[string]issuelinkage.PR
	issues map[string]issuelinkage.Issue
	enc    map[issuelinkage.Entity][]byte
	rows   map[issuelinkage.Entity]issuelinkage.Row
}

var _ issuelinkage.Store = (*Store)(nil)

// New builds an empty Store.
func New() *Store {
	return &Store{
		prs:    map[string]issuelinkage.PR{},
		issues: map[string]issuelinkage.Issue{},
		enc:    map[issuelinkage.Entity][]byte{},
		rows:   map[issuelinkage.Entity]issuelinkage.Row{},
	}
}

// Put implements issuelinkage.Store.
func (s *Store) Put(_ context.Context, prs []issuelinkage.PR, issues []issuelinkage.Issue) error {
	if err := issuelinkage.Validate(prs, issues); err != nil {
		return err
	}
	type put struct {
		e       issuelinkage.Entity
		enc     []byte
		updated time.Time
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prs = append([]issuelinkage.PR(nil), prs...)
	issues = append([]issuelinkage.Issue(nil), issues...)
	puts := make([]put, 0, len(prs)+len(issues))
	var moved []string
	for i, p := range prs {
		old, ok := s.prs[p.Key]
		p = plan.KeepNativePR(old, ok, p)
		if plan.MergeMoved(old, ok, p) {
			moved = append(moved, p.Key)
		}
		prs[i] = p
		b, err := plan.EncodePR(p)
		if err != nil {
			return err
		}
		puts = append(puts, put{issuelinkage.Entity{Side: issuelinkage.SidePR, Key: p.Key}, b, p.UpdatedAt})
	}
	idents := map[string]bool{}
	for i, is := range issues {
		old, ok := s.issues[is.Key]
		is = plan.KeepNativeIssue(old, ok, is)
		issues[i] = is
		b, err := plan.EncodeIssue(is)
		if err != nil {
			return err
		}
		e := issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: is.Key}
		plan.IssueIdents(idents, old, ok, !bytes.Equal(s.enc[e], b), is)
		puts = append(puts, put{issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: is.Key}, b, is.UpdatedAt})
	}
	for _, p := range puts {
		prev, exists := s.rows[p.e]
		changed := !bytes.Equal(s.enc[p.e], p.enc)
		s.rows[p.e] = plan.Touch(prev, exists, p.e, p.updated, changed)
		s.enc[p.e] = p.enc
	}
	for _, p := range prs {
		s.prs[p.Key] = p
	}
	for e, row := range s.rows {
		if e.Side == issuelinkage.SidePR && len(idents) > 0 && plan.Names(row, idents) {
			s.rows[e] = plan.Rearm(row)
			continue
		}
		if e.Side != issuelinkage.SideIssue {
			continue
		}
		for _, k := range moved {
			if plan.Links(row, k) {
				s.rows[e] = plan.Rearm(row)
				break
			}
		}
	}
	for _, is := range issues {
		s.issues[is.Key] = is
	}
	return nil
}

// Force implements issuelinkage.Store.
func (s *Store) Force(_ context.Context, entities ...issuelinkage.Entity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entities {
		if row, ok := s.rows[e]; ok {
			row.Status = issuelinkage.StatusMarkedForForceReprocess
			s.rows[e] = row
		}
	}
	return nil
}

// PR implements issuelinkage.Store.
func (s *Store) PR(_ context.Context, key string) (issuelinkage.PR, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.prs[key]
	return p, ok, nil
}

// Issue implements issuelinkage.Store.
func (s *Store) Issue(_ context.Context, key string) (issuelinkage.Issue, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	is, ok := s.issues[key]
	return is, ok, nil
}

// Trackers implements issuelinkage.Store.
func (s *Store) Trackers(context.Context) ([]issuelinkage.Vendor, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	seen := map[issuelinkage.Vendor]struct{}{}
	for _, is := range s.issues {
		seen[is.Vendor] = struct{}{}
	}
	out := make([]issuelinkage.Vendor, 0, len(seen))
	for v := range seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

// Eligible implements issuelinkage.Store.
func (s *Store) Eligible(_ context.Context, now time.Time, w issuelinkage.Window) ([]issuelinkage.Entity, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rows := make([]issuelinkage.Row, 0, len(s.rows))
	for _, r := range s.rows {
		rows = append(rows, r)
	}
	return plan.Eligible(rows, now, w), nil
}

// Row implements issuelinkage.Store.
func (s *Store) Row(_ context.Context, e issuelinkage.Entity) (issuelinkage.Row, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.rows[e]
	return r, ok, nil
}

// Rows implements issuelinkage.Store.
func (s *Store) Rows(context.Context) ([]issuelinkage.Row, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]issuelinkage.Row, 0, len(s.rows))
	for _, r := range s.rows {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Entity.Side != out[j].Entity.Side {
			return out[i].Entity.Side < out[j].Entity.Side
		}
		return out[i].Entity.Key < out[j].Entity.Key
	})
	return out, nil
}

// Write implements issuelinkage.Store.
func (s *Store) Write(_ context.Context, r issuelinkage.Result, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, ok := s.rows[r.Entity]
	if !ok {
		return fmt.Errorf("%w: no row for %s %q", issuelinkage.ErrInvalidRecord, r.Entity.Side, r.Entity.Key)
	}
	row, err := plan.Write(row, r, at)
	if err != nil {
		return err
	}
	s.rows[r.Entity] = row
	return nil
}

// Rearm implements issuelinkage.Store.
func (s *Store) Rearm(_ context.Context, e issuelinkage.Entity) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if row, ok := s.rows[e]; ok {
		s.rows[e] = plan.Rearm(row)
	}
	return nil
}

// IssuesByIdentifier implements issuelinkage.Lookup.
func (s *Store) IssuesByIdentifier(_ context.Context, ident string) ([]issuelinkage.Ref, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []issuelinkage.Ref
	for _, is := range s.issues {
		for _, id := range is.Identifiers {
			if id == ident {
				out = append(out, issuelinkage.Ref{Key: is.Key, Vendor: is.Vendor})
				break
			}
		}
	}
	sortRefs(out)
	return out, nil
}

// PRsByNumber implements issuelinkage.Lookup.
func (s *Store) PRsByNumber(_ context.Context, repository string, number int) ([]issuelinkage.Ref, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []issuelinkage.Ref
	for _, p := range s.prs {
		if p.Repository == repository && p.Number == number {
			out = append(out, issuelinkage.Ref{Key: p.Key, Vendor: p.Vendor})
		}
	}
	sortRefs(out)
	return out, nil
}

// MergedAt implements issuelinkage.Lookup.
func (s *Store) MergedAt(_ context.Context, prKeys []string) (map[string]time.Time, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]time.Time{}
	for _, k := range prKeys {
		if p, ok := s.prs[k]; ok && !p.MergedAt.IsZero() {
			out[k] = p.MergedAt
		}
	}
	return out, nil
}

// Closers implements issuelinkage.Lookup.
func (s *Store) Closers(_ context.Context, issueKeys []string) (map[string]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]string{}
	for _, k := range issueKeys {
		if row, ok := s.rows[issuelinkage.Entity{Side: issuelinkage.SideIssue, Key: k}]; ok && row.Result.Closer != "" {
			out[k] = row.Result.Closer
		}
	}
	return out, nil
}

func sortRefs(refs []issuelinkage.Ref) {
	sort.Slice(refs, func(i, j int) bool { return refs[i].Key < refs[j].Key })
}
