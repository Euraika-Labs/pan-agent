import { useState, useEffect, useCallback } from "react";
import { fetchJSON } from "../../api";
import { GATEWAY_SECTIONS, GATEWAY_PLATFORMS } from "../../constants";

interface HealthResponse {
  gateway: boolean;
  env: Record<string, string>;
  platformEnabled: Record<string, boolean>;
}

interface ConfigPutBody {
  profile?: string;
  env?: Record<string, string>;
  platformEnabled?: Record<string, boolean>;
}

function Gateway({ profile }: { profile?: string }): React.JSX.Element {
  const [gatewayRunning, setGatewayRunning] = useState(false);
  const [env, setEnv] = useState<Record<string, string>>({});
  const [platformEnabled, setPlatformEnabled] = useState<
    Record<string, boolean>
  >({});
  const [savedKey, setSavedKey] = useState<string | null>(null);
  const [visibleKeys, setVisibleKeys] = useState<Set<string>>(new Set());

  const profileParam = profile
    ? `?profile=${encodeURIComponent(profile)}`
    : "";

  const loadConfig = useCallback(async (): Promise<void> => {
    try {
      const health = await fetchJSON<HealthResponse>(
        `/v1/health${profileParam}`,
      );
      setGatewayRunning(health.gateway ?? false);
      setEnv(health.env ?? {});
      setPlatformEnabled(health.platformEnabled ?? {});
    } catch (err) {
      console.error("[Gateway] loadConfig error:", err);
    }
  }, [profileParam]);

  useEffect(() => {
    loadConfig();
  }, [loadConfig]);

  // Poll gateway status every 10s
  useEffect(() => {
    const interval = setInterval(async () => {
      try {
        const health = await fetchJSON<HealthResponse>(
          `/v1/health${profileParam}`,
        );
        setGatewayRunning(health.gateway ?? false);
      } catch {
        /* ignore poll errors */
      }
    }, 10000);
    return () => clearInterval(interval);
  }, [profileParam]);

  async function toggleGateway(): Promise<void> {
    try {
      if (gatewayRunning) {
        await fetchJSON("/v1/health/gateway/stop", { method: "POST" });
        setGatewayRunning(false);
      } else {
        await fetchJSON("/v1/health/gateway/start", { method: "POST" });
        // Re-check after a delay to confirm it came up
        setTimeout(async () => {
          try {
            const health = await fetchJSON<HealthResponse>(
              `/v1/health${profileParam}`,
            );
            setGatewayRunning(health.gateway ?? false);
          } catch {
            /* ignore */
          }
        }, 2000);
      }
    } catch (err) {
      console.error("[Gateway] toggleGateway error:", err);
    }
  }

  async function togglePlatform(platform: string): Promise<void> {
    const newValue = !platformEnabled[platform];
    setPlatformEnabled((prev) => ({ ...prev, [platform]: newValue }));
    try {
      const body: ConfigPutBody = {
        profile,
        platformEnabled: { [platform]: newValue },
      };
      await fetchJSON("/v1/config", {
        method: "PUT",
        body: JSON.stringify(body),
      });
      // Re-check gateway status after platform change
      setTimeout(async () => {
        try {
          const health = await fetchJSON<HealthResponse>(
            `/v1/health${profileParam}`,
          );
          setGatewayRunning(health.gateway ?? false);
        } catch {
          /* ignore */
        }
      }, 3000);
    } catch (err) {
      console.error("[Gateway] togglePlatform error:", err);
      // Revert optimistic update
      setPlatformEnabled((prev) => ({ ...prev, [platform]: !newValue }));
    }
  }

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
      console.error("[Gateway] setEnv error:", err);
    }
  }

  function handleChange(key: string, value: string): void {
    setEnv((prev) => ({ ...prev, [key]: value }));
  }

  function toggleVisibility(key: string): void {
    setVisibleKeys((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  }

  // Build a set of field keys that belong to platforms
  const platformFieldKeys = new Set(GATEWAY_PLATFORMS.flatMap((p) => p.fields));

  // Non-platform sections
  const otherSections = GATEWAY_SECTIONS.map((section) => ({
    ...section,
    items: section.items.filter((item) => !platformFieldKeys.has(item.key)),
  })).filter((section) => section.items.length > 0);

  const fieldDefs = new Map(
    GATEWAY_SECTIONS.flatMap((s) => s.items).map((f) => [f.key, f]),
  );

  return (
    <div className="settings-container">
      <h1 className="settings-header">Gateway</h1>

      <div className="settings-section">
        <div className="settings-section-title">Messaging Gateway</div>
        <div className="settings-field">
          <label className="settings-field-label">Status</label>
          <div className="settings-gateway-row">
            <span
              className={`settings-gateway-status ${gatewayRunning ? "running" : "stopped"}`}
            >
              {gatewayRunning ? "Running" : "Stopped"}
            </span>
            <button
              className="btn btn-secondary btn-sm"
              onClick={toggleGateway}
            >
              {gatewayRunning ? "Stop" : "Start"}
            </button>
          </div>
          <div className="settings-field-hint">
            Connects Pan-Agent to Telegram, Discord, Slack, and other platforms
          </div>
        </div>
      </div>

      <div className="settings-section">
        <div className="settings-section-title">Platforms</div>
        {GATEWAY_PLATFORMS.map((platform) => (
          <div key={platform.key} className="settings-platform-card">
            <div className="settings-platform-header">
              <div className="settings-platform-info">
                <span className="settings-platform-label">
                  {platform.label}
                </span>
                <span className="settings-platform-desc">
                  {platform.description}
                </span>
              </div>
              <label className="tools-toggle">
                <input
                  type="checkbox"
                  checked={!!platformEnabled[platform.key]}
                  onChange={() => togglePlatform(platform.key)}
                />
                <span className="tools-toggle-track" />
              </label>
            </div>
            {platformEnabled[platform.key] && (
              <div className="settings-platform-fields">
                {platform.fields.map((fieldKey) => {
                  const field = fieldDefs.get(fieldKey);
                  if (!field) return null;
                  return (
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
                            field.type === "password" &&
                            !visibleKeys.has(field.key)
                              ? "password"
                              : "text"
                          }
                          value={env[field.key] || ""}
                          onChange={(e) =>
                            handleChange(field.key, e.target.value)
                          }
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
                  );
                })}
              </div>
            )}
          </div>
        ))}
      </div>

      {otherSections.map((section) => (
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

export default Gateway;
