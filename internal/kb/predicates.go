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
	PredSessionFocus   = "session/focus"
	PredSessionInclude = "session/include"
	PredSessionExclude = "session/exclude"
	PredSessionStarted = "session/started"
	PredSessionEnded   = "session/ended"
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
		{Name: PredSessionFocus, Multiplicity: graphops.Functional},
		{Name: PredSessionInclude, Multiplicity: graphops.Relational},
		{Name: PredSessionExclude, Multiplicity: graphops.Relational},
		{Name: PredSessionStarted, Multiplicity: graphops.Functional},
		{Name: PredSessionEnded, Multiplicity: graphops.Functional},

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
