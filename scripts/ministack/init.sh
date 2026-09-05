#!/usr/bin/env bash
set -euo pipefail

# init.sh — bootstrap MiniStack resources for local development.
# Invoked manually via scripts/setup-env.

ENDPOINT="http://localhost:4566"
REGION="us-east-1"

export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION="$REGION"

echo "bootstrapping MiniStack resources..."

# S3
aws --endpoint-url="$ENDPOINT" s3 mb "s3://nautilus-dev" 2>/dev/null || true
echo "✓ S3 bucket: nautilus-dev"

# SES — verify a sender identity so emails don't get rejected
aws --endpoint-url="$ENDPOINT" ses verify-email-identity --email-address "noreply@localhost" 2>/dev/null || true
echo "✓ SES verified identity: noreply@localhost"

echo "✓ MiniStack bootstrap complete"
