package main

import (
	"flag"
	"fmt"
	"sort"

	"github.com/euraika-labs/pan-agent/internal/paths"
	"github.com/euraika-labs/pan-agent/internal/storage"
)

// cmdRAG dispatches `pan-agent rag <action>` — operational
// commands over the WS#13.B semantic-search index.
//
//	pan-agent rag stats              Counts per session + model totals.
//	pan-agent rag purge --session=X  Drop every embedding tied to one session.
//
// `pan-agent rag reindex` (delete-and-rebuild) is intentionally
// out of scope for this slice — re-embedding is a heavy operation
// that should run inside the gateway's watcher with its rate-limit
// + checkpoint logic, not from a fire-and-forget CLI.
func cmdRAG(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf(
			"missing rag action — usage: pan-agent rag [stats|purge]")
	}
	action := args[0]
	rest := args[1:]
	switch action {
	case "stats":
		return cmdRAGStats(rest)
	case "purge":
		return cmdRAGPurge(rest)
	default:
		return fmt.Errorf("unknown rag action %q — usage: pan-agent rag [stats|purge]",
			action)
	}
}

// cmdRAGStats reports per-session embedding counts. Useful for a
// power user to see "what's indexed?" without firing up the desktop.
func cmdRAGStats(args []string) error {
	fs := flag.NewFlagSet("rag stats", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	db, err := storage.Open(paths.StateDB())
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	sessions, models, total, err := ragStatsQuery(db)
	if err != nil {
		return fmt.Errorf("rag stats: %w", err)
	}

	fmt.Printf("rag_embeddings: %d row(s) total\n", total)
	if total == 0 {
		fmt.Println("(no embeddings yet — start the gateway with PAN_AGENT_RAG_EMBEDDER_URL set + chat for a bit)")
		return nil
	}

	if len(models) > 0 {
		fmt.Println()
		fmt.Println("By model:")
		// Sort by count descending for readability.
		type modelCount struct {
			Name  string
			Count int
		}
		mc := make([]modelCount, 0, len(models))
		for k, v := range models {
			mc = append(mc, modelCount{k, v})
		}
		sort.Slice(mc, func(i, j int) bool { return mc[i].Count > mc[j].Count })
		for _, m := range mc {
			fmt.Printf("  %-30s  %d\n", m.Name, m.Count)
		}
	}

	if len(sessions) > 0 {
		fmt.Println()
		fmt.Println("By session (top 20):")
		type sessCount struct {
			ID    string
			Count int
		}
		sc := make([]sessCount, 0, len(sessions))
		for k, v := range sessions {
			sc = append(sc, sessCount{k, v})
		}
		sort.Slice(sc, func(i, j int) bool { return sc[i].Count > sc[j].Count })
		if len(sc) > 20 {
			sc = sc[:20]
		}
		for _, s := range sc {
			label := s.ID
			if label == "" {
				label = "(no session)"
			}
			fmt.Printf("  %-40s  %d\n", label, s.Count)
		}
	}

	return nil
}

// cmdRAGPurge drops every embedding tied to a session id.
func cmdRAGPurge(args []string) error {
	fs := flag.NewFlagSet("rag purge", flag.ContinueOnError)
	session := fs.String("session", "", "session id to purge (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *session == "" {
		return fmt.Errorf("--session required")
	}

	db, err := storage.Open(paths.StateDB())
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer db.Close()

	n, err := db.DeleteEmbeddingsBySession(*session)
	if err != nil {
		return fmt.Errorf("rag purge: %w", err)
	}
	fmt.Printf("Deleted %d embedding row(s) for session %s\n", n, *session)
	return nil
}

// ragStatsQuery aggregates the rag_embeddings table into per-
// session + per-model counts. Pulled out as a helper so tests can
// run it directly against an in-memory storage.DB without spawning
// the CLI process.
func ragStatsQuery(db *storage.DB) (sessions, models map[string]int, total int, err error) {
	rows, qerr := db.RawDB().Query(
		`SELECT IFNULL(session_id,''), model FROM rag_embeddings`)
	if qerr != nil {
		return nil, nil, 0, qerr
	}
	defer rows.Close()
	sessions = map[string]int{}
	models = map[string]int{}
	for rows.Next() {
		var sid, model string
		if scanErr := rows.Scan(&sid, &model); scanErr != nil {
			return nil, nil, 0, scanErr
		}
		sessions[sid]++
		models[model]++
		total++
	}
	return sessions, models, total, rows.Err()
}
