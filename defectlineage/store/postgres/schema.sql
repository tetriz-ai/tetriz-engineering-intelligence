-- Copyright 2026 Workloom Solutions Private Limited
-- SPDX-License-Identifier: MIT

-- Defect lineage keeps two tables. They relate only by the logical key
-- (organization_id, bug_fix_pr_id) and carry no foreign key between them, so a
-- candidate row and its lifecycle row can be written, re-driven and purged
-- independently.
--
-- Neither table filters by a retention window. A candidate whose commit predates
-- a configured window is precisely the row this feature exists to record, and a
-- windowed read would hide it — the signal would under-report silently instead of
-- degrading honestly. The window verdict is derived at read time from
-- candidate_committed_at, because a stored verdict would keep reporting a
-- candidate as out of window after a widened window brought it back in.

-- This file CREATES the schema; it does not migrate one. Every statement is
-- if-absent, so running it again on a database that already matches is a no-op
-- — but it will not add a column to a table that predates it. Upgrading a
-- populated database between versions of this package is the operator's own
-- migration, and the release notes name what changed.

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- bugfix_pr_defect_lineage holds evidence only: one row per (fix, culprit
-- commit). The culprit commit is the durable key — it always exists, where the
-- pull request that carried it may not.
CREATE TABLE IF NOT EXISTS bugfix_pr_defect_lineage (
    id                      uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id         uuid NOT NULL,
    connector_id            character varying(50) NOT NULL,
    platform_repo_id        character varying(64) NOT NULL,
    bug_fix_pr_id           uuid NOT NULL,

    -- The provider's own pull-request number for the culprit, or NULL when the
    -- walk resolved none: a squash-merged culprit often carries no link. Mapping
    -- this number to a caller's internal pull-request row is the caller's
    -- concern, so no internal identifier is stored.
    candidate_pr_number     integer,

    -- candidate_commit_sha is the normalised culprit hash.
    candidate_commit_sha    text NOT NULL,

    -- The committer date, canonicalised UTC. A provider's blame commonly carries
    -- the AUTHOR date instead, and the two diverge on exactly the rebase,
    -- cherry-pick and squash histories this table exists to trace, so a writer
    -- must take the committer date rather than whatever blame returned.
    candidate_committed_at  timestamp with time zone,

    -- The culprit's changed region in the culprit's OWN revision, captured at
    -- blame time so the structural scorer needs no re-blame. candidate_path
    -- differs from the anchor path only under a rename-aware reconstruction.
    -- candidate_line is NULL for a provider whose blame reports only the
    -- queried-revision position, which drifts; the scorer then derives the line
    -- from the culprit's own diff, or degrades to same-file coverage. Always used
    -- against the file at candidate_commit_sha, so it is drift-safe.
    candidate_path          text,
    candidate_line          integer,

    -- The culprit commit's author as the provider's blame reported it. Resolving
    -- an email to a person needs identity data this feature does not own, so the
    -- email is stored and the mapping is left to the caller.
    candidate_author_name   text,
    candidate_author_email  text,

    -- NULL until a scoring pass writes it. The blame drain never sets these:
    -- rescoring must not force a re-blame.
    confidence_score        numeric(4,3),
    score_evidence          jsonb,

    evidence_type           text NOT NULL,

    -- The scoring lease, the same shape the pipeline's blame claim uses: a token
    -- that rotates on every claim and a clock the lease is measured from. Two
    -- scoring workers would otherwise both read a candidate as unscored and both
    -- pay a provider to score it.
    scoring_claim_token     uuid,
    scoring_claimed_at      timestamp with time zone,

    created_at              timestamp with time zone DEFAULT now() NOT NULL,
    updated_at              timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at              timestamp with time zone,

    CONSTRAINT bugfix_pr_defect_lineage_pkey PRIMARY KEY (id)
);

-- One candidate per (fix, culprit commit): several anchors agreeing on one commit
-- collapse to a single row, which is what makes a re-drive of the same fix
-- converge instead of accumulating duplicates.
CREATE UNIQUE INDEX IF NOT EXISTS ux_lineage_fix_candidate
    ON bugfix_pr_defect_lineage (organization_id, bug_fix_pr_id, candidate_commit_sha)
    WHERE (deleted_at IS NULL);

