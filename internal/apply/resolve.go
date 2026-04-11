package apply

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"sevens/internal/graph"
	"sevens/internal/store"
)

// EffectiveRequires returns the combined requires for a step: function-level + step-level.
// Function-level requires apply to all steps. Step-level requires can override or extend.
func EffectiveRequires(fn *Function, step *Step) []Require {
	seen := make(map[string]bool)
	var result []Require

	// Step-level takes precedence
	for _, r := range step.Requires {
		seen[r.Role] = true
		result = append(result, r)
	}

	// Function-level fills in what step didn't declare
	for _, r := range fn.Requires {
		if !seen[r.Role] {
			result = append(result, r)
		}
	}

	return result
}

// ResolveContext fetches all the nodes a function step needs from the graph.
// The walk provides the target node. Additional nodes are fetched based on Requires.
func ResolveContext(db *sql.DB, root string, fn *Function, step *Step, walk *graph.WalkOutput, targetBlock *graph.BlockTarget) (*ResolvedContext, error) {
	target := ResolvedNode{
		Title:   walk.Node.Title,
		Content: walk.Node.Content,
	}
	ctx := &ResolvedContext{
		Target:        target,
		NodeTitle:     walk.Node.Title,
		NodeContent:   walk.Node.Content,
		TargetKind:    "node",
		TargetLabel:   walk.Node.Title,
		ChildTitles:   walk.Node.Children,
		SiblingTitles: walk.Node.Siblings,
	}
	if targetBlock != nil {
		ctx.Target.Content = targetBlock.Markdown
		ctx.TargetKind = "block"
		ctx.TargetLabel = targetBlock.Label()
		ctx.Block = &ResolvedBlock{
			ID:        targetBlock.Subject,
			Path:      targetBlock.Path,
			Kind:      targetBlock.Kind,
			Text:      targetBlock.Text,
			Markdown:  targetBlock.Markdown,
			Signifier: targetBlock.Signifier,
			Scope:     append([]string(nil), targetBlock.Scope...),
		}
	}

	requires := EffectiveRequires(fn, step)

	for _, req := range requires {
		switch req.Role {
		case "target":
			// Already resolved from walk
			continue

		case "parent":
			if walk.Node.Parent == nil {
				if !req.Optional {
					fmt.Fprintf(os.Stderr, "[resolve] Node %q has no parent\n", walk.Node.Title)
				}
				continue
			}
			parentTitle := *walk.Node.Parent
			parentSubject, _ := store.ResolveNode(db, parentTitle, root)
			content, err := store.GetObject(db, parentSubject, "node/content")
			if err != nil {
				if !req.Optional {
					return nil, fmt.Errorf("resolving parent %q: %w", parentTitle, err)
				}
				fmt.Fprintf(os.Stderr, "[resolve] Could not fetch parent %q: %v\n", parentTitle, err)
				continue
			}
			ctx.Parent = &ResolvedNode{
				Title:   parentTitle,
				Content: content,
			}

		case "siblings":
			sibTitles, err := store.ComposeInverse(db, walk.Node.Subject, "node/parent", "node/parent")
			if err != nil {
				fmt.Fprintf(os.Stderr, "[resolve] Could not fetch siblings for %q: %v\n", walk.Node.Title, err)
				continue
			}
			var siblings []string
			for _, sibSubject := range sibTitles {
				sibTitle, err := store.NodeTitle(db, sibSubject)
				if err != nil || sibTitle == "" {
					continue
				}
				siblings = append(siblings, sibTitle)
			}
			sort.Strings(siblings)
			for _, sibTitle := range siblings {
				sibSubject, _ := store.ResolveNode(db, sibTitle, root)
				content, err := store.GetObject(db, sibSubject, "node/content")
				if err != nil {
					fmt.Fprintf(os.Stderr, "[resolve] Could not fetch sibling %q: %v\n", sibTitle, err)
					continue
				}
				role, _ := store.GetObject(db, sibSubject, "sibling/role")
				ctx.Siblings = append(ctx.Siblings, ResolvedNode{
					Title:   sibTitle,
					Content: content,
					Role:    role,
				})
			}

		case "children":
			childSubjects, err := store.GetSubjects(db, "node/parent", walk.Node.Subject)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[resolve] Could not fetch children for %q: %v\n", walk.Node.Title, err)
				continue
			}
			var childTitles []string
			for _, childSubject := range childSubjects {
				childTitle, err := store.NodeTitle(db, childSubject)
				if err != nil || childTitle == "" {
					continue
				}
				childTitles = append(childTitles, childTitle)
			}
			sort.Strings(childTitles)
			for _, childTitle := range childTitles {
				childSubject, _ := store.ResolveNode(db, childTitle, root)
				content, err := store.GetObject(db, childSubject, "node/content")
				if err != nil {
					fmt.Fprintf(os.Stderr, "[resolve] Could not fetch child %q: %v\n", childTitle, err)
					continue
				}
				role, _ := store.GetObject(db, childSubject, "sibling/role")
				ctx.Children = append(ctx.Children, ResolvedNode{
					Title:   childTitle,
					Content: content,
					Role:    role,
				})
			}

		case "history":
			entries, err := ReadLogDB(db, root, walk.Node.Title)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[resolve] Could not read history for %q: %v\n", walk.Node.Title, err)
				continue
			}
			ctx.History = entries
		}
	}

	// Cross-walk resolution: inject output from another function's last run on this node.
	if fn.CrossWalk != "" {
		entries, err := ReadLogDB(db, root, walk.Node.Title)
		if err == nil {
			// Check log entries (most recent first) for matching function output
			for i := len(entries) - 1; i >= 0; i-- {
				if entries[i].Function == fn.CrossWalk && entries[i].RawOutput != "" {
					ctx.CrossWalkOutput = entries[i].RawOutput
					break
				}
			}
		}
		// Also check pending suspensions for the cross-walk function
		if ctx.CrossWalkOutput == "" {
			rows, _ := db.Query(`
				SELECT DISTINCT t1.subject FROM triples t1
				JOIN triples t2 ON t1.subject = t2.subject
				WHERE t1.predicate = 'suspension/target' AND t1.object = ?
				AND t2.predicate = 'suspension/root' AND t2.object = ?
			`, walk.Node.Title, root)
			var subjects []string
			if rows != nil {
				for rows.Next() {
					var subj string
					if err := rows.Scan(&subj); err == nil {
						subjects = append(subjects, subj)
					}
				}
				rows.Close()
			}
			var latestTS string
			var latestOutput string
			for _, subj := range subjects {
				fn2, _ := store.GetObject(db, subj, "suspension/function")
				if fn2 != fn.CrossWalk {
					continue
				}
				ts, _ := store.GetObject(db, subj, "suspension/timestamp")
				rawOut, _ := store.GetObject(db, subj, "suspension/raw-output")
				if rawOut != "" && ts > latestTS {
					latestTS = ts
					latestOutput = rawOut
				}
			}
			ctx.CrossWalkOutput = latestOutput
		}
		if ctx.CrossWalkOutput == "" {
			fmt.Fprintf(os.Stderr, "[resolve] No prior %q output found for %q\n", fn.CrossWalk, walk.Node.Title)
		}
	}

	// Evaluate path specs if present
	if len(fn.Context) > 0 {
		pathResults, err := EvalPaths(db, walk.Node.Subject, fn.Context)
		if err != nil {
			return nil, fmt.Errorf("evaluating context paths: %w", err)
		}
		for name, result := range pathResults {
			switch name {
			case "parent":
				if len(result.Nodes) > 0 {
					ctx.Parent = &result.Nodes[0]
				}
			case "siblings":
				ctx.Siblings = append(ctx.Siblings, result.Nodes...)
			case "children":
				ctx.Children = append(ctx.Children, result.Nodes...)
			}
			// Other names become available through a generic mechanism (future)
		}
	}

	return ctx, nil
}

