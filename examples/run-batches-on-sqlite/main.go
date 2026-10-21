// Command run-batches-on-sqlite runs the attribution engine in its batch runner
// over the SQLite reference store, against synthetic data, with no network, no
// credentials and no hosted service.
//
//	go run ./examples/run-batches-on-sqlite
//
// Three sessions each match by a different signal, one matches nothing, and one
// belongs to a person with no linked vendor account. A second run finds nothing
// to do, because every session is settled or waiting out its cooldown.
package main

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/runner"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/runner/sqlite"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

const orgID = "example-org"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()
	day := time.Now().UTC().Truncate(24 * time.Hour)

	store, err := sqlite.Open(":memory:")
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()

	if err := store.Put(ctx, orgID, sessions(day)...); err != nil {
		return err
	}

	engine := attribution.New(attribution.DefaultParams(), candidates(day), identities{})
	result, err := runner.New(engine, store, nil).RunBatch(ctx, orgID, 100)
	if err != nil {
		return err
	}

	abstained := result.Breakdown.UnmatchedNoMatchingPR + result.Breakdown.UnmatchedVCSNotLinked + result.Breakdown.UnmatchedIdentityErr
	fmt.Printf("first run: %d sessions, %d matched, %d abstained\n", result.Sessions, result.Processed-abstained, abstained)
	again, err := runner.New(engine, store, nil).RunBatch(ctx, orgID, 100)
	if err != nil {
		return err
	}
	fmt.Printf("second run: %d sessions\n\n", again.Sessions)
	return report(ctx, store)
}

// sessions are five shapes worth seeing: one naming its pull request outright,
// one that only shares a branch, one that only shares edited files, one with
// nothing in common, and one whose person has no vendor account at all.
func sessions(day time.Time) []*attributionv1.SessionEvidence {
	base := func(id, person string) *attributionv1.SessionEvidence {
		return &attributionv1.SessionEvidence{
			SessionId: id, PersonId: person, OrgId: orgID,
			StartDate: timestamppb.New(day),
			IdeType:   "IDE_TYPE_CLAUDE_CODE",
			RepoName:  "acme/widgets",
		}
	}

	named := base("names-its-pr", "ada")
	named.PrReferences = []*attributionv1.PRReference{{
		PrNumber: 101, PrRepository: "acme/widgets",
		Source: attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND,
	}}

	branched := base("shares-a-branch", "ada")
	branched.BranchSignals = []*attributionv1.BranchSignal{{
		Name: "feat/rate-limiter", Source: attributionv1.SignalSource_SIGNAL_SOURCE_SESSION_COMMAND,
	}}

	edited := base("shares-its-edits", "ada")
	edited.FilesEdited = []string{"limiter/bucket.go", "limiter/window.go", "limiter/clock.go"}

	unrelated := base("matches-nothing", "ada")
	unrelated.FilesEdited = []string{"docs/README.md"}

	unlinked := base("person-not-linked", "unknown-person")
	unlinked.FilesEdited = []string{"limiter/bucket.go"}

	return []*attributionv1.SessionEvidence{named, branched, edited, unrelated, unlinked}
}

// candidates stands in for a VCS connector.
type prs struct{ list []*attributionv1.PRCandidate }

func candidates(day time.Time) prs {
	pr := func(num int32, branch string, files ...string) *attributionv1.PRCandidate {
		return &attributionv1.PRCandidate{
			PrKey:        fmt.Sprintf("acme/widgets#PR-%d#merged", num),
			PrRepository: "acme/widgets",
			PrNumber:     num,
			HeadBranch:   branch,
			FilesChanged: files,
			MergedAt:     timestamppb.New(day),
		}
	}
	return prs{list: []*attributionv1.PRCandidate{
		pr(101, "feat/retries", "retry/backoff.go"),
		pr(102, "feat/rate-limiter", "limiter/bucket.go", "limiter/window.go"),
		pr(103, "feat/clocks", "limiter/bucket.go", "limiter/window.go", "limiter/clock.go"),
	}}
}

func (p prs) ListAuthoredPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return p.list, nil
}
func (p prs) ListReviewedPRs(context.Context, string, []string, time.Time, time.Time) ([]*attributionv1.PRCandidate, error) {
	return nil, nil
}

// identities stands in for whatever maps your people to their vendor accounts.
// "unknown-person" resolves to nothing, which is how a session abstains rather
// than guessing.
type identities struct{}

func (identities) Resolve(_ context.Context, _, personID string) (attribution.Identity, error) {
	if personID != "ada" {
		return attribution.Identity{}, nil
	}
	return attribution.Identity{AuthorIDs: []string{"ada-at-vendor"}}, nil
}

func report(ctx context.Context, store *sqlite.Store) error {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "SESSION\tOUTCOME\tPULL REQUEST\tCONFIDENCE")

	for _, s := range sessions(time.Now().UTC().Truncate(24 * time.Hour)) {
		got, err := store.Get(ctx, orgID, s.GetSessionId())
		if err != nil {
			return err
		}
		attr := got.GetStoredAttribution()
		// A session can link to several pull requests, returned in no fixed
		// order, so they are printed sorted by key.
		matches := slices.Clone(attr.GetMatches())
		slices.SortFunc(matches, func(a, b *attributionv1.PRMatch) int { return strings.Compare(a.GetPrKey(), b.GetPrKey()) })
		for _, m := range matches {
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%.2f\n",
				s.GetSessionId(), short(m.GetMethod().String()), m.GetPrKey(), m.GetConfidence())
		}
		if len(attr.GetMatches()) > 0 {
			continue
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t-\t-\n",
			s.GetSessionId(), short(attr.GetUnmatched().GetReason().String()))
	}
	return w.Flush()
}

// short trims the enum prefixes so the table reads as prose.
func short(s string) string {
	for _, prefix := range []string{
		"SESSION_TO_PR_MATCH_METHOD_", "SESSION_TO_PR_UNMATCHED_REASON_",
	} {
		if len(s) > len(prefix) && s[:len(prefix)] == prefix {
			return s[len(prefix):]
		}
	}
	return s
}
