---
name: jira-my-issues
description: "Show the current user's open Jira issues, grouped by project and status."
---

## When to use

- The user asks "what's on my plate" / "my open Jira" / "what am I
  working on" / "stand-up summary" — anything that maps to "my
  issues that aren't Done".
- Avoid this skill when:
  - The user names a specific project or labels — use
    `jira-search-by-jql` (write your own; this skill is the
    "default standup" view).
  - `JIRA_HOST` + auth env vars aren't set. The tool surfaces a
    clear error; relay it back with setup instructions.

## Steps

1. Confirm the user reference. The stock JQL `assignee = currentUser()`
   maps to whoever the JIRA_EMAIL or JIRA_BEARER token resolves to.
   Verify by calling once:

       jira action: "myself"

   The `displayName` from the response is what you'll show as a
   header. Skip this probe if you've already run it earlier in the
   conversation — it's just a sanity check.

2. Run the search:

       jira action: "search"
            jql:    "assignee = currentUser() AND statusCategory != Done ORDER BY updated DESC"
            limit:  25
            fields: "summary,status,priority,project,updated"

3. Format the output. Group by `project.name`, then by `status.name`
   within each project:

       <ProjectName>  (<count>)
         In Progress
           PAN-42  Fix the bug · High · 2d ago
           PAN-44  Refactor X · Medium · today
         To Do
           PAN-50  …

   - Show `priority` only when it's High or Highest (the rest are
     noise for a quick standup view).
   - Format `updated` as a relative time ("2d ago", "today", "1w ago")
     — Jira returns ISO 8601 strings.
   - Always include the issue URL on the line for any High/Highest
     priority row so the user can click straight through.

4. Offer follow-ups:
   - "Want the full description of <key>?" → `jira get_issue`.
   - "Drop one to Done?" → write action; confirm + use a separate
     write skill (not in this cookbook today).

## Anti-patterns

- Don't include `statusCategory = Done` issues. Standup views are
  about WIP, not history.
- Don't paginate. 25 items is plenty for a standup; if the user
  has more, say "+N older — narrow the JQL?" and stop.
- Don't math on `updated` timestamps. Jira returns ISO 8601 in the
  user's TZ — pass through and let the rendering layer format
  the relative time.

## Failure modes

- 401 → bad credentials. Suggest re-running `pan-agent doctor`.
- 400 with "JQL parse error" → the assignee/statusCategory query
  may not work on older Server installs. Tell the user + suggest
  trying `assignee = "<your-email>"` instead.
- Empty list → "All clear. Nothing assigned in flight." (Don't
  invent items.)
