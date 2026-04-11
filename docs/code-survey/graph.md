# Code Survey: `internal/graph`

Survey date: 2026-04-10

---

## Package overview

`internal/graph` is the core domain package for the sevens system. It owns:

- Data type definitions for nodes, blocks, and configuration
- Markdown file parsing into typed structs
- Serialization of parsed data into triples for the store
- Graph queries (overview, walk, group resolution, validation)
- Block-level diffing and identity tracking
- File-level edit preparation (append, insert under heading, block extraction)
- Inbox summarization
- Two experimental/prototype subsystems: construction diffing and AST rematching

The package's dependency direction is: `graph` → `store` (the SQLite triple layer). Callers sit above `graph` in the `cmd/` layer.

---

## Exported types

### `types.go`

**`Config`** — Root-level configuration, loaded from `.sevens.edn`.
```
Path     string
Alias    string
MaxChars *int
Groups   map[string]Group
```

**`Group`** — A named subgraph definition used for context assembly.
```
Root    string    // subtree root node title
Exclude []string  // subtree roots to prune
Nodes   []string  // additional singleton node titles
```

**`Frontmatter`** — YAML frontmatter parsed from a markdown file.
```
Title        string
Parent       string
MaxChars     *int
ContextFiles []string
SiblingRole  string
IncludeGroup bool
```

**`ParsedNode`** — A fully parsed markdown file, after frontmatter and body extraction.
```
Title        string
Parent       *string
FilePath     string
Content      string
MaxChars     *int
ContextFiles []string
CrossRefs    []string   // wiki-link targets extracted from body
SiblingRole  string
IncludeGroup bool
Blocks       []ParsedBlock
```

**`ParsedBlock`** — A single structural unit within a node's body (heading, paragraph, list item, task).
```
Path         string     // dotted path e.g. "0.1.2"
Kind         string     // "heading", "paragraph", "list-item", "task"
Text         string
Level        int        // heading level (1–6); 0 for non-headings
Signifier    string     // task signifier e.g. "", "x", "-"
Tags         []string
HeadingChain []string   // ancestor heading texts
AnchorHashes []string   // short SHA1 shingles for fuzzy matching
TextHash     uint64
ScopeHash    uint64
TagHash      uint64
AnchorCount  uint8
Anchors      [4]uint64
```

**`ValidationReport`** — Output of graph validation checks.
```
Orphans          []string
MissingParents   []string
DuplicateTitles  []string
Overflow         []string   // nodes with >9 children
LengthViolations []string   // nodes exceeding max-chars
```

**`OverviewNode`** — A single node's metadata in a full-graph overview.
```
Title      string
Parent     *string
Children   []string
ChildCount int
CrossRefs  []string
CharCount  int
Pending    string   // pending suspension function name, if any
```

**`OverviewOutput`** — Full graph overview returned to callers.
```
Nodes      []OverviewNode
Validation ValidationReport
```

**`WalkNode`** — A single node's full context for a "walk" operation.
```
Subject      string              // triple-store subject URI (not serialized)
Title        string
Parent       *string
Content      string
Children     []string
Siblings     []string
CrossRefs    []string
ContextFiles []string            // not serialized
ChildRoles   map[string]string   // child title → sibling-role (not serialized)
SiblingRoles map[string]string   // sibling title → sibling-role (not serialized)
Role         string              // this node's own sibling-role (not serialized)
```

**`WalkOutput`** — Result of a walk operation.
```
Node     WalkNode
Unwalked []string   // all other node titles in the root
```

---

### `blockdiff.go`

**`ParsedBlockChange`** — A matched pair of old/new block paths.
```
OldPath string
NewPath string
```

**`ParsedBlockDiff`** — The classified result of diffing two block lists.
```
Unchanged    []ParsedBlockChange
Edited       []ParsedBlockChange
ScopeChanged []ParsedBlockChange
Reordered    []ParsedBlockChange
Inserted     []string
Deleted      []string
```

---

### `blockinspect.go`

**`BlockDiffEntry`** — A single block's before/after detail in a rich diff output.
```
Subject  string
Kind     string
OldPath  string
NewPath  string
OldText  string
NewText  string
OldScope []string
NewScope []string
```

