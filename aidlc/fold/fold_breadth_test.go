// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"fmt"
	"testing"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
)

// TestFoldPresence_SetsStagePeople proves the presence fold carries the actual
// per-stage people set into the readings, not just the collapsed count: a stage with
// invocations carries exactly its distinct invokers, and a presence stage with none
// carries an empty (not nil) set.
func TestFoldPresence_SetsStagePeople(t *testing.T) {
	sm := map[string]aidlcv1.AidlcStage{
		"art-review": aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW,
	}
	invs := []HarnessInvocation{
		{PersonID: "p1", ArtifactID: "art-review", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL},
		{PersonID: "p2", ArtifactID: "art-review", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL},
		{PersonID: "p1", ArtifactID: "art-review", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL}, // duplicate: still one person
	}
	rd := aidlc.NewReadings()
	foldPresence(rd, 5, invs, sm)

	review := rd.StagePeople(aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW)
	if got := review.Len(); got != 2 {
		t.Fatalf("review stage people = %d, want 2 (p1, p2)", got)
	}
	for _, id := range []string{"p1", "p2"} {
		if _, ok := review[id]; !ok {
			t.Errorf("review stage people missing %q", id)
		}
	}

	// A presence stage with zero invocations carries an empty set, not an absent one.
	if got := rd.StagePeople(aidlcv1.AidlcStage_AIDLC_STAGE_DEPLOY); got == nil || got.Len() != 0 {
		t.Errorf("deploy stage people = %v, want an empty set (no invocations)", got)
	}

	// A non-presence stage (plan) never gets an entry at all.
	if plan := rd.StagePeople(aidlcv1.AidlcStage_AIDLC_STAGE_PLAN); plan != nil {
		t.Errorf("plan stage people = %v, want nil (plan carries no presence level)", plan)
	}
}

// TestActiveEngineers_UnionsInPersonSpace proves the active engineers union the
// session half and the merge authors mapped to person ids: a person seen through both
// counts once, an empty id is dropped, and an author identity the mapping does not
// know is left out rather than counted under a foreign id.
func TestActiveEngineers_UnionsInPersonSpace(t *testing.T) {
	prs := []MergedPR{
		{PRKey: "a#1", AuthorIdentityID: sptr("gh-p1")}, // p1 also has a session
		{PRKey: "a#2", AuthorIdentityID: sptr("gh-p3")}, // p3 authored only
		{PRKey: "a#3", AuthorIdentityID: sptr("gh-unknown")},
		{PRKey: "a#4", AuthorIdentityID: nil},
		{PRKey: "a#5", AuthorIdentityID: sptr("")},
	}
	mapping := map[string]string{"gh-p1": "p1", "gh-p3": "p3"}

	got := activeEngineers([]string{"p1", "p2", ""}, prs, mapping)

	want := []string{"p1", "p2", "p3"}
	if len(got) != len(want) {
		t.Fatalf("activeEngineers() has %d members, want %d: %v", len(got), len(want), got)
	}
	for _, id := range want {
		if !got[id] {
			t.Errorf("activeEngineers() missing %q", id)
		}
	}
	if got[""] || got["gh-unknown"] {
		t.Errorf("activeEngineers() carries an empty or unmapped id: %v", got)
	}
}

// TestActiveEngineers_TwoIdentitiesOnePersonCountsOnce pins that ten people who each
// have a session and two author identities (two code hosts) mapping to their own
// person id count as ten active engineers, not twenty or thirty.
func TestActiveEngineers_TwoIdentitiesOnePersonCountsOnce(t *testing.T) {
	var sessions []string
	var prs []MergedPR
	mapping := map[string]string{}
	for i := 0; i < 10; i++ {
		p := fmt.Sprintf("p%02d", i)
		sessions = append(sessions, p)
		for _, host := range []string{"hostA", "hostB"} {
			id := host + "-" + p
			mapping[id] = p
			prs = append(prs, MergedPR{PRKey: id, AuthorIdentityID: sptr(id)})
		}
	}
	if got := activeEngineers(sessions, prs, mapping); len(got) != 10 {
		t.Fatalf("activeEngineers() = %d people, want 10", len(got))
	}
}

// TestActiveEngineers_NoMappingLeavesAuthorsOut pins that a nil author mapping counts
// the session half alone.
func TestActiveEngineers_NoMappingLeavesAuthorsOut(t *testing.T) {
	prs := []MergedPR{{PRKey: "a#1", AuthorIdentityID: sptr("gh-p9")}}
	got := activeEngineers([]string{"p1"}, prs, nil)
	if len(got) != 1 || !got["p1"] {
		t.Fatalf("activeEngineers() = %v, want exactly {p1}", got)
	}
}

// TestFoldGolden_BreadthNeverChangesVerdicts is the strip-breadth golden: the same
// fixture scored once with the breadth carriers (stage people, active engineers)
// populated and once with them stripped must produce identical state, decided-by and
// band for every one of the 45 cells. Breadth is a disclosure outside the hashed
// ruleset and must never move a verdict.
func TestFoldGolden_BreadthNeverChangesVerdicts(t *testing.T) {
	scoreFixture := func(strip bool) *aidlc.Result {
		res := Fold(buildFixture())
		if strip {
			res.Org.ActiveEngineers = aidlc.PeopleSet{}
			for stage := range presenceStageSignal {
				res.Readings.SetStagePeople(stage, aidlc.PeopleSet{})
			}
		}
		return aidlc.Score(rules.ForVersion("v1"), res.Readings, res.Org, nil)
	}

	withBreadth := scoreFixture(false)
	stripped := scoreFixture(true)

	if len(withBreadth.Cells) != 45 || len(stripped.Cells) != 45 {
		t.Fatalf("want 45 scored cells each, got %d and %d", len(withBreadth.Cells), len(stripped.Cells))
	}

	// Sanity: breadth actually differs between the two runs, so the comparison below
	// is not vacuous.
	changed := false
	for i := range withBreadth.Profile.GetStages() {
		if withBreadth.Profile.GetStages()[i].GetBreadth() != stripped.Profile.GetStages()[i].GetBreadth() {
			changed = true
		}
	}
	if !changed {
		t.Fatalf("expected breadth to differ between the two runs; the fixture may not exercise breadth at all")
	}

	a, b := cellStates(withBreadth), cellStates(stripped)
	if len(a) != 45 || len(b) != 45 {
		t.Fatalf("cellStates: want 45 entries each, got %d and %d", len(a), len(b))
	}
	for k, v := range a {
		if b[k] != v {
			t.Errorf("cell %s: state/decided-by/band changed when breadth was stripped: %q -> %q", k, v, b[k])
		}
	}
}
