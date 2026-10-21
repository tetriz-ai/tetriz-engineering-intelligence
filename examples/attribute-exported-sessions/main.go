// Command attribute-exported-sessions attributes local coding sessions to pull
// requests, reading the files two other tools write: agent-desk's
// local-session-extractor (`export --raw`) and tetriz-connectors' JSON-lines
// sink. Nothing here reaches the network, and no credential is involved.
//
//	go run ./examples/attribute-exported-sessions
//
// The eight sessions show the outcomes that matter: three author-side matches by
// a different signal each, one reviewer-side match, a session refused because its
// branch only came from the workspace, one with thin evidence, one that named a
// pull request closed without a merge, and one whose person has no linked vendor
// account.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/adapter/localsession"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/adapter/vcs"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/runner"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/attribution/runner/memory"
	attributionv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/attribution/v1"
)

const orgID = "example-org"

// exports maps each session export to the person whose machine wrote it. The
// extractor reads one machine and writes no person, so the caller supplies it.
var exports = []struct{ person, file string }{
	{"carol", "sessions-carol.jsonl"},
	{"frank", "sessions-frank.jsonl"},
}

func main() {
	if err := run(os.Stdout, "examples/attribute-exported-sessions/testdata"); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(w io.Writer, dir string) error {
	ctx := context.Background()

	sessions, err := loadExports(dir)
	if err != nil {
		return err
	}
	prs, err := loadPullRequests(filepath.Join(dir, "connector-records.jsonl"))
	if err != nil {
		return err
	}

	// The in-memory store: the runner's loop with no database. Swap in the
	// SQLite store (see run-batches-on-sqlite) to keep results across runs.
	store := memory.New()
	store.Put(orgID, sessions...)

	engine := attribution.New(attribution.DefaultParams(), prs, identities{})
	result, err := runner.New(engine, store, nil).RunBatch(ctx, orgID, 100)
	if err != nil {
		return err
	}

	abstained := result.Breakdown.UnmatchedNoMatchingPR + result.Breakdown.UnmatchedVCSNotLinked + result.Breakdown.UnmatchedIdentityErr
	_, _ = fmt.Fprintf(w, "%d merged pull requests from %d connector records\n", len(prs.merged), prs.read)
	_, _ = fmt.Fprintf(w, "%d sessions in, %d matched, %d abstained\n\n", result.Sessions, result.Processed-abstained, abstained)
	return report(w, store, sessions)
}

func loadExports(dir string) ([]*attributionv1.SessionEvidence, error) {
	var sessions []*attributionv1.SessionEvidence
	for _, e := range exports {
		got, err := loadSessions(filepath.Join(dir, e.file), e.person)
		if err != nil {
			return nil, err
		}
		sessions = append(sessions, got...)
	}
	return sessions, nil
}

// loadSessions reads one `export --raw` file.
func loadSessions(path, person string) ([]*attributionv1.SessionEvidence, error) {
	var out []*attributionv1.SessionEvidence
	err := eachLine(path, func(line []byte) error {
		var row localsession.Row
		if err := json.Unmarshal(line, &row); err != nil {
			return fmt.Errorf("decoding session row: %w", err)
		}
		sess, err := localsession.Convert(row, orgID, person)
		if err != nil {
			return err
		}
		out = append(out, sess)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return out, nil
}

// envelope is one line of the connector sink's file. The records are its events.
type envelope struct {
	Events []event `json:"events"`
}

// event is a connector record: protojson, so camelCase keys, a base64 payload
// (decoded here into the vendor's JSON) and RFC 3339 times.
type event struct {
	ConnectorID    string            `json:"connectorId"`
	OperationID    string            `json:"operationId"`
	OrganizationID string            `json:"organizationId"`
	ExternalID     string            `json:"externalId"`
	Payload        []byte            `json:"payload"`
	FetchedAt      time.Time         `json:"fetchedAt"`
	DataDate       *time.Time        `json:"dataDate"`
	Metadata       map[string]string `json:"metadata"`
}

// pullRequests is what the connector file holds for attribution: merged pull
// requests, and who reviewed each.
type pullRequests struct {
	read      int
	merged    []vcs.Record
	reviewers map[string]map[string]bool // pull request key, then reviewer id
}

// loadPullRequests reads GitHub's records from the connector sink's JSON-lines
// file; other code hosts name their operations and fields differently, and are
// skipped. A pull request gives an #opened and a #snapshot record, and a
// #merged record once merged or a #closed one if closed without a merge. Only
// #merged is a candidate, dated by the merge. Delivery is at least once, so one
// copy of each record is kept, the later fetch.
func loadPullRequests(path string) (*pullRequests, error) {
	type key struct{ org, op, id, date string }
	latest := map[key]event{}
	var order []key
	read := 0
	err := eachLine(path, func(line []byte) error {
		var env envelope
		if err := json.Unmarshal(line, &env); err != nil {
			return fmt.Errorf("decoding connector envelope: %w", err)
		}
		if len(env.Events) == 0 {
			return errors.New("a line with no events: the file must be the connector sink's envelopes")
		}
		for _, e := range env.Events {
			read++
			if !wanted(e) {
				continue
			}
			k := key{e.OrganizationID, e.OperationID, e.ExternalID, ""}
			if e.DataDate != nil {
				k.date = e.DataDate.UTC().Format(time.RFC3339Nano)
			}
			prev, seen := latest[k]
			if !seen {
				order = append(order, k)
			}
			if !seen || e.FetchedAt.After(prev.FetchedAt) {
				latest[k] = e
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	out := &pullRequests{read: read, reviewers: map[string]map[string]bool{}}
	for _, k := range order {
		e := latest[k]
		if e.OperationID == "fetch_prs" {
			out.merged = append(out.merged, vcs.Record{
				ExternalID: e.ExternalID, DataDate: *e.DataDate,
				Metadata: e.Metadata, Payload: e.Payload,
			})
			continue
		}
		var review struct {
			State string `json:"state"`
			User  *struct {
				ID int64 `json:"id"`
			} `json:"user"`
		}
		if err := json.Unmarshal(e.Payload, &review); err != nil {
			return nil, fmt.Errorf("%s: review %s: %w", path, e.ExternalID, err)
		}
		// A pending review was never submitted, and a deleted account has no user.
		if review.State == "PENDING" || review.User == nil {
			continue
		}
		reviewer := strconv.FormatInt(review.User.ID, 10)
		// A review on one's own pull request is authored work, not review.
		if reviewer == e.Metadata["pr_author_id"] {
			continue
		}
		pr := e.Metadata["pr_key"]
		if out.reviewers[pr] == nil {
			out.reviewers[pr] = map[string]bool{}
		}
		out.reviewers[pr][reviewer] = true
	}
	return out, nil
}

// wanted keeps the two GitHub operations attribution reads, so a file holding
// other operations' records does not hold them all in memory.
func wanted(e event) bool {
	if e.ConnectorID != "github" {
		return false
	}
	switch e.OperationID {
	case "fetch_prs":
		return e.Metadata["event_type"] == "merged" && e.DataDate != nil
	case "fetch_reviews":
		return true
	}
	return false
}

// ListAuthoredPRs returns the merged pull requests one of the person's accounts
// authored. The engine's windows narrow them by date.
func (p *pullRequests) ListAuthoredPRs(_ context.Context, _ string, authorIDs []string, _, _ time.Time) ([]*attributionv1.PRCandidate, error) {
	var mine []vcs.Record
	for _, pr := range p.merged {
		if slices.Contains(authorIDs, pr.Metadata["pr_author_id"]) {
			mine = append(mine, pr)
		}
	}
	return vcs.Convert(mine, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_AUTHORED), nil
}

// ListReviewedPRs returns the merged pull requests one of the person's accounts
// reviewed.
func (p *pullRequests) ListReviewedPRs(_ context.Context, _ string, reviewerIDs []string, _, _ time.Time) ([]*attributionv1.PRCandidate, error) {
	var mine []vcs.Record
	for _, pr := range p.merged {
		reviewed := p.reviewers[strings.TrimSuffix(pr.ExternalID, "#merged")]
		if slices.ContainsFunc(reviewerIDs, func(id string) bool { return reviewed[id] }) {
			mine = append(mine, pr)
		}
	}
	return vcs.Convert(mine, attributionv1.SessionParticipationKind_SESSION_PARTICIPATION_KIND_REVIEWED), nil
}

// eachLine calls fn with each non-empty line. Lines have no length cap: an
// exported row holds a whole conversation, and real ones pass several MiB.
func eachLine(path string, fn func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	r := bufio.NewReader(f)
	for n := 1; ; n++ {
		line, err := r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			if ferr := fn(line); ferr != nil {
				return fmt.Errorf("line %d: %w", n, ferr)
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("line %d: %w", n, err)
		}
	}
}

// identities links carol to her GitHub account, the numeric id the connector
// writes as pr_author_id and as a review's user id. Anyone else resolves to
// nothing, which is how a session abstains rather than guessing.
type identities struct{}

func (identities) Resolve(_ context.Context, _, person string) (attribution.Identity, error) {
	if person != "carol" {
		return attribution.Identity{}, nil
	}
	return attribution.Identity{AuthorIDs: []string{"3953824"}, ReviewerIdentityIDs: []string{"3953824"}}, nil
}

func report(w io.Writer, store *memory.Store, sessions []*attributionv1.SessionEvidence) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, "SESSION\tSIDE\tMETHOD\tCONFIDENCE\tEVIDENCE\tPULL REQUEST")
	for _, s := range sessions {
		attr := store.Get(orgID, s.GetSessionId()).GetStoredAttribution()
		// A session can link to more than one pull request, and the engine
		// returns them in no fixed order, so they are printed sorted by key.
		matches := slices.Clone(attr.GetMatches())
		slices.SortFunc(matches, func(a, b *attributionv1.PRMatch) int {
			return strings.Compare(a.GetPrKey(), b.GetPrKey())
		})
		for _, m := range matches {
			_, _ = fmt.Fprintf(tw, "%s\t%s\t%s\t%.2f\t%s\t%s\n",
				s.GetSessionId(),
				trim(m.GetParticipationKind().String(), "SESSION_PARTICIPATION_KIND_"),
				trim(m.GetMethod().String(), "SESSION_TO_PR_MATCH_METHOD_"),
				m.GetConfidence(),
				trim(m.GetEvidence().String(), "SESSION_TO_PR_MATCH_EVIDENCE_"),
				m.GetPrKey())
		}
		if len(attr.GetMatches()) == 0 {
			_, _ = fmt.Fprintf(tw, "%s\t-\tabstained: %s\t-\t-\t-\n",
				s.GetSessionId(), trim(attr.GetUnmatched().GetReason().String(), "SESSION_TO_PR_UNMATCHED_REASON_"))
		}
	}
	return tw.Flush()
}

func trim(s, prefix string) string { return strings.TrimPrefix(s, prefix) }
