package gateway

import (
	"context"
	"fmt"
	"log"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
	"github.com/slack-go/slack/slackevents"
)

// startSlack starts the Slack bot using Socket Mode (no public URL needed).
// It reads SLACK_BOT_TOKEN and SLACK_APP_TOKEN from the profile env.
// Socket Mode requires an app-level token (xapp-...) in addition to the bot token (xoxb-...).
func (s *Server) startSlack(botToken, appToken string) (context.CancelFunc, error) {
	if appToken == "" {
		return nil, fmt.Errorf("slack: SLACK_APP_TOKEN is required for Socket Mode")
	}

	api := slack.New(botToken,
		slack.OptionAppLevelToken(appToken),
	)

	client := socketmode.New(api)

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		for evt := range client.Events {
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				client.Ack(*evt.Request)

				switch eventsAPIEvent.Type {
				case slackevents.CallbackEvent:
					innerEvent := eventsAPIEvent.InnerEvent
					switch ev := innerEvent.Data.(type) {
					case *slackevents.MessageEvent:
						// Ignore bot messages.
						if ev.BotID != "" || ev.SubType != "" {
							continue
						}

						text := ev.Text
						if text == "" {
							continue
						}

						// Use channel as session key.
						sessionID := fmt.Sprintf("sl-%s", ev.Channel)

						response, err := s.runAgentLoop(ctx, sessionID, text)
						if err != nil {
							log.Printf("[slack] agent error: %v", err)
							_, _, _ = api.PostMessage(ev.Channel,
								slack.MsgOptionText(fmt.Sprintf("Error: %v", err), false))
							continue
						}

						if response == "" {
							response = "(no response)"
						}

						// Slack has a 40000 character limit but practically ~4000 is readable.
						for _, chunk := range splitMessage(response, 4000) {
							_, _, _ = api.PostMessage(ev.Channel,
								slack.MsgOptionText(chunk, false))
						}
					}
				}
			}
		}
	}()

	go func() {
		if err := client.RunContext(ctx); err != nil {
			log.Printf("[slack] socket mode error: %v", err)
		}
	}()

	return func() {
		cancel()
	}, nil
}
