#!/usr/bin/env bash
set -euo pipefail

# init.sh — bootstrap MiniStack resources for local development.
# Invoked by scripts/setup-env and scripts/migrate-dev.

ENDPOINT="http://localhost:4566"
REGION="us-east-1"

export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_DEFAULT_REGION="$REGION"

echo "bootstrapping MiniStack resources..."

# SES — verify a sender identity so emails don't get rejected
aws --endpoint-url="$ENDPOINT" ses verify-email-identity --email-address "noreply@localhost" 2>/dev/null || true
echo "✓ SES verified identity: noreply@localhost"

# S3 — create the development bucket once.
if ! aws --endpoint-url="$ENDPOINT" s3api head-bucket --bucket nautilus-dev >/dev/null 2>&1; then
  aws --endpoint-url="$ENDPOINT" s3api create-bucket --bucket nautilus-dev >/dev/null
fi
echo "✓ S3 development bucket ready"

# KMS — retain a stable local user key across development restarts.
if ! aws --endpoint-url="$ENDPOINT" kms describe-key --key-id alias/nautilus/users >/dev/null 2>&1; then
  user_key_arn="$(aws --endpoint-url="$ENDPOINT" kms create-key \
    --description "Nautilus development user secrets" --query KeyMetadata.Arn --output text)"
  aws --endpoint-url="$ENDPOINT" kms create-alias \
    --alias-name alias/nautilus/users --target-key-id "$user_key_arn"
fi
echo "✓ KMS user key ready"

echo "✓ MiniStack bootstrap complete"
