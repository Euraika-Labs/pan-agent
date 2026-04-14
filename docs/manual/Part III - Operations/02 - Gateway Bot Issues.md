# Gateway Bot Issues

This runbook covers problems with the messaging gateway bots (Telegram, Discord, Slack).

## Symptom: Telegram bot doesn't respond

You message the bot but it stays silent.

### Diagnosis

```bash
# Check the gateway is running
curl -sf http://localhost:8642/v1/health | jq .gateway

# Check the platform is enabled
curl -sf http://localhost:8642/v1/health | jq '.platformEnabled.telegram'

# Check the bot token is configured
curl -sf http://localhost:8642/v1/health | jq '.env.TELEGRAM_BOT_TOKEN'
```

### Causes

**Cause 1: Gateway not started.**

`/v1/health` returns `{"gateway": false}`. Click "Start Gateway" in the Gateway screen, or:

```bash
curl -sf -X POST http://localhost:8642/v1/health/gateway/start | jq
```

**Cause 2: Platform not enabled.**

The toggle on the Gateway screen wasn't switched on. Click it, then click Start Gateway again. The bots only start for platforms with `enabled: true` in `config.yaml`.

**Cause 3: Token is wrong.**

Check the backend stderr — when starting the bot, telego will fail with an authorization error if the token is invalid:

```
[telegram] start error: telegram: create bot: ...
```

Test the token with curl:

```bash
TOKEN=123456:ABC-DEF
curl -sf "https://api.telegram.org/bot${TOKEN}/getMe" | jq
```

If this returns `{"ok":false,"error_code":401}`, the token is wrong or revoked.

**Cause 4: TELEGRAM_ALLOWED_USERS is set and doesn't include you.**

If `TELEGRAM_ALLOWED_USERS` is non-empty, only listed user IDs can interact. Other users get "Access denied."

Get your Telegram user ID by talking to `@userinfobot` on Telegram. Add it to `TELEGRAM_ALLOWED_USERS` (comma-separated). Restart the gateway.

**Cause 5: Bot was added to a group but doesn't have message access.**

Telegram bots in groups only see commands (messages starting with `/`) by default. To see all messages, talk to `@BotFather` → `/setprivacy` → choose your bot → Disable. Then re-add the bot to the group.

For 1-on-1 chats with the bot, this isn't an issue — the bot sees everything.

## Symptom: Discord bot connects but ignores messages

Bot shows online in your server but doesn't reply.

### Diagnosis

The most common cause: missing Message Content intent.

### Fix

1. Go to https://discord.com/developers/applications.
2. Pick your application.
3. Bot tab → scroll to "Privileged Gateway Intents".
4. Toggle on "Message Content Intent".
5. Save.
6. Restart the gateway in Pan-Agent (Stop, then Start).

The Discord library subscribes to:
```go
discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentMessageContent
```

Without the privileged intent, the WebSocket connection succeeds but the `MessageCreate` events arrive with empty content.

## Symptom: Slack bot fails to start

Backend stderr shows: `slack: SLACK_APP_TOKEN is required for Socket Mode`.

### Fix

You set `SLACK_BOT_TOKEN` (xoxb-) but not `SLACK_APP_TOKEN` (xapp-). Socket Mode needs both.

1. Go to https://api.slack.com/apps → pick your app.
2. Basic Information → App-Level Tokens → Generate Token and Scopes.
3. Add the `connections:write` scope.
4. Copy the `xapp-...` token.
5. Paste into Pan-Agent's Gateway screen as `SLACK_APP_TOKEN`.
6. Restart the gateway.

## Symptom: Slack bot starts but doesn't see messages

The bot is online but doesn't respond.

### Diagnosis

Slack apps must subscribe to specific events.

### Fix

1. https://api.slack.com/apps → your app → Event Subscriptions.
2. Enable Events.
3. Subscribe to bot events: `message.channels`, `message.groups`, `message.im`, `message.mpim`.
4. Save changes.
5. OAuth & Permissions → reinstall the app to your workspace if scopes changed.
6. Restart the gateway in Pan-Agent.

Also verify the bot is actually invited to the channel where you're messaging:

```
/invite @your-bot-name
```

## Symptom: Bot replies are truncated

Long agent responses get cut off.

### Cause

Each platform has a max message length:
- Telegram: 4096
- Discord: 2000
- Slack: 4000 (technical limit is 40000 but readability suffers)

Pan-Agent splits long responses into multiple sequential messages. If you only see the first chunk, the bot might be rate-limited.

### Fix

Wait — the splits send sequentially. For very long responses, this can take seconds.

If you consistently see truncation without continuation, check the backend stderr for send errors.

## Symptom: Bot responds with "Error: ..."

This means `runAgentLoop` returned an error.

Common errors:

| Error | Likely cause |
|---|---|
| `no LLM client configured` | Setup wizard wasn't completed for the active profile |
| `LLM error: 401 ...` | API key is invalid for the configured provider |
| `LLM error: 429 ...` | Rate limit hit at the LLM provider |
| `LLM error: dial tcp: connection refused` | Local LLM server isn't running |
| `context canceled` | Gateway was stopped mid-message |

## Symptom: Gateway "running" but no bots are actually running

`GET /v1/health` shows `gateway: true` after server restart.

### Cause

The `gatewayRunning` bool is in-memory only. It does NOT survive restart. After restart, the bool resets to `false` — but if you didn't restart and the gateway was running before, it's still true (and bots are still running).

Edge case: if the agent crashed and was restarted by Tauri's sidecar mechanism, the bool resets but the previous bot processes might be orphaned (unlikely with goroutines but possible with subprocesses). Stop and restart the gateway to be sure.

## Read next
- [[08 - Messaging Gateway Bots]]
- [[00 - Troubleshooting Index]]
