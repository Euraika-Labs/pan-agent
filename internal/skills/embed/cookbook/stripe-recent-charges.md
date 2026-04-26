---
name: stripe-recent-charges
description: "Show the most recent Stripe charges with status and dashboard links."
---

## When to use

- The user asks "what came in today on Stripe" / "show recent
  payments" / "any failed charges this week" — anything that maps
  to a read-only Stripe lookup.
- Avoid this skill when:
  - The user wants to refund / capture / cancel a charge. Those
    are write actions and need the existing approval gate; refer
    them to a write-side skill (none ship in the cookbook today;
    create one before doing the action).
  - `STRIPE_API_KEY` (or `STRIPE_TEST_API_KEY` for test mode)
    isn't configured. The tool will surface a clear error; relay
    it back to the user with setup instructions instead of
    retrying.

## Steps

1. Pick the right mode. If the user said "test" / "sandbox" /
   "fake" / mentioned a test-only product, set `test_mode: true`.
   Otherwise default to live.

2. List charges:

       stripe action: "list"
              limit: 10        # raise to 25 if user wants "today's"
              test_mode: <see step 1>

   Returns JSON of the form:
     - `charges`: array of `{id, amount, currency, status, created, url}`
     - `has_more`: pagination flag

3. Format for the user:
   - One charge per line:
       `<status>  <currency> <amount/100>  ·  <id>  ·  <created date>`
   - Group by status when 3+ rows have the same value (helps the
     "any failed?" intent quickly answer "yes/no, here's how many").
   - Always include the dashboard URL on the row of any
     `failed` / `disputed` / `requires_action` charge so the user
     can click through.
   - Trim to the most recent 5 unless the user specifically asked
     for more — listing 10+ in chat is noisy.

4. Offer drill-down. End with:
   "Want details on one of these? (paste the id)"
   On a follow-up id, call:

       stripe action: "get"
              id:     "<charge-id>"

   Format the response as a short paragraph, not raw JSON.

## Anti-patterns

- Don't paginate automatically. `has_more=true` is signal that
  there's more data, not an instruction to crawl. Tell the user
  "+N older — paginate?" and wait.
- Don't reformat amounts as floats from arithmetic. Stripe
  amounts are integer minor-units (cents); divide by 100 only
  for display, never for math the user might rely on.
- Don't include the dashboard URL inline for every successful
  charge — that's a wall of links nobody reads. Reserve URLs for
  the rows that need a click.

## Failure modes

- Stripe returns 401 (bad key) → relay verbatim + suggest
  `pan-agent doctor` to check key resolution.
- Network timeout → suggest retrying in a moment; don't hammer.
- Empty `charges` array → "No recent charges in <mode> mode" —
  don't pretend there are some.
