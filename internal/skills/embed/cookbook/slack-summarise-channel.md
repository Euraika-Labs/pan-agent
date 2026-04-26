---
name: slack-summarise-channel
description: "Summarise the active Slack channel without screenshots, using the OS accessibility tree."
---

## When to use

- The user asks "what's happening in #channel" / "summarise Slack" /
  "catch me up on Slack" while Slack is the frontmost window.
- Avoid this skill when Slack is not visible — you'll get a stale or
  empty tree. Run `interact intent: "find window", app: "Slack"` first
  if unsure.

## Steps

1. Call `interact` with:
     intent:  "read active window"
     app:     "Slack"
   The tool returns ARIA-YAML describing the visible Slack window.

2. From the returned tree, locate the messages list:
     - role:  list
     - name:  "Messages"
   Each `list_item` child is one message; its `name:` is the author
   and its `value:` is the message body. The channel name is at
   `tree.children[0].name` (the sidebar selection).

3. Walk the most-recent N list_items (typically the last 50 — Slack
   virtualises older messages out of the tree, so what you see is
   what's loaded). Concatenate `<author>: <body>` lines.

4. Summarise in 3–5 bullets: who's discussing what, any open
   questions, decisions made.

## Expected ARIA-YAML shape

```yaml
app: com.tinyspeck.slackmacgap   # or "Slack" on Linux/Windows
window:
  title: "#general | Acme Slack"
tree:
  role: window
  children:
    - role: list
      name: "Channels"          # sidebar
      children: [...]
    - role: list
      name: "Messages"          # main pane
      children:
        - role: list_item
          name: "Alice"          # author
          value: "Lunch?"        # body
        - role: list_item
          name: "Bob"
          value: "👍"
```

## Failure modes

- **Slack not frontmost** → `interact` returns layer=`unsupported` with
  an error string. Surface "Open Slack first, then ask again."
- **Permission revoked** (macOS: Accessibility / per-app Automation)
  → `interact` returns layer=`unsupported`, error=`accessibility_revoked`
  or app-specific `automation_denied`. Open the Setup → Permissions
  step via the deep-link the tool returns.
- **Empty messages list** → channel is brand-new or just loaded;
  scroll up and retry, or fall back to "Slack is open but no messages
  loaded yet."

## Cost-shape

This is a read-only skill — no journal receipts produced. Token cost is
limited to the ARIA-YAML tree size (typically 2-4k tokens for an active
channel) plus the summary output.
