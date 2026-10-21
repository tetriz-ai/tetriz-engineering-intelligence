// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"fmt"
	"math"
	"sort"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// bottleneckIDReviewReach and bottleneckIDAdoptionReach are the two org-level
// Bottleneck.ID values this file derives; profile.go's title/body/move copy switches
// on them by name. The prefixes are the id shapes of the per-cell, per-stage and
// per-gap findings (prefix + stage/level/gap key).
const (
	bottleneckIDReviewReach   = "review_reach"
	bottleneckIDAdoptionReach = "adoption_reach"

	bottleneckIDPrefixRung   = "rung_"
	bottleneckIDPrefixLadder = "ladder_"
	bottleneckIDPrefixGap    = "gap_"
)

// adoptionPassiveLine is the passive share of the roster at or above which adoption
// reach fires, and the line its progress bar and distance are measured against.
const adoptionPassiveLine = 0.5

// OrgRead carries the org-level readings the bottleneck derivations need beyond the
// per-cell readings: the adoption cohort (for adoption_reach), whether declarations
// are honoured, and the active engineers (for breadth). fold.Fold fills it, or a
// caller sets it directly. Absent cohort data simply means adoption_reach
// cannot fire — an empty finding is a true statement.
type OrgRead struct {
	// HasAdoptionCohort is whether the cohort read is present.
	HasAdoptionCohort bool
	// AdoptionDenominatorPeople is the people who merged work in the window.
	AdoptionDenominatorPeople int
	// AdoptionPassivePeople is those with no AI-authored merge.
	AdoptionPassivePeople int
	// DeclarationsHonoured is whether this organisation's admin declarations are
	// honoured. It tells apart the two causes of a closed review-policy read:
	// declarations not honoured, or honoured and nothing declared. It never changes a
	// verdict.
	DeclarationsHonoured bool
	// ActiveEngineers is the organisation's active engineers in the window, breadth's
	// denominator. A stage's breadth numerator is its
	// presence people set intersected with this, so the ratio can never exceed one.
	ActiveEngineers PeopleSet
	// ActiveEngineersUnresolved is set when the active-engineer set could not be
	// read; ActiveEngineers is then meaningless and
	// every stage's breadth is absent rather than computed over a partial set.
	ActiveEngineersUnresolved bool
}

// Bottleneck is one ranked finding derived from the scored grid, the form a caller
// stores before assembly into a MaturityBottleneck.
type Bottleneck struct {
	ID      string
	Subject string
	Impact  aidlcv1.Impact
	Stage   aidlcv1.AidlcStage
	// Level is the rung a rung_ finding missed. For a ladder_ finding it is the
	// stage's lowest missed rung, whose definition the constraint's copy names. It is
	// unset for a finding with no single rung (review or adoption reach, a gap).
	Level   aidlcv1.AidlcLevel
	Rank    int
	HasMove bool
	Signals []aidlcv1.SignalKey
	// Terms are the numbers the move is composed from; nil when the finding rests on
	// no share (a gap, a ladder whose first missed rung recorded none).
	Terms *MoveTerms
	// SecondaryMiss is true when Terms was taken from the missed rung's secondary
	// share (Cell.Share2), not its primary one — the risk-class rule (review/L5,
	// product-spec/L5) denies a cell whose primary share is fine when its secondary
	// (the declared-policy-approval share) is not. A composer must ground copy in the
	// secondary practice when this is set, not the primary's.
	SecondaryMiss bool
	weight        float64
}

