package apply

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/samber/lo"
	"olympos.io/encoding/edn"
	"sevens/defaults"
	"sevens/internal/store"
)

func LoadFunction(name string) (*Function, error) {
	dir, err := store.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("get config dir: %w", err)
	}
	fnDir := filepath.Join(dir, "functions")
	path := filepath.Join(fnDir, name+".edn")
	data, err := readFunctionAsset(path, name+".edn")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("function %q not found — run 'sevens functions' to list available functions", name)
		}
		return nil, fmt.Errorf("reading function %q: %w", name, err)
	}
	var fn Function
	if err := edn.Unmarshal(data, &fn); err != nil {
		return nil, fmt.Errorf("parse function %s: %w", name, err)
	}
	if fn.Name == "" {
		return nil, fmt.Errorf("function %s: name must be non-empty", name)
	}

	// Load prompt templates from .md files where inline prompt is empty.
	if fn.Prompt == "" && len(fn.Steps) == 0 {
		// Try loading <name>.md as the single-step prompt.
		mdPath := filepath.Join(fnDir, name+".md")
		if md, err := readFunctionAsset(mdPath, name+".md"); err == nil {
			fn.Prompt = string(md)
		} else {
			return nil, fmt.Errorf("function %s: must have prompt (inline or %s.md) or steps", name, name)
		}
	}

	for i := range fn.Steps {
		if fn.Steps[i].Prompt == "" && fn.Steps[i].Fn == "" {
			// Load <name>.<step>.md (skip for delegating steps that use :fn)
			stepName := fn.Steps[i].Name
			mdPath := filepath.Join(fnDir, name+"."+stepName+".md")
			md, err := readFunctionAsset(mdPath, name+"."+stepName+".md")
			if err != nil {
				return nil, fmt.Errorf("function %s step %q: no inline prompt and %s not found", name, stepName, mdPath)
			}
			fn.Steps[i].Prompt = string(md)
		}
	}

	if err := fn.ValidateComposition(); err != nil {
		return nil, fmt.Errorf("function %s: %w", name, err)
	}

	return &fn, nil
}

func ListFunctions() ([]Function, error) {
	dir, err := store.ConfigDir()
	if err != nil {
		return nil, fmt.Errorf("get config dir: %w", err)
	}
	fnDir := filepath.Join(dir, "functions")
	entries, err := os.ReadDir(fnDir)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read functions dir %s: %w", fnDir, err)
	}
	nameSet := make(map[string]bool)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".edn" {
			continue
		}
		nameSet[strings.TrimSuffix(e.Name(), ".edn")] = true
	}
	if bundled, err := defaults.ListFunctionNames(); err == nil {
		for _, name := range bundled {
			nameSet[name] = true
		}
	}
	names := lo.Keys(nameSet)
	sort.Strings(names)
	fns := lo.FilterMap(names, func(name string, _ int) (Function, bool) {
		fn, err := LoadFunction(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			return Function{}, false
		}
		return *fn, true
	})
	return fns, nil
}

