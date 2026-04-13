// ── Shared Types ────────────────────────────────────────

export interface FieldDef {
  key: string;
  label: string;
  type: string;
  hint: string;
}

export interface SectionDef {
  title: string;
  items: FieldDef[];
}

// ── Providers ───────────────────────────────────────────

export const PROVIDERS = {
  options: [
    { value: "auto", label: "Auto-detect" },
    { value: "openrouter", label: "OpenRouter" },
    { value: "anthropic", label: "Anthropic" },
    { value: "openai", label: "OpenAI" },
    { value: "regolo", label: "Regolo" },
    { value: "custom", label: "Local / Custom" },
  ],

  labels: {
    openrouter: "OpenRouter",
    anthropic: "Anthropic",
    openai: "OpenAI",
    regolo: "Regolo",
    custom: "Custom",
  } as Record<string, string>,

  setup: [
    {
      id: "openrouter",
      name: "OpenRouter",
      desc: "200+ models",
      tag: "Recommended",
      envKey: "OPENROUTER_API_KEY",
      url: "https://openrouter.ai/keys",
      placeholder: "sk-or-v1-...",
      configProvider: "openrouter",
      baseUrl: "https://openrouter.ai/api/v1",
      needsKey: true,
    },
    {
      id: "anthropic",
      name: "Anthropic",
      desc: "Claude models",
      tag: "",
      envKey: "ANTHROPIC_API_KEY",
      url: "https://console.anthropic.com/settings/keys",
      placeholder: "sk-ant-...",
      configProvider: "anthropic",
      baseUrl: "",
      needsKey: true,
    },
    {
      id: "openai",
      name: "OpenAI",
      desc: "GPT models",
      tag: "",
      envKey: "OPENAI_API_KEY",
      url: "https://platform.openai.com/api-keys",
      placeholder: "sk-...",
      configProvider: "openai",
      baseUrl: "",
      needsKey: true,
    },
    {
      id: "regolo",
      name: "Regolo",
      desc: "EU-hosted, OpenAI-compatible",
      tag: "Euraika Partner",
      envKey: "REGOLO_API_KEY",
      url: "https://dashboard.regolo.ai",
      placeholder: "sk-...",
      configProvider: "regolo",
      baseUrl: "https://api.regolo.ai/v1",
      needsKey: true,
    },
    {
      id: "local",
      name: "Local LLM",
      desc: "LM Studio, Ollama, etc.",
      tag: "No API key needed",
      envKey: "",
      url: "",
      placeholder: "",
      configProvider: "custom",
      baseUrl: "http://localhost:1234/v1",
      needsKey: false,
    },
    {
      id: "custom-openai",
      name: "Custom OpenAI-compatible",
      desc: "Your own API endpoint",
      tag: "Advanced",
      envKey: "OPENAI_API_KEY",
      url: "",
      placeholder: "sk-... (leave blank if no auth)",
      configProvider: "custom",
      baseUrl: "",
      needsKey: false,
    },
  ],
};

export const LOCAL_PRESETS = [
  { id: "lmstudio", name: "LM Studio", port: "1234" },
  { id: "ollama", name: "Ollama", port: "11434" },
  { id: "vllm", name: "vLLM", port: "8000" },
  { id: "llamacpp", name: "llama.cpp", port: "8080" },
];

// ── Theme ───────────────────────────────────────────────

export const THEME_OPTIONS = [
  { value: "system" as const, label: "System" },
  { value: "light" as const, label: "Light" },
  { value: "dark" as const, label: "Dark" },
];

export const THEME_STORAGE_KEY = "hermes-theme";

// ── Settings API Key Sections ───────────────────────────

