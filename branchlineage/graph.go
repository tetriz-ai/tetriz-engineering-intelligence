// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"context"
	"sort"
)

// EdgeSource supplies stored edges to the walk.
type EdgeSource interface {
	// EdgesFrom returns the live edges of every carrier in carriers, in any
	// order. An unknown carrier has no edges and is not an error.
	EdgesFrom(ctx context.Context, carriers []string) ([]Edge, error)
}

// Graph is a derived edge set held in memory. It is an EdgeSource.
type Graph struct {
	derivations []Derivation
	byCarrier   map[string][]Edge
}

// Derive derives every merged pull request in prs and returns the edge graph:
// [NewHistory] then [History.DeriveAll].
func Derive(prs []PR, opts ...Option) (*Graph, error) {
	h, err := NewHistory(prs, opts...)
	if err != nil {
		return nil, err
	}
	return NewGraph(h.DeriveAll()), nil
}

// NewGraph holds derivations as a Graph.
func NewGraph(ds []Derivation) *Graph {
	g := &Graph{derivations: ds, byCarrier: make(map[string][]Edge, len(ds))}
	for _, d := range ds {
		if len(d.Edges) > 0 {
			g.byCarrier[d.Carrier] = d.Edges
		}
	}
	return g
}

// Derivations returns every derivation, carriers and non-carriers alike.
func (g *Graph) Derivations() []Derivation { return g.derivations }

// Edges returns every edge, ordered by carrier and then carried.
func (g *Graph) Edges() []Edge {
	var out []Edge
	for _, d := range g.derivations {
		out = append(out, d.Edges...)
	}
	sortEdges(out)
	return out
}

// EdgesFrom implements EdgeSource.
func (g *Graph) EdgesFrom(_ context.Context, carriers []string) ([]Edge, error) {
	var out []Edge
	for _, c := range carriers {
		out = append(out, g.byCarrier[c]...)
	}
	return out, nil
}

// sortEdges orders edges by carrier, then carried key.
func sortEdges(es []Edge) {
	sort.Slice(es, func(i, j int) bool {
		if es[i].Carrier != es[j].Carrier {
			return es[i].Carrier < es[j].Carrier
		}
		return es[i].Carried < es[j].Carried
	})
}
