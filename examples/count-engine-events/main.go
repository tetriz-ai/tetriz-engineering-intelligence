// Command count-engine-events plugs a custom Observer into the engine, which is
// how you wire its counters and explanatory events into your own metrics.
//
//	go run ./examples/count-engine-events
//
// Two people are attributed. Ada's sessions run through the gate, and one is
// refused. Grace's candidate store fails, so her session is left pending and the
// Observer is told why.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

// counter is an attribution.Observer that tallies every Count by event and
// labels, and keeps each Debug event. The engine calls it from several
// goroutines, so it locks.
type counter struct {
	mu     sync.Mutex
	counts map[string]int
	events []string
}

func (c *counter) Count(event string, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counts[event+labelString(labels)]++
}

func (c *counter) Observe(string, float64, map[string]string) {}

func (c *counter) Debug(event string, fields map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if event == "person_skipped" {
		c.events = append(c.events, fmt.Sprintf("%s person=%v reason=%v error=%q",
			event, fields["person_id"], fields["reason"], fields["error"]))
	}
}

func labelString(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, k+"="+v)
	}
	sort.Strings(parts)
	return "{" + strings.Join(parts, ",") + "}"
}

// candidates serves Ada's pull request and fails for everyone else.
type candidates struct{ pr *attributionv1.PRCandidate }

func (c candidates) ListAuthoredPRs(_ context.Context, _ string, authorIDs []string, _, _ time.Time) ([]*attributionv1.PRCandidate, error) {
	if authorIDs[0] != "ada" {
		return nil, errors.New("candidate store unavailable")
	}
	return []*attributionv1.PRCandidate{c.pr}, nil
}

func (candidates) ListReviewedPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, nil
}

type people struct{}

func (people) Resolve(_ context.Context, _, person string) (attribution.Identity, error) {
	return attribution.Identity{AuthorIDs: []string{person}}, nil
}

func main() {
	day := timestamppb.New(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))
	pr := &attributionv1.PRCandidate{
		PrKey: "acme/widgets#7", PrRepository: "acme/widgets", PrNumber: 7,
		HeadBranch: "feat/limiter", FilesChanged: []string{"limiter/bucket.go"}, MergedAt: day,
	}
	branch := func(src attributionv1.SignalSource) []*attributionv1.BranchSignal {
		return []*attributionv1.BranchSignal{{Name: "feat/limiter", Source: src}}
	}
	sessions := []*attributionv1.SessionEvidence{
		// Ada pushed the branch: the gate corroborates the match.
		{SessionId: "pushed", PersonId: "ada", OrgId: "my-org", IdeType: "IDE_TYPE_KIRO", StartDate: day,
			BranchSignals: branch(attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND)},
		// The branch only came from reading local git: the gate refuses it.
		{SessionId: "workspace-only", PersonId: "ada", OrgId: "my-org", IdeType: "IDE_TYPE_KIRO", StartDate: day,
			BranchSignals: branch(attributionv1.SignalSource_SIGNAL_SOURCE_LOCAL_GIT_WINDOWED)},
		// Grace's candidates cannot be fetched: left pending, and reported.
		{SessionId: "store-down", PersonId: "grace", OrgId: "my-org", StartDate: day},
	}

	obs := &counter{counts: map[string]int{}}
	engine := attribution.New(attribution.Params{Observer: obs}, candidates{pr}, people{})
	results, err := engine.Attribute(context.Background(), "my-org", sessions)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%d sessions in, %d results\n\n", len(sessions), len(results))
	keys := make([]string, 0, len(obs.counts))
	for k := range obs.counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%d  %s\n", obs.counts[k], k)
	}
	fmt.Println()
	for _, e := range obs.events {
		fmt.Println(e)
	}
}
