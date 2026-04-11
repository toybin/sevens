package main

// pipeline_cmds.go provides apply/accept/reject/pending commands backed
// by the new function.Executor and pipeline state machine.

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"sevens/internal/apply"
	"sevens/internal/backend"
	"sevens/internal/engine"
	"sevens/internal/function"
	"sevens/internal/ui"
)

// buildExecutor creates a function.Executor from the CLI flags and kbStack.
func buildExecutor(stack *kbStack, be backend.Backend) *function.Executor {
	var tb function.TransformBackend
	if be != nil {
		tb = function.NewLLMBackend(be)
	}
	ps := function.NewPipelineStore(stack.Store)
	return function.NewExecutor(stack.KB, tb, ps)
}

func applyCmd2() *cobra.Command {
	var root string
	var dryRun bool
	var yes bool
	var model string
	var backendFlag string

	cmd := &cobra.Command{
		Use:   "apply <function> <node-title>",
		Short: "Apply a function to a node",
		Args:  cobra.ExactArgs(2),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) == 0 {
				return completeFunctionNames(cmd, args, toComplete)
			}
			return completeNodeTitles(cmd, args, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			fnName := args[0]
			nodeTitle, err := resolveNodeTitle(args[1])
			if err != nil {
				return err
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			// Load old-style function definition and convert
			oldFn, err := apply.LoadFunction(fnName)
			if err != nil {
				return fmt.Errorf("loading function: %w", err)
			}
			fn := function.ConvertFunction(oldFn)

			if dryRun {
				// For dry-run, fall back to old pipeline which handles prompt rendering/display
				return runPipeline(resolved, nodeTitle, oldFn, 0, "", true, !yes, nil, model, nil, backendFlag, "", "")
			}

			// Open KB stack
			stack, err := openKB()
			if err != nil {
				return fmt.Errorf("opening KB: %w", err)
			}
			defer stack.Close()

			// Create backend
			globalConfig, err := apply.LoadGlobalConfig()
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			if model != "" {
				resolvedModel := globalConfig.ResolveModel(model)
				if resolvedModel.Model != globalConfig.LLM.Model || model == resolvedModel.Model {
					globalConfig.LLM = resolvedModel
				} else {
					globalConfig.LLM.Model = model
				}
			}

			be, err := backend.FromConfig(globalConfig, backendFlag)
			if err != nil {
				return fmt.Errorf("initializing backend: %w", err)
			}
			fmt.Fprintf(os.Stderr, "[backend] %s\n", be.Name())

			exec := buildExecutor(stack, be)

			result, err := exec.Apply(context.Background(), resolved, fn, nodeTitle)
			if err != nil {
				return fmt.Errorf("applying function: %w", err)
			}

			displayApplyResult(result, fn, nodeTitle)
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print rendered prompt without calling LLM")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip cost confirmation")
	cmd.Flags().StringVar(&model, "model", "", "Model name or profile")
	cmd.Flags().StringVar(&backendFlag, "backend", "", "Inference backend (codex, claude, anthropic)")
	return cmd
}

func acceptCmd2() *cobra.Command {
	var root string
	var with string
	var yes bool
	var backendFlag string

	cmd := &cobra.Command{
		Use:               "accept <node-title-or-pipeline-id>",
		Short:             "Accept pending suggestions for a node",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := args[0]

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return fmt.Errorf("opening KB: %w", err)
			}
			defer stack.Close()

			ps := function.NewPipelineStore(stack.Store)

			// Find the pipeline
			var pipeline *function.Pipeline
			if strings.HasPrefix(arg, "pipeline:") {
				pipeline, err = ps.Load(context.Background(), arg)
				if err != nil {
					return fmt.Errorf("loading pipeline: %w", err)
				}
			} else {
				nodeTitle, rerr := resolveNodeTitle(arg)
				if rerr != nil {
					return rerr
				}
				pending, err := ps.FindPending(context.Background(), resolved)
				if err != nil {
					return fmt.Errorf("finding pending: %w", err)
				}
				var matches []*function.Pipeline
				for _, p := range pending {
					if p.Target == nodeTitle {
						matches = append(matches, p)
					}
				}
				if len(matches) == 0 {
					// Fall back to old suspension system
					return runLegacyAccept(resolved, arg, with, !yes, backendFlag)
				}
				if len(matches) > 1 {
					fmt.Fprintf(os.Stderr, "%s multiple pending pipelines for %s:\n",
						ui.Warning.Render("[ambiguous]"), ui.NodeTitle.Render(nodeTitle))
					for _, p := range matches {
						fmt.Fprintf(os.Stderr, "  %s  %s  step %d\n", p.ID, p.FunctionName, p.CurrentStep)
					}
					return fmt.Errorf("ambiguous: pass the pipeline id instead of the node title")
				}
				pipeline = matches[0]
			}

			// Load the function definition
			oldFn, err := apply.LoadFunction(pipeline.FunctionName)
			if err != nil {
				return fmt.Errorf("loading function: %w", err)
			}
			fn := function.ConvertFunction(oldFn)

			if with != "" {
				// Revision
				globalConfig, _ := apply.LoadGlobalConfig()
				be, beErr := backend.FromConfig(globalConfig, backendFlag)
				if beErr != nil {
					return fmt.Errorf("initializing backend: %w", beErr)
				}
				exec := buildExecutor(stack, be)

				result, err := exec.Revise(context.Background(), resolved, fn, pipeline.ID, with)
				if err != nil {
					return fmt.Errorf("revising: %w", err)
				}

				displayApplyResult(result, fn, pipeline.Target)
				return nil
			}

			// No --with: accept and advance
			globalConfig, _ := apply.LoadGlobalConfig()
			be, beErr := backend.FromConfig(globalConfig, backendFlag)
			if beErr != nil {
				fmt.Fprintf(os.Stderr, "[warn] backend init: %v, continuing without backend\n", beErr)
			}

			exec := buildExecutor(stack, be)

			result, err := exec.Accept(context.Background(), resolved, fn, pipeline.ID)
			if err != nil {
				return fmt.Errorf("accepting: %w", err)
			}

			if result.Suspended {
				displayApplyResult(result, fn, pipeline.Target)
			} else {
				// Pipeline completed
				if result.Result != nil && len(result.Result.Ops) > 0 {
					// Execute file ops
					db, err := openDB()
					if err != nil {
						return err
					}
					defer db.Close()

					ops := convertOps(result.Result.Ops)
					created, edited, err := apply.ExecuteOps(ops, resolved, db)
					if err != nil {
						return fmt.Errorf("executing ops: %w", err)
					}

					if apply.IsGitRepo(resolved) {
						allFiles := append(created, edited...)
						if len(allFiles) > 0 {
							_, gerr := apply.CommitFiles(resolved,
								fmt.Sprintf("sevens: apply %s to %q", pipeline.FunctionName, pipeline.Target),
								allFiles)
							if gerr != nil {
								fmt.Fprintf(os.Stderr, "[warn] git commit: %v\n", gerr)
							}
						}
					}

					if err := syncRoot(resolved); err != nil {
						fmt.Fprintf(os.Stderr, "[warn] re-sync: %v\n", err)
					}

					fmt.Fprintf(os.Stderr, "%s Applied %s to %s\n",
						ui.Success.Render("[accept]"),
						ui.Label.Render(pipeline.FunctionName),
						ui.NodeTitle.Render(pipeline.Target))
				} else {
					fmt.Fprintf(os.Stderr, "%s Accepted %s for %s\n",
						ui.Success.Render("[accept]"),
						ui.Label.Render(pipeline.FunctionName),
						ui.NodeTitle.Render(pipeline.Target))
				}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().StringVar(&with, "with", "", "Revision feedback to re-run with")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip cost confirmation")
	cmd.Flags().StringVar(&backendFlag, "backend", "", "Inference backend override")
	return cmd
}

func rejectCmd2() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:               "reject <node-title-or-pipeline-id>",
		Short:             "Reject pending suggestions for a node",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			arg := args[0]

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return fmt.Errorf("opening KB: %w", err)
			}
			defer stack.Close()

			ps := function.NewPipelineStore(stack.Store)

			var pipeline *function.Pipeline
			if strings.HasPrefix(arg, "pipeline:") {
				pipeline, err = ps.Load(context.Background(), arg)
				if err != nil {
					return fmt.Errorf("loading pipeline: %w", err)
				}
			} else {
				nodeTitle, rerr := resolveNodeTitle(arg)
				if rerr != nil {
					return rerr
				}
				pending, err := ps.FindPending(context.Background(), resolved)
				if err != nil {
					return fmt.Errorf("finding pending: %w", err)
				}
				var matches []*function.Pipeline
				for _, p := range pending {
					if p.Target == nodeTitle {
						matches = append(matches, p)
					}
				}
				if len(matches) == 0 {
					// Fall back to old system
					return runLegacyReject(resolved, arg)
				}
				if len(matches) > 1 {
					fmt.Fprintf(os.Stderr, "%s multiple pending pipelines for %s:\n",
						ui.Warning.Render("[ambiguous]"), ui.NodeTitle.Render(nodeTitle))
					for _, p := range matches {
						fmt.Fprintf(os.Stderr, "  %s  %s  step %d\n", p.ID, p.FunctionName, p.CurrentStep)
					}
					return fmt.Errorf("ambiguous: pass the pipeline id instead of the node title")
				}
				pipeline = matches[0]
			}

			exec := function.NewExecutor(stack.KB, nil, ps)
			p, err := exec.Reject(context.Background(), pipeline.ID)
			if err != nil {
				return fmt.Errorf("rejecting: %w", err)
			}

			fmt.Fprintf(os.Stderr, "%s %s for %s\n",
				ui.Warning.Render("Rejected"),
				ui.Label.Render(p.FunctionName),
				ui.NodeTitle.Render(p.Target))
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	return cmd
}

