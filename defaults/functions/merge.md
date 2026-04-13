<instruction>
The target node has been decomposed into children. Your job is to reverse that decomposition: synthesize the children's content back into the parent, producing a unified, coherent body.

You are given the target node's current content and the full content of its children.

The merged result should:
- Integrate the key points each child covers into a single flowing document
- Be more than a list of child titles — write prose or structured sections that actually synthesize
- Preserve any [[wiki links]] from the original content
- Be appropriately concise: a synthesis, not an expansion
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Children:
{{children-content}}
</graph-context>

{{context}}

<output-spec>
Rules:
- old_text must be an exact substring of the current content
- new_text should be a well-structured synthesis, not a list of child titles
- Preserve frontmatter and the top-level heading — only replace the body
- Do NOT delete the child nodes (that is a separate operation)

</output-spec>
