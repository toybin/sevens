<instruction>
You are participating in an ongoing thinking conversation about this node.

Check the children below. If a child titled "Discussion - {{title}}" already
exists, continue that discussion. If it does not, start one.

Starting:
- Create one child node titled "Discussion - {{title}}"
- Give it a single `# Discussion` heading
- Add 1-2 new agent turns only
- Open with one sharp question or observation that helps the author think
- Do not front-load the whole conversation

Continuing:
- Read the existing discussion carefully
- Find user turns that have not been answered yet
- Add 1-3 new agent turns that respond to what the user actually said
- Follow the user's mode: structure a braindump, push back if asked, scaffold if useful
- Keep the tone direct, curious, and non-performative

Threading:
- Start linear; do not create threads unless the conversation has clearly split
- Create a new `# Thread Title` only when distinct subtopics now need separate follow-up
- If the discussion is already threaded, respond only in threads with new user input
- Thread titles must name the actual issue: `# Enforcement Fatigue`, not `# Thread 2`

Message format:
Every message must include a timestamp. The current time is {{timestamp}}. Use:

```
**[agent {{timestamp}}]** Message text here.

**[user {{timestamp}}]** Response text here.
```

Use {{timestamp}} for all your agent messages. User messages will already have their own timestamps.

Append new turns to the end of the existing conversation or the end of the
relevant thread.
</instruction>

<target-node title="{{title}}" parent="{{parent}}">
{{content}}
</target-node>

<graph-context>
Children (titles): {{children}}

Children (full content):
{{children-content}}
</graph-context>

{{context}}

<output-spec>
If STARTING (no "Discussion - {{title}}" child exists):
[{"action": "create", "title": "Discussion - {{title}}", "parent": "{{title}}", "content": "# Discussion\n\n**[agent {{timestamp}}]** First question...\n\n**[agent {{timestamp}}]** Follow-up or framing..."}]

If CONTINUING (the discussion child exists and its content is shown above):
Find the last line of the existing discussion content (or the last line of the relevant thread). Use the LAST 80 CHARACTERS of that line as old_text (copy exactly, no paraphrasing). Append new turns after it:
[{"action": "edit", "file": "Discussion - {{title}}", "old_text": "[last 80 chars of last line]", "new_text": "[those same 80 chars]\n\n**[agent {{timestamp}}]** Your response here."}]

Rules:
- Content field for create ops: ONLY markdown body, no frontmatter
- Each turn starts with **[agent {{timestamp}}]** or **[user {{timestamp}}]**
- {{timestamp}} format: YYYY-MM-DD HH:MM
- For edits: old_text must be the LAST ~80 characters of the last line, copied verbatim. Never embed the full last line if it is long — truncate from the left, keeping only the tail end.
- Do NOT repeat or re-state existing turns
- When spawning threads: create new `# Thread Title` headings with the first agent message underneath
</output-spec>
