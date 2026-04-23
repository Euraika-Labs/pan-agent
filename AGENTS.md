# Repository Guidelines

## Project Structure & Module Organization

- `cmd/pan-agent/` contains the CLI entry point for `serve`, `chat`, `doctor`, and `version`.
- `internal/` contains backend packages: `gateway/` for HTTP routes and chat flow, `tools/` for agent tools, `approval/` for command safety checks, `storage/` for SQLite persistence, `skills/`, and `claw3d/`.
- `desktop/src/` contains React UI code, `desktop/src-tauri/` contains the Rust shell, and `desktop/tests/` contains desktop E2E suites.
- `docs/` contains the manual, runbooks, design notes, and `docs/openapi.yaml`.
- Assets live in `desktop/src/assets/`, `desktop/src-tauri/icons/`, and `panagent.png`.

## Build, Test, and Development Commands

Run backend checks from the repository root:

```sh
go build ./...                              # compile all Go packages
go test ./... -count=1 -timeout 120s        # run the Go test suite
go run ./cmd/pan-agent serve --port 8642    # start the local API server
bash scripts/verify-api.sh                  # check route/OpenAPI drift
```

Run desktop commands from `desktop/`:

```sh
npm ci                 # install locked frontend dependencies
npm run dev:vite       # run the browser dev UI at localhost:5173
npm run dev            # run Vite plus Tauri dev shell
npm run typecheck      # run TypeScript checks
npm run build:vite     # build web assets
```

## Coding Style & Naming Conventions

Format Go with `gofmt`/`goimports`; CI runs `golangci-lint` with `govet`, `staticcheck`, `ineffassign`, and `unused`. Keep package names lowercase. Use platform suffixes and build tags consistently, for example `keyboard_linux.go`, `keyboard_windows.go`, and `snapshot_stub.go`.

React components use PascalCase `.tsx` files under `desktop/src/screens/` or `desktop/src/components/`. Keep TypeScript types explicit at API boundaries and follow existing CSS patterns.

## Testing Guidelines

Place Go tests beside the package under test as `*_test.go`; use `testdata/` for fixtures and `t.TempDir()` for filesystem isolation. Run `go test ./... -count=1 -timeout 120s` before submitting. Chaos tests run separately with `go test -tags chaos ./internal/claw3d/`.

For frontend changes, run `npm run typecheck`. E2E coverage lives under `desktop/tests/e2e/` and `desktop/tests/real-webview/`; update those when UI behavior changes.

## Commit & Pull Request Guidelines

Use Conventional Commits as seen in history: `feat(recovery): ...`, `fix(security): ...`, `chore(release): ...`. Keep scopes short and meaningful.

Pull requests should include a summary, changes, test plan, and related issues. Add screenshots or recordings for visible UI changes. The primary repo is GitLab; GitHub is a mirror for releases and CI.

## Security & Configuration Notes

Never commit private Tauri signing keys, API keys, local `.env` files, or profile data. When adding API routes, update `docs/openapi.yaml` or add a documented exemption in `scripts/openapi-exempt.txt`.
