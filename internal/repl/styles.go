package repl

import "charm.land/lipgloss/v2"

var (
	// promptStyle renders the node title portion of the prompt.
	promptStyle = lipgloss.NewStyle().Bold(true)

	// systemStyle is for REPL meta-output (not LLM content).
	systemStyle = lipgloss.NewStyle().Faint(true)

	// modeStyle renders mode indicators like [discussion].
	modeStyle = lipgloss.NewStyle().Bold(true).Italic(true)

	// listNumStyle renders the number prefix in numbered lists.
	listNumStyle = lipgloss.NewStyle().Faint(true)

	// listItemStyle renders a list item — no color override, uses terminal default.
	listItemStyle = lipgloss.NewStyle()

	// keyStyle renders a key in .info output.
	keyStyle = lipgloss.NewStyle().Faint(true)

	// valStyle renders a value in .info output.
	valStyle = lipgloss.NewStyle().Bold(true)

	// helpCmdStyle renders a regular command name in .help — ANSI cyan.
	helpCmdStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))

	// helpDescStyle renders a description in .help.
	helpDescStyle = lipgloss.NewStyle().Faint(true)

	// dotCmdStyle renders a dot command — ANSI magenta to distinguish from regular commands.
	dotCmdStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))
)
