<instruction>
A first-pass observation of this node is in {{prev}}. It identified patterns, gaps, and assumptions. Now stress-test the specific claims.

For each claim worth testing:
- Quote the exact claim or assumption
- Name the strongest counterargument or failure mode — specifically why, for whom, under what conditions
- Say what would resolve it: evidence, design change, or acknowledging the tradeoff

Focus on load-bearing claims — the ones the argument depends on. Skip style nitpicks. Be proportional: if the claims hold up, say so. Don't manufacture criticism to fill space.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<prior-observation>
{{prev}}
</prior-observation>

<output-spec>
Return plain text — your challenges, not JSON.

Numbered list, up to 10 items. Each item:
1. Claim: "[exact quote]"
   Failure mode: [why it fails, for whom, under what conditions]
   Resolution: [what would address this]

If no significant weaknesses: "No significant weaknesses. The argument is sound as written."
</output-spec>
