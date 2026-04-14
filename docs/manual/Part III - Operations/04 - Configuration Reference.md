# Configuration Reference

This is the canonical reference for all configuration files and environment variables Pan-Agent reads.

## Files

| File | Path (per-profile) | Purpose |
|---|---|---|
| `.env` | `<ProfileHome>/.env` | API keys + tokens |
| `config.yaml` | `<ProfileHome>/config.yaml` | Provider, model, base URL, platform toggles |
| `MEMORY.md` | `<ProfileHome>/MEMORY.md` | Persistent agent memory |
| `USER.md` | `<ProfileHome>/USER.md` | User profile information |
| `SOUL.md` | `<ProfileHome>/SOUL.md` | Persona / system prompt |
| `state.db` | `<AgentHome>/state.db` | SQLite database (global) |
| `models.json` | `<AgentHome>/models.json` | Cached model library (global) |
| `auth.json` | `<AgentHome>/auth.json` | Credential pool (global) |
| `cron/jobs.json` | `<AgentHome>/cron/jobs.json` | Scheduled tasks (global) |

## .env environment variables

### LLM provider keys

| Variable | Used for | Required for |
|---|---|---|
| `OPENAI_API_KEY` | OpenAI direct or fallback | OpenAI provider |
| `ANTHROPIC_API_KEY` | Anthropic direct | (not currently routed) |
| `REGOLO_API_KEY` | Regolo.ai | Regolo provider |
| `OPENROUTER_API_KEY` | OpenRouter | OpenRouter provider |
| `GROQ_API_KEY` | Groq | Groq provider |
| `GLM_API_KEY` | z.ai / GLM | Custom |
| `KIMI_API_KEY` | Moonshot Kimi | Custom |
| `MINIMAX_API_KEY` | MiniMax (global) | Custom |
| `MINIMAX_CN_API_KEY` | MiniMax (China endpoint) | Custom |
| `OPENCODE_ZEN_API_KEY` | OpenCode Zen | Custom |
| `OPENCODE_GO_API_KEY` | OpenCode Go | Custom |
| `HF_TOKEN` | Hugging Face Inference | Custom |
| `API_KEY` | Generic fallback | Last resort |

### Tool keys

| Variable | Used by tool |
|---|---|
| `EXA_API_KEY` | `web_search` (Exa) |
| `PARALLEL_API_KEY` | `web_search` (Parallel) |
| `TAVILY_API_KEY` | `web_search` (Tavily) |
| `FIRECRAWL_API_KEY` | `web_search` (Firecrawl) |
| `FAL_KEY` | `image_gen` (FAL.ai) |
| `HONCHO_API_KEY` | Cross-session AI memory |
| `BROWSERBASE_API_KEY` + `BROWSERBASE_PROJECT_ID` | `browser` (cloud) |
| `VOICE_TOOLS_OPENAI_KEY` | `tts` and Whisper STT |
| `TINKER_API_KEY` | RL training |
| `WANDB_API_KEY` | Experiment tracking |

### Messaging gateway tokens

| Variable | Required |
|---|---|
| `TELEGRAM_BOT_TOKEN` | Telegram bot |
| `TELEGRAM_ALLOWED_USERS` | Optional. Comma-separated user IDs. Empty = no restriction. |
| `DISCORD_BOT_TOKEN` | Discord bot |
| `SLACK_BOT_TOKEN` | Slack bot (xoxb-) |
| `SLACK_APP_TOKEN` | Slack Socket Mode (xapp-) |

## config.yaml fields

```yaml
# Provider name (UI label)
provider: "regolo"

# Default model identifier (provider-specific)
default: "Llama-3.3-70B-Instruct"

# OpenAI-compatible base URL
base_url: "https://api.regolo.ai/v1"

# Whether to use streaming responses
streaming: true

# Per-platform toggles (matches GATEWAY_PLATFORMS in constants.ts)
platforms:
  telegram:
    enabled: true
  discord:
    enabled: false
  slack:
    enabled: false
  whatsapp:
    enabled: false
  signal:
    enabled: false

# Smart routing (future feature, currently inactive)
smart_model_routing:
  enabled: false
```

The YAML is parsed with regex (no full YAML library). Only the keys above are read; comments and additional keys are preserved when writing.

## auth.json structure

Stored at `<AgentHome>/auth.json` (global, not per-profile):

```json
{
  "credential_pool": {
    "openrouter": [
      {"key": "sk-or-aaa", "label": "Work account"},
      {"key": "sk-or-bbb", "label": "Personal"}
    ],
    "anthropic": [
      {"key": "sk-ant-xxx", "label": "Default"}
    ]
  }
}
```

Used for load balancing or rotating API keys across multiple accounts.

## cron/jobs.json structure

```json
{
  "jobs": [
    {
      "id": "uuid-...",
      "name": "Daily report",
      "schedule": "0 9 * * *",
      "prompt": "Summarize yesterday's activity from my email."
    }
  ]
}
```

## API key resolution order

When the LLM client needs an API key, the backend checks (in order):

1. `REGOLO_API_KEY` from profile `.env`
2. `OPENAI_API_KEY` from profile `.env`
3. `API_KEY` from profile `.env`
4. `OPENAI_API_KEY` from system environment (`os.Getenv`)

This is implemented in `internal/gateway/server.go:refreshLLMClient`.

## Default ports

| Service | Port |
|---|---|
| Pan-Agent API | 8642 |
| Vite dev server (desktop) | 5173 |
| LM Studio (default) | 1234 |
| Ollama (default) | 11434 |
| vLLM (default) | 8000 |
| llama.cpp (default) | 8080 |
| Claw3D / pan-office adapter | varies |

## Path resolution

| Platform | AgentHome |
|---|---|
| Windows | `%LOCALAPPDATA%\pan-agent` |
| macOS | `~/Library/Application Support/pan-agent` |
| Linux | `~/.local/share/pan-agent` (or `$XDG_DATA_HOME/pan-agent` if set) |

## CSP

The Tauri webview enforces this CSP from `tauri.conf.json`:

```
default-src 'self';
connect-src 'self' http://localhost:8642;
style-src 'self' 'unsafe-inline';
img-src 'self' data:;
script-src 'self'
```

This blocks the webview from talking to anywhere except the local Pan-Agent backend.

## Read next
- [[04 - Data and Storage]]
- [[05 - Security Model]]
