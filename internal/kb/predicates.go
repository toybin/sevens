// Package kb is Layer 3: the sevens knowledge base.
//
// Imposes the PKM domain model onto a graph of triples: subject
// identity, predicate vocabulary, structural queries, validation,
// session/focus, and logging.
package kb

import "sevens/internal/graphops"

// --- Predicate vocabulary ---
//
// Single source of truth for all sevens predicate strings.
// No predicate string literals anywhere else in the codebase.

// Node predicates
const (
	PredNodeTitle     = "node/title"
	PredNodeParent    = "node/parent"
	PredNodeContent   = "node/content"
	PredNodeFile      = "node/file"
	PredNodeRoot      = "node/root"
	PredNodeCharCount = "node/char-count"
	PredNodeLink      = "node/link"
	PredNodeRole      = "node/role"
)

// Block predicates -- same entities, different namespace prefix.
// The namespace labels the compositional role.
const (
	PredBlockNode    = "block/node"
	PredBlockRoot    = "block/root"
	PredBlockContent = "block/content"
	PredBlockScope   = "block/scope"
	PredBlockKind    = "block/kind"
	PredBlockPath    = "block/path"
)

// Session predicates
const (
	PredSessionRoot    = "session/root"
	PredSessionFocus   = "session/focus"
	PredSessionInclude = "session/include"
	PredSessionExclude = "session/exclude"
	PredSessionStarted = "session/started"
	PredSessionEnded   = "session/ended"

	// CurrentSessionSubject is the well-known subject for the active session.
	// Only one session is active at a time.
	CurrentSessionSubject = "session:current"
)

// Function predicates
const (
	PredFnName          = "fn/name"
	PredFnDescription   = "fn/description"
	PredFnOutput        = "fn/output"
	PredFnPersona       = "fn/persona"
	PredFnSystemPrompt  = "fn/system-prompt"
	PredFnContextPolicy = "fn/context-policy"
	PredFnContextSpecs  = "fn/context-specs"
	PredFnRequires      = "fn/requires"
	PredFnPrompt        = "fn/prompt"
	PredFnStep          = "fn/step"
	PredFnPipelineOrder = "fn/pipeline-order"
	PredFnBackend       = "fn/backend"
	PredFnParams        = "fn/params"
)

// Step predicates
const (
	PredStepName       = "step/name"
	PredStepFunction   = "step/function"
	PredStepInput      = "step/input"
	PredStepOutput     = "step/output"
	PredStepOutputType = "step/output-type"
	PredStepGate       = "step/gate"
	PredStepPrompt     = "step/prompt"
	PredStepComposedOf = "step/composed-of"
	PredStepMapOver    = "step/map-over"
	PredStepBackend    = "step/backend"
	PredStepPersona    = "step/persona"
	PredStepSystemPrompt = "step/system-prompt"
	PredStepContextPolicy = "step/context-policy"
	PredStepRequires   = "step/requires"
)

// Type predicates
const (
	PredTypeName              = "type/name"
	PredTypeExtends           = "type/extends"
	PredTypePrimitive         = "type/primitive"
	PredTypeRequiresPred      = "type/requires-pred"
	PredTypeOptionalPred      = "type/optional-pred"
	PredTypeParentType        = "type/parent-type"
	PredTypeChildrenMin       = "type/children-min"
	PredTypeChildrenMax       = "type/children-max"
	PredTypeFrontmatter       = "type/frontmatter"
	PredTypeSchemaInstruction = "type/schema-instruction"
	PredTypeOrthography       = "type/orthography"
)

// Log predicates
const (
	PredLogEvent        = "log/event"
	PredLogRoot         = "log/root"
	PredLogFunction     = "log/function"
	PredLogNode         = "log/node"
	PredLogStep         = "log/step"
	PredLogStepIndex    = "log/step-index"
	PredLogTimestamp     = "log/timestamp"
	PredLogSession      = "log/session"
	PredLogResult       = "log/result"
	PredLogCommit       = "log/commit"
	PredLogNote         = "log/note"
	PredLogFilesCreated = "log/files-created"
	PredLogFilesEdited  = "log/files-edited"
)

