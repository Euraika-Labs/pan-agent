---
name: reminders-create
description: "Create a reminder in macOS Reminders.app via the accessibility tree, no screenshots."
---

## When to use

- The user says "remind me to X tomorrow at 9am" or any natural-
  language reminder request, AND the platform is macOS (Reminders.app
  is Apple-only).
- On Windows or Linux, fall back to `cron_schedule` or
  `task_create` with a notification step.

## Steps

1. Open the app:
     interact intent: "open app", app: "Reminders"
   Wait briefly for the window to come up; subsequent steps will
   re-scrape if needed.

2. Read the active window:
     interact intent: "read active window", app: "Reminders"
   Returns ARIA-YAML for the Reminders window. Locate the input row:
     - role:  text_field
     - name:  "New Reminder"
   It usually sits at the top of the active list pane.

3. Type the reminder text:
     interact intent: "type", app: "Reminders", text: "<reminder body>"
   This sets `value:` on the focused text_field.

4. Press Return:
     interact intent: "key", key: "Return"
   This commits the reminder. macOS auto-parses date/time from natural
   language ("tomorrow 9am" → schedules for tomorrow 09:00 local).

5. Confirm by re-scraping; the reminder should now appear as a
   `list_item` in the active list with the typed body and inferred
   due-date.

## Expected ARIA-YAML shape

```yaml
app: com.apple.reminders
window:
  title: "Reminders"
focused: "tree.children[1].children[0]"   # the New Reminder text_field
tree:
  role: window
  children:
    - role: list                          # sidebar
      name: "Lists"
      children:
        - role: list_item
          name: "Reminders"
          value: "selected"
    - role: text_area
      name: "Reminder list"
      children:
        - role: text_field
          name: "New Reminder"
          actions: [set_value, press]
        - role: list_item                  # newly created reminder
          name: "Buy milk"
          value: "Tomorrow at 9 AM"
```

## Failure modes

- **Reminders.app not installed** (rare — comes pre-installed on macOS
  but can be removed) → `interact intent: "open app"` returns an
  error; surface "Reminders.app isn't installed on this Mac."
- **Permission revoked** (Accessibility or Automation/Reminders) →
  `interact` returns the deep-link to the right Settings pane.
- **Date parsing missed** (macOS doesn't recognise the phrasing) →
  the reminder will be created without a due-date; scrape the
  resulting list_item, see its `value:` is empty, and re-prompt the
  user with a more explicit time format.

## Cost-shape

Produces one `KindFSWrite`-equivalent journal receipt via the
Reminders SQLite store. Reversible by deleting the resulting
list_item via:
  interact intent: "delete", app: "Reminders", text: "<reminder body>"
which the runtime maps to a focus + Cmd+Backspace sequence.
