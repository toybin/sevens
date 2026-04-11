package apply

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// ModelPricing holds per-million-token costs in USD.
type ModelPricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

// LookupPricing returns pricing for a model, or false if unknown.
func LookupPricing(model string) (ModelPricing, bool) {
	prices := map[string]ModelPricing{
		"claude-opus-4":   {15.0, 75.0},
		"claude-sonnet-4": {3.0, 15.0},
		"claude-haiku-4":  {0.80, 4.0},
	}
	for prefix, p := range prices {
		if strings.HasPrefix(model, prefix) {
			return p, true
		}
	}
	return ModelPricing{}, false
}

// CountTokens calls the Anthropic token counting API to get the exact input token count.
// Falls back to a heuristic estimate if the API call fails.
func CountTokens(config LLMConfig, systemPrompt, userPrompt string) (int, error) {
	apiKey, err := resolveAPIKey(config)
	if err != nil {
		// Fall back to heuristic
		return estimateTokens(systemPrompt) + estimateTokens(userPrompt), nil
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	params := anthropic.MessageCountTokensParams{
		Model: anthropic.Model(config.Model),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
		},
	}
	if systemPrompt != "" {
		params.System = anthropic.MessageCountTokensParamsSystemUnion{
			OfTextBlockArray: []anthropic.TextBlockParam{
				{Text: systemPrompt},
			},
		}
	}

	result, err := client.Messages.CountTokens(context.Background(), params)
	if err != nil {
		// Fall back to heuristic on API error
		return estimateTokens(systemPrompt) + estimateTokens(userPrompt), fmt.Errorf("token count API: %w", err)
	}

	return int(result.InputTokens), nil
}

// estimateTokens is the fallback heuristic (~4 chars per token).
func estimateTokens(text string) int {
	return (len(text) + 3) / 4
}
