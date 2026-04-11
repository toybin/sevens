package function

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"olympos.io/encoding/edn"
	"sevens/defaults"
	"sevens/internal/config"
)

// ednAgentConfig is the EDN representation of agent configuration.
type ednAgentConfig struct {
	Persona       string   `edn:"persona,omitempty"`
	SystemPrompt  string   `edn:"system-prompt,omitempty"`
	Model         string   `edn:"model,omitempty"`
	ContextPolicy string   `edn:"context-policy,omitempty"`
	Exploration   string   `edn:"exploration,omitempty"`
	Capabilities  []string `edn:"capabilities,omitempty"`
}

// ednPathSpec is the EDN representation of a context path.
type ednPathSpec struct {
	Path        []string `edn:"path"`
	ExcludeSelf bool     `edn:"exclude-self,omitempty"`
	With        []string `edn:"with,omitempty"`
	As          string   `edn:"as"`
}

// ednRequire is the EDN representation of a typed input requirement.
type ednRequire struct {
	Role     string `edn:"role"`
	Type     string `edn:"type"`
	Optional bool   `edn:"optional,omitempty"`
	Ref      string `edn:"ref,omitempty"`
	As       string `edn:"as,omitempty"`
}

// ednStep is the EDN representation of a pipeline step.
type ednStep struct {
	Name     string          `edn:"name"`
	Prompt   string          `edn:"prompt"`
	Input    string          `edn:"input"`
	Output   string          `edn:"output"`
	Gate     string          `edn:"gate"`
	Requires []ednRequire    `edn:"requires,omitempty"`
	Fn       string          `edn:"fn,omitempty"`
	MapOver  string          `edn:"map-over,omitempty"`
	Agent    *ednAgentConfig `edn:"agent,omitempty"`
}

// ednFunction is the EDN representation of a function definition.
type ednFunction struct {
	Name         string          `edn:"name"`
	Description  string          `edn:"description"`
	Prompt       string          `edn:"prompt"`
	Input        string          `edn:"input"`
	Output       string          `edn:"output"`
	Steps        []ednStep       `edn:"steps"`
	Requires     []ednRequire    `edn:"requires,omitempty"`
	Context      []ednPathSpec   `edn:"context,omitempty"`
	Agent        *ednAgentConfig `edn:"agent,omitempty"`
	Backend      string          `edn:"backend"`
	ContextFiles []string        `edn:"context-files"`
	CrossWalk    string          `edn:"cross-walk,omitempty"`
	AdHoc        bool            `edn:"ad-hoc,omitempty"`
}

// LoadFunction loads a function definition by name from EDN and converts
// it directly to the new Function type. Returns both the new Function and
// the raw EDN struct (for callers that still need legacy fields like
// ContextFiles, CrossWalk, etc.).
func LoadFunction(name string) (*Function, *ednFunction, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, nil, fmt.Errorf("get config dir: %w", err)
	}
	fnDir := filepath.Join(dir, "functions")
	path := filepath.Join(fnDir, name+".edn")
	data, err := readFunctionAsset(path, name+".edn")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, fmt.Errorf("function %q not found — run 'sevens functions' to list available functions", name)
		}
		return nil, nil, fmt.Errorf("reading function %q: %w", name, err)
	}

	var raw ednFunction
	if err := edn.Unmarshal(data, &raw); err != nil {
		return nil, nil, fmt.Errorf("parse function %s: %w", name, err)
	}
	if raw.Name == "" {
		return nil, nil, fmt.Errorf("function %s: name must be non-empty", name)
	}

	// Load prompt templates from .md files where inline prompt is empty.
	if raw.Prompt == "" && len(raw.Steps) == 0 {
		mdPath := filepath.Join(fnDir, name+".md")
		if md, err := readFunctionAsset(mdPath, name+".md"); err == nil {
			raw.Prompt = string(md)
		} else {
			return nil, nil, fmt.Errorf("function %s: must have prompt (inline or %s.md) or steps", name, name)
		}
	}

	for i := range raw.Steps {
		if raw.Steps[i].Prompt == "" && raw.Steps[i].Fn == "" {
			stepName := raw.Steps[i].Name
			mdPath := filepath.Join(fnDir, name+"."+stepName+".md")
			md, err := readFunctionAsset(mdPath, name+"."+stepName+".md")
			if err != nil {
				return nil, nil, fmt.Errorf("function %s step %q: no inline prompt and %s not found", name, stepName, mdPath)
			}
			raw.Steps[i].Prompt = string(md)
		}
	}

	fn := convertEDNFunction(&raw)

	if err := fn.ValidateComposition(); err != nil {
		return nil, nil, fmt.Errorf("function %s: %w", name, err)
	}

	return fn, &raw, nil
}

