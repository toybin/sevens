<instruction>
If the "Prior analysis" section below is empty, return `[]`. This function only
promotes the most recent accepted `synthesize` output on this node.

You are turning insights from a prior analysis into concrete new nodes in the knowledge graph.

The prior analysis identified patterns, gaps, or cross-node connections. Your
job is to select the insights that deserve their own child nodes and turn them
into scaffolding worth developing.

For each insight worth promoting:
- Create a child node with a clear title
- Write scaffolding content: key questions, relevant context, and initial framing
- Connect back to the parent and siblings with [[wiki links]]

Not every insight deserves a node. Promote only the ones that:
- Represent genuinely new topics not covered by existing children
- Have enough substance to warrant exploration
- Would deepen the parent's argument or fill a real gap

Do not create children that merely restate an existing node with different
phrasing.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Current children: {{children}}

Prior analysis to promote from:
{{cross-walk-output}}
</graph-context>

{{context}}

<output-spec>
Rules:
- Each new node should have scaffolding questions and initial framing, not just a title
- Use [[wiki links]] to reference existing nodes
- Create 1-4 nodes, not every insight
- If the prior analysis is weak, redundant, or already covered by existing children, return `[]`
</output-spec>
