// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package aidlc

import (
	"sort"
	"time"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

// isoDay is the layout of every calendar date a movement point carries.
const isoDay = "2006-01-02"

// movementShareEpsilon absorbs float round-trip noise on the stored share bounds
// so a bit-identical interval never reads as "narrowed".
const movementShareEpsilon = 1e-9

// movementAxisWeeks is the movement series' fixed width in weeks, the same for every
// caller rather than something a caller's date range controls. DeriveMovement never
// reports more weeks than this, however many calendar weeks the caller's
// [from, to] touches.
const movementAxisWeeks = 12

// promotionHoldWeeks is how many consecutive calendar weeks a risen reading must
// hold before it is reported as a promotion, and the weeks_held a MOVED_UP point
// carries. Demotion has no hold.
const promotionHoldWeeks = 2

// MovementSnapshotRow is what DeriveMovement needs from one stored day: whether the
// day published, and its ranked bottlenecks (meaningful only when Published). A day's
// profile carries both: a published profile's Bottlenecks, and Published false for a
// withheld or failed day.
type MovementSnapshotRow struct {
	AsOfDate    time.Time
	Published   bool
	Bottlenecks []*aidlcv1.MaturityBottleneck
}

// MovementCellRow is what DeriveMovement needs from one cell's reading on one day: a
// Result's Cells carry each field (State, Band, and Share's Lo and Hi). ShareLow and
// ShareHigh are meaningful only when HasShare is true. DeriveMovement reads a row only
// when a MovementSnapshotRow marks its calendar day (UTC) Published; a withheld day's
// rows, or a day with no snapshot, are ignored.
type MovementCellRow struct {
	AsOfDate            time.Time
	Stage               aidlcv1.AidlcStage
	Level               aidlcv1.AidlcLevel
	State               aidlcv1.CellState
	Band                aidlcv1.PrevalenceBand
	HasShare            bool
	ShareLow, ShareHigh float64
}

// cellKey identifies one cell's row across weeks — the movement series' grain for
// every rule but STUCK and HISTORY_STARTS.
type cellKey struct {
	stage aidlcv1.AidlcStage
	level aidlcv1.AidlcLevel
}

// DeriveMovement builds the movement series over stored days: the range's day rows
// (the published-day set and each day's ranked bottlenecks) and cell rows (verdict,
// band, share bounds). from and to are the inclusive date range; historyFrom is the
// earliest day any profile exists for, the zero time when none does.
//
// Weeks are ISO weeks (Monday start); a week's reading is its last published day,
// and a cell row whose day the snapshots do not mark published is never read.
// A week with no published day is a gap: no week-over-week comparison spans it,
// while the verdict a cell last held carries across it. The series drops a trailing
// week that has not run yet and covers at most 12 weeks, so weeks_held and the stuck
// duration count only published weeks inside it.
//
// MOVED_DOWN means met to not reached only. A cell that stays met but falls from
// established to emerging produces no point, while its rise back is reported as
// MOVED_UP.
func DeriveMovement(
	snaps []MovementSnapshotRow,
	cells []MovementCellRow,
	from, to time.Time,
	historyFrom time.Time,
) []*aidlcv1.MaturityMovementPoint {
	publishedWeeks, bottlenecksByWeek := groupSnapshotsByWeek(snaps)
	weeks := trimAxis(weekSequence(from, to), publishedWeeks)
	cellsByKey := groupCellsByWeek(cells, publishedDays(snaps))

	var out []*aidlcv1.MaturityMovementPoint
	out = append(out, derivePerCellMovement(cellsByKey, weeks)...)
	out = append(out, deriveStuck(weeks, bottlenecksByWeek)...)
	if p := deriveHistoryStarts(weeks, publishedWeeks, from, historyFrom); p != nil {
		out = append(out, p)
	}

	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.GetWeekStart() != b.GetWeekStart() {
			return a.GetWeekStart() < b.GetWeekStart()
		}
		if a.GetStage() != b.GetStage() {
			return a.GetStage() < b.GetStage()
		}
		if a.GetLevel() != b.GetLevel() {
			return a.GetLevel() < b.GetLevel()
		}
		return a.GetBottleneckId() < b.GetBottleneckId()
	})
	return out
}

// weekSequence lists every ISO week (its Monday) touching [from, to], oldest
// first — the calendar axis every rule walks, including weeks with no published
// day (a gap the chain does not span).
func weekSequence(from, to time.Time) []time.Time {
	var weeks []time.Time
	for wk := isoWeekMonday(from); !wk.After(isoWeekMonday(to)); wk = wk.AddDate(0, 0, 7) {
		weeks = append(weeks, wk)
	}
	return weeks
}

