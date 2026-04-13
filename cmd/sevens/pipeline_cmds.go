package main

// pipeline_cmds.go provides apply/accept/reject/pending commands.
// All orchestration is delegated to the workflow package.

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"sevens/internal/backend"
	"sevens/internal/config"
	"sevens/internal/function"
	"sevens/internal/ui"
	"sevens/internal/workflow"
)

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

			stack, err := openKB()
			if err != nil {
				return fmt.Errorf("opening KB: %w", err)
			}
			defer stack.Close()

			if dryRun {
				fn, _, err := function.LoadFunction(fnName)
				if err != nil {
					return fmt.Errorf("loading function: %w", err)
				}
				steps := fn.EffectiveSteps()
				if len(steps) == 0 {
					return fmt.Errorf("function %q has no steps", fnName)
				}
				step := steps[0]

				// Follow fn delegation: if this step delegates to another function,
				// load that function and use its first step.
				if step.ComposedOf != "" {
					delegated, _, err := function.LoadFunction(step.ComposedOf)
					if err != nil {
						return fmt.Errorf("loading delegated function %q: %w", step.ComposedOf, err)
					}
					delegatedSteps := delegated.EffectiveSteps()
					if len(delegatedSteps) > 0 {
						step = delegatedSteps[0]
					}
				}

				rc, rcErr := function.ResolveContext(context.Background(), stack.KB, resolved, nodeTitle, step, "")
				if rcErr != nil {
					return fmt.Errorf("resolving context: %w", rcErr)
				}
				prompt := function.RenderPrompt(step.Backend.PromptTemplate, rc)
				fmt.Println(prompt)
				return nil
			}

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

			deps := buildDeps(stack, be)
			result, err := workflow.ApplyFunction(ctx(), deps, resolved, fnName, nodeTitle)
			if err != nil {
				return fmt.Errorf("applying function: %w", err)
			}

			displayWorkflowApplyResult(result)
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

			nodeTitle, err := resolveNodeTitle(arg)
			if err != nil {
				return err
			}

			stack, err := openKB()
			if err != nil {
				return fmt.Errorf("opening KB: %w", err)
			}
			defer stack.Close()

			// Resolve backend: use flag, or persisted backend from pipeline
			globalConfig, _ := config.LoadGlobalConfig()
			var be backend.Backend
			if backendFlag != "" || with != "" {
				// Revision needs a backend; explicit flag always honored
				effectiveBackend := backendFlag
				if effectiveBackend == "" {
					// Try to get backend from pipeline
					deps := buildDeps(stack, nil)
					if p, pErr := resolvePipeline(deps, resolved, arg); pErr == nil && p.BackendName != "" {
						effectiveBackend = p.BackendName
					}
				}
				be, _ = backend.FromConfig(globalConfig, effectiveBackend)
			} else {
				be, _ = backend.FromConfig(globalConfig, "")
			}

			deps := buildDeps(stack, be)
			pipeline, err := resolvePipeline(deps, resolved, nodeTitle)
			if err != nil {
				return err
			}

			result, err := workflow.AcceptPipeline(ctx(), deps, resolved, pipeline.ID, with)
			if err != nil {
				return err
			}

			displayWorkflowAcceptResult(result)
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

			nodeTitle, err := resolveNodeTitle(arg)
			if err != nil {
				return err
			}

			stack, err := openKB()
			if err != nil {
				return fmt.Errorf("opening KB: %w", err)
			}
			defer stack.Close()

			deps := buildDeps(stack, nil)
			pipeline, err := resolvePipeline(deps, resolved, nodeTitle)
			if err != nil {
				return err
			}

			if err := workflow.RejectPipeline(ctx(), deps, resolved, pipeline.ID); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "%s %s for %s\n",
				ui.Warning.Render("Rejected"),
				ui.Label.Render(pipeline.FunctionName),
				ui.NodeTitle.Render(pipeline.Target))
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

