package graph

// Group defines a subgraph that can be included as context.
// Root + all recursive descendants are included, minus excluded subtrees.
// Nodes lists additional singleton nodes to include outside the subtree.
type Group struct {
	Root    string   `edn:"root"`
	Exclude []string `edn:"exclude"`
	Nodes   []string `edn:"nodes"` // extra singleton nodes outside the subtree
}

type Config struct {
	Path     string           `edn:"path"`
	Alias    string           `edn:"alias"`
	MaxChars *int             `edn:"max-chars"`
	Groups   map[string]Group `edn:"groups"`
}

type Frontmatter struct {
	Title        string   `yaml:"title"`
	Parent       string   `yaml:"parent"`
	MaxChars     *int     `yaml:"max-chars"`
	ContextFiles []string `yaml:"context-files"`
	SiblingRole  string   `yaml:"sibling-role"`
	IncludeGroup bool     `yaml:"include-group"`
}

type ParsedNode struct {
	Title        string
	Parent       *string
	FilePath     string
	Content      string
	MaxChars     *int
	ContextFiles []string
	CrossRefs    []string
	SiblingRole  string
	IncludeGroup bool
	Blocks       []ParsedBlock
}

type ParsedBlock struct {
	Path         string
	Kind         string
	Text         string
	Level        int
	Signifier    string
	Tags         []string
	HeadingChain []string
	AnchorHashes []string
	TextHash     uint64
	ScopeHash    uint64
	TagHash      uint64
	AnchorCount  uint8
	Anchors      [4]uint64
}

type ValidationReport struct {
	Orphans          []string `edn:"orphans"`
	MissingParents   []string `edn:"missing-parents"`
	DuplicateTitles  []string `edn:"duplicate-titles"`
	Overflow         []string `edn:"overflow"`
	LengthViolations []string `edn:"length-violations"`
}

type OverviewNode struct {
	Title      string   `edn:"title"`
	Parent     *string  `edn:"parent"`
	Children   []string `edn:"children"`
	ChildCount int      `edn:"child-count"`
	CrossRefs  []string `edn:"cross-refs"`
	CharCount  int      `edn:"char-count,omitempty"`
	Pending    string   `edn:"pending,omitempty"`
}

type OverviewOutput struct {
	Nodes      []OverviewNode   `edn:"nodes"`
	Validation ValidationReport `edn:"validation"`
}

type WalkNode struct {
	Subject      string            `edn:"-"`
	Title        string            `edn:"title"`
	Parent       *string           `edn:"parent"`
	Content      string            `edn:"content"`
	Children     []string          `edn:"children"`
	Siblings     []string          `edn:"siblings"`
	CrossRefs    []string          `edn:"cross-refs"`
	ContextFiles []string          `edn:"-"`
	ChildRoles   map[string]string `edn:"-"` // child title → sibling/role (if any)
	SiblingRoles map[string]string `edn:"-"` // sibling title → sibling/role (if any)
	Role         string            `edn:"-"` // this node's own sibling/role (if any)
}

type WalkOutput struct {
	Node     WalkNode `edn:"node"`
	Unwalked []string `edn:"unwalked"`
}
