# API Map

Auto-generated from source. Do not edit.

```
Generated: 2026-04-10T16:00:17Z
Commit:    1b74145
```

## `cmd/sevens`

### main.go

- `func openDB() (*sql.DB, error)`
- `func resolveRoot(explicit string) (string, error)`
- `func resolveNodeTitle(title string) (string, error)`
- `func syncRoot(rootDir string) error`
- `func syncAllRoots() error`
- `func printEDN(v any) error`
- `func initCmd() *cobra.Command`
- `func runGitInit(dir string) (string, error)`
- `func formatCharCount(n int) string`
- `func printTree(output *graph.OverviewOutput)`
- `func opName(op apply.FileOp) string`
- `func printSuggestion(entry apply.LogEntry)`
- `func printIntermediateOutput(raw string)`
- `func completeNodeTitles(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)`
- `func completeFunctionNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective)`
- `func syncCmd() *cobra.Command`
- `func overviewCmd() *cobra.Command`
- `func walkCmd() *cobra.Command`
- `func treeCmd() *cobra.Command`
- `func diffBlocksCmd() *cobra.Command`
- `func blocksCmd() *cobra.Command`
- `func inboxCmd() *cobra.Command`
- `func extractBlockCmd() *cobra.Command`
- `func rootsCmd() *cobra.Command`
- `func summarizeOutput(outputType, llmOutput string, ops []apply.FileOp) string`
- `func resolveSuspensionBlockForCLI(db *sql.DB, root, nodeTitle string, sus *engine.Suspension) *graph.BlockTarget`
- `func runPipeline(root, nodeTitle string, fn *apply.Function, startStep int, prev string, dryRun bool, confirm bool, includes []string, model string, allowedSteps map[string]bool, backendName string, blockPath string, blockID string) error`
- `func applyCmd() *cobra.Command`
- `func discussCmd() *cobra.Command`
- `func acceptCmd() *cobra.Command`
- `func rejectCmd() *cobra.Command`
- `func pendingCmd() *cobra.Command`
- `func functionsCmd() *cobra.Command`
- `func defineCmd() *cobra.Command`
- `func focusCmd() *cobra.Command`
- `func unfocusCmd() *cobra.Command`
- `func statusCmd() *cobra.Command`
- `func logCmd() *cobra.Command`
- `func queryCmd() *cobra.Command`
- `func searchCmd() *cobra.Command`
- `func revertCmd() *cobra.Command`
- `func prepareCmd() *cobra.Command`
- `func submitCmd() *cobra.Command`
- `func instantiateTemplateNode(db *sql.DB, root string, tmpl *apply.NodeTemplate, cliParent string, cliTarget string, vars map[string]string) (*apply.TemplateExecutionResult, error)`
- `func previewTemplateNode(db *sql.DB, root string, tmpl *apply.NodeTemplate, cliParent string, cliTarget string, vars map[string]string) error`
- `func addTemplateSemanticVars(varMap map[string]string, values map[string]string) map[string]string`
- `func templatesCmd() *cobra.Command`
- `func captureCmd() *cobra.Command`
- `func newCmd() *cobra.Command`
- `func instantiateCmd() *cobra.Command`
- `func configCmd() *cobra.Command`
- `func orDefault(s, def string) string`
- `func summarizeInline(text string, max int) string`
- `func main()`
### repl.go

- `func replCmd() *cobra.Command`
## `defaults`

### defaults.go

- `func ReadFunctionFile(name string) ([]byte, error)`
- `func ListFunctionNames() ([]string, error)`
- `func ReadTemplateFile(name string) ([]byte, error)`
- `func ListTemplateNames() ([]string, error)`
## `internal/apply`

### confirm.go

- `func ConfirmCost(config LLMConfig, backendName, systemPrompt, userPrompt string, threshold float64) (bool, error)`
### functions.go

- `func LoadFunction(name string) (*Function, error)`
- `func ListFunctions() ([]Function, error)`
- `func readFunctionAsset(userPath, bundledName string) ([]byte, error)`
- `func LoadContextFiles(root string, paths []string) string`
- `func RenderStepPrompt(prompt, title, content, parent string, children []string, prev, context string) string`
- `type PromptVars struct`
- `func RenderStepPromptWithVars(prompt string, vars PromptVars) string`
- `func RenderPrompt(fn *Function, title, content, parent string, children []string) string`
- `func ParseOps(llmOutput string) ([]FileOp, error)`
- `func stripBrackets(s string) string`
- `func SanitizeFilename(title string) string`
### git.go

