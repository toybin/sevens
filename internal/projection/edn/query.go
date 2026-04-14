package edn

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	ednencode "olympos.io/encoding/edn"

	"sevens/internal/function"
	"sevens/internal/kb"
	"sevens/internal/types"
)

// ListFunctions returns all function names from the store.
func (p *EDNProjection) ListFunctions(ctx context.Context) ([]string, error) {
	triples, err := p.store.ByPredicate(ctx, kb.PredFnName)
	if err != nil {
		return nil, fmt.Errorf("edn: list functions: %w", err)
	}
	var names []string
	for _, t := range triples {
		names = append(names, t.Object)
	}
	return names, nil
}

// LoadFunction reconstructs a function.Function from triples in the store.
func (p *EDNProjection) LoadFunction(ctx context.Context, name string) (*function.Function, error) {
	subj := "fn:" + name

	// Check existence
	fnName, err := p.lookupOne(ctx, subj, kb.PredFnName)
	if err != nil {
		return nil, err
	}
	if fnName == "" {
		return nil, fmt.Errorf("edn: function %q not found in graph", name)
	}

	fn := &function.Function{
		Name: fnName,
	}

	fn.Description, _ = p.lookupOne(ctx, subj, kb.PredFnDescription)
	fn.ContextPolicy, _ = p.lookupOne(ctx, subj, kb.PredFnContextPolicy)

	// Params
	paramsJSON, _ := p.lookupOne(ctx, subj, kb.PredFnParams)
	if paramsJSON != "" {
		var params []struct {
			Name     string `json:"Name"`
			Required bool   `json:"Required,omitempty"`
			Default  string `json:"Default,omitempty"`
		}
		if err := json.Unmarshal([]byte(paramsJSON), &params); err == nil {
			for _, p := range params {
				fn.Params = append(fn.Params, function.Param{
					Name:     p.Name,
					Required: p.Required,
					Default:  p.Default,
				})
			}
		}
	}

	// Check if multi-step
	pipelineOrder, _ := p.lookupOne(ctx, subj, kb.PredFnPipelineOrder)
	if pipelineOrder != "" {
		// Multi-step function
		stepNames := strings.Split(pipelineOrder, ",")
		for _, stepName := range stepNames {
			step, err := p.loadStep(ctx, name, stepName)
			if err != nil {
				return nil, fmt.Errorf("edn: load step %s/%s: %w", name, stepName, err)
			}
			fn.Steps = append(fn.Steps, *step)
		}
	} else {
		// Single-step function: build one step from fn-level triples
		step := function.Step{
			Name: "default",
		}

		output, _ := p.lookupOne(ctx, subj, kb.PredFnOutput)
		step.Output = parseOutputSignature(output, "")
		step.Input = function.Signature{Shape: function.ShapeText}

		// Output picker: if present in the graph, re-parse the
		// serialized EDN form back into a picker.OutputPicker and
		// attach it to the step. This is the equivalent of the
		// file-based loader's ParseOutputPicker call.
		if pickerEDN, _ := p.lookupOne(ctx, subj, kb.PredFnOutputPicker); pickerEDN != "" {
			var raw any
			if err := ednencode.Unmarshal([]byte(pickerEDN), &raw); err == nil {
				if op, perr := function.ParseOutputPicker(raw); perr == nil && op != nil {
					step.OutputPicker = op
					// Derive shape from alternatives if step's static
					// :output was empty.
					if output == "" {
						if shape, derr := function.DeriveShapeFromPicker(op); derr == nil {
							step.Output.Shape = shape
						}
					}
				}
			}
		}

		prompt, _ := p.lookupOne(ctx, subj, kb.PredFnPrompt)
		persona, _ := p.lookupOne(ctx, subj, kb.PredFnPersona)
		sysPrompt, _ := p.lookupOne(ctx, subj, kb.PredFnSystemPrompt)

		step.Backend = function.BackendSpec{
			Kind:           function.BackendLLM,
			PromptTemplate: prompt,
			Persona:        persona,
			SystemPrompt:   sysPrompt,
		}

		// Context specs -> paths
		specsJSON, _ := p.lookupOne(ctx, subj, kb.PredFnContextSpecs)
		if specsJSON != "" {
			var specs []struct {
				Path        []string `json:"path"`
				ExcludeSelf bool     `json:"exclude-self,omitempty"`
				With        []string `json:"with,omitempty"`
				As          string   `json:"as"`
			}
			if err := json.Unmarshal([]byte(specsJSON), &specs); err == nil {
				for _, s := range specs {
					step.Paths = append(step.Paths, function.PathSpec{
						Path:        s.Path,
						ExcludeSelf: s.ExcludeSelf,
						With:        s.With,
						As:          s.As,
					})
				}
			}
		}

		// Requires
		reqJSON, _ := p.lookupOne(ctx, subj, kb.PredFnRequires)
		if reqJSON != "" {
			var reqs []struct {
				Role     string `json:"role"`
				Type     string `json:"type,omitempty"`
				Optional bool   `json:"optional,omitempty"`
				As       string `json:"as,omitempty"`
			}
			if err := json.Unmarshal([]byte(reqJSON), &reqs); err == nil {
				for _, r := range reqs {
					step.Requires = append(step.Requires, function.Require{
						Role:     r.Role,
						Type:     r.Type,
						Optional: r.Optional,
						As:       r.As,
					})
				}
			}
		}

		fn.Steps = append(fn.Steps, step)
	}

	return fn, nil
}

