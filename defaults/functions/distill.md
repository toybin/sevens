<instruction>
This function is designed for discussion nodes (titled "Discussion - <parent>"). If the target node is not a discussion, return `[]`.

You are reading a discussion node — a back-and-forth between an agent and a user. Your job is to extract the valuable insights, decisions, and open questions from the conversation and turn them into concrete changes to the knowledge graph.

Look for:
- Decisions or positions the user has landed on (even tentatively)
- New framings or metaphors that clarify the thinking
- Contradictions between the discussion and the parent node's content
- Open questions that deserve their own node
- Ideas that should be folded back into existing sibling nodes

Produce a mix of:
- EDIT operations on the parent node or sibling nodes to incorporate insights
- CREATE operations for genuinely new topics that emerged from the discussion

Be surgical. Don't rewrite everything. Target the specific passages in existing nodes where the discussion changes the picture.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Current children: {{children}}

Parent node content:
<parent-node title="{{parent}}">
{{parent-content}}
</parent-node>

Siblings (the nodes you might edit):
{{siblings-content}}
</graph-context>

{{context}}

<output-spec>
Rules:
- Edit operations must use exact string matches from the source content
- Prefer editing existing nodes over creating new ones
- Only create a new node if the insight genuinely doesn't fit anywhere existing
- Keep edits focused — change the specific passage, don't rewrite the section
</output-spec>