- `func IsGitRepo(root string) bool`
- `func HasChanges(root string) (bool, error)`
- `func CommitAll(root, message string) (string, error)`
- `func ChangedFiles(root string) ([]string, error)`
- `func CommitFiles(root, message string, files []string) (string, error)`
- `func RevertCommit(root, hash string) (string, error)`
- `func runGit(root string, args ...string) (string, error)`
### llm.go

- `func LoadGlobalConfig() (GlobalConfig, error)`
- `func resolveAPIKey(config LLMConfig) (string, error)`
- `func CallLLM(ctx context.Context, config LLMConfig, systemPrompt, prompt string, streamTo io.Writer) (string, error)`
### log.go

- `func logSubject(nodeTitle, timestamp string) string`
- `func AppendLogDB(db *sql.DB, entry LogEntry) error`
- `func ReadLogDB(db *sql.DB, parts ...string) ([]LogEntry, error)`
- `func logSubjectToEntry(db *sql.DB, subject string) (*LogEntry, error)`
### ops.go

- `func ExecuteOps(ops []FileOp, root string, db *sql.DB) (filesCreated []string, filesEdited []string, err error)`
- `func stripContentFrontmatter(content string) string`
- `func createFile(op FileOp, root string) (string, error)`
- `func editFile(op FileOp, root string, db *sql.DB) (string, error)`
- `func truncate(s string, n int) string`
- `func findBestMatch(content, target string) (string, float64)`
- `func similarity(a, b string) float64`
### patheval.go

- `type PathResult struct`
- `func EvalPath(db *sql.DB, startSubject string, spec PathSpec) (*PathResult, error)`
- `func EvalPaths(db *sql.DB, startSubject string, specs []PathSpec) (map[string]*PathResult, error)`
### resolve.go

- `func EffectiveRequires(fn *Function, step *Step) []Require`
- `func ResolveContext(db *sql.DB, root string, fn *Function, step *Step, walk *graph.WalkOutput, targetBlock *graph.BlockTarget) (*ResolvedContext, error)`
- `func FormatResolvedNodes(tag string, nodes []ResolvedNode) string`
- `func FormatHistory(entries []LogEntry) string`
- `func RenderWithContext(prompt string, ctx *ResolvedContext, contextFiles string) string`
- `func HasRequires(fn *Function) bool`
### session.go

- `type Session struct`
- `func SessionPath() (string, error)`
- `func SaveSession(s *Session) error`
- `func LoadSession() (*Session, error)`
- `func ClearSession() error`
### templates.go

- `func LoadTemplate(name string) (*NodeTemplate, error)`
- `func readTemplateAsset(userPath, bundledName string) ([]byte, error)`
- `func ListTemplates() ([]string, error)`
- `func ResolveTemplateVars(tmpl *NodeTemplate, vars map[string]string) map[string]string`
- `func MissingTemplateVars(tmpl *NodeTemplate, vars map[string]string) []string`
- `func RenderTemplate(tmpl *NodeTemplate, vars map[string]string) *NodeTemplate`
- `func CleanRenderedTemplate(tmpl *NodeTemplate) *NodeTemplate`
- `func DraftTitle(tmpl *NodeTemplate) string`
- `func ExtractVariables(tmpl *NodeTemplate) []string`
- `func substituteVars(s string, vars map[string]string) string`
- `func stripUnresolvedVars(s string) string`
- `func InstantiateTemplate(tmpl *NodeTemplate, parent string, root string) []FileOp`
- `type TemplateExecutionOptions struct`
- `type TemplateExecutionResult struct`
- `type TemplatePreview struct`
- `func BindTemplateArgs(tmpl *NodeTemplate, args []string, vars map[string]string) map[string]string`
- `func PreviewTemplate(db *sql.DB, root string, tmpl *NodeTemplate, opts TemplateExecutionOptions) (*TemplatePreview, error)`
- `func ExecuteTemplate(db *sql.DB, root string, tmpl *NodeTemplate, opts TemplateExecutionOptions) (*TemplateExecutionResult, error)`
- `func SiblingRoleTriples(tmpl *NodeTemplate) []store.Triple`
### tokens.go