// loadStep reconstructs a single Step from step triples.
func (p *EDNProjection) loadStep(ctx context.Context, fnName, stepName string) (*function.Step, error) {
	stepSubj := fmt.Sprintf("step:%s:%s", fnName, stepName)

	step := &function.Step{
		Name: stepName,
	}

	composedOf, _ := p.lookupOne(ctx, stepSubj, kb.PredStepComposedOf)
	step.ComposedOf = composedOf

	mapOver, _ := p.lookupOne(ctx, stepSubj, kb.PredStepMapOver)
	step.MapOver = mapOver

	input, _ := p.lookupOne(ctx, stepSubj, kb.PredStepInput)
	step.Input = parseInputSignature(input)

	output, _ := p.lookupOne(ctx, stepSubj, kb.PredStepOutput)
	outputType, _ := p.lookupOne(ctx, stepSubj, kb.PredStepOutputType)
	step.Output = parseOutputSignature(output, outputType)

	gate, _ := p.lookupOne(ctx, stepSubj, kb.PredStepGate)
	if gate == "approve" {
		step.Gate = &function.GateSpec{
			Revisable:  true,
			Cancelable: true,
		}
	}

	// Backend
	backendJSON, _ := p.lookupOne(ctx, stepSubj, kb.PredStepBackend)
	if backendJSON != "" {
		var spec struct {
			Kind   string `json:"Kind"`
			Config *struct {
				Mode            string `json:"mode"`
				TitlePattern    string `json:"title-pattern,omitempty"`
				Parent          string `json:"parent,omitempty"`
				ParentTemplate  string `json:"parent-template,omitempty"`
				Target          string `json:"target,omitempty"`
				Heading         string `json:"heading,omitempty"`
				CreateIfMissing bool   `json:"create-if-missing,omitempty"`
			} `json:"Config,omitempty"`
		}
		if err := json.Unmarshal([]byte(backendJSON), &spec); err == nil && spec.Kind == "deterministic" {
			cfg := function.DeterministicConfig{}
			if spec.Config != nil {
				cfg = function.DeterministicConfig{
					Mode:            spec.Config.Mode,
					TitlePattern:    spec.Config.TitlePattern,
					Parent:          spec.Config.Parent,
					ParentTemplate:  spec.Config.ParentTemplate,
					Target:          spec.Config.Target,
					Heading:         spec.Config.Heading,
					CreateIfMissing: spec.Config.CreateIfMissing,
				}
			}
			cfgJSON, _ := json.Marshal(cfg)
			prompt, _ := p.lookupOne(ctx, stepSubj, kb.PredStepPrompt)
			step.Backend = function.BackendSpec{
				Kind:           function.BackendDeterministic,
				PromptTemplate: prompt,
				Handler:        string(cfgJSON),
			}
			return step, nil
		}
	}

	// Default: LLM backend
	prompt, _ := p.lookupOne(ctx, stepSubj, kb.PredStepPrompt)
	persona, _ := p.lookupOne(ctx, stepSubj, kb.PredStepPersona)
	sysPrompt, _ := p.lookupOne(ctx, stepSubj, kb.PredStepSystemPrompt)

	step.Backend = function.BackendSpec{
		Kind:           function.BackendLLM,
		PromptTemplate: prompt,
		Persona:        persona,
		SystemPrompt:   sysPrompt,
	}

	// Step requires
	reqJSON, _ := p.lookupOne(ctx, stepSubj, kb.PredStepRequires)
	if reqJSON != "" {
		var reqs []struct {
			Role     string `json:"role"`
			Type     string `json:"type,omitempty"`
			Optional bool   `json:"optional,omitempty"`
			As       string `json:"as,omitempty"`
		}
		if err := json.Unmarshal([]byte(reqJSON), &reqs); err == nil {
			for _, r := range reqs {
				step.Requires = append(step.Requires, function.Require{
					Role:     r.Role,
					Type:     r.Type,
					Optional: r.Optional,
					As:       r.As,
				})
			}
		}
	}

	return step, nil
}

