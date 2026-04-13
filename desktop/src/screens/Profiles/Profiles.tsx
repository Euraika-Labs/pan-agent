import { useState, useEffect, useCallback } from "react";
import {
  Plus,
  Trash2 as Trash,
  MessageSquare as ChatBubble,
} from "lucide-react";
import { fetchJSON } from "../../api";
import icon from "../../assets/icon.png";

interface ProfileInfo {
  name: string;
  isDefault: boolean;
  isActive: boolean;
  model: string;
  provider: string;
  hasEnv: boolean;
  hasSoul: boolean;
  skillCount: number;
  gatewayRunning: boolean;
}

interface ProfilesResponse {
  profiles: ProfileInfo[];
}

interface OperationResult {
  success: boolean;
  error?: string;
}

interface ProfilesProps {
  activeProfile: string;
  onSelectProfile: (name: string) => void;
  onChatWith: (name: string) => void;
}

function ProfileAvatar({ name }: { name: string }): React.JSX.Element {
  if (name === "default") {
    return (
      <div className="agents-card-avatar agents-card-avatar-icon">
        <img src={icon} width={22} height={22} alt="" />
      </div>
    );
  }
  return (
    <div className="agents-card-avatar">{name.charAt(0).toUpperCase()}</div>
  );
}

function Profiles({
  activeProfile,
  onSelectProfile,
  onChatWith,
}: ProfilesProps): React.JSX.Element {
  const [profiles, setProfiles] = useState<ProfileInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [showCreate, setShowCreate] = useState(false);
  const [newName, setNewName] = useState("");
  const [cloneConfig, setCloneConfig] = useState(true);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState("");
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  const loadProfiles = useCallback(async (): Promise<void> => {
    try {
      const resp = await fetchJSON<ProfilesResponse>("/v1/config/profiles");
      setProfiles(resp.profiles ?? []);
    } catch (err) {
      console.error("[Profiles] load error:", err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    loadProfiles();
  }, [loadProfiles]);

  async function handleCreate(): Promise<void> {
    const name = newName.trim();
    if (!name || name === "default") {
      setError("Profile name cannot be empty or 'default'");
      return;
    }
    setCreating(true);
    setError("");
    try {
      const result = await fetchJSON<OperationResult>("/v1/config/profiles", {
        method: "POST",
        body: JSON.stringify({ name, cloneConfig }),
      });
      if (result.success) {
        setShowCreate(false);
        setNewName("");
        await loadProfiles();
      } else {
        setError(result.error || "Failed to create profile");
      }
    } catch (err) {
      setError(String(err));
    } finally {
      setCreating(false);
    }
  }

  async function handleDelete(name: string): Promise<void> {
    try {
      const result = await fetchJSON<OperationResult>(
        `/v1/config/profiles/${encodeURIComponent(name)}`,
        { method: "DELETE" },
      );
      setConfirmDelete(null);
      if (result.success) {
        if (activeProfile === name) onSelectProfile("default");
        await loadProfiles();
      } else {
        setError(result.error || "Failed to delete profile");
      }
    } catch (err) {
      setError(String(err));
    }
  }

  if (loading) {
    return (
      <div className="settings-container">
        <h1 className="settings-header">Profiles</h1>
        <div style={{ display: "flex", justifyContent: "center", padding: 48 }}>
          <div className="loading-spinner" />
        </div>
      </div>
    );
  }

  return (
    <div className="settings-container">
      <div className="models-header">
        <div>
          <h1 className="settings-header" style={{ marginBottom: 4 }}>
            Profiles
          </h1>
          <p className="models-subtitle">
            Each profile has its own config, API keys, memory, skills, and
            persona.
          </p>
        </div>
        <button
          className="btn btn-primary btn-sm"
          onClick={() => setShowCreate(true)}
        >
          <Plus size={14} />
          New Profile
        </button>
      </div>

      {error && (
        <div className="memory-error" style={{ marginBottom: 12 }}>
          {error}
        </div>
      )}

      {showCreate && (
        <div className="settings-section">
          <div className="settings-section-title">New Profile</div>
          <div className="settings-field">
            <label className="settings-field-label">Name</label>
            <input
              className="input"
              type="text"
              placeholder="e.g. work, research, coding"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") handleCreate();
                if (e.key === "Escape") setShowCreate(false);
              }}
              autoFocus
            />
          </div>
          <div className="settings-field">
            <label style={{ display: "flex", alignItems: "center", gap: 8, fontSize: 13, cursor: "pointer" }}>
              <input
                type="checkbox"
                checked={cloneConfig}
                onChange={(e) => setCloneConfig(e.target.checked)}
              />
              Copy current config (model, API keys)
            </label>
          </div>
          <div style={{ display: "flex", gap: 8, marginTop: 8 }}>
            <button
              className="btn btn-primary btn-sm"
              onClick={handleCreate}
              disabled={creating || !newName.trim()}
            >
              {creating ? "Creating..." : "Create"}
            </button>
            <button
              className="btn btn-secondary btn-sm"
              onClick={() => {
                setShowCreate(false);
                setNewName("");
                setError("");
              }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      <div className="models-grid">
        {profiles.map((p) => (
          <div
            key={p.name}
            className={`models-card ${activeProfile === p.name ? "models-card--active" : ""}`}
            style={{ cursor: "pointer" }}
            onClick={() => onSelectProfile(p.name)}
          >
            <div className="models-card-header">
              <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                <ProfileAvatar name={p.name} />
                <div className="models-card-name">
                  {p.name === "default" ? "Default" : p.name}
                </div>
              </div>
              {activeProfile === p.name && (
                <span className="models-card-provider" style={{ color: "var(--success)" }}>
                  Active
                </span>
              )}
            </div>

            {p.model && (
              <div className="models-card-model">
                {p.provider !== "auto" && `${p.provider} / `}{p.model}
              </div>
            )}

            <div className="models-card-footer" style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
              {p.hasSoul && (
                <span className="sessions-tag">Persona</span>
              )}
              {p.hasEnv && (
                <span className="sessions-tag">Keys</span>
              )}
              {p.skillCount > 0 && (
                <span className="sessions-tag">{p.skillCount} skills</span>
              )}
              {p.gatewayRunning && (
                <span className="sessions-tag" style={{ color: "var(--success)" }}>
                  Gateway
                </span>
              )}
            </div>

            <div className="models-card-footer">
              <button
                className="btn btn-primary btn-sm"
                style={{ marginRight: 4 }}
                onClick={(e) => {
                  e.stopPropagation();
                  onChatWith(p.name);
                }}
              >
                <ChatBubble size={13} />
                Chat
              </button>
              {!p.isDefault && (
                confirmDelete === p.name ? (
                  <span
                    className="models-card-confirm"
                    onClick={(e) => e.stopPropagation()}
                  >
                    <span>Delete?</span>
                    <button
                      className="btn btn-sm"
                      style={{ color: "var(--error)" }}
                      onClick={() => handleDelete(p.name)}
                    >
                      Yes
                    </button>
                    <button
                      className="btn btn-sm"
                      onClick={() => setConfirmDelete(null)}
                    >
                      No
                    </button>
                  </span>
                ) : (
                  <button
                    className="btn-ghost models-card-delete"
                    onClick={(e) => {
                      e.stopPropagation();
                      setConfirmDelete(p.name);
                    }}
                    title="Delete profile"
                  >
                    <Trash size={14} />
                  </button>
                )
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

export default Profiles;
