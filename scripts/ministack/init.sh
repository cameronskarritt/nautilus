#!/usr/bin/env bash
set -euo pipefail

# init.sh — bootstrap MiniStack resources for local development.
# Invoked manually via scripts/setup-env.

ENDPOINT="http://localhost:4566"
REGION="us-east-1"
PREFIX="${SQS_QUEUE_PREFIX:-}"

export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION="$REGION"

echo "bootstrapping MiniStack resources..."

# S3
aws --endpoint-url="$ENDPOINT" s3 mb "s3://nautilus-dev" 2>/dev/null || true
echo "✓ S3 bucket: nautilus-dev"

# SQS — create the agent queue and its dead-letter queue.
DLQ_URL=$(aws --endpoint-url="$ENDPOINT" sqs create-queue \
  --queue-name "${PREFIX}agent-signals-dlq" \
  --query 'QueueUrl' --output text 2>/dev/null) || true
echo "✓ SQS queue: ${PREFIX}agent-signals-dlq"

DLQ_ARN=$(aws --endpoint-url="$ENDPOINT" sqs get-queue-attributes \
  --queue-url "$DLQ_URL" \
  --attribute-names QueueArn \
  --query 'Attributes.QueueArn' --output text)

aws --endpoint-url="$ENDPOINT" sqs create-queue \
  --queue-name "${PREFIX}agent-signals" \
  --attributes "{\"VisibilityTimeout\":\"300\",\"RedrivePolicy\":\"{\\\"deadLetterTargetArn\\\":\\\"${DLQ_ARN}\\\",\\\"maxReceiveCount\\\":\\\"5\\\"}\"}" \
  2>/dev/null || true
echo "✓ SQS queue: ${PREFIX}agent-signals (visibility=300s, DLQ after 5 receives)"

# SES — verify a sender identity so emails don't get rejected
aws --endpoint-url="$ENDPOINT" ses verify-email-identity --email-address "noreply@localhost" 2>/dev/null || true
echo "✓ SES verified identity: noreply@localhost"

echo "✓ MiniStack bootstrap complete"
