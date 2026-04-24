import { useState, useEffect, useRef, useCallback, useMemo, memo } from "react";
import icon from "../../assets/icon.png";
import { AgentMarkdown } from "../../components/AgentMarkdown";
import {
  ApprovalModal,
  type ApprovalRequest,
  type ApprovalResponse,
} from "../../components/ApprovalModal";
import { CostPill } from "../../components/CostPill";
import { BudgetBanner } from "../../components/BudgetBanner";
import {
  Trash2 as Trash,
  Send,
  Square as Stop,
  Plus,
  ChevronDown,
  Search,
  Clock,
  Mail,
  Code,
  ChartLine,
  Bell,
  Slash,
} from "lucide-react";
import { fetchJSON, streamSSE, setSessionBudget } from "../../api";
import { PROVIDERS } from "../../constants";

// ── Slash Commands ──────────────────────────────────────

interface SlashCommand {
  name: string;
  description: string;
  category: "chat" | "agent" | "tools" | "info";
  /** If true, the command is handled locally instead of sent to the backend */
  local?: boolean;
}

const SLASH_COMMANDS: SlashCommand[] = [
  // Chat control
  {
    name: "/new",
    description: "Start a new chat",
    category: "chat",
    local: true,
  },
  {
    name: "/clear",
    description: "Clear conversation history",
    category: "chat",
    local: true,
  },
  // Agent commands (sent to backend)
  {
    name: "/btw",
    description: "Ask a side question without affecting context",
    category: "agent",
  },
  {
    name: "/approve",
    description: "Approve a pending action",
    category: "agent",
  },
  { name: "/deny", description: "Deny a pending action", category: "agent" },
  {
    name: "/status",
    description: "Show current agent status",
    category: "agent",
  },
  {
    name: "/reset",
    description: "Reset conversation context",
    category: "agent",
  },
  {
    name: "/compact",
    description: "Compact and summarize the conversation",
    category: "agent",
  },
  { name: "/undo", description: "Undo the last action", category: "agent" },
  {
    name: "/retry",
    description: "Retry the last failed action",
    category: "agent",
  },
  // Tools & capabilities
  { name: "/web", description: "Search the web", category: "tools" },
  { name: "/image", description: "Generate an image", category: "tools" },
  { name: "/browse", description: "Browse a URL", category: "tools" },
  { name: "/code", description: "Write or execute code", category: "tools" },
  { name: "/file", description: "Read or write files", category: "tools" },
  { name: "/shell", description: "Run a shell command", category: "tools" },
  // Info
  {
    name: "/help",
    description: "Show available commands and help",
    category: "info",
  },
  { name: "/tools", description: "List available tools", category: "info" },
  { name: "/skills", description: "List installed skills", category: "info" },
  {
    name: "/model",
    description: "Show or switch the current model",
    category: "info",
  },
  { name: "/memory", description: "Show agent memory", category: "info" },
  { name: "/persona", description: "Show current persona", category: "info" },
  { name: "/version", description: "Show agent version", category: "info" },
];

function AgentAvatar({ size = 30 }: { size?: number }): React.JSX.Element {
  return (
    <div className="chat-avatar chat-avatar-agent">
      <img src={icon} width={size} height={size} alt="" />
    </div>
  );
}

export { AgentMarkdown };

const APPROVAL_RE =
  /⚠️.*dangerous|requires? (your )?approval|\/approve.*\/deny|do you want (me )?to (proceed|continue|run|execute)/i;

interface MessageRowProps {
  msg: ChatMessage;
  isLast: boolean;
  isLoading: boolean;
  onApprove: () => void;
  onDeny: () => void;
}

const MessageRow = memo(function MessageRow({
  msg,
  isLast,
  isLoading,
  onApprove,
  onDeny,
}: MessageRowProps): React.JSX.Element {
  return (
    <div className={`chat-message chat-message-${msg.role}`}>
      {msg.role === "user" ? (
        <div className="chat-avatar chat-avatar-user">U</div>
      ) : (
        <AgentAvatar />
      )}
      <div className={`chat-bubble chat-bubble-${msg.role}`}>
        {msg.role === "agent" ? (
          <AgentMarkdown>{msg.content}</AgentMarkdown>
        ) : (
          msg.content
        )}
      </div>
      {msg.role === "agent" &&
        !isLoading &&
        isLast &&
        APPROVAL_RE.test(msg.content) && (
          <div className="chat-approval-bar">
            <button
              className="chat-approval-btn chat-approve"
              onClick={onApprove}
            >
              Approve
            </button>
            <button className="chat-approval-btn chat-deny" onClick={onDeny}>
              Deny
            </button>
          </div>
        )}
    </div>
  );
});

