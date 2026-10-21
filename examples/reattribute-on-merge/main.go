// Command reattribute-on-merge embeds the engine in a caller's own worker, with
// no runner, and re-attributes sessions when a pull request merges.
//
//	go run ./examples/reattribute-on-merge
//
// Before the merge, no session can match and all three abstain. When the pull
// request merges, the worker raises a re-attribution request on every session
// whose start date falls in ReattributionWindow. Only those reopen, and the one
// that pushed the branch now matches.
package main

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sort"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

const orgID = "my-org"

// worker is the caller's own loop: a map of sessions, a candidate source, and the
// engine. It shows what runner.RunBatch does, written out.
type worker struct {
	engine   *attribution.Engine
	params   attribution.Params
	sessions map[string]*attributionv1.SessionEvidence
}

// pending lists the sessions IsPending selects, in a stable order.
func (w *worker) pending() []*attributionv1.SessionEvidence {
	var out []*attributionv1.SessionEvidence
	for _, s := range w.sessions {
		if attribution.IsPending(s, w.params, time.Now()) {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].GetSessionId() < out[j].GetSessionId() })
	return out
}

// run attributes the pending sessions and stores what Reduce returns.
func (w *worker) run(ctx context.Context) (attribution.Reduction, error) {
	results, err := w.engine.Attribute(ctx, orgID, w.pending())
	if err != nil {
		return attribution.Reduction{}, err
	}
	red := attribution.Reduce(results)
	for i, s := range red.Sessions {
		w.sessions[s.GetSessionId()].StoredAttribution = red.Attributions[i]
	}
	return red, nil
}

// requestReattribution raises the marker on every session that started inside
// the merged pull request's window. Each bound is an inclusive date.
func (w *worker) requestReattribution(openedAt, mergedAt time.Time) []string {
	from, to := attribution.ReattributionWindow(openedAt, mergedAt)
	var raised []string
	for id, s := range w.sessions {
		d := s.GetStartDate().AsTime()
		if !d.Before(from) && !d.After(to) {
			s.ReattributionSeq++
			raised = append(raised, id)
		}
	}
	sort.Strings(raised)
	return raised
}

// pullRequests serves the merged pull request once it exists.
type pullRequests struct{ merged *attributionv1.PRCandidate }

func (p *pullRequests) ListAuthoredPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	if p.merged == nil {
		return nil, nil
	}
	return []*attributionv1.PRCandidate{p.merged}, nil
}

func (*pullRequests) ListReviewedPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, nil
}

type people struct{}

func (people) Resolve(context.Context, string, string) (attribution.Identity, error) {
	return attribution.Identity{AuthorIDs: []string{"ada"}}, nil
}

func main() {
	ctx := context.Background()
	// Dates are anchored on today because the engine stamps every attempt with the
	// current time, and nothing below prints a date.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	day := func(offset int) *timestamppb.Timestamp { return timestamppb.New(today.AddDate(0, 0, offset)) }
	pushed := []*attributionv1.BranchSignal{{Name: "feat/limiter", Source: attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND}}

	prs := &pullRequests{}
	params := attribution.Params{}
	w := &worker{
		engine: attribution.New(params, prs, people{}),
		params: params,
		sessions: map[string]*attributionv1.SessionEvidence{
			"pushed-the-branch": {SessionId: "pushed-the-branch", PersonId: "ada", StartDate: day(-3), BranchSignals: pushed},
			"unrelated-recent":  {SessionId: "unrelated-recent", PersonId: "ada", StartDate: day(-2)},
			"long-before":       {SessionId: "long-before", PersonId: "ada", StartDate: day(-45), BranchSignals: pushed},
		},
	}

	if _, err := w.run(ctx); err != nil {
		log.Fatal(err)
	}
	fmt.Println("before the merge:")
	report(w)

	prs.merged = &attributionv1.PRCandidate{
		PrKey: "acme/widgets#9", PrRepository: "acme/widgets", PrNumber: 9, HeadBranch: "feat/limiter",
		OpenedAt: day(-4), MergedAt: day(0),
		ParticipationKind: attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED,
	}
	raised := w.requestReattribution(today.AddDate(0, 0, -4), today)
	fmt.Printf("\nacme/widgets#9 merged: re-attribution requested for %v\n", raised)
	var reopened []string
	for _, s := range w.pending() {
		reopened = append(reopened, s.GetSessionId())
	}
	fmt.Printf("pending now: %v\n", reopened)

	red, err := w.run(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("serviced %d requests, withdrew %d matches\n\nafter the merge:\n", red.Reattributed, len(red.Withdrawals))
	report(w)
}

func report(w *worker) {
	ids := make([]string, 0, len(w.sessions))
	for id := range w.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		attr := w.sessions[id].GetStoredAttribution()
		matches := slices.Clone(attr.GetMatches())
		slices.SortFunc(matches, func(a, b *attributionv1.PRMatch) int { return strings.Compare(a.GetPrKey(), b.GetPrKey()) })
		for _, m := range matches {
			fmt.Printf("  %-18s %s %s %.2f\n", id, m.GetMethod(), m.GetPrKey(), m.GetConfidence())
		}
		if u := attr.GetUnmatched(); u != nil {
			fmt.Printf("  %-18s abstained: %s\n", id, u.GetReason())
		}
	}
}