export const SETTINGS_SECTIONS: SectionDef[] = [
  {
    title: "LLM Providers",
    items: [
      { key: "OPENROUTER_API_KEY", label: "OpenRouter API Key", type: "password", hint: "200+ models via OpenRouter (recommended)" },
      { key: "OPENAI_API_KEY", label: "OpenAI API Key", type: "password", hint: "Direct access to GPT models" },
      { key: "ANTHROPIC_API_KEY", label: "Anthropic API Key", type: "password", hint: "Direct access to Claude models" },
      { key: "REGOLO_API_KEY", label: "Regolo API Key", type: "password", hint: "EU-hosted open models (Euraika partner)" },
      { key: "GROQ_API_KEY", label: "Groq API Key", type: "password", hint: "Used for voice tools and STT" },
      { key: "GLM_API_KEY", label: "z.ai / GLM API Key", type: "password", hint: "ZhipuAI GLM models" },
      { key: "KIMI_API_KEY", label: "Kimi / Moonshot API Key", type: "password", hint: "Moonshot AI coding models" },
      { key: "MINIMAX_API_KEY", label: "MiniMax API Key", type: "password", hint: "MiniMax models (global)" },
      { key: "MINIMAX_CN_API_KEY", label: "MiniMax China API Key", type: "password", hint: "MiniMax models (China endpoint)" },
      { key: "OPENCODE_ZEN_API_KEY", label: "OpenCode Zen API Key", type: "password", hint: "Curated GPT, Claude, Gemini models" },
      { key: "OPENCODE_GO_API_KEY", label: "OpenCode Go API Key", type: "password", hint: "Open models (GLM, Kimi, MiniMax)" },
      { key: "HF_TOKEN", label: "Hugging Face Token", type: "password", hint: "20+ open models via HF Inference" },
    ],
  },
  {
    title: "Tool API Keys",
    items: [
      { key: "EXA_API_KEY", label: "Exa Search API Key", type: "password", hint: "AI-native web search" },
      { key: "PARALLEL_API_KEY", label: "Parallel API Key", type: "password", hint: "AI-native web search and extract" },
      { key: "TAVILY_API_KEY", label: "Tavily API Key", type: "password", hint: "Web search for AI agents" },
      { key: "FIRECRAWL_API_KEY", label: "Firecrawl API Key", type: "password", hint: "Web search, extract, and crawl" },
      { key: "FAL_KEY", label: "FAL.ai Key", type: "password", hint: "Image generation with FAL.ai" },
      { key: "HONCHO_API_KEY", label: "Honcho API Key", type: "password", hint: "Cross-session AI user modeling" },
    ],
  },
  {
    title: "Browser & Automation",
    items: [
      { key: "BROWSERBASE_API_KEY", label: "Browserbase API Key", type: "password", hint: "Cloud browser automation" },
      { key: "BROWSERBASE_PROJECT_ID", label: "Browserbase Project ID", type: "text", hint: "Project ID for Browserbase" },
    ],
  },
  {
    title: "Voice & STT",
    items: [
      { key: "VOICE_TOOLS_OPENAI_KEY", label: "OpenAI Voice Key", type: "password", hint: "For Whisper STT and TTS" },
    ],
  },
  {
    title: "Research & Training",
    items: [
      { key: "TINKER_API_KEY", label: "Tinker API Key", type: "password", hint: "RL training service" },
      { key: "WANDB_API_KEY", label: "Weights & Biases Key", type: "password", hint: "Experiment tracking and metrics" },
    ],
  },
];

// ── Gateway Sections ────────────────────────────────────

export const GATEWAY_SECTIONS: SectionDef[] = [
  {
    title: "Messaging Platforms",
    items: [
      { key: "TELEGRAM_BOT_TOKEN", label: "Telegram Bot Token", type: "password", hint: "Get from @BotFather on Telegram" },
      { key: "TELEGRAM_ALLOWED_USERS", label: "Telegram Allowed Users", type: "text", hint: "Comma-separated Telegram user IDs" },
      { key: "DISCORD_BOT_TOKEN", label: "Discord Bot Token", type: "password", hint: "From the Discord Developer Portal" },
      { key: "SLACK_BOT_TOKEN", label: "Slack Bot Token", type: "password", hint: "xoxb-... token from Slack app settings" },
    ],
  },
];

export interface PlatformDef {
  key: string;
  label: string;
  description: string;
  fields: string[];
}

export const GATEWAY_PLATFORMS: PlatformDef[] = [
  { key: "telegram", label: "Telegram", description: "Connect to Telegram via Bot API", fields: ["TELEGRAM_BOT_TOKEN", "TELEGRAM_ALLOWED_USERS"] },
  { key: "discord", label: "Discord", description: "Connect to Discord via bot token", fields: ["DISCORD_BOT_TOKEN"] },
  { key: "slack", label: "Slack", description: "Connect to Slack workspace", fields: ["SLACK_BOT_TOKEN"] },
];
