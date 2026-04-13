import { useState, useEffect, useRef, useCallback } from "react";
import { fetchJSON } from "../../api";
import { SETTINGS_SECTIONS, PROVIDERS, THEME_OPTIONS } from "../../constants";

// ---------------------------------------------------------------------------
// Theme hook (mirrors ThemeProvider from Electron build)
// ---------------------------------------------------------------------------
function useTheme(): {
  theme: "system" | "light" | "dark";
  setTheme: (t: "system" | "light" | "dark") => void;
} {
  const [theme, setThemeState] = useState<"system" | "light" | "dark">(
    () =>
      (localStorage.getItem("hermes-theme") as "system" | "light" | "dark") ||
      "system",
  );

  function setTheme(t: "system" | "light" | "dark"): void {
    localStorage.setItem("hermes-theme", t);
    setThemeState(t);
    const isDark =
      t === "dark" ||
      (t === "system" &&
        window.matchMedia("(prefers-color-scheme: dark)").matches);
    document.documentElement.classList.toggle("dark", isDark);
  }

  return { theme, setTheme };
}

// ---------------------------------------------------------------------------
// Types coming from the backend config API
// ---------------------------------------------------------------------------

interface ConfigResponse {
  env: Record<string, string>;
  hermesHome: string;
  model: { provider: string; model: string; baseUrl: string };
  credentialPool: Record<string, Array<{ key: string; label: string }>>;
  appVersion: string;
  hermesVersion: string | null;
}

interface OperationResult {
  success: boolean;
  error?: string;
}

function getCachedVersion(): string | null {
  try {
    return localStorage.getItem("hermes-version-cache");
  } catch {
    return null;
  }
}