**`BlockDiffOutput`** — Full diff output for a node, comparing the store to the current file.
```
NodeTitle    string
FilePath     string
Unchanged    []BlockDiffEntry
Edited       []BlockDiffEntry
ScopeChanged []BlockDiffEntry
Reordered    []BlockDiffEntry
Inserted     []BlockDiffEntry
Deleted      []BlockDiffEntry
```

---

### `workflow.go`

**`InboxItemSummary`** — Summary of a child node as seen from the inbox.
```
Title        string
FilePath     string
Kind         string   // "note", "capture", "discussion", "date", "empty", "empty-date", "error"
CharCount    int
BlockCount   int
HeadingCount int
BulletCount  int
Empty        bool
Error        string
```

**`InboxOverview`** — The inbox node with summarized children.
```
NodeTitle string
Items     []InboxItemSummary
```

**`BlockListEntry`** — A block as presented in a listing (scope already computed).
```
Path      string
Kind      string
Text      string
Level     int
Signifier string
Scope     []string
```

**`BlockListOutput`** — All blocks in a node, ready to display.
```
NodeTitle string
FilePath  string
Blocks    []BlockListEntry
```

**`BlockTarget`** — A fully resolved block with rendered markdown.
```
Subject    string
NodeTitle  string
Path       string
Kind       string
Text       string
Markdown   string
Level      int
Signifier  string
Scope      []string
```

**`NodeEdit`** — A proposed file edit (old/new text diff for application by caller).
```
NodeTitle string
FilePath  string
OldText   string
NewText   string
```

**`ExtractedNode`** — A proposed new node derived from extracting a block.
```
Title       string
ParentTitle string
SourceTitle string
SourcePath  string
SourceKind  string
SourceScope []string
Content     string
```

---

### `construction.go`

**`ConstructionNode`** — A minimal node for testing structural diff behavior across snapshots.
```
ID        string
ParentID  string
PrevID    string
InboundID string
```

**`ConstructionDiff`** — Classification of how stable node IDs changed between two snapshots.
```
Unchanged      []string
Inserted       []string
Deleted        []string
Reparented     []string
Reordered      []string
InboundChanged []string
```

---

### `searchmatch.go`

**`SearchASTNode`** — A toy AST node for prototype rematching.
```
StableID  string
Kind      string
Label     string
Children  []*SearchASTNode
```

**`SearchMatchResult`** — Maps old stable IDs to paths in a new tree.
```
Matches map[string]string
```

---

## Exported functions

### File and configuration loading (`sync.go`)

```go
func ExpandTilde(path string) string
```
Expands a leading `~/` to the user's home directory.

```go
func LoadConfig(root string) (Config, error)
```
Reads and parses `.sevens.edn` from `root`. Expands tilde in the path field.

```go
func FindRoot(dir string) (string, error)
```
Walks up the directory tree from `dir` to find the nearest `.sevens.edn`.

```go
func ScanFiles(root string) ([]string, error)
```
Returns all `.md` file paths under `root`, skipping `.git`.

```go
func ParseAllFiles(files []string) (nodes []ParsedNode, duplicates []string)
```
Reads and parses each file. Skips files without a title. Returns deduplicated nodes and a list of any duplicate titles encountered.

```go
func Validate(db *sql.DB, root string, config Config) (ValidationReport, error)
```
Checks the stored graph for orphans, missing parents, overflow (>9 children), and length violations.

```go
func PrintValidationReport(report ValidationReport, nodeCount int)
```
Writes a human-readable validation summary to stderr.

---

### Triple serialization (`triples.go`)

```go
func NodeToTriples(node ParsedNode, root string, blockSubjects ...map[string]string) []store.Triple
```
Converts a `ParsedNode` into all its triples. Accepts an optional pre-computed block subject map; otherwise derives block subjects from content.

```go
func BlockToTriples(node ParsedNode, block ParsedBlock, root string, subject string) []store.Triple
```
Converts a single `ParsedBlock` into triples. Requires the block's stable subject string.

```go
func RootConfigToTriples(config Config, rootPath string) []store.Triple
```
Converts a `Config` into root-level triples.