- `type ModelPricing struct`
- `func LookupPricing(model string) (ModelPricing, bool)`
- `func CountTokens(config LLMConfig, systemPrompt, userPrompt string) (int, error)`
- `func estimateTokens(text string) int`
### types.go

- `type PathSpec struct`
- `type Require struct`
- `type AgentConfig struct`
- `type Step struct`
- `type Function struct`
- `func EffectiveAgent(fn *Function, step *Step) *AgentConfig`
- `type ResolvedNode struct`
- `type ResolvedBlock struct`
- `type ResolvedContext struct`
- `func (f *Function) EffectiveSteps() []Step`
- `func (f *Function) ValidateComposition() error`
- `type FileOp struct`
- `type LogEntry struct`
- `type NodeTemplate struct`
- `type TemplateTarget struct`
- `type TemplatePlacement struct`
- `type TemplateParam struct`
- `type TemplateDraft struct`
- `type LLMConfig struct`
- `type BackendConfig struct`
- `type GlobalConfig struct`
- `func (g *GlobalConfig) ResolveModel(name string) LLMConfig`
## `internal/backend`

### anthropic.go

- `type AnthropicBackend struct`
- `func NewAnthropicBackend(apiKey, apiKeyEnv string) (*AnthropicBackend, error)`
- `func (b *AnthropicBackend) Name() string { return "anthropic" }`
- `func (b *AnthropicBackend) Complete(ctx context.Context, req InferenceRequest) (string, error)`
### backend.go

- `type Backend interface`
- `type InferenceRequest struct`
- `func BuildPrompt(req InferenceRequest) string`
- `func BuildPreamble(exploration string, capabilities []string, allowFileReads bool) string`
- `func PreparePrompt(req InferenceRequest) string`
- `type BackendConfig struct`
- `type BackendSettings struct`
### capabilities.go

- `func kwStr(k edn.Keyword) string`
- `type MCPServerDef struct`
- `type Capabilities struct`
- `func LoadCapabilities() (*Capabilities, error)`
- `func GenerateCodexConfig(caps *Capabilities, _ string) error`
- `type claudeMCPConfig struct`
- `type claudeMCPServer struct`
- `func GenerateClaudeConfig(caps *Capabilities, outputDir string) error`
- `func CheckCapabilities(caps *Capabilities, requested []string) []string`
### claude.go

- `type ClaudeBackend struct`
- `func NewClaudeBackend(command, generatedConfDir string) *ClaudeBackend`
- `func (b *ClaudeBackend) Name() string { return "claude" }`
- `func (b *ClaudeBackend) Complete(ctx context.Context, req InferenceRequest) (string, error)`
- `func (b *ClaudeBackend) mcpConfigPath() string`
- `func (b *ClaudeBackend) buildAllowedTools(req InferenceRequest) string`
### codex.go

- `type CodexBackend struct`
- `func NewCodexBackend(command, generatedConfDir string) *CodexBackend`
- `func (b *CodexBackend) Name() string { return "codex" }`
- `func (b *CodexBackend) Complete(ctx context.Context, req InferenceRequest) (string, error)`
### factory.go

- `func FromConfig(globalConfig apply.GlobalConfig, backendName string) (Backend, error)`
- `func fromBackendConfig(name string, cfg apply.BackendConfig, globalConfig apply.GlobalConfig) (Backend, error)`
- `func newAnthropicFromGlobal(globalConfig apply.GlobalConfig) (*AnthropicBackend, error)`
- `func generatedDir(backendName string) string`
- `func configDirPath() (string, error)`
- `func expandHome(path string) string`
## `internal/engine`

### engine.go

