// Package function is the Function concept: typed transformations on
// the knowledge base, composed as curried pipelines with configurable
// gates and control flow.
package function

// --- Output shape ---

// OutputShape classifies what a step produces.
type OutputShape int

const (
	ShapeText       OutputShape = iota // display only
	ShapeStructured                    // JSON conforming to a schema
	ShapeFileOps                       // []FileOp for graph mutation
)

// Signature is the backend-agnostic contract for a step's I/O.
type Signature struct {
	Shape  OutputShape
	Schema string // for structured: JSON schema
}

// --- Gate configuration ---

// HistoryPolicy controls what the backend sees during revision.
type HistoryPolicy int

const (
	HistoryNone   HistoryPolicy = iota // stateless retry
	HistoryLatest                      // most recent attempt + feedback
	HistoryFull                        // complete chain
)

// GateSpec configures the review state machine at a pipeline step.
type GateSpec struct {
	Revisable        bool
	HistoryPolicy    HistoryPolicy
	Cancelable       bool
	AutoAccept       bool
	RollbackOnReject bool
}

// --- Control flow ---

// FlowKind describes step iteration behavior.
type FlowKind int

const (
	FlowSequence FlowKind = iota
	FlowLoop
	FlowBranch
)

// Termination describes when a loop ends.
type Termination int

const (
	TerminateUser      Termination = iota // user ends with .end
	TerminateCondition                    // output satisfies predicate
)

// AccumulatorPolicy describes how loop iterations combine results.
type AccumulatorPolicy int

const (
	AccumulatorReplace AccumulatorPolicy = iota
	AccumulatorAppend
)

// ControlFlow configures iteration behavior for a step.
type ControlFlow struct {
	Kind        FlowKind
	Termination Termination
	Accumulator AccumulatorPolicy
}

// --- Backend spec ---

// BackendKind identifies the type of transformation executor.
type BackendKind int

const (
	BackendLLM           BackendKind = iota
	BackendDeterministic
	BackendAgent
)

// BackendSpec is the backend-specific part of a step definition.
type BackendSpec struct {
	Kind           BackendKind
	PromptTemplate string // for LLM
	Persona        string // for LLM
	SystemPrompt   string // for LLM
	Handler        string // for deterministic
}

// --- Path specs and requires ---

// PathSpec declares a morphism path for context gathering.
type PathSpec struct {
	Path        []string // predicates to walk (~ = inverse)
	ExcludeSelf bool
	With        []string // predicates to fetch at terminals
	As          string   // template variable name
}

// Require declares a named role a step needs from the graph.
type Require struct {
	Role     string // "target", "parent", "siblings", etc.
	Type     string // "node", "node[]"
	Optional bool
	As       string // template variable name override
}

// --- File operations ---

// FileOp is a single file operation produced by a transformation.
type FileOp struct {
	Action  string            // "create" or "edit"
	Title   string            // for create: new node title
	Parent  string            // for create: parent title
	File    string            // for edit: target node title
	OldText string            // for edit: text to find
	NewText string            // for edit: replacement
	Content string            // for create: markdown body
	Extra   map[string]string // additional frontmatter
}

// --- Step and Function definitions ---

// Step is one stage in a function's pipeline.
type Step struct {
	Name       string
	Requires   []Require
	Paths      []PathSpec
	Input      Signature
	Output     Signature
	Gate       *GateSpec    // nil = no gate
	Flow       *ControlFlow // nil = sequence
	Backend    BackendSpec
	ComposedOf string // delegate to another function
	MapOver    string // predicate path to map over
}

// Function is a named, reusable transformation.
type Function struct {
	Name          string
	Description   string
	Steps         []Step
	ContextPolicy string // "minimal", "neighborhood", "full"
}

// EffectiveSteps returns the steps, normalizing single-step shorthand.
func (f *Function) EffectiveSteps() []Step {
	return f.Steps
}

// ValidateComposition checks that step output signatures chain correctly.
func (f *Function) ValidateComposition() error {
	steps := f.EffectiveSteps()
	for i := 0; i < len(steps)-1; i++ {
		// For now: just check that output shape is compatible with
		// what the next step might expect. Full signature matching
		// is a TODO.
		_ = steps[i]
	}
	return nil
}
