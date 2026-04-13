import { useState, useCallback, useEffect } from "react";
import Chat, { type ChatMessage } from "../Chat/Chat";
import Sessions from "../Sessions/Sessions";
import Profiles from "../Profiles/Profiles";
import Settings from "../Settings/Settings";
import Skills from "../Skills/Skills";
import Soul from "../Soul/Soul";
import Memory from "../Memory/Memory";
import Tools from "../Tools/Tools";
import Gateway from "../Gateway/Gateway";
import Office from "../Office/Office";
import Models from "../Models/Models";
import Schedules from "../Schedules/Schedules";
import Search from "../Search/Search";
import icon from "../../assets/icon.png";
import { fetchJSON } from "../../api";
import {
  MessageSquare as ChatBubble,
  Clock,
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
  | "profiles"
  | "office"
  | "models"
  | "skills"
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
  { view: "profiles", icon: Users, label: "Profiles" },
  { view: "office", icon: Building, label: "Office" },
  { view: "models", icon: Layers, label: "Models" },
  { view: "skills", icon: Puzzle, label: "Skills" },
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
        `/v1/sessions/${encodeURIComponent(sessionId)}/messages`,
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
            {activeProfile === "default" ? "Hermes Agent" : activeProfile}
          </div>
        </div>
      </aside>

      <main className="content">
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
      </main>
    </div>
  );
}

export default Layout;