export interface ChatMessage {
  id: string;
  role: "user" | "agent";
  content: string;
}

interface ModelGroup {
  provider: string;
  providerLabel: string;
  models: { provider: string; model: string; label: string; baseUrl: string }[];
}

// ── Backend API types ────────────────────────────────────

interface ModelConfig {
  provider: string;
  model: string;
  baseUrl: string;
}

interface ModelInfo {
  provider: string;
  model: string;
  name: string;
  baseUrl?: string;
}

interface MemoryState {
  memory: { exists: boolean; content: string };
  stats: { totalSessions: number; totalMessages: number };
}

interface ToolsetInfo {
  label: string;
  description: string;
  enabled: boolean;
}

interface SkillInfo {
  name: string;
  category: string;
  description: string;
}

interface ChatProps {
  messages: ChatMessage[];
  setMessages: React.Dispatch<React.SetStateAction<ChatMessage[]>>;
  sessionId: string | null;
  profile?: string;
  onSessionStarted?: () => void;
  onNewChat?: () => void;
}

function Chat({
  messages,
  setMessages,
  sessionId,
  profile: _profile,
  onSessionStarted,
  onNewChat,
}: ChatProps): React.JSX.Element {
  const [input, setInput] = useState("");
  const [isLoading, setIsLoading] = useState(false);
  const [agentSessionId, setAgentSessionId] = useState<string | null>(null);
  const [toolProgress, setToolProgress] = useState<string | null>(null);
  const [approvalRequest, setApprovalRequest] =
    useState<ApprovalRequest | null>(null);

  // Ref to the streamSSE cleanup function for the active stream
  const abortStreamRef = useRef<(() => void) | null>(null);

  // ── Approval response ──────────────────────────────────
  const handleApprovalResponse = useCallback(
    async (id: string, response: ApprovalResponse): Promise<void> => {
      const request = approvalRequest;
      setApprovalRequest(null);
      if (!request || request.id !== id) return;

      // Map ApprovalResponse to the backend's { approved: boolean } shape.
      // "preview" is treated as denial at the HTTP level (tool is not executed).
      const approved =
        response === "approved" || response === "preview" ? true : false;
      // For level-2 approvals the UX already required the user to type the
      // confirmation phrase before the modal fires onResponse, so we just
      // forward the boolean.
      try {
        await fetchJSON(`/v1/approvals/${id}`, {
          method: "POST",
          body: JSON.stringify({ approved }),
        });
      } catch (err) {
        console.error("[Chat] approval response failed:", err);
      }
    },
    [approvalRequest],
  );

  const [usage, setUsage] = useState<{
    promptTokens: number;
    completionTokens: number;
    totalTokens: number;
  } | null>(null);
  const [budgetState, setBudgetState] = useState<{
    type: "warning" | "exceeded" | null;
    costUsed: number;
    costCap: number;
  }>({ type: null, costUsed: 0, costCap: 0 });
  const messagesEndRef = useRef<HTMLDivElement>(null);
  const messagesContainerRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const isLoadingRef = useRef(false);
  const userScrolledUpRef = useRef(false);

  // Model picker state
  const [currentModel, setCurrentModel] = useState("");
  const [currentProvider, setCurrentProvider] = useState("auto");
  const [currentBaseUrl, setCurrentBaseUrl] = useState("");
  const [modelGroups, setModelGroups] = useState<ModelGroup[]>([]);
  const [showModelPicker, setShowModelPicker] = useState(false);
  const [customModelInput, setCustomModelInput] = useState("");
  const pickerRef = useRef<HTMLDivElement>(null);

  // Slash command menu state
  const [slashMenuOpen, setSlashMenuOpen] = useState(false);
  const [slashFilter, setSlashFilter] = useState("");
  const [slashSelectedIndex, setSlashSelectedIndex] = useState(0);
  const slashMenuRef = useRef<HTMLDivElement>(null);

  // Keep ref in sync for use in callbacks
  isLoadingRef.current = isLoading;

  // Filtered slash commands based on current input
  const filteredSlashCommands = useMemo(
    () =>
      slashMenuOpen
        ? SLASH_COMMANDS.filter((cmd) =>
            cmd.name.toLowerCase().startsWith(slashFilter.toLowerCase()),
          )
        : [],
    [slashMenuOpen, slashFilter],
  );

  const scrollToBottom = useCallback((force?: boolean) => {
    if (!force && userScrolledUpRef.current) return;
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
  }, []);

  // Track whether the user has scrolled away from the bottom
  useEffect(() => {
    const container = messagesContainerRef.current;
    if (!container) return;
    function handleScroll(): void {
      const el = container!;
      const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 60;
      userScrolledUpRef.current = !atBottom;
    }
    container.addEventListener("scroll", handleScroll, { passive: true });
    return () => container.removeEventListener("scroll", handleScroll);
  }, []);

  // Reset agent session when messages are cleared (new chat)
  useEffect(() => {
    if (messages.length === 0) {
      setAgentSessionId(null);
    }
  }, [messages]);

  const loadModelConfig = useCallback(async (): Promise<void> => {
    try {
      // GET /v1/config returns the full env map; model config lives there too.
      // GET /v1/models returns the list of available models.
      const [configEnv, savedModels] = await Promise.all([
        fetchJSON<Record<string, string>>("/v1/config"),
        fetchJSON<ModelInfo[]>("/v1/models"),
      ]);

      // The Go backend stores model config as PROVIDER / MODEL / BASE_URL env vars.
      const modelConfig: ModelConfig = {
        provider: configEnv["PROVIDER"] || "auto",
        model: configEnv["MODEL"] || "",
        baseUrl: configEnv["BASE_URL"] || "",
      };

      setCurrentModel(modelConfig.model);
      setCurrentProvider(modelConfig.provider);
      setCurrentBaseUrl(modelConfig.baseUrl);

      // Group saved models by provider
      const groupMap = new Map<string, ModelGroup>();
      for (const saved of savedModels ?? []) {
        if (!groupMap.has(saved.provider)) {
          groupMap.set(saved.provider, {
            provider: saved.provider,
            providerLabel: PROVIDERS.labels[saved.provider] || saved.provider,
            models: [],
          });
        }
        groupMap.get(saved.provider)!.models.push({
          provider: saved.provider,
          model: saved.model,
          label: saved.name,
          baseUrl: saved.baseUrl || "",
        });
      }
      setModelGroups(Array.from(groupMap.values()));
    } catch (err) {
      console.error("[Chat] loadModelConfig failed:", err);
    }
  }, []);

  // Load model config and build available models list on mount
  useEffect(() => {
    loadModelConfig();
  }, [loadModelConfig]);

  // Close picker on click outside
  useEffect(() => {
    if (!showModelPicker) return;
    function handleClickOutside(e: MouseEvent): void {
      if (pickerRef.current && !pickerRef.current.contains(e.target as Node)) {
        setShowModelPicker(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, [showModelPicker]);

  // Close slash menu on click outside
  useEffect(() => {
    if (!slashMenuOpen) return;
    function handleClickOutside(e: MouseEvent): void {
      if (
        slashMenuRef.current &&
        !slashMenuRef.current.contains(e.target as Node)
      ) {
        setSlashMenuOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, [slashMenuOpen]);

  // Scroll active slash menu item into view
  useEffect(() => {
    if (!slashMenuOpen) return;
    const active = slashMenuRef.current?.querySelector(
      ".slash-menu-item-active",
    );
    active?.scrollIntoView({ block: "nearest" });
  }, [slashSelectedIndex, slashMenuOpen]);

  async function selectModel(
    provider: string,
    model: string,
    baseUrl: string,
  ): Promise<void> {
    // POST /v1/models to update the active model config
    await fetchJSON("/v1/models", {
      method: "POST",
      body: JSON.stringify({ provider, model, base_url: baseUrl }),
    });
    setCurrentModel(model);
    setCurrentProvider(provider);
    setCurrentBaseUrl(baseUrl);
    setShowModelPicker(false);
    setCustomModelInput("");
  }

  async function handleCustomModelSubmit(): Promise<void> {
    const model = customModelInput.trim();
    if (!model) return;
    await selectModel(
      currentProvider === "auto" ? "auto" : currentProvider,
      model,
      currentBaseUrl,
    );
  }

  useEffect(() => {
    scrollToBottom();
  }, [messages, scrollToBottom]);

  // Reset scroll lock when user sends a new message
  const prevMessageCountRef = useRef(messages.length);
  useEffect(() => {
    const prevCount = prevMessageCountRef.current;
    prevMessageCountRef.current = messages.length;
    // A new user message was just added — re-engage auto-scroll
    if (
      messages.length > prevCount &&
      messages[messages.length - 1]?.role === "user"
    ) {
      userScrolledUpRef.current = false;
      scrollToBottom(true);
    }
  }, [messages, scrollToBottom]);

  useEffect(() => {
    if (!isLoading) {
      inputRef.current?.focus();
    }
  }, [isLoading]);

  // Keyboard shortcut: Cmd+N for new chat
  useEffect(() => {
    function handleKeyDown(e: KeyboardEvent): void {
      if ((e.metaKey || e.ctrlKey) && e.key === "n") {
        e.preventDefault();
        if (onNewChat) onNewChat();
      }
    }
    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [onNewChat]);

  /**
   * Send a message to the backend via SSE streaming.
   * Returns a promise that resolves when the stream ends or errors.
   */
  const sendToBackend = useCallback((
    text: string,
    history: { role: string; content: string }[],
    currentSessionId?: string,
  ): void => {
    // Build the message list: history + the new user message
    const messages = [
      ...history.map((m) => ({ role: m.role === "agent" ? "assistant" : m.role, content: m.content })),
      { role: "user", content: text },
    ];

    const stop = streamSSE(
      "/v1/chat/completions",
      {
        messages,
        stream: true,
        ...(currentSessionId ? { session_id: currentSessionId } : {}),
      },
      (evt) => {
        const type = evt.type as string;

        if (type === "chunk") {
          const chunk = (evt.content as string) ?? "";
          setMessages((prev) => {
            const last = prev[prev.length - 1];
            if (last && last.role === "agent") {
              return [
                ...prev.slice(0, -1),
                { ...last, content: last.content + chunk },
              ];
            }
            if (!chunk || !chunk.trim()) return prev;
            return [
              ...prev,
              { id: `agent-${Date.now()}`, role: "agent", content: chunk },
            ];
          });
        } else if (type === "tool_call") {
          const toolCall = evt.tool_call as { function?: { name?: string } } | undefined;
          const toolName = toolCall?.function?.name ?? "tool";
          setToolProgress(`Running ${toolName}…`);
        } else if (type === "tool_result") {
          setToolProgress(null);
        } else if (type === "approval_required") {
          // The backend sends approval_id + tool_call details
          const approvalId = (evt.approval_id as string) ?? "";
          const tc = evt.tool_call as {
            function?: { name?: string; arguments?: string };
          } | undefined;
          const cmdName = tc?.function?.name ?? "unknown command";
          const cmdArgs = tc?.function?.arguments ?? "";

          setApprovalRequest({
            id: approvalId,
            level: 1,
            command: `${cmdName}(${cmdArgs})`,
            description: `The agent wants to run: ${cmdName}`,
            patternKey: cmdName,
            reason: "",
            previewAvailable: false,
          });
        } else if (type === "usage") {
          const u = evt.usage as {
            prompt_tokens?: number;
            completion_tokens?: number;
            total_tokens?: number;
          } | undefined;
          if (u) {
            setUsage((prev) => ({
              promptTokens:
                (prev?.promptTokens || 0) + (u.prompt_tokens || 0),
              completionTokens:
                (prev?.completionTokens || 0) + (u.completion_tokens || 0),
              totalTokens:
                (prev?.totalTokens || 0) + (u.total_tokens || 0),
            }));
          }
        } else if (type === "done") {
          const sid = evt.session_id as string | undefined;
          if (sid) setAgentSessionId(sid);
          setToolProgress(null);
          setIsLoading(false);
          abortStreamRef.current = null;
        } else if (type === "error") {
          const errMsg = (evt.error as string) ?? "Unknown error";
          setMessages((prev) => [
            ...prev,
            {
              id: `error-${Date.now()}`,
              role: "agent",
              content: `Error: ${errMsg}`,
            },
          ]);
          setToolProgress(null);
          setIsLoading(false);
          abortStreamRef.current = null;
        } else if (type === "budget.warning") {
          setBudgetState({
            type: "warning",
            costUsed: (evt.cost_used as number) ?? 0,
            costCap: (evt.cost_cap as number) ?? 0,
          });
        } else if (type === "budget.exceeded") {
          setBudgetState({
            type: "exceeded",
            costUsed: (evt.cost_used as number) ?? 0,
            costCap: (evt.cost_cap as number) ?? 0,
          });
        }
      },
    );

    abortStreamRef.current = stop;
  }, [setMessages]);

  async function handleSend(): Promise<void> {
    const text = input.trim();
    if (!text || isLoading) return;

    setSlashMenuOpen(false);
    setInput("");

    if (inputRef.current) {
      inputRef.current.style.height = "auto";
    }

    // Intercept slash commands that can be handled locally
    if (text.startsWith("/")) {
      const cmd = text.split(/\s+/)[0].toLowerCase();
      const isLocal = SLASH_COMMANDS.some(
        (c) => c.name === cmd && (c.local || c.category === "info"),
      );
      if (isLocal) {
        if (cmd !== "/new" && cmd !== "/clear") {
          setMessages((prev) => [
            ...prev,
            { id: `user-${Date.now()}`, role: "user", content: text },
          ]);
        }
        await executeLocalCommand(text);
        return;
      }
    }

    setIsLoading(true);
    setMessages((prev) => [
      ...prev,
      { id: `user-${Date.now()}`, role: "user", content: text },
    ]);
    onSessionStarted?.();

    sendToBackend(
      text,
      messages.map((m) => ({ role: m.role, content: m.content })),
      agentSessionId || undefined,
    );
  }

  async function handleQuickAsk(): Promise<void> {
    const text = input.trim();
    if (!text || isLoading) return;
    // /btw sends an ephemeral side question that doesn't pollute conversation context
    setInput("");
    if (inputRef.current) inputRef.current.style.height = "auto";
    setIsLoading(true);
    setMessages((prev) => [
      ...prev,
      { id: `user-btw-${Date.now()}`, role: "user", content: `💭 ${text}` },
    ]);

    sendToBackend(
      `/btw ${text}`,
      messages.map((m) => ({ role: m.role, content: m.content })),
      agentSessionId || undefined,
    );
  }

  function handleKeyDown(e: React.KeyboardEvent): void {
    // Slash menu keyboard navigation
    if (slashMenuOpen && filteredSlashCommands.length > 0) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        setSlashSelectedIndex((i) =>
          i < filteredSlashCommands.length - 1 ? i + 1 : 0,
        );
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        setSlashSelectedIndex((i) =>
          i > 0 ? i - 1 : filteredSlashCommands.length - 1,
        );
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        handleSlashSelect(filteredSlashCommands[slashSelectedIndex]);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setSlashMenuOpen(false);
        return;
      }
    }

    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      handleSend();
    }
  }

  function handleInputChange(e: React.ChangeEvent<HTMLTextAreaElement>): void {
    const value = e.target.value;
    setInput(value);

    // Defer reflow-triggering resize to next frame
    const target = e.target;
    requestAnimationFrame(() => {
      target.style.height = "auto";
      target.style.height = `${Math.min(target.scrollHeight, 120)}px`;
    });

    // Slash command detection: open menu when input starts with /
    if (value.startsWith("/") && !value.includes(" ")) {
      const query = value.split(" ")[0];
      setSlashMenuOpen(true);
      setSlashFilter(query);
      setSlashSelectedIndex(0);
    } else if (slashMenuOpen) {
      setSlashMenuOpen(false);
    }
  }

  /** Push a fake agent message into the chat (for locally-handled commands). */
  function pushLocalResponse(content: string): void {
    setMessages((prev) => [
      ...prev,
      { id: `agent-local-${Date.now()}`, role: "agent", content },
    ]);
  }

  /**
   * Execute a slash command that can be resolved entirely in the frontend.
   * Returns true if handled, false if the command should go to the backend.
   */
  async function executeLocalCommand(cmdText: string): Promise<boolean> {
    const parts = cmdText.trim().split(/\s+/);
    const cmd = parts[0].toLowerCase();

    switch (cmd) {
      case "/new":
        onNewChat?.();
        return true;

      case "/clear":
        handleClear();
        return true;

      case "/model": {
        try {
          const configEnv = await fetchJSON<Record<string, string>>("/v1/config");
          const model = configEnv["MODEL"] || "Not set";
          const prov = configEnv["PROVIDER"] || "auto";
          const base = configEnv["BASE_URL"] || "";
          pushLocalResponse(
            `**Current model:** \`${model}\`\n**Provider:** ${prov}${base ? `\n**Base URL:** ${base}` : ""}`,
          );
        } catch (err) {
          pushLocalResponse(`Error fetching model config: ${err}`);
        }
        return true;
      }

      case "/memory": {
        try {
          const mem = await fetchJSON<MemoryState>("/v1/memory");
          const lines: string[] = ["**Agent Memory**\n"];
          if (mem.memory.exists && mem.memory.content.trim()) {
            lines.push(mem.memory.content.trim());
          } else {
            lines.push("_No memory entries yet._");
          }
          lines.push(
            `\n**Stats:** ${mem.stats.totalSessions} sessions, ${mem.stats.totalMessages} messages`,
          );
          pushLocalResponse(lines.join("\n"));
        } catch (err) {
          pushLocalResponse(`Error fetching memory: ${err}`);
        }
        return true;
      }

      case "/tools": {
        try {
          const tools = await fetchJSON<ToolsetInfo[]>("/v1/tools");
          if (!tools.length) {
            pushLocalResponse("No toolsets found.");
          } else {
            const rows = tools
              .map(
                (t) =>
                  `- **${t.label}** — ${t.description} ${t.enabled ? "*(enabled)*" : "*(disabled)*"}`,
              )
              .join("\n");
            pushLocalResponse(`**Available Toolsets**\n\n${rows}`);
          }
        } catch (err) {
          pushLocalResponse(`Error fetching tools: ${err}`);
        }
        return true;
      }

      case "/skills": {
        try {
          const skills = await fetchJSON<SkillInfo[]>("/v1/skills");
          if (!skills.length) {
            pushLocalResponse("No skills installed.");
          } else {
            const rows = skills
              .map((s) => `- **${s.name}** (${s.category}) — ${s.description}`)
              .join("\n");
            pushLocalResponse(`**Installed Skills**\n\n${rows}`);
          }
        } catch (err) {
          pushLocalResponse(`Error fetching skills: ${err}`);
        }
        return true;
      }

      case "/persona": {
        try {
          const res = await fetchJSON<{ persona: string }>("/v1/persona");
          const soul = res.persona || "";
          pushLocalResponse(
            soul.trim()
              ? `**Current Persona**\n\n${soul.trim()}`
              : "_No persona configured._",
          );
        } catch (err) {
          pushLocalResponse(`Error fetching persona: ${err}`);
        }
        return true;
      }

      case "/version": {
        // The HTTP backend doesn't expose a /v1/version endpoint yet;
        // fall back to a health check and display what we can.
        try {
          await fetchJSON("/v1/health");
          pushLocalResponse("**Pan Agent:** running (version info not available via HTTP API)");
        } catch {
          pushLocalResponse("**Pan Agent:** unreachable");
        }
        return true;
      }

      case "/help": {
        const grouped: Record<string, SlashCommand[]> = {};
        for (const c of SLASH_COMMANDS) {
          (grouped[c.category] ||= []).push(c);
        }
        const categoryLabels: Record<string, string> = {
          chat: "Chat",
          agent: "Agent",
          tools: "Tools",
          info: "Info",
        };
        let md = "**Available Commands**\n";
        for (const cat of ["chat", "agent", "tools", "info"]) {
          if (!grouped[cat]) continue;
          md += `\n**${categoryLabels[cat]}**\n`;
          for (const c of grouped[cat]) {
            md += `\`${c.name}\` — ${c.description}\n`;
          }
        }
        pushLocalResponse(md);
        return true;
      }

      default:
        return false;
    }
  }

  function handleSlashSelect(cmd: SlashCommand): void {
    setSlashMenuOpen(false);
    setInput("");
    if (inputRef.current) inputRef.current.style.height = "auto";

    // Commands that need no arguments — execute immediately
    if (cmd.local || ["info"].includes(cmd.category)) {
      // Show as user message for non-UI commands
      if (cmd.name !== "/new" && cmd.name !== "/clear") {
        setMessages((prev) => [
          ...prev,
          { id: `user-${Date.now()}`, role: "user", content: cmd.name },
        ]);
      }
      executeLocalCommand(cmd.name);
      return;
    }

    // For backend commands that take arguments, insert command + space
    const newValue = cmd.name + " ";
    setInput(newValue);
    inputRef.current?.focus();
  }

  function handleAbort(): void {
    // Call the streamSSE cleanup to abort the active stream
    if (abortStreamRef.current) {
      abortStreamRef.current();
      abortStreamRef.current = null;
    }
    setIsLoading(false);
    // Refocus input after aborting
    setTimeout(() => inputRef.current?.focus(), 50);
  }

  function handleClear(): void {
    // Abort any in-flight stream before clearing
    if (isLoading) {
      if (abortStreamRef.current) {
        abortStreamRef.current();
        abortStreamRef.current = null;
      }
      setIsLoading(false);
    }
    setMessages([]);
    setAgentSessionId(null);
    setUsage(null);
    setToolProgress(null);
    setBudgetState({ type: null, costUsed: 0, costCap: 0 });
  }

  const handleApprove = useCallback(() => {
    setInput("");
    setIsLoading(true);
    setMessages((prev) => [
      ...prev,
      { id: `user-approve-${Date.now()}`, role: "user", content: "/approve" },
    ]);
    const history = messages.map((m) => ({ role: m.role, content: m.content }));
    sendToBackend("/approve", history, agentSessionId || undefined);
  }, [agentSessionId, setMessages, messages, sendToBackend]);

  const handleDeny = useCallback(() => {
    setInput("");
    setIsLoading(true);
    setMessages((prev) => [
      ...prev,
      { id: `user-deny-${Date.now()}`, role: "user", content: "/deny" },
    ]);
    const history = messages.map((m) => ({ role: m.role, content: m.content }));
    sendToBackend("/deny", history, agentSessionId || undefined);
  }, [agentSessionId, setMessages, messages, sendToBackend]);

  const visibleMessages = useMemo(
    () => messages.filter((m) => m.content.trim()),
    [messages],
  );

  const displayModel = useMemo(
    () =>
      currentModel
        ? currentModel.split("/").pop() || currentModel
        : currentProvider === "auto"
          ? "Auto"
          : "No model set",
    [currentModel, currentProvider],
  );

  const lastMessageIsAgent = useMemo(
    () => messages.length > 0 && messages[messages.length - 1].role === "agent",
    [messages],
  );

  return (
    <div className="chat-container">
      <div className="chat-header">
        <div className="chat-header-left">
          <div className="chat-header-title">
            {sessionId ? `Session ${sessionId.slice(-6)}` : "New Chat"}
          </div>
          {usage && (
            <span
              className="chat-token-counter"
              title={`Prompt: ${usage.promptTokens} | Completion: ${usage.completionTokens}`}
            >
              {usage.totalTokens.toLocaleString()} tokens
            </span>
          )}
          {(budgetState.costUsed > 0 || budgetState.costCap > 0) && (
            <CostPill
              costUsed={budgetState.costUsed}
              costCap={budgetState.costCap}
            />
          )}
        </div>
        <div className="chat-header-actions">
          {onNewChat && (
            <button
              className="btn-ghost chat-clear-btn"
              onClick={onNewChat}
              title="New chat (Cmd+N)"
            >
              <Plus size={16} />
            </button>
          )}
          {messages.length > 0 && (
            <button
              className="btn-ghost chat-clear-btn"
              onClick={handleClear}
              title="Clear chat"
            >
              <Trash size={16} />
            </button>
          )}
        </div>
      </div>

      <div className="chat-messages" ref={messagesContainerRef}>
        {messages.length === 0 ? (
          <div className="chat-empty">
            <div className="chat-empty-icon">
              <img src={icon} width={64} height={64} alt="" />
            </div>
            <div className="chat-empty-text">How can I help you today?</div>
            <div className="chat-empty-hint">
              Ask me to write code, answer questions, search the web, and more
            </div>
            <div className="chat-empty-suggestions">
              <button
                className="chat-suggestion"
                onClick={() => {
                  setInput("Search the web for today's top tech news");
                  inputRef.current?.focus();
                }}
              >
                <Search size={16} />
                Search the web
              </button>
              <button
                className="chat-suggestion"
                onClick={() => {
                  setInput("Set a reminder to check emails every day at 9 AM");
                  inputRef.current?.focus();
                }}
              >
                <Bell size={16} />
                Set a reminder
              </button>
              <button
                className="chat-suggestion"
                onClick={() => {
                  setInput("Read my latest emails and summarize them");
                  inputRef.current?.focus();
                }}
              >
                <Mail size={16} />
                Summarize emails
              </button>
              <button
                className="chat-suggestion"
                onClick={() => {
                  setInput(
                    "Write a Python script to rename all files in a folder",
                  );
                  inputRef.current?.focus();
                }}
              >
                <Code size={16} />
                Write a script
              </button>
              <button
                className="chat-suggestion"
                onClick={() => {
                  setInput(
                    "Schedule a cron job to back up my database every night",
                  );
                  inputRef.current?.focus();
                }}
              >
                <Clock size={16} />
                Schedule a cron job
              </button>
              <button
                className="chat-suggestion"
                onClick={() => {
                  setInput("Analyze this CSV file and show key insights");
                  inputRef.current?.focus();
                }}
              >
                <ChartLine size={16} />
                Analyze data
              </button>
            </div>
          </div>
        ) : (
          visibleMessages.map((msg, i) => (
            <MessageRow
              key={msg.id}
              msg={msg}
              isLast={i === visibleMessages.length - 1}
              isLoading={isLoading}
              onApprove={handleApprove}
              onDeny={handleDeny}
            />
          ))
        )}

        {isLoading && !lastMessageIsAgent && (
          <div className="chat-message chat-message-agent">
            <AgentAvatar />
            <div className="chat-bubble chat-bubble-agent">
              {toolProgress ? (
                <div className="chat-tool-progress">{toolProgress}</div>
              ) : (
                <div className="chat-typing">
                  <span className="chat-typing-dot" />
                  <span className="chat-typing-dot" />
                  <span className="chat-typing-dot" />
                </div>
              )}
            </div>
          </div>
        )}

        {isLoading && toolProgress && lastMessageIsAgent && (
          <div className="chat-tool-progress-inline">{toolProgress}</div>
        )}

        <div ref={messagesEndRef} />
      </div>

      <BudgetBanner
        type={budgetState.type}
        costUsed={budgetState.costUsed}
        costCap={budgetState.costCap}
        onIncrease={async () => {
          if (agentSessionId && budgetState.costCap > 0) {
            try {
              await setSessionBudget(agentSessionId, budgetState.costCap * 2);
              setBudgetState((prev) => ({
                ...prev,
                type: null,
                costCap: prev.costCap * 2,
              }));
            } catch (err) {
              console.error("[Chat] setSessionBudget failed:", err);
            }
          }
        }}
        onDismiss={() => setBudgetState((prev) => ({ ...prev, type: null }))}
      />
      <div className="chat-input-area">
        {slashMenuOpen && filteredSlashCommands.length > 0 && (
          <div className="slash-menu" ref={slashMenuRef}>
            <div className="slash-menu-header">
              <Slash size={12} />
              Commands
            </div>
            <div className="slash-menu-list">
              {filteredSlashCommands.map((cmd, i) => (
                <button
                  key={cmd.name}
                  className={`slash-menu-item ${i === slashSelectedIndex ? "slash-menu-item-active" : ""}`}
                  onMouseEnter={() => setSlashSelectedIndex(i)}
                  onClick={() => handleSlashSelect(cmd)}
                >
                  <span className="slash-menu-item-name">{cmd.name}</span>
                  <span className="slash-menu-item-desc">
                    {cmd.description}
                  </span>
                </button>
              ))}
            </div>
          </div>
        )}
        <div className="chat-input-wrapper">
          <textarea
            ref={inputRef}
            className="chat-input"
            placeholder="Type a message... (Shift+Enter for new line)"
            value={input}
            onChange={handleInputChange}
            onKeyDown={handleKeyDown}
            rows={1}
            disabled={isLoading}
            autoFocus
          />
          {isLoading ? (
            <button
              className="chat-send-btn chat-stop-btn"
              onClick={handleAbort}
              title="Stop"
            >
              <Stop size={14} />
            </button>
          ) : (
            <>
              {input.trim() && agentSessionId && (
                <button
                  className="chat-btw-btn"
                  onClick={handleQuickAsk}
                  title="Quick Ask (/btw) — side question that won't affect conversation context"
                >
                  💭
                </button>
              )}
              <button
                className="chat-send-btn"
                onClick={handleSend}
                disabled={!input.trim()}
                title="Send"
              >
                <Send size={16} />
              </button>
            </>
          )}
        </div>

        <div className="chat-model-bar" ref={pickerRef}>
          <button
            className="chat-model-trigger"
            onClick={() => {
              if (!showModelPicker) loadModelConfig();
              setShowModelPicker(!showModelPicker);
            }}
          >
            <span className="chat-model-name">{displayModel}</span>
            <ChevronDown size={12} />
          </button>

          {showModelPicker && (
            <div className="chat-model-dropdown">
              {modelGroups.map((group) => (
                <div key={group.provider} className="chat-model-group">
                  <div className="chat-model-group-label">
                    {group.providerLabel}
                  </div>
                  {group.models.map((m) => (
                    <button
                      key={`${m.provider}:${m.model}`}
                      className={`chat-model-option ${currentModel === m.model && currentProvider === m.provider ? "active" : ""}`}
                      onClick={() =>
                        selectModel(m.provider, m.model, m.baseUrl)
                      }
                    >
                      <span className="chat-model-option-label">{m.label}</span>
                      <span className="chat-model-option-id">{m.model}</span>
                    </button>
                  ))}
                </div>
              ))}
              <div className="chat-model-group">
                <div className="chat-model-group-label">Custom</div>
                <div className="chat-model-custom">
                  <input
                    className="chat-model-custom-input"
                    type="text"
                    value={customModelInput}
                    onChange={(e) => setCustomModelInput(e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") handleCustomModelSubmit();
                    }}
                    placeholder="Type model name..."
                  />
                </div>
              </div>
            </div>
          )}
        </div>
      </div>
      <ApprovalModal
        request={approvalRequest}
        onResponse={handleApprovalResponse}
      />
    </div>
  );
}

export default Chat;
