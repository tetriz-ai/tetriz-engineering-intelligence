// Copyright 2026 Workloom Solutions Private Limited
// SPDX-License-Identifier: MIT

//go:build defectlineagequeue

package queue_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	snstypes "github.com/aws/aws-sdk-go-v2/service/sns/types"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tetriz-ai/tetriz-engineering-intelligence/defectlineage/queue"
)

// These tests run against the stack in defectlineage's docker-compose.yml. They are skipped
// unless it is pointed at, so `go test ./...` stays green with no services.
const (
	endpointEnv = "DEFECTLINEAGE_AWS_ENDPOINT"
	queueEnv    = "DEFECTLINEAGE_QUEUE_URL"
	topicEnv    = "DEFECTLINEAGE_TOPIC_ARN"
)

func awsClients(t *testing.T) (*sqs.Client, *sns.Client, string, string) {
	t.Helper()
	endpoint := os.Getenv(endpointEnv)
	queueURL := os.Getenv(queueEnv)
	topicARN := os.Getenv(topicEnv)
	if endpoint == "" || queueURL == "" || topicARN == "" {
		t.Skipf("set %s, %s and %s to run the transport tests", endpointEnv, queueEnv, topicEnv)
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("local-only", "local-only", "")),
	)
	require.NoError(t, err)
	sqsc := sqs.NewFromConfig(cfg, func(o *sqs.Options) { o.BaseEndpoint = aws.String(endpoint) })
	snsc := sns.NewFromConfig(cfg, func(o *sns.Options) { o.BaseEndpoint = aws.String(endpoint) })
	return sqsc, snsc, queueURL, topicARN
}

// TestAPublishedBugfixReachesTheDatabase drives the whole enqueue path: a
// classification event is published to the topic, the subscription's filter
// admits it, the consumer reads it, and the fix appears as a pending row.
func TestAPublishedBugfixReachesTheDatabase(t *testing.T) {
	ctx := context.Background()
	sqsc, snsc, queueURL, topicARN := awsClients(t)
	store, db := openStore(t)

	var fixID string
	require.NoError(t, db.QueryRow("SELECT gen_random_uuid()").Scan(&fixID))

	body := `{"organization_id":"` + orgID + `","connector_id":"github",` +
		`"platform_repo_id":"repo-queue","bug_fix_pr_id":"` + fixID + `"}`
	_, err := snsc.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(topicARN),
		Message:  aws.String(body),
		MessageAttributes: map[string]snstypes.MessageAttributeValue{
			"work_type": {DataType: aws.String("String"), StringValue: aws.String("BUGFIX")},
			"state":     {DataType: aws.String("String"), StringValue: aws.String("MERGED")},
		},
	})
	require.NoError(t, err)

	consumer, err := queue.NewConsumer(queue.ConsumerParams{
		Client: sqsc, QueueURL: queueURL, Store: store, WaitSeconds: 5,
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		stats, err := consumer.ReceiveOnce(ctx)
		if err != nil {
			return false
		}
		if stats.Enqueued == 0 {
			return false
		}
		return rowExists(t, db, fixID)
	}, 30*time.Second, time.Second, "the published fix must reach the database")

	var stage string
	require.NoError(t, db.QueryRow(
		`SELECT stage FROM defect_lineage_pipeline_state WHERE bug_fix_pr_id = $1`, fixID).Scan(&stage))
	assert.Equal(t, "DEFECT_LINEAGE_STAGE_PENDING", stage,
		"an enqueued fix waits for the drain and makes no provider call")
}

// TestAFilteredOutEventNeverArrives proves the filter is doing the work, not the
// consumer. A feature pull request is published to the same topic and must never
// reach the queue.
func TestAFilteredOutEventNeverArrives(t *testing.T) {
	ctx := context.Background()
	sqsc, snsc, queueURL, topicARN := awsClients(t)
	store, db := openStore(t)

	var fixID string
	require.NoError(t, db.QueryRow("SELECT gen_random_uuid()").Scan(&fixID))

	body := `{"organization_id":"` + orgID + `","connector_id":"github",` +
		`"platform_repo_id":"repo-queue","bug_fix_pr_id":"` + fixID + `"}`
	_, err := snsc.Publish(ctx, &sns.PublishInput{
		TopicArn: aws.String(topicARN),
		Message:  aws.String(body),
		MessageAttributes: map[string]snstypes.MessageAttributeValue{
			"work_type": {DataType: aws.String("String"), StringValue: aws.String("FEATURE")},
			"state":     {DataType: aws.String("String"), StringValue: aws.String("MERGED")},
		},
	})
	require.NoError(t, err)

	consumer, err := queue.NewConsumer(queue.ConsumerParams{
		Client: sqsc, QueueURL: queueURL, Store: store, WaitSeconds: 2,
	})
	require.NoError(t, err)
	// Drain whatever is queued; the feature event must not be among it.
	for i := 0; i < 3; i++ {
		if _, err := consumer.ReceiveOnce(ctx); err != nil {
			break
		}
	}
	assert.False(t, rowExists(t, db, fixID), "a feature pull request must never be enqueued")
}

func rowExists(t *testing.T, db *sql.DB, fixID string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRow(
		`SELECT count(*) FROM defect_lineage_pipeline_state WHERE bug_fix_pr_id = $1`, fixID).Scan(&n))
	return n > 0
}
