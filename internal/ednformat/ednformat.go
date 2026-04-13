// Package ednformat defines the shared EDN struct types used for parsing
// .edn configuration files (functions and types). These structs are the
// canonical wire format for EDN → Go unmarshaling; each consumer package
// imports them instead of defining its own copies.
package ednformat

import "fmt"

// --- Function EDN structs ---

// AgentConfig is the EDN representation of agent configuration.
type AgentConfig struct {
	Persona       string   `edn:"persona,omitempty"`
	SystemPrompt  string   `edn:"system-prompt,omitempty"`
	Model         string   `edn:"model,omitempty"`
	ContextPolicy string   `edn:"context-policy,omitempty"`
	Exploration   string   `edn:"exploration,omitempty"`
	Capabilities  []string `edn:"capabilities,omitempty"`
}

// PathSpec is the EDN representation of a context path.
type PathSpec struct {
	Path        []string `edn:"path"`
	ExcludeSelf bool     `edn:"exclude-self,omitempty" json:"exclude-self,omitempty"`
	With        []string `edn:"with,omitempty"`
	As          string   `edn:"as"`
}

// Require is the EDN representation of a typed input requirement.
type Require struct {
	Role     string `edn:"role"`
	Type     string `edn:"type"`
	Optional bool   `edn:"optional,omitempty"`
	Ref      string `edn:"ref,omitempty"`
	As       string `edn:"as,omitempty"`
}

// DeterministicConfig is the EDN representation of deterministic backend config.
type DeterministicConfig struct {
	Mode            string `edn:"mode"`
	TitlePattern    string `edn:"title-pattern,omitempty" json:"title-pattern,omitempty"`
	Parent          string `edn:"parent,omitempty"`
	ParentTemplate  string `edn:"parent-template,omitempty" json:"parent-template,omitempty"`
	Target          string `edn:"target,omitempty"`
	Heading         string `edn:"heading,omitempty"`
	CreateIfMissing bool   `edn:"create-if-missing,omitempty" json:"create-if-missing,omitempty"`
}

// BackendSpec is the EDN representation of a step's backend configuration.
type BackendSpec struct {
	Kind   string               `edn:"kind"`
	Config *DeterministicConfig `edn:"config,omitempty"`
}

// Param is the EDN representation of a function parameter.
type Param struct {
	Name     string `edn:"name"`
	Required bool   `edn:"required,omitempty"`
	Default  string `edn:"default,omitempty"`
}

// Step is the EDN representation of a pipeline step.
type Step struct {
	Name        string       `edn:"name"`
	Prompt      string       `edn:"prompt"`
	Input       string       `edn:"input"`
	Output      string       `edn:"output"`
	OutputType  string       `edn:"output-type"`
	Gate        string       `edn:"gate"`
	Requires    []Require    `edn:"requires,omitempty"`
	Fn          string       `edn:"fn,omitempty"`
	MapOver     string       `edn:"map-over,omitempty"`
	Agent       *AgentConfig `edn:"agent,omitempty"`
	BackendSpec *BackendSpec `edn:"backend,omitempty"`
}

// Function is the EDN representation of a function definition.
type Function struct {
	Name         string       `edn:"name"`
	Description  string       `edn:"description"`
	Prompt       string       `edn:"prompt"`
	Input        string       `edn:"input"`
	Output       string       `edn:"output"`
	Steps        []Step       `edn:"steps"`
	Requires     []Require    `edn:"requires,omitempty"`
	Context      []PathSpec   `edn:"context,omitempty"`
	Agent        *AgentConfig `edn:"agent,omitempty"`
	Backend      string       `edn:"backend"`
	ContextFiles []string     `edn:"context-files"`
	CrossWalk    string       `edn:"cross-walk,omitempty"`
	AdHoc        bool         `edn:"ad-hoc,omitempty"`
	Params       []Param      `edn:"params,omitempty"`
}

// EffectiveSteps returns the pipeline steps, normalizing single-prompt into a one-step pipeline.
func (f *Function) EffectiveSteps() []Step {
	if len(f.Steps) > 0 {
		return f.Steps
	}
	return []Step{{Name: "default", Prompt: f.Prompt, Input: f.Input, Output: f.Output}}
}

// ValidateComposition checks step output/input chaining.
func (f *Function) ValidateComposition() error {
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

// --- Type EDN structs ---

// Children is the EDN representation of children constraints.
type Children struct {
	Min int `edn:"min"`
	Max int `edn:"max"`
}

// Structure is the EDN representation of structural constraints.
type Structure struct {
	ParentType string    `edn:"parent-type"`
	Children   *Children `edn:"children"`
}

// Predicates is the EDN representation of predicate constraints.
type Predicates struct {
	Required []string `edn:"required"`
	Optional []string `edn:"optional"`
}

// OrthographyBinding is the EDN representation of an orthography binding.
type OrthographyBinding struct {
	Signifier  string `edn:"signifier" json:"signifier"`
	ValueModel string `edn:"value-model" json:"value-model"`
}

// Projection is the EDN representation of projection mappings.
type Projection struct {
	Frontmatter []string                      `edn:"frontmatter"`
	Orthography map[string]OrthographyBinding `edn:"orthography"`
}

// GatherSpec is the EDN representation of a gather specification.
type GatherSpec struct {
	Target   bool `edn:"target"`
	Parent   bool `edn:"parent"`
	Children bool `edn:"children"`
	Siblings bool `edn:"siblings"`
	Subtree  bool `edn:"subtree"`
}

// TypeDef is the EDN representation of a type definition.
type TypeDef struct {
	Name              string      `edn:"name"`
	Extends           string      `edn:"extends"`
	Primitive         bool        `edn:"primitive"`
	ContextPolicy     bool        `edn:"context-policy"`
	Description       string      `edn:"description"`
	SchemaInstruction string      `edn:"schema-instruction"`
	Predicates        Predicates  `edn:"predicates"`
	Structure         Structure   `edn:"structure"`
	Projection        Projection  `edn:"projection"`
	Gather            *GatherSpec `edn:"gather"`
}
