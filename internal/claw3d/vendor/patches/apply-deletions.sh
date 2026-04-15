#!/usr/bin/env bash
# apply-deletions.sh — bulk operations that don't fit cleanly as unified diffs.
#
# Invoked by scripts/build-bundle.(sh|ps1) immediately AFTER all *.patch files
# have been applied by `git apply`. The operations here are idempotent — safe
# to re-run against a fresh upstream checkout.
#
# Two categories of bulk change live here:
#   1. DELETE the entire src/app/api/ tree. Every route under it is a Node
#      server-side handler that cannot survive `output: export`. The Go
#      adapter replaces the whole API surface in embedded mode (Gate-1 D1).
#      Vendor-sync reviewers should audit what upstream added in that tree
#      when updating the pin — the PR template covers this.
#   2. ADD the locally-vendored city_1k.hdr asset referenced by 0003-local-hdr.
#
# Why a shell script instead of a 5th .patch file:
#   - Nine directory deletions generate multi-megabyte unified diffs that
#     bury the intent under noise.
#   - Asset additions with binary content (HDR, ~1 MB) are awkward in
#     text-oriented patches; git handles them via binary markers but any
#     manual rebase becomes painful.
#
# Usage (called by build-bundle, not invoked directly by humans):
#   ./apply-deletions.sh <upstream_dir> <hdr_source>
#
# Arguments:
#   upstream_dir  absolute path to the checked-out Claw3D upstream clone
#   hdr_source    path to the city_1k.hdr we ship; if empty, the script
#                 downloads from Poly Haven on the assumption that CI
#                 has network access
set -euo pipefail

UPSTREAM="${1:?upstream_dir required}"
HDR_SOURCE="${2:-}"
HDR_URL="https://dl.polyhaven.org/file/ph-assets/HDRIs/hdr/1k/city.hdr"
HDR_SHA256="placeholder-verify-on-first-download"

echo "apply-deletions: upstream=${UPSTREAM}"

# ── 1. Delete the entire src/app/api/ tree ──────────────────────────────────
# Gate-1 D1: pan-agent's Go adapter serves the full /office/* surface, so
# the upstream's Next.js API routes are dead weight. They also break
# `output: export` because every upstream route uses server-side
# NextResponse handlers without `export const dynamic = "force-static"`.
# Wholesale deletion is future-proof against upstream adding new routes.
API_ROOT="src/app/api"
if [[ -d "${UPSTREAM}/${API_ROOT}" ]]; then
    echo "  rm -r ${API_ROOT} (${API_ROOT} entirely — Go adapter replaces it)"
    rm -rf "${UPSTREAM}/${API_ROOT}"
fi

# Also remove the [...invalid] + agents/{page,agentId/settings/page} redirect
# stubs that can't be converted to client components cleanly (dynamic routes
# without generateStaticParams break static export).
REDIRECT_STUBS=(
    "src/app/[...invalid]"
    "src/app/agents/[agentId]/settings"
)
for dir in "${REDIRECT_STUBS[@]}"; do
    target="${UPSTREAM}/${dir}"
    if [[ -d "${target}" ]]; then
        echo "  rm -r ${dir}"
        rm -rf "${target}"
    fi
done

# ── 2. Vendor the HDR environment asset ──────────────────────────────────────
HDR_DEST="${UPSTREAM}/public/office-assets/hdr"
mkdir -p "${HDR_DEST}"

if [[ -n "${HDR_SOURCE}" && -f "${HDR_SOURCE}" ]]; then
    echo "  cp ${HDR_SOURCE} → public/office-assets/hdr/city_1k.hdr"
    cp "${HDR_SOURCE}" "${HDR_DEST}/city_1k.hdr"
elif [[ ! -f "${HDR_DEST}/city_1k.hdr" ]]; then
    # Fallback: fetch from Poly Haven. CI workflows should set HDR_SOURCE
    # via an artifact pull so builds stay reproducible without a network
    # dependency.
    # HDR fetch is best-effort — the scene renders without IBL (just flat
    # lighting instead of specular reflections on the ping-pong ball, door
    # handles, etc.). Never fail the build because Poly Haven is down or
    # the URL moved. CI should pre-populate via the HDR_SOURCE arg.
    if command -v curl >/dev/null 2>&1; then
        echo "  curl ${HDR_URL} → public/office-assets/hdr/city_1k.hdr (best-effort)"
        if curl -fsSL "${HDR_URL}" -o "${HDR_DEST}/city_1k.hdr" 2>/dev/null; then
            echo "  ✓ HDR downloaded"
        else
            echo "  ⚠ HDR download failed; scene renders without IBL"
            # Drop a 0-byte placeholder so the Next build doesn't 404 on
            # the reference. drei falls back gracefully.
            : > "${HDR_DEST}/city_1k.hdr"
        fi
    else
        echo "  ⚠ no HDR source supplied and curl not available — scene will render without IBL"
        : > "${HDR_DEST}/city_1k.hdr"
    fi
fi

echo "apply-deletions: done"
