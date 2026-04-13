/**
 * HTTP client for the Pan-Agent Go backend.
 *
 * The backend listens on localhost:8642.
 * REST calls use fetchJSON; streaming responses (chat) use streamSSE.
 */

const BASE = import.meta.env.VITE_API_BASE ?? "http://localhost:8642";

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
  const res = await fetch(`${BASE}${path}`, {
    headers: { "Content-Type": "application/json", ...options?.headers },
    ...options,
  });
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

  (async () => {
    let res: Response;
    try {
      res = await fetch(`${BASE}${path}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
        signal: controller.signal,
      });
    } catch (err) {
      if ((err as DOMException).name !== "AbortError") {
        console.error("[streamSSE] fetch failed:", err);
      }
      return;
    }

    if (!res.ok || !res.body) {
      const text = await res.text().catch(() => `HTTP ${res.status}`);
      console.error("[streamSSE] bad response:", text);
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
          const trimmed = line.trim();
          if (!trimmed || trimmed.startsWith(":")) continue; // empty / comment

          if (trimmed.startsWith("data: ")) {
            const data = trimmed.slice(6);
            if (data === "[DONE]") {
              controller.abort();
              return;
            }
            try {
              onEvent(JSON.parse(data));
            } catch {
              // Ignore non-JSON data lines (plain-text chunks, etc.)
            }
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
  return () => controller.abort();
}