// deriveBottlenecks runs the five derivations over the scored grid and the org read, ranked by impact then
// weight. Each fires only on inputs actually read for this org.
func deriveBottlenecks(rs *rules.RuleSet, grid map[aidlcv1.AidlcStage]map[aidlcv1.AidlcLevel]*Cell, org OrgRead) []Bottleneck {
	active := map[rules.BottleneckKind]bool{}
	move := map[rules.BottleneckKind]bool{}
	for _, b := range rs.Bottlenecks {
		active[b.Kind] = true
		move[b.Kind] = b.HasMove
	}
	var items []Bottleneck

	// 1. review_reach: AI-attributed delivery that no recorded review reached, read off
	// code L3's own share: the AI delivery PRs a person reviewed (hits) over every AI
	// delivery PR (population), with the PRs whose review could not be read as its
	// unread mass. It reads only a share code L3 recorded from resolved readings: a
	// missing, gap-blocked, stale or invalid reading records none, so there is no reach
	// to report. The bottleneck fires only when even the share's widened upper bound,
	// crediting every unread PR as reviewed, is below the threshold, so an org that
	// merely lacks reviewer data (a bound that widens to one) does not read as an
	// unreviewed one, while a share that straddles a lower cut still does.
	if active[rules.BottleneckKindReviewReach] {
		code := grid[aidlcv1.AidlcStage_AIDLC_STAGE_CODE]
		if code != nil {
			l3 := code[aidlcv1.AidlcLevel_AIDLC_LEVEL_L3]
			if recordedShare(l3) {
				a, r, unread := float64(l3.Share.Population), float64(l3.Share.Hits), float64(l3.Share.Unobserved)
				// Under v1 a recorded merged-change share already clears this floor; the
				// guard stays for a hand-built RuleSet, which is exported.
				if a >= float64(rs.PopulationFloors.Artifacts) {
					rateHi := math.Min(1.0, (r+unread)/a) // the share's widened upper bound
					if rateHi < rs.BandCuts.Established {
						items = append(items, Bottleneck{
							ID: bottleneckIDReviewReach, Subject: AudienceOrg,
							Impact: impactBand(rateHi < 0.25, false), // high if the upper bound is still <0.25, else medium
							Stage:  aidlcv1.AidlcStage_AIDLC_STAGE_CODE,
							weight: (1.0 - rateHi) * a, HasMove: move[rules.BottleneckKindReviewReach],
							Terms: &MoveTerms{Hits: int64(r), Population: int64(a), Unobserved: int64(unread), Grain: aidlcv1.ShareGrain_SHARE_GRAIN_MERGED_CHANGES},
							Signals: []aidlcv1.SignalKey{
								aidlcv1.SignalKey_SIGNAL_KEY_CODE_AGENT_UNIVERSE,
								aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_MERGED_PRS_HUMAN_REVIEWED,
							},
						})
					}
				}
			}
		}
	}

	// 2. adoption_reach: adoption that stops before most people who merge work.
	if active[rules.BottleneckKindAdoptionReach] && org.HasAdoptionCohort && org.AdoptionDenominatorPeople > 0 {
		share := float64(org.AdoptionPassivePeople) / float64(org.AdoptionDenominatorPeople)
		if share >= adoptionPassiveLine {
			items = append(items, Bottleneck{
				ID: bottleneckIDAdoptionReach, Subject: AudienceOrg,
				Impact:  impactBand(share >= 0.7, false),
				weight:  share * float64(org.AdoptionDenominatorPeople),
				HasMove: move[rules.BottleneckKindAdoptionReach],
				Terms:   &MoveTerms{Hits: int64(org.AdoptionPassivePeople), Population: int64(org.AdoptionDenominatorPeople), Grain: aidlcv1.ShareGrain_SHARE_GRAIN_PEOPLE},
			})
		}
	}

	// 3. rung_<stage>_<level>: the first not-reached rung on a stage with a met rung.
	if active[rules.BottleneckKindRungMissed] {
		for _, stage := range stageOrder {
			cells := grid[stage]
			if cells == nil {
				continue
			}
			var firstMissed aidlcv1.AidlcLevel
			var missedTerms *MoveTerms
			var missedSecondary bool
			foundMissed, anyMet := false, false
			for _, lv := range ladderOrder {
				c := cells[lv]
				if c == nil {
					continue
				}
				if c.State == aidlcv1.CellState_CELL_STATE_NOT_REACHED && !foundMissed {
					firstMissed, foundMissed = lv, true
					missedTerms, missedSecondary = termsOf(c), c.SecondaryMiss
				}
				if c.State == aidlcv1.CellState_CELL_STATE_MET {
					anyMet = true
				}
			}
			if foundMissed && anyMet {
				rung := levelOrd(firstMissed)
				items = append(items, Bottleneck{
					ID:      fmt.Sprintf("%s%s_%s", bottleneckIDPrefixRung, stageKey(stage), levelKey(firstMissed)),
					Subject: AudienceOrg, Impact: impactBand(false, rung >= 3),
					Stage: stage, Level: firstMissed, weight: 8.0 * float64(rung),
					HasMove: move[rules.BottleneckKindRungMissed], Terms: missedTerms, SecondaryMiss: missedSecondary,
				})
			}
		}
	}

	// 4. ladder_<stage>: stages read end to end with nothing met, top two by miss count.
	if active[rules.BottleneckKindLadderStalled] {
		type stalled struct {
			stage          aidlcv1.AidlcStage
			n              int
			first          *MoveTerms
			firstLevel     aidlcv1.AidlcLevel
			firstSecondary bool
		}
		var st []stalled
		for _, stage := range stageOrder {
			cells := grid[stage]
			if cells == nil {
				continue
			}
			met, miss := 0, 0
			var first *MoveTerms
			var firstLevel aidlcv1.AidlcLevel
			var firstSecondary bool
			for _, lv := range ladderOrder {
				c := cells[lv]
				if c == nil {
					continue
				}
				if c.State == aidlcv1.CellState_CELL_STATE_MET {
					met++
				}
				if c.State == aidlcv1.CellState_CELL_STATE_NOT_REACHED {
					miss++
					if first == nil {
						first, firstLevel, firstSecondary = termsOf(c), lv, c.SecondaryMiss // the lowest missed rung is the one to start with
					}
				}
			}
			if met == 0 && miss > 0 {
				st = append(st, stalled{stage, miss, first, firstLevel, firstSecondary})
			}
		}
		sort.SliceStable(st, func(i, j int) bool { return st[i].n > st[j].n })
		if len(st) > 2 {
			st = st[:2]
		}
		for _, s := range st {
			items = append(items, Bottleneck{
				ID: bottleneckIDPrefixLadder + stageKey(s.stage), Subject: AudienceOrg,
				Impact: impactBand(false, s.n >= 2), Stage: s.stage, Level: s.firstLevel,
				weight: 10.0 * float64(s.n), HasMove: move[rules.BottleneckKindLadderStalled], Terms: s.first, SecondaryMiss: s.firstSecondary,
			})
		}
	}

	// 5. gap_<gid>: the instrumentation gap blocking the most cells (product subject).
	if active[rules.BottleneckKindGapBlocks] {
		blocked := map[aidlcv1.InstrumentationGap]int{}
		for _, stage := range stageOrder {
			for _, lv := range ladderOrder {
				c := grid[stage][lv]
				if c != nil && c.State == aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE &&
					c.Gap != aidlcv1.InstrumentationGap_INSTRUMENTATION_GAP_UNSPECIFIED {
					blocked[c.Gap]++
				}
			}
		}
		if len(blocked) > 0 {
			var best aidlcv1.InstrumentationGap
			bestN := -1
			for g, n := range blocked {
				if n > bestN || (n == bestN && int(g) < int(best)) {
					best, bestN = g, n
				}
			}
			items = append(items, Bottleneck{
				ID: bottleneckIDPrefixGap + gapKey(best), Subject: AudienceProduct,
				Impact: impactBand(bestN >= 8, bestN >= 4), weight: 5.0 * float64(bestN),
				HasMove: move[rules.BottleneckKindGapBlocks],
			})
		}
	}

	sort.SliceStable(items, func(i, j int) bool {
		ri, rj := impactRank(items[i].Impact), impactRank(items[j].Impact)
		if ri != rj {
			return ri > rj
		}
		return items[i].weight > items[j].weight
	})
	for i := range items {
		items[i].Rank = i + 1
	}
	return items
}

