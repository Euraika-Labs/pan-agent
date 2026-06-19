---
name: clipboard-summarise
description: "Read the system clipboard and produce a short summary the user can paste back."
---

## When to use

- The user says "summarise this" / "tl;dr" / "what's on my clipboard"
  AND has already copied content (URL, article, message thread, etc.)
  outside the agent.
- The user pastes a long block into chat — already in the
  conversation, no clipboard read needed; skip this skill.
- Cross-platform: macOS, Linux, Windows. The clipboard tool handles
  the platform split.

## Steps

1. Read the clipboard:

       clipboard intent: "read"

   Returns one of:
     - text content (URLs, plain text, markdown, code snippets)
     - "<empty>" → tell the user nothing is on the clipboard and
       stop. Do NOT fabricate a summary.

2. Detect the content shape from the first non-blank line:
     - `http://` or `https://` prefix → URL. Skip ahead to step 3.
     - `<html` / `<!DOCTYPE` → HTML page. Skip ahead to step 3.
     - looks like code (curly braces, `def `, `func `, `import`,
       `#include`, etc.) → describe what the code does + flag
       anything obviously broken. Stop after this step.
     - else → treat as prose; produce a 3-bullet summary.

3. **URL or HTML clipboard:** ask the user before fetching. The
   clipboard contents may be private. Phrase the confirmation as:
   "I see a link to <host>. Should I fetch it and summarise?"
   On approval, call `browser intent: "open"` with the URL +
   `browser intent: "get-text"` to read the rendered DOM. Continue
   with step 4.

4. Summarise to **at most 5 bullets**:
   - One sentence per bullet.
   - Lead with the most-actionable point.
   - End with any explicit ask the source contains (e.g. "needs
     review by Friday" → surface as bullet 5).
   - Skip the bullet count when the source is shorter than ~4
     paragraphs; a 1–2 line response reads better than a forced
     list.

5. Offer a follow-up:
   - For a URL: "Want me to save this to your reading list?" (only
     if a reading-list skill / saved-pages tool is installed).
   - For prose / a message thread: "Draft a reply?" → switches to
     a separate response-drafting skill.
   - Otherwise: end the turn.

## Anti-patterns

- Don't paraphrase a URL without fetching it. The clipboard
  carries only the URL; without a fetch you're inventing content.
- Don't summarise a passphrase or 32+ char hex blob — those are
  almost certainly secrets the user copied for paste, not for
  summarisation. Decline politely + suggest they use a password
  manager.

## Failure modes

- Clipboard tool unavailable on this platform → say so, suggest
  the user paste the content into chat directly.
- HTML parser returns gibberish (paywall, JS-only page) → tell
  the user the page didn't render readable text and stop. Don't
  guess from the URL alone.
