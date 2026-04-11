package apply

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"olympos.io/encoding/edn"
	"sevens/defaults"
	"sevens/internal/graph"
	"sevens/internal/store"
)

var templateVarRe = regexp.MustCompile(`\{\{([\w-]+)\}\}`)

// LoadTemplate loads a template from ~/.config/sevens/templates with bundled fallback.
func LoadTemplate(name string) (*NodeTemplate, error) {
	dir, err := store.ConfigDir()
	if err != nil {
		return nil, err
	}
	tmplDir := filepath.Join(dir, "templates")
	path := filepath.Join(tmplDir, name+".edn")
	data, err := readTemplateAsset(path, name+".edn")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("template %q not found", name)
		}
		return nil, fmt.Errorf("reading template %q: %w", name, err)
	}

	var tmpl NodeTemplate
	if err := edn.Unmarshal(data, &tmpl); err != nil {
		return nil, fmt.Errorf("parsing template %s: %w", name, err)
	}
	if tmpl.Name == "" {
		return nil, fmt.Errorf("template %s: name must be non-empty", name)
	}
	if tmpl.Mode == "" {
		tmpl.Mode = "create-node"
	}
	if tmpl.Content == "" {
		mdPath := filepath.Join(tmplDir, name+".md")
		md, err := readTemplateAsset(mdPath, name+".md")
		if err == nil {
			tmpl.Content = string(md)
		}
	}
	return &tmpl, nil
}

func readTemplateAsset(userPath, bundledName string) ([]byte, error) {
	data, err := os.ReadFile(userPath)
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	data, err = defaults.ReadTemplateFile(bundledName)
	if err == nil {
		return data, nil
	}
	return nil, fmt.Errorf("open %s: %w", bundledName, err)
}

