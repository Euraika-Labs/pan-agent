---
name: notion-find-doc
description: "Find a Notion page or database by query and return a clickable result list."
---

## When to use

- The user asks "find the doc about X" / "where's our runbook for Y"
  / "search Notion for Z" — anything that maps to a Notion title-
  search.
- The user names a specific page id or url — skip search, jump
  straight to `notion get_page`.
- Avoid this skill when:
  - `NOTION_API_KEY` isn't configured. The tool surfaces a clear
    error; relay it back with setup instructions, don't retry.
  - The user wants to *create* / *append* — those are write actions
    + need approval gates not in this slice.

## Steps

1. Build the search query. Strip filler words ("the", "doc about",
   "find me") and pass the substantive terms as `query`. If the
   user's phrasing implies a specific object type, set the filter:
     - "database" / "table" / "list of …" → `filter: "database"`
     - "page" / "note" / "doc" → `filter: "page"`
     - otherwise omit the filter (returns both).

2. Call:

       notion action: "search"
              query:  "<terms>"
              filter: "<page|database>"
              limit:  10

3. Format for the user. One result per line:
   `<title> — <id_short> (<object>)  ·  <url>`
   - Use the FIRST 8 hex chars of the id — full id is too noisy.
   - Group consecutive results with empty/missing titles under a
     "(untitled)" header so the user knows they exist without
     wall-of-text.

4. If the user's intent looks like "open the first match", call
   `notion get_page` with the top result's id and summarise its
   content. Otherwise end the turn with: "Want details on one of
   these? (paste the id)".

## Anti-patterns

- Don't paginate when `has_more=true` — surface the count + cursor
  string + suggest the user narrows the query.
- Don't dump the raw `page` JSON. Pull title + last-edited + a
  one-line excerpt of the first paragraph.
- Don't follow links in the page body silently. If the user's
  follow-up is "open the link in this page", confirm + then fetch
  via the browser tool.

## Failure modes

- 401 / 403 → token missing the integration grant for that page.
  Tell the user: "the integration isn't shared with this page —
  open the page in Notion → Share → Add this connection".
- Empty results → "Nothing matched. Wider terms? The query was: <q>"
- Rate limited → back off + retry once with a small jitter.
