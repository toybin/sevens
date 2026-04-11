package engine

import (
	"context"
	crypto_rand "crypto/rand"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/samber/mo"
	"sevens/internal/apply"
	"sevens/internal/backend"
	"sevens/internal/config"
	"sevens/internal/graph"
	"sevens/internal/store"
)

// StepResult is the output of evaluating a pipeline step.
type StepResult struct {
	StepName   string
	Output     string         // raw LLM output
	OutputType string         // "suggestions", "ops", "text"
	Ops        []apply.FileOp // parsed ops if output type is "ops"
}

// Suspension represents a paused pipeline waiting for human input.
type Suspension struct {
	Subject     string // stable ID, e.g. "suspension:<title>:<timestamp>"
	Root        string
	Function    string
	Target      string
	TargetLabel string
	BlockID     string
	BlockPath   string
	StepName    string
	StepIndex   int
	GateType    string // "approve"
	Output      string // the output to review
	OutputType  string
	Ops         []apply.FileOp
	Summary     string
	Backend     string // backend name used when this suspension was created
}

// EvalResult is Either a Suspension (Left) or a completed StepResult (Right).
type EvalResult = mo.Either[Suspension, StepResult]

func suspensionSubject(root string) string {
	rootHash := sha1.Sum([]byte(strings.ToLower(root)))
	buf := make([]byte, 4)
	_, _ = crypto_rand.Read(buf)
	return fmt.Sprintf("suspension:%s:%s:%s",
		hex.EncodeToString(rootHash[:6]),
		time.Now().UTC().Format("20060102T150405"),
		hex.EncodeToString(buf),
	)
}

// PipelineConfig holds everything needed to run a pipeline.
type PipelineConfig struct {
	DB            *sql.DB
	Root          string
	NodeTitle     string
	TargetBlock   *graph.BlockTarget
	Function      *apply.Function
	GlobalConfig  config.GlobalConfig
	Walk          *graph.WalkOutput
	ContextStr    string // pre-built context files string
	DryRun        bool
	Confirm       bool
	StreamText    bool            // stream text output to stderr
	AllowedSteps  map[string]bool // if non-nil, only run steps whose names are in this set
	Backend       backend.Backend // inference backend (nil falls back to Anthropic API)
	ModelOverride string          // explicit model from --model flag (empty = use backend default)
	Instruction   string          // ad-hoc instruction for {{instruction}} substitution
}

func targetLabel(nodeTitle string, block *graph.BlockTarget) string {
	if block == nil {
		return nodeTitle
	}
	return block.Label()
}

func promptVars(cfg PipelineConfig, prev, context string) apply.PromptVars {
	parent := ""
	if cfg.Walk.Node.Parent != nil {
		parent = *cfg.Walk.Node.Parent
	}
	vars := apply.PromptVars{
		Title:       cfg.Walk.Node.Title,
		Content:     cfg.Walk.Node.Content,
		NodeTitle:   cfg.Walk.Node.Title,
		NodeContent: cfg.Walk.Node.Content,
		Parent:      parent,
		Children:    cfg.Walk.Node.Children,
		Prev:        prev,
		Context:     context,
		TargetKind:  "node",
		TargetLabel: cfg.Walk.Node.Title,
	}
	if cfg.TargetBlock != nil {
		vars.Content = cfg.TargetBlock.Markdown
		vars.TargetKind = "block"
		vars.TargetLabel = cfg.TargetBlock.Label()
		vars.BlockID = cfg.TargetBlock.Subject
		vars.BlockPath = cfg.TargetBlock.Path
		vars.BlockKind = cfg.TargetBlock.Kind
		vars.BlockText = cfg.TargetBlock.Text
		vars.BlockMarkdown = cfg.TargetBlock.Markdown
		vars.BlockSignifier = cfg.TargetBlock.Signifier
		vars.BlockScope = graph.ScopeString(cfg.TargetBlock.Scope)
	}
	return vars
}

