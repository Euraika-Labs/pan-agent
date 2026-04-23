# Build and CI Issues

This runbook covers problems with local builds and CI pipelines.

## Symptom: `cargo check` fails with `rustup could not choose a version of cargo to run`

Full error:
```
error: rustup could not choose a version of cargo to run, because one wasn't specified explicitly, and no default is configured.
help: run 'rustup default stable' to download the latest stable release of Rust and set it as your default toolchain.
```

### Cause

Rustup is installed, but no default Rust toolchain has been configured for this user yet.

### Fix

```bash
rustup default stable
rustup show
cargo --version
rustc --version
cd desktop
npm run check:tauri
```

If you are trying to produce a native desktop bundle, also make sure Node/npm dependencies are installed with `cd desktop && npm ci`, Linux Tauri packages are present on Ubuntu/WSL hosts, and the Go sidecar was built into `desktop/src-tauri/binaries/` with the correct target-triple filename before `npx tauri build`.

On Ubuntu/WSL, `npm run check:tauri` (which targets `desktop/src-tauri/`) also needs the GTK/WebKit pkg-config stack available locally (`atk`, `gdk-3.0`, `pango`, `gdk-pixbuf-2.0`, WebKitGTK, related headers). The GitHub Actions Tauri/lint jobs install those packages explicitly; if they are missing on your machine, expect a system-library failure before Rust code is compiled.

## Symptom: `npm ci` fails with "package.json and package-lock.json out of sync"

Full error:
```
npm error `npm ci` can only install packages when your package.json
and package-lock.json or npm-shrinkwrap.json are in sync.
Please update your lock file with `npm install` before continuing.
```

### Cause

A new dependency was added to `package.json` but `package-lock.json` wasn't regenerated.

### Fix

```bash
cd desktop
npm install
git add package-lock.json
git commit -m "chore: sync package-lock.json"
```

## Symptom: macOS build fails with "CGDisplayCreateImageForRect is unavailable"

Full error:
```
error: 'CGDisplayCreateImageForRect' is unavailable:
obsoleted in macOS 15.0 - Please use ScreenCaptureKit instead.
```

### Cause

`kbinani/screenshot` uses `CGDisplayCreateImageForRect` which was marked unavailable (not just deprecated) in the macOS 15 SDK shipped with Xcode 16+.

### Fix

Set `MACOSX_DEPLOYMENT_TARGET=14.0` before building. This tells the compiler to target the macOS 14 SDK where the API is still available.

```bash
# Locally
export MACOSX_DEPLOYMENT_TARGET=14.0
go build -o pan-agent ./cmd/pan-agent

# In CI (.github/workflows/*.yml)
- name: Build Go binary (macOS)
  run: go build -o ... ./cmd/pan-agent
  env:
    MACOSX_DEPLOYMENT_TARGET: "14.0"
```

This is wired up in both `ci.yml` and `release.yml` for macOS jobs.

## Symptom: Tauri build: "A public key has been found, but no private key"

Full error:
```
Error A public key has been found, but no private key.
Make sure to set `TAURI_SIGNING_PRIVATE_KEY` environment variable.
```

### Cause

`tauri.conf.json` has `bundle.createUpdaterArtifacts: "v1Compatible"` and `plugins.updater.pubkey` set. With these enabled, the Tauri build requires the matching private key to sign the updater artifacts.

### Fix

Set the env var. In CI, this comes from a GitHub secret:

```yaml
env:
  TAURI_SIGNING_PRIVATE_KEY: ${{ secrets.TAURI_SIGNING_PRIVATE_KEY }}
  TAURI_SIGNING_PRIVATE_KEY_PASSWORD: ${{ secrets.TAURI_SIGNING_PRIVATE_KEY_PASSWORD }}
```

To configure the secret:
1. https://github.com/Euraika-Labs/pan-agent/settings/secrets/actions
2. New repository secret → name `TAURI_SIGNING_PRIVATE_KEY`, value = full contents of `pan-agent.key`.
3. If you set a key password, also add `TAURI_SIGNING_PRIVATE_KEY_PASSWORD`.

