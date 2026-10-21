// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

//go:build defectlineagequeue

package queue_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/queue"
	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/store/memory"
	defectlineagev1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// fakeQueue serves canned messages and records what was deleted. It is what the
// SQSReceiver interface exists for: the consumer's three-way decision is the
// subtlest logic in the file, and it needs no queue to exercise.
type fakeQueue struct {
	bodies  []string
	deleted []string
}

func (f *fakeQueue) ReceiveMessage(context.Context, *sqs.ReceiveMessageInput, ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	msgs := make([]sqstypes.Message, 0, len(f.bodies))
	for i, b := range f.bodies {
		msgs = append(msgs, sqstypes.Message{
			Body:          aws.String(b),
			ReceiptHandle: aws.String("handle-" + string(rune('a'+i))),
		})
	}
	f.bodies = nil
	return &sqs.ReceiveMessageOutput{Messages: msgs}, nil
}

func (f *fakeQueue) DeleteMessage(_ context.Context, in *sqs.DeleteMessageInput, _ ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	f.deleted = append(f.deleted, aws.ToString(in.ReceiptHandle))
	return &sqs.DeleteMessageOutput{}, nil
}

// unprocessableStore rejects every write as permanently unstorable, the shape a
// schema that cannot represent an identifier produces.
type unprocessableStore struct{ defectlineage.Store }

func (unprocessableStore) Enqueue(context.Context, defectlineage.FixRef) error {
	return defectlineage.ErrUnprocessable
}

// unreachableStore fails in a way that could pass later.
type unreachableStore struct{ defectlineage.Store }

func (unreachableStore) Enqueue(context.Context, defectlineage.FixRef) error {
	return errors.New("database unreachable")
}

const goodEvent = `{"organization_id":"00000000-0000-4000-8000-000000000001",` +
	`"connector_id":"github","platform_repo_id":"r1",` +
	`"bug_fix_pr_id":"00000000-0000-4000-8000-000000000002"}`

// TestTheConsumerDecidesRejectRetryOrEnqueue. The distinction is the whole
// point: a message retried forever blocks on an event no attempt can satisfy,
// and a message deleted too eagerly loses a fix during a database restart.
func TestTheConsumerDecidesRejectRetryOrEnqueue(t *testing.T) {
	for _, tc := range []struct {
		name        string
		body        string
		store       defectlineage.Store
		wantEnqueue int
		wantReject  int
		wantFail    int
		wantDeleted bool
		why         string
	}{
		{
			name: "a well-formed event is enqueued and deleted",
			body: goodEvent, store: memory.New(),
			wantEnqueue: 1, wantDeleted: true,
			why: "the ordinary path",
		},
		{
			name: "a malformed body is dropped, not retried",
			body: "{not json", store: memory.New(),
			wantReject: 1, wantDeleted: true,
			why: "no attempt can parse it, so redelivery would never end",
		},
		{
			name: "an event missing an identity field is dropped",
			body: `{"organization_id":"org","connector_id":"github"}`, store: memory.New(),
			wantReject: 1, wantDeleted: true,
			why: "a row with no fix id is unclaimable and unreadable",
		},
		{
			name: "a write the store can never accept is dropped",
			body: goodEvent, store: unprocessableStore{memory.New()},
			wantReject: 1, wantDeleted: true,
			why: "an identifier the schema cannot represent fails identically every time",
		},
		{
			name: "a transient store failure is left on the queue",
			body: goodEvent, store: unreachableStore{memory.New()},
			wantFail: 1, wantDeleted: false,
			why: "a database restart must not lose the fix",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := &fakeQueue{bodies: []string{tc.body}}
			c, err := queue.NewConsumer(queue.ConsumerParams{
				Client: q, QueueURL: "https://example.invalid/q", Store: tc.store,
			})
			require.NoError(t, err)

			stats, err := c.ReceiveOnce(context.Background())
			require.NoError(t, err)
			assert.Equal(t, 1, stats.Received)
			assert.Equal(t, tc.wantEnqueue, stats.Enqueued, tc.why)
			assert.Equal(t, tc.wantReject, stats.Rejected, tc.why)
			assert.Equal(t, tc.wantFail, stats.Failed, tc.why)

			if tc.wantDeleted {
				assert.Len(t, q.deleted, 1, "a settled message leaves the queue: "+tc.why)
			} else {
				assert.Empty(t, q.deleted, "a retryable message stays: "+tc.why)
			}
		})
	}
}

// TestAConsumedEventReachesTheQueueAsPending: the handler writes a row and makes
// no provider call, which is what stops a burst of classifications becoming a
// burst of provider traffic.
func TestAConsumedEventReachesTheQueueAsPending(t *testing.T) {
	ctx := context.Background()
	s := memory.New()
	q := &fakeQueue{bodies: []string{goodEvent}}
	c, err := queue.NewConsumer(queue.ConsumerParams{
		Client: q, QueueURL: "https://example.invalid/q", Store: s,
	})
	require.NoError(t, err)

	_, err = c.ReceiveOnce(ctx)
	require.NoError(t, err)

	claims, err := s.ClaimPendingBlame(ctx,
		"00000000-0000-4000-8000-000000000001", "github", 10, time.Minute)
	require.NoError(t, err)
	require.Len(t, claims, 1)
	assert.Equal(t, defectlineagev1.DefectLineageStage_DEFECT_LINEAGE_STAGE_PENDING, claims[0].Stage)
}

func TestNewQueueConsumerRequiresItsDependencies(t *testing.T) {
	_, err := queue.NewConsumer(queue.ConsumerParams{})
	require.Error(t, err)
	_, err = queue.NewConsumer(queue.ConsumerParams{Client: &fakeQueue{}})
	require.Error(t, err)
	_, err = queue.NewConsumer(queue.ConsumerParams{
		Client: &fakeQueue{}, QueueURL: "https://example.invalid/q",
	})
	require.Error(t, err)
}