// ListTemplates returns all available template names.
func ListTemplates() ([]string, error) {
	dir, err := store.ConfigDir()
	if err != nil {
		return nil, err
	}
	tmplDir := filepath.Join(dir, "templates")
	entries, err := os.ReadDir(tmplDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	nameSet := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".edn" {
			continue
		}
		nameSet[strings.TrimSuffix(e.Name(), ".edn")] = true
	}
	if bundled, err := defaults.ListTemplateNames(); err == nil {
		for _, name := range bundled {
			nameSet[name] = true
		}
	}
	var names []string
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

// ResolveTemplateVars applies builtin defaults and template param defaults.
func ResolveTemplateVars(tmpl *NodeTemplate, vars map[string]string) map[string]string {
	resolved := make(map[string]string, len(vars)+8)
	for k, v := range vars {
		resolved[k] = v
	}

	now := time.Now()
	setDefault := func(key, value string) {
		if _, ok := resolved[key]; !ok {
			resolved[key] = value
		}
	}
	setDefault("date", now.Format("2006-01-02"))
	setDefault("today", now.Format("2006-01-02"))
	setDefault("time", now.Format("15:04"))
	setDefault("timestamp", now.Format("2006-01-02 15:04"))
	setDefault("template-name", tmpl.Name)

	for _, param := range tmpl.Params {
		if _, ok := resolved[param.Name]; ok {
			continue
		}
		if strings.TrimSpace(param.Default) == "" {
			continue
		}
		resolved[param.Name] = substituteVars(param.Default, resolved)
	}

	return resolved
}

// MissingTemplateVars returns the required template params still missing after defaults.
func MissingTemplateVars(tmpl *NodeTemplate, vars map[string]string) []string {
	required := ExtractVariables(tmpl)
	var missing []string
	for _, v := range required {
		if strings.TrimSpace(vars[v]) == "" {
			missing = append(missing, v)
		}
	}
	sort.Strings(missing)
	return missing
}

// RenderTemplate substitutes variables in a template's patterns.
func RenderTemplate(tmpl *NodeTemplate, vars map[string]string) *NodeTemplate {
	vars = ResolveTemplateVars(tmpl, vars)

	result := *tmpl
	result.TitlePattern = substituteVars(tmpl.TitlePattern, vars)
	result.DraftTitlePattern = substituteVars(tmpl.DraftTitlePattern, vars)
	result.ParentPattern = substituteVars(tmpl.ParentPattern, vars)
	result.Content = substituteVars(tmpl.Content, vars)
	result.CommitMessage = substituteVars(tmpl.CommitMessage, vars)
	if tmpl.Target != nil {
		target := *tmpl.Target
		target.Root = substituteVars(target.Root, vars)
		target.Parent = substituteVars(target.Parent, vars)
		target.Node = substituteVars(target.Node, vars)
		result.Target = &target
	}
	if tmpl.Placement != nil {
		placement := *tmpl.Placement
		placement.Kind = substituteVars(placement.Kind, vars)
		placement.Heading = substituteVars(placement.Heading, vars)
		placement.HeadingLevel = tmpl.Placement.HeadingLevel
		placement.CreateIfMissing = tmpl.Placement.CreateIfMissing
		result.Placement = &placement
	}
	if len(tmpl.Params) > 0 {
		result.Params = append([]TemplateParam(nil), tmpl.Params...)
	}

	if len(tmpl.Children) > 0 {
		result.Children = make([]NodeTemplate, len(tmpl.Children))
		for i, child := range tmpl.Children {
			rendered := RenderTemplate(&child, vars)
			result.Children[i] = *rendered
		}
	}

	return &result
}

func CleanRenderedTemplate(tmpl *NodeTemplate) *NodeTemplate {
	result := *tmpl
	result.TitlePattern = stripUnresolvedVars(tmpl.TitlePattern)
	result.DraftTitlePattern = stripUnresolvedVars(tmpl.DraftTitlePattern)
	result.ParentPattern = stripUnresolvedVars(tmpl.ParentPattern)
	result.Content = stripUnresolvedVars(tmpl.Content)
	result.CommitMessage = stripUnresolvedVars(tmpl.CommitMessage)
	if tmpl.Target != nil {
		target := *tmpl.Target
		target.Root = stripUnresolvedVars(target.Root)
		target.Parent = stripUnresolvedVars(target.Parent)
		target.Node = stripUnresolvedVars(target.Node)
		result.Target = &target
	}
	if tmpl.Placement != nil {
		placement := *tmpl.Placement
		placement.Kind = stripUnresolvedVars(placement.Kind)
		placement.Heading = stripUnresolvedVars(placement.Heading)
		placement.HeadingLevel = tmpl.Placement.HeadingLevel
		placement.CreateIfMissing = tmpl.Placement.CreateIfMissing
		result.Placement = &placement
	}
	if len(tmpl.Params) > 0 {
		result.Params = append([]TemplateParam(nil), tmpl.Params...)
	}
	if tmpl.Draft != nil {
		draft := *tmpl.Draft
		result.Draft = &draft
	}
	if len(tmpl.Children) > 0 {
		result.Children = make([]NodeTemplate, len(tmpl.Children))
		for i, child := range tmpl.Children {
			result.Children[i] = *CleanRenderedTemplate(&child)
		}
	}
	return &result
}

// DraftTitle returns a usable title when a template is being instantiated as a scaffold.
func DraftTitle(tmpl *NodeTemplate) string {
	title := strings.TrimSpace(tmpl.DraftTitlePattern)
	if title == "" {
		title = strings.TrimSpace(tmpl.TitlePattern)
	}
	if title == "" {
		title = tmpl.Name
	}
	if strings.Contains(title, "{{") {
		title = strings.TrimSpace(templateVarRe.ReplaceAllString(title, "draft"))
	}
	if title == "" {
		title = "Draft " + time.Now().Format("2006-01-02 15:04")
	}
	return title
}

// ExtractVariables returns required user-provided vars, excluding builtins.
func ExtractVariables(tmpl *NodeTemplate) []string {
	builtins := map[string]bool{
		"date": true, "today": true, "time": true, "timestamp": true, "template-name": true,
	}
	seen := map[string]bool{}

	if len(tmpl.Params) > 0 {
		for _, param := range tmpl.Params {
			if param.Required && !builtins[param.Name] {
				seen[param.Name] = true
			}
		}
	} else {
		for _, s := range []string{tmpl.TitlePattern, tmpl.DraftTitlePattern, tmpl.ParentPattern, tmpl.Content, tmpl.CommitMessage} {
			for _, match := range templateVarRe.FindAllStringSubmatch(s, -1) {
				name := match[1]
				if !builtins[name] {
					seen[name] = true
				}
			}
		}
	}

	for _, child := range tmpl.Children {
		for _, v := range ExtractVariables(&child) {
			seen[v] = true
		}
	}

	var vars []string
	for v := range seen {
		vars = append(vars, v)
	}
	sort.Strings(vars)
	return vars
}

func substituteVars(s string, vars map[string]string) string {
	for k, v := range vars {
		s = strings.ReplaceAll(s, "{{"+k+"}}", v)
	}
	return s
}

func stripUnresolvedVars(s string) string {
	return templateVarRe.ReplaceAllString(s, "")
}

// InstantiateTemplate creates the node files for a rendered template.
func InstantiateTemplate(tmpl *NodeTemplate, parent string, root string) []FileOp {
	var ops []FileOp

	title := strings.TrimSpace(tmpl.TitlePattern)
	if title == "" {
		title = tmpl.Name
	}

	content := strings.TrimSpace(tmpl.Content)
	if content == "" {
		content = "# " + title + "\n"
	} else if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}

	op := FileOp{
		Action:  "create",
		Title:   title,
		Parent:  parent,
		Content: content,
	}
	if tmpl.SiblingRole != "" {
		op.ExtraFrontmatter = map[string]string{"sibling-role": tmpl.SiblingRole}
	}
	ops = append(ops, op)

	for _, child := range tmpl.Children {
		childOps := InstantiateTemplate(&child, title, root)
		ops = append(ops, childOps...)
	}

	return ops
}

