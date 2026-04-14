import { useState, useEffect, useCallback } from "react";
import {
  RefreshCw as Refresh,
  Check,
  X,
  Play,
  AlertTriangle,
  Clock,
} from "lucide-react";
import { fetchJSON } from "../../api";

// ---------------------------------------------------------------------------
// Types — mirror the Go structs returned by the Phase 11 endpoints.
// ---------------------------------------------------------------------------

interface GuardFinding {
  category: string;
  pattern: string;
  severity: "block" | "warn";
  line: number;
  excerpt: string;
}

interface GuardResult {
  blocked: boolean;
  findings: GuardFinding[] | null;
  scanned_at: number;
  duration_ms: number;
}

interface ProposalMetadata {
  id: string;
  name: string;
  category: string;
  description: string;
  trust_tier: string;
  created_by: string;
  created_at: number;
  source: string;
  status: string;
  intent?: string;
  intent_targets?: string[];
  intent_new_category?: string;
  intent_reason?: string;
}

interface Proposal {
  metadata: ProposalMetadata;
  content: string;
  guard_result: GuardResult;
}

interface InstalledSkill {
  Category: string;
  Name: string;
  Description: string;
}

interface UsageStats {
  skill_id: string;
  total_count: number;
  success_rate_pct: number;
  last_used_at: number;
}

