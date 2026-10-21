// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const testRepo = "acme/widgets"

var testBase = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

// at is testBase plus min minutes.
func at(min int) time.Time { return testBase.Add(time.Duration(min) * time.Minute) }

// mpr builds a merged pull request in testRepo, merged at at(mergedMin), with
// 10 added and 5 deleted lines.
func mpr(key string, number int, head, base string, mergedMin int) PR {
	return PR{
		Key: key, Repository: testRepo, Number: number, HeadBranch: head, BaseBranch: base,
		OpenedAt: at(mergedMin - 60), MergedAt: at(mergedMin), LinesAdded: 10, LinesDeleted: 5,
	}
}

func history(t *testing.T, prs ...PR) *History {
	t.Helper()
	h, err := NewHistory(prs)
	require.NoError(t, err)
	return h
}

func carried(d Derivation) []string {
	out := make([]string, 0, len(d.Edges))
	for _, e := range d.Edges {
		out = append(out, e.Carried)
	}
	return out
}

func pairs(edges []WalkEdge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.Carrier+"->"+e.Carried)
	}
	return out
}

// edgeList is an EdgeSource over a fixed edge list, for walks over hand-built
// graphs.
type edgeList []Edge

func (l edgeList) EdgesFrom(_ context.Context, carriers []string) ([]Edge, error) {
	want := map[string]bool{}
	for _, c := range carriers {
		want[c] = true
	}
	var out []Edge
	for _, e := range l {
		if want[e.Carrier] {
			out = append(out, e)
		}
	}
	return out, nil
}

func edge(carrier, carriedKey, source, target string) Edge {
	return Edge{Repository: testRepo, Carrier: carrier, Carried: carriedKey, Source: source, Target: target}
}

// recorder is an Observer that keeps every event.
type recorder struct {
	counts  map[string]int
	samples map[string][]float64
	labels  map[string][]map[string]string
}

func newRecorder() *recorder {
	return &recorder{counts: map[string]int{}, samples: map[string][]float64{}, labels: map[string][]map[string]string{}}
}

func (r *recorder) Count(event string, labels map[string]string) {
	r.counts[event]++
	r.labels[event] = append(r.labels[event], labels)
}

func (r *recorder) Observe(event string, v float64, _ map[string]string) {
	r.samples[event] = append(r.samples[event], v)
}
