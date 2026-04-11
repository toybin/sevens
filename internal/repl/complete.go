package repl

import (
	"strings"

	"github.com/chzyer/readline"
)

// dotCommands is the fixed list of dot commands for tab completion.
var dotCommands = []string{
	".help", ".info", ".quit", ".exit", ".clear",
	".functions", ".fns",
	".model", ".backend", ".theme", ".dry",
	".include", ".exclude",
}

// namedCommands is the fixed list of non-function, non-dot commands.
var namedCommands = []string{
	"walk", "templates", "capture", "instantiate", "inbox", "blocks", "diff-blocks", "extract-block", "children", "siblings", "search",
	"pending", "log", "accept", "reject", "revert", "overview",
	"sync", "note", "discuss", "new",
	"focus", "f", "child", "c", "sibling", "s",
	"..", "up", "root",
}

// modelTiers for .model completion.
var modelTiers = []string{"fast", "capable", "powerful"}

// themes for .theme completion.
var themes = []string{"light", "dark"}

// completer implements readline.AutoCompleter for the REPL.
type completer struct {
	r *REPL
}

func newCompleter(r *REPL) readline.AutoCompleter {
	return &completer{r: r}
}

// Do returns completion candidates for the current line up to pos.
func (c *completer) Do(line []rune, pos int) (newLine [][]rune, length int) {
	// Work with the portion of line up to the cursor.
	input := string(line[:pos])
	trimmed := strings.TrimLeft(input, " ")

	// ── Dot command context ──────────────────────────────────────────────────

	if strings.HasPrefix(trimmed, ".model ") {
		prefix := strings.TrimPrefix(trimmed, ".model ")
		return completeFrom(modelTiers, prefix), len(prefix)
	}

	if strings.HasPrefix(trimmed, ".theme ") {
		prefix := strings.TrimPrefix(trimmed, ".theme ")
		return completeFrom(themes, prefix), len(prefix)
	}

	if strings.HasPrefix(trimmed, ".backend ") {
		prefix := strings.TrimPrefix(trimmed, ".backend ")
		backends := c.r.backendCandidates()
		return completeFrom(backends, prefix), len(prefix)
	}

	if strings.HasPrefix(trimmed, ".include ") {
		prefix := strings.TrimPrefix(trimmed, ".include ")
		candidates := append([]string{"clear"}, c.r.nodeTitles()...)
		candidates = append(candidates, c.r.groupNames()...)
		return completeFrom(candidates, prefix), len(prefix)
	}

	if strings.HasPrefix(trimmed, ".exclude ") {
		prefix := strings.TrimPrefix(trimmed, ".exclude ")
		return completeFrom(c.r.includes, prefix), len(prefix)
	}

	if strings.HasPrefix(trimmed, ".") && !strings.Contains(trimmed, " ") {
		// Completing a dot command itself.
		return completeFrom(dotCommands, trimmed), len(trimmed)
	}

	// ── Named command with argument ──────────────────────────────────────────

	if strings.HasPrefix(trimmed, "search ") ||
		strings.HasPrefix(trimmed, "walk ") ||
		strings.HasPrefix(trimmed, "inbox ") ||
		strings.HasPrefix(trimmed, "blocks ") ||
		strings.HasPrefix(trimmed, "diff-blocks ") ||
		strings.HasPrefix(trimmed, "log ") ||
		strings.HasPrefix(trimmed, "focus ") ||
		strings.HasPrefix(trimmed, "f ") {
		// Complete node titles after the command.
		parts := strings.SplitN(trimmed, " ", 2)
		prefix := ""
		if len(parts) == 2 {
			prefix = parts[1]
		}
		return completeFrom(c.r.nodeTitles(), prefix), len(prefix)
	}

	if strings.HasPrefix(trimmed, "instantiate ") {
		prefix := strings.TrimPrefix(trimmed, "instantiate ")
		var names []string
		if c.r.templateR != nil {
			names, _ = c.r.templateR.ListTemplates()
		}
		return completeFrom(names, prefix), len(prefix)
	}

	// ── Top-level completion ─────────────────────────────────────────────────

	// Combine all candidates: dot commands + named commands + function names + node titles.
	var candidates []string
	candidates = append(candidates, dotCommands...)
	candidates = append(candidates, namedCommands...)
	candidates = append(candidates, functionNames()...)
	candidates = append(candidates, c.r.nodeTitles()...)

	return completeFrom(candidates, trimmed), len(trimmed)
}

// completeFrom filters candidates by prefix and returns readline-style results.
func completeFrom(candidates []string, prefix string) [][]rune {
	var matches [][]rune
	lp := strings.ToLower(prefix)
	for _, c := range candidates {
		lc := strings.ToLower(c)
		if strings.HasPrefix(lc, lp) {
			// Return the suffix that completes the prefix.
			// Use the case-insensitive prefix length to slice the original.
			if len(prefix) <= len(c) {
				matches = append(matches, []rune(c[len(prefix):]))
			} else {
				matches = append(matches, []rune(c))
			}
		}
	}
	return matches
}

// groupNames returns @-prefixed group names from .sevens.edn for tab completion.
func (r *REPL) groupNames() []string {
	if r.graphQ == nil {
		return nil
	}
	config, err := r.graphQ.LoadConfig(r.root)
	if err != nil {
		return nil
	}
	var names []string
	for name := range config.Groups {
		names = append(names, "@"+name)
	}
	return names
}

// backendCandidates returns known backend names from global config.
func (r *REPL) backendCandidates() []string {
	base := []string{"anthropic", "codex", "claude"}
	seen := make(map[string]bool)
	for _, b := range base {
		seen[b] = true
	}
	for name := range r.globalCfg.Backends {
		if !seen[name] {
			base = append(base, name)
		}
	}
	return base
}