- `type StepResult struct`
- `type Suspension struct`
- `type EvalResult = mo.Either[Suspension, StepResult]`
- `func suspensionSubject(root string) string`
- `type PipelineConfig struct`
- `func targetLabel(nodeTitle string, block *graph.BlockTarget) string`
- `func promptVars(cfg PipelineConfig, prev, context string) apply.PromptVars`
- `func EvalComposedStep(ctx context.Context, cfg PipelineConfig, step apply.Step, stepIndex int, prev string) EvalResult`
- `func EvalStep(ctx context.Context, cfg PipelineConfig, step apply.Step, stepIndex int, prev string) EvalResult`
- `func RunPipeline(ctx context.Context, cfg PipelineConfig, startStep int, prev string) EvalResult`
- `func summarizeOps(ops []apply.FileOp) string`
- `func summarizeSuggestions(output string) string`
- `func joinParts(parts []string) string`
- `func WriteSuspension(db *sql.DB, root, nodeTitle, targetLabel string, block *graph.BlockTarget, function, step, gate, outputType, rawOutput string, stepIndex int, summary string, ops []apply.FileOp, backendName string)`
- `func FindSuspension(db *sql.DB, parts ...string) (*Suspension, string, error)`
- `func ResolveSuspension(db *sql.DB, subject, status string) error`
- `func ListSuspensions(db *sql.DB, root string) ([]Suspension, error)`
- `func findSuspensionBySubject(db *sql.DB, subject string) (*Suspension, string, error)`
- `func FindSuspensionBySubject(db *sql.DB, parts ...string) (*Suspension, error)`
- `func FindSuspensions(db *sql.DB, parts ...string) ([]Suspension, error)`
- `func BuildRevisionHistory(db *sql.DB, parts ...any) string`
- `type ReviseConfig struct`
- `func resolveSuspensionBlock(db *sql.DB, root, nodeTitle string, sus *Suspension) *graph.BlockTarget`
- `func ReviseStep(cfg ReviseConfig) (*apply.LogEntry, string, error)`
## `internal/graph`

### blockdiff.go

- `type ParsedBlockChange struct`
- `type ParsedBlockDiff struct`
- `func fillBlockIdentity(block *ParsedBlock)`
- `func normalizeBlockText(text string) string`
- `func shortHash(text string) string`
- `func shortHashUint64(text string) uint64`
- `func anchorHashesForText(text string) []string`
- `func anchorValuesForText(text string) ([4]uint64, uint8)`
- `func equalStrings(a, b []string) bool`
- `func overlapCount(a, b []string) int`
- `func overlapFixed(a [4]uint64, aCount uint8, b [4]uint64, bCount uint8) int`
- `func sharedPrefixLen(a, b []string) int`
- `func blockFamilyKey(block ParsedBlock) string`
- `func blockMatchScore(oldBlock, newBlock ParsedBlock) int`
- `type blockCandidate struct`
- `func resolveBlockMatches(oldBlocks, newBlocks []ParsedBlock) map[string]string`
- `func previousMatchedPath(blocks []ParsedBlock, path string, matchedOld map[string]bool) string`
- `func previousResolvedOldPath(blocks []ParsedBlock, path string, newToOld map[string]string) string`
- `func reorderedOldPaths(oldBlocks, newBlocks []ParsedBlock, newToOld map[string]string) map[string]bool`
- `func sortChanges(changes []ParsedBlockChange)`
- `func DiffParsedBlocks(oldBlocks, newBlocks []ParsedBlock) ParsedBlockDiff`
### blockinspect.go

- `type BlockDiffEntry struct`
- `type BlockDiffOutput struct`
- `func buildBlockEntry(oldSubject string, oldBlock, newBlock *ParsedBlock, oldPath, newPath string) BlockDiffEntry`
- `func BuildBlockDiff(db *sql.DB, root, nodeTitle string) (BlockDiffOutput, error)`
- `func ScopeString(scope []string) string`
### construction.go

- `type ConstructionNode struct`
- `type ConstructionDiff struct`
- `func indexConstruction(nodes []ConstructionNode) (map[string]ConstructionNode, error)`
- `func sortStrings(values []string)`
- `func DiffConstruction(oldNodes, newNodes []ConstructionNode) (ConstructionDiff, error)`
### query.go

- `func childrenInRoot(db *sql.DB, parentSubject, root string) ([]string, error)`
- `func BuildOverview(db *sql.DB, root string, config Config) (*OverviewOutput, error)`
- `func BuildWalk(db *sql.DB, root string, title string, depth int) (*WalkOutput, error)`
- `func AutoGroupIncludes(db *sql.DB, root string, nodeTitle string, config Config) ([]string, error)`
- `func ResolveGroup(db *sql.DB, root string, group Group) ([]string, error)`
### searchmatch.go

