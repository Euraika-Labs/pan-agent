// WebdriverIO config for pan-agent real-webview E2E tests (M5-C4).
//
// This harness is deliberately ISOLATED from the rest of the desktop
// app — it has its own package.json with wdio v7 pinned because
// v8/v9 break against tauri-driver. The main desktop/ package.json
// keeps its wdio-free dep graph; this subtree only gets installed on
// CI runners that actually run the real-webview matrix.
//
// Runtime flow:
//   1. onPrepare: `tauri build --debug --no-bundle` in desktop/
//      (skipped locally if the binary exists already).
//   2. beforeSession: spawn tauri-driver from ~/.cargo/bin/tauri-driver.
//   3. Each spec: wdio connects to the local WebDriver endpoint and
//      drives a real Tauri webview (WebView2 on Windows, WebKitGTK
//      on Linux; macOS is DROPPED per M5 Phase 1 — no upstream driver).
//   4. afterSession: SIGKILL tauri-driver.
//
// Flake mitigation:
//   - 90s mocha timeout per test (WebView2 cold start is slow).
//   - Single worker (maxInstances: 1) so tests don't race the same
//     WebDriver endpoint.
//   - wdio v7 pin is non-negotiable; don't upgrade until tauri-apps
//     fixes #10670.

import { spawn, spawnSync } from "node:child_process";
import os from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "../../..");

let tauriDriver = null;

function tauriBinaryPath() {
  const isWindows = process.platform === "win32";
  const name = isWindows ? "pan-agent.exe" : "pan-agent";
  return path.join(
    repoRoot,
    "desktop",
    "src-tauri",
    "target",
    "debug",
    name,
  );
}

export const config = {
  host: "127.0.0.1",
  port: 4444,
  specs: ["./specs/**/*.e2e.js"],
  maxInstances: 1,

  capabilities: [
    {
      maxInstances: 1,
      "tauri:options": {
        application: tauriBinaryPath(),
      },
    },
  ],

  logLevel: "info",
  bail: 0,
  waitforTimeout: 10_000,
  connectionRetryTimeout: 120_000,
  connectionRetryCount: 3,

  framework: "mocha",
  reporters: ["spec"],

  mochaOpts: {
    ui: "bdd",
    timeout: 90_000,
  },

  // Build the Tauri app (debug, no bundle) once before the suite runs.
  // --no-bundle skips installer creation which shaves ~30-60s on each
  // CI run. Debug build is fine here — we're testing webview behavior,
  // not installer signing or updater paths.
  onPrepare() {
    console.log("[wdio] building pan-agent (debug, no-bundle)...");
    const result = spawnSync(
      "npm",
      ["run", "tauri", "build", "--", "--debug", "--no-bundle"],
      {
        cwd: path.join(repoRoot, "desktop"),
        stdio: "inherit",
        shell: true,
      },
    );
    if (result.status !== 0) {
      throw new Error(`tauri build failed: exit ${result.status}`);
    }
  },

  beforeSession() {
    console.log("[wdio] spawning tauri-driver...");
    const driverPath = path.join(
      os.homedir(),
      ".cargo",
      "bin",
      process.platform === "win32" ? "tauri-driver.exe" : "tauri-driver",
    );
    tauriDriver = spawn(driverPath, [], {
      stdio: [null, process.stdout, process.stderr],
    });
  },

  afterSession() {
    if (tauriDriver) {
      tauriDriver.kill();
      tauriDriver = null;
    }
  },
};
