// Package edn implements the EDN projection -- syncing function definitions,
// type definitions, and value models from EDN config files into the triple store.
//
// EDN config files are a projection of the triple store, exactly the same way
// markdown files are a projection of the triple store. The flow:
//
//  1. User writes/edits task.edn or notice.edn
//  2. Sync parses the EDN, expands into triples, stores in the DB
//  3. The triple store is the source of truth at runtime
//  4. Type checking, function loading, schema resolution -- all graph queries
package edn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ednencode "olympos.io/encoding/edn"
	"sevens/internal/ednformat"
	"sevens/internal/kb"
	"sevens/internal/triple"
)

// EDNProjection syncs EDN config files into the triple store.
type EDNProjection struct {
	store *triple.Store
}

// New creates an EDN projection backed by the given triple store.
func New(store *triple.Store) *EDNProjection {
	return &EDNProjection{store: store}
}

// Sync reads all .edn files from the config directories (functions/, types/)
// and expands them into triples. Clears existing fn/*, type/*, step/*
// triples first, then re-asserts from the files.
func (p *EDNProjection) Sync(ctx context.Context, functionsDir, typesDir string) error {
	// Clear existing function and type triples.
	if err := p.store.RetractBySubjectPrefix(ctx, "fn:"); err != nil {
		return fmt.Errorf("edn: clear fn triples: %w", err)
	}
	if err := p.store.RetractBySubjectPrefix(ctx, "step:"); err != nil {
		return fmt.Errorf("edn: clear step triples: %w", err)
	}
	if err := p.store.RetractBySubjectPrefix(ctx, "type:"); err != nil {
		return fmt.Errorf("edn: clear type triples: %w", err)
	}

	if err := p.SyncFunctions(ctx, functionsDir); err != nil {
		return err
	}
	if err := p.SyncTypes(ctx, typesDir); err != nil {
		return err
	}
	return nil
}

// SyncFunctions reads function .edn files and their .md prompt sidecars,
// expands each into triples with fn/* and step/* predicates.
func (p *EDNProjection) SyncFunctions(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("edn: read functions dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".edn" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".edn")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("edn: read function %s: %w", name, err)
		}

		triples, err := expandFunction(data, name, dir)
		if err != nil {
			return fmt.Errorf("edn: expand function %s: %w", name, err)
		}

		if err := p.store.AssertBatch(ctx, triples); err != nil {
			return fmt.Errorf("edn: assert function %s: %w", name, err)
		}
	}
	return nil
}

// SyncTypes reads type .edn files and expands into triples with type/* predicates.
func (p *EDNProjection) SyncTypes(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("edn: read types dir: %w", err)
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".edn" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".edn")
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("edn: read type %s: %w", name, err)
		}

		triples, err := expandType(data, name)
		if err != nil {
			return fmt.Errorf("edn: expand type %s: %w", name, err)
		}

		if err := p.store.AssertBatch(ctx, triples); err != nil {
			return fmt.Errorf("edn: assert type %s: %w", name, err)
		}
	}
	return nil
}

// --- Function expansion ---

// Type aliases for shared EDN format structs.
type ednAgentConfig = ednformat.AgentConfig
type ednPathSpec = ednformat.PathSpec
type ednRequire = ednformat.Require
type ednDeterministicConfig = ednformat.DeterministicConfig
type ednBackendSpec = ednformat.BackendSpec
type ednParam = ednformat.Param
type ednStep = ednformat.Step
type ednFunction = ednformat.Function