type TemplateExecutionOptions struct {
	Parent     string
	TargetNode string
	Vars       map[string]string
}

type TemplateExecutionResult struct {
	TemplateName    string
	Mode            string
	TargetNode      string
	EffectiveParent string
	PrimaryTitle    string
	Created         []string
	Edited          []string
	Missing         []string
	Draft           bool
	CommitMessage   string
}

type TemplatePreview struct {
	TemplateName    string
	Mode            string
	Title           string
	Parent          string
	TargetNode      string
	Heading         string
	HeadingLevel    int
	CreateIfMissing bool
	BootstrapParent string
	Missing         []string
	Draft           bool
	Content         string
}

// BindTemplateArgs maps positional CLI args onto template params in order,
// skipping params already provided explicitly in vars.
func BindTemplateArgs(tmpl *NodeTemplate, args []string, vars map[string]string) map[string]string {
	bound := make(map[string]string, len(vars)+len(args))
	for k, v := range vars {
		bound[k] = v
	}
	if len(args) == 0 {
		return bound
	}

	argIndex := 0
	if len(tmpl.Params) > 0 {
		for _, param := range tmpl.Params {
			if _, ok := bound[param.Name]; ok {
				continue
			}
			if argIndex >= len(args) {
				break
			}
			bound[param.Name] = args[argIndex]
			argIndex++
		}
	}

	// Backward-compatible fallback for simple one-arg templates.
	if argIndex < len(args) && len(args) == 1 {
		if _, ok := bound["title"]; !ok {
			bound["title"] = args[0]
			argIndex = len(args)
		} else if _, ok := bound["topic"]; !ok {
			bound["topic"] = args[0]
			argIndex = len(args)
		}
	}
	return bound
}