- `type SearchASTNode struct`
- `type indexedSearchNode struct`
- `type SearchMatchResult struct`
- `func indexSearchTree(root *SearchASTNode) []*indexedSearchNode`
- `func similarityScore(oldNode, newNode *SearchASTNode) int`
- `func findIndexedByPath(nodes []*indexedSearchNode, path string) *indexedSearchNode`
- `func childrenOf(nodes []*indexedSearchNode, parent *indexedSearchNode) []*indexedSearchNode`
- `func candidateParents(oldParent *SearchASTNode, matchedParent *indexedSearchNode, newNodes []*indexedSearchNode) []*indexedSearchNode`
- `func absInt(v int) int`
- `func bestChildMatch(oldNode *SearchASTNode, parent *indexedSearchNode, newNodes []*indexedSearchNode, used map[string]bool) *indexedSearchNode`
- `func bestGlobalMatch(oldNode *SearchASTNode, near *indexedSearchNode, newNodes []*indexedSearchNode, used map[string]bool) *indexedSearchNode`
- `func rematchRecursive(oldNode *SearchASTNode, oldIndexed map[string]*indexedSearchNode, newNodes []*indexedSearchNode, matched map[string]string, used map[string]bool)`
- `func RematchStableIDs(oldRoot, newRoot *SearchASTNode) SearchMatchResult`
### sync.go

- `func stripWikiLink(s string) string`
- `func ExpandTilde(path string) string`
- `func LoadConfig(root string) (Config, error)`
- `func FindRoot(dir string) (string, error)`
- `func ScanFiles(root string) ([]string, error)`
- `func extractBody(data []byte) string`
- `func extractWikiLinks(body string) []string`
- `func formatBlockPath(path []int) string`
- `func plainText(n gast.Node, source []byte) string`
- `func extractTags(text string) []string`
- `func annotateBlocks(blocks []ParsedBlock) []ParsedBlock`
- `func newBlock(path []int, kind, text string) ParsedBlock`
- `func extractListItemBlock(item *gast.ListItem, source []byte, path []int) ParsedBlock`
- `func extractBlocks(body string) []ParsedBlock`
- `func parseFile(filePath string, data []byte) (*ParsedNode, error)`
- `func ParseAllFiles(files []string) ([]ParsedNode, []string)`
- `func resolveMaxChars(root, title string, db *sql.DB, config Config) *int`
- `func Validate(db *sql.DB, root string, config Config) (ValidationReport, error)`
- `func PrintValidationReport(report ValidationReport, nodeCount int)`
- `func formatList(items []string) string`
### triples.go

- `type storedBlock struct`
- `func loadStoredBlocks(db *sql.DB, root string, node ParsedNode) ([]storedBlock, error)`
- `func normalizeNodeBlocks(node ParsedNode) ParsedNode`
- `func assignBlockSubjects(db *sql.DB, root string, node ParsedNode) (map[string]string, error)`
- `func BlockToTriples(node ParsedNode, block ParsedBlock, root string, subject string) []store.Triple`
- `func NodeToTriples(node ParsedNode, root string, blockSubjects ...map[string]string) []store.Triple`
- `func RootConfigToTriples(config Config, rootPath string) []store.Triple`
- `func PopulateTriples(db *sql.DB, root string, nodes []ParsedNode, config Config) error`
### types.go

- `type Group struct`
- `type Config struct`
- `type Frontmatter struct`
- `type ParsedNode struct`
- `type ParsedBlock struct`
- `type ValidationReport struct`
- `type OverviewNode struct`
- `type OverviewOutput struct`
- `type WalkNode struct`
- `type WalkOutput struct`
### workflow.go

