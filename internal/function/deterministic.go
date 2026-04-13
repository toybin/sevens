package function

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// DeterministicBackend is a TransformBackend that expands template
// variables and produces FileOps without calling an LLM. Templates
// are functions with this backend instead of an LLM backend.
type DeterministicBackend struct{}

func (d *DeterministicBackend) Execute(_ context.Context, prompt RenderedPrompt) (TransformResult, error) {
	// System = JSON config, User = rendered markdown content
	var cfg DeterministicConfig
	if err := json.Unmarshal([]byte(prompt.System), &cfg); err != nil {
		return TransformResult{}, fmt.Errorf("deterministic: parse config: %w", err)
	}

	content := prompt.User

	switch cfg.Mode {
	case "create-node":
		return d.createNode(cfg, content)
	case "append-node":
		return d.appendNode(cfg, content)
	case "insert-block":
		return d.insertBlock(cfg, content)
	default:
		return TransformResult{}, fmt.Errorf("deterministic: unknown mode %q", cfg.Mode)
	}
}

func (d *DeterministicBackend) Name() string { return "deterministic" }

func (d *DeterministicBackend) createNode(cfg DeterministicConfig, content string) (TransformResult, error) {
	title := cfg.TitlePattern
	if title == "" {
		return TransformResult{}, fmt.Errorf("deterministic create-node: title-pattern is required")
	}

	op := FileOp{
		Action:  "create",
		Title:   title,
		Parent:  cfg.Parent,
		Content: content,
		Extra:   cfg.Frontmatter,
	}

	return TransformResult{
		Ops: []FileOp{op},
	}, nil
}

func (d *DeterministicBackend) appendNode(cfg DeterministicConfig, content string) (TransformResult, error) {
	target := cfg.Target
	if target == "" {
		return TransformResult{}, fmt.Errorf("deterministic append-node: target is required")
	}

	// For append, we produce an edit op that appends content to the end.
	// The old_text is empty and new_text is the content to append.
	// The executor/caller handles the actual file manipulation.
	op := FileOp{
		Action:  "edit",
		File:    target,
		OldText: "", // sentinel: empty old_text means "append to end"
		NewText: content,
	}

	return TransformResult{
		Ops: []FileOp{op},
	}, nil
}

func (d *DeterministicBackend) insertBlock(cfg DeterministicConfig, content string) (TransformResult, error) {
	target := cfg.Target
	if target == "" {
		return TransformResult{}, fmt.Errorf("deterministic insert-block: target is required")
	}
	heading := cfg.Heading
	if heading == "" {
		return TransformResult{}, fmt.Errorf("deterministic insert-block: heading is required")
	}

	// Encode the insert-block operation as an edit FileOp.
	// The OldText encodes the heading marker for the caller to locate.
	// The NewText is the content to insert under that heading.
	op := FileOp{
		Action:  "edit",
		File:    target,
		OldText: heading,
		NewText: content,
	}

	// Store metadata in Extra for the caller to interpret.
	op.Extra = map[string]string{
		"insert-mode":       "under-heading",
		"create-if-missing": fmt.Sprintf("%t", cfg.CreateIfMissing),
	}

	return TransformResult{
		Ops: []FileOp{op},
	}, nil
}

// templateVarRe matches {{variable}} patterns.
var templateVarRe = regexp.MustCompile(`\{\{([\w-]+)\}\}`)

// BuiltinVars returns the standard built-in template variables.
func BuiltinVars() map[string]string {
	now := time.Now()
	return map[string]string{
		"date":      now.Format("2006-01-02"),
		"time":      now.Format("15:04"),
		"timestamp": now.UTC().Format("2006-01-02T15:04:05Z"),
	}
}

// ResolveTemplateVars substitutes {{var}} placeholders in a string
// using the provided variable map.
func ResolveTemplateVars(s string, vars map[string]string) string {
	return templateVarRe.ReplaceAllStringFunc(s, func(match string) string {
		key := match[2 : len(match)-2]
		if v, ok := vars[key]; ok {
			return v
		}
		return match // leave unresolved
	})
}

// MissingParams returns the names of required params not present in vars.
func MissingParams(fn *Function, vars map[string]string) []string {
	var missing []string
	for _, p := range fn.Params {
		if !p.Required {
			continue
		}
		if v, ok := vars[p.Name]; !ok || strings.TrimSpace(v) == "" {
			missing = append(missing, p.Name)
		}
	}
	return missing
}

// BindArgs maps positional CLI arguments onto function params in order,
// skipping params already provided in vars.
func BindArgs(fn *Function, args []string, vars map[string]string) map[string]string {
	bound := make(map[string]string, len(vars)+len(args))
	for k, v := range vars {
		bound[k] = v
	}
	if len(args) == 0 || len(fn.Params) == 0 {
		return bound
	}

	argIdx := 0
	for _, p := range fn.Params {
		if _, ok := bound[p.Name]; ok {
			continue
		}
		if argIdx >= len(args) {
			break
		}
		bound[p.Name] = args[argIdx]
		argIdx++
	}

	// Backward-compatible fallback for single-arg functions.
	if argIdx < len(args) && len(args) == 1 {
		if _, ok := bound["title"]; !ok {
			bound["title"] = args[0]
		} else if _, ok := bound["topic"]; !ok {
			bound["topic"] = args[0]
		}
	}
	return bound
}

// IsDeterministic returns true if the function uses a deterministic backend.
func IsDeterministic(fn *Function) bool {
	for _, step := range fn.Steps {
		if step.Backend.Kind == BackendDeterministic {
			return true
		}
	}
	return false
}

// ListDeterministicFunctions returns names of functions with deterministic backends.
func ListDeterministicFunctions() ([]Function, error) {
	fns, err := ListFunctionDefs()
	if err != nil {
		return nil, err
	}
	var result []Function
	for _, fn := range fns {
		if IsDeterministic(&fn) {
			result = append(result, fn)
		}
	}
	return result, nil
}

// CleanUnresolved removes lines containing unresolved {{var}} placeholders.
func CleanUnresolved(s string) string {
	lines := strings.Split(s, "\n")
	var cleaned []string
	for _, line := range lines {
		if templateVarRe.MatchString(line) {
			// Keep headings even if they have unresolved vars (structure)
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				cleaned = append(cleaned, line)
				continue
			}
			// Skip non-heading lines with unresolved vars
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.Join(cleaned, "\n")
}
