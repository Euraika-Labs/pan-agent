# Profiles

Profiles let you maintain separate agent environments — different API keys, different memory, different persona, different installed skills.

## When to use profiles

- Separate work and personal contexts.
- Test a different provider without losing your default config.
- Give a friend access to your agent without exposing your real API key (rate-limit them via a separate provider account).
- Run different personas (creative writer, code reviewer, research assistant) in isolation.

## Create a profile

1. Open the Profiles screen.
2. Click "New Profile".
3. Enter a name (alphanumeric + hyphens/underscores).
4. Optionally check "Clone from current" to copy `.env` and `config.yaml` from the active profile.
5. Click Create.

## Switch profiles

Click any profile card to make it active. The frontend then reads/writes that profile's config.

**Important caveat**: switching profiles in the UI changes which profile the FRONTEND targets. The Go backend's active profile (set at server startup via `--profile`) does NOT change. The chat handler uses the backend's startup profile for persona, memory, and the LLM client.

To fully switch profiles: stop the agent, restart with `--profile <name>`, and refresh the desktop app.

## Delete a profile

Click the delete button on a profile card. Confirm in the modal.

The "default" profile cannot be deleted.

## What's in a profile

| File | Profile-specific? | Purpose |
|---|---|---|
| `.env` | Yes | API keys, tokens |
| `config.yaml` | Yes | Provider, model, platform toggles |
| `MEMORY.md` | Yes | Persistent agent memory |
| `USER.md` | Yes | User profile info |
| `SOUL.md` | Yes | Persona / system prompt |
| `skills/` | Yes | Installed skills |
| `state.db` | No | Sessions are global |
| `models.json` | No | Model library is global |
| `auth.json` | No | Credential pool is global |

Sessions are NOT scoped to profiles. A session created under "work" appears in the Sessions screen even when "personal" is active.

## Filesystem layout

```
<AgentHome>/                       ← default profile
├── .env
├── config.yaml
├── MEMORY.md
├── SOUL.md
├── skills/
└── profiles/
    ├── work/
    │   ├── .env
    │   ├── config.yaml
    │   └── ...
    └── personal/
        └── ...
```

## Profile cards

The Profiles screen shows a card per profile with:

- **Name** with avatar
- **Provider** + **Model** (e.g., "regolo / Llama-3.3-70B-Instruct")
- **Skills count**
- **Persona indicator** (whether SOUL.md is non-default)
- **Env indicator** (whether .env has any keys)
- **Gateway status** (currently always shows "off" — see [[02 - Gateway Bot Issues]])

## Path traversal protection

Profile names are validated against `^[a-zA-Z0-9][a-zA-Z0-9_-]*$`. Without this, a malicious frontend could call `DELETE /v1/config/profiles/../etc` and the backend's `os.RemoveAll` would resolve outside the profiles directory.

The validation is applied in both `CreateProfile` and `DeleteProfile`.

## Read next
- [[07 - Profile System]]
- [[04 - Configuration Reference]]