// EvalComposedStep evaluates a step that delegates to another function and/or maps over nodes.
func EvalComposedStep(ctx context.Context, cfg PipelineConfig, step apply.Step, stepIndex int, prev string) EvalResult {
	// Simple delegation: run another function's full pipeline on the same target
	if step.Fn != "" && step.MapOver == "" {
		delegateFn, err := apply.LoadFunction(step.Fn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[engine] error loading delegate function %q: %v\n", step.Fn, err)
			return mo.Right[Suspension](StepResult{StepName: step.Name, OutputType: step.Output})
		}

		subCfg := cfg
		subCfg.Function = delegateFn
		return RunPipeline(ctx, subCfg, 0, prev)
	}

	// Map-over: apply a function to each node reached by the predicate
	inverse := false
	pred := step.MapOver
	if len(pred) > 0 && pred[len(pred)-1] == '~' {
		inverse = true
		pred = pred[:len(pred)-1]
	}

	var targets []string
	var err error
	if inverse {
		targets, err = store.GetSubjects(cfg.DB, pred, cfg.NodeTitle)
	} else {
		targets, err = store.GetObjects(cfg.DB, cfg.NodeTitle, pred)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "[engine] map-over query error: %v\n", err)
		return mo.Right[Suspension](StepResult{StepName: step.Name, OutputType: step.Output})
	}

	if len(targets) == 0 {
		fmt.Fprintf(os.Stderr, "[engine] map-over: no targets found for %s from %q\n", step.MapOver, cfg.NodeTitle)
		return mo.Right[Suspension](StepResult{StepName: step.Name, OutputType: step.Output})
	}

	// Determine which function to map
	fnToMap := cfg.Function
	if step.Fn != "" {
		fnToMap, err = apply.LoadFunction(step.Fn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[engine] error loading map function %q: %v\n", step.Fn, err)
			return mo.Right[Suspension](StepResult{StepName: step.Name, OutputType: step.Output})
		}
	}

	fmt.Fprintf(os.Stderr, "[engine] mapping %s over %d nodes\n", fnToMap.Name, len(targets))

	var allOps []apply.FileOp
	var allOutput []string
	for _, target := range targets {
		targetWalk, err := graph.BuildWalk(cfg.DB, cfg.Root, target, 1)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[engine] map-over: skipping %q: %v\n", target, err)
			continue
		}

		subCfg := cfg
		subCfg.NodeTitle = target
		subCfg.Function = fnToMap
		subCfg.Walk = targetWalk

		result := RunPipeline(ctx, subCfg, 0, "")

		if result.IsLeft() {
			// Sub-pipeline suspended at a gate — propagate first suspension
			return result
		}

		stepResult := result.MustRight()
		allOutput = append(allOutput, stepResult.Output)
		allOps = append(allOps, stepResult.Ops...)
	}

	return mo.Right[Suspension](StepResult{
		StepName:   step.Name,
		Output:     joinParts(allOutput),
		OutputType: step.Output,
		Ops:        allOps,
	})
}

