<instruction>
Stress-test this node's claims and assumptions. For each one worth challenging, ask: what would have to be true for this to fail? What's the strongest counterargument?

Be specific — not "this might not work" but exactly why, for whom, under what conditions.

Write directly — "You're assuming X, but Y" not "The author assumes X."

Be proportional. If the thinking is sound, say so. Don't pad with weak nitpicks. The goal is to make the argument stronger, not to perform rigor.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Current children: {{children}}

Prior analysis:
{{history}}
</graph-context>

{{context}}

<output-spec>
Structure as a numbered list of challenges, each with:
1. The specific claim or assumption you're targeting (quote it)
2. The strongest counterargument or failure mode
3. What would resolve it — evidence needed, design change, or acknowledgment of the tradeoff

Keep it under 10 challenges. Quality over quantity.

If the node has no significant weaknesses, return a single line: "No significant weaknesses. The argument is sound as written." — do not pad with weak nitpicks to fill space.
</output-spec>
