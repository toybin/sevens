package function

import (
	"context"
	"fmt"
	"strings"
	"time"

	"sevens/internal/graphops"
	"sevens/internal/kb"
)

// ResolvedNode is a node resolved by a path spec, with optional extra predicates.
type ResolvedNode struct {
	Title   string
	Content string // node/content if fetched via With
}

// ResolvedContext holds all gathered graph context for rendering a step's prompt.
type ResolvedContext struct {
	Target      *kb.WalkResult
	Roles       map[string]*kb.WalkResult     // keyed by Require.As or Require.Role
	Paths       map[string][]string           // keyed by PathSpec.As -> resolved titles
	PathNodes   map[string][]ResolvedNode     // keyed by PathSpec.As -> full resolved nodes
	PrevStep    string                        // output from the prior step
	Instruction string                        // ad-hoc instruction from user
	Block       *BlockContext                 // optional block target
	History     string                        // formatted history
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
		Roles:     make(map[string]*kb.WalkResult),
		Paths:     make(map[string][]string),
		PathNodes: make(map[string][]ResolvedNode),
		PrevStep:  prevOutput,
	}

	// Walk the target node with full neighborhood context
	walk, err := k.Walk(ctx, root, target, kb.GatherNeighborhood)
	if err != nil {
		return nil, fmt.Errorf("walking target %q: %w", target, err)
	}
	rc.Target = walk

	// Helper to extract child titles from WalkNode slice
	childTitles := func(nodes []kb.WalkNode) []string {
		var titles []string
		for _, n := range nodes {
			titles = append(titles, n.Title)
		}
		return titles
	}

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
				pw, err := k.Walk(ctx, root, walk.Parent.Title, kb.GatherNeighborhood)
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
			// Store children as titles in Paths and full nodes in PathNodes
			rc.Paths[key] = childTitles(walk.Children)
			var childNodes []ResolvedNode
			for _, c := range walk.Children {
				childNodes = append(childNodes, ResolvedNode{Title: c.Title, Content: c.Content})
			}
			rc.PathNodes[key] = childNodes
		case "siblings":
			rc.Paths[key] = childTitles(walk.Siblings)
			var sibNodes []ResolvedNode
			for _, s := range walk.Siblings {
				sibNodes = append(sibNodes, ResolvedNode{Title: s.Title, Content: s.Content})
			}
			rc.PathNodes[key] = sibNodes
		case "history":
			entries, err := k.ReadLog(ctx, root, target)
			if err != nil && !req.Optional {
				return nil, fmt.Errorf("resolving history: %w", err)
			}
			if len(entries) > 0 {
				var parts []string
				for _, e := range entries {
					line := fmt.Sprintf("[%s] %s %s", e.Timestamp, e.Event, e.Function)
					if e.Result != "" {
						line += ": " + e.Result
					}
					parts = append(parts, line)
				}
				rc.History = strings.Join(parts, "\n")
			}
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
		// Resolve terminal subjects to titles and optional With predicates
		var titles []string
		var resolved []ResolvedNode
		fetchContent := false
		for _, w := range ps.With {
			if w == kb.PredNodeContent {
				fetchContent = true
			}
		}
		for _, subj := range terminals {
			t, _, _ := k.Graph().Lookup(ctx, subj, kb.PredNodeTitle)
			if t == "" {
				continue
			}
			titles = append(titles, t)
			rn := ResolvedNode{Title: t}
			if fetchContent {
				rn.Content, _, _ = k.Graph().Lookup(ctx, subj, kb.PredNodeContent)
			}
			resolved = append(resolved, rn)
		}
		rc.Paths[ps.As] = titles
		rc.PathNodes[ps.As] = resolved
	}

	return rc, nil
}

// RenderPrompt substitutes context into a prompt template.
// Supported placeholders: {{title}}, {{content}}, {{parent}}, {{children}}, {{siblings}}, {{prev}}.
func RenderPrompt(template string, rc *ResolvedContext) string {
	result := template

	if rc.Target != nil {
		result = strings.ReplaceAll(result, "{{title}}", rc.Target.Target.Title)
		result = strings.ReplaceAll(result, "{{content}}", rc.Target.Target.Content)
		if rc.Target.Parent != nil {
			result = strings.ReplaceAll(result, "{{parent}}", rc.Target.Parent.Title)
		} else {
			result = strings.ReplaceAll(result, "{{parent}}", "")
		}
		var childTitles, sibTitles []string
		for _, c := range rc.Target.Children {
			childTitles = append(childTitles, c.Title)
		}
		for _, s := range rc.Target.Siblings {
			sibTitles = append(sibTitles, s.Title)
		}
		result = strings.ReplaceAll(result, "{{children}}", strings.Join(childTitles, ", "))
		result = strings.ReplaceAll(result, "{{siblings}}", strings.Join(sibTitles, ", "))
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
		result = strings.ReplaceAll(result, "{{"+key+".title}}", walk.Target.Title)
		result = strings.ReplaceAll(result, "{{"+key+".content}}", walk.Target.Content)
	}

	// Resolve path-based placeholders: {{pathAs}} and {{pathAs-content}}
	for key, titles := range rc.Paths {
		result = strings.ReplaceAll(result, "{{"+key+"}}", strings.Join(titles, ", "))
	}
	for key, nodes := range rc.PathNodes {
		var contentParts []string
		for _, n := range nodes {
			if n.Content != "" {
				contentParts = append(contentParts, fmt.Sprintf("[%s]\n%s", n.Title, n.Content))
			}
		}
		joined := strings.Join(contentParts, "\n\n")
		result = strings.ReplaceAll(result, "{{"+key+"-content}}", joined)
		result = strings.ReplaceAll(result, "{{"+key+".content}}", joined)
	}

	// Clean up any remaining unresolved placeholders
	result = strings.ReplaceAll(result, "{{context}}", "")

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