// trimAxis narrows a raw calendar week list to the axis DeriveMovement actually
// reports: at most movementAxisWeeks ISO weeks within the requested range, ending
// at the latest published week. Every trailing week with no published day — not
// run yet, or failed — is dropped rather than shown as a gap or counted toward
// weeks_held, so the axis can report fewer weeks than the cap. A gap earlier in
// the list is left in place, where it breaks the week-over-week comparisons and a
// stuck run. The result is then capped to the most recent movementAxisWeeks
// entries, so a caller's range touching one calendar week more than the series'
// fixed width (an artifact of day-count math, not intent) never inflates the
// reported width.
func trimAxis(weeks []time.Time, publishedWeeks map[time.Time]bool) []time.Time {
	end := len(weeks)
	for end > 0 && !publishedWeeks[weeks[end-1]] {
		end--
	}
	weeks = weeks[:end]
	if len(weeks) > movementAxisWeeks {
		weeks = weeks[len(weeks)-movementAxisWeeks:]
	}
	return weeks
}

// isoWeekMonday returns the UTC midnight Monday of t's ISO week.
func isoWeekMonday(t time.Time) time.Time {
	t = t.UTC()
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	offset := (int(day.Weekday()) + 6) % 7 // Monday=0 ... Sunday=6
	return day.AddDate(0, 0, -offset)
}

// groupSnapshotsByWeek indexes every PUBLISHED snapshot by its ISO week, keeping
// only the week's last published day (the reading every rule uses) and that day's
// ranked bottlenecks by id.
func groupSnapshotsByWeek(
	snaps []MovementSnapshotRow,
) (published map[time.Time]bool, bottlenecks map[time.Time]map[string]*aidlcv1.MaturityBottleneck) {
	published = map[time.Time]bool{}
	bottlenecks = map[time.Time]map[string]*aidlcv1.MaturityBottleneck{}
	lastDay := map[time.Time]time.Time{}
	for _, sn := range snaps {
		if !sn.Published {
			continue
		}
		wk := isoWeekMonday(sn.AsOfDate)
		published[wk] = true
		if prev, ok := lastDay[wk]; ok && !sn.AsOfDate.After(prev) {
			continue
		}
		lastDay[wk] = sn.AsOfDate
		ids := map[string]*aidlcv1.MaturityBottleneck{}
		for _, b := range sn.Bottlenecks {
			ids[b.GetId()] = b
		}
		bottlenecks[wk] = ids
	}
	return published, bottlenecks
}

// publishedDays is the set of calendar days (UTC midnight) a snapshot marks published.
func publishedDays(snaps []MovementSnapshotRow) map[time.Time]bool {
	days := map[time.Time]bool{}
	for _, sn := range snaps {
		if sn.Published {
			days[utcDay(sn.AsOfDate)] = true
		}
	}
	return days
}

// utcDay is t's calendar day at UTC midnight.
func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// groupCellsByWeek indexes every cell row of a published day by (stage, level) then by
// ISO week, keeping only the week's last published day per cell. A row from a withheld
// day never becomes a week's reading.
func groupCellsByWeek(cells []MovementCellRow, published map[time.Time]bool) map[cellKey]map[time.Time]MovementCellRow {
	out := map[cellKey]map[time.Time]MovementCellRow{}
	lastDay := map[cellKey]map[time.Time]time.Time{}
	for _, c := range cells {
		if !published[utcDay(c.AsOfDate)] {
			continue
		}
		key := cellKey{stage: c.Stage, level: c.Level}
		wk := isoWeekMonday(c.AsOfDate)
		if lastDay[key] == nil {
			lastDay[key] = map[time.Time]time.Time{}
		}
		if prev, ok := lastDay[key][wk]; ok && !c.AsOfDate.After(prev) {
			continue
		}
		lastDay[key][wk] = c.AsOfDate
		if out[key] == nil {
			out[key] = map[time.Time]MovementCellRow{}
		}
		out[key][wk] = c
	}
	return out
}

// cellRank orders a reading on the promotion ladder: not-met is 0, met at an
// emerging (or rule-less) band is 1, met at an established band is 2. It decides
// whether a reading rises above the held verdict (a promotion candidate) or not
// (taking effect at once); demotion itself is the met-to-not-reached state change.
func cellRank(state aidlcv1.CellState, band aidlcv1.PrevalenceBand) int {
	if state != aidlcv1.CellState_CELL_STATE_MET {
		return 0
	}
	if band == aidlcv1.PrevalenceBand_PREVALENCE_BAND_ESTABLISHED {
		return 2
	}
	return 1
}

