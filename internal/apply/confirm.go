package apply

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"sevens/internal/ui"
)

// ConfirmCost counts tokens via the API, shows estimated cost, and asks for confirmation.
// If threshold > 0 and the estimated cost is below it, auto-approves without prompting.
// For non-Anthropic providers (or when the API key is unavailable), token counting is
// skipped and a simplified prompt is shown instead.
func ConfirmCost(config LLMConfig, backendName, systemPrompt, userPrompt string, threshold float64) (bool, error) {
	// Determine whether we can use Anthropic's token counting API.
	effectiveBackend := strings.ToLower(strings.TrimSpace(backendName))
	isAnthropicBackend := effectiveBackend == "" || effectiveBackend == "anthropic" || effectiveBackend == "api"
	isAnthropicConfig := strings.ToLower(config.Provider) == "anthropic" || config.Provider == ""
	isAnthropic := isAnthropicBackend && isAnthropicConfig
	apiKeyAvailable := config.APIKey != "" || os.Getenv(config.APIKeyEnv) != ""

	if !isAnthropic || !apiKeyAvailable {
		// Non-Anthropic backend or no API key: skip token counting entirely.
		// If a threshold is set, auto-approve (we can't estimate cost).
		if threshold > 0 {
			fmt.Fprintf(os.Stderr, "%s skipping token count (non-Anthropic backend), auto-approving\n",
				ui.Dim.Render("[cost]"))
			return true, nil
		}
		// Show a simplified prompt without token/cost info.
		fmt.Fprintf(os.Stderr, "\n%s Model: %s (token counting not available)\n",
			ui.Dim.Render("[cost]"), ui.Label.Render(config.Model))
		fmt.Fprintf(os.Stderr, "%s [Y/n] ", ui.Label.Render("Proceed?"))
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			return false, fmt.Errorf("reading input: %w", err)
		}
		input = strings.TrimSpace(strings.ToLower(input))
		return input == "" || input == "y" || input == "yes", nil
	}

	inputTokens, err := CountTokens(config, systemPrompt, userPrompt)
	if err != nil {
		// Token counting failed but we got a fallback estimate
		fmt.Fprintf(os.Stderr, "%s token count fallback: %v\n", ui.Dim.Render("[cost]"), err)
	}

	pricing, found := LookupPricing(config.Model)

	if found {
		inputCost := float64(inputTokens) * pricing.InputPerMillion / 1_000_000
		// Estimate output at ~4096 tokens for a typical response
		outputCost := 4096.0 * pricing.OutputPerMillion / 1_000_000
		totalCost := inputCost + outputCost

		if threshold > 0 && totalCost < threshold {
			fmt.Fprintf(os.Stderr, "%s %d input tokens, ~$%.4f (auto-approved, below $%.2f threshold)\n",
				ui.Dim.Render("[cost]"), inputTokens, totalCost, threshold)
			return true, nil
		}

		fmt.Fprintf(os.Stderr, "\n%s Model: %s\n", ui.Dim.Render("[cost]"), ui.Label.Render(config.Model))
		fmt.Fprintf(os.Stderr, "%s Input tokens: %d\n", ui.Dim.Render("[cost]"), inputTokens)
		fmt.Fprintf(os.Stderr, "%s Estimated cost: ~$%.4f (in: $%.4f, out: ~$%.4f)\n",
			ui.Dim.Render("[cost]"), totalCost, inputCost, outputCost)
	} else {
		fmt.Fprintf(os.Stderr, "\n%s Model: %s\n", ui.Dim.Render("[cost]"), ui.Label.Render(config.Model))
		fmt.Fprintf(os.Stderr, "%s Input tokens: %d\n", ui.Dim.Render("[cost]"), inputTokens)
		fmt.Fprintf(os.Stderr, "%s Pricing not available for this model\n", ui.Dim.Render("[cost]"))
	}

	fmt.Fprintf(os.Stderr, "%s [Y/n] ", ui.Label.Render("Proceed?"))
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("reading input: %w", err)
	}
	input = strings.TrimSpace(strings.ToLower(input))
	return input == "" || input == "y" || input == "yes", nil
}
