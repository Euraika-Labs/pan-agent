/**
 * HTTP client for the Pan-Agent Go backend.
 *
 * The backend listens on localhost:8642.
 * REST calls use fetchJSON; streaming responses (chat) use streamSSE.
 */
import { invoke } from "@tauri-apps/api/core";
import { listen, type UnlistenFn } from "@tauri-apps/api/event";
import { isTauri } from "./lib/tauri";

export const API_BASE =
  import.meta.env.VITE_API_BASE ?? "http://127.0.0.1:8642";
const NETWORK_RETRY_DELAYS_MS = [150, 350, 750, 1500];

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function isNetworkFetchError(err: unknown): boolean {
  return err instanceof TypeError && /fetch/i.test(err.message);
}

interface ApiFetchResponse {
  status: number;
  body: string;
}

interface ApiStreamEvent {
  kind: "status" | "chunk" | "error" | "done";
  status?: number;
  data?: string;
  error?: string;
}

async function browserFetch(path: string, options?: RequestInit): Promise<Response> {
  let res: Response;
  for (let attempt = 0; ; attempt += 1) {
    try {
      res = await fetch(`${API_BASE}${path}`, {
        headers: { "Content-Type": "application/json", ...options?.headers },
        ...options,
      });
      break;
    } catch (err) {
      const retryable =
        isNetworkFetchError(err) && attempt < NETWORK_RETRY_DELAYS_MS.length;
      if (!retryable) {
        throw new Error(
          `Could not reach local Pan API at ${API_BASE}${path}. ${err instanceof Error ? err.message : String(err)}`,
        );
      }
      await sleep(NETWORK_RETRY_DELAYS_MS[attempt]);
    }
  }
  return res;
}

async function tauriFetch(path: string, options?: RequestInit): Promise<ApiFetchResponse> {
  return invoke<ApiFetchResponse>("api_fetch", {
    method: options?.method ?? "GET",
    path,
    body: typeof options?.body === "string" ? options.body : null,
  });
}

// ---------------------------------------------------------------------------
// JSON helper
// ---------------------------------------------------------------------------

/**
 * Fetch a JSON endpoint on the backend.
 *
 * @param path    - URL path, e.g. "/api/conversations"
 * @param options - Standard fetch RequestInit (method, headers, body, …)
 * @returns       Parsed JSON typed as T
 * @throws        Error with the response body text on non-2xx status
 */
export async function fetchJSON<T>(
  path: string,
  options?: RequestInit,
): Promise<T> {
  if (isTauri()) {
    const res = await tauriFetch(path, options);
    if (res.status < 200 || res.status >= 300) {
      throw new Error(res.body || `HTTP ${res.status}`);
    }
    return res.body ? (JSON.parse(res.body) as T) : (undefined as T);
  }

  const res = await browserFetch(path, options);
  if (!res.ok) {
    const text = await res.text();
    throw new Error(text || `HTTP ${res.status}`);
  }
  return res.json() as Promise<T>;
}

// ---------------------------------------------------------------------------
// SSE streaming helper
// ---------------------------------------------------------------------------

/**
 * Open a Server-Sent Events stream by POSTing `body` to `path`.
 *
 * The backend sends newline-delimited JSON objects prefixed with "data: ".
 * Each parsed object is delivered to `onEvent`.
 *
 * Usage:
 * ```ts
 * const stop = streamSSE("/api/chat", { conversationId, message }, (evt) => {
 *   if (evt.type === "token") appendToken(evt.content);
 *   if (evt.type === "done")  markDone();
 * });
 * // Later:
 * stop(); // aborts the stream
 * ```
 *
 * @param path    - URL path, e.g. "/api/chat"
 * @param body    - Request payload (JSON-serialised)
 * @param onEvent - Callback invoked for every parsed SSE event object
 * @returns       Cleanup function — call it to abort the stream
 */
