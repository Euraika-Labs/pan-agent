# Setup Wizard Issues

This runbook covers problems with the first-run Setup Wizard.

## Symptom: Setup wizard keeps appearing

After completing setup, every relaunch shows the wizard again.

### Diagnosis

The wizard appears when `Layout.tsx` determines no LLM provider is configured. Check what the API returns:

```bash
curl -sf http://localhost:8642/v1/config | jq '.env, .model'
```

### Causes and fixes

**Cause 1: Setup didn't actually save the key.**

If `.env` shows the key but `.env`'s permissions or path are wrong, it's invisible to the backend.

```bash
# Check the file
cat "$LOCALAPPDATA/pan-agent/.env"  # Windows (in Git Bash)
cat ~/.local/share/pan-agent/.env    # Linux
cat ~/Library/Application\ Support/pan-agent/.env  # macOS

# Check permissions
ls -la <AgentHome>/.env  # should be 0600
```

If the file is missing, the wizard's `PUT /v1/config` failed silently. Check the backend's stderr for errors during your last setup attempt.

**Cause 2: API key set but model.baseUrl is empty AND model.provider is "custom".**

The wizard logic in `Layout.tsx`:
```typescript
const hasKey = ["OPENROUTER_API_KEY", "OPENAI_API_KEY",
                "ANTHROPIC_API_KEY", "REGOLO_API_KEY"]
  .some(k => env[k]?.trim());
const hasCustomUrl = cfg.model.baseUrl && cfg.model.provider === "custom";
setSetupRequired(!hasKey && !hasCustomUrl);
```

If you're using Local LLM and the wizard set `provider: "custom"` but no `baseUrl`, the wizard re-appears. Manually set `base_url` in `<AgentHome>/config.yaml`.

**Cause 3: You're using a profile but the wizard wrote to "default".**

The Setup wizard always writes to the active profile (the one the server was started with). If you launched `pan-agent serve --profile work` but the wizard's PUT didn't include `profile: "work"` in the body, the key landed in the default profile.

Restart the server with `--profile default`, or move the key into the right profile's `.env`.

## Symptom: Setup wizard never appears even though no key is set

You expect to see the wizard but the app goes straight to Chat.

### Diagnosis

```bash
curl -sf http://localhost:8642/v1/config | jq '.env, .model'
```

### Causes

**Cause 1: A different env var is set.**

The wizard checks for `OPENROUTER_API_KEY`, `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `REGOLO_API_KEY`. If you have `GROQ_API_KEY` or `HF_TOKEN` or `API_KEY`, the agent CAN run but the wizard doesn't show.

This is intentional — you have a key, just for an unexpected provider. Open Settings to configure the correct provider, or remove the key to trigger the wizard.

**Cause 2: A custom base URL is set.**

If `config.yaml` has `provider: "custom"` and `base_url: "..."`, the wizard considers you set up even with no API key (e.g., for a local LLM with no auth).

**Cause 3: The `OPENAI_API_KEY` env var is set in your shell.**

The Go backend's API key resolution falls back to `os.Getenv("OPENAI_API_KEY")`. If your shell has `OPENAI_API_KEY` exported, the agent will use it without needing a `.env` file.

```bash
unset OPENAI_API_KEY  # then restart pan-agent
```

## Symptom: "Could not reach the server" during setup

The Continue button shows "Setting up..." then the error.

### Causes

**Cause 1: For Local LLM, your local server isn't running.**

The wizard's `POST /v1/models/sync` calls your local LLM's `/v1/models` endpoint. If LM Studio / Ollama / vLLM isn't running on the configured port, you get a connection refused.

The actual setup still succeeds — model sync is non-blocking and runs in the background. You can dismiss the error and start chatting. Or click Skip to bypass.

**Cause 2: For OpenRouter / OpenAI / etc., DNS or network is broken.**

```bash
# Test from your shell
curl -sf https://openrouter.ai/api/v1/models -H "Authorization: Bearer <your-key>" | jq '.data | length'
```

If this fails, your machine can't reach the provider — fix your network, then retry the wizard.

## Symptom: "Invalid API key" error

### Causes

**Cause 1: Wrong key format.**

Each provider has a specific prefix:
- OpenRouter: `sk-or-v1-...`
- OpenAI: `sk-...`
- Anthropic: `sk-ant-...`
- Regolo: `sk-...`

Pasting an OpenAI key when OpenRouter is selected will return 401.

**Cause 2: Key was revoked.**

Test the key directly with the provider's API. If it's revoked, generate a new one.

**Cause 3: Trailing whitespace.**

Some providers reject keys with trailing newlines. The wizard does `apiKey.trim()` but if a paste included a hidden character, that might still cause issues.

## Read next
- [[01 - Installation and First Run]]
- [[04 - Configuration Reference]]
