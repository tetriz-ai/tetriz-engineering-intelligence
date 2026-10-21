// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package memory is an in-process SessionSource, for tests and for a first run
// against data an adopter already holds.
//
// It keeps nothing across restarts. It has one writer: run one worker per store.
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/runner/internal/reupload"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// Store holds sessions in memory, keyed by session id within an org.
type Store struct {
	mu sync.Mutex
	// byOrg[orgID][sessionID] is the session as last written.
	byOrg map[string]map[string]*attributionv1.SessionEvidence
	// Params sets the selection LoadPending applies (see attribution.IsPending):
	// RetryCooldownHours, MaxRetryDays, and MaxSessionSpanDays, which bounds the
	// last-activity anchor the retry budget runs from. Set MaxSessionSpanDays to
	// the engine's value. Zero fields take their defaults.
	Params attribution.Params
	now    func() time.Time
}

// New builds an empty Store.
func New() *Store {
	return &Store{
		byOrg: map[string]map[string]*attributionv1.SessionEvidence{},
		now:   func() time.Time { return time.Now().UTC() },
	}
}

// Put adds sessions, or replaces ones the store holds.
//
// A new session is stored as given. A session the store already holds takes
// the incoming evidence but keeps its person, org, start date and instant,
// stored attribution and re-attribution request, and its last activity becomes
// the later of the two. The request is raised by one when an input the matcher
// reads changed: the pull-request references, the branches and their
// provenance, the files edited, the single pull-request number, repository and
// branch, or the days the duration spans.
func (s *Store) Put(orgID string, sessions ...*attributionv1.SessionEvidence) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.byOrg[orgID] == nil {
		s.byOrg[orgID] = map[string]*attributionv1.SessionEvidence{}
	}
	for _, sess := range sessions {
		if stored, ok := s.byOrg[orgID][sess.GetSessionId()]; ok {
			s.byOrg[orgID][sess.GetSessionId()] = reupload.Merge(stored, sess)
			continue
		}
		s.byOrg[orgID][sess.GetSessionId()] = proto.CloneOf(sess)
	}
}

// RequestReattribution raises each session's re-attribution request by one, so
// the next run attributes it again. An unknown session is skipped.
func (s *Store) RequestReattribution(orgID string, sessionIDs ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range sessionIDs {
		if sess, ok := s.byOrg[orgID][id]; ok {
			sess.ReattributionSeq++
		}
	}
}

// Get returns a copy of one session, or nil.
func (s *Store) Get(orgID, sessionID string) *attributionv1.SessionEvidence {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.byOrg[orgID][sessionID]
	if !ok {
		return nil
	}
	return proto.CloneOf(sess)
}

// LoadPending returns up to limit sessions attribution.IsPending selects under
// the store's Params, oldest start date first, undated last, then by session id.
func (s *Store) LoadPending(_ context.Context, orgID string, limit int) ([]*attributionv1.SessionEvidence, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var out []*attributionv1.SessionEvidence
	for _, sess := range s.byOrg[orgID] {
		if attribution.IsPending(sess, s.Params, now) {
			out = append(out, proto.CloneOf(sess))
		}
	}
	sortOldestFirst(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// sortOldestFirst orders sessions by start date, undated last, then by session id.
func sortOldestFirst(sessions []*attributionv1.SessionEvidence) {
	sort.Slice(sessions, func(i, j int) bool { return reupload.OldestFirst(sessions[i], sessions[j]) })
}

// WriteResults stores each merged attribution against its session and returns
// how many it stored. It writes the attribution only: the evidence and the
// re-attribution request stay as the store holds them, so a request raised
// while the batch ran stays ahead of the value the attribution serviced, and
// the session is selected again. A session the store does not hold is not
// written and counts as unpersisted.
func (s *Store) WriteResults(_ context.Context, orgID string, r attribution.Reduction, _ time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	persisted := 0
	for i, sess := range r.Sessions {
		stored, ok := s.byOrg[orgID][sess.GetSessionId()]
		if !ok {
			continue
		}
		stored.StoredAttribution = proto.CloneOf(r.Attributions[i])
		persisted++
	}
	return persisted, nil
}
