# Build and Release Pipeline

This runbook describes the CI/CD pipeline that builds and releases Pan-Agent.

## Two repos, two CIs

| Repo | URL | Role |
|---|---|---|
| Primary | `git.euraika.net/euraika/pan-agent` (GitLab) | Source of truth. Code reviews via merge requests. |
| Mirror | `github.com/Euraika-Labs/pan-agent` (GitHub) | CI runners + binary distribution. |

GitLab CI runs `go test` + `go build` + desktop typecheck. GitHub Actions runs the cross-platform CI matrix and the release workflow.

## CI workflow (.github/workflows/ci.yml)

Triggered on every push and PR. Three jobs:

### 1. `go` — Go build + test (3-OS matrix)
- Runs on `ubuntu-latest`, `windows-latest`, `macos-latest`.
- `go build ./...` and `go test ./... -count=1 -timeout 120s`.
- Builds platform-specific binary with version ldflags.
- Uploads as artifact (30-day retention).
- macOS: `MACOSX_DEPLOYMENT_TARGET=14.0` env var.

### 2. `desktop` — Frontend typecheck + build (Linux only)
- Runs on `ubuntu-latest` (no platform-specific code in the React frontend).
- `npm ci` → `npx tsc --noEmit` → `npx vite build`.

### 3. `tauri` — Full Tauri build (3-OS matrix)
- Depends on `go` and `desktop`.
- Runs on `windows-latest`, `macos-latest`, `ubuntu-22.04`.
- Builds the Go sidecar with the right target-triple name:
  - Windows: `pan-agent-x86_64-pc-windows-msvc.exe`
  - macOS: `pan-agent-aarch64-apple-darwin`
  - Linux: `pan-agent-x86_64-unknown-linux-gnu`
- Linux: installs WebKitGTK 4.1 + GTK 3 + appindicator + librsvg + patchelf + soup3 + javascriptcoregtk.
- `npm ci` → `npx tauri build`.
- Uploads installers as artifacts.

Required secrets: `TAURI_SIGNING_PRIVATE_KEY`, `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`. The Tauri build fails without these because `createUpdaterArtifacts` is enabled in `tauri.conf.json`.

## Release workflow (.github/workflows/release.yml)

Triggered on `v*` tag pushes. Same 3-OS matrix as the Tauri CI job, but uses `tauri-apps/tauri-action@v0` to:

1. Build the Go sidecar (with version ldflags from `${{ steps.version.outputs.version }}`).
2. Build the Tauri app.
3. Sign the updater artifacts with the private key.
4. Generate `latest.json` (the updater manifest).
5. Create a GitHub Release.
6. Upload all artifacts: NSIS installer, MSI, DMG, DEB, AppImage, signature files, and `latest.json`.

The release is published immediately (`releaseDraft: false`).

## Cutting a release

```bash
# 1. Bump versions in three places (manual for now)
# Files: desktop/src-tauri/tauri.conf.json, desktop/src-tauri/Cargo.toml, desktop/package.json

# 2. Commit the version bump
git add desktop/
git commit -m "chore: bump version to 0.x.y"
git push origin main
git push github main

# 3. Tag and push
git tag v0.x.y
git push origin v0.x.y
git push github v0.x.y

# 4. Watch the release workflow at:
# https://github.com/Euraika-Labs/pan-agent/actions
```

If the release fails partway through, delete the tag + release and retry:

```bash
gh release delete v0.x.y --yes
git tag -d v0.x.y
git push origin :refs/tags/v0.x.y
git push github :refs/tags/v0.x.y
git tag v0.x.y
git push origin v0.x.y
git push github v0.x.y
```

## Required GitHub secrets

| Secret | Value source |
|---|---|
| `TAURI_SIGNING_PRIVATE_KEY` | Contents of the `pan-agent.key` file (Ed25519 private key, base64-encoded by the Tauri signer) |
| `TAURI_SIGNING_PRIVATE_KEY_PASSWORD` | Password set during `npx @tauri-apps/cli signer generate` (can be empty) |

The public key is embedded in `tauri.conf.json` `plugins.updater.pubkey`. Never commit the private key.

## Generating a new signing key

Run once, locally:

```bash
npx @tauri-apps/cli signer generate -w pan-agent.key
```

This produces:
- `pan-agent.key` — private key. Add contents to GitHub secrets. **Do not commit.**
- `pan-agent.key.pub` — public key. Embed in `tauri.conf.json` and commit.

`.gitignore` already excludes `*.key` and includes `*.key.pub` (negation rule).

## Rolling the signing key

If the private key leaks:

1. Generate a new key pair with `signer generate`.
2. Update `pan-agent.key.pub` in the repo.
3. Update `tauri.conf.json` `plugins.updater.pubkey` with the new public key.
4. Update the GitHub secret `TAURI_SIGNING_PRIVATE_KEY`.
5. Cut a new release.

**Warning**: existing installs will refuse to apply updates signed with the new key (the old public key is baked into their binary). Affected users will need to download the installer manually.

## Common CI failures

| Symptom | Likely cause | Fix |
|---|---|---|
| `npm ci ... package.json and package-lock.json out of sync` | New npm dep added without `npm install` | Run `npm install` locally and commit `package-lock.json` |
| `CGDisplayCreateImageForRect is unavailable` (macOS) | `kbinani/screenshot` uses an API removed in macOS 15 SDK | Set `MACOSX_DEPLOYMENT_TARGET=14.0` in the env block |
| `npm error Missing script: tauri` | `tauri-action` runs `npm run tauri` but the script doesn't exist | Add `"tauri": "tauri"` to `package.json` scripts |
| `A public key has been found, but no private key` | Tauri build needs signing but `TAURI_SIGNING_PRIVATE_KEY` is missing | Add the secret to GitHub repo settings |
| `cannot use _cgo2 (variable of type *_Ctype_CFTypeRef) as *unsafe.Pointer value` | macOS CGo type mismatch | Use `var x unsafe.Pointer` instead of `var x C.CFTypeRef` for `CFDictionaryGetValueIfPresent` |

## Read next
- [[03 - Auto-Update System]]
- [[04 - Build and CI Issues]]
