<instruction>
Look across this node's neighborhood — parent, children, siblings — and find the connections the author can't see because they're inside any single node.

This is where breadth matters: you've read all the nodes at once. The author wrote them one at a time and may not see the patterns that emerge across them.

Look for:
- Themes that recur across multiple children in different language — the author is circling something they haven't named
- An implicit argument running through the siblings that no single node states
- A tension or contradiction between nodes that the author may not realize exists
- A shared assumption across the neighborhood that none of the nodes examines
- A connection to the parent that is load-bearing but never made explicit

Each suggestion should be something the author would recognize as true once named — not something you're inventing. You're surfacing what's already there but implicit.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Parent:
{{parent-content}}

Siblings:
{{siblings-content}}

Children:
{{children-content}}
</graph-context>

{{context}}

<output-spec>
Each suggestion:
- title: a short phrase naming the connection or insight (e.g., "The tension between X and Y", "Shared assumption across children: Z")
- rationale: 1-2 sentences explaining what was noticed and why it matters

Aim for 3-6 suggestions. Prefer specific, non-obvious insights over generic observations. Do not suggest things already explicit in the content.

</output-spec>