```go
func PopulateTriples(db *sql.DB, root string, nodes []ParsedNode, config Config) error
```
The main write path: clears all existing triples for `root`, then inserts fresh triples for the config and all provided nodes. Handles block subject assignment (stable ID tracking) internally.

---

### Graph queries (`query.go`)

```go
func BuildOverview(db *sql.DB, root string, config Config) (*OverviewOutput, error)
```
Returns the full node list with parent/child/cross-ref metadata and a validation report.

```go
func BuildWalk(db *sql.DB, root string, title string, depth int) (*WalkOutput, error)
```
Returns full context for a single node: content, parent, children (if `depth >= 1`), siblings, cross-refs, context files, and sibling roles. Also returns all unwalked titles.

```go
func ResolveGroup(db *sql.DB, root string, group Group) ([]string, error)
```
Returns all node titles in a `Group`: root + recursive descendants minus excluded subtrees, plus any singleton extras.

```go
func AutoGroupIncludes(db *sql.DB, root string, nodeTitle string, config Config) ([]string, error)
```
If a node has `include-group: true` in its frontmatter and is the root of a configured group, returns the group's member titles (excluding the node itself). Returns `nil` otherwise.

---

### Block-level diffing (`blockdiff.go` + `blockinspect.go`)

```go
func DiffParsedBlocks(oldBlocks, newBlocks []ParsedBlock) ParsedBlockDiff
```
Compares two block lists using fuzzy matching (shingle anchors + text hashes). Classifies each block as unchanged, edited, scope-changed, reordered, inserted, or deleted.

```go
func BuildBlockDiff(db *sql.DB, root, nodeTitle string) (BlockDiffOutput, error)
```
Loads the stored blocks for a node from the DB and the current file from disk, runs `DiffParsedBlocks`, and returns a rich `BlockDiffOutput` with full text and scope fields.

```go
func ScopeString(scope []string) string
```
Joins a heading chain into a human-readable `"A > B > C"` string.

---

### Block inspection and editing (`workflow.go`)

```go
func BuildBlockList(db *sql.DB, root, nodeTitle string) (BlockListOutput, error)
```
Parses the current file and returns all blocks with their visible scope.

```go
func ResolveBlockTarget(db *sql.DB, root, nodeTitle, blockPath string) (*BlockTarget, error)
```
Looks up a block by node title + dotted path, returns full detail including rendered markdown.

```go
func ResolveBlockTargetBySubject(db *sql.DB, subject string) (*BlockTarget, error)
```
Same as `ResolveBlockTarget` but by triple-store subject URI.

```go
func RenderBlockMarkdown(block BlockListEntry) string
```
Renders a `BlockListEntry` back to markdown text.

```go
func PrepareAppendToNode(db *sql.DB, root, nodeTitle, markdown string) (NodeEdit, error)
```
Computes a `NodeEdit` that appends `markdown` to the end of the node's body. Does not write the file.

```go
func PrepareInsertUnderHeading(db *sql.DB, root, nodeTitle, heading string, requestedHeadingLevel int, createIfMissing bool, markdown string) (NodeEdit, error)
```
Computes a `NodeEdit` that inserts `markdown` beneath a named heading (supports scoped paths like `"Foo > Bar"`). Creates the heading if `createIfMissing` is true and it is absent.

```go
func PrepareBlockExtraction(db *sql.DB, root, sourceTitle, blockPath, newTitle, parentTitle string) (ExtractedNode, error)
```
Computes an `ExtractedNode` representing a new node to be created from an existing block. If the block is a heading, the entire sub-section is included. Does not write any files.

```go
func BuildInboxOverview(db *sql.DB, root, nodeTitle string) (InboxOverview, error)
```
Returns an overview of the inbox node's children with kind classification and content metrics.

---

### Construction diffing (`construction.go`)

```go
func DiffConstruction(oldNodes, newNodes []ConstructionNode) (ConstructionDiff, error)
```
Compares two snapshots of a construction (a set of stable-ID nodes) and classifies each as unchanged, inserted, deleted, reparented, reordered, or inbound-changed.

---

### AST rematching (`searchmatch.go`)

