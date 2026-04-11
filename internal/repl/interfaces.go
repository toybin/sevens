package repl

import (
	"sevens/internal/sevtypes"
)

// --------------------------------------------------------------------------
// Interfaces abstracting old-package functionality (apply, engine, graph,
// store) so that the repl package has zero imports of those packages.
// Implementations live in cmd/sevens (the CLI layer) and are injected via
// Option functions at construction time.
// --------------------------------------------------------------------------

// --- Graph queries (replaces graph + store imports) ---

// WalkNode is the REPL's view of a walked node.
type WalkNode struct {
	Subject      string
	Title        string
	Parent       *string
	Content      string
	Children     []string
	Siblings     []string
	CrossRefs    []string
	ChildRoles   map[string]string
	SiblingRoles map[string]string
	Role         string
	ContextFiles []string // from frontmatter
}

// WalkOutput wraps a walked node and any unvisited titles.
type WalkOutput struct {
	Node     WalkNode
	Unwalked []string
}

// OverviewNode is one node in the full tree.
type OverviewNode struct {
	Title      string
	Parent     *string
	Children   []string
	ChildCount int
	CrossRefs  []string
	CharCount  int
}

// OverviewOutput is the full tree.
type OverviewOutput struct {
	Nodes []OverviewNode
}

// BlockListEntry is one block in a block listing.
type BlockListEntry struct {
	Path      string
	Kind      string
	Text      string
	Level     int
	Signifier string
	Scope     []string
}

// BlockListOutput is the result of listing blocks.
type BlockListOutput struct {
	NodeTitle string
	FilePath  string
	Blocks    []BlockListEntry
}

// BlockTarget identifies a specific block for function targeting.
type BlockTarget struct {
	Subject   string
	NodeTitle string
	Path      string
	Kind      string
	Text      string
	Markdown  string
	Level     int
	Signifier string
	Scope     []string
}

// Label returns a human-readable label for the block target.
func (bt *BlockTarget) Label() string {
	if bt == nil {
		return ""
	}
	if bt.NodeTitle != "" && bt.Path != "" {
		return bt.NodeTitle + "#" + bt.Path
	}
	return bt.NodeTitle
}

// BlockDiffEntry is one entry in a block diff.
type BlockDiffEntry struct {
	OldPath  string
	NewPath  string
	OldText  string
	NewText  string
	OldScope []string
	NewScope []string
}

// BlockDiffOutput is the result of diffing blocks.
type BlockDiffOutput struct {
	NodeTitle    string
	FilePath     string
	Unchanged    []BlockDiffEntry
	Edited       []BlockDiffEntry
	ScopeChanged []BlockDiffEntry
	Reordered    []BlockDiffEntry
	Inserted     []BlockDiffEntry
	Deleted      []BlockDiffEntry
}

// InboxItemSummary describes one child in an inbox overview.
type InboxItemSummary struct {
	Title        string
	FilePath     string
	Kind         string
	CharCount    int
	BlockCount   int
	HeadingCount int
	BulletCount  int
	Empty        bool
	Error        string
}

// InboxOverview is the result of summarizing an inbox-like container.
type InboxOverview struct {
	NodeTitle string
	Items     []InboxItemSummary
}

// ExtractedNode is the result of preparing a block extraction.
type ExtractedNode struct {
	Title       string
	ParentTitle string
	SourceTitle string
	SourcePath  string
	SourceKind  string
	SourceScope []string
	Content     string
}

// GraphQuerier abstracts graph/store queries the REPL needs.
type GraphQuerier interface {
	// Node resolution
	ResolveTitle(title, root string) string
	ResolveNode(title, root string) (subject string, filePath string)
	GetObject(subject, predicate string) (string, error)
	NodeTitle(subject string) (string, error)
	ListNodeTitles(root string) ([]string, error)
	SearchTitles(query, root string) ([]string, error)
	SearchContent(query, root string) ([]string, error)

	// Walk / overview
	BuildWalk(root, title string, depth int) (*WalkOutput, error)
	BuildOverview(root string) (*OverviewOutput, error)

	// Block operations
	BuildBlockList(root, nodeTitle string) (BlockListOutput, error)
	BuildBlockDiff(root, nodeTitle string) (BlockDiffOutput, error)
	BuildInboxOverview(root, nodeTitle string) (InboxOverview, error)
	PrepareBlockExtraction(root, sourceTitle, blockPath, newTitle, parentTitle string) (ExtractedNode, error)
	ResolveBlockTarget(root, nodeTitle, blockPath string) (*BlockTarget, error)
	ResolveBlockTargetBySubject(subject string) (*BlockTarget, error)

	// Config
	LoadConfig(root string) (GraphConfig, error)
	AutoGroupIncludes(root, nodeTitle string) ([]string, error)
	ResolveGroup(root string, group GraphGroup) ([]string, error)

	// Sync
	Resync(root string) error

	// Rendering helpers
	ScopeString(scope []string) string
	RenderBlockMarkdown(block BlockListEntry) string
}

// GraphConfig is the REPL's view of a .sevens.edn config.
type GraphConfig struct {
	Groups map[string]GraphGroup
}

