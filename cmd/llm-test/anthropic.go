package main

import (
	"log"
	"net/http"

	"nautilus/internal/ai/llm"
	"nautilus/internal/ai/llm/anthropic"
	"nautilus/internal/config"
	"nautilus/internal/enums"
	"nautilus/internal/optional"
)

func createAnthropicClient(transport http.RoundTripper, isReplay, thinking bool) (llm.Client, enums.Model, error) {
	apiKey := config.Get[string]("ANTHROPIC_API_KEY")
	if apiKey == "" && !isReplay {
		log.Fatal("ANTHROPIC_API_KEY environment variable is required")
	}

	opts := &anthropic.RequestOptions{}

	if thinking {
		opts.Thinking = optional.Set(anthropic.Thinking{
			BudgetTokens: 8000,
		})
		// max_tokens must be greater than budget_tokens
		opts.MaxTokens = optional.Set(16000)
	}

	client := anthropic.NewClient(opts).WithAPIKey(apiKey)

	if transport != nil {
		client = client.WithTransport(transport)
	}

	return client, anthropic.ClaudeSonnet45, nil
}
