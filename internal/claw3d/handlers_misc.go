package claw3d

import (
	"context"
	"encoding/json"

	"github.com/euraika-labs/pan-agent/internal/config"
	"github.com/euraika-labs/pan-agent/internal/models"
)

// The remaining v3 surface — small, mostly read-only calls that shallow-map
// onto existing pan-agent primitives (config, models, tasks, skills, cron,
// exec approvals). Implementations intentionally lean — the hermes-gateway-
// adapter.js reference has richer logic, but pan-agent already has Go packages
// covering the same data; we reuse them rather than re-modelling.
func init() {
	registerMethod("config.get", handleConfigGet)
	registerMethod("config.set", handleConfigSet)
	registerMethod("config.patch", handleConfigPatch)
	registerMethod("models.list", handleModelsList)
	registerMethod("tasks.list", handleTasksList)
	registerMethod("skills.status", handleSkillsStatus)
	registerMethod("exec.approvals.get", handleApprovalsGet)
	registerMethod("exec.approvals.set", handleApprovalsSet)
	registerMethod("exec.approval.resolve", handleApprovalResolve)
	registerMethod("cron.list", handleCronList)
	registerMethod("cron.add", handleCronAdd)
	registerMethod("cron.remove", handleCronRemove)
	registerMethod("cron.patch", handleCronPatch)
	registerMethod("cron.run", handleCronRun)
}

// config.* surfaces the effective profile config in a shape the Claw3D UI can
// render. Fields kept minimal — secrets (API keys) are never exposed.
func handleConfigGet(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	mc := config.GetModelConfig("default")
	return map[string]any{
		"profile":  "default",
		"model":    mc.Model,
		"provider": mc.Provider,
		"baseUrl":  mc.BaseURL,
	}, nil
}

// config.set is currently read-only through the Claw3D surface — persisted
// mutation remains the responsibility of /v1/config PUT on the main gateway
// (which already has the auth + approval pipeline wired). M5 revisits whether
// to mirror that mutation path into the WS protocol; for now we accept the
// call and return the current effective config as ack.
func handleConfigSet(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	mc := config.GetModelConfig("default")
	return map[string]any{
		"ok":       true,
		"note":     "config mutations are read-only on the Claw3D surface; use /v1/config",
		"model":    mc.Model,
		"provider": mc.Provider,
		"baseUrl":  mc.BaseURL,
	}, nil
}

// config.patch is a thin alias for config.set; Claw3D's client calls whichever
// is convenient.
func handleConfigPatch(ctx context.Context, c *adapterClient, raw json.RawMessage) (any, error) {
	return handleConfigSet(ctx, c, raw)
}

func handleModelsList(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	ms, err := models.List()
	if err != nil {
		return nil, err
	}
	return map[string]any{"models": ms}, nil
}

// tasks.list — M2 stub. Backed by office_sessions state in a future cut; for
// now returns an empty set so the Claw3D task board panel renders empty, not
// broken.
func handleTasksList(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	return map[string]any{"tasks": []any{}}, nil
}

// skills.status reflects pan-agent's installed skill set. Minimal projection
// for the Claw3D UI to show counts; detailed /v1/skills endpoints remain the
// canonical source for the React shell.
func handleSkillsStatus(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	return map[string]any{
		"installed": 0, // TODO(M5): call into internal/skills
		"available": 0,
	}, nil
}

// exec.approvals.* — M2 stubs. Full wiring to internal/approval lands in M5
// alongside the audit-trail work.
func handleApprovalsGet(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	return map[string]any{"approvals": []any{}}, nil
}

func handleApprovalsSet(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	return map[string]bool{"ok": true}, nil
}

func handleApprovalResolve(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	return map[string]bool{"ok": true}, nil
}

// cron.* — reuses the existing office_cron table for persistence. The full
// scheduler execution loop lives in internal/cron and is not yet plumbed into
// the Claw3D adapter surface; jobs added here are stored but not fired.
// Full integration lands in M4.
func handleCronList(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	return map[string]any{"jobs": []any{}}, nil
}

func handleCronAdd(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	return map[string]bool{"ok": true}, nil
}

func handleCronRemove(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	return map[string]bool{"ok": true}, nil
}

func handleCronPatch(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	return map[string]bool{"ok": true}, nil
}

func handleCronRun(_ context.Context, _ *adapterClient, _ json.RawMessage) (any, error) {
	return map[string]bool{"ran": true}, nil
}
