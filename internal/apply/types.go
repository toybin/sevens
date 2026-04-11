package apply

import (
	"fmt"

	"sevens/internal/config"
)

// PathSpec declares a morphism path to traverse from the target node.
type PathSpec struct {
	Path        []string `edn:"path"`                   // predicates to compose; "~" suffix = inverse
	ExcludeSelf bool     `edn:"exclude-self,omitempty"` // remove starting node from results
	With        []string `edn:"with,omitempty"`         // additional predicates to fetch from terminal nodes
	As          string   `edn:"as"`                     // template variable name for results
}

// Require declares a structural role a function needs resolved from the graph.
type Require struct {
	Role     string `edn:"role"`               // "target", "parent", "siblings", "children", "child[N]"
	Type     string `edn:"type"`               // "node", "node[]"
	Optional bool   `edn:"optional,omitempty"` // if true, missing is not an error
	Ref      string `edn:"ref,omitempty"`      // reference to another step's output (for pipelines)
	As       string `edn:"as,omitempty"`       // template variable name override
}

// AgentConfig defines the persona and context policy for a function or step.
type AgentConfig struct {
	Persona        string   `edn:"persona,omitempty"`          // named persona (for display/logging)
	SystemPrompt   string   `edn:"system-prompt,omitempty"`    // override system prompt for this function/step
	Model          string   `edn:"model,omitempty"`            // model tier ("fast", "capable", "powerful") or raw model ID
	ContextPolicy  string   `edn:"context-policy,omitempty"`   // "minimal", "neighborhood", "full", "cached", "custom"
	Exploration    string   `edn:"exploration,omitempty"`      // "closed" (default), "scoped"
	Capabilities   []string `edn:"capabilities,omitempty"`     // MCP server names needed for this function
	AllowFileReads bool     `edn:"allow-file-reads,omitempty"` // if true, CLI may read explicitly-referenced files
	ReadOnly       bool     `edn:"read-only,omitempty"`        // if true, CLI should not write files
}

// Step is one stage in a function's pipeline.
type Step struct {
	Name     string       `edn:"name"`
	Prompt   string       `edn:"prompt"`             // inline prompt OR loaded from .md file
	Input    string       `edn:"input"`              // "node", "suggestions", "ops", "text"
	Output   string       `edn:"output"`             // "suggestions", "ops", "text"
	Gate     string       `edn:"gate"`               // "" (no gate, auto-advance) or "approve"
	Requires []Require    `edn:"requires,omitempty"` // typed inputs for this step
	Fn       string       `edn:"fn,omitempty"`       // delegate to another function by name
	MapOver  string       `edn:"map-over,omitempty"` // predicate path to map over (e.g., "node/parent~")
	Agent    *AgentConfig `edn:"agent,omitempty"`    // per-step agent override
}

// Function is a named, reusable LLM-powered transformation defined as an EDN file.
// A function with Steps uses the pipeline. A function with just Prompt is a single-step shorthand.
type Function struct {
	Name         string       `edn:"name"`
	Description  string       `edn:"description"`
	Prompt       string       `edn:"prompt"`             // single-step shorthand
	Input        string       `edn:"input"`              // single-step input type
	Output       string       `edn:"output"`             // single-step output type
	Steps        []Step       `edn:"steps"`              // multi-step pipeline
	Requires     []Require    `edn:"requires,omitempty"` // function-level requires (applied to all steps)
	Context      []PathSpec   `edn:"context,omitempty"`  // morphism paths for context gathering
	Agent        *AgentConfig `edn:"agent,omitempty"`    // function-level agent config
	Backend      string       `edn:"backend"`
	ContextFiles []string     `edn:"context-files"`        // extra files to inject into prompt
	CrossWalk    string       `edn:"cross-walk,omitempty"` // name of another function whose output to inject
	AdHoc        bool         `edn:"ad-hoc,omitempty"`     // if true, function accepts an arbitrary instruction via {{instruction}}
}

// EffectiveAgent returns the agent config for a step, falling back to the function-level config.
func EffectiveAgent(fn *Function, step *Step) *AgentConfig {
	if step.Agent != nil {
		return step.Agent
	}
	return fn.Agent
}

// ResolvedNode holds a fetched node's content for template rendering.
type ResolvedNode struct {
	Title   string
	Content string
	Role    string // sibling/role if any
}

type ResolvedBlock struct {
	ID        string
	Path      string
	Kind      string
	Text      string
	Markdown  string
	Signifier string
	Scope     []string
}

// ResolvedContext holds all resolved inputs for a function step.
type ResolvedContext struct {
	Target          ResolvedNode // the focused node
	NodeTitle       string
	NodeContent     string
	TargetKind      string
	TargetLabel     string
	Block           *ResolvedBlock
	Parent          *ResolvedNode  // parent node content (nil if root or not requested)
	Siblings        []ResolvedNode // sibling nodes with content
	Children        []ResolvedNode // child nodes with content
	ChildTitles     []string       // child titles from walk (always available, even without content)
	SiblingTitles   []string       // sibling titles from walk (always available)
	History         []LogEntry     // prior function applications on this node
	Prev            string         // previous step output
	CrossWalkOutput string         // output from a different function's last run (cross-walk)
	Instruction     string         // ad-hoc instruction passed at invocation time ({{instruction}})
}

