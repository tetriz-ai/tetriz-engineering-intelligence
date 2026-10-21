// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package fold

import (
	"fmt"
	"strings"
	"testing"

	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
)

// TestReviewReach_ReadsTheAIDeliveryPopulation pins that review reach is one
// population's share: the AI-authored delivery pull requests a person reviewed, over
// all AI-authored delivery pull requests. Here 100 are AI-authored, 40 by seat-linked
// authors (none reviewed) and 60 not, 15 of which were reviewed: the reach is 15 of
// 100, never 15 of the 40 seat-linked ones. Ten reviewed non-AI pull requests give code
// L3 the review control it reads, so the cell decides its share.
func TestReviewReach_ReadsTheAIDeliveryPopulation(t *testing.T) {
	var prs []MergedPR
	authored := map[string]struct{}{}
	for n := 0; n < 100; n++ {
		id := fmt.Sprintf("pr-%d", n)
		author := fmt.Sprintf("p%d", n%10)
		prs = append(prs, MergedPR{
			PRKey: id, AuthorIdentityID: &author, WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE,
			Paths: []string{"main.go"}, HasFileList: true, ReviewersKnown: true,
			AuthorSeatLinked: n < 40,
			SubmittedReview:  n >= 40 && n < 55,
		})
		authored[id] = struct{}{}
	}
	for n := 0; n < 10; n++ {
		author := fmt.Sprintf("p%d", n)
		prs = append(prs, MergedPR{
			PRKey: fmt.Sprintf("human-%d", n), AuthorIdentityID: &author, WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE,
			Paths: []string{"main.go"}, HasFileList: true, ReviewersKnown: true, SubmittedReview: true,
		})
	}
	folded := Fold(Facts{MergedPRs: prs, Attribution: Attribution{AuthoredPRKeys: authored}})
	res := aidlc.Score(rules.ForVersion("v1"), folded.Readings, folded.Org, nil)

	var reach *aidlcv1.MaturityBottleneck
	for _, b := range res.Profile.GetBottlenecks() {
		if b.GetId() == "review_reach" {
			reach = b
		}
	}
	if reach == nil {
		t.Fatal("review reach did not fire on 15 reviewed of 100")
	}
	if !strings.Contains(reach.GetBody(), "(15 of 100 merged") {
		t.Errorf("review reach body = %q, want it read over the 100 AI-authored delivery PRs", reach.GetBody())
	}
	if p := reach.GetProgress(); p.GetPopulation() != 100 || p.GetCurrent() != 15 {
		t.Errorf("progress = %d of %d, want 15 of 100", p.GetCurrent(), p.GetPopulation())
	}
}
