package types

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"olympos.io/encoding/edn"
	"sevens/internal/config"
	"sevens/internal/ednformat"
)

// GraphTypeLoader can load types from the triple store.
// Set by the EDN projection at startup.
var GraphTypeLoader interface {
	LoadTypeDef(ctx context.Context, name string) (*TypeDef, error)
	ListTypes(ctx context.Context) ([]string, error)
	LoadAllTypes(ctx context.Context) (map[string]*TypeDef, error)
}

// Type aliases for the shared EDN format structs.
type ednChildren = ednformat.Children
type ednStructure = ednformat.Structure
type ednPredicates = ednformat.Predicates
type ednOrthographyBinding = ednformat.OrthographyBinding
type ednProjection = ednformat.Projection
type ednGatherSpec = ednformat.GatherSpec
type ednTypeDef = ednformat.TypeDef

// typesDir returns the path to the types configuration directory.
func typesDir() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "types"), nil
}

// LoadTypeDef loads a single type definition by name from the config dir.
func LoadTypeDef(name string) (*TypeDef, error) {
	// Try graph-based loading first.
	if GraphTypeLoader != nil {
		td, err := GraphTypeLoader.LoadTypeDef(context.Background(), name)
		if err == nil && td != nil {
			return td, nil
		}
	}

	// Fall back to file-based loading.
	// Namespaced names like "sevens/minimal" map to filename "sevens-minimal.edn".
	dir, err := typesDir()
	if err != nil {
		return nil, fmt.Errorf("get types dir: %w", err)
	}
	filename := strings.ReplaceAll(name, "/", "-")
	path := filepath.Join(dir, filename+".edn")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("type %q not found", name)
		}
		return nil, fmt.Errorf("reading type %q: %w", name, err)
	}
	return parseTypeDef(data, name)
}

// ListTypeDefs returns all defined types, loaded from the config dir.
func ListTypeDefs() ([]TypeDef, error) {
	// Try graph-based listing first.
	if GraphTypeLoader != nil {
		names, err := GraphTypeLoader.ListTypes(context.Background())
		if err == nil && len(names) > 0 {
			all, aErr := GraphTypeLoader.LoadAllTypes(context.Background())
			if aErr == nil {
				var defs []TypeDef
				sort.Strings(names)
				for _, n := range names {
					if td, ok := all[n]; ok {
						defs = append(defs, *td)
					}
				}
				return defs, nil
			}
		}
	}

	// Fall back to file-based listing.
	dir, err := typesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read types dir: %w", err)
	}
	var defs []TypeDef
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".edn" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".edn")
		td, err := LoadTypeDef(name)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: %v\n", err)
			continue
		}
		defs = append(defs, *td)
	}
	sort.Slice(defs, func(i, j int) bool { return defs[i].Name < defs[j].Name })
	return defs, nil
}

// LoadAllTypeDefs loads all type definitions into a map keyed by name.
func LoadAllTypeDefs() (map[string]*TypeDef, error) {
	// Try graph-based loading first.
	if GraphTypeLoader != nil {
		all, err := GraphTypeLoader.LoadAllTypes(context.Background())
		if err == nil && len(all) > 0 {
			return all, nil
		}
	}

	// Fall back to file-based loading.
	defs, err := ListTypeDefs()
	if err != nil {
		return nil, err
	}
	m := make(map[string]*TypeDef, len(defs))
	for i := range defs {
		m[defs[i].Name] = &defs[i]
	}
	return m, nil
}

// parseTypeDef parses EDN bytes into a TypeDef.
func parseTypeDef(data []byte, filename string) (*TypeDef, error) {
	var raw ednTypeDef
	if err := edn.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse type %s: %w", filename, err)
	}
	if raw.Name == "" {
		return nil, fmt.Errorf("type %s: name must be non-empty", filename)
	}
	td := &TypeDef{
		Name:              raw.Name,
		Extends:           raw.Extends,
		Primitive:         raw.Primitive,
		ContextPolicy:     raw.ContextPolicy,
		Description:       raw.Description,
		SchemaInstruction: raw.SchemaInstruction,
		Predicates: PredicateSpec{
			Required: raw.Predicates.Required,
			Optional: raw.Predicates.Optional,
		},
		Structure: StructureSpec{
			ParentType: raw.Structure.ParentType,
		},
		Projection: ProjectionSpec{
			Frontmatter: raw.Projection.Frontmatter,
		},
	}
	if raw.Structure.Children != nil {
		td.Structure.ChildrenMin = raw.Structure.Children.Min
		td.Structure.ChildrenMax = raw.Structure.Children.Max
	}
	if len(raw.Projection.Orthography) > 0 {
		td.Projection.Orthography = make(map[string]OrthographyBinding, len(raw.Projection.Orthography))
		for k, v := range raw.Projection.Orthography {
			td.Projection.Orthography[k] = OrthographyBinding{
				Signifier:  v.Signifier,
				ValueModel: v.ValueModel,
			}
		}
	}
	if raw.Gather != nil {
		td.Gather = GatherSpec{
			Target:   raw.Gather.Target,
			Parent:   raw.Gather.Parent,
			Children: raw.Gather.Children,
			Siblings: raw.Gather.Siblings,
			Subtree:  raw.Gather.Subtree,
		}
	}
	return td, nil
}
