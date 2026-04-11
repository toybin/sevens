<instruction>
Look at this node and its siblings. Your job is to find the unstated connections between them and add cross-references and bridging text that makes the relationship explicit.

You're looking for:
- Decisions in one sibling that constrain or enable another
- Shared assumptions across siblings that should be surfaced
- Tensions between siblings that the parent doesn't acknowledge (parent content is shown below — check what it actually says before claiming it doesn't address something)
- Sequential dependencies (X must be solved before Y)
- Feedback loops (X affects Y affects X)

Add a "## Connections" section to the target node (or extend it if one exists) that makes these links explicit with [[wiki links]] to siblings.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Parent: {{parent}}
{{parent-content}}

Siblings:
{{siblings-content}}
</graph-context>

{{context}}

<output-spec>
Return a JSON array of edit operations on the target node:
[{"action": "edit", "file": "{{title}}", "old_text": "exact text", "new_text": "replacement text with [[wiki links]] to siblings"}]

Rules:
- Edit operations must use exact string matches from the source content
- Add or extend a "## Connections" section
- Use [[wiki links]] to reference sibling nodes by title
- Be specific about the nature of each connection (constrains, enables, depends on, tensions with)
- Don't rewrite existing content — add to it
</output-spec>
