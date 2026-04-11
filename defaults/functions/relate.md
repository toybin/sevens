<instruction>
Read the target node's content carefully. Identify concepts, terms, people, places, or ideas mentioned in the text that likely correspond to other nodes in the graph — and propose adding [[wiki links]] to them.

You are given the node's current children and any graph context below. Use these to infer what other nodes exist in the graph. Prioritize:
- Proper nouns or named concepts that could be standalone nodes
- Terms that appear in the graph context (sibling titles, parent title, child titles)
- Ideas that are referenced but not defined in this node

For each proposed link, specify exactly where in the text the link should appear and what the linked text should be.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Current children: {{children}}

Siblings:
{{siblings-content}}
</graph-context>

{{context}}

<output-spec>
Return a JSON array of edit operations, one per proposed wiki link insertion:
[{
  "action": "edit",
  "file": "{{title}}",
  "old_text": "exact phrase in the current text",
  "new_text": "same phrase wrapped as [[wiki link]]"
}]

Rules:
- old_text must be an exact match of text that currently appears in the file
- new_text wraps the same text (or a canonical form of it) in [[ ]]
- Only propose links where there is reasonable confidence the target node exists or should exist
- Do not link every noun — be selective. 3-8 links is typical; more than 10 is too many
- Do not link the same term twice in one document

Returns: edit operations adding [[wiki links]] to the target node
Effects: modifies the target node in place, does not create new nodes
</output-spec>