// recordedShare reports whether a cell recorded a share: the scorer records one only
// once both terms resolved to valid counts over a non-empty population.
func recordedShare(c *Cell) bool {
	return c != nil && c.Share != nil && c.Share.Population > 0
}

// impactBand returns HIGH when high is true, else MEDIUM_HIGH when medHigh is true,
// else MEDIUM.
func impactBand(high, medHigh bool) aidlcv1.Impact {
	switch {
	case high:
		return aidlcv1.Impact_IMPACT_HIGH
	case medHigh:
		return aidlcv1.Impact_IMPACT_MEDIUM_HIGH
	default:
		return aidlcv1.Impact_IMPACT_MEDIUM
	}
}

// termsOf copies a scored cell's share onto move terms; nil when the cell recorded no
// share. Prefers the secondary share (Share2) over the primary (Share) when the cell's
// NOT_REACHED verdict came from the secondary denying it (SecondaryMiss) — using the
// primary there would hand a composer the share that actually cleared the cut.
func termsOf(c *Cell) *MoveTerms {
	if c == nil {
		return nil
	}
	sh := c.Share
	if c.SecondaryMiss && c.Share2 != nil {
		sh = c.Share2
	}
	if sh == nil || sh.Population == 0 {
		return nil
	}
	return &MoveTerms{Hits: sh.Hits, Population: sh.Population, Unobserved: sh.Unobserved, Grain: sh.Grain}
}

func impactRank(i aidlcv1.Impact) int {
	switch i {
	case aidlcv1.Impact_IMPACT_HIGH:
		return 2
	case aidlcv1.Impact_IMPACT_MEDIUM_HIGH:
		return 1
	default:
		return 0
	}
}