// derivePerCellMovement walks each cell's readings in calendar order, applying
// rules 1 (lost sight of), 2 (coverage gained), 3 (demotion) and 4 (promotion).
//
// Promotion and demotion are both measured against the cell's held verdict: the
// last visible reading that took effect — never an unconfirmed promotion
// candidate — carried across abstains and gaps. A reading at or below the held
// rung takes effect at once, so a fall from met to not reached is a demotion the
// week it is measured. A reading above it is only a pending candidate, and takes
// effect (a promotion) the week the SAME reading holds for the next consecutive
// calendar week, promotionHoldWeeks in all. A fall while a candidate is pending
// clears the candidate silently, since the rise it would have undone never took
// effect: a cell straddling the cut draws no line in either direction. Exactly
// one point is emitted per cell-week: when a week both confirms a pending
// promotion and narrows its share bounds, the promotion wins and coverage-gained
// is not also emitted (the first matching rule wins).
//
// Because the held verdict carries across abstains and gaps, a cell that is met,
// loses sight, then is met again at the same rung has not moved, and a rise or
// fall measured after a gap is still a movement. A loss of sight is judged the
// same way: an abstain whose last visible reading, across gaps, has not already
// been reported lost is reported on the first axis week the abstain is in effect,
// whether the loss began that week, before a gap, or before the axis. A gap
// breaks only the strictly week-over-week comparisons: the promotion hold (a
// candidate is confirmed only by the calendar week right after it) and coverage
// gained.
//
// The walk starts at the cell's oldest row, not at the axis: a caller may pass a
// week or more before the requested range as a baseline, and the twelve-week cap can
// drop further weeks off the axis's front, so a cell's state entering the axis can
// depend on several weeks the series does not show. Every week read runs through
// the same rules, and points are emitted only for weeks on the axis.
func derivePerCellMovement(
	cellsByKey map[cellKey]map[time.Time]MovementCellRow,
	weeks []time.Time,
) []*aidlcv1.MaturityMovementPoint {
	if len(weeks) == 0 {
		return nil
	}
	axisStart, axisEnd := weeks[0], weeks[len(weeks)-1]
	var out []*aidlcv1.MaturityMovementPoint

	keys := make([]cellKey, 0, len(cellsByKey))
	for key := range cellsByKey {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].stage != keys[j].stage {
			return keys[i].stage < keys[j].stage
		}
		return keys[i].level < keys[j].level
	})

	visible := func(r MovementCellRow) bool {
		return r.State != aidlcv1.CellState_CELL_STATE_NOT_YET_VISIBLE
	}
	sameReading := func(a, b MovementCellRow) bool {
		return a.State == b.State && a.Band == b.Band
	}

	for _, key := range keys {
		byWeek := cellsByKey[key]
		walkStart := axisStart
		for wk := range byWeek {
			if wk.Before(walkStart) {
				walkStart = wk
			}
		}

		// A lost-sight point says whether the cell reads again in the axis's last week.
		lastRow, lastOK := byWeek[axisEnd]
		visibleAgain := lastOK && visible(lastRow)

		var held, candidate MovementCellRow
		haveHeld, pending := false, false
		// lastReadingVisible is whether the cell's most recent reading, skipping
		// gaps, was visible; lossUnreported is a loss of sight not yet emitted.
		lastReadingVisible, lossUnreported := false, false

		for wk := walkStart; !wk.After(axisEnd); wk = wk.AddDate(0, 0, 7) {
			onAxis := !wk.Before(axisStart)
			emit := func(kind aidlcv1.MaturityMovementKind, weeksHeld int32) {
				if onAxis {
					p := movementPoint(wk, kind, key, weeksHeld, "", "")
					p.VisibleAgain = kind == aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_LOST_SIGHT && visibleAgain
					out = append(out, p)
				}
			}

			r, ok := byWeek[wk]
			if !ok {
				pending = false
				continue
			}
			prevWeek := wk.AddDate(0, 0, -7)
			pr, prevOK := byWeek[prevWeek]
			prevVisible := prevOK && visible(pr)

			if !visible(r) {
				if lastReadingVisible {
					lossUnreported = true
				}
				if onAxis && lossUnreported {
					emit(aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_LOST_SIGHT, 0)
					lossUnreported = false
				}
				lastReadingVisible, pending = false, false
				continue
			}
			lastReadingVisible, lossUnreported = true, false

			if !haveHeld {
				held, haveHeld = r, true
				continue
			}

			switch {
			// A candidate is cleared on every week that does not confirm it, so a pending
			// candidate is always the previous calendar week's.
			case pending && sameReading(r, candidate):
				emit(aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_MOVED_UP, promotionHoldWeeks)
				held, pending = r, false

			case cellRank(r.State, r.Band) > cellRank(held.State, held.Band):
				candidate, pending = r, true

			default:
				pending = false
				narrowed := prevVisible && sameReading(pr, r) && pr.HasShare && r.HasShare &&
					(pr.ShareHigh-pr.ShareLow)-(r.ShareHigh-r.ShareLow) > movementShareEpsilon
				switch {
				case held.State == aidlcv1.CellState_CELL_STATE_MET && r.State == aidlcv1.CellState_CELL_STATE_NOT_REACHED:
					emit(aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_MOVED_DOWN, 0)
				case narrowed:
					emit(aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_COVERAGE_GAINED, 0)
				}
				held = r
			}
		}
	}
	return out
}

