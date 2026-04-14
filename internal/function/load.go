package function

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"olympos.io/encoding/edn"
	"sevens/internal/config"
	"sevens/internal/ednformat"
)

// GraphFunctionLoader can load functions from the triple store.
// Set by the EDN projection at startup.
var GraphFunctionLoader interface {
	LoadFunction(ctx context.Context, name string) (*Function, error)
	ListFunctions(ctx context.Context) ([]string, error)
}

// Type aliases for the shared EDN format structs.
type ednAgentConfig = ednformat.AgentConfig
type ednPathSpec = ednformat.PathSpec
type ednRequire = ednformat.Require
type ednDeterministicConfig = ednformat.DeterministicConfig
type ednBackendSpec = ednformat.BackendSpec
type ednParam = ednformat.Param
type ednStep = ednformat.Step
type ednFunction = ednformat.Function

// LoadFunction loads a function definition by name from EDN and converts
// it directly to the new Function type. Returns both the new Function and
// the raw EDN struct (for callers that still need legacy fields like
// ContextFiles, CrossWalk, etc.).
func LoadFunction(name string) (*Function, *ednFunction, error) {
	// Try graph-based loading first (populated by EDN projection sync).
	if GraphFunctionLoader != nil {
		fn, err := GraphFunctionLoader.LoadFunction(context.Background(), name)
		if err == nil && fn != nil {
			return fn, nil, nil
		}
	}

	// Fall back to file-based loading.
	dir, err := config.ConfigDir()
	if err != nil {
		return nil, nil, fmt.Errorf("get config dir: %w", err)
	}
	fnDir := filepath.Join(dir, "functions")
	path := filepath.Join(fnDir, name+".edn")
	data, err := os.ReadFile(path)
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
		if md, err := os.ReadFile(mdPath); err == nil {
			raw.Prompt = string(md)
		} else {
			return nil, nil, fmt.Errorf("function %s: must have prompt (inline or %s.md) or steps", name, name)
		}
	}

	for i := range raw.Steps {
		if raw.Steps[i].Prompt == "" && raw.Steps[i].Fn == "" {
			stepName := raw.Steps[i].Name
			mdPath := filepath.Join(fnDir, name+"."+stepName+".md")
			md, err := os.ReadFile(mdPath)
			if err != nil {
				return nil, nil, fmt.Errorf("function %s step %q: no inline prompt and %s not found", name, stepName, mdPath)
			}
			raw.Steps[i].Prompt = string(md)
		}
	}

	fn := convertEDNFunction(&raw)

	return fn, &raw, nil
}

// ListFunctions returns all available function names.
func ListFunctions() ([]string, error) {
	// Try graph-based listing first.
	if GraphFunctionLoader != nil {
		names, err := GraphFunctionLoader.ListFunctions(context.Background())
		if err == nil && len(names) > 0 {
			sort.Strings(names)
			return names, nil
		}
	}

	// Fall back to file-based listing.
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

// convertEDNFunction converts raw EDN to the new Function type.
func convertEDNFunction(raw *ednFunction) *Function {
	fn := &Function{
		Name:        raw.Name,
		Description: raw.Description,
	}

	if raw.Agent != nil && raw.Agent.ContextPolicy != "" {
		fn.ContextPolicy = raw.Agent.ContextPolicy
	}

	for _, p := range raw.Params {
		fn.Params = append(fn.Params, Param{
			Name:     p.Name,
			Required: p.Required,
			Default:  p.Default,
		})
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
	return []ednStep{{
		Name:         "default",
		Prompt:       raw.Prompt,
		Input:        raw.Input,
		Output:       raw.Output,
		OutputPicker: raw.OutputPicker,
	}}
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

	step.Output = ParseOutputSignature(s.Output, s.OutputType)
	step.Input = ParseInputSignature(s.Input)

	// Parse :output-picker if present. An error here is fatal: a
	// malformed picker declaration should fail at load time, not
	// silently fall through to the static output.
	if s.OutputPicker != nil {
		op, err := ParseOutputPicker(s.OutputPicker)
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"warning: step %q: output-picker parse failed: %v\n",
				s.Name, err)
		} else {
			step.OutputPicker = op
			// If the step did not declare a static :output, derive
			// the output shape from the picker's alternatives. All
			// alternatives must share a primitive shape family
			// (create/edit → ShapeFileOps; text → ShapeText;
			// suggestion → ShapeStructured). Mixed families are a
			// load-time error.
			if s.Output == "" {
				shape, derr := DeriveShapeFromPicker(op)
				if derr != nil {
					fmt.Fprintf(os.Stderr,
						"warning: step %q: %v\n", s.Name, derr)
				} else {
					step.Output.Shape = shape
				}
			}
		}
	}

	if s.Gate == "approve" {
		step.Gate = &GateSpec{
			Revisable:  true,
			Cancelable: true,
		}
	}

	if s.BackendSpec != nil && s.BackendSpec.Kind == "deterministic" {
		cfg := DeterministicConfig{}
		if s.BackendSpec.Config != nil {
			cfg = DeterministicConfig{
				Mode:            s.BackendSpec.Config.Mode,
				TitlePattern:    s.BackendSpec.Config.TitlePattern,
				Parent:          s.BackendSpec.Config.Parent,
				ParentTemplate:  s.BackendSpec.Config.ParentTemplate,
				Target:          s.BackendSpec.Config.Target,
				Heading:         s.BackendSpec.Config.Heading,
				CreateIfMissing: s.BackendSpec.Config.CreateIfMissing,
			}
		}
		cfgJSON, _ := json.Marshal(cfg)
		step.Backend = BackendSpec{
			Kind:           BackendDeterministic,
			PromptTemplate: s.Prompt,
			Handler:        string(cfgJSON),
		}
	} else {
		step.Backend = BackendSpec{
			Kind:           BackendLLM,
			PromptTemplate: s.Prompt,
		}
		agent := effectiveEDNAgent(fn, &s)
		if agent != nil {
			step.Backend.Persona = agent.Persona
			step.Backend.SystemPrompt = agent.SystemPrompt
		}
	}

	return step
}

func effectiveEDNAgent(fn *ednFunction, step *ednStep) *ednAgentConfig {
	if step.Agent != nil {
		return step.Agent
	}
	return fn.Agent
}

// EDNFunction is the exported alias for the raw EDN function struct.
// Callers that need legacy fields (ContextFiles, CrossWalk, etc.) use this.
type EDNFunction = ednformat.Function

// EDNStep is the exported alias for an EDN step definition.
type EDNStep = ednformat.Step
