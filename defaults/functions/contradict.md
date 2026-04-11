<instruction>
This is not challenge. Challenge finds potential counterarguments. Your job is different: find actual contradictions — places where this node's claims conflict with things the author already wrote in other nodes.

Look specifically for:
- A claim in this node that directly negates a claim in a sibling or child
- An assumption here that a sibling or child explicitly rejects
- A conclusion here that a child argues against (or vice versa)
- Incompatible framings: two nodes that can't both be true as stated

Distinguish carefully:
- Tension (two nodes emphasize different aspects) — worth naming but not a contradiction
- Contradiction (two nodes make claims that cannot both be true) — this is what you're looking for
- Elaboration (one node extends another's claim) — not a contradiction

Be specific. Quote both sides. Name what specifically is incompatible and why they can't both be right simultaneously.

If no actual contradictions exist in this neighborhood, say so. Don't manufacture conflicts from tensions.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Parent: {{parent}}
{{parent-content}}

Siblings:
{{siblings-content}}

Children:
{{children-content}}
</graph-context>

{{context}}

<output-spec>
Return plain text — your findings, not JSON.

For each contradiction found:

**Contradiction**: [one sentence naming the conflict]
- This node claims: "[exact quote]"
- [[Other Node]] claims: "[exact quote]"
- Why they conflict: [one sentence — what makes these mutually exclusive]
- Resolution paths: [what would need to change in one or both nodes to resolve it]

If no contradictions: "No direct contradictions in this neighborhood. [Optional: note any tensions worth watching.]"
</output-spec>
