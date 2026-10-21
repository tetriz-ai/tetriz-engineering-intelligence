// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

//go:build defectlineagequeue

// Package queue consumes published bug-fix events from SQS and enqueues each
// one as a fix for the pipeline to trace.
//
// It is behind the `defectlineagequeue` build tag because it carries the AWS
// SDK, which the engine does not need: the pipeline runs against any
// defectlineage.Store, and a queue is one way to feed it rather than a
// requirement. Without the tag the package is skipped and nothing here is
// compiled or downloaded into a build.

package queue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage"
	dlv1 "github.com/tetriz-ai/tetriz-engineering-intelligence/gen/defectlineage/v1"
)

// SQSReceiver is the part of the queue client this consumer uses. Taking an
// interface rather than the concrete client keeps the consumer testable without
// a queue.
type SQSReceiver interface {
	ReceiveMessage(ctx context.Context, in *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, in *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
}

// Consumer turns queued classification events into enqueued fixes.
//
// It does nothing else. The handler writes one row and makes no provider call,
// which is what stops a burst of classifications becoming a burst of provider
// traffic: a thousand events become a thousand rows, and the drain's batch size
// decides how fast they are blamed.
//
// The consumer trusts the subscription's filter and does not re-read the
// verdict. A message that reaches this queue is a merged bug fix by
// construction; re-deciding here would be a second source of truth that can
// disagree with the one that routed the message.
type Consumer struct {
	client   SQSReceiver
	queueURL string
	store    defectlineage.Store
	obs      defectlineage.Observer
	batch    int32
	waitSecs int32
}

// ConsumerParams configure a [Consumer].
type ConsumerParams struct {
	// Client receives and deletes messages. Required.
	Client SQSReceiver
	// QueueURL names the queue to drain. Required.
	QueueURL string
	// Store is where an enqueued fix is written. Required.
	Store defectlineage.Store
	// BatchSize is how many messages one receive asks for, capped at 10 by the
	// queue service. Zero means 10.
	BatchSize int32
	// WaitSeconds is how long a receive waits for a message before returning
	// empty. Zero means 20, the maximum, which is what makes an idle consumer
	// cost one request every 20 seconds instead of a tight poll.
	WaitSeconds int32
	// Observer receives the consumer's diagnostic events. Nil discards them.
	Observer defectlineage.Observer
}

// NewConsumer builds a consumer.
func NewConsumer(p ConsumerParams) (*Consumer, error) {
	if p.Client == nil {
		return nil, errors.New("wiring: ConsumerParams.Client is required")
	}
	if p.QueueURL == "" {
		return nil, errors.New("wiring: ConsumerParams.QueueURL is required")
	}
	if p.Store == nil {
		return nil, errors.New("wiring: ConsumerParams.Store is required")
	}
	batch := p.BatchSize
	if batch <= 0 || batch > 10 {
		batch = 10
	}
	wait := p.WaitSeconds
	if wait <= 0 || wait > 20 {
		wait = 20
	}
	return &Consumer{
		client: p.Client, queueURL: p.QueueURL, store: p.Store,
		obs: p.Observer, batch: batch, waitSecs: wait,
	}, nil
}

// ConsumeStats is what one receive-and-handle pass did.
type ConsumeStats struct {
	// Received is how many messages the pass read.
	Received int
	// Enqueued is how many fixes it wrote.
	Enqueued int
	// Rejected is how many messages were unreadable and deleted rather than
	// retried.
	Rejected int
	// Failed is how many were left on the queue for another attempt.
	Failed int
}

// ReceiveOnce drains one batch.
//
// A message that cannot be read at all — malformed, or missing an identity
// field — is deleted rather than retried, because redelivering it would block
// the queue forever on an event no attempt can satisfy. A message that fails for
// a reason that might pass later, such as the database being briefly
// unreachable, is left for redelivery.
func (c *Consumer) ReceiveOnce(ctx context.Context) (ConsumeStats, error) {
	var stats ConsumeStats
	out, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(c.queueURL),
		MaxNumberOfMessages: c.batch,
		WaitTimeSeconds:     c.waitSecs,
	})
	if err != nil {
		return stats, fmt.Errorf("receiving from %s: %w", c.queueURL, err)
	}
	stats.Received = len(out.Messages)

	for _, m := range out.Messages {
		switch c.handle(ctx, m) {
		case handleEnqueued:
			stats.Enqueued++
			c.delete(ctx, m)
		case handleRejected:
			stats.Rejected++
			c.delete(ctx, m)
		default:
			stats.Failed++
		}
	}
	return stats, nil
}

type handleResult int

const (
	handleFailed handleResult = iota
	handleEnqueued
	handleRejected
)

func (c *Consumer) handle(ctx context.Context, m types.Message) handleResult {
	var ev dlv1.BugfixClassifiedEvent
	body := aws.ToString(m.Body)
	if err := protojson.Unmarshal([]byte(body), &ev); err != nil {
		c.debug("event_unreadable", map[string]any{"error": err.Error()})
		return handleRejected
	}
	if err := defectlineage.Enqueue(ctx, c.store, &ev); err != nil {
		// Two kinds of failure, and they need opposite handling. An event the
		// store can never accept — a missing identity field, or an identifier its
		// schema cannot represent — fails identically on every redelivery, so
		// retrying it means redelivering it forever; it is dropped. Anything else
		// is transient and left on the queue.
		if errors.Is(err, defectlineage.ErrUnprocessable) {
			c.debug("event_unprocessable", map[string]any{"error": err.Error()})
			return handleRejected
		}
		if _, refErr := defectlineage.RefFromEvent(&ev); refErr != nil {
			c.debug("event_incomplete", map[string]any{"error": refErr.Error()})
			return handleRejected
		}
		c.debug("enqueue_failed", map[string]any{"error": err.Error()})
		return handleFailed
	}
	return handleEnqueued
}

func (c *Consumer) delete(ctx context.Context, m types.Message) {
	if _, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl: aws.String(c.queueURL), ReceiptHandle: m.ReceiptHandle,
	}); err != nil {
		// The fix is already written, and enqueueing is insert-if-absent, so a
		// redelivery of this message is harmless.
		c.debug("delete_failed", map[string]any{"error": err.Error()})
	}
}

func (c *Consumer) debug(event string, fields map[string]any) {
	if c.obs != nil {
		c.obs.Debug(event, fields)
	}
}

// Run drains the queue until ctx is cancelled.
//
// A receive failure pauses briefly rather than spinning, so a queue that is
// unreachable costs a retry every second instead of a hot loop.
func (c *Consumer) Run(ctx context.Context) error {
	const backoff = time.Second
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if _, err := c.ReceiveOnce(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.debug("receive_failed", map[string]any{"error": err.Error()})
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
		}
	}
}
