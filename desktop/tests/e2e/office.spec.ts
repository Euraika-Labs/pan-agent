import { expect, test, type Page } from "@playwright/test";

// M4 W2 Commit D — Office UI smoke tests. Mocks all backend endpoints
// via page.route() so no pan-agent binary is required. The 3 tests
// (IT-1, IT-4, IT-5) cover the state-machine transitions that Gate-1
// refinement #5 (no auto-dismiss), #6 (engine toggle), and #6 (sticky
// persistence alert) specified.

interface MockOpts {
  migrationNeeded?: boolean;
  migrationAcked?: boolean;
  engine?: "go" | "node";
  enginePersisted?: boolean;
}

async function mockBackend(page: Page, opts: MockOpts = {}): Promise<void> {
  // IMPORTANT: Playwright matches routes in REGISTRATION order; the first
  // matching route wins. Specific routes MUST be registered BEFORE the
  // catch-all `/office/**` below, otherwise the catch-all shadows them.
  // Gate-2 refinement #5 — comment preserved for future maintainers.

  // ─── CSP report endpoint ─────────────────────────────────────────────
  // main.tsx attaches the listener before React mounts, and the mock
  // needs to intercept reports fired during the initial page load. We
  // stub with 204 so the listener's drain queue stays empty.
  await page.route("**/v1/office/csp-report", (route) =>
    route.fulfill({ status: 204, body: "" }),
  );

  // ─── Migration status + run ──────────────────────────────────────────
  await page.route("**/v1/office/migration/status", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        needed: opts.migrationNeeded ?? false,
        legacyPath: opts.migrationNeeded ? "/tmp/clawd3d-history.json" : "",
        acked: opts.migrationAcked ?? false,
      }),
    }),
  );

  await page.route("**/v1/office/migration/run", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        imported: { agents: 3, sessions: 5, messages: 12, cron: 0 },
        status: "ok",
        digest: "abcdef123456",
      }),
    }),
  );

  // ─── Engine GET + POST ────────────────────────────────────────────────
  await page.route("**/v1/office/engine", async (route) => {
    const method = route.request().method();
    if (method === "GET") {
      return route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          engine: opts.engine ?? "go",
          switchable: true,
        }),
      });
    }
    // POST — parse the body and echo the swap result
    const raw = route.request().postData() ?? "{}";
    const body = JSON.parse(raw) as { engine: "go" | "node" };
    return route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        engine: body.engine,
        changed: true,
        from: opts.engine ?? "go",
        persisted: opts.enginePersisted ?? true,
      }),
    });
  });

  // ─── Config PUT (banner ack) ─────────────────────────────────────────
  await page.route("**/v1/config", (route) =>
    route.fulfill({ status: 200, body: "{}" }),
  );

  // ─── Bundle config.js ────────────────────────────────────────────────
  await page.route("**/office/config.js", (route) =>
    route.fulfill({
      status: 200,
      contentType: "application/javascript",
      body:
        'window.__CLAW3D_BUNDLE_SHA__="deadbeefcafe1234567890";\n' +
        'window.__CLAW3D_WS_URL__="ws://localhost:8642/office/ws";\n' +
        'window.__CLAW3D_API_BASE__="http://localhost:8642";',
    }),
  );

  // ─── Catch-all LAST — iframe body stub ───────────────────────────────
  // Must be registered LAST so specific routes above win. See comment
  // above the mock bundle.
  await page.route("**/office/**", async (route) => {
    const url = route.request().url();
    // Let /office/config.js fall through to its specific handler.
    if (url.includes("/office/config.js")) return route.fallback();
    return route.fulfill({
      status: 200,
      contentType: "text/html",
      body: "<!doctype html><html><body>stub</body></html>",
    });
  });
}

test.describe("Office UI — M4 W2 Commit D", () => {
  test("IT-1 migration banner renders, imports, summary persists until explicit close", async ({
    page,
  }) => {
    await mockBackend(page, { migrationNeeded: true });
    await page.goto("/");

    // Banner appears with its aria-labelledby region
    const banner = page.getByRole("region", { name: /claw3d is now built-in/i });
    await expect(banner).toBeVisible();

    // Import action
    await banner.getByRole("button", { name: /import history/i }).click();

    // Summary shows with the exact counts from the mock
    await expect(banner).toContainText(
      /imported 3 agents, 5 sessions, 12 messages/i,
    );

    // Summary persists — no auto-dismiss (Gate-1 refinement #5, WCAG 2.2.1)
    await page.waitForTimeout(1500);
    await expect(banner).toBeVisible();

    // Explicit close removes the banner
    await banner.getByRole("button", { name: /close migration banner/i }).click();
    await expect(banner).not.toBeVisible();
  });

  test("IT-4 engine toggle round-trips go → node via debug panel", async ({
    page,
  }) => {
    await mockBackend(page, {
      engine: "go",
      enginePersisted: true,
      migrationAcked: true,
    });
    await page.goto("/");

    // Navigate to Office tab (Layout sidebar button)
    await page.getByRole("button", { name: /^office$/i }).click();

    // Open the debug panel via the kebab button (NOT keyboard shortcut —
    // kbd shortcuts are unit-tested separately in layout.shortcuts.test.ts
    // so we don't need to assert focus state here).
    await page.getByRole("button", { name: /toggle debug panel/i }).click();

    const debugPanel = page.getByRole("region", { name: /office debug panel/i });
    await expect(debugPanel).toBeVisible();

    // Engine dropdown starts at "go"
    const select = debugPanel.getByRole("combobox", { name: /claw3d engine/i });
    await expect(select).toHaveValue("go");

    // Swap to node — response mock returns persisted:true, so NO
    // PersistenceAlert should appear
    await select.selectOption("node");
    // Give the async POST a moment to complete
    await page.waitForTimeout(200);

    const persistAlert = page
      .getByRole("alert")
      .filter({ hasText: /engine swap not saved/i });
    await expect(persistAlert).not.toBeVisible();
  });

  test("IT-5 persisted=false triggers sticky PersistenceAlert", async ({
    page,
  }) => {
    await mockBackend(page, {
      engine: "go",
      enginePersisted: false, // the critical bit — yaml write failed
      migrationAcked: true,
    });
    await page.goto("/");

    await page.getByRole("button", { name: /^office$/i }).click();
    await page.getByRole("button", { name: /toggle debug panel/i }).click();

    const debugPanel = page.getByRole("region", { name: /office debug panel/i });
    await debugPanel
      .getByRole("combobox", { name: /claw3d engine/i })
      .selectOption("node");

    // Sticky alert appears above the Office screen
    const persistAlert = page
      .getByRole("alert")
      .filter({ hasText: /engine swap not saved/i });
    await expect(persistAlert).toBeVisible();

    // Dismissing requires an explicit click (Gate-1 refinement #6)
    await persistAlert
      .getByRole("button", { name: /dismiss persistence alert/i })
      .click();
    await expect(persistAlert).not.toBeVisible();
  });
});
