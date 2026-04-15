// pan-agent real-webview E2E suite (M5-C4).
//
// These 5 tests exercise what the Playwright smoke tests CAN'T: the
// actual Tauri WebView2 (Windows) and WebKitGTK (Linux) engines. The
// gap matters because Playwright's "webkit" channel is upstream WebKit,
// NOT Apple's WKWebView — and even on Windows, Playwright's Chromium
// drifts from WebView2. We ship anyway if these pass on the two CI
// matrix legs.
//
// Intentionally kept minimal: any test that could be covered by the
// mocked Playwright suite (Commit D) should live there instead. These
// 5 only test things that REQUIRE a real webview:
//   1. Tauri window actually launches
//   2. /office/ iframe loads against the real gateway
//   3. WebSocket upgrade completes with cookie auth
//   4. WebGL2 context creates a real rendering context (no mock)
//   5. EventSource delivers SSE from /v1/chat/completions
//
// The assumption: pan-agent gateway is already running at :8642
// with a seeded session. CI job starts it via background process
// before wdio boots; local devs must run it manually (README note).

describe("pan-agent real webview (M5-C4)", () => {
  it("1. launches with Pan Desktop title", async () => {
    const title = await browser.getTitle();
    expect(title).toMatch(/Pan\s*Desktop/i);
  });

  it("2. navigates to /office/ and iframe loads", async () => {
    // Navigate the webview to the gateway's office route. Assumes
    // the backend is up; CI workflow ensures this.
    await browser.url("http://localhost:8642/office/");
    // The Office screen iframes the embedded Claw3D bundle. Wait for
    // the iframe element to exist; document-level check is enough
    // because the embedded bundle hydrates its own internal content.
    const iframe = await $("iframe");
    await iframe.waitForExist({ timeout: 15_000 });
  });

  it("3. upgrades WebSocket to /office/ws with session cookie", async () => {
    // First, GET /office/ to mint the session cookie. The embedded
    // bundle does this automatically, but we trigger it explicitly
    // to make the test deterministic.
    await browser.url("http://localhost:8642/office/");
    // Now open a WS from inside the webview context. Same-origin
    // means the session cookie attaches automatically. Resolve
    // with a boolean to keep the test runner's assertion surface
    // simple.
    const ok = await browser.execute(() => {
      return new Promise((resolve) => {
        try {
          const ws = new WebSocket("ws://localhost:8642/office/ws");
          ws.onopen = () => {
            ws.close();
            resolve(true);
          };
          ws.onerror = () => resolve(false);
          setTimeout(() => {
            ws.close();
            resolve(false);
          }, 5000);
        } catch {
          resolve(false);
        }
      });
    });
    expect(ok).toBe(true);
  });

  it("4. creates a real WebGL2 context (no mock)", async () => {
    // This is the test that catches the WebView2-without-GPU case.
    // If it fails on the Windows matrix leg, that's the signal to
    // ship the M5-C3 FallbackBanner + splash flow for those users.
    const result = await browser.execute(() => {
      try {
        const canvas = document.createElement("canvas");
        const gl = canvas.getContext("webgl2");
        if (!gl) return null;
        // GL_VERSION = 0x1F02. Returning the string proves we have
        // a real driver, not a stub.
        return gl.getParameter(0x1F02);
      } catch {
        return null;
      }
    });
    expect(result).not.toBeNull();
    expect(String(result)).toMatch(/WebGL/i);
  });

  it("5. EventSource delivers an SSE message from /v1/chat/completions", async () => {
    // Regression guard for WKWebView's historical EventSource bug
    // (Safari <15.4 silently dropped events on idle connections).
    // The mock endpoint path `/v1/chat/completions?probe=1` should
    // emit at least one message within 10 seconds.
    const gotMessage = await browser.execute(() => {
      return new Promise((resolve) => {
        try {
          const es = new EventSource(
            "http://localhost:8642/v1/chat/completions?probe=1",
          );
          es.onmessage = () => {
            es.close();
            resolve(true);
          };
          setTimeout(() => {
            es.close();
            resolve(false);
          }, 10_000);
        } catch {
          resolve(false);
        }
      });
    });
    // Note: this test may fail if /v1/chat/completions?probe=1 isn't
    // set up to emit a probe message. TODO at M6-C1: add a doctor
    // seed step or an explicit probe endpoint.
    expect(gotMessage).toBe(true);
  });
});