// ListFunctions returns all available function names.
func ListFunctions() ([]string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("get config dir: %w", err)
	}
	fnDir := filepath.Join(dir, "functions")
	entries, err := os.ReadDir(fnDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read functions dir %s: %w", fnDir, err)
	}
	nameSet := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".edn" {
			continue
		}
		nameSet[strings.TrimSuffix(e.Name(), ".edn")] = true
	}
	if bundled, err := defaults.ListFunctionNames(); err == nil {
		for _, name := range bundled {
			nameSet[name] = true
		}
	}
	names := make([]string, 0, len(nameSet))
	for n := range nameSet {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// ListFunctionDefs returns all available functions with their descriptions.
func ListFunctionDefs() ([]Function, error) {
	names, err := ListFunctions()
	if err != nil {
		return nil, err
	}
	var fns []Function
	for _, name := range names {
		fn, _, err := LoadFunction(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			continue
		}
		fns = append(fns, *fn)
	}
	return fns, nil
}

func readFunctionAsset(userPath, bundledName string) ([]byte, error) {
	data, err := os.ReadFile(userPath)
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	data, err = defaults.ReadFunctionFile(bundledName)
	if err == nil {
		return data, nil
	}
	return nil, fmt.Errorf("open %s: %w", bundledName, err)
}

// convertEDNFunction converts raw EDN to the new Function type.
func convertEDNFunction(raw *ednFunction) *Function {
	fn := &Function{
		Name:        raw.Name,
		Description: raw.Description,
	}

	if raw.Agent != nil && raw.Agent.ContextPolicy != "" {
		fn.ContextPolicy = raw.Agent.ContextPolicy
	}

	steps := effectiveEDNSteps(raw)
	for _, s := range steps {
		step := convertEDNStep(s, raw)
		fn.Steps = append(fn.Steps, step)
	}

	return fn
}

// effectiveEDNSteps normalizes single-prompt functions into a one-step pipeline.
func effectiveEDNSteps(raw *ednFunction) []ednStep {
	if len(raw.Steps) > 0 {
		return raw.Steps
	}
	return []ednStep{{Name: "default", Prompt: raw.Prompt, Input: raw.Input, Output: raw.Output}}
}

func convertEDNStep(s ednStep, fn *ednFunction) Step {
	step := Step{
		Name:       s.Name,
		ComposedOf: s.Fn,
		MapOver:    s.MapOver,
	}

	for _, r := range s.Requires {
		step.Requires = append(step.Requires, Require{
			Role:     r.Role,
			Type:     r.Type,
			Optional: r.Optional,
			As:       r.As,
		})
	}
	for _, r := range fn.Requires {
		step.Requires = append(step.Requires, Require{
			Role:     r.Role,
			Type:     r.Type,
			Optional: r.Optional,
			As:       r.As,
		})
	}

	for _, p := range fn.Context {
		step.Paths = append(step.Paths, PathSpec{
			Path:        p.Path,
			ExcludeSelf: p.ExcludeSelf,
			With:        p.With,
			As:          p.As,
		})
	}

	switch s.Output {
	case "ops":
		step.Output = Signature{Shape: ShapeFileOps}
	case "suggestions":
		step.Output = Signature{Shape: ShapeStructured}
	default:
		step.Output = Signature{Shape: ShapeText}
	}

	switch s.Input {
	case "ops":
		step.Input = Signature{Shape: ShapeFileOps}
	case "suggestions":
		step.Input = Signature{Shape: ShapeStructured}
	default:
		step.Input = Signature{Shape: ShapeText}
	}

	if s.Gate == "approve" {
		step.Gate = &GateSpec{
			Revisable:  true,
			Cancelable: true,
		}
	}

	step.Backend = BackendSpec{
		Kind:           BackendLLM,
		PromptTemplate: s.Prompt,
	}
	agent := effectiveEDNAgent(fn, &s)
	if agent != nil {
		step.Backend.Persona = agent.Persona
		step.Backend.SystemPrompt = agent.SystemPrompt
	}

	return step
}

func effectiveEDNAgent(fn *ednFunction, step *ednStep) *ednAgentConfig {
	if step.Agent != nil {
		return step.Agent
	}
	return fn.Agent
}

// EffectiveSteps returns the pipeline steps, normalizing single-prompt into a one-step pipeline.
func (f *ednFunction) EffectiveSteps() []ednStep {
	return effectiveEDNSteps(f)
}

// ValidateComposition checks step output/input chaining.
func (f *ednFunction) ValidateComposition() error {
	steps := f.EffectiveSteps()
	for i := 1; i < len(steps); i++ {
		prev := steps[i-1]
		curr := steps[i]
		if prev.Fn != "" || prev.MapOver != "" || curr.Fn != "" || curr.MapOver != "" {
			continue
		}
		if prev.Output != "" && curr.Input != "" && prev.Output != curr.Input {
			return fmt.Errorf("step %q outputs %q but step %q expects input %q",
				prev.Name, prev.Output, curr.Name, curr.Input)
		}
	}
	return nil
}

// EDNFunction is the exported alias for the raw EDN function struct.
// Callers that need legacy fields (ContextFiles, CrossWalk, etc.) use this.
type EDNFunction = ednFunction

// EDNStep is the exported alias for an EDN step definition.
type EDNStep = ednStep
