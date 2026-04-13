package main

// bridge.go provides helpers for the new package architecture.
// Commands can be migrated one at a time from old packages (store,
// graph, apply, engine) to new packages (triple, graphops, kb,
// function, projection) by switching to these helpers.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"sevens/internal/backend"
	"sevens/internal/config"
	"sevens/internal/function"
	"sevens/internal/graphops"
	"sevens/internal/kb"
	projEdn "sevens/internal/projection/edn"
	"sevens/internal/projection/md"
	"sevens/internal/triple"
	"sevens/internal/types"
	"sevens/internal/workflow"
)

func ctx() context.Context { return context.Background() }

// kbStack holds the full initialized stack so callers don't have to
// manage three layers of initialization.
type kbStack struct {
	Store      *triple.Store
	Graph      *graphops.Graph
	KB         *kb.KB
	EDN        *projEdn.EDNProjection
	close      func()
}

// openKB creates the full Layer 1-3 stack using the existing sevens
// database. Returns a kbStack whose Close() must be deferred.
// Also syncs the EDN projection (functions, types) into the triple store.
func openKB() (*kbStack, error) {
	// Reuse the existing openDB() which handles config dir, path,
	// WAL mode, etc. The new triple.Store wraps the same *sql.DB.
	db, err := openDB()
	if err != nil {
		return nil, err
	}

	store, err := triple.New(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("initialising triple store: %w", err)
	}

	graph := graphops.New(store)
	k := kb.New(graph)

	// Sync EDN config (functions, types) into the triple store.
	ednProj := projEdn.New(store)
	configDir, _ := config.ConfigDir()
	if configDir != "" {
		fnDir := filepath.Join(configDir, "functions")
		tyDir := filepath.Join(configDir, "types")
		if err := ednProj.Sync(context.Background(), fnDir, tyDir); err != nil {
			// Non-fatal: log but continue. Functions/types will fall back to file loading.
			fmt.Fprintf(os.Stderr, "[warn] EDN sync: %v\n", err)
		}
	}

	// Register graph-based loaders so function/types packages query the graph.
	function.GraphFunctionLoader = ednProj
	types.GraphTypeLoader = ednProj

	return &kbStack{
		Store: store,
		Graph: graph,
		KB:    k,
		EDN:   ednProj,
		close: func() { db.Close() },
	}, nil
}

func (s *kbStack) Close() {
	if s.close != nil {
		s.close()
	}
}

// openProjection creates a markdown projection backed by a kbStack.
func openProjection(stack *kbStack) *md.MarkdownProjection {
	return md.New(stack.KB)
}

// buildExecutor creates a function.Executor from the CLI flags and kbStack.
// Used by commands not yet migrated to the workflow layer.
func buildExecutor(stack *kbStack, be backend.Backend) *function.Executor {
	var tb function.TransformBackend
	if be != nil {
		tb = function.NewLLMBackend(be)
	}
	ps := function.NewPipelineStore(stack.Store)
	return function.NewExecutor(stack.KB, tb, ps)
}

// buildDeps constructs a workflow.Deps from a kbStack and optional backend.
func buildDeps(stack *kbStack, be backend.Backend) *workflow.Deps {
	var tb function.TransformBackend
	if be != nil {
		tb = function.NewLLMBackend(be)
	}
	return &workflow.Deps{
		KB:      stack.KB,
		Proj:    openProjection(stack),
		Store:   function.NewPipelineStore(stack.Store),
		Backend: tb,
	}
}

// warnIfStale checks whether the root has .md files that changed since the last
// sync and prints a warning to stderr. Returns true if stale.
func warnIfStale(root string) bool {
	if !md.IsGitRepo(root) {
		return false
	}
	files, err := md.ChangedFiles(root)
	if err != nil {
		return false
	}
	mdCount := 0
	for _, f := range files {
		if strings.HasSuffix(f, ".md") {
			mdCount++
		}
	}
	if mdCount > 0 {
		fmt.Fprintf(os.Stderr, "[warn] %d markdown file(s) changed since last sync — run `sevens sync` first\n", mdCount)
		return true
	}
	return false
}

// NormalizeShapeName adds the "sevens/" prefix to well-known shape names
// if missing, so users can type just "minimal" as shorthand for "sevens/minimal".
func NormalizeShapeName(name string) string {
	if strings.HasPrefix(name, "sevens/") {
		return name
	}
	switch name {
	case "minimal", "neighborhood", "children", "subtree":
		return "sevens/" + name
	default:
		return name
	}
}

// ResolveGatherSpec loads a context policy type def by name and returns
// the corresponding kb.GatherSpec. Falls back to hardcoded defaults when
// the type system hasn't been synced yet. Accepts both short names
// ("minimal") and namespaced names ("sevens/minimal").
func ResolveGatherSpec(shapeName string) kb.GatherSpec {
	shapeName = NormalizeShapeName(shapeName)

	td, err := types.LoadTypeDef(shapeName)
	if err == nil && td.ContextPolicy {
		return td.Gather
	}
	// Fallback for when types aren't synced yet.
	switch shapeName {
	case "sevens/minimal":
		return kb.GatherMinimal
	case "sevens/neighborhood":
		return kb.GatherNeighborhood
	case "sevens/children":
		return kb.GatherChildren
	case "sevens/subtree":
		return kb.GatherSubtree
	default:
		return kb.GatherNeighborhood
	}
}

// resolvePipeline finds a pipeline by full ID, short ID suffix, or node title.
func resolvePipeline(deps *workflow.Deps, root, arg string) (*function.Pipeline, error) {
	// Full pipeline ID.
	if strings.HasPrefix(arg, "pipeline:") {
		return deps.Store.Load(ctx(), arg)
	}

	// Short ID (e.g. "20260412T175303:a175d086") — search pending for suffix match.
	if strings.Contains(arg, "T") && strings.Contains(arg, ":") {
		pending, err := deps.Store.FindPending(ctx(), root)
		if err != nil {
			return nil, err
		}
		for _, p := range pending {
			if strings.HasSuffix(p.ID, arg) {
				return p, nil
			}
		}
		// Also search all pipelines (not just pending) by loading directly
		// with possible prefix patterns.
		return nil, fmt.Errorf("no pipeline matching %q", arg)
	}

	return workflow.FindPendingPipeline(ctx(), deps, root, arg)
}
