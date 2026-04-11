package backend

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"os"
)

// AnthropicBackend calls the Anthropic API directly.
type AnthropicBackend struct {
	APIKey string
}

// NewAnthropicBackend creates an Anthropic API backend.
// If apiKey is empty, it reads from the ANTHROPIC_API_KEY environment variable.
func NewAnthropicBackend(apiKey, apiKeyEnv string) (*AnthropicBackend, error) {
	key := apiKey
	if key == "" {
		env := apiKeyEnv
		if env == "" {
			env = "ANTHROPIC_API_KEY"
		}
		key = os.Getenv(env)
	}
	if key == "" {
		return nil, fmt.Errorf("no API key: set ANTHROPIC_API_KEY or configure api-key in config.edn")
	}
	return &AnthropicBackend{APIKey: key}, nil
}

func (b *AnthropicBackend) Name() string { return "anthropic" }

func (b *AnthropicBackend) Complete(ctx context.Context, req InferenceRequest) (string, error) {
	client := anthropic.NewClient(option.WithAPIKey(b.APIKey))

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: 16384,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(req.Prompt)),
		},
	}
	if req.SystemPrompt != "" {
		params.System = []anthropic.TextBlockParam{
			{Text: req.SystemPrompt},
		}
	}

	stream := client.Messages.NewStreaming(ctx, params)
	var result strings.Builder
	dotCount := 0
	for stream.Next() {
		event := stream.Current()
		if event.Type == "content_block_delta" && event.Delta.Text != "" {
			if req.StreamTo != nil {
				fmt.Fprint(req.StreamTo, event.Delta.Text)
			} else {
				dotCount++
				if dotCount%50 == 0 {
					fmt.Fprint(os.Stderr, ".")
				}
			}
			result.WriteString(event.Delta.Text)
		}
	}
	if req.StreamTo != nil {
		fmt.Fprintln(req.StreamTo)
	} else if dotCount >= 50 {
		fmt.Fprintln(os.Stderr)
	}
	if err := stream.Err(); err != nil {
		return "", fmt.Errorf("anthropic streaming: %w", err)
	}

	if result.Len() == 0 {
		return "", fmt.Errorf("anthropic returned empty response")
	}
	return result.String(), nil
}