func PreviewTemplate(db *sql.DB, root string, tmpl *NodeTemplate, opts TemplateExecutionOptions) (*TemplatePreview, error) {
	vars := make(map[string]string, len(opts.Vars))
	for k, v := range opts.Vars {
		vars[k] = v
	}

	rendered := RenderTemplate(tmpl, vars)
	missing := MissingTemplateVars(tmpl, vars)
	isDraft := len(missing) > 0 && tmpl.Draft != nil && tmpl.Draft.WhenMissingParams
	if len(missing) > 0 && !isDraft {
		return nil, fmt.Errorf("template %q requires variables: %s\nUse --set key=value to provide them", tmpl.Name, strings.Join(missing, ", "))
	}
	if isDraft {
		rendered.TitlePattern = DraftTitle(rendered)
	} else {
		rendered = CleanRenderedTemplate(rendered)
	}

	mode := rendered.Mode
	if mode == "" {
		mode = "create-node"
	}
	preview := &TemplatePreview{
		TemplateName: tmpl.Name,
		Mode:         mode,
		Missing:      append([]string(nil), missing...),
		Draft:        isDraft,
		Content:      rendered.Content,
	}

	switch mode {
	case "append-node", "insert-block":
		targetNode := strings.TrimSpace(opts.TargetNode)
		if targetNode == "" && rendered.Target != nil {
			targetNode = strings.TrimSpace(rendered.Target.Node)
		}
		if targetNode == "" {
			targetNode = strings.TrimSpace(opts.Parent)
		}
		if targetNode == "" {
			return nil, fmt.Errorf("template %q requires a target node", tmpl.Name)
		}
		if canonical := store.ResolveTitle(db, targetNode, root); canonical != "" {
			targetNode = canonical
		}
		preview.TargetNode = targetNode
		if rendered.Placement != nil {
			preview.Heading = rendered.Placement.Heading
			preview.HeadingLevel = rendered.Placement.HeadingLevel
			preview.CreateIfMissing = rendered.Placement.CreateIfMissing
		}
	default:
		parent := strings.TrimSpace(opts.Parent)
		if parent == "" {
			switch {
			case rendered.Target != nil && rendered.Target.Parent != "":
				parent = rendered.Target.Parent
			case rendered.ParentPattern != "":
				parent = rendered.ParentPattern
			}
		}
		if canonical := store.ResolveTitle(db, parent, root); canonical != "" {
			parent = canonical
		}
		preview.Parent = parent
		preview.Title = strings.TrimSpace(rendered.TitlePattern)
		if parent != "" && rendered.ParentTemplate != "" {
			parentSubject, _ := store.ResolveNode(db, parent, root)
			if parentSubject == "" {
				preview.BootstrapParent = rendered.ParentTemplate
			}
		}
	}

	return preview, nil
}

