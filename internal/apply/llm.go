package apply

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"sevens/internal/config"
)

// LoadGlobalConfig delegates to config.LoadGlobalConfig.
// Callers should migrate to importing the config package directly.
func LoadGlobalConfig() (GlobalConfig, error) {
	return config.LoadGlobalConfig()
}

// resolveAPIKey returns the API key from config.APIKey if set, otherwise falls
// back to the environment variable named by config.APIKeyEnv.
func resolveAPIKey(config LLMConfig) (string, error) {
	if config.APIKey != "" {
		return config.APIKey, nil
	}
	key := os.Getenv(config.APIKeyEnv)
	if key == "" {
		return "", fmt.Errorf("environment variable %s is not set", config.APIKeyEnv)
	}
	return key, nil
}

func CallLLM(ctx context.Context, config LLMConfig, systemPrompt, prompt string, streamTo io.Writer) (string, error) {
	apiKey, err := resolveAPIKey(config)
	if err != nil {
		return "", err
	}

	switch config.Provider {
	case "anthropic":
		client := anthropic.NewClient(option.WithAPIKey(apiKey))
		params := anthropic.MessageNewParams{
			Model:     anthropic.Model(config.Model),
			MaxTokens: 16384,
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
			},
		}
		if systemPrompt != "" {
			params.System = []anthropic.TextBlockParam{
				{Text: systemPrompt},
			}
		}

		stream := client.Messages.NewStreaming(ctx, params)
		var result strings.Builder
		dotCount := 0
		for stream.Next() {
			event := stream.Current()
			if event.Type == "content_block_delta" && event.Delta.Text != "" {
				if streamTo != nil {
					fmt.Fprint(streamTo, event.Delta.Text)
				} else {
					// Show progress dots for non-streamed output
					dotCount++
					if dotCount%50 == 0 {
						fmt.Fprint(os.Stderr, ".")
					}
				}
				result.WriteString(event.Delta.Text)
			}
		}
		if streamTo != nil {
			fmt.Fprintln(streamTo)
		} else if dotCount >= 50 {
			fmt.Fprintln(os.Stderr) // newline after dots
		}
		if err := stream.Err(); err != nil {
			return "", fmt.Errorf("anthropic streaming: %w", err)
		}

		if result.Len() == 0 {
			return "", fmt.Errorf("anthropic returned empty response")
		}
		return result.String(), nil

	default:
		return "", fmt.Errorf("unsupported LLM provider: %s", config.Provider)
	}
}