export function streamSSE(
  path: string,
  body: object,
  onEvent: (event: Record<string, unknown>) => void,
): () => void {
  const controller = new AbortController();
  let cancelled = false;
  let tauriUnlisten: UnlistenFn | null = null;

  const emitError = (message: string) => {
    if (cancelled) return;
    onEvent({ type: "error", error: message });
  };

  const handleSSELine = (line: string): boolean => {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith(":")) return false; // empty / comment

    if (trimmed.startsWith("data: ")) {
      const data = trimmed.slice(6);
      if (data === "[DONE]") return true;
      try {
        onEvent(JSON.parse(data));
      } catch {
        // Ignore non-JSON data lines (plain-text chunks, etc.)
      }
    }
    return false;
  };

  (async () => {
    if (isTauri()) {
      const streamId =
        globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random()}`;
      let status = 200;
      let tauriBuffer = "";
      try {
        tauriUnlisten = await listen<ApiStreamEvent>(
          `api-stream:${streamId}`,
          (evt) => {
            if (cancelled) return;
            const payload = evt.payload;
            if (payload.kind === "status") {
              status = payload.status ?? 200;
            } else if (payload.kind === "chunk") {
              if (status >= 200 && status < 300) {
                tauriBuffer += payload.data ?? "";
                const lines = tauriBuffer.split("\n");
                tauriBuffer = lines.pop() ?? "";
                for (const line of lines) {
                  if (handleSSELine(line)) {
                    cancelled = true;
                    tauriUnlisten?.();
                    return;
                  }
                }
              }
            } else if (payload.kind === "error") {
              emitError(payload.error || "Local API stream failed");
              cancelled = true;
              tauriUnlisten?.();
            } else if (payload.kind === "done") {
              if (tauriBuffer) {
                handleSSELine(tauriBuffer);
                tauriBuffer = "";
              }
              if (status < 200 || status >= 300) {
                emitError(`HTTP ${status}`);
              }
              cancelled = true;
              tauriUnlisten?.();
            }
          },
        );
        await invoke("api_stream", {
          path,
          body: JSON.stringify(body),
          streamId,
        });
      } catch (err) {
        if (!cancelled) {
          console.error("[streamSSE] Tauri local API request failed:", err);
          emitError(err instanceof Error ? err.message : String(err));
        }
        tauriUnlisten?.();
      }
      return;
    }

    let res: Response;
    try {
      res = await fetch(`${API_BASE}${path}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
        signal: controller.signal,
      });
    } catch (err) {
      if ((err as DOMException).name !== "AbortError") {
        console.error("[streamSSE] fetch failed:", err);
        emitError(
          `Could not reach local Pan API at ${API_BASE}${path}. ${err instanceof Error ? err.message : String(err)}`,
        );
      }
      return;
    }

    if (!res.ok || !res.body) {
      const text = await res.text().catch(() => `HTTP ${res.status}`);
      console.error("[streamSSE] bad response:", text);
      emitError(text || `HTTP ${res.status}`);
      return;
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = "";

    try {
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });

        // Process complete SSE lines from the buffer.
        const lines = buffer.split("\n");
        // Keep the last (potentially incomplete) line in the buffer.
        buffer = lines.pop() ?? "";

        for (const line of lines) {
          if (handleSSELine(line)) {
            cancelled = true;
            controller.abort();
            return;
          }
        }
      }
    } catch (err) {
      if ((err as DOMException).name !== "AbortError") {
        console.error("[streamSSE] read error:", err);
      }
    } finally {
      reader.releaseLock();
    }
  })();

  // Return cleanup — callers call this to abort mid-stream.
  return () => {
    cancelled = true;
    tauriUnlisten?.();
    controller.abort();
  };
}

// ---------------------------------------------------------------------------
// Phase 12: session budgets + task runner
// ---------------------------------------------------------------------------

export interface SessionBudget {
  session_id: string;
  cost_cap_usd: number;
}

export interface SessionInfo {
  id: string;
  source: string;
  startedAt: number;
  endedAt?: number;
  messageCount: number;
  model: string;
  title: string;
  tokenBudgetUsed: number;
  tokenBudgetCap: number;
  costUsedUsd: number;
  costCapUsd: number;
}

/**
 * Fetch the session record (including budget fields) for a single session.
 * Used by the Chat screen on mount to seed the cost-pill UI so the budget
 * display survives navigating away and back to a session rather than
 * resetting to zero until the next SSE budget event arrives.
 *
 * Note: GET /v1/sessions/{id} returns the message list (historical), so
 * the resource lives at GET /v1/sessions/{id}/info.
 */
export function getSession(sessionId: string): Promise<SessionInfo> {
  return fetchJSON<SessionInfo>(`/v1/sessions/${sessionId}/info`);
}

export function setSessionBudget(
  sessionId: string,
  costCapUsd: number,
): Promise<SessionBudget> {
  return fetchJSON<SessionBudget>(`/v1/sessions/${sessionId}/budget`, {
    method: "PUT",
    body: JSON.stringify({ cost_cap_usd: costCapUsd }),
  });
}

