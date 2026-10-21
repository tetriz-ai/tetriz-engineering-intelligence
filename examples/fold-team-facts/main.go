// Command fold-team-facts folds a small team's raw facts into readings, checks the
// preconditions, and prints the published maturity map: each stage's headline, the
// constraints and the moves that clear them.
//
//	go run ./examples/fold-team-facts
package main

import (
	"fmt"
	"strings"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/fold"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/rules"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
)

func main() {
	facts := teamFacts()
	folded := fold.Fold(facts)
	rs := rules.ForVersion("v1")

	if _, withheld := aidlc.CheckPreconditions(rs, folded.Preconditions); withheld != aidlcv1.PreconditionKey_PRECONDITION_KEY_UNSPECIFIED {
		fmt.Println("withheld by", withheld)
		return
	}
	res := aidlc.Score(rs, folded.Readings, folded.Org, nil)

	fmt.Println(res.Profile.GetSummary())
	fmt.Println()
	for _, st := range res.Profile.GetStages() {
		fmt.Printf("%-13s frontier %-17s emerging %s\n", stageName(st.GetStage()), levelName(st.GetFrontier()), levelName(st.GetEmergingEdge()))
	}
	fmt.Println()
	for _, b := range res.Profile.GetBottlenecks() {
		fmt.Println("-", b.GetTitle())
		for _, r := range res.Profile.GetRecommendations() {
			if r.GetAddresses() == b.GetId() {
				fmt.Println("  move:", r.GetTitle())
			}
		}
	}
}

// teamFacts is a ten-person team over one window: who used which AI-harness skill,
// thirty merged pull requests with their reviews and AI authorship, and the tools
// their sessions used.
func teamFacts() fold.Facts {
	var roster, sessions []string
	authors := map[string]string{}
	for i := 1; i <= 10; i++ {
		p := fmt.Sprintf("person-%02d", i)
		roster = append(roster, p)
		sessions = append(sessions, p)
		authors["gh-"+p] = p
	}

	stageMap := map[string]aidlcv1.AidlcStage{
		"skill-review": aidlcv1.AidlcStage_AIDLC_STAGE_REVIEW,
		"skill-design": aidlcv1.AidlcStage_AIDLC_STAGE_TECH_SPEC,
	}
	var invs []fold.HarnessInvocation
	for i, p := range roster {
		invs = append(invs, fold.HarnessInvocation{PersonID: p, ArtifactID: "skill-review", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL})
		if i < 4 {
			invs = append(invs, fold.HarnessInvocation{PersonID: p, ArtifactID: "skill-design", Type: aidlcv1.HarnessArtifactType_HARNESS_ARTIFACT_TYPE_SKILL})
		}
	}

	attr := fold.Attribution{AuthoredPRKeys: map[string]struct{}{}, SessionPersonIDs: sessions}
	turns := map[string]int{}
	reviews := map[string]fold.PRReviewFacts{}
	var prs []fold.MergedPR
	for n := 0; n < 30; n++ {
		key := fmt.Sprintf("acme/api#%d", 100+n)
		author := "gh-" + roster[n%10]
		reviewed := n%3 != 0
		prs = append(prs, fold.MergedPR{
			PRKey: key, AuthorIdentityID: &author, AuthorSeatLinked: true,
			WorkType: aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE,
			Paths:    []string{"service/handler.go", "service/handler_test.go"}, HasFileList: true,
			SubmittedReview: reviewed, ReviewersKnown: true, BaseBranch: "main",
		})
		if n < 24 {
			attr.AuthoredPRKeys[key] = struct{}{}
			turns[key] = 6
			if n < 6 {
				turns[key] = 2
			}
		}
		humans := 0
		if reviewed {
			humans = 1
		}
		reviews[key] = fold.PRReviewFacts{Captured: true, HumanRows: humans, BotPresent: n%2 == 0, HumanNonAuthorApproved: reviewed}
	}

	toolUse := map[string][]string{"person-01": {"Read", "mcp__datadog__query"}, "person-02": {"Bash", "Grafana"}, "person-03": {"Edit"}}
	holders := 0
	for _, tools := range toolUse {
		for _, tool := range tools {
			if fold.IsObservabilityTool(tool) {
				holders++
				break
			}
		}
	}

	return fold.Facts{
		Roster: roster, Invocations: invs, StageMap: stageMap, MergedPRs: prs,
		Attribution: attr, HumanTurns: turns, ReviewFacts: reviews,
		Observability:    fold.ObservabilityRead{SeatHolders: holders, People: holders, ToolSessions: 40, Coverage: 0.6},
		DeclarationState: fold.DeclarationsRead,
		Declarations: fold.Declarations{ReviewPolicy: map[aidlcv1.PRWorkType]fold.ApprovalKind{
			aidlcv1.PRWorkType_PR_WORK_TYPE_FEATURE: fold.ApprovalHuman,
		}},
		AuthorPersonIDs: authors,
	}
}

func stageName(s aidlcv1.AidlcStage) string {
	return strings.ToLower(strings.TrimPrefix(s.String(), "AIDLC_STAGE_"))
}

func levelName(l aidlcv1.AidlcLevel) string {
	if l == aidlcv1.AidlcLevel_AIDLC_LEVEL_UNSPECIFIED {
		return "none"
	}
	return strings.TrimPrefix(l.String(), "AIDLC_LEVEL_")
}
