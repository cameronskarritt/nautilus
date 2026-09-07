#!/usr/bin/env bash
set -euo pipefail

# The development server creates the nautilus namespace on every startup.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"

echo "waiting for Temporal and its development namespace..."
docker compose up -d --wait --wait-timeout 60 temporal
docker compose exec -T temporal temporal operator namespace describe \
  --namespace nautilus --command-timeout 5s >/dev/null
echo "✓ Temporal namespace ready: nautilus"
