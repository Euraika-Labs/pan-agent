# Models and Providers

Pan-Agent supports 9 LLM providers via one OpenAI-compatible client.

## Provider matrix

| Provider | Type | Cost | Setup |
|---|---|---|---|
| OpenRouter | Aggregator | Pay-per-token | One key, 200+ models |
| OpenAI | Direct | Pay-per-token | Key from platform.openai.com |
| Anthropic | Direct | Pay-per-token | Key from console.anthropic.com |
| Regolo | EU-hosted | Pay-per-token | Key from dashboard.regolo.ai |
| Groq | Fast inference | Pay-per-token | Key from console.groq.com |
| Ollama | Local | Free | Install Ollama, run a model |
| LM Studio | Local | Free | Install LM Studio, start server |
| vLLM | Local/server | Free | Run `vllm serve <model>` |
| llama.cpp | Local | Free | Run `llama-server` |

## Recommended for...

| Use case | Pick |
|---|---|
| Best general experience | OpenRouter (variety + reliability) |
| Lowest cost | Local LLMs (Ollama with quantized models) |
| Fastest responses | Groq (purpose-built fast inference) |
| Privacy / EU data residency | Regolo or local LLMs |
| Latest Claude/GPT | OpenRouter or direct provider |
| Tool use / agentic | Anthropic Claude or GPT-4o (best tool-calling) |

## The Models screen

Lists all known models. Sources:
- **Synced from provider** — fetched via the provider's `/v1/models` endpoint.
- **Manually added** — entered by name without sync.

Click "Sync Models" (top right) to refresh from the active provider. This calls `POST /v1/models/sync` with the active provider/baseURL/key.

## Setting the active model

Click any model in the list to set it as active. This:
1. Updates `config.yaml` with the new `provider`, `default`, and `base_url`.
2. Refreshes the in-process `llm.Client` (drops the old one, creates a new one with the new model).
3. The next chat turn uses the new model.

You can also set the active model via Settings → Model section.

## Per-message model override

`POST /v1/chat/completions` accepts a `model` field that overrides the configured default for that single request:

```json
{
  "messages": [...],
  "model": "anthropic/claude-3.5-sonnet",
  "stream": true
}
```

The backend creates a one-off client with this model and the existing API key + base URL. The active model in `config.yaml` is unchanged.

This is useful for routing different requests to different models (e.g., cheap model for routine tasks, expensive model for hard problems).

## Local LLM setup

### Ollama

```bash
# Install
brew install ollama  # macOS
curl -fsSL https://ollama.com/install.sh | sh  # Linux

# Run a model
ollama run llama3.1

# Pan-Agent setup
# Setup wizard → Local LLM → Ollama preset → http://localhost:11434/v1
```

### LM Studio

1. Download from https://lmstudio.ai.
2. Search and download a model.
3. Open Local Server tab. Click Start Server.
4. In Pan-Agent setup: Local LLM → LM Studio preset → http://localhost:1234/v1.

### vLLM

```bash
pip install vllm
vllm serve meta-llama/Llama-3.1-8B-Instruct --port 8000
```

In Pan-Agent: Local LLM → vLLM preset → http://localhost:8000/v1.

### llama.cpp

```bash
./llama-server -m model.gguf --port 8080
```

In Pan-Agent: Local LLM → llama.cpp preset → http://localhost:8080/v1.

## Credential pool

The credential pool stores multiple API keys per provider, useful for rotating keys or load balancing.

Settings screen → Credential Pool section → add credentials per provider.

Currently the credential pool is read by the backend but not actively rotated. The agent uses the first key in the pool, falling back to the `.env` value if the pool is empty.

## Operator rule
Local LLMs running on your machine are NOT free in CPU/RAM cost. A 70B model needs 64GB+ RAM in 4-bit quantization. If your system grinds to a halt while chatting, you're running a model too big for your hardware — switch to a smaller one (8B or 3B) or use a cloud provider.

## Read next
- [[03 - LLM Client and Providers]]
- [[01 - Chat]]