- `type InboxItemSummary struct`
- `type InboxOverview struct`
- `type BlockListEntry struct`
- `type BlockListOutput struct`
- `type BlockTarget struct`
- `type NodeEdit struct`
- `type ExtractedNode struct`
- `func visibleBlockScope(kind, text string, scope []string) []string`
- `func BuildBlockList(db *sql.DB, root, nodeTitle string) (BlockListOutput, error)`
- `func ResolveBlockTarget(db *sql.DB, root, nodeTitle, blockPath string) (*BlockTarget, error)`
- `func ResolveBlockTargetBySubject(db *sql.DB, subject string) (*BlockTarget, error)`
- `func PrepareAppendToNode(db *sql.DB, root, nodeTitle, markdown string) (NodeEdit, error)`
- `func PrepareInsertUnderHeading(db *sql.DB, root, nodeTitle, heading string, requestedHeadingLevel int, createIfMissing bool, markdown string) (NodeEdit, error)`
- `func prepareInsertWithCreatedHeading(node *ParsedNode, canonical, filePath, raw, body string, headingPath []string, requestedHeadingLevel int, markdown string) NodeEdit`
- `func parseHeadingRef(heading string) []string`
- `func matchesHeadingRef(block ParsedBlock, headingPath []string) bool`
- `func findHeadingInsertionPoint(node *ParsedNode, body string, parentPath []string) (insertAt int, parentLevel int, ok bool)`
- `func inferHeadingLevel(node *ParsedNode, requested int) int`
- `func firstMatchingLine(lines []string, target string) int`
- `func firstMatchingLineAfter(lines []string, target string, start int) int`
- `func headingLevel(line string) int`
- `func resolveBlockTargetSubject(db *sql.DB, subject string, canonicalNodeTitle string) (*BlockTarget, error)`
- `func (b BlockTarget) Label() string`
- `func BuildInboxOverview(db *sql.DB, root, nodeTitle string) (InboxOverview, error)`
- `func summarizeNodeForInbox(db *sql.DB, root, nodeTitle string) (InboxItemSummary, error)`
- `func classifyInboxItem(title string, summary InboxItemSummary) string`
- `func PrepareBlockExtraction(db *sql.DB, root, sourceTitle, blockPath, newTitle, parentTitle string) (ExtractedNode, error)`
- `func loadCurrentParsedNode(db *sql.DB, root, nodeTitle string) (*ParsedNode, string, string, error)`
- `func findBlockByPath(blocks []ParsedBlock, blockPath string) (ParsedBlock, int, error)`
- `func selectExtractedBlocks(blocks []ParsedBlock, idx int) []ParsedBlock`
- `func hasHeadingPrefix(chain, prefix []string) bool`
- `func renderExtractedNodeContent(sourceTitle string, target ParsedBlock, selected []ParsedBlock) string`
- `func RenderBlockMarkdown(block BlockListEntry) string`
- `func renderMarkdownBlock(block ParsedBlock, baseHeadingLevel int) string`
- `func renderBlockMarkdown(kind, text string, level int, signifier string, baseHeadingLevel int) string`
- `func isDiscussionTitle(title string) bool`
## `internal/repl`

### blocks.go

- `func (r *REPL) handleSync() error`
- `func (r *REPL) handleBlocks(tokens []string) error`
- `func (r *REPL) handleBlockDiff(tokens []string) error`
- `func (r *REPL) handleInbox(tokens []string) error`
- `func (r *REPL) handleExtractBlock(tokens []string) error`
- `func blockListArgs(tokens []string) (root string, ednOutput bool, nodeTitle string, err error)`
- `func blockDiffArgs(tokens []string) (root string, ednOutput bool, showUnchanged bool, nodeTitle string, err error)`
- `func inboxArgs(tokens []string) (root string, ednOutput bool, nodeTitle string, err error)`
- `func printREPLBlockDiff(output graph.BlockDiffOutput, showUnchanged bool)`
- `func summarizeBlockText(text string, max int) string`
- `func extractBlockArgs(tokens []string, defaultSource string, defaultPath string, resolveTitle func(string) string) (root string, sourceTitle string, blockPath string, title string, parent string, err error)`
- `func blockPathLike(s string) bool`
### complete.go

- `type completer struct`
- `func newCompleter(r *REPL) readline.AutoCompleter`
- `func (c *completer) Do(line []rune, pos int) (newLine [][]rune, length int)`
- `func completeFrom(candidates []string, prefix string) [][]rune`
- `func (r *REPL) groupNames() []string`
- `func (r *REPL) backendCandidates() []string`
### discuss.go

