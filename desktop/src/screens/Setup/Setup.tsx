import { useState } from "react";
import { fetchJSON } from "../../api";
import { PROVIDERS, LOCAL_PRESETS } from "../../constants";
import icon from "../../assets/icon.png";

type SetupProvider = (typeof PROVIDERS.setup)[number];

interface SetupProps {
  onComplete: () => void;
}

export default function Setup({ onComplete }: SetupProps) {
  const [provider, setProvider] = useState<SetupProvider | null>(null);
  const [apiKey, setApiKey] = useState("");
  const [keyVisible, setKeyVisible] = useState(false);
  const [baseUrl, setBaseUrl] = useState("");
  const [localPreset, setLocalPreset] = useState<string | null>(null);
  const [syncing, setSyncing] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit() {
    if (!provider) return;
    setSyncing(true);
    setError(null);

    try {
      // Step A: Save API key if needed
      if (provider.envKey && apiKey.trim()) {
        await fetchJSON("/v1/config", {
          method: "PUT",
          body: JSON.stringify({ env: { [provider.envKey]: apiKey.trim() } }),
        });
      }

      // Step B: Set model config
      const url =
        provider.id === "custom-openai"
          ? baseUrl
          : provider.id === "local"
            ? baseUrl || provider.baseUrl
            : provider.baseUrl;

      await fetchJSON("/v1/models", {
        method: "POST",
        body: JSON.stringify({
          provider: provider.configProvider,
          model: "",
          base_url: url,
        }),
      });

      // Step C: Sync models (non-blocking — fire and forget)
      fetchJSON("/v1/models/sync", {
        method: "POST",
        body: JSON.stringify({
          provider: provider.configProvider,
          base_url: url,
          api_key: apiKey.trim(),
        }),
      }).catch(() => {
        // Sync failure is non-fatal
      });

      onComplete();
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      if (msg.includes("401") || msg.includes("403")) {
        setError("Invalid API key. Please check your key and try again.");
      } else if (msg.includes("connection refused") || msg.includes("dial tcp")) {
        setError(
          "Could not reach the server. Please verify the URL and that the server is running.",
        );
      } else {
        setError(msg);
      }
      setSyncing(false);
    }
  }

  function handlePresetSelect(presetId: string) {
    const preset = LOCAL_PRESETS.find((p) => p.id === presetId);
    if (preset) {
      setLocalPreset(presetId);
      setBaseUrl(`http://localhost:${preset.port}/v1`);
    }
  }

  const showKeyInput = provider && provider.needsKey;
  const showLocalPresets = provider?.id === "local";
  const showCustomUrl = provider?.id === "custom-openai";
  const canSubmit =
    provider &&
    (!provider.needsKey || apiKey.trim() || provider.id === "custom-openai");

  return (
    <div className="setup-screen">
      <img
        src={icon}
        width={64}
        height={64}
        alt=""
        style={{ borderRadius: "var(--radius-md)" }}
      />
      <h1 className="setup-title">Welcome to Pan-Agent</h1>
      <p className="setup-subtitle">Choose your LLM provider to get started</p>

      <div className="setup-provider-grid">
        {PROVIDERS.setup.map((p) => (
          <button
            key={p.id}
            className={`setup-provider-card${provider?.id === p.id ? " selected" : ""}`}
            onClick={() => {
              setProvider(p);
              setApiKey("");
              setBaseUrl(p.baseUrl);
              setError(null);
              setLocalPreset(null);
            }}
          >
            <div className="setup-provider-name">{p.name}</div>
            <div className="setup-provider-desc">{p.desc}</div>
            {p.tag && <span className="setup-provider-tag">{p.tag}</span>}
          </button>
        ))}
      </div>

      {provider && (
        <div className="setup-form">
          {showKeyInput && (
            <>
              <label className="setup-label">API Key</label>
              <div className="setup-input-group">
                <input
                  className="input"
                  type={keyVisible ? "text" : "password"}
                  placeholder={provider.placeholder}
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                  autoFocus
                />
                <button
                  className="setup-toggle-visibility"
                  onClick={() => setKeyVisible(!keyVisible)}
                  type="button"
                >
                  {keyVisible ? "Hide" : "Show"}
                </button>
              </div>
              {provider.url && (
                <a
                  className="setup-link"
                  href={provider.url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  Get your API key &rarr;
                </a>
              )}
            </>
          )}

          {showLocalPresets && (
            <>
              <label className="setup-label">Server</label>
              <div className="setup-local-presets">
                {LOCAL_PRESETS.map((preset) => (
                  <button
                    key={preset.id}
                    className={`setup-local-preset${localPreset === preset.id ? " active" : ""}`}
                    onClick={() => handlePresetSelect(preset.id)}
                  >
                    {preset.name}
                  </button>
                ))}
              </div>
              <label className="setup-label" style={{ marginTop: 12 }}>
                Base URL
              </label>
              <input
                className="input"
                type="text"
                placeholder="http://localhost:1234/v1"
                value={baseUrl}
                onChange={(e) => setBaseUrl(e.target.value)}
              />
            </>
          )}

          {showCustomUrl && (
            <>
              <label className="setup-label">Base URL</label>
              <input
                className="input"
                type="text"
                placeholder="https://your-api.example.com/v1"
                value={baseUrl}
                onChange={(e) => setBaseUrl(e.target.value)}
                autoFocus
              />
              <label className="setup-label" style={{ marginTop: 12 }}>
                API Key <span className="setup-label-optional">(optional)</span>
              </label>
              <div className="setup-input-group">
                <input
                  className="input"
                  type={keyVisible ? "text" : "password"}
                  placeholder="sk-... (leave blank if no auth)"
                  value={apiKey}
                  onChange={(e) => setApiKey(e.target.value)}
                />
                <button
                  className="setup-toggle-visibility"
                  onClick={() => setKeyVisible(!keyVisible)}
                  type="button"
                >
                  {keyVisible ? "Hide" : "Show"}
                </button>
              </div>
            </>
          )}

          {error && <div className="setup-error">{error}</div>}

          <button
            className="setup-continue btn btn-primary"
            disabled={!canSubmit || syncing}
            onClick={handleSubmit}
          >
            {syncing ? "Setting up..." : "Continue"}
          </button>
        </div>
      )}
    </div>
  );
}