// EvalStep evaluates a single pipeline step. Returns Either[Suspension, StepResult].
func EvalStep(ctx context.Context, cfg PipelineConfig, step apply.Step, stepIndex int, prev string) EvalResult {
	// Resolve agent config for this step
	agent := apply.EffectiveAgent(cfg.Function, &step)

	// Determine system prompt — step/function agent overrides global
	systemPrompt := cfg.GlobalConfig.SystemPrompt
	if agent != nil && agent.SystemPrompt != "" {
		systemPrompt = agent.SystemPrompt
	}

	// Determine model — CLI --model flag > function agent config > global default
	llmConfig := cfg.GlobalConfig.LLM
	modelExplicit := cfg.ModelOverride != "" // true if model was explicitly set via --model
	if cfg.ModelOverride != "" {
		resolved := cfg.GlobalConfig.ResolveModel(cfg.ModelOverride)
		if resolved.Model != cfg.GlobalConfig.LLM.Model || cfg.ModelOverride == resolved.Model {
			llmConfig = resolved
		} else {
			llmConfig.Model = cfg.ModelOverride
		}
	} else if agent != nil && agent.Model != "" {
		modelExplicit = true
		resolved := cfg.GlobalConfig.ResolveModel(agent.Model)
		if resolved.Model != cfg.GlobalConfig.LLM.Model || agent.Model == resolved.Model {
			llmConfig = resolved
		} else {
			llmConfig.Model = agent.Model
		}
	}

	// Determine context policy
	contextPolicy := "full"
	if agent != nil && agent.ContextPolicy != "" {
		contextPolicy = agent.ContextPolicy
	}

	// Resolve context based on policy
	var prompt string
	switch contextPolicy {
	case "minimal":
		// Target only — no path resolution, no context files
		prompt = apply.RenderStepPromptWithVars(step.Prompt, promptVars(cfg, prev, ""))
	case "neighborhood":
		// Target + structural info (titles, roles) but not full sibling/child content
		prompt = apply.RenderStepPromptWithVars(step.Prompt, promptVars(cfg, prev, cfg.ContextStr))
	default: // "full", "cached", "custom"
		if apply.HasRequires(cfg.Function) {
			resolved, err := apply.ResolveContext(cfg.DB, cfg.Root, cfg.Function, &step, cfg.Walk, cfg.TargetBlock)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[engine] resolve error: %v\n", err)
				return mo.Right[Suspension](StepResult{StepName: step.Name, Output: "", OutputType: step.Output})
			}
			resolved.Prev = prev
			resolved.Instruction = cfg.Instruction
			prompt = apply.RenderWithContext(step.Prompt, resolved, cfg.ContextStr)
		} else {
			prompt = apply.RenderStepPromptWithVars(step.Prompt, promptVars(cfg, prev, cfg.ContextStr))
		}
	}

	// Inject ad-hoc instruction. Check the raw template for an explicit placeholder;
	// if absent, append at the end so the instruction always overrides template content.
	if cfg.Instruction != "" {
		if strings.Contains(step.Prompt, "{{instruction}}") {
			// Already substituted by the render path above — nothing more to do.
		} else {
			prompt += "\n\n<instruction>" + cfg.Instruction + "</instruction>"
		}
	}

	if cfg.DryRun {
		if agent != nil && agent.Persona != "" {
			fmt.Fprintf(os.Stderr, "[persona] %s\n", agent.Persona)
		}
		fmt.Println(prompt)
		return mo.Right[Suspension](StepResult{StepName: step.Name, Output: prompt, OutputType: step.Output})
	}

	// Cost confirmation
	if cfg.Confirm {
		backendName := ""
		if cfg.Backend != nil {
			backendName = cfg.Backend.Name()
		}
		ok, err := apply.ConfirmCost(llmConfig, backendName, systemPrompt, prompt, cfg.GlobalConfig.CostThreshold)
		if err != nil || !ok {
			fmt.Fprintln(os.Stderr, "[abort] Cancelled by user")
			return mo.Right[Suspension](StepResult{StepName: step.Name, Output: "", OutputType: step.Output})
		}
	}

	// Call LLM via backend
	if agent != nil && agent.Persona != "" {
		fmt.Fprintf(os.Stderr, "[llm] Running step %q as %s...\n", step.Name, agent.Persona)
	} else {
		fmt.Fprintf(os.Stderr, "[llm] Running step %q...\n", step.Name)
	}
	var streamTo *os.File
	if step.Output == "text" && cfg.StreamText {
		streamTo = os.Stderr
	}

	// For CLI backends, only send the model if it was explicitly set.
	// Otherwise let the CLI use its own configured default.
	reqModel := llmConfig.Model
	if cfg.Backend != nil && !modelExplicit {
		reqModel = "" // let CLI use its default
	}

	inferReq := backend.InferenceRequest{
		SystemPrompt: systemPrompt,
		Prompt:       prompt,
		Model:        reqModel,
		StreamTo:     streamTo,
	}
	// Populate exploration fields from agent config
	if agent != nil {
		inferReq.Exploration = agent.Exploration
		inferReq.ReadOnly = agent.ReadOnly
		inferReq.AllowFileReads = agent.AllowFileReads
		inferReq.Capabilities = agent.Capabilities
	}

	var llmOutput string
	var llmErr error
	if cfg.Backend != nil {
		llmOutput, llmErr = cfg.Backend.Complete(ctx, inferReq)
	} else {
		// Fallback to direct Anthropic API call
		llmOutput, llmErr = apply.CallLLM(ctx, llmConfig, systemPrompt, prompt, streamTo)
	}
	if llmErr != nil {
		fmt.Fprintf(os.Stderr, "[engine] LLM error: %v\n", llmErr)
		return mo.Right[Suspension](StepResult{StepName: step.Name, Output: "", OutputType: step.Output})
	}

	result := StepResult{
		StepName:   step.Name,
		Output:     llmOutput,
		OutputType: step.Output,
	}

	// Parse ops if applicable
	if step.Output == "ops" {
		ops, err := apply.ParseOps(llmOutput)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[engine] ops parse error: %v\n", err)
		} else {
			result.Ops = ops
		}
	}

	return mo.Right[Suspension](result)
}