// expandFunction parses an EDN function definition and returns triples.
func expandFunction(data []byte, name, dir string) ([]triple.Triple, error) {
	var raw ednFunction
	if err := ednencode.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	subj := "fn:" + raw.Name
	var ts []triple.Triple

	ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredFnName, Object: raw.Name})
	if raw.Description != "" {
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredFnDescription, Object: raw.Description})
	}

	// Agent-level properties
	if raw.Agent != nil {
		if raw.Agent.Persona != "" {
			ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredFnPersona, Object: raw.Agent.Persona})
		}
		if raw.Agent.SystemPrompt != "" {
			ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredFnSystemPrompt, Object: raw.Agent.SystemPrompt})
		}
		if raw.Agent.ContextPolicy != "" {
			ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredFnContextPolicy, Object: raw.Agent.ContextPolicy})
		}
	}

	// Context specs as JSON
	if len(raw.Context) > 0 {
		specJSON, err := json.Marshal(raw.Context)
		if err != nil {
			return nil, fmt.Errorf("marshal context specs: %w", err)
		}
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredFnContextSpecs, Object: string(specJSON)})
	}

	// Requires as JSON
	if len(raw.Requires) > 0 {
		reqJSON, err := json.Marshal(raw.Requires)
		if err != nil {
			return nil, fmt.Errorf("marshal requires: %w", err)
		}
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredFnRequires, Object: string(reqJSON)})
	}

	// Params as JSON
	if len(raw.Params) > 0 {
		paramJSON, err := json.Marshal(raw.Params)
		if err != nil {
			return nil, fmt.Errorf("marshal params: %w", err)
		}
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredFnParams, Object: string(paramJSON)})
	}

	// Multi-step function
	if len(raw.Steps) > 0 {
		var stepNames []string
		for _, s := range raw.Steps {
			stepNames = append(stepNames, s.Name)
		}
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredFnPipelineOrder, Object: strings.Join(stepNames, ",")})

		for _, s := range raw.Steps {
			stepSubj := fmt.Sprintf("step:%s:%s", raw.Name, s.Name)
			ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredFnStep, Object: stepSubj})

			stepTriples, err := expandStep(s, raw.Name, raw.Agent, dir)
			if err != nil {
				return nil, fmt.Errorf("step %s: %w", s.Name, err)
			}
			ts = append(ts, stepTriples...)
		}
	} else {
		// Single-step function: store output and input at fn level,
		// and load the prompt sidecar.
		if raw.Output != "" {
			ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredFnOutput, Object: raw.Output})
		}

		// Output picker: store the original EDN source for the
		// :output-picker key so the graph loader can reparse it.
		// We re-serialize the decoded value back to EDN rather than
		// trying to extract the substring from the file. A marshal
		// failure here is a real problem — it means the function's
		// polymorphic dispatch will silently disappear from the
		// graph — so propagate it rather than swallowing.
		if raw.OutputPicker != nil {
			pickerEDN, err := ednencode.Marshal(raw.OutputPicker)
			if err != nil {
				return nil, fmt.Errorf(
					"function %q: serializing :output-picker for graph storage: %w",
					name, err)
			}
			ts = append(ts, triple.Triple{
				Subject:   subj,
				Predicate: kb.PredFnOutputPicker,
				Object:    string(pickerEDN),
			})
		}

		// Prompt: inline or .md sidecar
		prompt := raw.Prompt
		if prompt == "" {
			mdPath := filepath.Join(dir, name+".md")
			if md, err := os.ReadFile(mdPath); err == nil {
				prompt = string(md)
			}
		}
		if prompt != "" {
			ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredFnPrompt, Object: prompt})
		}
	}

	return ts, nil
}

