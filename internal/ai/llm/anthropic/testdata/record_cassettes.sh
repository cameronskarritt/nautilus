#!/bin/bash
# Script to record Anthropic test cassettes
# Requires ANTHROPIC_API_KEY to be set

set -e

cd "$(dirname "$0")/../.."
BASE="internal/llm/anthropic/testdata"

echo "Recording Anthropic cassettes..."

# Non-streaming text
echo "Recording completion_text.jsonl..."
go run ./cmd/llm-test --provider anthropic --scenario text --record "$BASE/completion_text.jsonl"

# Non-streaming tool calls
echo "Recording completion_tool_calls.jsonl..."
go run ./cmd/llm-test --provider anthropic --scenario tool_calls --record "$BASE/completion_tool_calls.jsonl"

# Streaming text
echo "Recording stream_text.jsonl..."
go run ./cmd/llm-test --provider anthropic --scenario text --stream --record "$BASE/stream_text.jsonl"

# Streaming tool calls
echo "Recording stream_tool_calls.jsonl..."
go run ./cmd/llm-test --provider anthropic --scenario tool_calls --stream --record "$BASE/stream_tool_calls.jsonl"

echo "Done recording Anthropic cassettes!"
