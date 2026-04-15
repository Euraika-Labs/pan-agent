# scripts/build-bundle.ps1 — Claw3D bundle build for Windows dev.
#
# Clones upstream Claw3D at the pinned SHA, applies patches, runs
# next build && next export, pre-gzips the output, and copies the
# result into internal/claw3d/bundle/. Idempotent — safe to re-run.
#
# Usage:
#   ./scripts/build-bundle.ps1                 # full build
#   ./scripts/build-bundle.ps1 -DryRun         # apply patches, skip Node
#   ./scripts/build-bundle.ps1 -Bootstrap      # generate patches for first time
#   ./scripts/build-bundle.ps1 -Clean          # rm upstream/ out/ node_modules/

param(
    [switch]$DryRun,
    [switch]$Bootstrap,
    [switch]$Clean
)

$ErrorActionPreference = 'Stop'
$RepoRoot = Split-Path -Parent $PSScriptRoot
$VendorDir = Join-Path $RepoRoot 'internal\claw3d\vendor'
$UpstreamDir = Join-Path $VendorDir 'upstream'
$PatchesDir = Join-Path $VendorDir 'patches'
$BundleDir = Join-Path $RepoRoot 'internal\claw3d\bundle'
$UpstreamUrl = (Get-Content (Join-Path $VendorDir 'upstream.url') -Raw).Trim()
$UpstreamSha = (Get-Content (Join-Path $VendorDir 'upstream.sha') -Raw).Trim()

function Write-Phase($name) {
    Write-Host ''
    Write-Host "═══ $name ═══" -ForegroundColor Cyan
}

if ($Clean) {
    Write-Phase 'Clean'
    if (Test-Path $UpstreamDir) { Remove-Item -Recurse -Force $UpstreamDir }
    Write-Host 'Cleaned upstream/'
    exit 0
}

Write-Phase "Vendor baseline — pin $($UpstreamSha.Substring(0,12)) @ $UpstreamUrl"

# ─── 1. Clone or fetch upstream at pinned SHA ───────────────────────────────
if (-not (Test-Path $UpstreamDir)) {
    Write-Host "Cloning $UpstreamUrl..."
    git clone --quiet $UpstreamUrl $UpstreamDir
}
Push-Location $UpstreamDir
try {
    git fetch --quiet origin
    git reset --quiet --hard $UpstreamSha
    git clean --quiet -fdx
    Write-Host "Checked out $UpstreamSha"
} finally {
    Pop-Location
}

# ─── 2. Apply patches in order ──────────────────────────────────────────────
Write-Phase 'Apply patches'
$patches = Get-ChildItem -Path $PatchesDir -Filter '*.patch' -ErrorAction SilentlyContinue | Sort-Object Name
if (-not $patches -and -not $Bootstrap) {
    Write-Warning 'No patches/*.patch found. Run with -Bootstrap to generate the initial set.'
    Write-Warning 'Falling through to a pristine upstream build.'
}
foreach ($p in $patches) {
    Write-Host "  applying $($p.Name)"
    Push-Location $UpstreamDir
    try {
        git apply --check $p.FullName
        if ($LASTEXITCODE -ne 0) {
            throw "Patch $($p.Name) does not apply cleanly against upstream $($UpstreamSha.Substring(0,12)). See patches/README.md for rebase procedure."
        }
        git apply $p.FullName
    } finally {
        Pop-Location
    }
}

# Bulk operations that don't fit cleanly as .patch files. Uses bash via Git
# for Windows (bundled with git) since PowerShell's native delete/download
# semantics diverge too much across versions to be worth rewriting.
$deletionScript = Join-Path $PatchesDir 'apply-deletions.sh'
if (Test-Path $deletionScript) {
    Write-Host '  running apply-deletions.sh'
    & bash $deletionScript $UpstreamDir ''
    if ($LASTEXITCODE -ne 0) {
        throw 'apply-deletions.sh failed'
    }
}

