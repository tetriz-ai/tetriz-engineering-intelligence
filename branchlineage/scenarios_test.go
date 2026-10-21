// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package branchlineage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// scenario is one testdata/scenarios file: a repository's merge history, its
// flow, and the numbers the engine must reproduce.
type scenario struct {
	Repository string    `json:"repository"`
	About      string    `json:"about"`
	Note       string    `json:"note"`
	Base       time.Time `json:"base"`
	Tracked    []string  `json:"tracked"`
	Flow       []Hop     `json:"flow"`
	MaxDepth   int       `json:"max_depth"`
	PRs        []struct {
		Number          int      `json:"number"`
		Head            string   `json:"head"`
		Base            string   `json:"base"`
		MergedHour      float64  `json:"merged_hour"`
		FirstCommitHour *float64 `json:"first_commit_hour"`
		CrossRepository bool     `json:"cross_repository"`
		Attributed      bool     `json:"attributed"`
	} `json:"prs"`
	Expected struct {
		Edges            int `json:"edges"`
		AttributedBefore int `json:"attributed_before"`
		AttributedAfter  int `json:"attributed_after"`
		Carried          int `json:"carried"`
		Unresolvable     int `json:"unresolvable"`
		DepthCapHits     int `json:"depth_cap_hits"`
		Projected        int `json:"projected"`
	} `json:"expected"`
}

func (s scenario) hour(h float64) time.Time {
	return s.Base.Add(time.Duration(h * float64(time.Hour)))
}

// load returns the scenario's pull requests and its directly attributed keys.
func (s scenario) load() ([]PR, map[string]struct{}) {
	var prs []PR
	attributed := map[string]struct{}{}
	for _, p := range s.PRs {
		pr := PR{
			Key:        fmt.Sprintf("%s#PR-%d#merged", s.Repository, p.Number),
			Repository: s.Repository, Number: p.Number, HeadBranch: p.Head, BaseBranch: p.Base,
			MergedAt: s.hour(p.MergedHour), OpenedAt: s.hour(p.MergedHour - 2),
			LinesAdded: 40, LinesDeleted: 10, CrossRepository: p.CrossRepository,
		}
		if p.FirstCommitHour != nil {
			pr.FirstCommitAt = s.hour(*p.FirstCommitHour)
		}
		if p.Attributed {
			attributed[pr.Key] = struct{}{}
		}
		prs = append(prs, pr)
	}
	return prs, attributed
}

func loadScenarios(t *testing.T) map[string]scenario {
	t.Helper()
	files, err := filepath.Glob("testdata/scenarios/*.json")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	out := map[string]scenario{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		require.NoError(t, err)
		var s scenario
		require.NoError(t, json.Unmarshal(raw, &s), f)
		out[strings.TrimSuffix(filepath.Base(f), ".json")] = s
	}
	return out
}

// TestScenarios derives each scenario's history end to end and checks the edge
// count, the attributed tracked-branch merges before and after projection, the
// carried pairs and the depth-cap report.
func TestScenarios(t *testing.T) {
	for name, s := range loadScenarios(t) {
		t.Run(name, func(t *testing.T) {
			prs, attributed := s.load()
			g, err := Derive(prs)
			require.NoError(t, err)
			assert.Len(t, g.Edges(), s.Expected.Edges, "edges")

			tracked := map[string]bool{}
			for _, b := range s.Tracked {
				tracked[b] = true
			}
			seeds := TrackedSeeds(prs, func(_, branch string) bool { return tracked[branch] })
			onTracked := map[string]bool{}
			for _, sd := range seeds {
				onTracked[sd.Carrier] = true
			}
			countOnTracked := func(keys map[string]struct{}) int {
				n := 0
				for k := range keys {
					if onTracked[k] {
						n++
					}
				}
				return n
			}
			assert.Equal(t, s.Expected.AttributedBefore, countOnTracked(attributed), "attributed before")

			limits := Limits{MaxDepth: s.MaxDepth}
			proj, err := Project(context.Background(), g, ProjectInput{
				Seeds: seeds, Flows: map[string][]Hop{s.Repository: s.Flow}, Attributed: attributed, Limits: limits,
			})
			require.NoError(t, err)
			after := map[string]struct{}{}
			for k := range attributed {
				after[k] = struct{}{}
			}
			for _, k := range proj.Keys() {
				after[k] = struct{}{}
			}
			assert.Equal(t, s.Expected.AttributedAfter, countOnTracked(after), "attributed after")
			if s.Expected.Projected > 0 {
				assert.Len(t, after, s.Expected.Projected, "the attributed set after projection")
			}

			rec := newRecorder()
			walked, err := Walk(context.Background(), g, seeds, s.Flow, limits, WithObserver(rec))
			require.NoError(t, err)
			cw := CarriedBy(walked, nil)
			assert.Len(t, cw.Pairs, s.Expected.Carried, "carried pairs")
			assert.Equal(t, s.Expected.Unresolvable, cw.Unresolvable, "carried pull requests not resolved")
			assert.Equal(t, s.Expected.DepthCapHits, rec.counts["walk_depth_cap_hit"], "depth cap hits")
		})
	}
}

// TestScenario_NinePRPaths pins the path behind each credit on the nine-pull-
// request topology: the release promotion is credited through the develop
// promotion it carried, and that promotion stays off the tracked branch.
func TestScenario_NinePRPaths(t *testing.T) {
	s := loadScenarios(t)["nine_pr"]
	prs, attributed := s.load()
	g, err := Derive(prs)
	require.NoError(t, err)
	key := func(n int) string { return fmt.Sprintf("%s#PR-%d#merged", s.Repository, n) }
	byCarrier := map[string][]string{}
	for _, e := range g.Edges() {
		byCarrier[e.Carrier] = append(byCarrier[e.Carrier], e.Carried)
	}
	assert.Equal(t, map[string][]string{
		key(10): {key(1), key(2), key(4)},
		key(11): {key(10)},
		key(12): {key(3)},
	}, byCarrier)

	proj, err := Project(context.Background(), g, ProjectInput{
		Seeds:      TrackedSeeds(prs, func(_, b string) bool { return b == "main" }),
		Flows:      map[string][]Hop{s.Repository: s.Flow},
		Attributed: attributed,
	})
	require.NoError(t, err)
	paths := map[string][]string{}
	for _, c := range proj.Credits {
		paths[c.Carrier] = c.Path
	}
	assert.Equal(t, map[string][]string{
		key(10): {key(10), key(1)},
		key(11): {key(11), key(10), key(1)},
		key(12): {key(12), key(3)},
	}, paths)
}
