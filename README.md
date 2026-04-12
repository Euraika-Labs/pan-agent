# Pan-Agent

AI desktop agent with full PC control. Built in Go.

## Quick start

```sh
go build -o pan-agent ./cmd/pan-agent
./pan-agent serve
```

## Architecture

- Go HTTP API server on localhost:8642
- OpenAI-compatible streaming chat
- 16 tool implementations (terminal, browser, filesystem, etc.)
- Level 1/2 dangerous command approval system
- SQLite session persistence
- Designed as backend for Tauri desktop app (React frontend)
