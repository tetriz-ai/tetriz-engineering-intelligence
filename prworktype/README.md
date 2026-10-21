# prworktype

## Contents

- [What it does](#what-it-does) lists the entry points and how they fit together.
- [Quick start](#quick-start) classifies one pull request against a running model server.
- [Usage](#usage) shows the classifier and the lower-level client in complete programs.
- [Use with tetriz-connectors](#use-with-tetriz-connectors) maps a connector's pull-request record onto `Input`.
- [Reference](#reference) gives the exact cascade rules, defaults and buckets.
- [Before you rely on it](#before-you-rely-on-it) lists the limits to know before shipping.
- [Project](../README.md#project), at the repository root, covers versioning, tests, contributing, security and the license.

## What it does

`prworktype` labels a pull request on two axes from its **title** and **branch**:

- **type** (always resolved): `feature, bugfix, refactor, chore-deps, test, docs, performance`
- **domain** (nullable): `infra-devops, data-pipeline, observability, security-compliance, ui-design, ai-ml`

It resolves each axis through a three-layer cascade — a deterministic rule, then a fast embedding match, then an LLM only for what the embedding can't call confidently:

```
Layer 0  rules       bot author → chore-deps, cherry-pick        (deterministic)
Layer 1  embedding   MiniLM centroids, confident-or-abstain      (fast path)
Layer 2  LLM         disambiguates what the embedding can't call (accuracy path)
```

Unlike the other engines in this repository, **`prworktype` is not pure-Go and not offline.** The embedding model (MiniLM) and the LLM run in the companion Python service [`model-service`](../model-service/README.md); this Go package is a thin client that orchestrates the cascade (the rules and the routing thresholds run in Go) and calls that service over HTTP for the two model layers.

### How it fits together

```mermaid
flowchart LR
    pr["A PR's title + branch<br/>(+ optional author, files, body)"] --> clf["prworktype.Classifier.Classify"]
    clf -->|"Layer 0"| rules["rules (in Go)"]
    clf -->|"Layer 1 / 2 over HTTP"| ms["model-service model server<br/>/embed-classify · /llm-classify"]
    ms --> model["MiniLM centroids + your LLM"]
    clf -->|Classification| out["type + domain,<br/>each with its method"]
```

### What it does not do

- It does not run the model. A confident-but-absent answer is an error-free abstention; a model server that is unreachable, 5xx or malformed makes `Classify` return an error — it is a client, not the engine.
- It does not host an LLM. The LLM layer is enabled per request (`UseLLM`) and served by `model-service`, which brings its own provider (Gemini). With the LLM disabled, ambiguous rows abstain rather than guess.
- It does not read a diff, files or a body itself; you pass whatever signals you have on `Input`.

## Quick start

**Before anything below.** This is the one engine here that is not self-contained, and the
setup is not small:

| You need | Why | Cost |
|---|---|---|
| Go 1.25.2+ | to run this package | — |
| Python 3.10+ and Docker | to build and serve the model | — |
| The model bundle, built once | the server refuses to start without it | **~1 GB of downloads** (PyTorch, then MiniLM) and anywhere from 10 to 60 minutes on a first run |
| A Gemini API key | **only** for the LLM layer | per call, to your provider |

Build the bundle first, following [model-service's Setup](../model-service/README.md#setup-once).
Until that is done, nothing below will start.

**You do not need the API key to try this.** Pass `false` instead of `true` below, or drop
`--llm` from the CSV runner, and the engine runs embedding-only: confident rows are labelled
and ambiguous ones abstain, with no provider and no cost. The examples here switch the LLM
on because that is the full cascade — it is not a requirement.

Then start the model server and classify:

```sh
cd model-service && docker compose up model-server    # serves :8000
```

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/prworktype"
)

func main() {
	clf := prworktype.NewClassifier(prworktype.NewModelClient("http://localhost:8000"), true /* use LLM */)
	got, err := clf.Classify(context.Background(), prworktype.Input{
		Title:  "fix: null deref on empty connector response",
		Branch: "fix/npe-connector",
	})
	if err != nil {
		log.Fatal(err) // the model server was unreachable, 5xx or malformed
	}
	fmt.Println(got.Type.Bucket, got.Type.Method)     // bugfix embedding
	fmt.Println(got.Domain.Bucket, got.Domain.Method) // "" / a domain, by method
}
```

A ready CSV runner is in [`cmd/classify`](cmd/classify) — it reads a `title` column, with optional `branch`, `author_login`, `files` and `body`, and writes the buckets, methods and scores. `files` is one cell holding the paths separated by `|`, not a JSON array:

```bash
# From the repository root. Drop --llm to run embedding-only, with no key.
go run ./prworktype/cmd/classify --in prs.csv --out results.csv \
    --model-url http://localhost:8000 --llm --topk 0
```

## Usage

`Classifier` composes the whole cascade; `ModelClient` exposes the two model-server calls directly if you want to build your own routing.

```go
// The cascade, with the routing knobs exposed.
clf := &prworktype.Classifier{
	Client:    prworktype.NewModelClient("http://localhost:8000"),
	MinScore:  prworktype.DefaultMinScore,  // 0.35 — accept the embedding at/above this
	MinMargin: prworktype.DefaultMinMargin, // 0.08 — ...and this lead over the runner-up
	TopK:      0,                           // 0 = give the LLM the full taxonomy on escalation
	UseLLM:    true,
}
res, err := clf.Classify(ctx, prworktype.Input{Title: title, Branch: branch, AuthorLogin: who})
// err is non-nil only when the model server fails; res otherwise holds the classification.

// The lower-level client (build your own cascade).
mc := prworktype.NewModelClient("http://localhost:8000")
cands, _ := mc.EmbedClassify(ctx, "type", title, branch) // ranked candidates + top score + margin
```

### Use with tetriz-connectors

When: [tetriz-connectors](https://github.com/tetriz-ai/tetriz-connectors) syncs your pull
requests, and you want each one labelled.

Its `fetch_prs` operation emits one record per pull-request event. Decode the record's
base64 `payload` as JSON and fill `Input` from it:

| `Input` field | Where it comes from |
|---|---|
| `Title` | payload `title` |
| `Branch` | metadata `head_branch`, or payload `headRefName` — the **source** branch, not `base_ref_name` |
| `AuthorLogin` | payload `author.login`. Metadata's `pr_author_id` is the numeric id, not the handle the rules match on |
| `Body` | payload `body` |
| `Files` | metadata `files_changed`, a JSON array encoded as a string. Prefer it over payload `files.nodes[].path`, which stops at 100 files |

**Classify each pull request once.** One pull request emits up to four records —
`#opened`, `#merged`, `#closed` and `#snapshot` — and every sync run re-emits all of them
carrying the same payload. A loop over the raw stream therefore classifies the same pull
request up to four times, and with `UseLLM` on it pays the provider four times for one
answer. Either keep only the `#snapshot` record, as `toInput` below does, or deduplicate
on the key [tetriz-connectors' README](https://github.com/tetriz-ai/tetriz-connectors#readme)
gives. One sync of the connectors simulator emits 2,634 `fetch_prs` records for 900 pull
requests, so the filter is the difference between 900 classifications and 2,634.

```go
// Each line of the connector's records file is an envelope, not a record: the
// records are in its events. Decoding a line straight into prRecord silently
// yields an empty struct and classifies nothing.
type envelope struct {
	Events []prRecord `json:"events"`
}

// prRecord is the part of a connector record this needs. `payload` is the
// vendor's own JSON, base64-encoded.
type prRecord struct {
	ExternalID string            `json:"externalId"`
	Metadata   map[string]string `json:"metadata"`
	Payload    []byte            `json:"payload"` // encoding/json decodes base64 into []byte
}

type prPayload struct {
	Title       string `json:"title"`
	Body        string `json:"body"`
	HeadRefName string `json:"headRefName"`
	Author      struct {
		Login string `json:"login"`
	} `json:"author"`
}

// toInput maps one record. It returns false for a record that is not the snapshot,
// which is what keeps a pull request from being classified — and billed — four times.
func toInput(r prRecord) (prworktype.Input, bool) {
	if !strings.HasSuffix(r.ExternalID, "#snapshot") {
		return prworktype.Input{}, false
	}
	var p prPayload
	if err := json.Unmarshal(r.Payload, &p); err != nil {
		return prworktype.Input{}, false
	}
	branch := r.Metadata["head_branch"]
	if branch == "" {
		branch = p.HeadRefName
	}
	var files []string
	// files_changed is a JSON array held in a string, and is complete past 100 files.
	_ = json.Unmarshal([]byte(r.Metadata["files_changed"]), &files)

	return prworktype.Input{
		Title:       p.Title,
		Branch:      branch,
		AuthorLogin: p.Author.Login,
		Files:       files,
		Body:        p.Body,
	}, true
}
```

Remember:

- **Other vendors encode differently.** GitLab and Bitbucket records are snake_case REST
  rather than GitHub's camelCase GraphQL, so the payload struct above is GitHub's. See
  tetriz-connectors' records documentation for each vendor's shape.
- **A record with no title cannot be classified.** `Title` is the only required field;
  everything else sharpens the answer.

## Reference

**Cascade, per axis** (`Classify`):

- **type:** Layer 0 rule → if none, Layer 1 embedding (accept when `score ≥ MinScore` **and** `margin ≥ MinMargin`) → else Layer 2 LLM. With the LLM enabled **and a provider configured on the model server**, type always resolves (the LLM picks, or the embedding's top bucket as a best-effort fallback). With the LLM off — or switched on against a server that has no provider, which is the same thing from here — an unsure embedding **abstains**. The method says which layer decided.
- **domain:** Layer 1 embedding → else Layer 2 LLM, which may return **no domain** (nullable). With the LLM off and the embedding unsure, domain abstains.

**`Method`** on each `AxisResult` records which layer decided: `rule`, `embedding` (carries the cosine `Score`), `llm`, or `abstain` (no confident answer and no LLM).

**Thresholds:** `DefaultMinScore = 0.35`, `DefaultMinMargin = 0.08`. A `Classifier` with a non-positive `MinScore`/`MinMargin` (e.g. the zero value) falls back to these defaults rather than accepting every candidate. `TopK = 0` hands the LLM the full taxonomy on escalation, which scores best on the public centroid.

**Errors.** `Classify` returns an error when the model server is unreachable, answers 5xx, returns malformed JSON, or returns a bucket outside the taxonomy — never a silent abstention. A legitimate "no confident answer" is `MethodAbstain` with a nil error.

**Optional Postgres persistence (`--db`).** The `cmd/classify` runner can also write each result to a `classifications` table (created on first use; see `storage.go` for the schema). `lib/pq` defaults to `sslmode=require`, so a local server without TLS must spell it out, e.g. `--db "postgres://user:pass@localhost:5432/db?sslmode=disable"`.

**`Input`, in full.** `Title` and `Branch` are what the embedding reads; `AuthorLogin` feeds the Layer 0 rules; `Files` and `Body` are passed to the LLM layer only. All but `Title` are optional.

| Field | Type | What it is |
|---|---|---|
| `Title` | `string` | The pull request's title. |
| `Branch` | `string` | The **source (head)** branch, the one the work was written on — not the target. A target branch is `main` on nearly every pull request, so passing it silently degrades every label instead of erroring. |
| `AuthorLogin` | `string` | The author's provider handle, which the bot rules key on. |
| `Files` | `[]string` | Changed paths. LLM layer only. |
| `Body` | `string` | The description. LLM layer only. |

The buckets, the per-axis input recipe, and the model itself are owned by [`model-service`](../model-service/README.md) and the centroid bundle it serves — this package must point at a server built from the same bundle.

## Before you rely on it

- **It needs a running model server.** This is the one engine here that is not self-contained; nothing classifies without `model-service` reachable at the configured URL.
- **The embedding sees two short strings.** Layer 1 is given the title and branch only, and accuracy on those alone tops out below a 0.90 macro-F1. `Files` and `Body`, when you supply them, reach the **LLM layer** — so they sharpen the rows that escalate and do nothing for the rows the embedding calls confidently. See [`model-service`](../model-service/README.md) and its `datasets/RESULTS.md` for the honest, independently-labelled numbers.
- **The LLM layer costs per call and can be down.** With `UseLLM` off (or the provider unset on the server), ambiguous rows abstain instead of escalating — by design, not an error.
