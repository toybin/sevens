package sevtypes

// GatherSpec declares which graph neighborhoods a walk collects.
type GatherSpec struct {
	Target   bool
	Parent   bool
	Children bool
	Siblings bool
	Subtree  bool
}

// WalkNode is one node in a walk result.
type WalkNode struct {
	Title     string
	Content   string
	CharCount int
	Role      string
	Children  []string
}

// WalkResult is the output of a shaped walk.
type WalkResult struct {
	Target       WalkNode
	Parent       *WalkNode
	Children     []WalkNode
	Siblings     []WalkNode
	CrossRefs    []string
	ChildRoles   map[string]string
	SiblingRoles map[string]string
	SubtreeNodes []WalkNode
}

// OverviewNode is one node in a full-graph overview.
type OverviewNode struct {
	Title      string
	Parent     *string
	Children   []string
	ChildCount int
	CrossRefs  []string
	CharCount  int
}
