# Chat

The Chat screen is where you talk to your Pan-Agent.

## Basic flow

1. Type a message in the input box.
2. Press Enter (Shift+Enter for newline).
3. The agent's response streams in token by token.
4. When the agent calls a tool, you'll see the call in the message stream.
5. Dangerous tool calls trigger an approval modal — click Approve or Deny.
6. The conversation persists. Refresh the page or restart the app and your messages are still there.

## Streaming responses

Pan-Agent uses Server-Sent Events for chat. The frontend opens an SSE stream to `POST /v1/chat/completions` and renders chunks as they arrive.

If the connection drops mid-response, the partial message stays in the database and visible in the UI. The agent's full response was generated up to whatever was streamed before disconnect.

## Tool calls in the chat

When the LLM decides to use a tool, you see a tool call card in the conversation:

```
┌─ Tool call: filesystem ────────────────┐
│ {"action": "read", "path": "README.md"} │
│                                          │
│ Result: # Pan-Agent ...                  │
└──────────────────────────────────────────┘
```

For dangerous tools (terminal, filesystem, code_execution, browser), you'll see an approval modal first:

```
┌─ Approval required ──────────────────────┐
│ Tool: terminal                            │
│ Args: rm /tmp/build/old-binary           │
│                                            │
│ [Approve]   [Deny]                        │
└────────────────────────────────────────────┘
```

After approval, the tool runs and the result appears.

## Aborting generation

The Stop button cancels the current agent loop. It calls `POST /v1/chat/abort` with the session ID, which cancels the context. The LLM stops streaming, any pending tool calls are abandoned, and the UI stops rendering.

## New chat

Click "New Chat" or press Ctrl+N (Cmd+N on macOS). This clears the conversation and starts a new session. The previous session is still in your history under Sessions.

## Resume a session

The Sessions screen lists all past conversations. Click any session to load its messages back into the Chat view.

You can continue an old conversation — the agent re-reads the entire message history before generating the next response.

## Search

Press Ctrl+K (Cmd+K) to open the Search screen. Searches use SQLite FTS5 against all message content across all sessions. Click any result to jump to that session.

## What the agent sees

Each chat turn, the agent receives:
- The persona from `SOUL.md` as a system message.
- The full conversation history (all messages from this session).
- The tool definitions for all registered tools.

The persona controls the agent's identity, tone, and any standing instructions. Edit it from the Soul screen.

## Multi-turn tool use

The agent loop runs up to 20 turns per chat request:

```
Turn 1: LLM → "I need to read the file"
        Tool: filesystem read → result
Turn 2: LLM → "Now I'll search for..."
        Tool: web_search → result
Turn 3: LLM → "Based on these, the answer is..."
        (no tool call → loop ends)
```

Most simple questions use 1-2 turns. Complex multi-step tasks can use the full 20.

## Read next
- [[02 - Tools Catalog]]
- [[05 - Approval System]]
- [[05 - Memory and Persona]]
