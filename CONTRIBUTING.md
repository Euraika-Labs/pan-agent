# Contributing to Pan-Agent

Thanks for your interest in contributing. Whether it's a bug fix, new feature, improved docs, or a typo — contributions are welcome.

## Getting Started

```sh
# Clone
git clone https://git.euraika.net/euraika/pan-agent.git
cd pan-agent

# Go backend
alias go='C:/Users/bertc/go-sdk/go/bin/go.exe'  # Windows user-local install
go build -o pan-agent.exe ./cmd/pan-agent
go test ./... -count=1 -timeout 120s

# Desktop frontend
cd desktop && npm install && npm run dev:vite
npx tsc --noEmit
```

## Workflow

1. Fork or branch from `main`
2. Make your changes
3. Run `go test ./...` and `npx tsc --noEmit` — both must pass
4. Commit with a descriptive message following [Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`, `docs:`, `refactor:`, etc.)
5. Open a merge request on GitLab (primary) or a pull request on GitHub (mirror)

## Repository Layout

- **GitLab** (`git.euraika.net/euraika/pan-agent`) — source of truth for code and merge requests
- **GitHub** (`github.com/Euraika-Labs/pan-agent`) — mirror, used for releases and CI

## Code Style

- **Go:** Standard `gofmt` formatting. No third-party linter config — the stdlib conventions apply.
- **TypeScript/React:** Follow existing patterns in `desktop/src/`. No ESLint config yet — keep it consistent with what's there.
- **Tests:** Go tests use `t.TempDir()` for isolation. Frontend has no test framework yet.

## What to Work On

Check the [issues](https://github.com/Euraika-Labs/pan-agent/issues) for open tasks. If you want to work on something not listed, open an issue first to discuss.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
