# Claw3D vendor contract

This directory pins a specific upstream Claw3D commit and the patch set we
apply to produce the static bundle that ships inside `pan-agent`. It is the
**source of truth** for "which Claw3D are we shipping."

## Files

| File | Role |
|---|---|
| `upstream.sha` | 40-char commit SHA on `iamlukethedev/Claw3D:main` that we track |
| `upstream.url` | Canonical upstream URL (don't change unless upstream moves) |
| `patches/*.patch` | Ordered `git format-patch` files applied on top of `upstream.sha` |
| `patches/README.md` | Human-readable intent for each patch |
| `upstream/` | Transient clone — populated by `scripts/build-bundle.*`, gitignored |

## Current pin

- **Upstream**: `iamlukethedev/Claw3D`
- **SHA**: `04efa31c4014566e4d16ec811f1793d71870c6f5`
- **Date verified**: 2026-04-12 (build-bundle script reports on each run)
- **License**: MIT (Copyright © 2026 Luke The Dev)

## Build pipeline

```
scripts/build-bundle.(sh|ps1)
  1. Ensure upstream/ is a clean clone of upstream.url at upstream.sha
  2. For each patches/NNNN-*.patch (sorted):
        git -C upstream apply --check <p>  # dry-run validates
        git -C upstream apply <p>
  3. cd upstream && npm ci && npm run build && npx next export -o ../out
  4. Pre-gzip *.js *.css *.html *.svg in out/ (serve with Content-Encoding)
  5. rsync out/ → internal/claw3d/bundle/  (overwrite, strip source maps)
  6. go generate ./internal/claw3d/...     (stamps BundleSHA256)
  7. Report bundle size + SHA256 + file count
```

Running with `-n` / `--dry-run` skips steps 3-6 so contributors can validate
patches apply cleanly without a full Node build.

## Bumping the pin

Procedure for moving to a newer upstream commit:

1. Update `upstream.sha` to the new commit.
2. Run `scripts/build-bundle.ps1 --dry-run`. Any patch that doesn't apply
   cleanly will fail here with the rejected hunk — do NOT force.
3. For a rejected patch, rebase it: check out the new upstream, manually
   re-do the patch's intent, `git format-patch -1` → overwrite the file.
4. Run the full build. Verify bundle size delta is reasonable (<10% creep
   or an intentional reason).
5. Commit `upstream.sha` + the regenerated patches + the rebuilt bundle in
   ONE commit titled `chore(claw3d): sync upstream to <short-sha>`.
6. The CI workflow `.github/workflows/vendor-sync.yml` does steps 1-4
   automatically on a weekly cron and opens a draft PR — which is the
   preferred path.

## Why not a submodule?

Git submodules make patches painful to manage (every bump is a force-push on
the submodule branch). Pinning via `upstream.sha` + `git apply`-able
`.patch` files keeps the history of what we changed and why explicit, and
makes rebase-on-new-upstream a normal review flow.

## Why not a fork repo?

We could maintain `euraika-labs/claw3d-patched` as a rebased fork. Two
reasons we don't:

1. **Distribution** — users install `pan-agent`, not `claw3d-patched`. The
   fork only exists to produce a bundle; a patches directory achieves the
   same outcome with less infrastructure.
2. **Upstream alignment** — a rebased fork silently diverges over time. A
   patches directory FAILS LOUDLY when a rebase conflicts, forcing us to
   confront the divergence during a bump.
