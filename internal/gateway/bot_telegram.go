package gateway

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

// startTelegram starts the Telegram bot using long polling.
func (s *Server) startTelegram(token string, allowedUsers string) (context.CancelFunc, error) {
	bot, err := telego.NewBot(token)
	if err != nil {
		return nil, fmt.Errorf("telegram: create bot: %w", err)
	}

	allowed := parseAllowedUsers(allowedUsers)
	ctx, cancel := context.WithCancel(context.Background())

	updates, err := bot.UpdatesViaLongPolling(ctx, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("telegram: start polling: %w", err)
	}

	go func() {
		for update := range updates {
			if update.Message == nil || update.Message.Text == "" {
				continue
			}
			msg := update.Message

			// Check allowed users.
			if len(allowed) > 0 && (msg.From == nil || !allowed[msg.From.ID]) {
				_, _ = bot.SendMessage(ctx, tu.Message(
					tu.ID(msg.Chat.ID),
					"Access denied. Your user ID is not in the allowed list.",
				))
				continue
			}

			// Generate a session ID from the chat ID for conversation continuity.
			sessionID := fmt.Sprintf("tg-%d", msg.Chat.ID)

			response, err := s.runAgentLoop(ctx, sessionID, msg.Text)
			if err != nil {
				log.Printf("[telegram] agent error: %v", err)
				_, _ = bot.SendMessage(ctx, tu.Message(
					tu.ID(msg.Chat.ID),
					fmt.Sprintf("Error: %v", err),
				))
				continue
			}

			if response == "" {
				response = "(no response)"
			}

			// Telegram has a 4096 character limit per message.
			for _, chunk := range splitMessage(response, 4096) {
				_, _ = bot.SendMessage(ctx, tu.Message(
					tu.ID(msg.Chat.ID),
					chunk,
				))
			}
		}
	}()

	return func() {
		cancel()
	}, nil
}

// parseAllowedUsers parses a comma-separated list of Telegram user IDs.
func parseAllowedUsers(s string) map[int64]bool {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	result := make(map[int64]bool)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if id, err := strconv.ParseInt(part, 10, 64); err == nil {
			result[id] = true
		}
	}
	return result
}

// splitMessage splits a string into chunks of at most maxLen bytes.
func splitMessage(s string, maxLen int) []string {
	if len(s) <= maxLen {
		return []string{s}
	}
	var chunks []string
	for len(s) > maxLen {
		chunks = append(chunks, s[:maxLen])
		s = s[maxLen:]
	}
	if len(s) > 0 {
		chunks = append(chunks, s)
	}
	return chunks
}
