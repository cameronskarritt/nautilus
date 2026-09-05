#!/bin/bash
# Script to record OpenAI test cassettes
# Requires OPENAI_API_KEY to be set

set -e

cd "$(dirname "$0")/../.."
BASE="internal/llm/openai/testdata"

echo "Recording OpenAI cassettes..."

# Non-streaming text
echo "Recording completion_text.jsonl..."
go run ./cmd/llm-test --provider openai --scenario text --record "$BASE/completion_text.jsonl"

# Non-streaming tool calls
echo "Recording completion_tool_calls.jsonl..."
go run ./cmd/llm-test --provider openai --scenario tool_calls --record "$BASE/completion_tool_calls.jsonl"

# Streaming text
echo "Recording stream_text.jsonl..."
go run ./cmd/llm-test --provider openai --scenario text --stream --record "$BASE/stream_text.jsonl"

# Streaming tool calls
echo "Recording stream_tool_calls.jsonl..."
go run ./cmd/llm-test --provider openai --scenario tool_calls --stream --record "$BASE/stream_tool_calls.jsonl"

echo "Done recording OpenAI cassettes!"
