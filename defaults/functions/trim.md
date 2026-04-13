<instruction>
Find content in this node that doesn't belong and propose surgical removals.

Look for three things specifically:

1. **Redundancy with children**: Content that has been decomposed into child nodes and is now better stated there. After decompose/elaborate cycles, the parent often still contains the original dense version of something a child now covers properly.

2. **Scope drift**: Paragraphs or sentences that belong to a different node — they address a topic that either already has its own node or should. Content that would fit more precisely elsewhere.

3. **Padding**: Hedges, qualifications, or restatements that don't advance the node's argument. Sentences that say the same thing as the sentence before. Opening paragraphs that describe what the node is going to say instead of saying it.

Do not:
- Remove content just because it's long
- Remove content that is the node's actual argument
- Rewrite or restructure — only remove

The children are shown below. Check them before flagging anything as redundant — only flag it if a child genuinely covers it better.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Parent: {{parent}}

Children:
{{children-content}}
</graph-context>

{{context}}

<output-spec>
Rules:
- old_text must be an exact substring of the current content
- new_text must be non-empty — use surrounding context to stitch, not an empty string
- new_text must be non-empty — the surrounding context is enough (e.g., old_text spans the preceding paragraph through the removed paragraph; new_text is just the preceding paragraph)
- If nothing should be removed, return `[]`
</output-spec>
