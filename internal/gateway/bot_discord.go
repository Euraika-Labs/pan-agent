package gateway

import (
	"context"
	"fmt"
	"log"

	"github.com/bwmarrin/discordgo"
)

// startDiscord starts the Discord bot using WebSocket gateway.
// It reads DISCORD_BOT_TOKEN from the profile env.
func (s *Server) startDiscord(token string) (context.CancelFunc, error) {
	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("discord: create session: %w", err)
	}

	// Request message content intent (required since Sept 2022).
	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentMessageContent

	ctx, cancel := context.WithCancel(context.Background())

	dg.AddHandler(func(sess *discordgo.Session, m *discordgo.MessageCreate) {
		// Ignore messages from the bot itself.
		if m.Author.ID == sess.State.User.ID {
			return
		}

		text := m.Content
		if text == "" {
			return
		}

		// Use channel ID as session key for conversation continuity.
		sessionID := fmt.Sprintf("dc-%s", m.ChannelID)

		response, err := s.runAgentLoop(ctx, sessionID, text)
		if err != nil {
			log.Printf("[discord] agent error: %v", err)
			_, _ = sess.ChannelMessageSend(m.ChannelID, fmt.Sprintf("Error: %v", err))
			return
		}

		if response == "" {
			response = "(no response)"
		}

		// Discord has a 2000 character limit per message.
		for _, chunk := range splitMessage(response, 2000) {
			_, _ = sess.ChannelMessageSend(m.ChannelID, chunk)
		}
	})

	if err := dg.Open(); err != nil {
		cancel()
		return nil, fmt.Errorf("discord: open connection: %w", err)
	}

	return func() {
		_ = dg.Close()
		cancel()
	}, nil
}
