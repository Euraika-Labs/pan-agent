# Schedules

Schedules let you run recurring agent tasks on a cron schedule.

## Use cases

- Daily morning briefing: "Summarize my unread email and the day's calendar."
- Hourly status check: "Run pan-agent doctor and alert me if any check fails."
- Weekly review: "Look through my session history and identify recurring topics."
- Periodic ingestion: "Fetch the RSS feed and summarize new entries."

## Create a schedule

Schedules screen → "New Schedule" → fill in:

| Field | Example | Notes |
|---|---|---|
| Name | "Morning briefing" | Display label |
| Schedule | `0 9 * * *` | Standard cron expression (5 fields) |
| Prompt | "Summarize unread email and today's calendar" | What the agent should do |

Click Create.

## Cron syntax

Standard 5-field cron:

```
* * * * *
| | | | |
| | | | └── day of week (0-7, Sun = 0 or 7)
| | | └──── month (1-12)
| | └────── day of month (1-31)
| └──────── hour (0-23)
└────────── minute (0-59)
```

Examples:

| Expression | Meaning |
|---|---|
| `0 9 * * *` | Every day at 09:00 |
| `0 9 * * 1-5` | Weekdays at 09:00 |
| `0 */3 * * *` | Every 3 hours |
| `30 6,18 * * *` | 06:30 and 18:30 every day |
| `0 0 1 * *` | First of every month, midnight |

## How a scheduled job runs

When the cron tick fires:

1. The cron worker creates a new chat session.
2. Sends the configured prompt as a user message.
3. Lets the agent loop run with full tool access (auto-approves dangerous tools).
4. Saves the session for later review.

You'll see scheduled job sessions in the Sessions screen alongside your interactive chats.

## Storage

Cron jobs are stored at `<AgentHome>/cron/jobs.json`. The format:

```json
{
  "jobs": [
    {
      "id": "uuid-...",
      "name": "Morning briefing",
      "schedule": "0 9 * * *",
      "prompt": "Summarize unread email and today's calendar"
    }
  ]
}
```

## Delete a schedule

Schedules screen → click the delete button on a row. Or via API:

```bash
curl -X DELETE http://localhost:8642/v1/cron/<job-id>
```

## When the agent isn't running

Cron jobs only fire while `pan-agent serve` is running. If you close the desktop app or stop the headless server, scheduled jobs are skipped — they don't queue or fire later.

For 24/7 scheduling, run `pan-agent serve` as a system service:

- **Linux**: systemd user unit
- **macOS**: launchd plist
- **Windows**: Task Scheduler with "At startup" trigger running `pan-agent serve`

## Operator rule
Bot conversations and cron jobs both auto-approve dangerous tools. Be careful what prompts you schedule — "delete temp files" sounds harmless but "delete files that look temporary" with the agent's interpretation could lose work.

## Read next
- [[02 - Tools Catalog]]
- [[01 - Chat]]
