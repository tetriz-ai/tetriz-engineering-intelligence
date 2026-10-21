// Command attribute-across-promotions joins three engines on pull-request keys:
// attribution matches a session to the pull request it produced, branchlineage
// finds the promotion that carried that pull request to main, and the aidlc
// fold counts AI work shipped to main with and without that lineage.
//
//	go run ./examples/attribute-across-promotions
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/aidlc/fold"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	attributionvcs "github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/adapter/vcs"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage"
	lineagevcs "github.com/tetriz-ai/tetriz-engineering-intelligence/branchlineage/adapter/vcs"
	aidlcv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/aidlc/v1"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// One connector fetch: a feature merged into develop, then develop promoted to main.
var day = time.Date(2026, 4, 2, 0, 0, 0, 0, time.UTC)

var records = []attributionvcs.Record{
	{ExternalID: "acme/shop#PR-7#merged", DataDate: day.Add(9 * time.Hour),
		Metadata: map[string]string{"repo": "acme/shop", "head_branch": "feature/search", "base_ref_name": "develop"}},
	{ExternalID: "acme/shop#PR-8#merged", DataDate: day.Add(15 * time.Hour),
		Metadata: map[string]string{"repo": "acme/shop", "head_branch": "develop", "base_ref_name": "main"}},
}

// candidates serves the fetched pull requests to the attribution engine.
type candidates []*attributionv1.PRCandidate

func (c candidates) ListAuthoredPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return c, nil
}

func (candidates) ListReviewedPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, nil
}

type people struct{}

func (people) Resolve(context.Context, string, string) (attribution.Identity, error) {
	return attribution.Identity{AuthorIDs: []string{"ada"}}, nil
}

// branchRoles marks main as the shipped branch.
type branchRoles struct{}

func (branchRoles) RoleOf(_, branch string) fold.BranchRole {
	return fold.BranchRole{Shipped: branch == "main"}
}
func (branchRoles) IsProduction(_, branch string) bool { return branch == "main" }
func (branchRoles) Known() bool                        { return true }

func main() {
	ctx := context.Background()

	// 1. attribution: the session named pull request 7.
	authored := attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED
	engine := attribution.New(attribution.Params{}, candidates(attributionvcs.Convert(records, authored)), people{})
	session := &attributionv1.SessionEvidence{
		SessionId: "s1", PersonId: "ada", StartDate: timestamppb.New(day),
		PrReferences: []*attributionv1.PRReference{{PrNumber: 7, PrRepository: "acme/shop"}},
	}
	results, err := engine.Attribute(ctx, "my-org", []*attributionv1.SessionEvidence{session})
	if err != nil {
		log.Fatal(err)
	}
	direct := map[string]struct{}{}
	for _, m := range results[0].Attribution.GetMatches() {
		direct[m.GetPrKey()] = struct{}{}
		fmt.Println("attribution: session s1 produced", m.GetPrKey())
	}

	// 2. branchlineage: which promotion carried it to main.
	var lineageRecords []lineagevcs.Record
	for _, r := range records {
		lineageRecords = append(lineageRecords, lineagevcs.Record(r))
	}
	prs := lineagevcs.Convert(lineageRecords)
	g, err := branchlineage.Derive(prs)
	if err != nil {
		log.Fatal(err)
	}
	proj, err := branchlineage.Project(ctx, g, branchlineage.ProjectInput{
		Seeds:      branchlineage.TrackedSeeds(prs, func(_, b string) bool { return b == "main" }),
		Flows:      map[string][]branchlineage.Hop{"acme/shop": {{Source: "develop", Target: "main"}}},
		Attributed: direct,
	})
	if err != nil {
		log.Fatal(err)
	}
	withLineage := map[string]struct{}{}
	for k := range direct {
		withLineage[k] = struct{}{}
	}
	for _, c := range proj.Credits {
		withLineage[c.Carrier] = struct{}{}
		fmt.Println("branchlineage:", c.Carrier, "carried it to main:", c.Path)
	}

	// 3. aidlc fold: AI pull requests merged into a shipped branch.
	fmt.Println("aidlc: AI pull requests shipped to main, direct only:", shipped(prs, direct))
	fmt.Println("aidlc: AI pull requests shipped to main, with lineage:", shipped(prs, withLineage))
}

func shipped(prs []branchlineage.PR, ai map[string]struct{}) float64 {
	var merged []fold.MergedPR
	for _, p := range prs {
		repo, author := p.Repository, "ada"
		merged = append(merged, fold.MergedPR{
			PRKey: p.Key, BaseBranch: p.BaseBranch, RepositoryID: &repo,
			AuthorIdentityID: &author, AuthorSeatLinked: true,
		})
	}
	res := fold.Fold(fold.Facts{MergedPRs: merged, Attribution: fold.Attribution{AuthoredPRKeys: ai}, BranchRoles: branchRoles{}})
	for _, e := range res.Readings.All() {
		if e.Signal == aidlcv1.SignalKey_SIGNAL_KEY_AI_ATTRIBUTED_SHIPPED_BRANCH_PRS_MERGED && e.Reading.Value != nil {
			return *e.Reading.Value
		}
	}
	return 0
}
