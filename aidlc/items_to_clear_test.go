// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"testing"
	"time"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// searchItemsToClear is a one-at-a-time reference search the closed form must agree
// with.
func searchItemsToClear(t MoveTerms, cut float64) int64 {
	if t.Population <= 0 {
		return 0
	}
	if t.Grain != aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE && cut >= 1 {
		return 0
	}
	above := func(h, p int64) bool { return float64(h)/float64(p) > cut }
	if above(t.Hits, t.Population) {
		return 0
	}
	for x := int64(1); ; x++ {
		if t.Grain == aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE {
			if above(t.Hits+x, t.Population) || t.Hits+x >= t.Population {
				return x
			}
			continue
		}
		if above(t.Hits+x, t.Population+x) {
			return x
		}
	}
}

// TestItemsToClear_AgreesWithTheSearch pins the closed form against the one-at-a-time
// search over populations up to a few thousand, every grain, and cuts across (0, 1].
func TestItemsToClear_AgreesWithTheSearch(t *testing.T) {
	cuts := []float64{0.01, 0.05, 0.2, 0.25, 1.0 / 3, 0.5, 0.7, 0.9, 0.99, 1.0}
	grains := []aidlcv1.ShareGrain{aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE, aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES}
	check := func(h, p int64) {
		for _, g := range grains {
			for _, c := range cuts {
				terms := MoveTerms{Hits: h, Population: p, Grain: g}
				if got, want := itemsToClear(terms, c), searchItemsToClear(terms, c); got != want {
					t.Fatalf("itemsToClear(%d of %d, %v, cut %v) = %d, want %d", h, p, g, c, got, want)
				}
			}
		}
	}
	for p := int64(1); p <= 300; p++ {
		for h := int64(0); h <= p; h++ {
			check(h, p)
		}
	}
	for _, p := range []int64{997, 1000, 1507, 2500, 3000} {
		for h := int64(0); h <= p; h += 7 {
			check(h, p)
		}
	}
}

// TestItemsToClear_LargePopulationsReturn pins that the cost does not grow with the
// population: a population of 1e12 answers at once, and the answer is the smallest
// count that clears the cut.
func TestItemsToClear_LargePopulationsReturn(t *testing.T) {
	done := make(chan [2]int64, 1)
	go func() {
		m := itemsToClear(MoveTerms{Hits: 1, Population: 1e12, Grain: aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES}, 0.5)
		p := itemsToClear(MoveTerms{Hits: 1, Population: 1e12, Grain: aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE}, 0.5)
		done <- [2]int64{m, p}
	}()
	select {
	case got := <-done:
		above := func(h, p int64) bool { return float64(h)/float64(p) > 0.5 }
		if m := got[0]; !above(1+m, 1e12+m) || above(m, 1e12+m-1) {
			t.Errorf("merged-change answer %d is not the smallest that clears 0.5", m)
		}
		if p := got[1]; !above(1+p, 1e12) || above(p, 1e12) {
			t.Errorf("people answer %d is not the smallest that clears 0.5", p)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("itemsToClear did not return within 5s at a population of 1e12")
	}
}

// TestItemsToClear_PeopleStopAtTheRoster pins the people grain at a cut of one: no
// count is above it, so the answer stops at the people not yet counted.
func TestItemsToClear_PeopleStopAtTheRoster(t *testing.T) {
	if got := itemsToClear(MoveTerms{Hits: 3, Population: 10, Grain: aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE}, 1.0); got != 7 {
		t.Errorf("itemsToClear(3 of 10 people, cut 1) = %d, want 7", got)
	}
}