- `func isThreaded(filePath string) bool`
- `func resolveDiscussionFilePath(db *sql.DB, root, discussTitle string) (string, error)`
- `func (r *REPL) enterDiscussion(nonInteractive bool) error`
- `func (r *REPL) handleDiscussionInput(line string) error`
- `func (r *REPL) endDiscussion() error`
- `func (r *REPL) cancelDiscussion() error`
- `func (r *REPL) showDiscussionTurns(discussTitle string)`
### dispatch.go

- `func (r *REPL) dispatch(line string) error`
- `func (r *REPL) handleDot(tokens []string) error`
- `func (r *REPL) printInfo()`
- `func (r *REPL) printHelp()`
- `func shouldAutoSync(tokens []string) bool`
- `func (r *REPL) printFunctions() error`
- `func (r *REPL) handleNavUp() error`
- `func (r *REPL) handleFocusExplicit(title string) error`
- `func (r *REPL) handleRelativeNav(rel, indexStr string) error`
- `func (r *REPL) handleNumericSelect(n int) error`
- `func (r *REPL) showFocusedBlock() error`
- `func (r *REPL) showFocusSummary() error`
- `func (r *REPL) handleWalk(tokens []string) error`
- `func (r *REPL) handleChildren() error`
- `func (r *REPL) handleSiblings() error`
- `func (r *REPL) handleSearch(query string) error`
- `func countIn(sub, set []string) int`
- `func (r *REPL) handlePending() error`
- `func (r *REPL) handleLog(tokens []string) error`
- `func (r *REPL) handleOverview() error`
- `func (r *REPL) handleApply(tokens []string) error`
- `func (r *REPL) handleAccept(tokens []string) error`
- `func (r *REPL) handleReject() error`
- `func (r *REPL) resolveSuspensionSubject(nodeTitle string) (string, error)`
- `type inlineFlags struct`
- `func (f inlineFlags) has(name string) bool`
- `func parseInlineFlags(tokens []string) inlineFlags`
- `func printOverviewTree(output *graph.OverviewOutput, highlightTitle string)`
- `func tokenize(line string) []string`
- `func parsePositiveInt(s string) (int, bool)`
- `func removeString(slice []string, s string) []string`
- `func orDefault(s, def string) string`
### repl.go

- `type Mode int`
- `type REPL struct`
- `func New(db *sql.DB, root string, focusNode string, globalCfg apply.GlobalConfig) (*REPL, error)`
- `func (r *REPL) Run() error`
- `func (r *REPL) prompt() string`
- `func (r *REPL) updatePrompt()`
- `func (r *REPL) setFocus(title string)`
- `func (r *REPL) setFocusBlock(block graph.BlockListEntry)`
- `func (r *REPL) clearFocusBlock()`
- `func truncateTitle(s string, maxRunes int) string`
- `func (r *REPL) printSystem(format string, args ...any)`
- `func (r *REPL) printError(msg string)`
- `func (r *REPL) printList(titles []string)`
- `func (r *REPL) requireFocus() (string, error)`
- `func (r *REPL) nodeTitles() []string`
- `func (r *REPL) validateRootFlag(explicit string) error`
- `func printEDN(v any) error`
- `func functionNames() []string`
- `func isFunctionName(name string) bool`
- `func (r *REPL) isNodeTitle(s string) bool`
- `func (r *REPL) resolveTitle(s string) string`
- `func opName(op apply.FileOp) string`
- `func historyFile() (string, error)`
- `func nowISO() string`
- `func resolveSuspensionBlock(db *sql.DB, root, nodeTitle string, sus *engine.Suspension) *graph.BlockTarget`
- `func (r *REPL) effectiveCfg() apply.GlobalConfig`
- `type pipelineOpts struct`
- `func (r *REPL) runPipeline(nodeTitle string, fn *apply.Function, startStep int, prev string, dryRun bool, opts ...pipelineOpts) error`
- `func (r *REPL) doAccept(nodeTitle, withFeedback string, susSubjectOverride ...string) error`
- `func (r *REPL) doReject(nodeTitle string, susSubjectOverride ...string) error`
- `func (r *REPL) doRevert(nodeTitle string) error`
- `func (r *REPL) enterNote() error`
- `func (r *REPL) handleNoteInput(line string) error`
- `func (r *REPL) endNote() error`
- `func (r *REPL) handleNew(title string) error`
- `func (r *REPL) includeGroup(name string) error`
- `func (r *REPL) resync() error`
- `func (r *REPL) resyncQuiet() error`
### templates.go

