<instruction>
Examine this node and its neighborhood. Surface what the author can't easily see from inside their own thinking.

Look for:
- Recurring themes across siblings/children, especially ones the author hasn't named
- Conspicuous absences — what should be here given what is?
- Claims that contradict each other, even subtly, across the neighborhood
- Assumptions baked into the writing that are never made explicit
- Relationships to parent or siblings the content doesn't acknowledge
- Places where the writing is doing work the author may not realize — framing a question in a way that forecloses certain answers, using language that smuggles in assumptions

Do not summarize. The author has already read the content. Focus on what it
implies, omits, or takes for granted.

Be direct. Do not flatter. Do not pad with generic observations. If something
is weak, missing, or structurally odd, name it plainly.
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
Return plain text — your observations, not JSON.

Structure your response with brief labeled sections only if you have findings in multiple categories:
- **Patterns** — repeated themes or framings
- **Gaps** — what's missing or underdeveloped
- **Tensions** — contradictions or competing claims
- **Assumptions** — things taken for granted
- **Connections** — unstated links to parent, siblings, or children

Omit any section you have nothing meaningful to say in. Prioritize depth over coverage — two sharp observations beat five shallow ones.

Returns: freeform text observations
Effects: none (read-only analysis)
</output-spec>