func readFunctionAsset(userPath, bundledName string) ([]byte, error) {
	data, err := os.ReadFile(userPath)
	if err == nil {
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	data, err = defaults.ReadFunctionFile(bundledName)
	if err == nil {
		return data, nil
	}
	return nil, fmt.Errorf("open %s: %w", bundledName, err)
}

// LoadContextFiles reads and concatenates the contents of context files.
// Paths are resolved relative to root. Tilde expansion is applied.
func LoadContextFiles(root string, paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, p := range paths {
		resolved := p
		if strings.HasPrefix(p, "~/") {
			home, err := os.UserHomeDir()
			if err == nil {
				resolved = filepath.Join(home, p[2:])
			}
		} else if !filepath.IsAbs(p) {
			resolved = filepath.Join(root, p)
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[warn] context file %s: %v\n", p, err)
			continue
		}
		sb.WriteString("<context-file path=\"")
		sb.WriteString(p)
		sb.WriteString("\">\n")
		sb.WriteString(strings.TrimSpace(string(data)))
		sb.WriteString("\n</context-file>\n\n")
	}
	return sb.String()
}

// RenderStepPrompt substitutes template variables into a step's prompt.
func RenderStepPrompt(prompt, title, content, parent string, children []string, prev, context string) string {
	return RenderStepPromptWithVars(prompt, PromptVars{
		Title:       title,
		Content:     content,
		NodeTitle:   title,
		NodeContent: content,
		Parent:      parent,
		Children:    children,
		Prev:        prev,
		Context:     context,
		TargetKind:  "node",
		TargetLabel: title,
	})
}

type PromptVars struct {
	Title          string
	Content        string
	NodeTitle      string
	NodeContent    string
	Parent         string
	Children       []string
	Prev           string
	Context        string
	TargetKind     string
	TargetLabel    string
	BlockID        string
	BlockPath      string
	BlockKind      string
	BlockText      string
	BlockMarkdown  string
	BlockSignifier string
	BlockScope     string
}

func RenderStepPromptWithVars(prompt string, vars PromptVars) string {
	if vars.NodeTitle == "" {
		vars.NodeTitle = vars.Title
	}
	if vars.NodeContent == "" {
		vars.NodeContent = vars.Content
	}
	if vars.TargetKind == "" {
		vars.TargetKind = "node"
	}
	if vars.TargetLabel == "" {
		vars.TargetLabel = vars.NodeTitle
	}
	children := vars.Children
	ch := strings.Join(children, ", ")
	if ch == "" {
		ch = "none"
	}
	par := vars.Parent
	if par == "" {
		par = "none"
	}
	replacements := [][2]string{
		{"{{title}}", vars.Title},
		{"{{content}}", vars.Content},
		{"{{node-title}}", vars.NodeTitle},
		{"{{node-content}}", vars.NodeContent},
		{"{{target-kind}}", vars.TargetKind},
		{"{{target-label}}", vars.TargetLabel},
		{"{{block-id}}", vars.BlockID},
		{"{{block-path}}", vars.BlockPath},
		{"{{block-kind}}", vars.BlockKind},
		{"{{block-text}}", vars.BlockText},
		{"{{block-markdown}}", vars.BlockMarkdown},
		{"{{block-signifier}}", vars.BlockSignifier},
		{"{{block-scope}}", vars.BlockScope},
		{"{{parent}}", par},
		{"{{children}}", ch},
		{"{{prev}}", vars.Prev},
		{"{{context}}", vars.Context},
		{"{{timestamp}}", time.Now().Format("2006-01-02 15:04")},
	}
	return lo.Reduce(replacements, func(s string, pair [2]string, _ int) string {
		return strings.ReplaceAll(s, pair[0], pair[1])
	}, prompt)
}

// RenderPrompt is a convenience wrapper for single-step functions.
func RenderPrompt(fn *Function, title, content, parent string, children []string) string {
	return RenderStepPrompt(fn.Prompt, title, content, parent, children, "", "")
}

func ParseOps(llmOutput string) ([]FileOp, error) {
	s := strings.TrimSpace(llmOutput)

	if strings.HasPrefix(s, "```") {
		lines := strings.Split(s, "\n")
		start := 1
		end := len(lines) - 1
		for end > start && strings.TrimSpace(lines[end]) != "```" {
			end--
		}
		s = strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	}

	if !strings.HasSuffix(s, "]") {
		lastBrace := strings.LastIndex(s, "}")
		if lastBrace > 0 {
			s = s[:lastBrace+1] + "\n]"
			fmt.Fprintln(os.Stderr, "[warn] LLM response was truncated, recovering partial ops")
		}
	}

	var ops []FileOp
	if err := json.Unmarshal([]byte(s), &ops); err != nil {
		return nil, fmt.Errorf("unmarshal ops: %w", err)
	}

	for i := range ops {
		ops[i].Parent = stripBrackets(ops[i].Parent)
		ops[i].File = stripBrackets(ops[i].File)
		ops[i].Title = stripBrackets(ops[i].Title)

		switch ops[i].Action {
		case "create":
			if ops[i].Title == "" {
				return nil, fmt.Errorf("op %d (create): title must be non-empty", i)
			}
			if ops[i].Content == "" {
				return nil, fmt.Errorf("op %d (create): content must be non-empty", i)
			}
		case "edit":
			if ops[i].File == "" {
				return nil, fmt.Errorf("op %d (edit): file must be non-empty", i)
			}
			if ops[i].OldText == "" {
				return nil, fmt.Errorf("op %d (edit): old_text must be non-empty", i)
			}
			if ops[i].NewText == "" {
				return nil, fmt.Errorf("op %d (edit): new_text must be non-empty", i)
			}
		default:
			return nil, fmt.Errorf("op %d: unknown action %q", i, ops[i].Action)
		}
	}
	return ops, nil
}

func stripBrackets(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[[")
	s = strings.TrimSuffix(s, "]]")
	return s
}

func SanitizeFilename(title string) string {
	s := strings.ToLower(title)
	s = strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return -1 // drop unsafe chars
		default:
			return r
		}
	}, s)
	s = strings.TrimSpace(s)
	return s + ".md"
}
