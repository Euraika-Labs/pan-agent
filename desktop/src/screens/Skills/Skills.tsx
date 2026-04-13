import { useState, useEffect, useRef, useCallback } from "react";
import {
  Search,
  X,
  Download,
  Trash2 as Trash,
  RefreshCw as Refresh,
} from "lucide-react";
import { fetchJSON } from "../../api";
import { AgentMarkdown } from "../../components/AgentMarkdown";

interface InstalledSkill {
  name: string;
  category: string;
  description: string;
  path: string;
}

interface BundledSkill {
  name: string;
  description: string;
  category: string;
  source: string;
  installed: boolean;
}

interface OperationResult {
  success: boolean;
  error?: string;
}

interface SkillsProps {
  profile?: string;
}

type Tab = "installed" | "browse";

function Skills({ profile }: SkillsProps): React.JSX.Element {
  const [tab, setTab] = useState<Tab>("installed");
  const [installedSkills, setInstalledSkills] = useState<InstalledSkill[]>([]);
  const [bundledSkills, setBundledSkills] = useState<BundledSkill[]>([]);
  const [search, setSearch] = useState("");
  const [categoryFilter, setCategoryFilter] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [detailSkill, setDetailSkill] = useState<InstalledSkill | null>(null);
  const [detailContent, setDetailContent] = useState("");
  const [actionInProgress, setActionInProgress] = useState<string | null>(null);
  const [error, setError] = useState("");
  const searchRef = useRef<HTMLInputElement>(null);

  const profileParam = profile
    ? `?profile=${encodeURIComponent(profile)}`
    : "";

  const loadInstalled = useCallback(async (): Promise<void> => {
    const list = await fetchJSON<InstalledSkill[]>(
      `/v1/skills${profileParam}`,
    );
    setInstalledSkills(list);
  }, [profileParam]);

  const loadBundled = useCallback(async (): Promise<void> => {
    const list = await fetchJSON<BundledSkill[]>("/v1/skills/bundled");
    setBundledSkills(list);
  }, []);

  const loadAll = useCallback(async (): Promise<void> => {
    setLoading(true);
    try {
      await Promise.all([loadInstalled(), loadBundled()]);
    } catch (err) {
      console.error("[Skills] load error:", err);
    } finally {
      setLoading(false);
    }
  }, [loadInstalled, loadBundled]);

  useEffect(() => {
    loadAll();
  }, [loadAll]);

  async function handleViewDetail(skill: InstalledSkill): Promise<void> {
    setDetailSkill(skill);
    try {
      const resp = await fetchJSON<{ content: string }>(
        `/v1/skills/${encodeURIComponent(skill.name)}/content${profileParam}`,
      );
      setDetailContent(resp.content);
    } catch {
      setDetailContent("");
    }
  }

  async function handleInstall(name: string): Promise<void> {
    setActionInProgress(name);
    setError("");
    try {
      const result = await fetchJSON<OperationResult>(
        `/v1/skills/install${profileParam}`,
        {
          method: "POST",
          body: JSON.stringify({ name }),
        },
      );
      if (result.success) {
        await loadInstalled();
      } else {
        setError(result.error || "Failed to install skill");
      }
    } catch (err) {
      setError(String(err));
    } finally {
      setActionInProgress(null);
    }
  }

  async function handleUninstall(name: string): Promise<void> {
    setActionInProgress(name);
    setError("");
    try {
      const result = await fetchJSON<OperationResult>(
        `/v1/skills/${encodeURIComponent(name)}${profileParam}`,
        { method: "DELETE" },
      );
      if (result.success) {
        setDetailSkill(null);
        await loadInstalled();
      } else {
        setError(result.error || "Failed to uninstall skill");
      }
    } catch (err) {
      setError(String(err));
    } finally {
      setActionInProgress(null);
    }
  }

  const installedNames = new Set(
    installedSkills.map((s) => s.name.toLowerCase()),
  );

  const filteredInstalled = installedSkills.filter((s) => {
    if (search) {
      const query = search.toLowerCase();
      return (
        s.name.toLowerCase().includes(query) ||
        s.description.toLowerCase().includes(query) ||
        s.category.toLowerCase().includes(query)
      );
    }
    return true;
  });

  const filteredBundled = bundledSkills.filter((s) => {
    let matches = true;
    if (search) {
      const query = search.toLowerCase();
      matches =
        s.name.toLowerCase().includes(query) ||
        s.description.toLowerCase().includes(query) ||
        s.category.toLowerCase().includes(query);
    }
    if (categoryFilter) {
      matches = matches && s.category === categoryFilter;
    }
    return matches;
  });

  const categories = Array.from(
    new Set(bundledSkills.map((s) => s.category)),
  ).sort();

  if (loading) {
    return (
      <div className="skills-container">
        <div className="skills-loading">
          <div className="loading-spinner" />
        </div>
      </div>
    );
  }

  return (
    <div className="skills-container">
      {/* Detail Panel */}
      {detailSkill && (
        <div
          className="skills-detail-overlay"
          onClick={() => setDetailSkill(null)}
        >
          <div className="skills-detail" onClick={(e) => e.stopPropagation()}>
            <div className="skills-detail-header">
              <div>
                <div className="skills-detail-name">{detailSkill.name}</div>
                <div className="skills-detail-category">
                  {detailSkill.category}
                </div>
              </div>
              <div className="skills-detail-actions">
                <button
                  className="btn btn-secondary btn-sm"
                  onClick={() => handleUninstall(detailSkill.name)}
                  disabled={actionInProgress === detailSkill.name}
                >
                  {actionInProgress === detailSkill.name ? (
                    "Removing..."
                  ) : (
                    <>
                      <Trash size={13} />
                      Uninstall
                    </>
                  )}
                </button>
                <button
                  className="btn-ghost"
                  onClick={() => setDetailSkill(null)}
                >
                  <X size={18} />
                </button>
              </div>
            </div>
            <div className="skills-detail-content">
              <AgentMarkdown>{detailContent}</AgentMarkdown>
            </div>
          </div>
        </div>
      )}

      <div className="skills-header">
        <div>
          <h2 className="skills-title">Skills</h2>
          <p className="skills-subtitle">
            Extend your agent with reusable skills and workflows
          </p>
        </div>
        <button className="btn btn-secondary btn-sm" onClick={loadAll}>
          <Refresh size={14} />
          Refresh
        </button>
      </div>

      {error && (
        <div className="skills-error">
          {error}
          <button className="btn-ghost" onClick={() => setError("")}>
            <X size={14} />
          </button>
        </div>
      )}

      {/* Tabs */}
      <div className="skills-tabs">
        <button
          className={`skills-tab ${tab === "installed" ? "active" : ""}`}
          onClick={() => setTab("installed")}
        >
          Installed ({installedSkills.length})
        </button>
        <button
          className={`skills-tab ${tab === "browse" ? "active" : ""}`}
          onClick={() => setTab("browse")}
        >
          Browse ({bundledSkills.length})
        </button>
      </div>

      {/* Search */}
      <div className="skills-search">
        <Search size={15} />
        <input
          ref={searchRef}
          className="skills-search-input"
          type="text"
          placeholder={
            tab === "installed"
              ? "Filter installed skills..."
              : "Search skills..."
          }
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        {search && (
          <button
            className="btn-ghost skills-search-clear"
            onClick={() => {
              setSearch("");
              searchRef.current?.focus();
            }}
          >
            <X size={14} />
          </button>
        )}
      </div>

      {/* Category filter pills (browse tab only) */}
      {tab === "browse" && categories.length > 0 && (
        <div className="skills-category-pills">
          <button
            className={`skills-pill ${categoryFilter === null ? "active" : ""}`}
            onClick={() => setCategoryFilter(null)}
          >
            All
          </button>
          {categories.map((cat) => (
            <button
              key={cat}
              className={`skills-pill ${categoryFilter === cat ? "active" : ""}`}
              onClick={() =>
                setCategoryFilter(categoryFilter === cat ? null : cat)
              }
            >
              {cat}
            </button>
          ))}
        </div>
      )}

      {/* Grid */}
      {tab === "installed" ? (
        filteredInstalled.length === 0 ? (
          <div className="skills-empty">
            <p className="skills-empty-text">
              {search ? "No matching skills found" : "No skills installed yet"}
            </p>
            <p className="skills-empty-hint">
              {search
                ? "Try a different search term"
                : "Browse available skills and install them to extend your agent"}
            </p>
          </div>
        ) : (
          <div className="skills-grid">
            {filteredInstalled.map((skill) => (
              <button
                key={`${skill.category}/${skill.name}`}
                className="skills-card"
                onClick={() => handleViewDetail(skill)}
              >
                <div className="skills-card-category">{skill.category}</div>
                <div className="skills-card-name">{skill.name}</div>
                {skill.description && (
                  <div className="skills-card-description">
                    {skill.description}
                  </div>
                )}
              </button>
            ))}
          </div>
        )
      ) : filteredBundled.length === 0 ? (
        <div className="skills-empty">
          <p className="skills-empty-text">No skills found</p>
          <p className="skills-empty-hint">
            Try a different search term or category filter
          </p>
        </div>
      ) : (
        <div className="skills-grid">
          {filteredBundled.map((skill) => {
            const isInstalled = installedNames.has(skill.name.toLowerCase());
            const isActioning = actionInProgress === skill.name;
            return (
              <div
                key={`${skill.category}/${skill.name}`}
                className="skills-card"
              >
                <div className="skills-card-category">{skill.category}</div>
                <div className="skills-card-name">{skill.name}</div>
                {skill.description && (
                  <div className="skills-card-description">
                    {skill.description}
                  </div>
                )}
                <div className="skills-card-footer">
                  {isInstalled ? (
                    <span className="skills-card-installed-badge">
                      Installed
                    </span>
                  ) : (
                    <button
                      className="btn btn-primary btn-sm skills-card-install-btn"
                      onClick={(e) => {
                        e.stopPropagation();
                        handleInstall(skill.name);
                      }}
                      disabled={isActioning}
                    >
                      {isActioning ? (
                        "Installing..."
                      ) : (
                        <>
                          <Download size={13} />
                          Install
                        </>
                      )}
                    </button>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}

export default Skills;
