package main

// export_cmds.go provides the export and harvest commands for GUI LLM workflows.

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"sevens/internal/function"
	"sevens/internal/types"
)

func exportCmd() *cobra.Command {
	var root string
	var shape string
	var fnName string

	cmd := &cobra.Command{
		Use:               "export <node-title>",
		Short:             "Render node context for pasting into a GUI LLM",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeTitle, err := resolveNodeTitle(args[0])
			if err != nil {
				return err
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			stack, err := openKB()
			if err != nil {
				return err
			}
			defer stack.Close()

			warnIfStale(resolved)

			// If a function is specified, use its context policy as default shape.
			effectiveShape := shape
			if fnName != "" && !cmd.Flags().Changed("shape") {
				if fn, _, loadErr := function.LoadFunction(fnName); loadErr == nil {
					if fn.ContextPolicy != "" {
						effectiveShape = fn.ContextPolicy
					}
				}
			}

			gather := ResolveGatherSpec(effectiveShape)

			w, err := stack.KB.Walk(context.Background(), resolved, nodeTitle, gather)
			if err != nil {
				return fmt.Errorf("walking node: %w", err)
			}

			var sb strings.Builder

			// Function instruction (if provided).
			if fnName != "" {
				if fn, _, loadErr := function.LoadFunction(fnName); loadErr == nil {
					steps := fn.EffectiveSteps()
					if len(steps) > 0 {
						step := steps[0]
						if step.Backend.SystemPrompt != "" {
							sb.WriteString("## Instruction\n\n")
							sb.WriteString(step.Backend.SystemPrompt)
							sb.WriteString("\n\n")
						}
						if step.Backend.PromptTemplate != "" {
							// Resolve context and render the prompt template.
							rc, rcErr := function.ResolveContext(context.Background(), stack.KB, resolved, nodeTitle, step, "")
							if rcErr == nil {
								rendered := function.RenderPrompt(step.Backend.PromptTemplate, rc)
								sb.WriteString("## Prompt\n\n")
								sb.WriteString(rendered)
								sb.WriteString("\n\n")
							}
						}
					}
				}
			}

			// Node context.
			sb.WriteString("## Context: ")
			sb.WriteString(w.Target.Title)
			sb.WriteString("\n\n")

			if w.Parent != nil {
				sb.WriteString("Parent: ")
				sb.WriteString(w.Parent.Title)
				sb.WriteString("\n\n")
			}

			if len(w.Children) > 0 {
				sb.WriteString("Children: ")
				titles := make([]string, len(w.Children))
				for i, c := range w.Children {
					titles[i] = c.Title
				}
				sb.WriteString(strings.Join(titles, ", "))
				sb.WriteString("\n\n")
			}

			// Target content.
			if w.Target.Content != "" {
				sb.WriteString("### ")
				sb.WriteString(w.Target.Title)
				sb.WriteString("\n\n")
				sb.WriteString(w.Target.Content)
				sb.WriteString("\n\n")
			}

			// Parent content.
			if gather.Parent && w.Parent != nil && w.Parent.Content != "" {
				sb.WriteString("### ")
				sb.WriteString(w.Parent.Title)
				sb.WriteString(" (parent)\n\n")
				sb.WriteString(w.Parent.Content)
				sb.WriteString("\n\n")
			}

			// Children content.
			if (gather.Children || gather.Subtree) && len(w.Children) > 0 {
				for _, child := range w.Children {
					if child.Content == "" {
						continue
					}
					sb.WriteString("### ")
					sb.WriteString(child.Title)
					sb.WriteString(" (child)\n\n")
					sb.WriteString(child.Content)
					sb.WriteString("\n\n")
				}
			}

			// Sibling content.
			if gather.Siblings && len(w.Siblings) > 0 {
				for _, sib := range w.Siblings {
					if sib.Content == "" {
						continue
					}
					sb.WriteString("### ")
					sb.WriteString(sib.Title)
					sb.WriteString(" (sibling)\n\n")
					sb.WriteString(sib.Content)
					sb.WriteString("\n\n")
				}
			}

			// Subtree content (beyond immediate children).
			if gather.Subtree && len(w.SubtreeNodes) > 0 {
				for _, node := range w.SubtreeNodes {
					if node.Content == "" {
						continue
					}
					sb.WriteString("### ")
					sb.WriteString(node.Title)
					sb.WriteString(" (subtree)\n\n")
					sb.WriteString(node.Content)
					sb.WriteString("\n\n")
				}
			}

			// Footer note.
			sb.WriteString("---\n")
			sb.WriteString("This context was exported from a knowledge graph managed by sevens.\n")
			sb.WriteString("Node: ")
			sb.WriteString(nodeTitle)
			sb.WriteString(" | Shape: ")
			sb.WriteString(effectiveShape)
			sb.WriteString("\n")

			fmt.Print(sb.String())
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().StringVar(&shape, "shape", "sevens/neighborhood", "Walk shape: sevens/minimal, sevens/neighborhood, sevens/children, sevens/subtree (sevens/ prefix optional)")
	cmd.Flags().StringVar(&fnName, "function", "", "Include function instruction as framing prompt")
	return cmd
}

func harvestCmd() *cobra.Command {
	var root string
	var typeName string
	var fnName string

	cmd := &cobra.Command{
		Use:               "harvest <node-title>",
		Short:             "Print a prompt for structuring GUI LLM output as importable JSON",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeNodeTitles,
		RunE: func(cmd *cobra.Command, args []string) error {
			nodeTitle, err := resolveNodeTitle(args[0])
			if err != nil {
				return err
			}

			resolved, err := resolveRoot(root)
			if err != nil {
				return fmt.Errorf("resolving root: %w", err)
			}

			// Validate root exists.
			_ = resolved

			// Resolve output type: explicit --type, or derived from --function.
			effectiveType := typeName
			if effectiveType == "" && fnName != "" {
				if fn, _, loadErr := function.LoadFunction(fnName); loadErr == nil {
					steps := fn.EffectiveSteps()
					if len(steps) > 0 {
						lastStep := steps[len(steps)-1]
						if lastStep.Output.TypeName != "" {
							effectiveType = lastStep.Output.TypeName
						}
					}
				}
			}

			var sb strings.Builder

			sb.WriteString("# Instructions for structuring your response\n\n")
			sb.WriteString("Please structure your response as a JSON object that can be imported into sevens.\n")
			sb.WriteString("The target node is: **")
			sb.WriteString(nodeTitle)
			sb.WriteString("**\n\n")

			// Schema instruction from type.
			if effectiveType != "" {
				allTypes, loadErr := types.LoadAllTypeDefs()
				if loadErr == nil {
					if td, ok := allTypes[effectiveType]; ok {
						schema := types.ComposeSchemaInstruction(td, allTypes)
						if schema != "" {
							sb.WriteString("## Output format\n\n")
							sb.WriteString(schema)
							sb.WriteString("\n\n")
						}
					}
				}
			}

			// If no type-based schema, provide generic ops format.
			if effectiveType == "" {
				sb.WriteString("## Output format\n\n")
				sb.WriteString("Respond with a JSON object containing an `ops` field with an array of operations.\n")
				sb.WriteString("Each operation should have:\n")
				sb.WriteString("- `action`: one of `create` or `edit`\n")
				sb.WriteString("- For `create`: `title`, `parent`, `content` (markdown body)\n")
				sb.WriteString("- For `edit`: `title`, `content` (new markdown body)\n\n")
				sb.WriteString("Example:\n")
				sb.WriteString("```json\n")
				sb.WriteString("{\n")
				sb.WriteString("  \"ops\": [\n")
				sb.WriteString("    {\"action\": \"create\", \"title\": \"New Node\", \"parent\": \"" + nodeTitle + "\", \"content\": \"...\"}\n")
				sb.WriteString("  ]\n")
				sb.WriteString("}\n")
				sb.WriteString("```\n\n")
			}

			// Submit command.
			sb.WriteString("## Importing the response\n\n")
			sb.WriteString("After receiving the response, save the JSON to a file and run:\n\n")
			sb.WriteString("```\n")

			outputType := "ops"
			submitFn := fnName
			if submitFn == "" {
				submitFn = "manual"
			}

			sb.WriteString(fmt.Sprintf("sevens submit %q --function %s --output %s --response-file response.json",
				nodeTitle, submitFn, outputType))

			if root != "" {
				sb.WriteString(fmt.Sprintf(" --root %q", root))
			}
			sb.WriteString("\n```\n")

			fmt.Print(sb.String())
			return nil
		},
	}

	cmd.Flags().StringVar(&root, "root", "", "Root directory")
	cmd.Flags().StringVar(&typeName, "type", "", "Output type for schema instructions")
	cmd.Flags().StringVar(&fnName, "function", "", "Function name (derives output type)")
	return cmd
}