- `func (r *REPL) handleTemplates() error`
- `func (r *REPL) handleCapture(tokens []string) error`
- `func (r *REPL) handleInstantiate(tokens []string) error`
- `func (r *REPL) runTemplate(tmpl *apply.NodeTemplate, parent string, targetNode string, vars map[string]string, dryRun bool) error`
- `func (r *REPL) printTemplatePreview(preview *apply.TemplatePreview)`
- `func parseTemplateInvokeArgs(tokens []string) (root string, parent string, targetNode string, vars map[string]string, args []string, dryRun bool, err error)`
## `internal/store`

### store.go

- `func ConfigDir() (string, error)`
- `func OpenDB() (*sql.DB, error)`
- `func LoadRoots() ([]string, error)`
- `func SaveRoots(roots []string) error`
- `func AddRoot(root string) error`
### triples.go

- `type Triple struct`
- `func NodeSubject(root, title string) string`
- `func BlockSubject(root, nodeTitle, path string) string`
- `func NodeTitle(db *sql.DB, subject string) (string, error)`
- `func scanStrings(rows *sql.Rows) ([]string, error)`
- `func scanTriples(rows *sql.Rows) ([]Triple, error)`
- `func InitTriplesSchema(db *sql.DB) error`
- `func InsertTriple(db *sql.DB, t Triple) error`
- `func SetTriple(db *sql.DB, subject, predicate, object string) error`
- `func InsertTriples(db *sql.DB, triples []Triple) error`
- `func DeleteBySubject(db *sql.DB, subject string) error`
- `func DeleteBySubjectPrefix(db *sql.DB, prefix string) error`
- `func DeleteByPredicate(db *sql.DB, predicate string) error`
- `func ClearRootTriples(db *sql.DB, root string) error`
- `func ResolveTitle(db *sql.DB, title, root string) string`
- `func ResolveNode(db *sql.DB, title, root string) (string, string)`
- `func GetObject(db *sql.DB, subject, predicate string) (string, error)`
- `func GetObjects(db *sql.DB, subject, predicate string) ([]string, error)`
- `func GetSubjects(db *sql.DB, predicate, object string) ([]string, error)`
- `func GetSubjectTriples(db *sql.DB, subject string) ([]Triple, error)`
- `func GetPredicateTriples(db *sql.DB, predicate string) ([]Triple, error)`
- `func SearchContent(db *sql.DB, query string, root string) ([]string, error)`
- `func SearchTitles(db *sql.DB, query string, root string) ([]string, error)`
- `func ListNodeTitles(db *sql.DB, root string) ([]string, error)`
- `func GetRootNodeData(db *sql.DB, root string, predicates []string) (map[string]map[string][]string, error)`
- `func joinPlaceholders(ph []string) string`
- `func RunQuery(db *sql.DB, sqlQuery string, args ...any) ([][]string, error)`
- `func Compose(db *sql.DB, start, pred1, pred2 string) ([]string, error)`
- `func ComposeInverse(db *sql.DB, start, pred1, pred2 string) ([]string, error)`
## `internal/ui`

### render.go

- `func SetTheme(t string)`
- `func Theme() string`
- `func DetectBackground() {}`
- `func glamourOpts() glamour.TermRendererOption`
- `func expandTilde(p string) string`
- `func RenderMarkdown(md string) (string, error)`
- `func RenderMarkdownOrPlain(md string) string`
- `func FormatNodeHeader(title string, parent *string, role string, children, siblings []string, childRoles, siblingRoles map[string]string, crossRefs []string) string`
- `func formatWithRoles(titles []string, roles map[string]string) string`
- `func FormatStep(fnName, stepName, nodeTitle string) string`
- `func FormatPersona(persona string) string`
- `func FormatCost(tokens int, cost float64, autoApproved bool, threshold float64) string`
- `func FormatOp(action, name string) string`
- `func FormatLogEntry(timestamp, event, function, step, commit, note string) string`
- `type PrepareStep struct`
- `type PrepareData struct`
- `func RenderPrepareChecklist(d PrepareData) string`
- `func FormatPending(target, function, step, summary, subject string) string`