-- For a reader's own queries, not this package's: nothing here scans by
-- organisation alone, and the unique index above already covers the
-- organisation-and-fix lookups. It is here because these tables are meant to be
-- queried directly, and an organisation-wide read is the first thing a reader
-- writes.
CREATE INDEX IF NOT EXISTS idx_lineage_org
    ON bugfix_pr_defect_lineage (organization_id);

-- Also for a reader: "which bug fixes trace back to this pull request" is the
-- question that inverts the table, and it has no index without this one.
CREATE INDEX IF NOT EXISTS idx_lineage_candidate
    ON bugfix_pr_defect_lineage (organization_id, candidate_pr_number)
    WHERE (candidate_pr_number IS NOT NULL AND deleted_at IS NULL);

-- The scoring work list. Ordered to end on id so a page is stable under
-- concurrent claims, and partial so an already-scored candidate costs nothing to
-- skip. The lease clock leads the key for the same reason the blame claim's
-- does: never-claimed rows first, then the oldest lease.
CREATE INDEX IF NOT EXISTS idx_lineage_unscored
    ON bugfix_pr_defect_lineage (organization_id, connector_id, scoring_claimed_at NULLS FIRST, id)
    WHERE (candidate_commit_sha <> '' AND confidence_score IS NULL AND deleted_at IS NULL);

-- defect_lineage_pipeline_state holds lifecycle only: exactly one row per fix.
--
-- The claim is compare-and-set rather than a lock: claim_token is a fresh uuid
-- each claim and last_attempted_at is the lease clock, so a claim that outlived
-- its lease is re-offered, and a stale worker's gated write matches no row. That
-- is what makes a worker dying mid-walk recoverable without a reaper.
CREATE TABLE IF NOT EXISTS defect_lineage_pipeline_state (
    id                  uuid DEFAULT gen_random_uuid() NOT NULL,
    organization_id     uuid NOT NULL,
    connector_id        character varying(50) NOT NULL,
    platform_repo_id    character varying(64) NOT NULL,
    bug_fix_pr_id       uuid NOT NULL,

    -- The fix's position in the pipeline. NOT NULL and always a real value.
    stage               text NOT NULL,
    -- Rotates on every claim; NULL on a never-claimed row.
    claim_token         uuid,
    -- The lease clock, stamped at claim time. A claimable stage whose lease
    -- predates the cutoff is re-offered; NULL, meaning never claimed, is also
    -- claimable.
    last_attempted_at   timestamp with time zone,
    -- How a RESOLVED fix ended — the axis an operator alerts on. NULL until the
    -- stage reaches RESOLVED.
    terminal_outcome    text,
    -- Counts claims, bumped on each one: the observable retry depth of a fix.
    attempt_count       integer DEFAULT 0 NOT NULL,

    created_at          timestamp with time zone DEFAULT now() NOT NULL,
    updated_at          timestamp with time zone DEFAULT now() NOT NULL,
    deleted_at          timestamp with time zone,

    CONSTRAINT defect_lineage_pipeline_state_pkey PRIMARY KEY (id)
);

-- Exactly one lifecycle row per fix. This is what makes Enqueue
-- insert-if-absent, so a re-delivered event or a sweep re-scan cannot reset a
-- fix that has already advanced.
CREATE UNIQUE INDEX IF NOT EXISTS ux_lineage_pipeline_fix
    ON defect_lineage_pipeline_state (organization_id, bug_fix_pr_id)
    WHERE (deleted_at IS NULL);

-- The claim scan, shaped to match the claim query exactly.
--
-- The stage filter lives in the index PREDICATE rather than its key columns, so
-- the remaining key is the query's own sort order and the scan can stop at the
-- batch size instead of sorting every claimable row. The sort is declared
-- NULLS FIRST because a never-claimed fix has no lease clock and must be
-- offered before any retry; a default ASC index puts those nulls last and could
-- not serve that order.
--
-- The predicate lists the blame stages rather than excluding the terminal one:
-- that also keeps out CANDIDATES_FOUND, which owes a score, not a second walk.
CREATE INDEX IF NOT EXISTS idx_lineage_pipeline_claim
    ON defect_lineage_pipeline_state (organization_id, connector_id, last_attempted_at NULLS FIRST, id)
    WHERE (deleted_at IS NULL AND stage IN (
        'DEFECT_LINEAGE_STAGE_PENDING',
        'DEFECT_LINEAGE_STAGE_PREPARED',
        'DEFECT_LINEAGE_STAGE_BLAMED'));
