# Release Readiness Checklist

Use this checklist before cutting a tag or declaring Pan-Agent usable for release.
If any item marked "blocking" is not true, the verdict is "no-go".

## Scope

This checklist covers four things:
1. local backend health
2. desktop frontend health
3. native desktop build/release health
4. one manual smoke pass of the shipped user path

## 1) Automated pre-ship gates

All blocking checks must pass on the candidate commit.

### Backend and OpenAPI
- [ ] Blocking: `go build ./...`
- [ ] Blocking: `go test ./... -count=1 -timeout 120s`
- [ ] Blocking: `bash scripts/verify-api.sh`

### Desktop frontend
Run from `desktop/`.
- [ ] Blocking: `npm ci`
- [ ] Blocking: `npm run typecheck`
- [ ] Blocking: `npm run lint`
- [ ] Blocking: `npm run build:vite`

### Native Tauri build
Before running these, build the Go sidecar with the target-specific name expected by `desktop/src-tauri/binaries/`.
Run native desktop checks from `desktop/`; use `npm run check:tauri` for the Rust preflight and `npx tauri build` for the full package build.

Minimum confidence gate:
- [ ] Blocking: `cd desktop && npm run check:tauri`

Release-grade confidence gate:
- [ ] Blocking: `npx tauri build`

Operator rule: a passing web build is not enough. The release candidate is not ready unless the Rust/Tauri layer is also validated on a supported build host or by the release CI workflow.

## 2) CI and workflow gates

- [ ] Blocking: `ci.yml` is green for the candidate commit.
- [ ] Blocking: `lint.yml` is green for the candidate commit.
- [ ] Blocking: the Tauri matrix completed on the platforms required for this release.
- [ ] Blocking for a tagged release: `release.yml` completed and uploaded the expected artifacts.
- [ ] Non-blocking but recommended: confirm the release body includes hashes and platform notes before announcing the release.

Current workflow reality:
- `ci.yml` covers Go build/test, desktop typecheck/build, and Tauri builds.
- `lint.yml` covers Go lint/security checks, desktop typecheck, desktop lint, and Rust fmt/clippy.
- Packaged-app launch smoke tests are not fully covered in CI, so a manual smoke pass remains mandatory.

## 3) Manual smoke test

Run this on at least one supported desktop target for the release artifacts you plan to ship.
If CI produced installers but nobody launched the app, treat that as no-go.

### Smoke path
1. Install or launch the desktop app.
2. Confirm the backend starts and `GET /v1/health` succeeds.
3. Complete the first-run Setup Wizard, or confirm an existing saved config loads cleanly.
4. Open Chat and send a simple prompt.
5. Confirm the response streams instead of hanging.
6. Confirm model listing or provider sync works.
7. Trigger one safe tool path such as a filesystem listing or web search.
8. Trigger one approval-required action and confirm the approval UI appears instead of silently executing.
9. Restart the app and confirm the session/config still loads.

### Manual acceptance checklist
- [ ] Blocking: app launches without crashing.
- [ ] Blocking: backend is reachable from the desktop shell.
- [ ] Blocking: setup flow or saved-config path works.
- [ ] Blocking: basic chat round-trip works.
- [ ] Blocking: streaming response is visible.
- [ ] Blocking: at least one tool path works.
- [ ] Blocking: approval UI appears for a dangerous action.
- [ ] Recommended: verify auto-update metadata/artifacts are present for tagged releases.

## 4) Known caveats to explicitly accept or reject

These are not automatic blockers by themselves, but they must be consciously accepted and documented for the release.

- Windows installers are currently unsigned, so SmartScreen/Defender warnings may appear.
- Linux PC-control tools require X11 or XWayland; pure Wayland is not supported.
- CI does not fully replace a packaged-app launch test; manual smoke testing is still required.
- Cross-platform release confidence requires at least one real validation path per shipped platform: local supported host, CI packaging evidence, or both.

Operator rule: if a caveat changes first-run usability and is not already documented in README/release notes/manual, document it before shipping.

## 5) Go / no-go rubric

Declare "Go" only if all of the following are true:
- every blocking automated check passed
- required CI workflows are green
- native Tauri validation passed
- at least one manual smoke pass succeeded on a supported target
- accepted caveats are documented

Declare "No-go" if any of the following are true:
- a blocking command fails
- the desktop shell cannot launch or cannot reach the backend
- setup, chat, streaming, tool invocation, or approval flow is broken
- release artifacts are missing for a promised platform
- native desktop validation is still unproven for the release candidate

## Evidence to record in the release issue / PR

Copy these into the release notes, PR description, or ship/no-ship comment:
- commit/tag being evaluated
- pass/fail result for each automated gate
- links to green CI/release workflows
- platform(s) used for the manual smoke test
- any accepted caveats
- final verdict: Go / No-go

## Read next
- [[02 - Build and Release Pipeline]]
- [[04 - Build and CI Issues]]
- [[03 - Auto-Update System]]
