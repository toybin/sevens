package function

import (
	"context"
	"fmt"
	"strings"
	"time"

	"sevens/internal/graphops"
	"sevens/internal/kb"
)

// ResolvedContext holds all gathered graph context for rendering a step's prompt.
type ResolvedContext struct {
	Target      *kb.WalkContext
	Roles       map[string]*kb.WalkContext // keyed by Require.As or Require.Role
	Paths       map[string][]string        // keyed by PathSpec.As -> resolved titles
	PrevStep    string                     // output from the prior step
	Instruction string                     // ad-hoc instruction from user
	Block       *BlockContext              // optional block target
	History     string                     // formatted history
}

// BlockContext holds resolved data for a block-targeted step.
type BlockContext struct {
	Path     string
	Kind     string
	Text     string
	Markdown string
	Scope    string
}

// ResolveContext gathers graph context for a step based on its Requires and Paths.
func ResolveContext(ctx context.Context, k *kb.KB, root, target string, step Step, prevOutput string) (*ResolvedContext, error) {
	rc := &ResolvedContext{
		Roles: make(map[string]*kb.WalkContext),
		Paths: make(map[string][]string),
		PrevStep: prevOutput,
	}

	// Walk the target node
	walk, err := k.Walk(ctx, root, target)
	if err != nil {
		return nil, fmt.Errorf("walking target %q: %w", target, err)
	}
	rc.Target = walk

	// Resolve requires
	for _, req := range step.Requires {
		key := req.As
		if key == "" {
			key = req.Role
		}

		switch req.Role {
		case "target":
			rc.Roles[key] = walk
		case "parent":
			if walk.Parent != nil {
				pw, err := k.Walk(ctx, root, *walk.Parent)
				if err != nil && !req.Optional {
					return nil, fmt.Errorf("resolving parent: %w", err)
				}
				if pw != nil {
					rc.Roles[key] = pw
				}
			} else if !req.Optional {
				return nil, fmt.Errorf("node %q has no parent", target)
			}
		case "children":
			// Store children as titles in Paths
			rc.Paths[key] = walk.Children
		case "siblings":
			rc.Paths[key] = walk.Siblings
		default:
			if !req.Optional {
				return nil, fmt.Errorf("unknown require role %q", req.Role)
			}
		}
	}

	// Resolve path specs via graphops.Compose
	for _, ps := range step.Paths {
		if ps.As == "" {
			continue
		}
		subject := kb.NodeSubject(root, target)
		path := graphops.ParsePath(ps.Path)
		terminals, err := k.Graph().Compose(ctx, subject, path)
		if err != nil {
			continue // best-effort for path specs
		}
		if ps.ExcludeSelf {
			terminals = excludeString(terminals, subject)
		}
		// Resolve terminal subjects to titles
		var titles []string
		for _, subj := range terminals {
			if t, _, _ := k.Graph().Lookup(ctx, subj, kb.PredNodeTitle); t != "" {
				titles = append(titles, t)
			}
		}
		rc.Paths[ps.As] = titles
	}

	return rc, nil
}

// RenderPrompt substitutes context into a prompt template.
// Supported placeholders: {{title}}, {{content}}, {{parent}}, {{children}}, {{siblings}}, {{prev}}.
func RenderPrompt(template string, rc *ResolvedContext) string {
	result := template

	if rc.Target != nil {
		result = strings.ReplaceAll(result, "{{title}}", rc.Target.Title)
		result = strings.ReplaceAll(result, "{{content}}", rc.Target.Content)
		if rc.Target.Parent != nil {
			result = strings.ReplaceAll(result, "{{parent}}", *rc.Target.Parent)
		} else {
			result = strings.ReplaceAll(result, "{{parent}}", "")
		}
		result = strings.ReplaceAll(result, "{{children}}", strings.Join(rc.Target.Children, ", "))
		result = strings.ReplaceAll(result, "{{siblings}}", strings.Join(rc.Target.Siblings, ", "))
	}

	result = strings.ReplaceAll(result, "{{prev}}", rc.PrevStep)
	result = strings.ReplaceAll(result, "{{timestamp}}", time.Now().UTC().Format("2006-01-02T15:04:05Z"))
	result = strings.ReplaceAll(result, "{{instruction}}", rc.Instruction)

	if rc.Block != nil {
		result = strings.ReplaceAll(result, "{{block.path}}", rc.Block.Path)
		result = strings.ReplaceAll(result, "{{block.kind}}", rc.Block.Kind)
		result = strings.ReplaceAll(result, "{{block.text}}", rc.Block.Text)
		result = strings.ReplaceAll(result, "{{block.markdown}}", rc.Block.Markdown)
		result = strings.ReplaceAll(result, "{{block.scope}}", rc.Block.Scope)
	}
	result = strings.ReplaceAll(result, "{{history}}", rc.History)

	// Resolve role-based placeholders: {{role.title}}, {{role.content}}
	for key, walk := range rc.Roles {
		if walk == nil {
			continue
		}
		result = strings.ReplaceAll(result, "{{"+key+".title}}", walk.Title)
		result = strings.ReplaceAll(result, "{{"+key+".content}}", walk.Content)
	}

	// Resolve path-based placeholders: {{pathAs}}
	for key, titles := range rc.Paths {
		result = strings.ReplaceAll(result, "{{"+key+"}}", strings.Join(titles, ", "))
	}

	return result
}

func excludeString(ss []string, exclude string) []string {
	var result []string
	for _, s := range ss {
		if s != exclude {
			result = append(result, s)
		}
	}
	return result
}
