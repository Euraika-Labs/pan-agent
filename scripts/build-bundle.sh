#!/usr/bin/env bash
# scripts/build-bundle.sh — Claw3D bundle build for Linux/macOS/CI.
#
# Mirrors scripts/build-bundle.ps1. Keep the two in lockstep when either is
# modified; the PowerShell variant has inline comments explaining intent.
#
# Usage:
#   ./scripts/build-bundle.sh                 # full build
#   ./scripts/build-bundle.sh --dry-run       # apply patches, skip Node
#   ./scripts/build-bundle.sh --clean         # remove upstream/ caches
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VENDOR_DIR="${REPO_ROOT}/internal/claw3d/vendor"
UPSTREAM_DIR="${VENDOR_DIR}/upstream"
PATCHES_DIR="${VENDOR_DIR}/patches"
BUNDLE_DIR="${REPO_ROOT}/internal/claw3d/bundle"
UPSTREAM_URL="$(<"${VENDOR_DIR}/upstream.url" tr -d '\r\n')"
UPSTREAM_SHA="$(<"${VENDOR_DIR}/upstream.sha" tr -d '\r\n')"

DRY_RUN=0
CLEAN=0
for a in "$@"; do
    case "$a" in
        --dry-run) DRY_RUN=1 ;;
        --clean)   CLEAN=1 ;;
        -h|--help) grep '^#' "$0" | head -30; exit 0 ;;
        *) echo "unknown flag: $a"; exit 2 ;;
    esac
done

phase() { printf '\n\033[36m═══ %s ═══\033[0m\n' "$1"; }

if [[ "$CLEAN" == 1 ]]; then
    phase "Clean"
    rm -rf "${UPSTREAM_DIR}"
    echo "cleaned upstream/"
    exit 0
fi

phase "Vendor baseline — pin ${UPSTREAM_SHA:0:12} @ ${UPSTREAM_URL}"
if [[ ! -d "${UPSTREAM_DIR}" ]]; then
    git clone --quiet "${UPSTREAM_URL}" "${UPSTREAM_DIR}"
fi
git -C "${UPSTREAM_DIR}" fetch --quiet origin
git -C "${UPSTREAM_DIR}" reset --quiet --hard "${UPSTREAM_SHA}"
git -C "${UPSTREAM_DIR}" clean --quiet -fdx
echo "checked out ${UPSTREAM_SHA}"

phase "Apply patches"
shopt -s nullglob
patches=( "${PATCHES_DIR}"/*.patch )
if [[ ${#patches[@]} -eq 0 ]]; then
    echo "warning: no patches/*.patch found — building pristine upstream"
fi
for p in "${patches[@]}"; do
    echo "  applying $(basename "$p")"
    git -C "${UPSTREAM_DIR}" apply --check "$p"
    git -C "${UPSTREAM_DIR}" apply "$p"
done

# Bulk operations that don't fit cleanly as .patch files (directory removals,
# binary asset additions). Always runs after .patch application, never before.
if [[ -x "${PATCHES_DIR}/apply-deletions.sh" ]]; then
    echo "  running apply-deletions.sh"
    bash "${PATCHES_DIR}/apply-deletions.sh" "${UPSTREAM_DIR}" "${HDR_SOURCE:-}"
fi

if [[ "$DRY_RUN" == 1 ]]; then
    phase "DryRun — patches validated, skipping Node build"
    exit 0
fi

phase "npm ci"
( cd "${UPSTREAM_DIR}" && npm ci --silent )

phase "next build && next export"
( cd "${UPSTREAM_DIR}" && npm run build )
if [[ ! -d "${UPSTREAM_DIR}/out" ]]; then
    ( cd "${UPSTREAM_DIR}" && npx next export -o out )
fi

phase "Pre-gzip text assets"
find "${UPSTREAM_DIR}/out" \( -name '*.js' -o -name '*.css' -o -name '*.html' -o -name '*.svg' \) -type f \
    -exec sh -c 'gzip -9 -k -f "$1"' _ {} \;
n_gz=$(find "${UPSTREAM_DIR}/out" -name '*.gz' -type f | wc -l)
echo "  gzipped ${n_gz} files"

phase "Copy out/ → ${BUNDLE_DIR}"
rm -rf "${BUNDLE_DIR}"
mkdir -p "${BUNDLE_DIR}"
cp -R "${UPSTREAM_DIR}/out/." "${BUNDLE_DIR}/"
find "${BUNDLE_DIR}" -name '*.map' -type f -delete
echo "  copied bundle + stripped source maps"

phase "Stamp BundleSHA256"
( cd "${REPO_ROOT}" && go generate ./internal/claw3d/... )

phase "Bundle report"
bundle_bytes=$(find "${BUNDLE_DIR}" -type f -exec stat -c%s {} + 2>/dev/null | awk '{s+=$1} END {print s}')
bundle_files=$(find "${BUNDLE_DIR}" -type f | wc -l)
echo "  files : ${bundle_files}"
echo "  size  : $(awk -v b="${bundle_bytes:-0}" 'BEGIN{printf "%.2f MB", b/1024/1024}')"
echo ""
printf '\033[32m✓ Bundle built and embedded. Run `go build ./...` to verify.\033[0m\n'