// --- Type loading ---

// ListTypes returns all type names from the store.
func (p *EDNProjection) ListTypes(ctx context.Context) ([]string, error) {
	triples, err := p.store.ByPredicate(ctx, kb.PredTypeName)
	if err != nil {
		return nil, fmt.Errorf("edn: list types: %w", err)
	}
	var names []string
	for _, t := range triples {
		names = append(names, t.Object)
	}
	return names, nil
}

// LoadTypeDef reconstructs a types.TypeDef from triples in the store.
func (p *EDNProjection) LoadTypeDef(ctx context.Context, name string) (*types.TypeDef, error) {
	subj := "type:" + name

	typeName, err := p.lookupOne(ctx, subj, kb.PredTypeName)
	if err != nil {
		return nil, err
	}
	if typeName == "" {
		return nil, fmt.Errorf("edn: type %q not found in graph", name)
	}

	td := &types.TypeDef{
		Name: typeName,
	}

	td.Extends, _ = p.lookupOne(ctx, subj, kb.PredTypeExtends)

	primStr, _ := p.lookupOne(ctx, subj, kb.PredTypePrimitive)
	td.Primitive = primStr == "true"

	td.SchemaInstruction, _ = p.lookupOne(ctx, subj, kb.PredTypeSchemaInstruction)

	// Required predicates (relational -- multiple values)
	required, err := p.store.BySubjectPredicate(ctx, subj, kb.PredTypeRequiresPred)
	if err != nil {
		return nil, err
	}
	td.Predicates.Required = required

	// Optional predicates (relational)
	optional, err := p.store.BySubjectPredicate(ctx, subj, kb.PredTypeOptionalPred)
	if err != nil {
		return nil, err
	}
	td.Predicates.Optional = optional

	td.Structure.ParentType, _ = p.lookupOne(ctx, subj, kb.PredTypeParentType)

	minStr, _ := p.lookupOne(ctx, subj, kb.PredTypeChildrenMin)
	if minStr != "" {
		td.Structure.ChildrenMin, _ = strconv.Atoi(minStr)
	}
	maxStr, _ := p.lookupOne(ctx, subj, kb.PredTypeChildrenMax)
	if maxStr != "" {
		td.Structure.ChildrenMax, _ = strconv.Atoi(maxStr)
	}

	// Frontmatter (relational)
	frontmatter, err := p.store.BySubjectPredicate(ctx, subj, kb.PredTypeFrontmatter)
	if err != nil {
		return nil, err
	}
	td.Projection.Frontmatter = frontmatter

	// Orthography
	orthJSON, _ := p.lookupOne(ctx, subj, kb.PredTypeOrthography)
	if orthJSON != "" {
		var orth map[string]struct {
			Signifier  string `json:"signifier"`
			ValueModel string `json:"value-model"`
		}
		if err := json.Unmarshal([]byte(orthJSON), &orth); err == nil {
			td.Projection.Orthography = make(map[string]types.OrthographyBinding, len(orth))
			for k, v := range orth {
				td.Projection.Orthography[k] = types.OrthographyBinding{
					Signifier:  v.Signifier,
					ValueModel: v.ValueModel,
				}
			}
		}
	}

	return td, nil
}

// LoadAllTypes loads all type definitions from the store.
func (p *EDNProjection) LoadAllTypes(ctx context.Context) (map[string]*types.TypeDef, error) {
	names, err := p.ListTypes(ctx)
	if err != nil {
		return nil, err
	}
	m := make(map[string]*types.TypeDef, len(names))
	for _, name := range names {
		td, err := p.LoadTypeDef(ctx, name)
		if err != nil {
			return nil, err
		}
		m[name] = td
	}
	return m, nil
}

// --- Helpers ---

// lookupOne returns the first object for a (subject, predicate) pair.
// Returns ("", nil) if no value exists.
func (p *EDNProjection) lookupOne(ctx context.Context, subject, predicate string) (string, error) {
	vals, err := p.store.BySubjectPredicate(ctx, subject, predicate)
	if err != nil {
		return "", err
	}
	if len(vals) == 0 {
		return "", nil
	}
	return vals[0], nil
}

// parseOutputSignature delegates to the canonical implementation.
func parseOutputSignature(output, outputType string) function.Signature {
	return function.ParseOutputSignature(output, outputType)
}

// parseInputSignature delegates to the canonical implementation.
func parseInputSignature(input string) function.Signature {
	return function.ParseInputSignature(input)
}