// RunPipeline evaluates steps sequentially, suspending at gates.
// Returns Either[Suspension, StepResult] where the StepResult is from the last completed step.
func RunPipeline(ctx context.Context, cfg PipelineConfig, startStep int, prev string) EvalResult {
	steps := cfg.Function.EffectiveSteps()
	currentTargetLabel := targetLabel(cfg.NodeTitle, cfg.TargetBlock)

	for i := startStep; i < len(steps); i++ {
		step := steps[i]
		isLastStep := i == len(steps)-1

		// Skip steps not in the allowed set (if filtering is active)
		if cfg.AllowedSteps != nil && !cfg.AllowedSteps[step.Name] {
			continue
		}

		// Use composed evaluation if step delegates or maps
		var result EvalResult
		if step.Fn != "" || step.MapOver != "" {
			result = EvalComposedStep(ctx, cfg, step, i, prev)
		} else {
			result = EvalStep(ctx, cfg, step, i, prev)
		}

		// If evaluation returned a suspension (from sub-pipelines), propagate
		if result.IsLeft() {
			return result
		}

		stepResult := result.MustRight()
		if cfg.DryRun {
			return mo.Right[Suspension](stepResult)
		}

		// Check for gate — suspend if gate present or if last step
		if step.Gate == "approve" || isLastStep {
			// If output type is "ops" and ops slice is empty, short-circuit as a no-op.
			if stepResult.OutputType == "ops" && len(stepResult.Ops) == 0 {
				entry := apply.LogEntry{
					Event:     "completed",
					Root:      cfg.Root,
					Function:  cfg.Function.Name,
					Target:    cfg.NodeTitle,
					Step:      step.Name,
					StepIndex: i,
					Timestamp: time.Now().UTC().Format(time.RFC3339),
					RawOutput: stepResult.Output,
					Summary:   "no changes proposed",
				}
				apply.AppendLogDB(cfg.DB, entry)
				fmt.Fprintf(os.Stderr, "[engine] no changes proposed for %q\n", currentTargetLabel)
				return mo.Right[Suspension](stepResult)
			}

			// Log the suggestion
			entry := apply.LogEntry{
				Event:     "suggested",
				Root:      cfg.Root,
				Function:  cfg.Function.Name,
				Target:    cfg.NodeTitle,
				Step:      step.Name,
				StepIndex: i,
				Timestamp: time.Now().UTC().Format(time.RFC3339),
				RawOutput: stepResult.Output,
			}

			// For non-ops last steps, mark as completed instead of suggested
			if isLastStep && step.Output != "ops" {
				entry.Event = "completed"
			}

			if stepResult.OutputType == "ops" && len(stepResult.Ops) > 0 {
				entry.Ops = stepResult.Ops
				entry.Summary = summarizeOps(stepResult.Ops)
			} else if stepResult.OutputType == "suggestions" {
				entry.Summary = summarizeSuggestions(stepResult.Output)
			}

			apply.AppendLogDB(cfg.DB, entry)

			// If it's a completed non-ops step, return as Right (done)
			if isLastStep && step.Output != "ops" {
				return mo.Right[Suspension](stepResult)
			}

			// Write suspension triples to DB
			backendName := ""
			if cfg.Backend != nil {
				backendName = cfg.Backend.Name()
			}
			WriteSuspension(cfg.DB, cfg.Root, cfg.NodeTitle, currentTargetLabel, cfg.TargetBlock, cfg.Function.Name, step.Name, step.Gate, stepResult.OutputType, stepResult.Output, i, entry.Summary, stepResult.Ops, backendName)

			// Otherwise, suspend for review
			suspension := Suspension{
				Function:    cfg.Function.Name,
				Root:        cfg.Root,
				Target:      cfg.NodeTitle,
				TargetLabel: currentTargetLabel,
				StepName:    step.Name,
				StepIndex:   i,
				GateType:    step.Gate,
				Output:      stepResult.Output,
				OutputType:  stepResult.OutputType,
				Ops:         stepResult.Ops,
				Summary:     entry.Summary,
			}
			if cfg.TargetBlock != nil {
				suspension.BlockID = cfg.TargetBlock.Subject
				suspension.BlockPath = cfg.TargetBlock.Path
			}
			return mo.Left[Suspension, StepResult](suspension)
		}

		// No gate — advance with this step's output as prev
		prev = stepResult.Output
	}

	// Should not reach here — last step always hits the gate/isLastStep branch
	return mo.Right[Suspension](StepResult{})
}

func summarizeOps(ops []apply.FileOp) string {
	creates, edits := 0, 0
	for _, op := range ops {
		switch op.Action {
		case "create":
			creates++
		case "edit":
			edits++
		}
	}
	var parts []string
	if creates > 0 {
		parts = append(parts, fmt.Sprintf("create %d nodes", creates))
	}
	if edits > 0 {
		parts = append(parts, fmt.Sprintf("edit %d nodes", edits))
	}
	return joinParts(parts)
}

func summarizeSuggestions(output string) string {
	// Try to count JSON array items
	count := 0
	for i := 0; i < len(output); i++ {
		if output[i] == '{' {
			count++
		}
	}
	if count > 0 {
		return fmt.Sprintf("%d suggestions", count)
	}
	if len(output) > 80 {
		return output[:80] + "..."
	}
	return output
}

func joinParts(parts []string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += ", "
		}
		result += p
	}
	return result
}

