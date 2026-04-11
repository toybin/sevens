package main

// pipeline_cmds.go provides apply/accept/reject/pending commands backed
// by the new function.Executor and pipeline state machine.

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"sevens/internal/backend"
	"sevens/internal/config"
	"sevens/internal/function"
	"sevens/internal/projection"
	projmd "sevens/internal/projection/md"
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

			// Load function definition
			fn, _, err := function.LoadFunction(fnName)
			if err != nil {
				return fmt.Errorf("loading function: %w", err)
			}

			// Open KB stack
			stack, err := openKB()
			if err != nil {
				return fmt.Errorf("opening KB: %w", err)
			}
			defer stack.Close()

			if dryRun {
				// Dry-run: render the prompt for the first step and print it
				steps := fn.EffectiveSteps()
				if len(steps) == 0 {
					return fmt.Errorf("function %q has no steps", fnName)
				}
				rc, rcErr := function.ResolveContext(context.Background(), stack.KB, resolved, nodeTitle, steps[0], "")
				if rcErr != nil {
					return fmt.Errorf("resolving context: %w", rcErr)
				}
				prompt := function.RenderPrompt(steps[0].Backend.PromptTemplate, rc)
				fmt.Println(prompt)
				return nil
			}

			// Create backend
			globalConfig, err := config.LoadGlobalConfig()
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
					return fmt.Errorf("no pending suggestions for %s", nodeTitle)
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
			fn, _, err := function.LoadFunction(pipeline.FunctionName)
			if err != nil {
				return fmt.Errorf("loading function: %w", err)
			}

			if with != "" {
				// Revision
				globalConfig, _ := config.LoadGlobalConfig()
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
			globalConfig, _ := config.LoadGlobalConfig()
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
					// Execute file ops via projection
					proj := openProjection(stack)
					projOps := make([]projection.FileOp, len(result.Result.Ops))
					for i, op := range result.Result.Ops {
						projOps[i] = projection.FileOp(op)
					}
					applyResult, err := proj.ApplyOps(context.Background(), resolved, projOps)
					if err != nil {
						return fmt.Errorf("executing ops: %w", err)
					}

					if projmd.IsGitRepo(resolved) {
						allFiles := append(applyResult.FilesCreated, applyResult.FilesEdited...)
						if len(allFiles) > 0 {
							_, gerr := projmd.CommitFiles(resolved,
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
					return fmt.Errorf("no pending suggestions for %s", nodeTitle)
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

			pipelines, err := ps.FindPending(context.Background(), resolved)
			if err != nil {
				return fmt.Errorf("listing pending pipelines: %w", err)
			}

			if len(pipelines) == 0 {
				fmt.Fprintln(os.Stderr, "No pending suggestions")
				return nil
			}

			for _, p := range pipelines {
				steps := ""
				if p.CurrentStep > 0 {
					steps = fmt.Sprintf("step %d", p.CurrentStep)
				}
				fmt.Println(ui.FormatPending(p.Target, p.FunctionName, steps, "", p.ID))
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
