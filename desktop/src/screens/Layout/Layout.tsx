import { useState, useCallback, useEffect } from "react";
import { ErrorBoundary } from "../../components/ErrorBoundary";
import Chat, { type ChatMessage } from "../Chat/Chat";
import Sessions from "../Sessions/Sessions";
import Tasks from "../Tasks/Tasks";
import Profiles from "../Profiles/Profiles";
import Settings from "../Settings/Settings";
import Setup from "../Setup/Setup";
import Skills from "../Skills/Skills";
import SkillReview from "../SkillReview/SkillReview";
import Soul from "../Soul/Soul";
import Memory from "../Memory/Memory";
import Tools from "../Tools/Tools";
import Gateway from "../Gateway/Gateway";
import Office from "../Office/Office";
import Models from "../Models/Models";
import Schedules from "../Schedules/Schedules";
import Search from "../Search/Search";
// M4 W2 Commit D — mount above screen stack, self-gate internally
import MigrationBanner from "../../components/MigrationBanner";
import PersistenceAlert from "../../components/PersistenceAlert";
// M5-C3 — WebView2 fallback banner, self-gates on office.browser_fallback_until
import FallbackBanner from "../../components/FallbackBanner";
import icon from "../../assets/icon.png";
import { fetchJSON } from "../../api";
import {
  MessageSquare as ChatBubble,
  Clock,
  ListChecks,
  Users,
  Settings as SettingsIcon,
  Puzzle,
  Sparkles,
  Brain,
  Wrench,
  Radio as Signal,
  Building,
  Layers,
  Timer,
  Search as SearchIcon,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";

type View =
  | "chat"
  | "sessions"
  | "tasks"
  | "profiles"
  | "office"
  | "models"
  | "skills"
  | "skill-review"
  | "soul"
  | "memory"
  | "tools"
  | "schedules"
  | "gateway"
  | "search"
  | "settings";

const NAV_ITEMS: { view: View; icon: LucideIcon; label: string }[] = [
  { view: "chat", icon: ChatBubble, label: "Chat" },
  { view: "sessions", icon: Clock, label: "Sessions" },
  { view: "tasks", icon: ListChecks, label: "Tasks" },
  { view: "profiles", icon: Users, label: "Profiles" },
  { view: "office", icon: Building, label: "Office" },
  { view: "models", icon: Layers, label: "Models" },
  { view: "skills", icon: Puzzle, label: "Skills" },
  { view: "skill-review", icon: Sparkles, label: "Skill Review" },
  { view: "soul", icon: Sparkles, label: "Persona" },
  { view: "memory", icon: Brain, label: "Memory" },
  { view: "tools", icon: Wrench, label: "Tools" },
  { view: "schedules", icon: Timer, label: "Schedules" },
  { view: "gateway", icon: Signal, label: "Gateway" },
  { view: "search", icon: SearchIcon, label: "Search" },
  { view: "settings", icon: SettingsIcon, label: "Settings" },
];

function Layout(): React.JSX.Element {
  const [view, setView] = useState<View>("chat");
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [currentSessionId, setCurrentSessionId] = useState<string | null>(null);
  const [activeProfile, setActiveProfile] = useState("default");
  // Lazy mount Office — only render after first visit, then keep mounted
  const [officeVisited, setOfficeVisited] = useState(false);
  // First-run onboarding: null = checking, true = show setup, false = ready
  const [setupRequired, setSetupRequired] = useState<boolean | null>(null);

  const handleNewChat = useCallback(() => {
    setMessages([]);
    setCurrentSessionId(null);
    setView("chat");
  }, []);

  const handleSelectProfile = useCallback((name: string) => {
    setActiveProfile(name);
    setMessages([]);
    setCurrentSessionId(null);
  }, []);

  const handleResumeSession = useCallback(async (sessionId: string) => {
    try {
      interface DbMessage {
        id: string;
        role: string;
        content: string;
      }
      const dbMessages = await fetchJSON<DbMessage[]>(
        `/v1/sessions/${encodeURIComponent(sessionId)}`,
      );
      const chatMessages: ChatMessage[] = dbMessages.map((m) => ({
        id: `db-${m.id}`,
        role: m.role === "user" ? "user" : "agent",
        content: m.content,
      }));
      setMessages(chatMessages);
      setCurrentSessionId(sessionId);
      setView("chat");
    } catch (err) {
      console.error("[Layout] resumeSession error:", err);
    }
  }, []);

  // First-run detection: check if any LLM provider is configured
  useEffect(() => {
    let cancelled = false;
    async function checkFirstRun(attempt = 0) {
      try {
        const cfg = await fetchJSON<{
          env: Record<string, string>;
          model: { provider: string; model: string; baseUrl: string };
        }>("/v1/config");
        if (cancelled) return;

        const hasKey = ["OPENROUTER_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "REGOLO_API_KEY"]
          .some((k) => cfg.env[k]?.trim());
        const hasCustomUrl = cfg.model.baseUrl && cfg.model.provider === "custom";

        setSetupRequired(!hasKey && !hasCustomUrl);
      } catch {
        if (cancelled) return;
        if (attempt < 5) {
          setTimeout(() => checkFirstRun(attempt + 1), Math.min(1000 * 2 ** attempt, 8000));
        } else {
          setSetupRequired(false); // give up checking, show app
        }
      }
    }
    checkFirstRun();
    return () => { cancelled = true; };
  }, []);

  // Keyboard shortcut: Ctrl/Cmd+N → new chat
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent): void {
      if ((e.metaKey || e.ctrlKey) && e.key === "n") {
        e.preventDefault();
        handleNewChat();
      }
      if ((e.metaKey || e.ctrlKey) && e.key === "k") {
        e.preventDefault();
        setView("search");
      }
    }
    document.addEventListener("keydown", handleKeyDown);
    return () => document.removeEventListener("keydown", handleKeyDown);
  }, [handleNewChat]);

  // Show nothing while checking first-run status
  if (setupRequired === null) {
    return <div className="setup-screen" />;
  }

  // Show onboarding wizard if no provider is configured
  if (setupRequired) {
    return <Setup onComplete={() => setSetupRequired(false)} />;
  }

  return (
    <div className="layout">
      <aside className="sidebar">
        <div className="sidebar-brand">
          <img src={icon} height={30} alt="" />
        </div>

        <nav className="sidebar-nav">
          {NAV_ITEMS.map(({ view: v, icon: Icon, label }) => (
            <button
              key={v}
              className={`sidebar-nav-item ${view === v ? "active" : ""}`}
              onClick={() => {
                if (v === "office") setOfficeVisited(true);
                setView(v);
              }}
            >
              <Icon size={16} />
              {label}
            </button>
          ))}
        </nav>

        <div className="sidebar-footer">
          <div className="sidebar-footer-text">
            {activeProfile === "default" ? "Pan-Agent" : activeProfile}
          </div>
        </div>
      </aside>

      <main className="content">
        {/*
          M4 W2 Commit D — PersistenceAlert and MigrationBanner mount here,
          above the screen stack. Both components self-gate:
            - PersistenceAlert listens for the pan-agent:persistence-alert
              event fired by OfficeDebugPanel on engine-swap persisted=false.
            - MigrationBanner polls /v1/office/migration/status on mount.
          Setup-first gating above (first-run users with no LLM provider)
          already returned early, so these never render on first-launch.
        */}
        <PersistenceAlert />
        <FallbackBanner />
        <MigrationBanner />
        {/* Wrap screens in an ErrorBoundary so one crashy screen (e.g. Memory
            before the shape fix) doesn't blank the whole app. key=view resets
            the boundary on view change so a new screen always gets a fresh
            render attempt. */}
        <ErrorBoundary key={view} screenName={view}>
        {/* Chat — always mounted, hidden when not active */}
        <div
          style={{
            display: view === "chat" ? "flex" : "none",
            flex: 1,
            flexDirection: "column",
            overflow: "hidden",
          }}
        >
          <Chat
            messages={messages}
            setMessages={setMessages}
            sessionId={currentSessionId}
            profile={activeProfile}
            onNewChat={handleNewChat}
          />
        </div>

        {view === "sessions" && (
          <Sessions
            onResumeSession={handleResumeSession}
            onNewChat={handleNewChat}
            currentSessionId={currentSessionId}
          />
        )}

        {view === "tasks" && <Tasks profile={activeProfile} />}

        {view === "profiles" && (
          <Profiles
            activeProfile={activeProfile}
            onSelectProfile={handleSelectProfile}
            onChatWith={(name: string) => {
              handleSelectProfile(name);
              setView("chat");
            }}
          />
        )}

        {/* Office — lazy mount, keep alive once visited */}
        {officeVisited && (
          <div
            style={{
              display: view === "office" ? "flex" : "none",
              flex: 1,
              flexDirection: "column",
              overflow: "hidden",
            }}
          >
            <Office visible={view === "office"} />
          </div>
        )}

        {view === "models" && <Models />}
        {view === "skills" && <Skills profile={activeProfile} />}
        {view === "skill-review" && <SkillReview />}
        {view === "soul" && <Soul profile={activeProfile} />}
        {view === "memory" && <Memory profile={activeProfile} />}
        {view === "tools" && <Tools profile={activeProfile} />}
        {view === "schedules" && <Schedules profile={activeProfile} />}
        {view === "gateway" && <Gateway profile={activeProfile} />}
        {view === "search" && (
          <Search
            onResumeSession={handleResumeSession}
            currentSessionId={currentSessionId}
          />
        )}

        {/* Settings — always mounted, hidden when not active */}
        <div
          style={{
            display: view === "settings" ? "flex" : "none",
            flex: 1,
            flexDirection: "column",
            overflow: "hidden",
          }}
        >
          <Settings
            profile={activeProfile}
            visible={view === "settings"}
          />
        </div>
        </ErrorBoundary>
      </main>
    </div>
  );
}

export default Layout;
