## Vendor-sync PR — Claw3D upstream drift

<!--
This template is used by .github/workflows/vendor-sync.yml when the
weekly cron detects that iamlukethedev/Claw3D:main has moved past the
pin in internal/claw3d/vendor/upstream.sha. The workflow rebases the
patch set, rebuilds the bundle, and opens this PR as a draft.

Human review must confirm:
  1. The upstream diff doesn't introduce dependencies we can't vendor
  2. All four patches still apply cleanly (workflow already verified)
  3. The bundle size delta is reasonable (<10% creep, or intentional)
  4. No new third-party licenses appear (SBOM would flag on release.yml)
-->

### Upstream diff

**Previous SHA:** `{{previous_sha}}`
**New SHA:** `{{new_sha}}`
**Compare:** https://github.com/iamlukethedev/Claw3D/compare/{{previous_sha}}...{{new_sha}}

### Automated validation (CI already ran)

- [x] `scripts/build-bundle.sh --dry-run` — all patches apply cleanly
- [x] `scripts/build-bundle.sh` — full bundle rebuild
- [x] `go build ./...` — pan-agent compiles with new bundle
- [x] `go test ./...` — test suite green
- [x] Bundle SHA re-stamped via `go generate`

### Human review checklist

- [ ] Upstream changelog / commit messages reviewed for behavior changes
- [ ] Bundle size delta is within expected range (current: {{bundle_size_delta}} bytes)
- [ ] No new npm dependencies introduced (check `upstream/package-lock.json` diff)
- [ ] No AGPL / GPL-3 dependencies (SBOM job on the next release tag will flag otherwise)
- [ ] Patches directory unchanged OR updated with clear rationale in commit message

### Rollback

If the bumped bundle breaks at runtime:
1. Revert this PR
2. Revert `internal/claw3d/vendor/upstream.sha` to the previous value
3. File an issue against upstream Claw3D if the break is in their code
4. Consider adding a new patch under `internal/claw3d/vendor/patches/` if the break is specific to our usage