export type TaskStatus =
  | "queued"
  | "running"
  | "paused"
  | "zombie"
  | "succeeded"
  | "failed"
  | "cancelled";

export interface Task {
  id: string;
  plan_json?: string;
  status: TaskStatus;
  session_id: string;
  created_at: number;
  last_heartbeat_at?: number;
  next_plan_step_index: number;
  token_budget_cap: number;
  cost_cap_usd: number;
}

export type TaskEventKind =
  | "tool_call"
  | "approval"
  | "journal_receipt"
  | "artifact"
  | "cost"
  | "error"
  | "heartbeat"
  | "step_completed";

export interface TaskEvent {
  id: number;
  task_id: string;
  step_id: string;
  attempt: number;
  sequence: number;
  kind: TaskEventKind;
  payload_json?: string;
  created_at: number;
}

export function createTask(
  sessionId: string,
  planJson?: string,
  costCapUsd?: number,
): Promise<Task> {
  return fetchJSON<Task>("/v1/tasks", {
    method: "POST",
    body: JSON.stringify({
      session_id: sessionId,
      plan_json: planJson ?? "",
      cost_cap_usd: costCapUsd ?? 0,
    }),
  });
}

export function listTasks(sessionId?: string): Promise<Task[]> {
  const qs = sessionId ? `?session_id=${encodeURIComponent(sessionId)}` : "";
  return fetchJSON<Task[]>(`/v1/tasks${qs}`);
}

export function getTask(id: string): Promise<Task> {
  return fetchJSON<Task>(`/v1/tasks/${id}`);
}

export function getTaskEvents(taskId: string): Promise<TaskEvent[]> {
  return fetchJSON<TaskEvent[]>(`/v1/tasks/${taskId}/events`);
}

export function pauseTask(id: string): Promise<{ status: string }> {
  return fetchJSON<{ status: string }>(`/v1/tasks/${id}/pause`, {
    method: "POST",
  });
}

export function resumeTask(id: string): Promise<{ status: string }> {
  return fetchJSON<{ status: string }>(`/v1/tasks/${id}/resume`, {
    method: "POST",
  });
}

export function cancelTask(id: string): Promise<{ status: string }> {
  return fetchJSON<{ status: string }>(`/v1/tasks/${id}/cancel`, {
    method: "POST",
  });
}

// ---------------------------------------------------------------------------
// Phase 12: Recovery / History
// ---------------------------------------------------------------------------

export interface ReceiptDTO {
  id: string;
  taskId: string;
  kind: "fs_write" | "fs_delete" | "shell" | "browser_form" | "saas_api";
  snapshotTier: "cow" | "copyfs" | "audit_only";
  reversalStatus: "reversible" | "audit_only" | "reversed_externally" | "irrecoverable";
  redactedPayload: string;
  /**
   * Public http(s) URL into the SaaS UI ("Open in Gmail", "View in Stripe").
   * Only set for receipts whose `kind` is `saas_api`. Always safe to render
   * as a link — reverser-private hints (snapshot subpaths, manual-undo
   * targets) live in a separate column on the backend and are not exposed.
   */
  saasUrl?: string;
  createdAt: number;
}

export interface DiffResponse {
  kind: string;
  before: string;
  after: string;
  contentType: "text/plain" | "json" | "binary";
}

export interface UndoResponse {
  applied: boolean;
  newStatus: string;
  details: string;
  approvalId?: string;
}

export function listRecoveries(sessionId?: string, limit?: number): Promise<ReceiptDTO[]> {
  const params = new URLSearchParams();
  if (sessionId) params.set("session_id", sessionId);
  if (limit) params.set("limit", String(limit));
  const qs = params.toString();
  return fetchJSON<ReceiptDTO[]>(`/v1/recovery/list${qs ? `?${qs}` : ""}`);
}

/**
 * List receipts scoped to a single task (newest-first). Used by the
 * task-grouped History UI to fan out one HTTP call per task header.
 */
export function listRecoveriesByTask(
  taskId: string,
  limit?: number,
  offset?: number,
): Promise<ReceiptDTO[]> {
  const params = new URLSearchParams();
  if (limit) params.set("limit", String(limit));
  if (offset) params.set("offset", String(offset));
  const qs = params.toString();
  return fetchJSON<ReceiptDTO[]>(
    `/v1/recovery/list/${taskId}${qs ? `?${qs}` : ""}`,
  );
}

