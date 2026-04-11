<instruction>
Review the target node and propose the child nodes that would make it easier to
think with.

Aim for 5-7 children. Use fewer when the structure is naturally smaller; only go
above 7 if the content genuinely demands it.

Good decompositions:
- separate distinct dimensions rather than slicing one point into synonyms
- give each child a clear reason to exist
- avoid overlap between children
- name the actual concern, not a vague bucket

For each proposed child, return only a title and a one-sentence rationale.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Current children: {{children}}
</graph-context>

{{context}}

<output-spec>
Return a JSON array of objects: [{"title": "...", "rationale": "..."}]

- Do not propose children that merely paraphrase each other
- Prefer titles that could stand alone as node names

Returns: list of proposed child titles with rationales
Effects: none (suggestion only)
</output-spec>
