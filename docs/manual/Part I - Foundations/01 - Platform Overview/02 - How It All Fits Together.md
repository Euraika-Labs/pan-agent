# How It All Fits Together

This note traces a single chat message from the user pressing Enter to the response appearing on screen.

## The full chat round trip

```mermaid
sequenceDiagram
    autonumber
    participant U as User
    participant Chat as Chat.tsx
    participant API as fetchJSON / EventSource
    participant GW as gateway/chat.go<br/>handleChatCompletions
    participant LLM as llm/client.go<br/>ChatStream
    participant Provider as LLM Provider<br/>(Regolo / OpenAI / etc.)
    participant Tool as tools/<tool>.go<br/>Execute
    participant Approval as approval/store.go
    participant DB as storage/<br/>SQLite

    U->>Chat: types message + Enter
    Chat->>API: POST /v1/chat/completions
    API->>GW: HTTP request
    GW->>DB: CreateSession (if new)
    GW->>DB: AddMessage (user)
    GW->>LLM: ChatStream(ctx, msgs, tools)
    LLM->>Provider: POST /chat/completions stream=true
    Provider-->>LLM: SSE chunks + tool_calls
    LLM-->>GW: <-chan StreamEvent
    GW-->>API: SSE chunk events
    API-->>Chat: EventSource onmessage
    Chat-->>U: token-by-token render

    alt tool call detected
        GW->>GW: dangerousTools[name]?
        opt yes
            GW->>Approval: Create
            GW-->>Chat: SSE approval_required
            Chat->>U: shows approval modal
            U->>Chat: approve / deny
            Chat->>API: POST /v1/approvals/{id}
            API->>Approval: Resolve
            Approval-->>GW: unblocks Wait()
        end
        GW->>Tool: Execute(ctx, args)
        Tool-->>GW: result string
        GW-->>Chat: SSE tool_result
        GW->>LLM: next turn with tool result appended
    end

    GW->>DB: AddMessage (assistant)
    GW-->>Chat: SSE done + session_id
```

## What the desktop app does

The Tauri app loads the React bundle and talks to `localhost:8642` via plain `fetch` and `EventSource`. There is no Tauri command bridge for the chat — Tauri only manages the window, the WebView, the sidecar process spawn, and the auto-updater.

Sidecar spawn: Tauri's `externalBin` config in `tauri.conf.json` declares `binaries/pan-agent` (with the platform target triple appended). On app launch, `tauri-plugin-shell` spawns this binary as a child process with a default working directory of the AgentHome. The Go binary opens its database, registers tools, and starts the HTTP server.

## What the Go backend does

The `gateway` package wires everything together:

```mermaid
graph TB
    subgraph "Server struct"
        S["Addr<br/>profile<br/>db<br/>approvals<br/>llmClient<br/>llmMu<br/>gatewayRunning<br/>botCancels"]
    end

    subgraph "Per-request handlers"
        H1["handleChatCompletions"]
        H2["handleConfigGet/Put"]
        H3["handleProfile* (CRUD)"]
        H4["handleGatewayStart/Stop"]
        H5["handleHealth"]
    end

    subgraph "Shared helpers"
        F1["resolveProfile(r)"]
        F2["getLLMClient()"]
        F3["refreshLLMClient(...)"]
        F4["isGatewayRunning()"]
        F5["runAgentLoop(ctx, sid, msg)"]
    end

    subgraph "External calls"
        C1["llm.NewClient<br/>llm.ChatStream"]
        C2["config.ReadProfileEnv<br/>config.SetModelConfig"]
        C3["tools.Get<br/>tools.All"]
        C4["startTelegram<br/>startDiscord<br/>startSlack"]
    end

    H1 --> F2 --> C1
    H1 --> C3
    H2 --> F1
    H2 --> C2
    H2 --> F3
    H3 --> C2
    H4 --> C4
    H4 --> F4
    H5 --> F4
```

## What the bot goroutines do

When you click "Start Gateway" on the Gateway screen:

1. `handleGatewayStart` reads `config.GetPlatformEnabled(profile)` and `config.ReadProfileEnv(profile)`.
2. For each enabled platform with a token configured, it calls `startTelegram(...)` / `startDiscord(...)` / `startSlack(...)`.
3. Each `start*` function returns a `context.CancelFunc`. The Server struct keeps these in `botCancels[platform]`.
4. The bot runs in a goroutine, polling/listening for messages.
5. On incoming message, the bot calls `s.runAgentLoop(ctx, sessionID, text)` — the same agent loop the HTTP chat handler uses, minus the SSE streaming.
6. The bot sends the final response back to the platform.

`handleGatewayStop` iterates `botCancels` and calls each `cancel()` to stop the bot goroutines.

## What the approval system does

Tools listed in `dangerousTools` (terminal, filesystem, code_execution, browser) trigger an approval flow:

1. `executeToolCall` creates an `Approval` record in the in-memory `approval.Store`.
2. An `approval_required` SSE event is sent with the approval ID.
3. The handler blocks on `s.approvals.Wait(approvalID, ctx.Done())`.
4. The frontend modal POSTs `/v1/approvals/{id}` with `{approved: bool}`.
5. `approval.Store.Resolve` unblocks the wait.
6. If approved, the tool runs; otherwise, an error is returned to the model.

Bot conversations skip this entirely — there is no SSE stream for an approval modal to attach to. Bots auto-approve all tools.

## Read next
- [[03 - Top 10 Things Every User Should Know]]
- [[01 - Service Architecture]]
- [[02 - HTTP API Surface]]
