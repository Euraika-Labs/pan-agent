# Memory and Persona

Pan-Agent uses two text files to give the agent identity (persona) and memory across sessions.

## Persona (SOUL.md)

The persona is the agent's identity. It's prepended to every chat as a system message.

Default persona:
```
You are Pan, a helpful AI assistant. You are honest, concise,
and you use the available tools to accomplish what the user asks.
```

You can customize this from the Soul screen. Common edits:
- Add domain expertise: "You are an expert in Go and Tauri development."
- Set tone: "Always respond in Dutch unless asked otherwise."
- Add behavioral constraints: "Never use the terminal tool without first explaining what the command will do."
- Add running context: "The user is Bert. He works on Pan-Agent. He prefers terse responses."

The persona is per-profile. Edit it once per profile.

### Reset to default
Click "Reset" on the Soul screen, or:

```bash
curl -X POST http://localhost:8642/v1/persona/reset
```

This restores the bundled default persona.

## Memory (MEMORY.md)

The memory file stores facts the agent should remember across conversations. Format: entries separated by `§` delimiters.

```
This is one memory entry about the user's preferences.

§

This is another entry about a project we've been working on.

§

A third entry with technical context that should persist.
```

The memory tool (`memory_tool`) lets the agent add entries during conversations. Memories show up in the system prompt prepended to chats.

### Add a memory manually

Memory screen → "Add Entry" → type the content → save.

Or via API:

```bash
curl -X POST http://localhost:8642/v1/memory \
  -H "Content-Type: application/json" \
  -d '{"content": "User prefers code examples over prose."}'
```

### Update / delete

Memory screen lists entries with edit and delete buttons. Or via API:

```bash
# Update entry at index 0 (zero-based)
curl -X PUT http://localhost:8642/v1/memory/0 \
  -H "Content-Type: application/json" \
  -d '{"content": "Updated text"}'

# Delete entry at index 0
curl -X DELETE http://localhost:8642/v1/memory/0
```

## USER.md

USER.md is a separate file for user profile information. It's read alongside MEMORY.md but isn't currently displayed in a dedicated screen.

You can edit it directly with a text editor. Format is plain text.

## How memory is loaded

When you send a chat message, the backend:

1. Reads `SOUL.md` → uses as the system message.
2. Loads the conversation history for the current session from SQLite.
3. The agent generates a response.

The MEMORY.md content is not automatically prepended — the agent must use the `memory_tool` to query its memory. This keeps the system prompt small while letting the agent recall things on demand.

## Memory format conventions

The memory file has an informal but consistent style:

- Each entry is one paragraph.
- Entries should be self-contained (don't reference "the previous entry").
- Use present tense for facts ("User is named Bert") and past tense for events ("Migrated from Hermes Desktop in April 2026").
- Avoid storing temporary state. Memory is for things that should persist.

## Operator rule
The persona affects every chat. Test changes carefully — a poorly-worded persona can make the agent unhelpful or unsafe across all conversations until you fix it.

## Read next
- [[03 - Profiles]]
- [[02 - Tools Catalog]]