export function getRecoveryDiff(receiptId: string): Promise<DiffResponse> {
  return fetchJSON<DiffResponse>(`/v1/recovery/diff/${receiptId}`);
}

/**
 * Result of a successful (or pending-approval) recovery undo.
 *
 * httpStatus distinguishes the two terminal-OK paths:
 *   200 — synchronous reversal applied (FS receipts).
 *   202 — async approval needed; the reversal will run after the user
 *         approves at /v1/approvals/{approvalId}. Caller should poll.
 */
export interface UndoResult {
  applied: boolean;
  newStatus: string;
  details: string;
  approvalId?: string;
  httpStatus: 200 | 202;
}

/**
 * Trigger the reversal for a receipt. The backend mandates a
 * `{"confirm": true}` body to guard against double-click accidents — the
 * old fire-and-forget caller skipped it and silently 400'd, leaving the
 * History "Undo" button non-functional. Throws on 4xx/5xx.
 */
export async function undoRecovery(receiptId: string): Promise<UndoResult> {
  const res = await fetch(`${API_BASE}/v1/recovery/undo/${receiptId}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ confirm: true }),
  });
  if (res.status !== 200 && res.status !== 202) {
    const text = await res.text().catch(() => `HTTP ${res.status}`);
    throw new Error(`undoRecovery: ${res.status} ${text}`);
  }
  const data = (await res.json()) as Omit<UndoResult, "httpStatus">;
  // Backend also sets X-Approval-ID; data.approvalId is the canonical
  // copy on the body, so the header read is redundant but harmless.
  return { ...data, httpStatus: res.status as 200 | 202 };
}

// ---------------------------------------------------------------------------
// Approvals
// ---------------------------------------------------------------------------

export type ApprovalStatus = "pending" | "approved" | "rejected";

export interface Approval {
  id: string;
  session_id: string;
  tool_name: string;
  arguments: string;
  status: ApprovalStatus;
  created_at: number;
  resolved_at?: number;
}

/**
 * Read a single approval record. Used by the History undo flow to poll
 * a 202-deferred reversal — once the human approves at /v1/approvals/{id},
 * the backend triggers the actual reversal asynchronously, and the
 * frontend pivots from "Pending Approval" to "Reverted at HH:MM"
 * without waiting on the 5 s journal-list auto-refresh.
 */
export function getApproval(id: string): Promise<Approval> {
  return fetchJSON<Approval>(`/v1/approvals/${id}`);
}

// ---------------------------------------------------------------------------
// M4 W2: office engine + migration + bundle info + persistence alert bus
// ---------------------------------------------------------------------------
//
// Typed helpers for the 0.4.0 Claw3D embedded-engine runtime toggle and the
// one-shot legacy-JSON migration importer. The Go side landed these in
// internal/gateway/office_engine.go and office_migration.go earlier in this
// session; the frontend now consumes them here. All responses shapes mirror
// the Go types verbatim so a single protocol change propagates through one
// file only.

// ─── Engine contracts ──────────────────────────────────────────────────────

/**
 * Shape returned by GET /v1/office/engine. `switchable` lets the UI disable
 * the dropdown on builds that lock the engine (none yet, but future-proof).
 */
export interface EngineGetResponse {
  engine: "go" | "node";
  switchable: boolean;
}

/** POST body for /v1/office/engine. */
export interface EnginePostRequest {
  engine: "go" | "node";
}

/**
 * POST response — `persisted:false` means the in-memory swap succeeded but
 * the yaml write failed, producing a restart-time divergence. The UI must
 * surface this as a sticky alert (PersistenceAlert component), NOT as a
 * cosmetic badge — see Gate-1 decision #6.
 */
export interface EnginePostResponse {
  engine: "go" | "node";
  changed: boolean;
  from?: "go" | "node"; // omitted when changed=false (no-op swap)
  persisted: boolean;
}

export function getEngine(init?: RequestInit): Promise<EngineGetResponse> {
  return fetchJSON<EngineGetResponse>("/v1/office/engine", init);
}

export function postEngine(body: EnginePostRequest): Promise<EnginePostResponse> {
  return fetchJSON<EnginePostResponse>("/v1/office/engine", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

// ─── Migration contracts ───────────────────────────────────────────────────

/** GET /v1/office/migration/status — the banner-gating signal. */
export interface MigrationStatusResponse {
  needed: boolean;
  legacyPath: string; // "" when needed=false
  acked: boolean;
}

/** POST body for /v1/office/migration/run — all fields optional. */
export interface MigrationRunRequest {
  dryRun?: boolean;
  force?: boolean;
  source?: string;
  backupDir?: string;
}

/**
 * Report returned by /v1/office/migration/run. `status:"missing"` is NOT an
 * error — it means there was nothing to migrate; the banner should hide.
 */
export interface MigrationReport {
  imported: {
    agents: number;
    sessions: number;
    messages: number;
    cron: number;
  };
  status: "ok" | "skip" | "missing";
  digest: string;
  backupPath?: string;
}

export function getMigrationStatus(init?: RequestInit): Promise<MigrationStatusResponse> {
  return fetchJSON<MigrationStatusResponse>("/v1/office/migration/status", init);
}

export function postMigrationRun(body: MigrationRunRequest = {}): Promise<MigrationReport> {
  return fetchJSON<MigrationReport>("/v1/office/migration/run", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

// ─── Config patch (dismiss migration banner) ──────────────────────────────

/** Subset of /v1/config PUT body — only the M4-W2 fields we write. */
export interface ConfigPatchRequest {
  office?: { migration_ack?: boolean };
}

export function patchConfig(body: ConfigPatchRequest): Promise<void> {
  return fetchJSON<void>("/v1/config", {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

// ─── Bundle info (parsed from /office/config.js text) ─────────────────────

/**
 * Pieces of the runtime bootstrap that the Go adapter writes into
 * `/office/config.js` on request. Used by OfficeDebugPanel to display the
 * bundle SHA. On any parse failure the value is `"unknown"` rather than
 * throwing — a broken debug display is better than a broken Office tab.
 */
export interface BundleInfo {
  sha: string;
  wsUrl: string;
  apiBase: string;
}

/**
 * Extract a `window.<key> = "<value>"` assignment from the Go-generated
 * config.js. We capture the full JS string literal including its quotes,
 * then `JSON.parse` it. That handles Go's `%q` formatter outputs —
 * embedded escapes like `\"`, `\\`, `\u00e9` — which a naïve `"([^"]*)"`
 * pattern silently corrupts (Gate-2 refinement #4). Failures return
 * `"unknown"` so the UI can render something meaningful.
 */
function pickBundleValue(source: string, key: string): string {
  const re = new RegExp(`window\\.${key}\\s*=\\s*("(?:[^"\\\\]|\\\\.)*")`);
  const m = source.match(re);
  if (!m) return "unknown";
  try {
    return JSON.parse(m[1]) as string;
  } catch {
    return "unknown";
  }
}

export async function getBundleInfo(): Promise<BundleInfo> {
  const res = await fetch(`${API_BASE}/office/config.js`, { cache: "no-store" });
  if (!res.ok) {
    return { sha: "unknown", wsUrl: "unknown", apiBase: "unknown" };
  }
  const text = await res.text();
  return {
    sha: pickBundleValue(text, "__CLAW3D_BUNDLE_SHA__"),
    wsUrl: pickBundleValue(text, "__CLAW3D_WS_URL__"),
    apiBase: pickBundleValue(text, "__CLAW3D_API_BASE__"),
  };
}

// ─── Persistence-alert event bus ───────────────────────────────────────────

/**
 * DOM event name used to bridge OfficeDebugPanel (emitter) to
 * PersistenceAlert (consumer) without a shared state library. Using the
 * browser's event system keeps both components independent — either can
 * be mounted/unmounted without the other knowing.
 */
export const PERSISTENCE_ALERT_EVENT = "pan-agent:persistence-alert";

/** Payload attached to the CustomEvent.detail. */
export interface PersistenceAlertDetail {
  engine: "go" | "node";
  from?: "go" | "node";
}

/**
 * Emit a sticky page-level warning that an engine swap succeeded in-memory
 * but failed to persist to config.yaml. Consumer mounts in Layout.tsx and
 * stays visible until the user explicitly dismisses — per Gate-1 decision
 * #6, this is a restart-flip risk, not cosmetic.
 */
export function emitPersistenceAlert(detail: PersistenceAlertDetail): void {
  window.dispatchEvent(
    new CustomEvent<PersistenceAlertDetail>(PERSISTENCE_ALERT_EVENT, { detail }),
  );
}
