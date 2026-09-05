package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"time"

	"nautilus/internal/ai/llm"
	"nautilus/internal/config"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/testutil"
)

func main() {
	// Parse flags
	provider := flag.String("provider", "openai", "LLM provider to use (openai, anthropic)")
	stream := flag.Bool("stream", false, "Use streaming completion")
	thinking := flag.Bool("thinking", false, "Show thinking/reasoning tokens")
	record := flag.String("record", "", "Record interactions to cassette file")
	replay := flag.String("replay", "", "Replay interactions from cassette file")
	scenario := flag.String("scenario", "text", "Test scenario: text, tool_calls")
	flag.Parse()

	config.LoadDotenv()

	fmt.Printf("Provider: %s\n", *provider)
	fmt.Printf("Scenario: %s\n", *scenario)
	fmt.Printf("Stream: %v\n", *stream)

	// Set up transport based on mode
	var transport http.RoundTripper
	var cassette *testutil.Cassette

	if *record != "" && *replay != "" {
		log.Fatal("Cannot use both --record and --replay")
	}

	if *record != "" {
		fmt.Printf("Recording to cassette: %s\n", *record)
		var err error
		cassette, err = testutil.NewCassette(*record)
		if err != nil {
			log.Fatalf("Failed to create cassette: %v", err)
		}
		defer cassette.Close()
		transport = httputil.NewRecordingTransport(http.DefaultTransport, cassette)
	}

	if *replay != "" {
		fmt.Printf("Replaying from cassette: %s\n", *replay)
		var err error
		cassette, err = testutil.LoadCassette(*replay)
		if err != nil {
			log.Fatalf("Failed to load cassette: %v", err)
		}
		transport = httputil.NewReplayTransport(cassette, httputil.WithFastForward(), httputil.WithConsume())
	}

	// Create client based on provider
	client, model, err := createClient(enums.Provider(*provider), transport, *replay != "", *thinking)
	if err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	fmt.Println("Using 120 second timeout...")

	request := createRequest(model, *scenario)

	// Test streaming completion
	if *stream {
		fmt.Println("Testing streaming completion...")
		fmt.Println()

		streamResp, err := client.StreamCompletion(ctx, request)
		if err != nil {
			log.Fatalf("error starting stream: %v", err)
		}

		inThinking := false
		printedResponse := false
		for token := range streamResp.Tokens() {
			switch token.TokenType() {
			case enums.TokenTypeReasoning:
				if *thinking {
					if !inThinking {
						fmt.Print("Thinking: ")
						inThinking = true
					}
					fmt.Print(token.Content())
				}
			case enums.TokenTypeText:
				if inThinking {
					fmt.Println()
					fmt.Println()
					inThinking = false
				}
				if !printedResponse {
					fmt.Print("Response: ")
					printedResponse = true
				}
				fmt.Print(token.Content())
			case enums.TokenTypeError:
				fmt.Printf("\nError: %v\n", token.Content())
			case enums.TokenTypeUsage:
				// Usage info at the end
			}
		}
		fmt.Println()
		fmt.Println()

		// Print metrics
		if m, ok := streamResp.(llm.Metricer); ok {
			metrics := m.Metrics()
			fmt.Printf("Metrics:\n")
			fmt.Printf("  TTFT:             %v\n", metrics.TTFT())
			fmt.Printf("  Thinking:         %v\n", metrics.ThinkingDuration())
			fmt.Printf("  Total:            %v\n", metrics.TotalDuration())
			fmt.Printf("  Tokens/sec:       %.1f\n", metrics.TokensPerSecond())
			fmt.Printf("  Input tokens:     %d\n", metrics.InputTokens)
			fmt.Printf("  Output tokens:    %d\n", metrics.OutputTokens)
		}

		fmt.Println()
		fmt.Println("Stream completed successfully!")
		return
	}

	// Test non-streaming completion
	fmt.Println("Testing non-streaming completion...")
	fmt.Printf("Model: %s\n", request.Model)
	fmt.Printf("Messages: %d\n", len(request.Messages))
	fmt.Printf("Tools: %d\n", len(request.Tools))
	fmt.Println()

	fmt.Println("Sending request...")
	start := time.Now()
	resp, err := client.Completion(ctx, request)
	fmt.Printf("Request took: %v\n", time.Since(start))
	if err != nil {
		log.Fatalf("error getting completion: %v", err)
	}

	if *thinking && resp.Reasoning != "" {
		fmt.Printf("Thinking: %s\n\n", resp.Reasoning)
	}
	fmt.Printf("Response: %s\n", resp.Content)
	if resp.Usage != nil {
		fmt.Printf("\nUsage: input=%d, output=%d, total=%d tokens\n",
			resp.Usage.InputTokens, resp.Usage.OutputTokens, resp.Usage.TotalTokens)
	}
	if resp.Metrics != nil {
		fmt.Printf("\nMetrics:\n")
		fmt.Printf("  Latency:          %v\n", resp.Metrics.TotalDuration())
		fmt.Printf("  Tokens/sec:       %.1f\n", resp.Metrics.TokensPerSecond())
	}
	fmt.Println()
	fmt.Println("Completion successful!")
}

func createRequest(model enums.Model, scenario string) *llm.Request {
	switch scenario {
	case "tool_calls":
		return &llm.Request{
			Model: model,
			Messages: []llm.Message{
				{
					Role:    enums.RoleUser,
					Content: "Call get_weather for San Francisco and get_time for America/New_York. Do not write text.",
				},
			},
			Tools: []llm.Tool{
				{
					Name:        "get_weather",
					Description: "Get weather.",
					Parameters: &llm.Schema{
						Type: llm.TypeObject,
						Properties: llm.Properties{
							"location": llm.S("City"),
						},
						Required: []string{"location"},
					},
				},
				{
					Name:        "get_time",
					Description: "Get time.",
					Parameters: &llm.Schema{
						Type: llm.TypeObject,
						Properties: llm.Properties{
							"timezone": llm.S("Timezone"),
						},
						Required: []string{"timezone"},
					},
				},
			},
		}
	case "text":
		fallthrough
	default:
		return &llm.Request{
			Model: model,
			Messages: []llm.Message{
				{
					Role:    enums.RoleUser,
					Content: "Reply with exactly: Hello.",
				},
			},
		}
	}
}

func createClient(provider enums.Provider, transport http.RoundTripper, isReplay, thinking bool) (llm.Client, enums.Model, error) {
	switch provider {
	case enums.ProviderOpenAI:
		return createOpenAIClient(transport, isReplay, thinking)
	case enums.ProviderAnthropic:
		return createAnthropicClient(transport, isReplay, thinking)
	default:
		return nil, "", errors.Errorf("unsupported provider: %s", provider)
	}
}