// FormatResolvedNodes renders a slice of ResolvedNodes as XML-tagged content blocks.
func FormatResolvedNodes(tag string, nodes []ResolvedNode) string {
	if len(nodes) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, n := range nodes {
		if n.Role != "" {
			sb.WriteString(fmt.Sprintf("<%s title=%q role=%q>\n%s\n</%s>\n\n", tag, n.Title, n.Role, n.Content, tag))
		} else {
			sb.WriteString(fmt.Sprintf("<%s title=%q>\n%s\n</%s>\n\n", tag, n.Title, n.Content, tag))
		}
	}
	return sb.String()
}

// FormatHistory renders log entries as a readable summary for injection into prompts.
func FormatHistory(entries []LogEntry) string {
	if len(entries) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<history>\n")
	for _, e := range entries {
		switch e.Event {
		case "completed", "suggested":
			summary := e.Summary
			if summary == "" && len(e.RawOutput) > 200 {
				summary = e.RawOutput[:200] + "..."
			} else if summary == "" {
				summary = e.RawOutput
			}
			sb.WriteString(fmt.Sprintf("[%s] %s/%s: %s\n", e.Timestamp, e.Function, e.Step, summary))
		case "applied":
			sb.WriteString(fmt.Sprintf("[%s] applied %s (commit %s)\n", e.Timestamp, e.Function, e.Commit))
		}
	}
	sb.WriteString("</history>")
	return sb.String()
}