// ExecuteTemplate deterministically instantiates or inserts a rendered template.
func ExecuteTemplate(db *sql.DB, root string, tmpl *NodeTemplate, opts TemplateExecutionOptions) (*TemplateExecutionResult, error) {
	vars := make(map[string]string, len(opts.Vars))
	for k, v := range opts.Vars {
		vars[k] = v
	}

	rendered := RenderTemplate(tmpl, vars)
	missing := MissingTemplateVars(tmpl, vars)
	isDraft := len(missing) > 0 && tmpl.Draft != nil && tmpl.Draft.WhenMissingParams
	if len(missing) > 0 && !isDraft {
		return nil, fmt.Errorf("template %q requires variables: %s\nUse --set key=value to provide them", tmpl.Name, strings.Join(missing, ", "))
	}

	if isDraft {
		rendered.TitlePattern = DraftTitle(rendered)
	} else {
		rendered = CleanRenderedTemplate(rendered)
	}

	mode := rendered.Mode
	if mode == "" {
		mode = "create-node"
	}
	result := &TemplateExecutionResult{
		TemplateName: tmpl.Name,
		Mode:         mode,
		Missing:      append([]string(nil), missing...),
		Draft:        isDraft,
	}

	switch mode {
	case "append-node", "insert-block":
		targetNode := strings.TrimSpace(opts.TargetNode)
		if targetNode == "" && rendered.Target != nil {
			targetNode = strings.TrimSpace(rendered.Target.Node)
		}
		if targetNode == "" {
			targetNode = strings.TrimSpace(opts.Parent)
		}
		if targetNode == "" {
			return nil, fmt.Errorf("template %q requires a target node", tmpl.Name)
		}
		canonical := store.ResolveTitle(db, targetNode, root)
		if canonical == "" {
			return nil, fmt.Errorf("target node not found: %q", targetNode)
		}
		result.TargetNode = canonical

		var (
			edit graph.NodeEdit
			err  error
		)
		switch mode {
		case "append-node":
			edit, err = graph.PrepareAppendToNode(db, root, canonical, rendered.Content)
		case "insert-block":
			heading := ""
			headingLevel := 0
			createIfMissing := false
			if rendered.Placement != nil {
				heading = rendered.Placement.Heading
				headingLevel = rendered.Placement.HeadingLevel
				createIfMissing = rendered.Placement.CreateIfMissing
			}
			if strings.TrimSpace(heading) == "" {
				return nil, fmt.Errorf("template %q insert-block requires :placement {:heading ...}", tmpl.Name)
			}
			edit, err = graph.PrepareInsertUnderHeading(db, root, canonical, heading, headingLevel, createIfMissing, rendered.Content)
		}
		if err != nil {
			return nil, err
		}
		_, edited, err := ExecuteOps([]FileOp{{
			Action:  "edit",
			File:    edit.NodeTitle,
			OldText: edit.OldText,
			NewText: edit.NewText,
		}}, root, db)
		if err != nil {
			return nil, fmt.Errorf("editing from template: %w", err)
		}
		result.Edited = edited

	default:
		effectiveParent := strings.TrimSpace(opts.Parent)
		if effectiveParent == "" {
			switch {
			case rendered.Target != nil && rendered.Target.Parent != "":
				effectiveParent = rendered.Target.Parent
			case rendered.ParentPattern != "":
				effectiveParent = rendered.ParentPattern
			}
		}
		result.EffectiveParent = effectiveParent
		result.PrimaryTitle = strings.TrimSpace(rendered.TitlePattern)

		if effectiveParent != "" && rendered.ParentTemplate != "" {
			parentSubject, _ := store.ResolveNode(db, effectiveParent, root)
			if parentSubject == "" {
				parentTmpl, err := LoadTemplate(rendered.ParentTemplate)
				if err != nil {
					return nil, fmt.Errorf("loading parent template: %w", err)
				}
				parentResult, err := ExecuteTemplate(db, root, parentTmpl, TemplateExecutionOptions{Vars: vars})
				if err != nil {
					return nil, fmt.Errorf("creating parent from template: %w", err)
				}
				result.Created = append(result.Created, parentResult.Created...)
				result.Edited = append(result.Edited, parentResult.Edited...)
			}
		}

		ops := InstantiateTemplate(rendered, effectiveParent, root)
		created, edited, err := ExecuteOps(ops, root, db)
		if err != nil {
			return nil, fmt.Errorf("creating from template: %w", err)
		}
		result.Created = append(result.Created, created...)
		result.Edited = append(result.Edited, edited...)

		roleTriples := SiblingRoleTriples(rendered)
		if len(roleTriples) > 0 {
			if err := store.InsertTriples(db, roleTriples); err != nil {
				return nil, fmt.Errorf("writing sibling roles: %w", err)
			}
		}
	}

	commitMsg := strings.TrimSpace(rendered.CommitMessage)
	if commitMsg == "" {
		switch mode {
		case "append-node", "insert-block":
			commitMsg = fmt.Sprintf("sevens: instantiate template %s", tmpl.Name)
		default:
			commitMsg = fmt.Sprintf("sevens: new from template %s", tmpl.Name)
		}
	}
	result.CommitMessage = commitMsg
	return result, nil
}

// SiblingRoleTriples generates triples for sibling roles defined in a template.
func SiblingRoleTriples(tmpl *NodeTemplate) []store.Triple {
	var triples []store.Triple
	for _, child := range tmpl.Children {
		if child.SiblingRole != "" {
			title := child.TitlePattern
			triples = append(triples, store.Triple{
				Subject:   title,
				Predicate: "sibling/role",
				Object:    child.SiblingRole,
			})
		}
		triples = append(triples, SiblingRoleTriples(&child)...)
	}
	return triples
}