// WriteSuspension creates a new pending suspension record in the DB.
func WriteSuspension(db *sql.DB, root, nodeTitle, targetLabel string, block *graph.BlockTarget, function, step, gate, outputType, rawOutput string, stepIndex int, summary string, ops []apply.FileOp, backendName string) {
	suspSubject := suspensionSubject(root)
	suspTriples := []store.Triple{
		{Subject: suspSubject, Predicate: "suspension/root", Object: root},
		{Subject: suspSubject, Predicate: "suspension/target", Object: nodeTitle},
		{Subject: suspSubject, Predicate: "suspension/function", Object: function},
		{Subject: suspSubject, Predicate: "suspension/step", Object: step},
		{Subject: suspSubject, Predicate: "suspension/step-index", Object: fmt.Sprintf("%d", stepIndex)},
		{Subject: suspSubject, Predicate: "suspension/gate", Object: gate},
		{Subject: suspSubject, Predicate: "suspension/output-type", Object: outputType},
		{Subject: suspSubject, Predicate: "suspension/raw-output", Object: rawOutput},
		{Subject: suspSubject, Predicate: "suspension/timestamp", Object: time.Now().UTC().Format(time.RFC3339)},
		{Subject: suspSubject, Predicate: "suspension/status", Object: "pending"},
	}
	if targetLabel != "" && targetLabel != nodeTitle {
		suspTriples = append(suspTriples, store.Triple{Subject: suspSubject, Predicate: "suspension/target-label", Object: targetLabel})
	}
	if block != nil {
		suspTriples = append(suspTriples,
			store.Triple{Subject: suspSubject, Predicate: "suspension/block-id", Object: block.Subject},
			store.Triple{Subject: suspSubject, Predicate: "suspension/block-path", Object: block.Path},
		)
	}
	if backendName != "" {
		suspTriples = append(suspTriples, store.Triple{Subject: suspSubject, Predicate: "suspension/backend", Object: backendName})
	}
	if summary != "" {
		suspTriples = append(suspTriples, store.Triple{Subject: suspSubject, Predicate: "suspension/summary", Object: summary})
	}
	if len(ops) > 0 {
		opsJSON, _ := json.Marshal(ops)
		suspTriples = append(suspTriples, store.Triple{Subject: suspSubject, Predicate: "suspension/ops", Object: string(opsJSON)})
	}
	store.InsertTriples(db, suspTriples)
}

