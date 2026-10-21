// Command match-one-session attributes one session to the pull request it names.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// pullRequests is where candidates come from: your own VCS data.
type pullRequests []*attributionv1.PRCandidate

func (p pullRequests) ListAuthoredPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return p, nil
}

func (pullRequests) ListReviewedPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, nil
}

// people maps a person to the vendor accounts they work under.
type people struct{}

func (people) Resolve(context.Context, string, string) (attribution.Identity, error) {
	return attribution.Identity{AuthorIDs: []string{"ada"}}, nil
}

func main() {
	day := timestamppb.New(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	prs := pullRequests{{PrKey: "acme/widgets#42", PrRepository: "acme/widgets", PrNumber: 42, MergedAt: day}}
	session := &attributionv1.SessionEvidence{
		SessionId: "s1", PersonId: "ada", StartDate: day,
		PrReferences: []*attributionv1.PRReference{{PrNumber: 42, PrRepository: "acme/widgets"}},
	}

	engine := attribution.New(attribution.Params{}, prs, people{})
	results, err := engine.Attribute(context.Background(), "my-org", []*attributionv1.SessionEvidence{session})
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range results[0].Attribution.GetMatches() {
		fmt.Println(m.GetPrKey(), m.GetMethod(), m.GetConfidence())
	}
	// Output: acme/widgets#42 SESSION_TO_PR_MATCH_METHOD_DIRECT_PR_REF 1
}