// GraphGroup is a named group from the config.
type GraphGroup struct {
	Root    string
	Exclude []string
	Nodes   []string
}

// --- Pipeline / engine (replaces engine + apply imports) ---

// FileOp is re-exported from sevtypes.
type FileOp = sevtypes.FileOp

// Suspension represents a paused pipeline.
type Suspension struct {
	Subject     string
	Root        string
	Function    string
	Target      string
	TargetLabel string
	BlockID     string
	BlockPath   string
	StepName    string
	StepIndex   int
	GateType    string
	Output      string
	OutputType  string
	Ops         []FileOp
	Summary     string
	Backend     string
}

// StepResult is the output of a pipeline step.
type StepResult struct {
	StepName   string
	Output     string
	OutputType string
	Ops        []FileOp
}

// PipelineConfig holds everything needed to run a pipeline.
type PipelineConfig struct {
	Root          string
	NodeTitle     string
	FunctionName  string
	TargetBlock   *BlockTarget
	StartStep     int
	PrevOutput    string
	DryRun        bool
	ModelOverride string
	BackendName   string
	Instruction   string
	ContextStr    string
	StreamText    bool
	Includes      []string
}

// PipelineResult represents either a suspension or a completed step.
type PipelineResult struct {
	Suspended  bool
	Suspension *Suspension
	Result     *StepResult
}

// LogEntry is the REPL's view of a log entry.
type LogEntry struct {
	Event        string
	Root         string
	Function     string
	Target       string
	Step         string
	Timestamp    string
	Commit       string
	Note         string
	Ops          []FileOp
	FilesCreated []string
	FilesEdited  []string
}

// ReviseConfig holds parameters for a revision.
type ReviseConfig struct {
	Root       string
	NodeTitle  string
	FuncName   string
	SusSubject string
	Feedback   string
	BackendName string
	ModelFlag  string
}

// ReviseResult holds the output of a revision step.
type ReviseResult struct {
	NewEntry   *LogEntry
	LLMOutput  string
	IsLast     bool
	OutputType string
	StepName   string
	TargetLabel string
}

// PipelineRunner abstracts the engine pipeline and suspension system.
type PipelineRunner interface {
	// Pipeline execution
	RunPipeline(cfg PipelineConfig) (*PipelineResult, error)

	// Suspension queries
	FindSuspension(root, nodeTitle string) (*Suspension, string, error)
	FindSuspensionBySubject(root, subject string) (*Suspension, error)
	FindSuspensions(root, nodeTitle string) ([]Suspension, error)
	ListSuspensions(root string) ([]Suspension, error)

	// Suspension lifecycle
	WriteSuspension(root, nodeTitle, targetLabel string, block *BlockTarget,
		function, step, gate, outputType, rawOutput string,
		stepIndex int, summary string, ops []FileOp, backendName string)
	ResolveSuspension(subject, status string) error

	// Revision
	ReviseStep(cfg ReviseConfig) (*ReviseResult, error)
}

// --- Apply operations (replaces apply imports) ---

// FunctionDef is the REPL's view of a function definition.
type FunctionDef struct {
	Name         string
	Description  string
	ContextFiles []string
}

// ApplyRunner abstracts apply package operations.
type ApplyRunner interface {
	// Function loading
	LoadFunction(name string) (*FunctionDef, error)
	ListFunctions() ([]FunctionDef, error)

	// Context
	LoadContextFiles(root string, paths []string) string

	// Ops
	ExecuteOps(ops []FileOp, root string) (created []string, edited []string, err error)

	// Log
	AppendLog(entry LogEntry) error
	ReadLog(root, nodeTitle string) ([]LogEntry, error)

	// Git
	RevertCommit(root, hash string) (newHash string, err error)

	// File helpers
	SanitizeFilename(title string) string
}

// --- Template operations (replaces apply template imports) ---

// NodeTemplate is the REPL's view of a template definition.
type NodeTemplate struct {
	Name            string
	Description     string
	Mode            string
	ParamNames      []string
}

// TemplatePreview is the REPL's view of a template preview.
type TemplatePreview struct {
	TemplateName    string
	Mode            string
	Draft           bool
	Missing         []string
	Title           string
	Parent          string
	BootstrapParent string
	TargetNode      string
	Heading         string
	CreateIfMissing bool
	Content         string
}

// TemplateResult is the REPL's view of a template execution result.
type TemplateResult struct {
	PrimaryTitle  string
	Created       []string
	Edited        []string
	CommitMessage string
}

// TemplateRunner abstracts template operations.
type TemplateRunner interface {
	LoadTemplate(name string) (*NodeTemplate, error)
	ListTemplates() ([]string, error)
	BindTemplateArgs(tmpl *NodeTemplate, args []string, vars map[string]string) map[string]string
	PreviewTemplate(root string, tmpl *NodeTemplate, parent, targetNode string, vars map[string]string) (*TemplatePreview, error)
	ExecuteTemplate(root string, tmpl *NodeTemplate, parent, targetNode string, vars map[string]string) (*TemplateResult, error)
}

// --- Config helpers (replaces store.ConfigDir, etc.) ---

// ConfigHelper abstracts config directory operations.
type ConfigHelper interface {
	ConfigDir() (string, error)
}
