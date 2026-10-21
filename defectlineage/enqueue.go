// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

package defectlineage

import (
	"context"
	"errors"
	"fmt"

	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// Enqueue is the pipeline's entry point: it records that one merged bug fix
// needs tracing, so a later drain pass can claim and blame it.
//
// It writes a row and makes no provider call. That separation is what stops a
// backlog becoming a provider burst: a thousand fixes classified at once become
// a thousand rows, and the drain's batch size — not the arrival rate — decides
// how fast they are blamed.
//
// The write is insert-if-absent, so a re-delivered event, a replayed queue or a
// sweep re-scan never resets a fix that has already advanced.
func Enqueue(ctx context.Context, store Store, ev *defectlineagev1.BugfixClassifiedEvent) error {
	if store == nil {
		return errors.New("defectlineage: nil store")
	}
	ref, err := RefFromEvent(ev)
	if err != nil {
		return err
	}
	if err := store.Enqueue(ctx, ref); err != nil {
		return fmt.Errorf("enqueueing bug fix %s: %w", ref.BugFixPRID, err)
	}
	return nil
}

// RefFromEvent reads an event's identity fields.
//
// It rejects an event missing any of them rather than writing a partial row: a
// row with an empty organisation or fix id is unclaimable and unreadable, and a
// queue that keeps redelivering it would never make progress.
func RefFromEvent(ev *defectlineagev1.BugfixClassifiedEvent) (FixRef, error) {
	if ev == nil {
		return FixRef{}, errors.New("defectlineage: nil event")
	}
	ref := FixRef{
		OrganizationID: ev.GetOrganizationId(),
		ConnectorID:    ev.GetConnectorId(),
		PlatformRepoID: ev.GetPlatformRepoId(),
		BugFixPRID:     ev.GetBugFixPrId(),
	}
	var missing []string
	if ref.OrganizationID == "" {
		missing = append(missing, "organization_id")
	}
	if ref.ConnectorID == "" {
		missing = append(missing, "connector_id")
	}
	if ref.PlatformRepoID == "" {
		missing = append(missing, "platform_repo_id")
	}
	if ref.BugFixPRID == "" {
		missing = append(missing, "bug_fix_pr_id")
	}
	if len(missing) > 0 {
		return FixRef{}, fmt.Errorf("defectlineage: event is missing %v", missing)
	}
	return ref, nil
}
