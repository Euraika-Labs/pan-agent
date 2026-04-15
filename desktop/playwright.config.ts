import { defineConfig } from "@playwright/test";

// M4 W2 Commit D — Playwright smoke tests for the Office activation flow.
//
// The 3 tests in tests/e2e/office.spec.ts exercise UI state machines only
// (migration banner, engine toggle, persistence alert), NOT API round-trip
// correctness — that's covered by Go integration tests. page.route() mocks
// every /v1/office/* + /office/config.js request so these tests run against
// a bare Vite dev server with no pan-agent binary required. M5's tauri-
// driver matrix covers the real Tauri webview path.
//
// Single worker: tests share the same :5173 port + same in-browser state
// bus (PersistenceAlert CustomEvents), so running them in parallel causes
// cross-contamination.
export default defineConfig({
  testDir: "./tests/e2e",
  timeout: 15_000,
  retries: 1,
  workers: 1,
  use: {
    baseURL: "http://localhost:5173",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  webServer: {
    command: "npm run dev:vite",
    url: "http://localhost:5173",
    reuseExistingServer: !process.env.CI,
    timeout: 60_000,
  },
});
