package storage

import (
	"database/sql"
	"fmt"
)

// LogSkillUsage records a single skill invocation. Returns the new row ID.
func (d *DB) LogSkillUsage(u SkillUsage) (int64, error) {
	const q = `
INSERT INTO skill_usage (session_id, skill_id, message_id, used_at, outcome, context_hint)
VALUES (?, ?, ?, ?, ?, ?)`
	var msgID sql.NullInt64
	if u.MessageID != 0 {
		msgID = sql.NullInt64{Int64: u.MessageID, Valid: true}
	}
	res, err := d.db.Exec(q, u.SessionID, u.SkillID, msgID, u.UsedAt, u.Outcome, u.ContextHint)
	if err != nil {
		return 0, fmt.Errorf("LogSkillUsage: %w", err)
	}
	return res.LastInsertId()
}

// ListSkillUsage returns recent usage rows for a given skill, newest first.
func (d *DB) ListSkillUsage(skillID string, limit int) ([]SkillUsage, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
SELECT id, session_id, skill_id, COALESCE(message_id, 0), used_at, outcome, COALESCE(context_hint, '')
FROM skill_usage
WHERE skill_id = ?
ORDER BY used_at DESC
LIMIT ?`
	rows, err := d.db.Query(q, skillID, limit)
	if err != nil {
		return nil, fmt.Errorf("ListSkillUsage query: %w", err)
	}
	defer rows.Close()

	var out []SkillUsage
	for rows.Next() {
		var u SkillUsage
		if err := rows.Scan(&u.ID, &u.SessionID, &u.SkillID, &u.MessageID, &u.UsedAt, &u.Outcome, &u.ContextHint); err != nil {
			return nil, fmt.Errorf("ListSkillUsage scan: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountSkillUsage returns total usage count for a skill.
func (d *DB) CountSkillUsage(skillID string) (int, error) {
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM skill_usage WHERE skill_id = ?`, skillID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("CountSkillUsage: %w", err)
	}
	return n, nil
}

// SkillUsageStats aggregates usage for a skill.
type SkillUsageStats struct {
	SkillID     string `json:"skill_id"`
	TotalCount  int    `json:"total_count"`
	SuccessRate int    `json:"success_rate_pct"` // 0-100
	LastUsedAt  int64  `json:"last_used_at"`
}

// GetSkillUsageStats returns aggregate stats for a skill.
func (d *DB) GetSkillUsageStats(skillID string) (SkillUsageStats, error) {
	stats := SkillUsageStats{SkillID: skillID}
	const q = `
SELECT
  COUNT(*) AS total,
  COALESCE(MAX(used_at), 0) AS last_used,
  CAST(100.0 * SUM(CASE WHEN outcome = 'success' THEN 1 ELSE 0 END) / NULLIF(COUNT(*), 0) AS INTEGER) AS success_pct
FROM skill_usage WHERE skill_id = ?`
	var successPct sql.NullInt64
	err := d.db.QueryRow(q, skillID).Scan(&stats.TotalCount, &stats.LastUsedAt, &successPct)
	if err != nil {
		return stats, fmt.Errorf("GetSkillUsageStats: %w", err)
	}
	stats.SuccessRate = int(successPct.Int64)
	return stats, nil
}