For local dev builds without signing:
```bash
npx tauri build --no-sign
```

(`--no-sign` is the reliable way to skip updater signing for local testing builds.)

## Symptom: tauri-action fails: "Missing script: tauri"

Full error:
```
npm error Missing script: "tauri"
```

### Cause

`tauri-apps/tauri-action@v0` runs `npm run tauri build`. The `tauri` script must exist in `package.json`.

### Fix

```json
{
  "scripts": {
    "tauri": "tauri",
    "build:tauri": "tauri build",
    ...
  }
}
```

The `tauri` script just delegates to the `tauri` CLI (provided by `@tauri-apps/cli` in `devDependencies`).

## Symptom: macOS CGo compile error in window_manager_darwin.go

Error:
```
cannot use _cgo2 (variable of type *_Ctype_CFTypeRef) as
*unsafe.Pointer value in argument to _Cfunc_CFDictionaryGetValueIfPresent
```

### Cause

`CFDictionaryGetValueIfPresent` expects `*unsafe.Pointer`, not `*C.CFTypeRef`. Older code (or AI-written code) often gets this wrong.

### Fix

```go
// Wrong
var nameRef C.CFTypeRef
C.CFDictionaryGetValueIfPresent(dict, key, &nameRef)

// Right
var namePtr unsafe.Pointer
C.CFDictionaryGetValueIfPresent(dict, key, &namePtr)
title := cfStringToGo(C.CFStringRef(namePtr))
```

## Symptom: CI hangs / takes hours

Full Tauri builds on Windows and macOS can take 5-10 minutes each. The Rust compilation is the main bottleneck.

### Mitigations

`swatinem/rust-cache@v2` is used in the release workflow. It caches `desktop/src-tauri/target` between runs. The first run after a Cargo.toml change is slow; subsequent runs are fast.

If a job runs >20 minutes, it's likely stuck — cancel and retry.

## Symptom: Release tag pushed but no release appears

You ran `git push origin v0.x.y` but nothing happened on GitHub.

### Causes

**Cause 1: Tag was pushed to GitLab only.**

GitLab is the primary repo, GitHub is the mirror. The release workflow runs on GitHub. Push the tag to both:

```bash
git push origin v0.x.y    # GitLab
git push github v0.x.y    # GitHub
```

**Cause 2: Workflow file isn't on the tag's commit.**

The release workflow runs the version of `release.yml` that exists on the tagged commit. If you tagged a commit before the workflow was added, no workflow runs.

Move the tag forward:

```bash
gh release delete v0.x.y --yes
git tag -d v0.x.y
git push origin :refs/tags/v0.x.y
git push github :refs/tags/v0.x.y
git tag v0.x.y  # now points at HEAD which has the workflow
git push origin v0.x.y
git push github v0.x.y
```

**Cause 3: Workflow failed before creating the release.**

Check https://github.com/Euraika-Labs/pan-agent/actions for failed runs.

## Symptom: Windows build flagged by Defender

```
github.com/euraika-labs/pan-agent/cmd/pan-agent:
open ...\a.out.exe: De bewerking is niet voltooid omdat het bestand
een virus of mogelijk ongewenste software bevat.
```

### Cause

Windows Defender false-positive on Go binaries that include networking libraries (Discord WebSocket triggers it most often).

### Fix

This affects local Windows builds only — CI runs on Linux/macOS where it doesn't happen. Workarounds:

1. Add an exclusion for your build directory in Windows Security → Virus & threat protection → Manage settings → Exclusions.
2. Use the standalone binary (no Tauri Discord deps) for local CLI work.
3. Submit the binary to Microsoft as a false positive: https://www.microsoft.com/en-us/wdsi/filesubmission.

## Read next
- [[02 - Build and Release Pipeline]]
- [[03 - Auto-Update System]]
- [[00 - Troubleshooting Index]]
