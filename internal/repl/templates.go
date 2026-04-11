package repl

import (
	"fmt"
	"os"
	"strings"

	"sevens/internal/apply"
	projmd "sevens/internal/projection/md"
	"sevens/internal/ui"
)

func (r *REPL) handleTemplates() error {
	templates, err := apply.ListTemplates()
	if err != nil {
		return fmt.Errorf("listing templates: %w", err)
	}
	if len(templates) == 0 {
		r.printSystem("no templates defined")
		return nil
	}

	fmt.Println()
	for _, name := range templates {
		tmpl, err := apply.LoadTemplate(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s %s: %v\n", ui.Warning.Render("[warn]"), name, err)
			continue
		}
		fmt.Printf("  %s  %s\n", ui.Label.Render(name), ui.Dim.Render(tmpl.Description))
	}
	fmt.Println()
	return nil
}

func (r *REPL) handleCapture(tokens []string) error {
	tmpl, err := apply.LoadTemplate("inbox-capture")
	if err != nil {
		return err
	}
	root, parent, _, vars, args, dryRun, err := parseTemplateInvokeArgs(tokens[1:])
	if err != nil {
		return err
	}
	if err := r.validateRootFlag(root); err != nil {
		return err
	}
	vars = apply.BindTemplateArgs(tmpl, args, vars)
	return r.runTemplate(tmpl, parent, "", vars, dryRun)
}

func (r *REPL) handleInstantiate(tokens []string) error {
	if len(tokens) < 2 {
		return fmt.Errorf("usage: instantiate <template> [args...]")
	}
	tmpl, err := apply.LoadTemplate(tokens[1])
	if err != nil {
		return err
	}
	root, parent, targetNode, vars, args, dryRun, err := parseTemplateInvokeArgs(tokens[2:])
	if err != nil {
		return err
	}
	if err := r.validateRootFlag(root); err != nil {
		return err
	}
	vars = apply.BindTemplateArgs(tmpl, args, vars)
	if targetNode == "" && (tmpl.Mode == "append-node" || tmpl.Mode == "insert-block") && r.focus != "" {
		targetNode = r.focus
	}
	return r.runTemplate(tmpl, parent, targetNode, vars, dryRun)
}

func (r *REPL) runTemplate(tmpl *apply.NodeTemplate, parent string, targetNode string, vars map[string]string, dryRun bool) error {
	if targetNode == "." {
		targetNode = r.focus
	}
	if canonical := r.resolveTitle(targetNode); canonical != "" {
		targetNode = canonical
	}
	if canonical := r.resolveTitle(parent); canonical != "" {
		parent = canonical
	}
	if dryRun {
		preview, err := apply.PreviewTemplate(r.db, r.root, tmpl, apply.TemplateExecutionOptions{
			Parent:     parent,
			TargetNode: targetNode,
			Vars:       vars,
		})
		if err != nil {
			return err
		}
		r.printTemplatePreview(preview)
		return nil
	}

	result, err := apply.ExecuteTemplate(r.db, r.root, tmpl, apply.TemplateExecutionOptions{
		Parent:     parent,
		TargetNode: targetNode,
		Vars:       vars,
	})
	if err != nil {
		return err
	}

	files := append([]string(nil), result.Created...)
	files = append(files, result.Edited...)
	if projmd.IsGitRepo(r.root) && len(files) > 0 {
		h, cerr := projmd.CommitFiles(r.root, result.CommitMessage, files)
		if cerr != nil {
			fmt.Fprintf(os.Stderr, "%s git commit: %v\n", ui.Warning.Render("[warn]"), cerr)
		} else if len(result.Created) > 0 {
			r.printSystem("templated %s (%s)", strings.Join(result.Created, ", "), h)
		} else {
			r.printSystem("templated %s (%s)", strings.Join(result.Edited, ", "), h)
		}
	} else if len(result.Created) > 0 {
		r.printSystem("templated %s", strings.Join(result.Created, ", "))
	} else if len(result.Edited) > 0 {
		r.printSystem("templated %s", strings.Join(result.Edited, ", "))
	}

	if err := r.resync(); err != nil {
		fmt.Fprintf(os.Stderr, "%s re-sync: %v\n", ui.Warning.Render("[warn]"), err)
	}
	if result.PrimaryTitle != "" {
		title := result.PrimaryTitle
		if canonical := r.resolveTitle(title); canonical != "" {
			title = canonical
		}
		r.setFocus(title)
	}
	return nil
}