// FindSuspension finds the most recent pending suspension for a node.
func FindSuspension(db *sql.DB, parts ...string) (*Suspension, string, error) {
	var root, nodeTitle string
	switch len(parts) {
	case 1:
		nodeTitle = parts[0]
	case 2:
		root = parts[0]
		nodeTitle = parts[1]
	default:
		return nil, "", fmt.Errorf("FindSuspension expects nodeTitle or root,nodeTitle")
	}

	var (
		rows *sql.Rows
		err  error
	)
	if root != "" {
		rows, err = db.Query(`
			SELECT DISTINCT t1.subject FROM triples t1
			JOIN triples t2 ON t1.subject = t2.subject
			WHERE t1.predicate = 'suspension/target' AND t1.object = ?
			AND t2.predicate = 'suspension/root' AND t2.object = ?
		`, nodeTitle, root)
	} else {
		rows, err = db.Query(`
			SELECT DISTINCT subject FROM triples
			WHERE predicate = 'suspension/target' AND object = ?
		`, nodeTitle)
	}
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	var subjects []string
	for rows.Next() {
		var subj string
		if err := rows.Scan(&subj); err != nil {
			return nil, "", err
		}
		subjects = append(subjects, subj)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	var latestSubject string
	var latestTimestamp string
	for _, subj := range subjects {
		status, _ := store.GetObject(db, subj, "suspension/status")
		if status != "pending" {
			continue
		}
		ts, _ := store.GetObject(db, subj, "suspension/timestamp")
		if ts > latestTimestamp {
			latestTimestamp = ts
			latestSubject = subj
		}
	}

	if latestSubject == "" {
		return nil, "", nil
	}

	sus, _, err := findSuspensionBySubject(db, latestSubject)
	return sus, latestSubject, err
}

// ResolveSuspension marks a suspension as resolved with the given status.
func ResolveSuspension(db *sql.DB, subject, status string) error {
	return store.SetTriple(db, subject, "suspension/status", status)
}

// ListSuspensions returns all pending suspensions, optionally filtered by root.
// If root is empty, returns suspensions across all roots.
func ListSuspensions(db *sql.DB, root string) ([]Suspension, error) {
	var rows *sql.Rows
	var err error
	if root != "" {
		rows, err = db.Query(`
			SELECT DISTINCT t1.subject FROM triples t1
			JOIN triples t2 ON t1.subject = t2.subject
			WHERE t1.predicate = 'suspension/status' AND t1.object = 'pending'
			AND t2.predicate = 'suspension/root' AND t2.object = ?
			ORDER BY t1.subject DESC
		`, root)
	} else {
		rows, err = db.Query(`
			SELECT DISTINCT t1.subject FROM triples t1
			JOIN triples t2 ON t1.subject = t2.subject
			WHERE t1.predicate = 'suspension/status' AND t1.object = 'pending'
			AND t2.predicate = 'suspension/target'
			ORDER BY t1.subject DESC
		`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	// Collect all subjects first, then close the rows cursor before issuing
	// follow-up queries — SQLite can't handle nested queries on one connection.
	var subjects []string
	for rows.Next() {
		var subj string
		rows.Scan(&subj)
		subjects = append(subjects, subj)
	}
	rows.Close()

	var results []Suspension
	for _, subj := range subjects {
		sus, _, err := findSuspensionBySubject(db, subj)
		if err != nil || sus == nil {
			continue
		}
		results = append(results, *sus)
	}
	return results, nil
}

func findSuspensionBySubject(db *sql.DB, subject string) (*Suspension, string, error) {
	sus := &Suspension{}
	sus.Subject = subject
	sus.Root, _ = store.GetObject(db, subject, "suspension/root")
	sus.Target, _ = store.GetObject(db, subject, "suspension/target")
	sus.TargetLabel, _ = store.GetObject(db, subject, "suspension/target-label")
	sus.BlockID, _ = store.GetObject(db, subject, "suspension/block-id")
	sus.BlockPath, _ = store.GetObject(db, subject, "suspension/block-path")
	sus.Function, _ = store.GetObject(db, subject, "suspension/function")
	sus.StepName, _ = store.GetObject(db, subject, "suspension/step")
	idxStr, _ := store.GetObject(db, subject, "suspension/step-index")
	fmt.Sscanf(idxStr, "%d", &sus.StepIndex)
	sus.GateType, _ = store.GetObject(db, subject, "suspension/gate")
	sus.OutputType, _ = store.GetObject(db, subject, "suspension/output-type")
	sus.Output, _ = store.GetObject(db, subject, "suspension/raw-output")
	sus.Summary, _ = store.GetObject(db, subject, "suspension/summary")
	sus.Backend, _ = store.GetObject(db, subject, "suspension/backend")

	opsJSON, _ := store.GetObject(db, subject, "suspension/ops")
	if opsJSON != "" {
		json.Unmarshal([]byte(opsJSON), &sus.Ops)
	}

	return sus, subject, nil
}

// FindSuspensionBySubject looks up a suspension by its exact subject ID.
func FindSuspensionBySubject(db *sql.DB, parts ...string) (*Suspension, error) {
	var root, subject string
	switch len(parts) {
	case 1:
		subject = parts[0]
	case 2:
		root = parts[0]
		subject = parts[1]
	default:
		return nil, fmt.Errorf("FindSuspensionBySubject expects subject or root,subject")
	}
	// Verify it exists and is pending.
	status, _ := store.GetObject(db, subject, "suspension/status")
	if status != "pending" {
		return nil, nil
	}
	if root != "" {
		suspRoot, _ := store.GetObject(db, subject, "suspension/root")
		if suspRoot != root {
			return nil, nil
		}
	}
	sus, _, err := findSuspensionBySubject(db, subject)
	return sus, err
}

// FindSuspensions returns all pending suspensions for a node, ordered by timestamp descending.
func FindSuspensions(db *sql.DB, parts ...string) ([]Suspension, error) {
	var root, nodeTitle string
	switch len(parts) {
	case 1:
		nodeTitle = parts[0]
	case 2:
		root = parts[0]
		nodeTitle = parts[1]
	default:
		return nil, fmt.Errorf("FindSuspensions expects nodeTitle or root,nodeTitle")
	}

	var (
		rows *sql.Rows
		err  error
	)
	if root != "" {
		rows, err = db.Query(`
			SELECT DISTINCT t1.subject FROM triples t1
			JOIN triples t2 ON t1.subject = t2.subject
			WHERE t1.predicate = 'suspension/target' AND t1.object = ?
			AND t2.predicate = 'suspension/root' AND t2.object = ?
		`, nodeTitle, root)
	} else {
		rows, err = db.Query(`
			SELECT DISTINCT subject FROM triples
			WHERE predicate = 'suspension/target' AND object = ?
		`, nodeTitle)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subjects []string
	for rows.Next() {
		var subj string
		if err := rows.Scan(&subj); err != nil {
			return nil, err
		}
		subjects = append(subjects, subj)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	type entry struct {
		subject   string
		timestamp string
	}
	var pending []entry
	for _, subj := range subjects {
		status, _ := store.GetObject(db, subj, "suspension/status")
		if status != "pending" {
			continue
		}
		ts, _ := store.GetObject(db, subj, "suspension/timestamp")
		pending = append(pending, entry{subj, ts})
	}

	// Sort descending by timestamp (lexicographic works for RFC3339).
	for i := 1; i < len(pending); i++ {
		for j := i; j > 0 && pending[j].timestamp > pending[j-1].timestamp; j-- {
			pending[j], pending[j-1] = pending[j-1], pending[j]
		}
	}

	var results []Suspension
	for _, e := range pending {
		sus, _, err := findSuspensionBySubject(db, e.subject)
		if err != nil || sus == nil {
			continue
		}
		results = append(results, *sus)
	}
	return results, nil
}

// BuildRevisionHistory formats the suggestion/revision thread for a node's current step
// as XML context for the LLM to consider when re-running with feedback.
func BuildRevisionHistory(db *sql.DB, parts ...any) string {
	var (
		root      string
		nodeTitle string
		stepIndex int
	)
	switch len(parts) {
	case 2:
		nodeTitle, _ = parts[0].(string)
		stepIndex, _ = parts[1].(int)
	case 3:
		root, _ = parts[0].(string)
		nodeTitle, _ = parts[1].(string)
		stepIndex, _ = parts[2].(int)
	default:
		return ""
	}
	entries, err := apply.ReadLogDB(db, root, nodeTitle)
	if err != nil || len(entries) == 0 {
		return ""
	}

	var thread []apply.LogEntry
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Event == "suggested" && e.StepIndex == stepIndex {
			thread = append([]apply.LogEntry{e}, thread...)
		} else if e.Event == "revision" {
			thread = append([]apply.LogEntry{e}, thread...)
		} else {
			break
		}
	}
	if len(thread) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("<previous-attempts>\n")
	for _, e := range thread {
		switch e.Event {
		case "suggested":
			sb.WriteString("<suggestion>\n")
			sb.WriteString(e.RawOutput)
			sb.WriteString("\n</suggestion>\n")
		case "revision":
			sb.WriteString("<revision>")
			sb.WriteString(e.Note)
			sb.WriteString("</revision>\n")
		}
	}
	sb.WriteString("</previous-attempts>")
	return sb.String()
}

// ReviseConfig holds parameters for ReviseStep.
type ReviseConfig struct {
	DB            *sql.DB
	Root          string
	NodeTitle     string
	Function      *apply.Function
	Suspension    *Suspension
	Feedback      string
	Confirm       bool
	StreamText    *os.File        // nil to suppress streaming
	Backend       backend.Backend // inference backend (nil falls back to Anthropic API)
	GlobalConfig  *config.GlobalConfig
	ContextStr    string // pre-built context string (includes, context files)
	ModelOverride string
	Instruction   string
}

func resolveSuspensionBlock(db *sql.DB, root, nodeTitle string, sus *Suspension) *graph.BlockTarget {
	if sus == nil {
		return nil
	}
	if sus.BlockID != "" {
		if block, err := graph.ResolveBlockTargetBySubject(db, sus.BlockID); err == nil {
			return block
		}
	}
	if sus.BlockPath != "" {
		if block, err := graph.ResolveBlockTarget(db, root, nodeTitle, sus.BlockPath); err == nil {
			return block
		}
	}
	return nil
}

// ReviseStep re-runs a suspended pipeline step with revision feedback.
// Returns the new log entry and the raw LLM output.
func ReviseStep(cfg ReviseConfig) (*apply.LogEntry, string, error) {
	// Log the revision note
	revEntry := apply.LogEntry{
		Event:     "revision",
		Root:      cfg.Root,
		Target:    cfg.NodeTitle,
		Note:      cfg.Feedback,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if err := apply.AppendLogDB(cfg.DB, revEntry); err != nil {
		return nil, "", fmt.Errorf("appending revision log: %w", err)
	}

	walk, err := graph.BuildWalk(cfg.DB, cfg.Root, cfg.NodeTitle, 1)
	if err != nil {
		return nil, "", fmt.Errorf("building walk: %w", err)
	}
	targetBlock := resolveSuspensionBlock(cfg.DB, cfg.Root, cfg.NodeTitle, cfg.Suspension)

	steps := cfg.Function.EffectiveSteps()
	stepIndex := cfg.Suspension.StepIndex
	if stepIndex >= len(steps) {
		stepIndex = len(steps) - 1
	}
	step := steps[stepIndex]

	// Resolve global config
	var globalConfig *config.GlobalConfig
	if cfg.GlobalConfig != nil {
		globalConfig = cfg.GlobalConfig
	} else {
		gc, err := config.LoadGlobalConfig()
		if err != nil {
			return nil, "", fmt.Errorf("loading global config: %w", err)
		}
		globalConfig = &gc
	}

	// Resolve agent config (same precedence as EvalStep: step > function > global)
	agent := apply.EffectiveAgent(cfg.Function, &step)

	systemPrompt := globalConfig.SystemPrompt
	if agent != nil && agent.SystemPrompt != "" {
		systemPrompt = agent.SystemPrompt
	}

	// Resolve model (same precedence as EvalStep)
	llmConfig := globalConfig.LLM
	modelExplicit := cfg.ModelOverride != ""
	if cfg.ModelOverride != "" {
		resolved := globalConfig.ResolveModel(cfg.ModelOverride)
		if resolved.Model != globalConfig.LLM.Model || cfg.ModelOverride == resolved.Model {
			llmConfig = resolved
		} else {
			llmConfig.Model = cfg.ModelOverride
		}
	} else if agent != nil && agent.Model != "" {
		modelExplicit = true
		resolved := globalConfig.ResolveModel(agent.Model)
		if resolved.Model != globalConfig.LLM.Model || agent.Model == resolved.Model {
			llmConfig = resolved
		} else {
			llmConfig.Model = agent.Model
		}
	}

	// Resolve context policy
	contextPolicy := "full"
	if agent != nil && agent.ContextPolicy != "" {
		contextPolicy = agent.ContextPolicy
	}

	// Build context string if not provided
	contextStr := cfg.ContextStr
	if contextStr == "" {
		var contextFiles []string
		contextFiles = append(contextFiles, globalConfig.ContextFiles...)
		contextFiles = append(contextFiles, cfg.Function.ContextFiles...)
		contextStr = apply.LoadContextFiles(cfg.Root, contextFiles)
	}

	// Build prompt using the same resolution as EvalStep
	var basePrompt string
	pipelineCfg := PipelineConfig{
		DB:          cfg.DB,
		Root:        cfg.Root,
		NodeTitle:   cfg.NodeTitle,
		TargetBlock: targetBlock,
		Function:    cfg.Function,
		Walk:        walk,
	}
	switch contextPolicy {
	case "minimal":
		basePrompt = apply.RenderStepPromptWithVars(step.Prompt, promptVars(pipelineCfg, cfg.Suspension.Output, ""))
	case "neighborhood":
		basePrompt = apply.RenderStepPromptWithVars(step.Prompt, promptVars(pipelineCfg, cfg.Suspension.Output, contextStr))
	default: // "full"
		if apply.HasRequires(cfg.Function) {
			resolved, err := apply.ResolveContext(cfg.DB, cfg.Root, cfg.Function, &step, walk, targetBlock)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[engine] resolve error during revision: %v\n", err)
				// Fall back to simple prompt
				basePrompt = apply.RenderStepPromptWithVars(step.Prompt, promptVars(pipelineCfg, cfg.Suspension.Output, contextStr))
			} else {
				resolved.Prev = cfg.Suspension.Output
				resolved.Instruction = cfg.Instruction
				basePrompt = apply.RenderWithContext(step.Prompt, resolved, contextStr)
			}
		} else {
			basePrompt = apply.RenderStepPromptWithVars(step.Prompt, promptVars(pipelineCfg, cfg.Suspension.Output, contextStr))
		}
	}

	// Append ad-hoc instruction if not already handled by RenderWithContext
	if cfg.Instruction != "" && !strings.Contains(step.Prompt, "{{instruction}}") {
		basePrompt += "\n\n<instruction>" + cfg.Instruction + "</instruction>"
	}

	// Append revision history and feedback
	history := BuildRevisionHistory(cfg.DB, cfg.Root, cfg.NodeTitle, stepIndex)
	revisedPrompt := basePrompt + "\n\n" + history

	if cfg.Confirm {
		backendName := ""
		if cfg.Backend != nil {
			backendName = cfg.Backend.Name()
		}
		ok, err := apply.ConfirmCost(llmConfig, backendName, systemPrompt, revisedPrompt, globalConfig.CostThreshold)
		if err != nil {
			return nil, "", fmt.Errorf("cost confirmation: %w", err)
		}
		if !ok {
			return nil, "", nil // cancelled by user
		}
	}

	fmt.Fprintf(os.Stderr, "[llm] Re-running with revision...\n")
	callCtx := context.Background()

	// For CLI backends, only send model if explicitly set
	reqModel := llmConfig.Model
	if cfg.Backend != nil && !modelExplicit {
		reqModel = ""
	}

	inferReq := backend.InferenceRequest{
		SystemPrompt: systemPrompt,
		Prompt:       revisedPrompt,
		Model:        reqModel,
		StreamTo:     cfg.StreamText,
	}
	if agent != nil {
		inferReq.Exploration = agent.Exploration
		inferReq.ReadOnly = agent.ReadOnly
		inferReq.AllowFileReads = agent.AllowFileReads
		inferReq.Capabilities = agent.Capabilities
	}

	var llmOutput string
	if cfg.Backend != nil {
		llmOutput, err = cfg.Backend.Complete(callCtx, inferReq)
	} else {
		llmOutput, err = apply.CallLLM(callCtx, llmConfig, systemPrompt, revisedPrompt, cfg.StreamText)
	}
	if err != nil {
		return nil, "", fmt.Errorf("calling LLM: %w", err)
	}

	newEntry := apply.LogEntry{
		Event:     "suggested",
		Root:      cfg.Root,
		Function:  cfg.Suspension.Function,
		Target:    cfg.NodeTitle,
		Step:      step.Name,
		StepIndex: stepIndex,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		RawOutput: llmOutput,
	}

	isLastStep := stepIndex == len(steps)-1
	if isLastStep && step.Output == "ops" {
		ops, err := apply.ParseOps(llmOutput)
		if err != nil {
			return nil, "", fmt.Errorf("parsing ops: %w", err)
		}
		newEntry.Ops = ops
	}

	if err := apply.AppendLogDB(cfg.DB, newEntry); err != nil {
		return nil, "", fmt.Errorf("appending log: %w", err)
	}

	return &newEntry, llmOutput, nil
}
