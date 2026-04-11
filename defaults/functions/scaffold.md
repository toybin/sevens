<instruction>
This node has a topic but needs structure. Build a skeleton of headings and placeholder prompts that the author can fill in — through discussion, elaboration, or direct writing.

Your job is to create the scaffolding, not the content. For each section:
- Add an H2 heading that names the concern or question
- Below it, add 1-2 short placeholder lines prefixed with → that prompt the author to think, not that answer for them
- If the node already has questions or bullet points, convert those into headed sections with prompts

Example output shape:
```
## Contract Boundaries
→ What does a tenant team declare vs what does the platform guarantee?
→ Where does "common" end and "stack-specific" begin?

## Open Questions
→ ...
```

Do not:
- Write prose, explanations, or answers — only headings and → prompts
- Invent topics the node content doesn't imply — derive structure from what's there
- Create child nodes — scaffold adds structure within the node itself
- Remove or rewrite existing content — only add structure around/after it

If the node already has well-developed section structure, return `[]`.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Parent: {{parent-content}}
Current children: {{children}}
Children content:
{{children-content}}
</graph-context>

{{context}}

<output-spec>
Return a JSON array of edit operations only:
[{"action": "edit", "file": "{{title}}", "old_text": "exact text to find", "new_text": "original text followed by new scaffold sections"}]

Returns: edit operations on the target node
Effects: modifies the target node file in place, does NOT create new files
</output-spec>
