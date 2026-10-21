// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package runner_test

import (
	"context"
	"fmt"
	"log"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/runner"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/runner/memory"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

type noPullRequests struct{}

func (noPullRequests) ListAuthoredPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, nil
}

func (noPullRequests) ListReviewedPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, nil
}

type unlinked struct{}

func (unlinked) Resolve(context.Context, string, string) (attribution.Identity, error) {
	return attribution.Identity{}, nil
}

// A batch over the in-memory store. The person has no linked vendor account, so
// the session abstains, and a second run finds it waiting out its cooldown.
func ExampleRunner_RunBatch() {
	store := memory.New()
	store.Put("my-org", &attributionv1.SessionEvidence{SessionId: "s1", PersonId: "ada", StartDate: timestamppb.Now()})
	r := runner.New(attribution.New(attribution.Params{}, noPullRequests{}, unlinked{}), store, nil)

	first, err := r.RunBatch(context.Background(), "my-org", 100)
	if err != nil {
		log.Fatal(err)
	}
	second, err := r.RunBatch(context.Background(), "my-org", 100)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(first.Processed, first.Breakdown.UnmatchedVCSNotLinked, second.Sessions)
	// Output: 1 1 0
}