// RenderWithContext substitutes all template variables using a ResolvedContext.
// This replaces RenderStepPrompt for functions that declare :requires.
func RenderWithContext(prompt string, ctx *ResolvedContext, contextFiles string) string {
	// Build children titles string (for backward compat with {{children}})
	childTitles := make([]string, len(ctx.Children))
	for i, c := range ctx.Children {
		childTitles[i] = c.Title
	}
	childrenStr := strings.Join(childTitles, ", ")
	if childrenStr == "" {
		// Fall back to ChildTitles if children weren't resolved with content
		childrenStr = strings.Join(ctx.ChildTitles, ", ")
	}
	if childrenStr == "" {
		childrenStr = "none"
	}

	parentStr := "none"
	parentContent := ""
	if ctx.Parent != nil {
		parentStr = ctx.Parent.Title
		parentContent = ctx.Parent.Content
	}

	siblingContent := FormatResolvedNodes("sibling", ctx.Siblings)
	childContent := FormatResolvedNodes("child-node", ctx.Children)
	historyStr := FormatHistory(ctx.History)
	targetKind := ctx.TargetKind
	if targetKind == "" {
		targetKind = "node"
	}
	targetLabel := ctx.TargetLabel
	if targetLabel == "" {
		targetLabel = ctx.Target.Title
	}
	blockID := ""
	blockPath := ""
	blockKind := ""
	blockText := ""
	blockMarkdown := ""
	blockSignifier := ""
	blockScope := ""
	if ctx.Block != nil {
		blockID = ctx.Block.ID
		blockPath = ctx.Block.Path
		blockKind = ctx.Block.Kind
		blockText = ctx.Block.Text
		blockMarkdown = ctx.Block.Markdown
		blockSignifier = ctx.Block.Signifier
		blockScope = strings.Join(ctx.Block.Scope, " > ")
	}

	replacements := map[string]string{
		"{{title}}":             ctx.Target.Title,
		"{{content}}":           ctx.Target.Content,
		"{{node-title}}":        ctx.NodeTitle,
		"{{node-content}}":      ctx.NodeContent,
		"{{target-kind}}":       targetKind,
		"{{target-label}}":      targetLabel,
		"{{block-id}}":          blockID,
		"{{block-path}}":        blockPath,
		"{{block-kind}}":        blockKind,
		"{{block-text}}":        blockText,
		"{{block-markdown}}":    blockMarkdown,
		"{{block-signifier}}":   blockSignifier,
		"{{block-scope}}":       blockScope,
		"{{parent}}":            parentStr,
		"{{parent-content}}":    parentContent,
		"{{children}}":          childrenStr,
		"{{children-content}}":  childContent,
		"{{siblings}}":          siblingContent,
		"{{siblings-content}}":  siblingContent,
		"{{history}}":           historyStr,
		"{{prev}}":              ctx.Prev,
		"{{context}}":           contextFiles,
		"{{cross-walk-output}}": ctx.CrossWalkOutput,
		"{{instruction}}":       ctx.Instruction,
		"{{timestamp}}":         time.Now().Format("2006-01-02 15:04"),
	}

	result := prompt
	for k, v := range replacements {
		result = strings.ReplaceAll(result, k, v)
	}
	return result
}

// HasRequires returns true if the function declares any requires or context paths (function-level or step-level).
func HasRequires(fn *Function) bool {
	if len(fn.Requires) > 0 || len(fn.Context) > 0 || fn.CrossWalk != "" {
		return true
	}
	for _, s := range fn.Steps {
		if len(s.Requires) > 0 {
			return true
		}
	}
	return false
}