// allSpecs returns the predicate specifications to register with
// the graph layer during KB initialization.
func allSpecs() []graphops.PredicateSpec {
	return []graphops.PredicateSpec{
		// Node predicates
		{Name: PredNodeTitle, Multiplicity: graphops.Functional},
		{Name: PredNodeParent, Multiplicity: graphops.Functional, Inverse: "children"},
		{Name: PredNodeContent, Multiplicity: graphops.Functional},
		{Name: PredNodeFile, Multiplicity: graphops.Functional},
		{Name: PredNodeRoot, Multiplicity: graphops.Functional},
		{Name: PredNodeCharCount, Multiplicity: graphops.Functional},
		{Name: PredNodeLink, Multiplicity: graphops.Relational},
		{Name: PredNodeRole, Multiplicity: graphops.Relational},

		// Block predicates
		{Name: PredBlockNode, Multiplicity: graphops.Functional},
		{Name: PredBlockRoot, Multiplicity: graphops.Functional},
		{Name: PredBlockContent, Multiplicity: graphops.Functional},
		{Name: PredBlockScope, Multiplicity: graphops.Functional},
		{Name: PredBlockKind, Multiplicity: graphops.Functional},
		{Name: PredBlockPath, Multiplicity: graphops.Functional},

		// Session predicates
		{Name: PredSessionRoot, Multiplicity: graphops.Functional},
		{Name: PredSessionFocus, Multiplicity: graphops.Functional},
		{Name: PredSessionInclude, Multiplicity: graphops.Relational},
		{Name: PredSessionExclude, Multiplicity: graphops.Relational},
		{Name: PredSessionStarted, Multiplicity: graphops.Functional},
		{Name: PredSessionEnded, Multiplicity: graphops.Functional},

		// Function predicates
		{Name: PredFnName, Multiplicity: graphops.Functional},
		{Name: PredFnDescription, Multiplicity: graphops.Functional},
		{Name: PredFnOutput, Multiplicity: graphops.Functional},
		{Name: PredFnPersona, Multiplicity: graphops.Functional},
		{Name: PredFnSystemPrompt, Multiplicity: graphops.Functional},
		{Name: PredFnContextPolicy, Multiplicity: graphops.Functional},
		{Name: PredFnContextSpecs, Multiplicity: graphops.Functional},
		{Name: PredFnRequires, Multiplicity: graphops.Functional},
		{Name: PredFnPrompt, Multiplicity: graphops.Functional},
		{Name: PredFnStep, Multiplicity: graphops.Relational},
		{Name: PredFnPipelineOrder, Multiplicity: graphops.Functional},
		{Name: PredFnBackend, Multiplicity: graphops.Functional},
		{Name: PredFnParams, Multiplicity: graphops.Functional},

		// Step predicates
		{Name: PredStepName, Multiplicity: graphops.Functional},
		{Name: PredStepFunction, Multiplicity: graphops.Functional},
		{Name: PredStepInput, Multiplicity: graphops.Functional},
		{Name: PredStepOutput, Multiplicity: graphops.Functional},
		{Name: PredStepOutputType, Multiplicity: graphops.Functional},
		{Name: PredStepGate, Multiplicity: graphops.Functional},
		{Name: PredStepPrompt, Multiplicity: graphops.Functional},
		{Name: PredStepComposedOf, Multiplicity: graphops.Functional},
		{Name: PredStepMapOver, Multiplicity: graphops.Functional},
		{Name: PredStepBackend, Multiplicity: graphops.Functional},
		{Name: PredStepPersona, Multiplicity: graphops.Functional},
		{Name: PredStepSystemPrompt, Multiplicity: graphops.Functional},
		{Name: PredStepContextPolicy, Multiplicity: graphops.Functional},
		{Name: PredStepRequires, Multiplicity: graphops.Functional},

		// Type predicates
		{Name: PredTypeName, Multiplicity: graphops.Functional},
		{Name: PredTypeExtends, Multiplicity: graphops.Functional},
		{Name: PredTypePrimitive, Multiplicity: graphops.Functional},
		{Name: PredTypeRequiresPred, Multiplicity: graphops.Relational},
		{Name: PredTypeOptionalPred, Multiplicity: graphops.Relational},
		{Name: PredTypeParentType, Multiplicity: graphops.Functional},
		{Name: PredTypeChildrenMin, Multiplicity: graphops.Functional},
		{Name: PredTypeChildrenMax, Multiplicity: graphops.Functional},
		{Name: PredTypeFrontmatter, Multiplicity: graphops.Relational},
		{Name: PredTypeSchemaInstruction, Multiplicity: graphops.Functional},
		{Name: PredTypeOrthography, Multiplicity: graphops.Functional},

		// Log predicates (all functional per entry)
		{Name: PredLogEvent, Multiplicity: graphops.Functional},
		{Name: PredLogRoot, Multiplicity: graphops.Functional},
		{Name: PredLogFunction, Multiplicity: graphops.Functional},
		{Name: PredLogNode, Multiplicity: graphops.Functional},
		{Name: PredLogStep, Multiplicity: graphops.Functional},
		{Name: PredLogStepIndex, Multiplicity: graphops.Functional},
		{Name: PredLogTimestamp, Multiplicity: graphops.Functional},
		{Name: PredLogSession, Multiplicity: graphops.Functional},
		{Name: PredLogResult, Multiplicity: graphops.Functional},
		{Name: PredLogCommit, Multiplicity: graphops.Functional},
		{Name: PredLogNote, Multiplicity: graphops.Functional},
		{Name: PredLogFilesCreated, Multiplicity: graphops.Relational},
		{Name: PredLogFilesEdited, Multiplicity: graphops.Relational},
	}
}
