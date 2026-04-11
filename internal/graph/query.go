package graph

import (
	"database/sql"
	"fmt"
	"sort"
	"strconv"

	"sevens/internal/store"
)

// childrenInRoot returns all node titles whose parent subject and root match.
func childrenInRoot(db *sql.DB, parentSubject, root string) ([]string, error) {
	rows, err := db.Query(`
		SELECT COALESCE(t3.object, t1.subject) FROM triples t1
		JOIN triples t2 ON t1.subject = t2.subject
		LEFT JOIN triples t3 ON t1.subject = t3.subject AND t3.predicate = 'node/title'
		WHERE t1.predicate = 'node/parent' AND t1.object = ?
		AND t2.predicate = 'node/root' AND t2.object = ?
		ORDER BY 1
	`, parentSubject, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		results = append(results, s)
	}
	return results, rows.Err()
}

func BuildOverview(db *sql.DB, root string, config Config) (*OverviewOutput, error) {
	titles, err := store.ListNodeTitles(db, root)
	if err != nil {
		return nil, fmt.Errorf("querying nodes: %w", err)
	}
	sort.Strings(titles)

	// Batch-fetch all node metadata in a single query instead of N+1
	nodeData, err := store.GetRootNodeData(db, root, []string{
		"node/parent", "ref/wiki-link", "content/char-count",
	})
	if err != nil {
		return nil, fmt.Errorf("batch querying node data: %w", err)
	}

	// Build parent→children map from the parent data
	childMap := make(map[string][]string)
	for _, title := range titles {
		if data, ok := nodeData[title]; ok {
			if parents := data["node/parent"]; len(parents) > 0 {
				parentTitle, err := store.NodeTitle(db, parents[0])
				if err == nil && parentTitle != "" {
					childMap[parentTitle] = append(childMap[parentTitle], title)
				}
			}
		}
	}
	// Sort children for consistency
	for k := range childMap {
		sort.Strings(childMap[k])
	}

	// Batch-fetch pending suspensions for this root
	pendingMap := make(map[string]string) // node title → function name
	pendingSubs, _ := db.Query(`
		SELECT t2.object, t3.object FROM triples t1
		JOIN triples t2 ON t1.subject = t2.subject
		JOIN triples t3 ON t1.subject = t3.subject
		JOIN triples t4 ON t1.subject = t4.subject
		WHERE t1.predicate = 'suspension/status' AND t1.object = 'pending'
		AND t2.predicate = 'suspension/target'
		AND t3.predicate = 'suspension/function'
		AND t4.predicate = 'suspension/root' AND t4.object = ?
	`, root)
	if pendingSubs != nil {
		defer pendingSubs.Close()
		for pendingSubs.Next() {
			var target, fn string
			pendingSubs.Scan(&target, &fn)
			pendingMap[target] = fn
		}
	}

	nodes := make([]OverviewNode, 0, len(titles))
	for _, title := range titles {
		data := nodeData[title]

		var parent *string
		if parents := data["node/parent"]; len(parents) > 0 {
			parentTitle, err := store.NodeTitle(db, parents[0])
			if err == nil && parentTitle != "" {
				parent = &parentTitle
			}
		}

		children := childMap[title]
		if children == nil {
			children = []string{}
		}

		crossRefs := data["ref/wiki-link"]
		if crossRefs == nil {
			crossRefs = []string{}
		}

		charCount := 0
		if counts := data["content/char-count"]; len(counts) > 0 {
			charCount, _ = strconv.Atoi(counts[0])
		}

		nodes = append(nodes, OverviewNode{
			Title:      title,
			Parent:     parent,
			Children:   children,
			ChildCount: len(children),
			CrossRefs:  crossRefs,
			CharCount:  charCount,
			Pending:    pendingMap[title],
		})
	}

	report, err := Validate(db, root, config)
	if err != nil {
		return nil, fmt.Errorf("validating graph: %w", err)
	}

	return &OverviewOutput{Nodes: nodes, Validation: report}, nil
}

