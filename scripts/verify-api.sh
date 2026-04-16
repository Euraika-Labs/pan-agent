#!/usr/bin/env bash
# verify-api.sh — lightweight contract check between the Go route table
# and docs/openapi.yaml.
#
# For every "METHOD /path" registered in internal/gateway/routes.go,
# either:
#   - the path appears under `paths:` in docs/openapi.yaml, OR
#   - the path is explicitly listed in scripts/openapi-exempt.txt
#
# This is deliberately not a full OpenAPI validator — it only catches the
# most common drift (a new route lands without being spec'd).
#
# Exit codes:
#   0 — every registered path is documented or exempt
#   1 — one or more undocumented paths found
#
# Usage: bash scripts/verify-api.sh
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ROUTES="$REPO_ROOT/internal/gateway/routes.go"
OPENAPI="$REPO_ROOT/docs/openapi.yaml"
EXEMPT="$REPO_ROOT/scripts/openapi-exempt.txt"

if [[ ! -f "$ROUTES" ]]; then
  echo "verify-api: routes.go not found at $ROUTES" >&2
  exit 2
fi
if [[ ! -f "$OPENAPI" ]]; then
  echo "verify-api: openapi.yaml not found at $OPENAPI" >&2
  exit 2
fi

# Extract every "METHOD /v1/..." path from mux.HandleFunc("...", ...).
# macOS bash 3.2 lacks mapfile, so read into arrays the old-fashioned way.
routes=()
while IFS= read -r line; do
  routes+=("$line")
done < <(grep -oE 'HandleFunc\("(GET|POST|PUT|DELETE|PATCH) /v1/[^"]+"' "$ROUTES" \
  | sed -E 's|^HandleFunc\("([A-Z]+) (/v1/[^"]+)"|\2|' \
  | sort -u)

documented=()
while IFS= read -r line; do
  documented+=("$line")
done < <(awk '
  /^paths:/ { inside=1; next }
  inside && /^[^ ]/ { inside=0 }
  inside && /^  \//{
    sub(/:$/, "")
    sub(/^  /, "")
    print
  }
' "$OPENAPI" | sort -u)

# Load exemptions (one path per line; `#` comments allowed).
exempt=()
if [[ -f "$EXEMPT" ]]; then
  while IFS= read -r line; do
    line="${line%%#*}"  # strip comments
    line="${line//[[:space:]]/}"
    [[ -n "$line" ]] && exempt+=("$line")
  done < "$EXEMPT"
fi

# Path params in routes are `{id}` in Go's ServeMux; they are `{id}` in
# OpenAPI too (same syntax). So we can compare raw strings.
missing=()
for r in "${routes[@]}"; do
  # Strip any METHOD prefix that leaked through.
  path="$r"
  found=0
  for d in "${documented[@]}"; do
    if [[ "$path" == "$d" ]]; then found=1; break; fi
  done
  if (( found == 0 )) && (( ${#exempt[@]} > 0 )); then
    for e in "${exempt[@]}"; do
      if [[ "$path" == "$e" ]]; then found=1; break; fi
    done
  fi
  if (( found == 0 )); then
    missing+=("$path")
  fi
done

if (( ${#missing[@]} == 0 )); then
  echo "verify-api: OK — ${#routes[@]} routes, ${#documented[@]} documented, ${#exempt[@]} exempt"
  exit 0
fi

echo "verify-api: ${#missing[@]} route(s) lack OpenAPI documentation or exemption:" >&2
for m in "${missing[@]}"; do
  echo "  - $m" >&2
done
echo >&2
echo "Fix by either:" >&2
echo "  1. documenting the path under 'paths:' in docs/openapi.yaml, or" >&2
echo "  2. adding it to scripts/openapi-exempt.txt with a one-line comment." >&2
exit 1
