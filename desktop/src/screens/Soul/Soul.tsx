import { useState, useEffect, useRef, useCallback } from "react";
import { RefreshCw as Refresh } from "lucide-react";
import { fetchJSON } from "../../api";

interface SoulProps {
  profile?: string;
}

interface PersonaResponse {
  content: string;
}

interface OperationResult {
  success: boolean;
  content?: string;
  error?: string;
}

function Soul({ profile }: SoulProps): React.JSX.Element {
  const [content, setContent] = useState("");
  const [loading, setLoading] = useState(true);
  const [saved, setSaved] = useState(false);
  const [showReset, setShowReset] = useState(false);
  const loaded = useRef(false);
  const saveTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const profileParam = profile
    ? `?profile=${encodeURIComponent(profile)}`
    : "";

  const loadSoul = useCallback(async (): Promise<void> => {
    loaded.current = false;
    setLoading(true);
    try {
      const resp = await fetchJSON<PersonaResponse>(
        `/v1/persona${profileParam}`,
      );
      setContent(resp.content ?? "");
    } catch (err) {
      console.error("[Soul] load error:", err);
    } finally {
      setLoading(false);
      setTimeout(() => {
        loaded.current = true;
      }, 300);
    }
  }, [profileParam]);

  useEffect(() => {
    loadSoul();
  }, [loadSoul]);

  const saveSoul = useCallback(
    async (text: string) => {
      if (!loaded.current) return;
      try {
        await fetchJSON(`/v1/persona${profileParam}`, {
          method: "PUT",
          body: JSON.stringify({ content: text }),
        });
        setSaved(true);
        setTimeout(() => setSaved(false), 2000);
      } catch (err) {
        console.error("[Soul] save error:", err);
      }
    },
    [profileParam],
  );

  useEffect(() => {
    if (!loaded.current) return;
    if (saveTimer.current) clearTimeout(saveTimer.current);
    saveTimer.current = setTimeout(() => {
      saveSoul(content);
    }, 500);
    return () => {
      if (saveTimer.current) clearTimeout(saveTimer.current);
    };
  }, [content, saveSoul]);

  async function handleReset(): Promise<void> {
    try {
      const result = await fetchJSON<OperationResult>(
        `/v1/persona/reset${profileParam}`,
        { method: "POST" },
      );
      if (result.content !== undefined) {
        loaded.current = false;
        setContent(result.content);
        setShowReset(false);
        setSaved(true);
        setTimeout(() => {
          loaded.current = true;
          setSaved(false);
        }, 2000);
      }
    } catch (err) {
      console.error("[Soul] reset error:", err);
    }
  }

  if (loading) {
    return (
      <div className="soul-container">
        <div className="soul-loading">
          <div className="loading-spinner" />
        </div>
      </div>
    );
  }

  return (
    <div className="soul-container">
      <div className="soul-header">
        <div>
          <h2 className="soul-title">
            Persona
            {saved && <span className="soul-saved">Saved</span>}
          </h2>
          <p className="soul-subtitle">
            Define your agent&apos;s personality, tone, and instructions via
            SOUL.md
          </p>
        </div>
        <button
          className="btn btn-secondary btn-sm"
          onClick={() => setShowReset(true)}
          title="Reset to default"
        >
          <Refresh size={14} />
          Reset
        </button>
      </div>

      {showReset && (
        <div className="soul-reset-confirm">
          <span>
            Reset to the default persona? Your current content will be lost.
          </span>
          <div className="soul-reset-actions">
            <button className="btn btn-primary btn-sm" onClick={handleReset}>
              Reset
            </button>
            <button
              className="btn btn-secondary btn-sm"
              onClick={() => setShowReset(false)}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      <textarea
        className="soul-editor"
        value={content}
        onChange={(e) => setContent(e.target.value)}
        placeholder="Write your agent's persona instructions here..."
        spellCheck={false}
      />

      <div className="soul-hint">
        This file is loaded fresh for every conversation. Use it to define your
        agent&apos;s personality, communication style, and any standing
        instructions.
      </div>
    </div>
  );
}

export default Soul;