func BuildWalk(db *sql.DB, root string, title string, depth int) (*WalkOutput, error) {
	subject, canonical := store.ResolveNode(db, title, root)
	if canonical == "" {
		return nil, fmt.Errorf("node not found: %s", title)
	}
	title = canonical

	content, err := store.GetObject(db, subject, "node/content")
	if err != nil {
		return nil, fmt.Errorf("querying content for %q: %w", title, err)
	}

	parentSubject, err := store.GetObject(db, subject, "node/parent")
	if err != nil {
		return nil, fmt.Errorf("querying parent for %q: %w", title, err)
	}
	var parent *string
	if parentSubject != "" {
		parentTitle, err := store.NodeTitle(db, parentSubject)
		if err != nil {
			return nil, fmt.Errorf("querying parent title for %q: %w", title, err)
		}
		parent = &parentTitle
	}

	contextFiles, err := store.GetObjects(db, subject, "context/file")
	if err != nil {
		return nil, fmt.Errorf("querying context files for %q: %w", title, err)
	}
	if contextFiles == nil {
		contextFiles = []string{}
	}

	children := []string{}
	if depth >= 1 {
		children, err = childrenInRoot(db, subject, root)
		if err != nil {
			return nil, fmt.Errorf("querying children of %q: %w", title, err)
		}
		if children == nil {
			children = []string{}
		}
	}

	siblings := []string{}
	if parentSubject != "" {
		all, err := childrenInRoot(db, parentSubject, root)
		if err != nil {
			return nil, fmt.Errorf("querying siblings of %q: %w", title, err)
		}
		for _, s := range all {
			if s != title {
				siblings = append(siblings, s)
			}
		}
	}

	crossRefs, err := store.GetObjects(db, subject, "ref/wiki-link")
	if err != nil {
		return nil, fmt.Errorf("querying cross refs for %q: %w", title, err)
	}
	if crossRefs == nil {
		crossRefs = []string{}
	}

	allTitles, err := store.ListNodeTitles(db, root)
	if err != nil {
		return nil, fmt.Errorf("querying unwalked nodes: %w", err)
	}
	sort.Strings(allTitles)
	unwalked := []string{}
	for _, t := range allTitles {
		if t != title {
			unwalked = append(unwalked, t)
		}
	}

	roleMap := make(map[string]string)
	roleData, err := store.GetRootNodeData(db, root, []string{"sibling/role"})
	if err == nil {
		for nodeTitle, data := range roleData {
			if roles := data["sibling/role"]; len(roles) > 0 {
				roleMap[nodeTitle] = roles[0]
			}
		}
	}

	childRoles := make(map[string]string)
	for _, ch := range children {
		if r, ok := roleMap[ch]; ok {
			childRoles[ch] = r
		}
	}
	siblingRoles := make(map[string]string)
	for _, sib := range siblings {
		if r, ok := roleMap[sib]; ok {
			siblingRoles[sib] = r
		}
	}
	ownRole := roleMap[title]

	walkNode := WalkNode{
		Subject:      subject,
		Title:        title,
		Parent:       parent,
		Content:      content,
		Children:     children,
		Siblings:     siblings,
		CrossRefs:    crossRefs,
		ContextFiles: contextFiles,
		ChildRoles:   childRoles,
		SiblingRoles: siblingRoles,
		Role:         ownRole,
	}

	return &WalkOutput{Node: walkNode, Unwalked: unwalked}, nil
}

// AutoGroupIncludes checks if the target node has include-group: true in its
// frontmatter. If so, finds the group in config where this node is the root
// and resolves all member titles (excluding the target itself, since it's
// already the context target).
func AutoGroupIncludes(db *sql.DB, root string, nodeTitle string, config Config) ([]string, error) {
	subject, _ := store.ResolveNode(db, nodeTitle, root)
	flag, _ := store.GetObject(db, subject, "node/include-group")
	if flag != "true" {
		return nil, nil
	}

	// Find which group has this node as its root.
	for _, group := range config.Groups {
		if group.Root == nodeTitle {
			titles, err := ResolveGroup(db, root, group)
			if err != nil {
				return nil, err
			}
			// Exclude the target itself — it's already the context target.
			var filtered []string
			for _, t := range titles {
				if t != nodeTitle {
					filtered = append(filtered, t)
				}
			}
			return filtered, nil
		}
	}
	return nil, nil
}

// ResolveGroup returns the list of node titles in a group: root + all recursive
// descendants, minus excluded subtrees, plus any extra singleton nodes.
func ResolveGroup(db *sql.DB, root string, group Group) ([]string, error) {
	excluded := make(map[string]bool)
	for _, e := range group.Exclude {
		excluded[e] = true
	}

	var titles []string

	// Recursive descent from group root, pruning excluded subtrees.
	var walk func(title string) error
	walk = func(title string) error {
		if excluded[title] {
			return nil // prune entire subtree
		}
		titles = append(titles, title)
		subject, _ := store.ResolveNode(db, title, root)
		children, err := childrenInRoot(db, subject, root)
		if err != nil {
			return err
		}
		for _, child := range children {
			if err := walk(child); err != nil {
				return err
			}
		}
		return nil
	}

	if err := walk(group.Root); err != nil {
		return nil, fmt.Errorf("resolving group from %q: %w", group.Root, err)
	}

	// Add singleton nodes (not already present).
	seen := make(map[string]bool, len(titles))
	for _, t := range titles {
		seen[t] = true
	}
	for _, n := range group.Nodes {
		if !seen[n] && !excluded[n] {
			titles = append(titles, n)
		}
	}

	return titles, nil
}
