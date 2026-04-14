# Tools Catalog

Pan-Agent ships with 20+ tools the agent can use during conversations. This is the complete catalog.

## Core tools

### terminal
**Approval: Yes**

Execute shell commands. The command runs in your default shell (cmd on Windows, bash on Unix). Output is captured and returned to the agent.

```json
{"command": "ls -la /tmp"}
```

### filesystem
**Approval: Yes**

Read, write, list, and search files. Supports operations: `read`, `write`, `list`, `search`, `delete`, `move`, `copy`.

```json
{"action": "read", "path": "C:\\src\\pan-agent\\go.mod"}
```

### code_execution
**Approval: Yes**

Run code snippets in sandboxed environments. Currently supports Python via the system `python3` interpreter.

### browser
**Approval: Yes**

Automate Chromium via the DevTools Protocol (go-rod). Can navigate, click, fill forms, screenshot, and extract content. Auto-downloads Chromium on first use.

### web_search
Search the web. Uses the API key from your `.env` (Exa, Tavily, Firecrawl, or Parallel).

```json
{"query": "Tauri v2 release notes 2025"}
```

## PC control tools (cross-platform)

### screenshot
Capture the screen. Returns a base64-encoded PNG.

| Platform | Backend |
|---|---|
| Windows | GDI |
| macOS | CoreGraphics |
| Linux | X11 (jezek/xgb) |

### keyboard
Simulate keyboard input.

```json
{"operation": "type", "text": "hello world"}
{"operation": "press", "key": "enter"}
{"operation": "hotkey", "modifiers": ["ctrl"], "key": "c"}
```

Supported keys: enter, tab, escape, backspace, delete, arrow keys, page up/down, home, end, insert, space, f1-f12, a-z, 0-9.

Modifiers: ctrl, alt, shift, win/super (Cmd on macOS).

### mouse
Move cursor and click.

```json
{"operation": "move", "x": 500, "y": 300}
{"operation": "click", "x": 500, "y": 300}
{"operation": "double_click", "x": 500, "y": 300}
{"operation": "right_click", "x": 500, "y": 300}
{"operation": "scroll", "x": 500, "y": 300, "delta": 240}
```

Coordinates are absolute screen pixels.

### window_manager
Manage top-level windows.

```json
{"operation": "list"}
{"operation": "find", "title": "Code"}
{"operation": "focus", "title": "Code"}
{"operation": "move", "title": "Code", "x": 100, "y": 100}
{"operation": "resize", "title": "Code", "width": 1280, "height": 800}
{"operation": "close", "title": "Code"}
```

The title match is case-insensitive substring.

| Platform | Backend |
|---|---|
| Windows | EnumWindows + SetForegroundWindow + MoveWindow |
| macOS | CGWindowListCopyWindowInfo + osascript |
| Linux | EWMH atoms + ConfigureWindow |

macOS requires Accessibility permission for focus/move/resize/close.

### ocr
Extract text from a screenshot. Uses a vision LLM (not Tesseract). Requires your active LLM provider to support vision.

```json
{}
```

## AI tools

### vision
Analyze an image with the active vision LLM. Pass a path or base64 data URI.

### image_gen
Generate an image via the configured image API (FAL.ai, etc.).

### tts
Text-to-speech synthesis. Returns audio bytes.

### clarify
Ask the user a clarifying question. The chat UI surfaces this prominently.

### delegation
Delegate a subtask to a specialized sub-agent (a separate LLM call with a focused prompt).

### moa
Mixture of Agents. Query multiple models in parallel, then synthesize.

## Utility tools

### memory_tool
Read or write the agent's persistent memory. The agent uses this to remember facts between sessions.

```json
{"operation": "add", "content": "User prefers concise responses."}
```

### session_search
Full-text search across past sessions (FTS5).

### todo
Manage task lists. Add, complete, list todos.

### cron_tool
Schedule recurring tasks. The agent can create cron jobs that run periodically.

## Toggling tools on/off

The Tools screen lets you disable specific tools. Disabled tools won't appear in the LLM's tool list — the agent literally won't know they exist.

This is useful for:
- Read-only profiles (disable terminal + filesystem + code_execution + browser)
- Cost control (disable web_search to avoid API costs)
- Focus (disable PC control to keep the agent in chat-only mode)

## Read next
- [[04 - Tool Registry]]
- [[05 - Approval System]]
- [[03 - Cross-Platform Tool Architecture]]