```go
func RematchStableIDs(oldRoot, newRoot *SearchASTNode) SearchMatchResult
```
Attempts to carry stable IDs from `oldRoot` onto `newRoot` using a bounded search strategy: matched parent first, then sibling parents, then globally similar nodes ordered by distance.

---

## Significant unexported types and functions

### `triples.go`

**`storedBlock`** (`{Subject string; Block ParsedBlock}`) — Pairs a triple-store subject URI with its decoded `ParsedBlock`. Used internally during block subject assignment.

**`assignBlockSubjects`** — Before a write, loads existing block subjects and fuzzy-matches them to new blocks via `resolveBlockMatches`. This is how block identity survives edits.

**`loadStoredBlocks`** — Decodes block triples from the store back into `ParsedBlock` values, repopulating identity fields.

### `sync.go`

**`parseFile`** — Parses a single markdown file into a `*ParsedNode`. Returns `nil` if the file has no `title` frontmatter field (those files are silently skipped).

**`extractBlocks`** — Walks a goldmark AST and produces `[]ParsedBlock`. Recognizes headings, paragraphs, and list items (with task signifier detection).

**`resolveMaxChars`** — Walks up the parent chain in the store to find the nearest `max-chars` limit applicable to a given node.

**`annotateBlocks`** — Post-processes the block list to attach `HeadingChain` to each block and call `fillBlockIdentity`.

### `blockdiff.go`

**`resolveBlockMatches`** — The fuzzy matching core: indexes old blocks by anchor shingles and exact text hash, then for each new block generates scored candidates and resolves a one-to-one match set (greedy by score). Returns `newPath → oldPath`.

**`reorderedOldPaths`** — Detects which matched blocks changed relative order by computing the longest increasing subsequence over old positions. Blocks outside the LIS are marked reordered.

**`blockMatchScore`** — Computes a numeric similarity score between two blocks. Returns -1 for incompatible kind/level/signifier. Weights: anchor overlap (100/anchor), exact text (200), same scope (25), shared scope prefix (15/level), tag match (10).

**`fillBlockIdentity`** — Computes and sets all hash fields on a `*ParsedBlock`: `TextHash`, `ScopeHash`, `TagHash`, `AnchorHashes`, and `Anchors`.

### `workflow.go`

**`loadCurrentParsedNode`** — Resolves a node title to its file path via the store, reads the file, and parses it. Used by all edit-preparation functions.

**`visibleBlockScope`** — For headings, strips the heading's own text from the end of its `HeadingChain` before displaying scope (so "## Foo" under "## Foo" does not show "Foo > Foo").

---

## How the pieces fit together

### Intended API for callers

A typical caller (in `cmd/`) interacts with this package in two modes:

**Sync mode** (writing the graph):
1. `FindRoot` / `LoadConfig` — locate and parse config
2. `ScanFiles` → `ParseAllFiles` — get all `ParsedNode` values from disk
3. `PopulateTriples` — clear and rewrite the triple store for the root

**Query mode** (reading the graph):
- `BuildOverview` — full node list with metadata and validation
- `BuildWalk` — single-node context (content + tree position + roles)
- `ResolveGroup` / `AutoGroupIncludes` — group membership for context assembly
- `Validate` — standalone validation report

**Edit mode** (preparing mutations):
- `BuildBlockList` → `ResolveBlockTarget` — find a block
- `PrepareAppendToNode` / `PrepareInsertUnderHeading` — generate a `NodeEdit` (caller writes the file and calls sync)
- `PrepareBlockExtraction` — generate an `ExtractedNode` (caller creates the file and calls sync)
- `BuildBlockDiff` — show what has changed in a file since the last sync

The `NodeEdit` and `ExtractedNode` return types are pure data; no file I/O happens inside the Prepare functions. The caller is responsible for applying the edit to disk and re-syncing.

### Prototype subsystems

`construction.go` and `searchmatch.go` are self-contained prototypes. `DiffConstruction` models structural change tracking for stable-ID node sets. `RematchStableIDs` models how stable IDs might be recovered after a tree is rebuilt without them. Neither is currently wired to the sync or query paths.