// expandStep expands a single pipeline step into triples.
func expandStep(s ednStep, fnName string, fnAgent *ednAgentConfig, dir string) ([]triple.Triple, error) {
	stepSubj := fmt.Sprintf("step:%s:%s", fnName, s.Name)
	var ts []triple.Triple

	ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepName, Object: s.Name})
	ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepFunction, Object: fnName})

	if s.Input != "" {
		ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepInput, Object: s.Input})
	}
	if s.Output != "" {
		ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepOutput, Object: s.Output})
	}
	if s.OutputType != "" {
		ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepOutputType, Object: s.OutputType})
	}
	if s.Gate != "" {
		ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepGate, Object: s.Gate})
	}
	if s.Fn != "" {
		ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepComposedOf, Object: s.Fn})
	}
	if s.MapOver != "" {
		ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepMapOver, Object: s.MapOver})
	}

	// Step-level agent overrides function-level agent
	agent := s.Agent
	if agent == nil {
		agent = fnAgent
	}
	if agent != nil {
		if agent.Persona != "" {
			ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepPersona, Object: agent.Persona})
		}
		if agent.SystemPrompt != "" {
			ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepSystemPrompt, Object: agent.SystemPrompt})
		}
		if agent.ContextPolicy != "" {
			ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepContextPolicy, Object: agent.ContextPolicy})
		}
	}

	// Step requires as JSON
	if len(s.Requires) > 0 {
		reqJSON, err := json.Marshal(s.Requires)
		if err != nil {
			return nil, fmt.Errorf("marshal step requires: %w", err)
		}
		ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepRequires, Object: string(reqJSON)})
	}

	// Backend spec
	if s.BackendSpec != nil {
		backendJSON, err := json.Marshal(s.BackendSpec)
		if err != nil {
			return nil, fmt.Errorf("marshal backend spec: %w", err)
		}
		ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepBackend, Object: string(backendJSON)})
	}

	// Prompt: inline or .md sidecar
	prompt := s.Prompt
	if prompt == "" && s.Fn == "" {
		mdPath := filepath.Join(dir, fnName+"."+s.Name+".md")
		if md, err := os.ReadFile(mdPath); err == nil {
			prompt = string(md)
		}
	}
	if prompt != "" {
		ts = append(ts, triple.Triple{Subject: stepSubj, Predicate: kb.PredStepPrompt, Object: prompt})
	}

	return ts, nil
}

// --- Type expansion ---

// Type aliases for shared EDN type structs.
type ednChildren = ednformat.Children
type ednStructure = ednformat.Structure
type ednPredicates = ednformat.Predicates
type ednOrthographyBinding = ednformat.OrthographyBinding
type ednProjection = ednformat.Projection
type ednTypeDef = ednformat.TypeDef

// expandType parses an EDN type definition and returns triples.
func expandType(data []byte, name string) ([]triple.Triple, error) {
	var raw ednTypeDef
	if err := ednencode.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}

	subj := "type:" + raw.Name
	var ts []triple.Triple

	ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredTypeName, Object: raw.Name})

	if raw.Extends != "" {
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredTypeExtends, Object: raw.Extends})
	}

	if raw.Primitive {
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredTypePrimitive, Object: "true"})
	} else {
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredTypePrimitive, Object: "false"})
	}

	if raw.SchemaInstruction != "" {
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredTypeSchemaInstruction, Object: raw.SchemaInstruction})
	}

	for _, pred := range raw.Predicates.Required {
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredTypeRequiresPred, Object: pred})
	}
	for _, pred := range raw.Predicates.Optional {
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredTypeOptionalPred, Object: pred})
	}

	if raw.Structure.ParentType != "" {
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredTypeParentType, Object: raw.Structure.ParentType})
	}
	if raw.Structure.Children != nil {
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredTypeChildrenMin, Object: fmt.Sprintf("%d", raw.Structure.Children.Min)})
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredTypeChildrenMax, Object: fmt.Sprintf("%d", raw.Structure.Children.Max)})
	}

	for _, fm := range raw.Projection.Frontmatter {
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredTypeFrontmatter, Object: fm})
	}

	// Orthography as JSON (multiple bindings in one triple)
	if len(raw.Projection.Orthography) > 0 {
		orthJSON, err := json.Marshal(raw.Projection.Orthography)
		if err != nil {
			return nil, fmt.Errorf("marshal orthography: %w", err)
		}
		ts = append(ts, triple.Triple{Subject: subj, Predicate: kb.PredTypeOrthography, Object: string(orthJSON)})
	}

	return ts, nil
}
