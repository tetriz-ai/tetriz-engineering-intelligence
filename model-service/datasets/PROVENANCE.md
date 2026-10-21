# Corpus provenance & redistribution rights

This document records the public corpus sources, redistribution rights and source
aliases. Per-source copyright and licence details are in
[`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md), with full licence texts in
[`licenses/`](licenses/).

## What is redistributed

Each public corpus row contains two strings from a public pull request —
its **title** and its **head branch name** — plus provenance columns (`source`,
`source_url`, `license`). Across the canonical corpora (`type_combined.csv`,
`domain_clean.csv`, `gold_type.csv`, `gold_domain.csv`) that is **14,617 rows**
drawn from **66 public repositories**.

What is **not** redistributed: no source code, diffs or file contents; no
customer or private-organisation data; no internal PR numbers, org names or
engineer logins. Rows do carry each PR's own public GitHub URL, and some branch
names contain the public contributor login — all already public.

## Licences and the basis for redistribution

Every source repository is permissively licensed. The row counts by licence:

| Licence | Sources | Rows |
|---|---|---|
| MIT | 31 | 7,805 |
| Apache-2.0 | 31 | 5,344 |
| BSD-3-Clause | 3 | 1,083 |
| ISC | 1 | 385 |

These are permissive licences with notice-retention conditions. The schedule links
complete, byte-preserved upstream LICENSE files under `licenses/LICENSE-*.txt`,
including every copyright-holder notice. Each Apache NOTICE found at the source
root is also bundled verbatim. Generic family texts remain available alongside
the source-specific copies. `licenses/sources.json` records the upstream Git blob
identity and SHA-256 of each copied LICENSE; refresh and offline validation retain
and check those copies.

Whether each contribution's title/branch text falls within the repository licence
is a rights question for the sign-off below. Public visibility or a current SPDX
label alone does not establish that conclusion. GitHub's [contribution terms](https://docs.github.com/en/site-policy/github-terms/github-terms-of-service#6-contributions-under-repository-license)
are a potential basis, subject to applicable historical terms and separate agreements.

## What was verified

- **Upstream SPDX cross-check.** On 2026-10-16 each source's licence was fetched
  from GitHub's licence API and compared against the label the corpora carry.
  All 66 matched; there were **no mismatches**.
- **No unvetted licence.** Every public row carries one of the four licences
  above; `corpus.attribution validate` fails the build on any other value.
- **Schedule ↔ corpora ↔ texts are in sync**, enforced offline in CI by
  `tests/test_attribution.py`.

This checks the stated licences against the upstream repositories. It does not
by itself constitute legal clearance to redistribute — that is the sign-off below.

## Recorded source aliases

On 2026-10-20 both paths in each pair resolved through GitHub's repository API to
the same repository ID, with `fork=false` and no parent. These mappings identify
the same repository, not a separate-fork licensing exception:

| Corpus source label | Current canonical repository | Repository ID | Rows |
|---|---|---:|---:|
| `tiangolo/fastapi` | `fastapi/fastapi` | 160919119 | 469 |
| `dbt-labs/dbt-core` | `dbt-labs/dbt` | 53548867 | 138 |
| `great-expectations/great_expectations` | `fivetran/great_expectations` | 103071520 | 115 |

The historical source labels and PR URLs remain unchanged. Current identity does
not establish every past transfer date. Three deterministic PR samples per alias
(minimum, median, maximum PR number) matched their committed title/branch and base
repository ID; sampled historical LICENSE bodies supported the recorded MIT or
Apache-2.0 terms. This is nine samples, not a full historical review of all 722
alias rows. The current canonical dbt path is `dbt-labs/dbt`; Great Expectations
resolves to the same repository identity under `fivetran`.

## Model (bundle) redistribution

The builder reads the two committed training CSVs. It includes their SHA-256
hashes and filenames in the manifest, plus per-axis licence counts. Generated
bundles omit `dataset.csv`; the original training CSVs remain in this source
repository for rebuilding. The SDK serves from the centroids and encoder.

Every generated bundle carries actual copies of this statement, the source
notice schedule, all referenced LICENSE/NOTICE files, and the encoder's Apache
licence/model card/pinned revision. Paths resolve within the bundle and the
manifest pins every packaged notice hash. Validation compares these against the
reviewed source files, so deleting a notice or updating only its manifest hash
cannot pass. See [`../BUILD.md`](../BUILD.md) for reproducible source inputs and
commands. Packaging is not legal approval, and a custom/private corpus needs its
own provenance review.

## Release authorization

Mohit Patni, release owner, approved redistribution of the source and committed
public corpus on **2026-10-20**, together with commercial classifier
training/inference using that corpus. Redistribution retains the applicable
third-party licences and notices documented above.

The approved corpus inventory SHA-256 is
`7f9e065ab43516570cc0fab489603cb9783f7cd7109bce118bf7ddfbd9e21e69`.
It is the SHA-256 of the UTF-8 JSON filename-to-file-SHA-256 map for the four
canonical CSVs above, with sorted keys, compact separators and no trailing newline.

This authorization covers the source and committed corpus. Generated model
artifacts require separate validation against the notice and provenance inventory.
Private or customer inputs, later corpus changes and uses outside this scope
require their own rights review.
