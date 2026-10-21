// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

// Package memory is an in-process branchlineage.Store, for tests and for a run
// over pull requests already in memory. It keeps nothing across restarts and
// has one writer; reads may run alongside it.
package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage/store/internal/plan"
)

// Store holds pull requests, edges and the derivation queue in memory.
type Store struct {
	mu      sync.RWMutex
	prs     map[string]branchlineage.PR
	edges   map[string][]branchlineage.Edge // by carrier
	derived map[string]struct{}
	pending map[string]struct{}
	opts    []branchlineage.Option
}

var _ branchlineage.Store = (*Store)(nil)

// New builds an empty Store. opts (an Observer) apply to every derivation.
func New(opts ...branchlineage.Option) *Store {
	return &Store{
		prs:     map[string]branchlineage.PR{},
		edges:   map[string][]branchlineage.Edge{},
		derived: map[string]struct{}{},
		pending: map[string]struct{}{},
		opts:    opts,
	}
}

// Put implements branchlineage.Store.
func (s *Store) Put(_ context.Context, prs ...branchlineage.PR) (branchlineage.Invalidation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var versions []branchlineage.PR
	for _, p := range prs {
		if old, ok := s.prs[p.Key]; ok {
			versions = append(versions, old)
		}
	}
	stored := append(s.inRepos(plan.Repositories(prs, versions)), versions...)
	pl, err := plan.Put(stored, func(k string) bool { _, ok := s.derived[k]; return ok }, prs, s.opts...)
	if err != nil {
		return branchlineage.Invalidation{}, err
	}
	for _, p := range prs {
		s.prs[p.Key] = p
	}
	for _, k := range pl.DropEdges {
		delete(s.edges, k)
	}
	for _, k := range pl.Forget {
		delete(s.derived, k)
		delete(s.pending, k)
	}
	for _, k := range pl.Queue {
		s.pending[k] = struct{}{}
	}
	return pl.Invalidation, nil
}

// Pending implements branchlineage.Store.
func (s *Store) Pending(context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedKeys(s.pending), nil
}

// Derive implements branchlineage.Store.
func (s *Store) Derive(context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := sortedKeys(s.pending)
	repos := map[string]struct{}{}
	for _, k := range keys {
		repos[s.prs[k].Repository] = struct{}{}
	}
	repoList := make([]string, 0, len(repos))
	for r := range repos {
		repoList = append(repoList, r)
	}
	state := s.inRepos(repoList)
	for _, k := range keys {
		if s.prs[k].Repository == "" {
			state = append(state, s.prs[k])
		}
	}
	ds, err := plan.Derive(state, keys, s.opts...)
	if err != nil {
		return 0, err
	}
	for _, d := range ds {
		if len(d.Edges) > 0 {
			s.edges[d.Carrier] = d.Edges
		} else {
			delete(s.edges, d.Carrier)
		}
		s.derived[d.Carrier] = struct{}{}
		delete(s.pending, d.Carrier)
	}
	return len(ds), nil
}

// EdgesFrom implements branchlineage.EdgeSource.
func (s *Store) EdgesFrom(_ context.Context, carriers []string) ([]branchlineage.Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []branchlineage.Edge
	for _, c := range carriers {
		out = append(out, s.edges[c]...)
	}
	return out, nil
}

// Edges implements branchlineage.Store.
func (s *Store) Edges(_ context.Context, repository string) ([]branchlineage.Edge, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []branchlineage.Edge
	for _, es := range s.edges {
		for _, e := range es {
			if e.Repository == repository {
				out = append(out, e)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Carrier != out[j].Carrier {
			return out[i].Carrier < out[j].Carrier
		}
		return out[i].Carried < out[j].Carried
	})
	return out, nil
}

// inRepos returns the stored pull requests of repos.
func (s *Store) inRepos(repos []string) []branchlineage.PR {
	want := make(map[string]struct{}, len(repos))
	for _, r := range repos {
		if r != "" {
			want[r] = struct{}{}
		}
	}
	var out []branchlineage.PR
	for _, p := range s.prs {
		if _, ok := want[p.Repository]; ok {
			out = append(out, p)
		}
	}
	return out
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
