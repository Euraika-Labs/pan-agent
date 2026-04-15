# Patch set applied on top of `upstream.sha`

Numbered `git format-patch` files applied in sort order. Each patch is
**intent-documented** in its own commit message; the ordering matters when
later patches assume earlier changes are already in place.

| # | File | Intent |
|---|---|---|
| 0001 | `frame-ancestors.patch` | Relax CSP `frame-ancestors` so pan-agent's Tauri shell (`tauri://localhost`, `http://localhost:5173`) can iframe Claw3D. Upstream ships `frame-ancestors 'self'` which blocks this. |
| 0002 | `local-hdr.patch` | Replace drei `<Environment preset="city">` (which fetches from `market.pmnd.rs` CDN at runtime) with `<Environment files="/office-assets/hdr/city_1k.hdr" />` so offline/air-gapped installs still render. The HDR file itself is committed under `assets/hdr/` by this patch. |
| 0003 | `static-export.patch` | Set `output: 'export'` + `images.unoptimized: true` + `trailingSlash: true` in `next.config.ts`. Remove `headers()` (pan-agent's Go gateway serves CSP). Delete 9 complex API routes that require runtime Node (Gate-1 D1 decision: port 12 thin routes, 501 the rest). Deleted: `api/office/voice/*`, `api/office/standup/*`, `api/office/github`, `api/office/browser-preview`, `api/office/call`, `api/office/remote-message`, `api/task-store`, `api/runtime/custom`, `api/gateway/ws` (replaced by Go adapter). |
| 0004 | `redirect-pages-client.patch` | Convert three server-component `redirect("/office")` pages to client-side `"use client"` + `useRouter().replace()` so static export doesn't fail on dynamic-route redirects. Affects `src/app/page.tsx`, `src/app/[...invalid]/page.tsx`, `src/app/agents/page.tsx`, `src/app/agents/[agentId]/settings/page.tsx`. |

## Regenerating patches

After manually re-applying a patch's intent on a new upstream:

```bash
cd internal/claw3d/vendor/upstream
git format-patch -1 --stdout HEAD > ../patches/0001-frame-ancestors.patch
# repeat for each patch
```

The build script applies patches with `git apply --check` first — any
failure aborts before `npm ci`, so you never waste a 2-minute install on
a broken patch set.

## First-time setup note (M3 bootstrap)

The four `.patch` files in this directory are **placeholders** until the
first build-bundle run produces them. The build script detects an empty
patches directory, checks out upstream, applies the intended changes
programmatically (via `sed`/`jq` as appropriate), commits, and
`format-patch`es the result. This bootstrap runs once; subsequent bumps
use the committed `.patch` files directly.

See `scripts/build-bundle.ps1` for the bootstrap logic.
