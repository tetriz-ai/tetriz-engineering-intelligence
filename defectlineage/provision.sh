#!/usr/bin/env sh
# Copyright 2026 Workloom Solutions Private Limited
# SPDX-License-Identifier: MIT

# Creates the topic, the queue and the subscription the defect-lineage enqueue
# path runs on, inside the local AWS stand-in.
#
# The topic is a general fan-out: it carries every pull request a work-type
# classifier has labelled, not only bug fixes. The subscription is what narrows
# that to the fixes this pipeline traces, through a filter policy on the
# message's attributes. Filtering at the subscription rather than in the
# consumer is deliberate — it keeps one source of truth for "is this a merged
# bug fix", so a consumer can enqueue everything it receives instead of
# re-deciding and possibly disagreeing.
#
# It is safe to run again: every step either creates or returns what exists.
set -eu

ENDPOINT="${AWS_ENDPOINT_URL:-http://localhost:4566}"
REGION="${AWS_REGION:-us-east-1}"
TOPIC_NAME="${LINEAGE_TOPIC:-pr-worktype-classified}"
QUEUE_NAME="${LINEAGE_QUEUE:-defect-lineage-enqueue}"

aws() { command aws --endpoint-url "$ENDPOINT" --region "$REGION" "$@"; }

echo "provisioning against $ENDPOINT"

TOPIC_ARN=$(aws sns create-topic --name "$TOPIC_NAME" --output text --query TopicArn)
echo "topic:  $TOPIC_ARN"

QUEUE_URL=$(aws sqs create-queue --queue-name "$QUEUE_NAME" --output text --query QueueUrl)
QUEUE_ARN=$(aws sqs get-queue-attributes --queue-url "$QUEUE_URL" \
  --attribute-names QueueArn --output text --query 'Attributes.QueueArn')
echo "queue:  $QUEUE_URL"

# The queue must allow the topic to deliver to it; without this the subscription
# is created and every message is silently dropped.
aws sqs set-queue-attributes --queue-url "$QUEUE_URL" --attributes "$(cat <<JSON
{"Policy":"{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"Service\":\"sns.amazonaws.com\"},\"Action\":\"sqs:SendMessage\",\"Resource\":\"$QUEUE_ARN\",\"Condition\":{\"ArnEquals\":{\"aws:SourceArn\":\"$TOPIC_ARN\"}}}]}"}
JSON
)" >/dev/null

SUB_ARN=$(aws sns subscribe --topic-arn "$TOPIC_ARN" --protocol sqs \
  --notification-endpoint "$QUEUE_ARN" --output text --query SubscriptionArn)

# Raw delivery hands the queue the message body the publisher sent. Without it
# the body arrives wrapped in a notification envelope and a consumer has to
# unwrap it before it can read anything.
aws sns set-subscription-attributes --subscription-arn "$SUB_ARN" \
  --attribute-name RawMessageDelivery --attribute-value true

# The filter that makes this subscription about bug fixes. A message reaches the
# queue only when it carries both attributes with these values; everything else
# the topic fans out is never delivered and never billed.
aws sns set-subscription-attributes --subscription-arn "$SUB_ARN" \
  --attribute-name FilterPolicy \
  --attribute-value '{"work_type":["BUGFIX"],"state":["MERGED"]}'

echo "subscription: $SUB_ARN (filtered to merged bug fixes, raw delivery)"
echo
echo "export these to point a consumer at this stack:"
echo "  export AWS_ENDPOINT_URL=$ENDPOINT"
echo "  export LINEAGE_TOPIC_ARN=$TOPIC_ARN"
echo "  export LINEAGE_QUEUE_URL=$QUEUE_URL"