// stuckMinWeeks is the shortest trailing run of consecutive weeks a constraint must
// be present in to be reported stuck.
const stuckMinWeeks = 3

// deriveStuck emits one STUCK point per bottleneck id present in a trailing run of
// at least stuckMinWeeks consecutive axis weeks, ending at the axis's last week,
// with weeks_held the run's length. The run is counted back from the last week
// while the id is present in each week; a gap week (no published day) carries no
// bottleneck data, so it ends every run — a constraint cannot be "stuck" through a
// week no profile was published for. An id absent from the last week is not stuck,
// however long it was present before.
func deriveStuck(
	weeks []time.Time,
	bottlenecksByWeek map[time.Time]map[string]*aidlcv1.MaturityBottleneck,
) []*aidlcv1.MaturityMovementPoint {
	if len(weeks) < stuckMinWeeks {
		return nil
	}
	lastWeek := weeks[len(weeks)-1]
	type run struct {
		id    string
		weeks int
	}
	var runs []run
	for id := range bottlenecksByWeek[lastWeek] {
		n := 0
		for i := len(weeks) - 1; i >= 0; i-- {
			if _, ok := bottlenecksByWeek[weeks[i]][id]; !ok {
				break
			}
			n++
		}
		if n >= stuckMinWeeks {
			runs = append(runs, run{id: id, weeks: n})
		}
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].id < runs[j].id })

	out := make([]*aidlcv1.MaturityMovementPoint, 0, len(runs))
	for _, r := range runs {
		b := bottlenecksByWeek[lastWeek][r.id]
		out = append(out, movementPoint(
			lastWeek, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_STUCK,
			cellKey{stage: b.GetStage(), level: b.GetLevel()}, int32(r.weeks), r.id, "",
		))
	}
	return out
}

// deriveHistoryStarts emits the series' single start-of-history line when the
// reported axis's early weeks predate the history, or the history is too short
// yet to read movement over. Returns nil when neither condition holds. The history
// boundary is compared against the axis's first week, not the caller's from: the
// twelve-week cap can drop from's own week, and a history start in a week the
// series does not show leaves every reported week with history.
//
// The published-week count is taken over weeks (the requested [from, to] range)
// rather than publishedWeeks' own key set, because publishedWeeks may also carry
// a baseline week the caller passed from before the range: that week is not part
// of what the caller asked for, so it must never count toward "too little history".
func deriveHistoryStarts(
	weeks []time.Time,
	publishedWeeks map[time.Time]bool,
	from, historyFrom time.Time,
) *aidlcv1.MaturityMovementPoint {
	// weeks (the trimmed, reported axis) can be empty when no week in the whole
	// requested range has published yet — trimAxis trims a trailing gap, and every
	// week is one here. That is exactly "too little history to read movement over,"
	// so the baseline point still anchors to the requested range's own first
	// calendar week rather than going silent for lack of a reported axis to anchor to.
	axisStart := isoWeekMonday(from)
	if len(weeks) > 0 {
		axisStart = weeks[0]
	}
	inRangePublished := 0
	for _, wk := range weeks {
		if publishedWeeks[wk] {
			inRangePublished++
		}
	}
	switch {
	case !historyFrom.IsZero() && historyFrom.After(axisStart):
		return movementPoint(axisStart, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_HISTORY_STARTS,
			cellKey{}, 0, "", historyFrom.UTC().Format(isoDay))
	case inRangePublished < 2:
		return movementPoint(axisStart, aidlcv1.MaturityMovementKind_MATURITY_MOVEMENT_KIND_HISTORY_STARTS,
			cellKey{}, 0, "", "")
	default:
		return nil
	}
}

// movementPoint assembles one point; weekStart is formatted as an ISO calendar date.
func movementPoint(
	weekStart time.Time,
	kind aidlcv1.MaturityMovementKind,
	key cellKey,
	weeksHeld int32,
	bottleneckID, historyFrom string,
) *aidlcv1.MaturityMovementPoint {
	return &aidlcv1.MaturityMovementPoint{
		WeekStart:    weekStart.UTC().Format(isoDay),
		Kind:         kind,
		Stage:        key.stage,
		Level:        key.level,
		WeeksHeld:    weeksHeld,
		BottleneckId: bottleneckID,
		HistoryFrom:  historyFrom,
	}
}
