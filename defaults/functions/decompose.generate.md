<instruction>
Create the child nodes listed in the approved suggestions. Each child should be scaffolding — a starting point, not a finished document. For each child, generate a short markdown body with:
- 1–2 sentences framing what this node is about
- 2–3 questions that would draw out the key thinking
- Cross-references using [[wiki links]] where obvious

Keep each child under 500 characters. Do NOT write full essays or enumerate detailed sub-points. The user will flesh these out.

Also edit the source node to replace its dense content with a concise summary that links to the new children.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<approved-suggestions>
{{prev}}
</approved-suggestions>

{{context}}

<output-spec>
Return a JSON array of operations:
- Create children: {"action": "create", "title": "...", "parent": "{{title}}", "content": "..."}
- Edit source: {"action": "edit", "file": "{{title}}", "old_text": "...", "new_text": "..."}

Rules:
- The content field for create ops should contain ONLY the markdown body — no frontmatter. Title and parent are set automatically from the other fields.

Returns: create ops for each child + edit ops for the source node
Effects: creates new .md files, edits the source node
</output-spec>