func pendingCmd2() *cobra.Command {
	var root string

	cmd := &cobra.Command{
		Use:   "pending",
		Short: "List nodes with pending suggestions",
		RunE: func(cmd *cobra.Command, args []string) error {
			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return fmt.Errorf("opening KB: %w", err)
			}
			defer stack.Close()

			ps := function.NewPipelineStore(stack.Store)

			// New pipeline-based pending
			pipelines, err := ps.FindPending(context.Background(), resolved)
			if err != nil {
				return fmt.Errorf("listing pending pipelines: %w", err)
			}

			// Also check old suspension system
			db, err := openDB()
			if err != nil {
				return err
			}
			defer db.Close()

			oldSuspensions, _ := engine.ListSuspensions(db, resolved)

			if len(pipelines) == 0 && len(oldSuspensions) == 0 {
				fmt.Fprintln(os.Stderr, "No pending suggestions")
				return nil
			}

			// Display new pipelines
			for _, p := range pipelines {
				steps := ""
				if p.CurrentStep > 0 {
					steps = fmt.Sprintf("step %d", p.CurrentStep)
				}
				fmt.Println(ui.FormatPending(p.Target, p.FunctionName, steps, "", p.ID))
			}

			// Display old suspensions
			for _, sus := range oldSuspensions {
				fmt.Println(ui.FormatPending(
					orDefault(sus.TargetLabel, sus.Target),
					sus.Function, sus.StepName, sus.Summary, sus.Subject))
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	return cmd
}

// displayApplyResult shows the result of an Apply/Accept/Revise call.
func displayApplyResult(result *function.ApplyResult, fn *function.Function, nodeTitle string) {
	steps := fn.EffectiveSteps()
	stepName := ""
	if result.Pipeline.CurrentStep < len(steps) {
		stepName = steps[result.Pipeline.CurrentStep].Name
	} else if len(steps) > 0 {
		stepName = steps[len(steps)-1].Name
	}

	if result.Suspended {
		fmt.Fprintln(os.Stderr, ui.FormatStep(fn.Name, stepName, nodeTitle))
		if result.Result != nil {
			if len(result.Result.Ops) > 0 {
				for _, op := range result.Result.Ops {
					name := op.Title
					if name == "" {
						name = op.File
					}
					fmt.Fprintln(os.Stderr, ui.FormatOp(op.Action, name))
				}
				fmt.Fprintf(os.Stderr, "\nRun `sevens accept %q` to apply.\n", nodeTitle)
			} else {
				fmt.Print(ui.RenderMarkdownOrPlain(result.Result.Raw))
				fmt.Fprintf(os.Stderr, "\nRun `sevens accept %q` to approve and continue.\n", nodeTitle)
			}
		}
	} else {
		fmt.Fprintln(os.Stderr, ui.FormatStep(fn.Name, stepName, nodeTitle))
		if result.Result != nil && result.Result.Raw != "" {
			fmt.Println(ui.RenderMarkdownOrPlain(result.Result.Raw))
		}
	}
}

// convertOps converts function.FileOp to apply.FileOp for execution.
func convertOps(ops []function.FileOp) []apply.FileOp {
	result := make([]apply.FileOp, len(ops))
	for i, op := range ops {
		result[i] = apply.FileOp{
			Action:  op.Action,
			Title:   op.Title,
			Parent:  op.Parent,
			File:    op.File,
			OldText: op.OldText,
			NewText: op.NewText,
			Content: op.Content,
		}
	}
	return result
}

// runLegacyAccept falls back to the old engine.Suspension-based accept.
func runLegacyAccept(resolved, arg, with string, confirm bool, backendFlag string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	var sus *engine.Suspension
	var susSubject string

	if strings.HasPrefix(arg, "suspension:") {
		sus, err = engine.FindSuspensionBySubject(db, resolved, arg)
		if err != nil {
			return fmt.Errorf("finding pending: %w", err)
		}
		if sus == nil {
			return fmt.Errorf("no pending suspension with id %s", arg)
		}
		susSubject = arg
	} else {
		nodeTitle, rerr := resolveNodeTitle(arg)
		if rerr != nil {
			return rerr
		}
		all, ferr := engine.FindSuspensions(db, resolved, nodeTitle)
		if ferr != nil {
			return fmt.Errorf("finding pending: %w", ferr)
		}
		if len(all) == 0 {
			return fmt.Errorf("no pending suggestions for %s", nodeTitle)
		}
		if len(all) > 1 {
			fmt.Fprintf(os.Stderr, "%s multiple pending suspensions for %s:\n",
				ui.Warning.Render("[ambiguous]"), ui.NodeTitle.Render(nodeTitle))
			for _, s := range all {
				fmt.Println(ui.FormatPending(orDefault(s.TargetLabel, s.Target), s.Function, s.StepName, s.Summary, s.Subject))
			}
			return fmt.Errorf("ambiguous: pass the suspension id instead of the node title")
		}
		sus = &all[0]
		susSubject = sus.Subject
	}
	nodeTitle := sus.Target

	fn, err := apply.LoadFunction(sus.Function)
	if err != nil {
		return fmt.Errorf("loading function: %w", err)
	}

	steps := fn.EffectiveSteps()
	nextStep := sus.StepIndex + 1

	if with != "" {
		// Revision path -- delegate back to old engine
		stepIndex := sus.StepIndex
		if stepIndex >= len(steps) {
			stepIndex = len(steps) - 1
		}
		step := steps[stepIndex]

		var streamTo *os.File
		if step.Output == "text" {
			streamTo = os.Stderr
		}

		globalConfig, _ := apply.LoadGlobalConfig()
		resolvedBackend := backendFlag
		if resolvedBackend == "" {
			resolvedBackend = sus.Backend
		}
		be, beErr := backend.FromConfig(globalConfig, resolvedBackend)
		if beErr != nil {
			fmt.Fprintf(os.Stderr, "[warn] backend init: %v, falling back to API\n", beErr)
		}

		newEntry, llmOutput, err := engine.ReviseStep(engine.ReviseConfig{
			DB:         db,
			Root:       resolved,
			NodeTitle:  nodeTitle,
			Function:   fn,
			Suspension: sus,
			Feedback:   with,
			Confirm:    confirm,
			StreamText: streamTo,
			Backend:    be,
		})
		if err != nil {
			return err
		}
		if newEntry == nil {
			fmt.Fprintf(os.Stderr, "%s Cancelled by user\n", ui.Warning.Render("[abort]"))
			return nil
		}

		isLastStep := stepIndex == len(steps)-1
		if isLastStep && step.Output == "ops" {
			newEntry.Summary = summarizeOutput("ops", llmOutput, newEntry.Ops)
			printSuggestion(*newEntry)
		} else if isLastStep {
			fmt.Fprintln(os.Stderr, ui.FormatStep(sus.Function, step.Name, orDefault(sus.TargetLabel, nodeTitle)))
			fmt.Println(ui.RenderMarkdownOrPlain(llmOutput))
		} else {
			fmt.Fprintln(os.Stderr, ui.FormatStep(fn.Name, step.Name, orDefault(sus.TargetLabel, nodeTitle)))
			printIntermediateOutput(llmOutput)
			fmt.Fprintf(os.Stderr, "\nRun `sevens accept %q` to approve and continue.\n", nodeTitle)
		}

		revBackendName := ""
		if be != nil {
			revBackendName = be.Name()
		}
		engine.WriteSuspension(db, resolved, nodeTitle, orDefault(sus.TargetLabel, nodeTitle), resolveSuspensionBlockForCLI(db, resolved, nodeTitle, sus), sus.Function, step.Name, sus.GateType, step.Output, newEntry.RawOutput, stepIndex, newEntry.Summary, newEntry.Ops, revBackendName)
		engine.ResolveSuspension(db, susSubject, "revised")
		return nil
	}

	// Accept path
	if nextStep < len(steps) {
		acceptEntry := apply.LogEntry{
			Event:     "accepted",
			Root:      resolved,
			Function:  sus.Function,
			Target:    nodeTitle,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		if err := apply.AppendLogDB(db, acceptEntry); err != nil {
			return fmt.Errorf("appending log: %w", err)
		}
		engine.ResolveSuspension(db, susSubject, "accepted")

		pipelineBackend := backendFlag
		if pipelineBackend == "" {
			pipelineBackend = sus.Backend
		}
		return runPipeline(resolved, nodeTitle, fn, nextStep, sus.Output, false, confirm, nil, "", nil, pipelineBackend, sus.BlockPath, sus.BlockID)
	}

	// Last step
	lastStep := steps[sus.StepIndex]
	if lastStep.Output != "ops" {
		acceptEntry := apply.LogEntry{
			Event:     "accepted",
			Root:      resolved,
			Function:  sus.Function,
			Target:    nodeTitle,
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		}
		if err := apply.AppendLogDB(db, acceptEntry); err != nil {
			return fmt.Errorf("appending log: %w", err)
		}
		engine.ResolveSuspension(db, susSubject, "accepted")
		fmt.Fprintf(os.Stderr, "%s Acknowledged %s output for %s\n",
			ui.Success.Render("[accept]"), lastStep.Output, ui.NodeTitle.Render(nodeTitle))
		return nil
	}

	created, edited, err := apply.ExecuteOps(sus.Ops, resolved, db)
	if err != nil {
		return fmt.Errorf("executing ops: %w", err)
	}

	acceptEntry := apply.LogEntry{
		Event:     "accepted",
		Root:      resolved,
		Function:  sus.Function,
		Target:    nodeTitle,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if err := apply.AppendLogDB(db, acceptEntry); err != nil {
		return fmt.Errorf("appending log: %w", err)
	}
	engine.ResolveSuspension(db, susSubject, "accepted")

	commitHash := ""
	if apply.IsGitRepo(resolved) {
		allFiles := append(created, edited...)
		if len(allFiles) > 0 {
			hash, cerr := apply.CommitFiles(resolved,
				fmt.Sprintf("sevens: apply %s to %q", sus.Function, nodeTitle), allFiles)
			if cerr != nil {
				fmt.Fprintf(os.Stderr, "%s git commit failed: %v\n", ui.Success.Render("[accept]"), cerr)
			} else {
				commitHash = hash
			}
		}
	}

	appliedEntry := apply.LogEntry{
		Event:        "applied",
		Root:         resolved,
		Function:     sus.Function,
		Target:       nodeTitle,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
		Commit:       commitHash,
		FilesCreated: created,
		FilesEdited:  edited,
	}
	if err := apply.AppendLogDB(db, appliedEntry); err != nil {
		return fmt.Errorf("appending log: %w", err)
	}

	if err := syncRoot(resolved); err != nil {
		fmt.Fprintf(os.Stderr, "%s re-sync failed: %v\n", ui.Success.Render("[accept]"), err)
	}

	return nil
}

// runLegacyReject falls back to the old engine.Suspension-based reject.
func runLegacyReject(resolved, arg string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	var sus *engine.Suspension
	var susSubject string

	if strings.HasPrefix(arg, "suspension:") {
		sus, err = engine.FindSuspensionBySubject(db, resolved, arg)
		if err != nil {
			return fmt.Errorf("finding pending: %w", err)
		}
		if sus == nil {
			return fmt.Errorf("no pending suspension with id %s", arg)
		}
		susSubject = arg
	} else {
		nodeTitle, rerr := resolveNodeTitle(arg)
		if rerr != nil {
			return rerr
		}
		all, ferr := engine.FindSuspensions(db, resolved, nodeTitle)
		if ferr != nil {
			return fmt.Errorf("finding pending: %w", ferr)
		}
		if len(all) == 0 {
			return fmt.Errorf("no pending suggestions for %s", nodeTitle)
		}
		if len(all) > 1 {
			fmt.Fprintf(os.Stderr, "%s multiple pending suspensions for %s:\n",
				ui.Warning.Render("[ambiguous]"), ui.NodeTitle.Render(nodeTitle))
			for _, s := range all {
				fmt.Println(ui.FormatPending(orDefault(s.TargetLabel, s.Target), s.Function, s.StepName, s.Summary, s.Subject))
			}
			return fmt.Errorf("ambiguous: pass the suspension id instead of the node title")
		}
		sus = &all[0]
		susSubject = sus.Subject
	}
	nodeTitle := sus.Target

	entry := apply.LogEntry{
		Event:     "rejected",
		Root:      resolved,
		Target:    nodeTitle,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if err := apply.AppendLogDB(db, entry); err != nil {
		return fmt.Errorf("appending log: %w", err)
	}

	engine.ResolveSuspension(db, susSubject, "rejected")
	fmt.Fprintf(os.Stderr, "%s %s\n", ui.Warning.Render("Rejected suggestions for"), ui.NodeTitle.Render(nodeTitle))
	return nil
}
