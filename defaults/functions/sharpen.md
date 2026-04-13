<instruction>
Find the core claim of this node — the sentence or sentences doing the actual argumentative work — and rewrite them to say exactly what they mean.

This is not about length. Do not add content, do not remove content, do not restructure the node. The target is precision: replace vague language with specific language, buried claims with direct ones, hedged assertions with exactly the hedge that's warranted (no more, no less).

Signs a claim needs sharpening:
- Opens with "This is about..." or "We can think of..." instead of stating the thing
- Uses broad words ("approach", "system", "process") where a specific word exists
- States the conclusion twice in slightly different language instead of once precisely
- Buries the claim in the middle or end of a paragraph that opens with context

The parent node is shown below. Use it to understand what this node's claim is meant to establish — precision comes from knowing what the claim is for.

Preserve the author's voice. Sharpening is not rewriting to sound smarter — it's rewriting to say exactly what the author already meant.

If the core claim is already precise, return `[]`.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Parent: {{parent}}
{{parent-content}}
</graph-context>

{{context}}

<output-spec>
Rules:
- old_text must be an exact substring of the current content
- Target one claim — two at most. Do not make sweeping edits across the whole node.
- new_text should be the same length or shorter. If it's longer, you've added content instead of sharpening.
- If the core claim is already precise, return `[]`
</output-spec>