// formatSuggestions formats parsed suggestions for display.
func formatSuggestions(suggestions []function.Suggestion) string {
	if len(suggestions) == 0 {
		return ""
	}
	var out string
	out += "\nProposed children:\n"
	for i, s := range suggestions {
		out += fmt.Sprintf("  %d. %s\n", i+1, ui.Label.Render(s.Title))
		if s.Rationale != "" {
			out += fmt.Sprintf("     %s\n", ui.Dim.Render(s.Rationale))
		}
	}
	return out
}

// --- Display helpers ---

// displayWorkflowApplyResult shows the result of a workflow.ApplyFunction call.
func displayWorkflowApplyResult(r *workflow.ApplyResult) {
	fmt.Fprintln(os.Stderr, ui.FormatStep(r.FunctionName, r.StepName, r.Target))

	if r.Suspended {
		if len(r.Ops) > 0 {
			for _, op := range r.Ops {
				name := op.Title
				if name == "" {
					name = op.File
				}
				fmt.Fprintln(os.Stderr, ui.FormatOp(op.Action, name))
			}
			fmt.Fprintf(os.Stderr, "\nRun `sevens accept %q` to apply.\n", r.Target)
		} else if len(r.Suggestions) > 0 || r.Output != "" {
			if formatted := formatSuggestions(r.Suggestions); formatted != "" {
				fmt.Fprint(os.Stderr, formatted)
			} else {
				fmt.Print(ui.RenderMarkdownOrPlain(r.Output))
			}
			fmt.Fprintf(os.Stderr, "\nRun `sevens accept %q` to approve and continue.\n", r.Target)
			fmt.Fprintf(os.Stderr, "    or: sevens accept %q --with \"feedback\"\n", r.Target)
			fmt.Fprintf(os.Stderr, "    or: sevens reject %q\n", r.Target)
		}
		return
	}

	// Completed
	if len(r.FilesCreated) > 0 || len(r.FilesEdited) > 0 {
		fmt.Fprintf(os.Stderr, "%s Applied %s to %s\n",
			ui.Success.Render("[apply]"),
			ui.Label.Render(r.FunctionName),
			ui.NodeTitle.Render(r.Target))
	} else if r.Output != "" {
		fmt.Println(ui.RenderMarkdownOrPlain(r.Output))
	}
}

// displayWorkflowAcceptResult shows the result of a workflow.AcceptPipeline call.
func displayWorkflowAcceptResult(r *workflow.AcceptResult) {
	fmt.Fprintln(os.Stderr, ui.FormatStep(r.FunctionName, r.StepName, r.Target))

	if r.Suspended {
		if len(r.Ops) > 0 {
			for _, op := range r.Ops {
				name := op.Title
				if name == "" {
					name = op.File
				}
				fmt.Fprintln(os.Stderr, ui.FormatOp(op.Action, name))
			}
			fmt.Fprintf(os.Stderr, "\nRun `sevens accept %q` to apply.\n", r.Target)
		} else if len(r.Suggestions) > 0 || r.Output != "" {
			if formatted := formatSuggestions(r.Suggestions); formatted != "" {
				fmt.Fprint(os.Stderr, formatted)
			} else {
				fmt.Print(ui.RenderMarkdownOrPlain(r.Output))
			}
			fmt.Fprintf(os.Stderr, "\nRun `sevens accept %q` to approve and continue.\n", r.Target)
			fmt.Fprintf(os.Stderr, "    or: sevens accept %q --with \"feedback\"\n", r.Target)
			fmt.Fprintf(os.Stderr, "    or: sevens reject %q\n", r.Target)
		}
		return
	}

	// Completed
	if len(r.FilesCreated) > 0 || len(r.FilesEdited) > 0 {
		fmt.Fprintf(os.Stderr, "%s Applied %s to %s\n",
			ui.Success.Render("[accept]"),
			ui.Label.Render(r.FunctionName),
			ui.NodeTitle.Render(r.Target))
	} else {
		fmt.Fprintf(os.Stderr, "%s Accepted %s for %s\n",
			ui.Success.Render("[accept]"),
			ui.Label.Render(r.FunctionName),
			ui.NodeTitle.Render(r.Target))
	}
}
