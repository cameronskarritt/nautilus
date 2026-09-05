package main

import (
	"log"
	"net/http"

	"nautilus/internal/ai/llm"
	"nautilus/internal/ai/llm/openai"
	"nautilus/internal/config"
	"nautilus/internal/enums"
	"nautilus/internal/optional"
)

func createOpenAIClient(transport http.RoundTripper, isReplay, thinking bool) (llm.Client, enums.Model, error) {
	apiKey := config.Get[string]("OPENAI_API_KEY")
	if apiKey == "" && !isReplay {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	opts := &openai.RequestOptions{
		Endpoint:        optional.Set(openai.ResponsesEndpoint),
		UseResponsesAPI: true,
	}

	if thinking {
		opts.Reasoning = optional.Set(openai.Reasoning{
			Effort:  optional.Set("medium"),
			Summary: optional.Set("detailed"),
		})
	}

	client := openai.NewClient(opts).WithAPIKey(apiKey)

	if transport != nil {
		client = client.WithTransport(transport)
	}

	return client, openai.GPT5Mini, nil
}