interface SkillAgentReport {
  agent: "reviewer" | "curator";
  turns: number;
  tool_calls: number;
  final_reply?: string;
  error?: string;
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function formatAgo(ms: number): string {
  if (!ms) return "never";
  const diff = Date.now() - ms;
  const days = Math.floor(diff / 86_400_000);
  if (days >= 1) return `${days}d ago`;
  const hours = Math.floor(diff / 3_600_000);
  if (hours >= 1) return `${hours}h ago`;
  const mins = Math.floor(diff / 60_000);
  return `${mins}m ago`;
}

const STALE_DAYS_THRESHOLD = 7;
const FAILURE_RATE_ALERT_PCT = 20; // below 20% success ⇒ failing skill

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

function SkillReview(): React.JSX.Element {
  const [proposals, setProposals] = useState<Proposal[]>([]);
  const [activeSkills, setActiveSkills] = useState<InstalledSkill[]>([]);
  const [staleSkills, setStaleSkills] = useState<string[]>([]);
  const [failingSkills, setFailingSkills] = useState<string[]>([]);

  const [selected, setSelected] = useState<Proposal | null>(null);
  const [loading, setLoading] = useState(true);
  const [runningAgent, setRunningAgent] = useState<
    "reviewer" | "curator" | null
  >(null);
  const [lastRun, setLastRun] = useState<SkillAgentReport | null>(null);
  const [error, setError] = useState<string>("");
  // Tracks an in-flight approve/reject so double-clicks can't race: the
  // first mutation's refresh() might otherwise resolve AFTER the second
  // mutation's, briefly re-rendering the just-approved item.
  const [mutating, setMutating] = useState<string | null>(null);

  // Load queue + compute health metrics for the sidebar.
  const refresh = useCallback(async () => {
    setError("");
    try {
      const [props, skills] = await Promise.all([
        fetchJSON<Proposal[]>("/v1/skills/proposals"),
        fetchJSON<InstalledSkill[]>("/v1/skills"),
      ]);
      setProposals(props);
      setActiveSkills(skills);

      // Fetch usage stats in parallel — up to 25 skills to keep the payload
      // sane; health metrics are best-effort.
      const now = Date.now();
      const staleCut = now - STALE_DAYS_THRESHOLD * 86_400_000;
      const checks = skills.slice(0, 25).map((s) =>
        fetchJSON<UsageStats>(
          `/v1/skills/usage/${s.Category}/${s.Name}/stats`,
        )
          .then((stats) => ({ skill: s, stats }))
          .catch(() => null),
      );
      const results = await Promise.all(checks);
      const stale: string[] = [];
      const failing: string[] = [];
      for (const r of results) {
        if (!r) continue;
        const id = `${r.skill.Category}/${r.skill.Name}`;
        if (!r.stats.last_used_at || r.stats.last_used_at < staleCut) {
          stale.push(id);
        }
        // Only flag "failing" once we have real data; total_count >= 5 and
        // success rate below threshold means the skill is misfiring.
        if (
          r.stats.total_count >= 5 &&
          r.stats.success_rate_pct < FAILURE_RATE_ALERT_PCT
        ) {
          failing.push(id);
        }
      }
      setStaleSkills(stale);
      setFailingSkills(failing);
    } catch (err) {
      setError(String(err));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // -------------------------------------------------------------- Actions

  async function handleApprove(id: string): Promise<void> {
    if (mutating) return; // double-click / refresh-race guard
    setMutating(id);
    try {
      await fetchJSON(`/v1/skills/proposals/${id}/approve`, {
        method: "POST",
        body: JSON.stringify({}),
      });
      setSelected(null);
      await refresh();
    } catch (err) {
      setError(`Approve failed: ${err}`);
    } finally {
      setMutating(null);
    }
  }

  async function handleReject(id: string): Promise<void> {
    if (mutating) return;
    const reason = window.prompt(
      "Reject reason (required):",
      "out of scope",
    );
    if (!reason) return;
    setMutating(id);
    try {
      await fetchJSON(`/v1/skills/proposals/${id}/reject`, {
        method: "POST",
        body: JSON.stringify({ reason }),
      });
      setSelected(null);
      await refresh();
    } catch (err) {
      setError(`Reject failed: ${err}`);
    } finally {
      setMutating(null);
    }
  }

  async function runAgent(which: "reviewer" | "curator"): Promise<void> {
    setRunningAgent(which);
    setError("");
    try {
      const report = await fetchJSON<SkillAgentReport>(
        `/v1/skills/${which}/run`,
        { method: "POST" },
      );
      setLastRun(report);
      await refresh();
    } catch (err) {
      setError(`${which} run failed: ${err}`);
    } finally {
      setRunningAgent(null);
    }
  }

  // ------------------------------------------------------------- Render

  const backlog = proposals.length;

  return (
    <div className="settings-container">
      <div className="memory-header">
        <div>
          <h1 className="settings-header" style={{ marginBottom: 4 }}>
            Skill Review
          </h1>
          <p className="memory-subtitle">
            Self-healing skill proposals, curator actions, and library health.
          </p>
        </div>
        <div style={{ display: "flex", gap: 8 }}>
          <button
            className="btn btn-secondary btn-sm"
            onClick={refresh}
            disabled={loading}
          >
            <Refresh size={13} /> Refresh
          </button>
          <button
            className="btn btn-primary btn-sm"
            onClick={() => runAgent("reviewer")}
            disabled={runningAgent !== null}
          >
            <Play size={13} />{" "}
            {runningAgent === "reviewer" ? "Running…" : "Run Reviewer"}
          </button>
          <button
            className="btn btn-primary btn-sm"
            onClick={() => runAgent("curator")}
            disabled={runningAgent !== null}
          >
            <Play size={13} />{" "}
            {runningAgent === "curator" ? "Running…" : "Run Curator"}
          </button>
        </div>
      </div>

      {/* Health sidebar + last-run summary */}
      <div
        className="memory-stats"
        style={{
          gridTemplateColumns: "repeat(4, minmax(0, 1fr))",
          marginBottom: 16,
        }}
      >
        <div className="memory-stat">
          <span className="memory-stat-value">{backlog}</span>
          <span className="memory-stat-label">Proposals in queue</span>
        </div>
        <div className="memory-stat">
          <span
            className="memory-stat-value"
            style={{ color: staleSkills.length > 0 ? "#d97706" : undefined }}
          >
            {staleSkills.length}
          </span>
          <span className="memory-stat-label">
            Stale ({STALE_DAYS_THRESHOLD}+ days)
          </span>
        </div>
        <div className="memory-stat">
          <span
            className="memory-stat-value"
            style={{ color: failingSkills.length > 0 ? "#dc2626" : undefined }}
          >
            {failingSkills.length}
          </span>
          <span className="memory-stat-label">Failing (&lt;20% success)</span>
        </div>
        <div className="memory-stat">
          <span className="memory-stat-value">{activeSkills.length}</span>
          <span className="memory-stat-label">Active skills</span>
        </div>
      </div>

      {lastRun && (
        <div
          style={{
            padding: "10px 14px",
            marginBottom: 16,
            background: "var(--bg-secondary, #f4f4f5)",
            borderRadius: 8,
            fontSize: "0.9rem",
          }}
        >
          <strong>{lastRun.agent}</strong> ran {lastRun.turns} turn
          {lastRun.turns === 1 ? "" : "s"} / {lastRun.tool_calls} tool calls
          {lastRun.final_reply && ` — ${lastRun.final_reply}`}
          {lastRun.error && (
            <span style={{ color: "#dc2626" }}> — {lastRun.error}</span>
          )}
        </div>
      )}

      {error && (
        <div
          style={{
            padding: "10px 14px",
            marginBottom: 16,
            background: "#fee2e2",
            color: "#991b1b",
            borderRadius: 8,
            fontSize: "0.9rem",
          }}
        >
          <AlertTriangle size={14} style={{ verticalAlign: "middle" }} />{" "}
          {error}
        </div>
      )}

      {/* Proposal queue */}
      {loading ? (
        <div className="sessions-empty">
          <p className="sessions-empty-text">Loading…</p>
        </div>
      ) : proposals.length === 0 ? (
        <div className="sessions-empty">
          <Clock size={32} className="sessions-empty-icon" />
          <p className="sessions-empty-text">No proposals queued</p>
          <p className="sessions-empty-hint">
            The queue is empty. Run the curator to generate proposals, or wait
            for the main agent to author new skills during its work.
          </p>
        </div>
      ) : (
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1.4fr", gap: 16 }}>
          {/* Left: proposal list */}
          <div className="sessions-list">
            {proposals.map((p) => {
              const m = p.metadata;
              const isSelected = selected?.metadata.id === m.id;
              return (
                <button
                  key={m.id}
                  className={`sessions-card ${isSelected ? "sessions-card--active" : ""}`}
                  onClick={() => setSelected(p)}
                >
                  <div className="sessions-card-main">
                    <span className="sessions-card-title">
                      {m.category}/{m.name}
                    </span>
                    <span className="sessions-card-time">
                      {formatAgo(m.created_at)}
                    </span>
                  </div>
                  <div className="sessions-card-tags">
                    <span className="sessions-tag sessions-tag--source">
                      {m.source}
                    </span>
                    {m.intent && (
                      <span className="sessions-tag">{m.intent}</span>
                    )}
                    {p.guard_result.blocked && (
                      <span
                        className="sessions-tag"
                        style={{ background: "#fee2e2", color: "#991b1b" }}
                      >
                        guard block
                      </span>
                    )}
                    {!p.guard_result.blocked &&
                      p.guard_result.findings &&
                      p.guard_result.findings.length > 0 && (
                        <span
                          className="sessions-tag"
                          style={{ background: "#fef3c7", color: "#854d0e" }}
                        >
                          {p.guard_result.findings.length} warn
                        </span>
                      )}
                  </div>
                </button>
              );
            })}
          </div>

          {/* Right: proposal detail */}
          {selected ? (
            <div
              style={{
                padding: 16,
                background: "var(--bg-secondary, #f4f4f5)",
                borderRadius: 8,
                overflow: "auto",
              }}
            >
              <h3 style={{ marginTop: 0 }}>
                {selected.metadata.category}/{selected.metadata.name}
              </h3>
              <p style={{ fontSize: "0.9rem", color: "var(--text-secondary)" }}>
                {selected.metadata.description}
              </p>

              {selected.metadata.intent && (
                <div
                  style={{
                    padding: "8px 12px",
                    background: "var(--bg-tertiary, #e4e4e7)",
                    borderRadius: 6,
                    marginBottom: 12,
                    fontSize: "0.85rem",
                  }}
                >
                  <strong>intent:</strong> {selected.metadata.intent} ·{" "}
                  <strong>targets:</strong>{" "}
                  {selected.metadata.intent_targets?.join(", ") ?? "—"}
                  {selected.metadata.intent_reason && (
                    <>
                      <br />
                      <strong>reason:</strong>{" "}
                      {selected.metadata.intent_reason}
                    </>
                  )}
                </div>
              )}

              {selected.guard_result.findings &&
                selected.guard_result.findings.length > 0 && (
                  <div
                    style={{
                      padding: "8px 12px",
                      background: selected.guard_result.blocked
                        ? "#fee2e2"
                        : "#fef3c7",
                      borderRadius: 6,
                      marginBottom: 12,
                      fontSize: "0.85rem",
                    }}
                  >
                    <strong>
                      Guard {selected.guard_result.blocked ? "blocked" : "warnings"}:
                    </strong>
                    <ul style={{ marginTop: 4, paddingLeft: 20 }}>
                      {selected.guard_result.findings.map((f, i) => (
                        <li key={i}>
                          [{f.severity}] {f.category}/{f.pattern} @ line{" "}
                          {f.line}
                        </li>
                      ))}
                    </ul>
                  </div>
                )}

              <pre
                style={{
                  padding: 12,
                  background: "var(--bg-code, #1e1e1e)",
                  color: "var(--text-code, #e4e4e7)",
                  borderRadius: 6,
                  overflow: "auto",
                  fontSize: "0.8rem",
                  maxHeight: 400,
                }}
              >
                {selected.content}
              </pre>

              <div style={{ display: "flex", gap: 8, marginTop: 12 }}>
                <button
                  className="btn btn-primary"
                  onClick={() => handleApprove(selected.metadata.id)}
                  disabled={mutating !== null}
                >
                  <Check size={14} />{" "}
                  {mutating === selected.metadata.id ? "Approving…" : "Approve"}
                </button>
                <button
                  className="btn btn-secondary"
                  onClick={() => handleReject(selected.metadata.id)}
                  disabled={mutating !== null}
                >
                  <X size={14} /> Reject
                </button>
              </div>
            </div>
          ) : (
            <div
              style={{
                padding: 16,
                color: "var(--text-secondary, #666)",
                fontStyle: "italic",
              }}
            >
              Select a proposal to review.
            </div>
          )}
        </div>
      )}
    </div>
  );
}

export default SkillReview;