func (r *REPL) printTemplatePreview(preview *apply.TemplatePreview) {
	fmt.Println()
	fmt.Println(ui.Label.Render("Template Preview"))
	fmt.Printf("  %s %s\n", ui.Dim.Render("template:"), preview.TemplateName)
	fmt.Printf("  %s %s\n", ui.Dim.Render("mode:"), preview.Mode)
	if preview.Draft {
		fmt.Printf("  %s yes\n", ui.Dim.Render("draft:"))
	}
	if len(preview.Missing) > 0 {
		fmt.Printf("  %s %s\n", ui.Dim.Render("missing:"), strings.Join(preview.Missing, ", "))
	}
	switch preview.Mode {
	case "append-node", "insert-block":
		fmt.Printf("  %s %s\n", ui.Dim.Render("target:"), preview.TargetNode)
		if preview.Heading != "" {
			fmt.Printf("  %s %s\n", ui.Dim.Render("heading:"), preview.Heading)
			if preview.CreateIfMissing {
				fmt.Printf("  %s yes\n", ui.Dim.Render("create-heading:"))
			}
		}
	default:
		fmt.Printf("  %s %s\n", ui.Dim.Render("title:"), preview.Title)
		if preview.Parent != "" {
			fmt.Printf("  %s %s\n", ui.Dim.Render("parent:"), preview.Parent)
		}
		if preview.BootstrapParent != "" {
			fmt.Printf("  %s %s\n", ui.Dim.Render("bootstrap:"), preview.BootstrapParent)
		}
	}
	fmt.Println(ui.Separator.Render(strings.Repeat("─", 60)))
	fmt.Println(ui.RenderMarkdownOrPlain(preview.Content))
}

func parseTemplateInvokeArgs(tokens []string) (root string, parent string, targetNode string, vars map[string]string, args []string, dryRun bool, err error) {
	vars = make(map[string]string)
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if !strings.HasPrefix(t, "-") {
			args = append(args, t)
			continue
		}
		longFlag := strings.HasPrefix(t, "--")
		flagText := t
		if !longFlag {
			switch t {
			case "-a":
				flagText = "--at"
			case "-p":
				flagText = "--parent"
			case "-s":
				flagText = "--set"
			default:
				args = append(args, t)
				continue
			}
		}
		key, val, hasEq := strings.Cut(flagText[2:], "=")
		if key == "dry-run" {
			if hasEq {
				return "", "", "", nil, nil, false, fmt.Errorf("--dry-run does not take a value")
			}
			dryRun = true
			continue
		}
		if !hasEq {
			if i+1 >= len(tokens) || strings.HasPrefix(tokens[i+1], "--") {
				return "", "", "", nil, nil, false, fmt.Errorf("flag --%s requires a value", key)
			}
			val = tokens[i+1]
			i++
		}
		switch key {
		case "root":
			root = val
		case "parent":
			parent = val
		case "at":
			targetNode = val
		case "set":
			k, v, ok := strings.Cut(val, "=")
			if !ok {
				return "", "", "", nil, nil, false, fmt.Errorf("--set requires key=value")
			}
			vars[k] = v
		case "heading", "text", "title", "summary":
			vars[key] = val
		default:
			return "", "", "", nil, nil, false, fmt.Errorf("unknown flag %q", "--"+key)
		}
	}
	return root, parent, targetNode, vars, args, dryRun, nil
}
