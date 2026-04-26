---
name: vscode-open-under-cursor
description: "Open the file path under the cursor in VS Code, using the editor's accessibility tree."
---

## When to use

- The user asks "open this file" / "jump to that path" / "open the
  file the cursor is on" while Visual Studio Code is the frontmost
  window.
- The cursor must be inside an editor pane; if it's in a panel
  (terminal, output, debug console) the path-extraction heuristic
  changes — see Failure modes.

## Steps

1. Read the active window:
     interact intent: "read active window", app: "Visual Studio Code"
   Returns ARIA-YAML for the VS Code window. The active editor is
   identifiable by `id: "editor.main"` — VS Code sets a stable id on
   the main editor's text_area role.

2. Inspect the focused node:
     - `focused:` path resolves to a `text_area` with `id: "editor.main"`
     - `value:` contains the editor's text content
     - The cursor offset is *not* directly in the tree on macOS AX,
       so we approximate via the focused line's content. Use the
       `focused-line` field if the scrape was done with
       `include_focused_line: true`; otherwise scan for the most
       recent `\n` before the rendered cursor coords.

3. Extract a path-shaped token from the cursor neighbourhood. A
   "path-shaped token" is a contiguous run of non-whitespace
   characters containing at least one `/` (or `\\` on Windows) and
   matching one of:
     - relative to repo root: `internal/foo/bar.go`
     - absolute:              `/Users/.../foo.go`
     - import path:           `github.com/user/repo/foo`
   The first form is the most common in code; if you find multiple
   candidates, prefer the one closest to the cursor.

4. Open via VS Code's quick-open palette:
     interact intent: "key", key: "cmd+p"     (macOS) or "ctrl+p" (others)
     interact intent: "type", text: "<extracted path>"
     interact intent: "key", key: "Return"

5. Confirm by re-scraping; the active editor's `value:` should now be
   the contents of the opened file. If quick-open auto-suggested a
   different (mis-spelled) match, report which file actually opened.

## Expected ARIA-YAML shape

```yaml
app: com.microsoft.VSCode
window:
  title: "interact.go — pan-agent"
focused: "tree.children[0].children[2].children[1]"
tree:
  role: window
  children:
    - role: tab_group
      name: "Editors"
      children:
        - role: tab
          name: "interact.go"
          value: "active"
          actions: [press]
    - role: text_area
      name: "Editor: interact.go"
      id: "editor.main"
      value: |
        package interact

        import (
          "context"
          "internal/tools/aria"   // <-- cursor here, "internal/tools/aria" is the candidate
        )
      actions: [press, set_value]
```

## Failure modes

- **Cursor in a panel, not the editor** → `id: "editor.main"` is not
  the focused node. Surface "the cursor isn't in an editor — click
  into a file first" rather than guessing which path to open.
- **No path-shaped token near cursor** → ambiguous request; ask the
  user "which path should I open?" with the 2-3 closest candidates.
- **Quick-open palette didn't appear** (a modal dialog has focus) →
  the Cmd+P key gets swallowed; scrape, detect the modal, dismiss it
  with Esc, retry.
- **Permission revoked** → `interact` returns the Accessibility deep-
  link via the error path, same as the other cookbook skills.

## Cost-shape

Read-only. Opening a file in VS Code is a UI navigation action; no
journal receipt produced.