if ($Bootstrap) {
    Write-Phase 'Bootstrap: generate patches 0001-0004 from intent description'
    Write-Warning 'Bootstrap mode is a one-time manual flow. See patches/README.md.'
    Write-Warning 'Apply intended changes to upstream/ manually, then:'
    Write-Warning '  git -C upstream add -A && git -C upstream commit -m "0001: frame-ancestors"'
    Write-Warning '  git -C upstream format-patch -1 --stdout HEAD > patches/0001-frame-ancestors.patch'
    Write-Warning '  (repeat for 0002, 0003, 0004)'
    exit 0
}

if ($DryRun) {
    Write-Phase 'DryRun — patches validated, skipping Node build'
    exit 0
}

# ─── 3. npm ci && next build && next export ────────────────────────────────
Write-Phase 'npm ci'
Push-Location $UpstreamDir
try {
    npm ci --silent
    if ($LASTEXITCODE -ne 0) { throw 'npm ci failed' }
} finally { Pop-Location }

Write-Phase 'next build && next export'
Push-Location $UpstreamDir
try {
    # next 13+ consolidated export into build; if patches set
    # output: 'export' the build itself emits the out/ tree.
    npm run build
    if ($LASTEXITCODE -ne 0) { throw 'next build failed' }

    # Older Next may need a separate export; best-effort.
    if (-not (Test-Path 'out')) {
        npx next export -o out
        if ($LASTEXITCODE -ne 0) { throw 'next export failed' }
    }
} finally { Pop-Location }

# ─── 4. Pre-gzip text assets ───────────────────────────────────────────────
Write-Phase 'Pre-gzip .js / .css / .html / .svg'
$OutDir = Join-Path $UpstreamDir 'out'
$gzipTargets = Get-ChildItem -Path $OutDir -Recurse -Include *.js,*.css,*.html,*.svg
foreach ($f in $gzipTargets) {
    $gz = "$($f.FullName).gz"
    $fs = [System.IO.File]::OpenRead($f.FullName)
    $gs = [System.IO.Compression.GZipStream]::new(
        [System.IO.File]::Create($gz),
        [System.IO.Compression.CompressionLevel]::Optimal)
    $fs.CopyTo($gs); $gs.Close(); $fs.Close()
}
Write-Host "  gzipped $($gzipTargets.Count) files"

# ─── 5. Copy to internal/claw3d/bundle/ ────────────────────────────────────
Write-Phase "Copy out/ → $BundleDir"
if (Test-Path $BundleDir) { Remove-Item -Recurse -Force $BundleDir }
New-Item -ItemType Directory -Force -Path $BundleDir | Out-Null
Copy-Item -Recurse -Path (Join-Path $OutDir '*') -Destination $BundleDir

# Strip source maps from production bundle.
Get-ChildItem -Path $BundleDir -Recurse -Include *.map | Remove-Item
Write-Host "  copied bundle + stripped source maps"

# ─── 6. Stamp BundleSHA256 via go:generate ─────────────────────────────────
Write-Phase 'Stamp BundleSHA256'
Push-Location $RepoRoot
try {
    & go generate ./internal/claw3d/...
    if ($LASTEXITCODE -ne 0) { Write-Warning 'go generate returned non-zero — bundle_sha.go may be stale' }
} finally { Pop-Location }

# ─── 7. Report ─────────────────────────────────────────────────────────────
Write-Phase 'Bundle report'
$bundleSize = (Get-ChildItem -Path $BundleDir -Recurse -File |
    Measure-Object -Property Length -Sum).Sum
$bundleFiles = (Get-ChildItem -Path $BundleDir -Recurse -File).Count
Write-Host "  files : $bundleFiles"
Write-Host "  size  : $([math]::Round($bundleSize / 1MB, 2)) MB"
Write-Host "  sha   : (see internal/claw3d/bundle_sha.go)"
Write-Host ''
Write-Host "✓ Bundle built and embedded. Run 'go build ./...' to verify." -ForegroundColor Green