function Settings({
  profile,
  visible,
}: {
  profile?: string;
  visible?: boolean;
}): React.JSX.Element {
  const [env, setEnv] = useState<Record<string, string>>({});
  const [savedKey, setSavedKey] = useState<string | null>(null);
  const [hermesHome, setHermesHome] = useState("");
  const [visibleKeys, setVisibleKeys] = useState<Set<string>>(new Set());
  const { theme, setTheme } = useTheme();

  const [hermesVersion, setHermesVersion] = useState<string | null>(
    getCachedVersion,
  );
  const [appVersion, setAppVersion] = useState("");
  const [doctorOutput, setDoctorOutput] = useState<string | null>(null);
  const [doctorRunning, setDoctorRunning] = useState(false);
  const [updating, setUpdating] = useState(false);
  const [updateResult, setUpdateResult] = useState<string | null>(null);

  // Model config
  const [modelProvider, setModelProvider] = useState("auto");
  const [modelName, setModelName] = useState("");
  const [modelBaseUrl, setModelBaseUrl] = useState("");
  const [modelSaved, setModelSaved] = useState(false);
  const modelLoaded = useRef(false);
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Credential pool
  const [credPool, setCredPool] = useState<
    Record<string, Array<{ key: string; label: string }>>
  >({});
  const [poolProvider, setPoolProvider] = useState("");
  const [poolNewKey, setPoolNewKey] = useState("");
  const [poolNewLabel, setPoolNewLabel] = useState("");

  const loadConfig = useCallback(async (): Promise<void> => {
    try {
      const cfg = await fetchJSON<ConfigResponse>(
        profile ? `/v1/config?profile=${encodeURIComponent(profile)}` : "/v1/config",
      );
      setEnv(cfg.env ?? {});
      setHermesHome(cfg.hermesHome ?? "");
      setModelProvider(cfg.model?.provider ?? "auto");
      setModelName(cfg.model?.model ?? "");
      setModelBaseUrl(cfg.model?.baseUrl ?? "");
      setCredPool(cfg.credentialPool ?? {});
      setAppVersion(cfg.appVersion ?? "");
      if (cfg.hermesVersion) {
        setHermesVersion(cfg.hermesVersion);
        try {
          localStorage.setItem("hermes-version-cache", cfg.hermesVersion);
        } catch {
          /* ignore */
        }
      }
      requestAnimationFrame(() => {
        modelLoaded.current = true;
      });
    } catch (err) {
      console.error("[Settings] loadConfig error:", err);
    }
  }, [profile]);

  useEffect(() => {
    modelLoaded.current = false;
    loadConfig();
  }, [loadConfig]);

  // Refresh model config when screen becomes visible
  useEffect(() => {
    if (!visible) return;
    (async (): Promise<void> => {
      try {
        const cfg = await fetchJSON<ConfigResponse>(
          profile ? `/v1/config?profile=${encodeURIComponent(profile)}` : "/v1/config",
        );
        modelLoaded.current = false;
        setModelProvider(cfg.model?.provider ?? "auto");
        setModelName(cfg.model?.model ?? "");
        setModelBaseUrl(cfg.model?.baseUrl ?? "");
        requestAnimationFrame(() => {
          modelLoaded.current = true;
        });
      } catch {
        /* ignore */
      }
    })();
  }, [visible, profile]);

  // Auto-save model config (debounced)
  const saveModelConfig = useCallback(async () => {
    if (!modelLoaded.current) return;
    try {
      await fetchJSON("/v1/config", {
        method: "PUT",
        body: JSON.stringify({
          profile,
          model: { provider: modelProvider, model: modelName, baseUrl: modelBaseUrl },
        }),
      });
      setModelSaved(true);
      setTimeout(() => setModelSaved(false), 2000);
    } catch (err) {
      console.error("[Settings] saveModelConfig error:", err);
    }
  }, [modelProvider, modelName, modelBaseUrl, profile]);

  useEffect(() => {
    if (!modelLoaded.current) return;
    if (saveTimer.current) clearTimeout(saveTimer.current);
    saveTimer.current = setTimeout(() => {
      saveModelConfig();
    }, 500);
    return () => {
      if (saveTimer.current) clearTimeout(saveTimer.current);
    };
  }, [modelProvider, modelName, modelBaseUrl, saveModelConfig]);

  async function handleBlur(key: string): Promise<void> {
    const value = env[key] || "";
    try {
      await fetchJSON("/v1/config", {
        method: "PUT",
        body: JSON.stringify({ profile, env: { [key]: value } }),
      });
      setSavedKey(key);
      setTimeout(() => setSavedKey(null), 2000);
    } catch (err) {
      console.error("[Settings] setEnv error:", err);
    }
  }

  function handleChange(key: string, value: string): void {
    setEnv((prev) => ({ ...prev, [key]: value }));
  }

  async function handleAddPoolKey(): Promise<void> {
    if (!poolProvider || !poolNewKey.trim()) return;
    const existing = credPool[poolProvider] || [];
    const entries = [
      ...existing,
      {
        key: poolNewKey.trim(),
        label: poolNewLabel.trim() || `Key ${existing.length + 1}`,
      },
    ];
    try {
      await fetchJSON("/v1/config", {
        method: "PUT",
        body: JSON.stringify({ profile, credentialPool: { [poolProvider]: entries } }),
      });
      setCredPool((prev) => ({ ...prev, [poolProvider]: entries }));
      setPoolNewKey("");
      setPoolNewLabel("");
    } catch (err) {
      console.error("[Settings] addPoolKey error:", err);
    }
  }

  async function handleRemovePoolKey(
    provider: string,
    index: number,
  ): Promise<void> {
    const entries = [...(credPool[provider] || [])];
    entries.splice(index, 1);
    try {
      await fetchJSON("/v1/config", {
        method: "PUT",
        body: JSON.stringify({ profile, credentialPool: { [provider]: entries } }),
      });
      setCredPool((prev) => ({ ...prev, [provider]: entries }));
    } catch (err) {
      console.error("[Settings] removePoolKey error:", err);
    }
  }

  function toggleVisibility(key: string): void {
    setVisibleKeys((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  async function handleDoctor(): Promise<void> {
    setDoctorRunning(true);
    setDoctorOutput(null);
    try {
      const result = await fetchJSON<{ output: string }>("/v1/config/doctor", {
        method: "POST",
      });
      setDoctorOutput(result.output);
    } catch (err) {
      setDoctorOutput(String(err));
    } finally {
      setDoctorRunning(false);
    }
  }

  async function handleUpdateHermes(): Promise<void> {
    setUpdating(true);
    setUpdateResult(null);
    try {
      const result = await fetchJSON<OperationResult>("/v1/config/update", {
        method: "POST",
      });
      if (result.success) {
        setUpdateResult("Updated successfully!");
        // Refresh version
        const cfg = await fetchJSON<ConfigResponse>("/v1/config");
        if (cfg.hermesVersion) {
          setHermesVersion(cfg.hermesVersion);
          try {
            localStorage.setItem("hermes-version-cache", cfg.hermesVersion);
          } catch {
            /* ignore */
          }
        }
      } else {
        setUpdateResult(result.error || "Update failed.");
      }
    } catch (err) {
      setUpdateResult(String(err));
    } finally {
      setUpdating(false);
    }
  }

  const parsedVersion = (() => {
    if (!hermesVersion) return null;
    const version = hermesVersion.match(/v([\d.]+)/)?.[1] || "";
    const date = hermesVersion.match(/\(([\d.]+)\)/)?.[1] || "";
    const python = hermesVersion.match(/Python:\s*([\d.]+)/)?.[1] || "";
    const sdk = hermesVersion.match(/OpenAI SDK:\s*([\d.]+)/)?.[1] || "";
    const updateMatch = hermesVersion.match(/Update available:\s*(.+?)(?:\s*—|$)/);
    const updateInfo = updateMatch?.[1]?.trim() || null;
    return { version, date, python, sdk, updateInfo };
  })();

  const isCustomProvider = modelProvider === "custom";

  return (
    <div className="settings-container">
      <h1 className="settings-header">Settings</h1>

      <div className="settings-section">
        <div className="settings-section-title">Hermes Agent</div>
        <div className="settings-hermes-info">
          <div className="settings-hermes-row">
            <div className="settings-hermes-detail">
              <span className="settings-hermes-label">Engine</span>
              {hermesVersion === null ? (
                <span className="skeleton skeleton-sm" />
              ) : (
                <span className="settings-hermes-value">
                  {parsedVersion ? `v${parsedVersion.version}` : "Not detected"}
                </span>
              )}
            </div>
            <div className="settings-hermes-detail">
              <span className="settings-hermes-label">Released</span>
              {hermesVersion === null ? (
                <span className="skeleton skeleton-sm" />
              ) : (
                <span className="settings-hermes-value">
                  {parsedVersion?.date || "—"}
                </span>
              )}
            </div>
            <div className="settings-hermes-detail">
              <span className="settings-hermes-label">Desktop</span>
              {!appVersion ? (
                <span className="skeleton skeleton-sm" />
              ) : (
                <span className="settings-hermes-value">v{appVersion}</span>
              )}
            </div>
            <div className="settings-hermes-detail">
              <span className="settings-hermes-label">Python</span>
              {hermesVersion === null ? (
                <span className="skeleton skeleton-sm" />
              ) : (
                <span className="settings-hermes-value">
                  {parsedVersion?.python || "—"}
                </span>
              )}
            </div>
            <div className="settings-hermes-detail">
              <span className="settings-hermes-label">OpenAI SDK</span>
              {hermesVersion === null ? (
                <span className="skeleton skeleton-sm" />
              ) : (
                <span className="settings-hermes-value">
                  {parsedVersion?.sdk || "—"}
                </span>
              )}
            </div>
            <div className="settings-hermes-detail">
              <span className="settings-hermes-label">Home</span>
              {!hermesHome ? (
                <span className="skeleton skeleton-md" />
              ) : (
                <span className="settings-hermes-value settings-hermes-path">
                  {hermesHome}
                </span>
              )}
            </div>
          </div>
          {parsedVersion?.updateInfo && (
            <div className="settings-hermes-update-badge">
              {parsedVersion.updateInfo}
            </div>
          )}
          <div className="settings-hermes-actions">
            {parsedVersion?.updateInfo ? (
              <button
                className="btn btn-primary"
                onClick={handleUpdateHermes}
                disabled={updating}
              >
                {updating ? "Updating..." : "Update Engine"}
              </button>
            ) : (
              <button className="btn btn-secondary" disabled>
                Up to date
              </button>
            )}
            <button
              className="btn btn-secondary"
              onClick={handleDoctor}
              disabled={doctorRunning}
            >
              {doctorRunning ? "Running..." : "Run Doctor"}
            </button>
          </div>
          {updateResult && (
            <div
              className={`settings-hermes-result ${updateResult.includes("success") ? "success" : "error"}`}
            >
              {updateResult}
            </div>
          )}
          {doctorOutput && (
            <pre className="settings-hermes-doctor">{doctorOutput}</pre>
          )}
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Appearance</div>
        <div className="settings-field">
          <label className="settings-field-label">Theme</label>
          <div className="settings-theme-options">
            {THEME_OPTIONS.map((opt) => (
              <button
                key={opt.value}
                className={`settings-theme-option ${theme === opt.value ? "active" : ""}`}
                onClick={() => setTheme(opt.value)}
              >
                {opt.label}
              </button>
            ))}
          </div>
          <div className="settings-field-hint">
            Choose your preferred appearance
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section-title">
          Model
          {modelSaved && (
            <span className="settings-saved" style={{ marginLeft: 8 }}>
              Saved
            </span>
          )}
        </div>

        <div className="settings-field">
          <label className="settings-field-label">Provider</label>
          <select
            className="input settings-select"
            value={modelProvider}
            onChange={(e) => {
              const provider = e.target.value;
              setModelProvider(provider);
              if (provider === "regolo") {
                setModelBaseUrl("https://api.regolo.ai/v1");
              } else if (provider === "custom" && !modelBaseUrl) {
                setModelBaseUrl("http://localhost:1234/v1");
              }
            }}
          >
            {PROVIDERS.options.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
          <div className="settings-field-hint">
            {isCustomProvider
              ? "Use any OpenAI-compatible endpoint (LM Studio, Ollama, vLLM, etc.)"
              : "Select your inference provider, or auto-detect from API keys"}
          </div>
        </div>

        <div className="settings-field">
          <label className="settings-field-label">Model</label>
          <input
            className="input"
            type="text"
            value={modelName}
            onChange={(e) => setModelName(e.target.value)}
            placeholder="e.g. anthropic/claude-opus-4.6"
          />
          <div className="settings-field-hint">
            Default model name (leave blank for provider default)
          </div>
        </div>

        {isCustomProvider && (
          <div className="settings-field">
            <label className="settings-field-label">Base URL</label>
            <input
              className="input"
              type="text"
              value={modelBaseUrl}
              onChange={(e) => setModelBaseUrl(e.target.value)}
              placeholder="http://localhost:1234/v1"
            />
            <div className="settings-field-hint">
              OpenAI-compatible API endpoint
            </div>
          </div>
        )}
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Credential Pool</div>
        <div className="settings-field">
          <div className="settings-field-hint" style={{ marginBottom: 10 }}>
            Add multiple API keys per provider for automatic rotation and load
            balancing. Hermes will cycle through them.
          </div>
          <div className="settings-pool-add">
            <select
              className="input"
              value={poolProvider}
              onChange={(e) => setPoolProvider(e.target.value)}
              style={{ width: 140 }}
            >
              <option value="">Provider</option>
              {PROVIDERS.options
                .filter((p) => p.value !== "auto")
                .map((p) => (
                  <option key={p.value} value={p.value}>
                    {p.label}
                  </option>
                ))}
            </select>
            <input
              className="input"
              type="password"
              value={poolNewKey}
              onChange={(e) => setPoolNewKey(e.target.value)}
              placeholder="API key"
              style={{ flex: 1 }}
            />
            <input
              className="input"
              type="text"
              value={poolNewLabel}
              onChange={(e) => setPoolNewLabel(e.target.value)}
              placeholder="Label (optional)"
              style={{ width: 120 }}
            />
            <button
              className="btn btn-primary btn-sm"
              onClick={handleAddPoolKey}
              disabled={!poolProvider || !poolNewKey.trim()}
            >
              Add
            </button>
          </div>
          {Object.entries(credPool).map(
            ([provider, entries]) =>
              entries.length > 0 && (
                <div key={provider} className="settings-pool-group">
                  <div className="settings-pool-provider">
                    {PROVIDERS.options.find((p) => p.value === provider)
                      ?.label || provider}
                  </div>
                  {entries.map((entry, idx) => (
                    <div key={idx} className="settings-pool-entry">
                      <span className="settings-pool-label">
                        {entry.label || `Key ${idx + 1}`}
                      </span>
                      <span className="settings-pool-key">
                        {entry.key
                          ? `${entry.key.slice(0, 8)}...${entry.key.slice(-4)}`
                          : "(empty)"}
                      </span>
                      <button
                        className="btn-ghost"
                        style={{ color: "var(--error)", fontSize: 11 }}
                        onClick={() => handleRemovePoolKey(provider, idx)}
                      >
                        Remove
                      </button>
                    </div>
                  ))}
                </div>
              ),
          )}
        </div>
      </div>

      {SETTINGS_SECTIONS.map((section) => (
        <div key={section.title} className="settings-section">
          <div className="settings-section-title">{section.title}</div>
          {section.items.map((field) => (
            <div key={field.key} className="settings-field">
              <label className="settings-field-label">
                {field.label}
                {savedKey === field.key && (
                  <span className="settings-saved">Saved</span>
                )}
              </label>
              <div className="settings-input-row">
                <input
                  className="input"
                  type={
                    field.type === "password" && !visibleKeys.has(field.key)
                      ? "password"
                      : "text"
                  }
                  value={env[field.key] || ""}
                  onChange={(e) => handleChange(field.key, e.target.value)}
                  onBlur={() => handleBlur(field.key)}
                  placeholder={`Enter ${field.label.toLowerCase()}`}
                />
                {field.type === "password" && (
                  <button
                    className="btn-ghost settings-toggle-btn"
                    onClick={() => toggleVisibility(field.key)}
                  >
                    {visibleKeys.has(field.key) ? "Hide" : "Show"}
                  </button>
                )}
              </div>
              <div className="settings-field-hint">{field.hint}</div>
            </div>
          ))}
        </div>
      ))}
    </div>
  );
}

export default Settings;
