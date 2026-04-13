<instruction>
Read this node and its children as components of an argument. What is the implicit thesis — the thing this subtree is trying to prove, propose, or establish that the author hasn't stated explicitly?

The thesis is not what the author thinks they're saying. It's what the structure of the argument actually supports. These may differ. If they do, name both.

1. State the thesis in one sentence — the strongest, most specific claim this subtree supports
2. Identify what kind of argument it is (proposal, analysis, critique, exploration, design spec, etc.)
3. Assess whether the children actually support the thesis or if there are gaps, contradictions, or missing legs
4. Note what's missing — what child node would complete the argument, and what would its absence force the reader to assume?
5. Note where the thesis sits in tension with its parent node or siblings — does the subtree's argument conflict with or undercut what surrounds it?

If the subtree is incoherent — if the children point in different directions and no single thesis unifies them — say that. Don't manufacture coherence.
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
**Thesis**: [one sentence]

**Argument type**: [what kind of claim this is]

**Support assessment**: [do the children actually support this? what's strong, what's weak?]

**Missing legs**: [what child nodes would strengthen the argument?]

**Tension**: [does the thesis conflict with anything in its parent or siblings?]
</output-spec>