// EffectiveSteps returns the pipeline steps, converting a single-prompt function into a one-step pipeline.
func (f *Function) EffectiveSteps() []Step {
	if len(f.Steps) > 0 {
		return f.Steps
	}
	return []Step{{Name: "default", Prompt: f.Prompt, Input: f.Input, Output: f.Output}}
}

// ValidateComposition checks that step outputs feed into the next step's expected input.
func (f *Function) ValidateComposition() error {
	steps := f.EffectiveSteps()
	for i := 1; i < len(steps); i++ {
		prev := steps[i-1]
		curr := steps[i]
		// Skip type checking for composed steps — they don't chain output/input directly
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

// FileOp is a single file operation returned by the LLM as structured output.
type FileOp struct {
	Action           string            `json:"action" edn:"action"`               // "create" or "edit"
	Title            string            `json:"title,omitempty" edn:"title"`       // for "create": new node title
	Parent           string            `json:"parent,omitempty" edn:"parent"`     // for "create": parent node title
	File             string            `json:"file,omitempty" edn:"file"`         // for "edit": node title of file to edit
	OldText          string            `json:"old_text,omitempty" edn:"old-text"` // for "edit": exact text to find
	NewText          string            `json:"new_text,omitempty" edn:"new-text"` // for "edit": replacement text
	Content          string            `json:"content,omitempty" edn:"content"`   // for "create": markdown body
	ExtraFrontmatter map[string]string `json:"-" edn:"-"`                         // internal: extra frontmatter fields from templates
}

// LogEntry is one event in the append-only log for a node.
type LogEntry struct {
	Event        string   `edn:"event"`
	Root         string   `edn:"root,omitempty"`
	Function     string   `edn:"function,omitempty"`
	Target       string   `edn:"target,omitempty"`
	Step         string   `edn:"step,omitempty"`       // which pipeline step produced this
	StepIndex    int      `edn:"step-index,omitempty"` // 0-based index in the pipeline
	Timestamp    string   `edn:"timestamp"`
	Ops          []FileOp `edn:"ops,omitempty"`        // only on final step
	RawOutput    string   `edn:"raw-output,omitempty"` // LLM output for {{prev}} in next step
	Commit       string   `edn:"commit,omitempty"`
	Note         string   `edn:"note,omitempty"`
	FilesCreated []string `edn:"files-created,omitempty"`
	FilesEdited  []string `edn:"files-edited,omitempty"`
	Summary      string   `edn:"summary,omitempty"` // brief description of what was suggested
}

// NodeTemplate defines a structural pattern for creating nodes.
type NodeTemplate struct {
	Name              string             `edn:"name"`
	Description       string             `edn:"description,omitempty"`
	Mode              string             `edn:"mode,omitempty"`
	TitlePattern      string             `edn:"title-pattern"`                 // e.g., "{{date}}" or "{{topic}}: Pros"
	DraftTitlePattern string             `edn:"draft-title-pattern,omitempty"` // fallback title when required params are missing
	ParentPattern     string             `edn:"parent-pattern,omitempty"`      // parent title pattern
	ParentTemplate    string             `edn:"parent-template,omitempty"`     // create parent from this template if missing
	Target            *TemplateTarget    `edn:"target,omitempty"`
	Placement         *TemplatePlacement `edn:"placement,omitempty"`
	Type              string             `edn:"type,omitempty"`         // node type for frontmatter (e.g., "journal", "entry")
	Content           string             `edn:"content,omitempty"`      // scaffolding content
	SiblingRole       string             `edn:"sibling-role,omitempty"` // role of this node among its siblings
	Params            []TemplateParam    `edn:"params,omitempty"`
	Draft             *TemplateDraft     `edn:"draft,omitempty"`
	CommitMessage     string             `edn:"commit-message,omitempty"`
	Children          []NodeTemplate     `edn:"children,omitempty"` // child nodes to create (subtree template)
}

type TemplateTarget struct {
	Root   string `edn:"root,omitempty"`
	Parent string `edn:"parent,omitempty"`
	Node   string `edn:"node,omitempty"`
}

type TemplatePlacement struct {
	Kind            string `edn:"kind,omitempty"`
	Heading         string `edn:"heading,omitempty"`
	HeadingLevel    int    `edn:"heading-level,omitempty"`
	CreateIfMissing bool   `edn:"create-if-missing,omitempty"`
}

type TemplateParam struct {
	Name     string `edn:"name"`
	Required bool   `edn:"required,omitempty"`
	Default  string `edn:"default,omitempty"`
}

type TemplateDraft struct {
	WhenMissingParams bool `edn:"when-missing-params,omitempty"`
	Open              bool `edn:"open,omitempty"`
}

// LLMConfig is an alias for config.LLMConfig.
// Canonical definition lives in the config package.
type LLMConfig = config.LLMConfig

// BackendConfig is an alias for config.BackendConfig.
type BackendConfig = config.BackendConfig

// GlobalConfig is an alias for config.GlobalConfig.
type GlobalConfig = config.GlobalConfig
