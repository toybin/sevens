<instruction>
This function is for external content, collected material, or nodes that have grown bloated through AI elaboration. If the author wrote this node themselves and it's the right length, summarizing it is outsourcing comprehension — return `[]`.

Condense this node's content. Your job is to shorten, not rewrite — the voice, stances, and structure belong to the author.

- Preserve the author's positions. Do not soften, hedge, or add nuance that isn't in the original.
- Prioritize decisions, conclusions, and stated positions over background explanation.
- Preserve section headers where the original has them — a 3-section note should still have 3 sections, just shorter.
- Keep all [[wiki links]] that appear in the content.
- Do not add context, caveats, or framing that wasn't in the original.
- Target roughly half the length. 2-3 paragraphs for prose nodes; a bulleted list for already-structured nodes.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Current children: {{children}}
</graph-context>

{{context}}

<output-spec>
Return a JSON array with edit operations:
[{"action": "edit", "file": "{{title}}", "old_text": "the full body text after the heading", "new_text": "concise 2-3 paragraph summary"}]

Returns: edit operations replacing verbose content with summary
Effects: modifies the target node, preserves frontmatter and heading, preserves [[wiki links]]
</output-spec>
