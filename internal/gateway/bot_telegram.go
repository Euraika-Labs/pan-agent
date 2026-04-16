package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// startTelegram starts the Telegram bot using long polling.
//
// Implementation note: we speak the Telegram Bot HTTP API directly with
// net/http rather than import a bot SDK. The three operations we use —
// getUpdates, sendMessage, and the JSON-envelope error path — are small
// enough that a dedicated SDK cost ~5 MB of transitive deps (bytedance/
// sonic, cloudwego/base64x, valyala/fasthttp, golang-asm) without
// carrying its weight. M10.
func (s *Server) startTelegram(token string, allowedUsers string) (context.CancelFunc, error) {
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("telegram: empty token")
	}
	client := &telegramClient{
		token:   token,
		apiBase: "https://api.telegram.org/bot" + token,
		http:    &http.Client{Timeout: 60 * time.Second},
	}
	// getMe is a cheap sanity check — mirrors what telego.NewBot did.
	if _, err := client.call(context.Background(), "getMe", nil); err != nil {
		return nil, fmt.Errorf("telegram: create bot: %w", err)
	}

	allowed := parseAllowedUsers(allowedUsers)
	ctx, cancel := context.WithCancel(context.Background())

	updates := make(chan tgUpdate, 64)
	go client.pollLoop(ctx, updates)

	go func() {
		for update := range updates {
			if update.Message == nil || update.Message.Text == "" {
				continue
			}
			msg := update.Message

			if len(allowed) > 0 && (msg.From == nil || !allowed[msg.From.ID]) {
				_ = client.sendMessage(ctx, msg.Chat.ID,
					"Access denied. Your user ID is not in the allowed list.")
				continue
			}

			// Per-chat session so conversations stay coherent across turns.
			sessionID := fmt.Sprintf("tg-%d", msg.Chat.ID)

			response, err := s.runAgentLoop(ctx, sessionID, msg.Text)
			if err != nil {
				log.Printf("[telegram] agent error: %v", err)
				_ = client.sendMessage(ctx, msg.Chat.ID, fmt.Sprintf("Error: %v", err))
				continue
			}
			if response == "" {
				response = "(no response)"
			}

			// Telegram caps each message at 4096 chars.
			for _, chunk := range splitMessage(response, 4096) {
				_ = client.sendMessage(ctx, msg.Chat.ID, chunk)
			}
		}
	}()

	return cancel, nil
}

// ---------------------------------------------------------------------------
// Minimal Telegram Bot API client — no third-party deps beyond stdlib.
// ---------------------------------------------------------------------------

type telegramClient struct {
	token   string
	apiBase string
	http    *http.Client
	offset  int64 // last-seen update_id + 1
}

type tgUser struct {
	ID int64 `json:"id"`
}

type tgChat struct {
	ID int64 `json:"id"`
}

type tgMessage struct {
	MessageID int64   `json:"message_id"`
	From      *tgUser `json:"from"`
	Chat      tgChat  `json:"chat"`
	Text      string  `json:"text"`
}

type tgUpdate struct {
	UpdateID int64      `json:"update_id"`
	Message  *tgMessage `json:"message,omitempty"`
}

// tgResponse is the standard Bot-API envelope: {ok, result, description}.
type tgResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description,omitempty"`
	Result      json.RawMessage `json:"result,omitempty"`
}

// call POSTs to method with form-encoded params and decodes the common
// envelope. Returns the raw Result bytes on ok=true.
func (c *telegramClient) call(ctx context.Context, method string, params url.Values) (json.RawMessage, error) {
	body := ""
	if params != nil {
		body = params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.apiBase+"/"+method, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16 MB cap
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	var env tgResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode: %w (body: %.200s)", err, raw)
	}
	if !env.OK {
		return nil, fmt.Errorf("telegram: %s", env.Description)
	}
	return env.Result, nil
}

// pollLoop runs long-poll getUpdates until ctx is cancelled.
func (c *telegramClient) pollLoop(ctx context.Context, out chan<- tgUpdate) {
	defer close(out)
	for {
		if ctx.Err() != nil {
			return
		}
		params := url.Values{}
		params.Set("timeout", "30") // long poll
		if c.offset > 0 {
			params.Set("offset", strconv.FormatInt(c.offset, 10))
		}
		result, err := c.call(ctx, "getUpdates", params)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			log.Printf("[telegram] getUpdates: %v", err)
			// Back off on transient errors so we don't hammer the API.
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		var updates []tgUpdate
		if err := json.Unmarshal(result, &updates); err != nil {
			log.Printf("[telegram] decode updates: %v", err)
			continue
		}
		for _, u := range updates {
			if u.UpdateID >= c.offset {
				c.offset = u.UpdateID + 1
			}
			select {
			case out <- u:
			case <-ctx.Done():
				return
			}
		}
	}
}

// sendMessage POSTs a plain-text message to chatID.
func (c *telegramClient) sendMessage(ctx context.Context, chatID int64, text string) error {
	params := url.Values{}
	params.Set("chat_id", strconv.FormatInt(chatID, 10))
	params.Set("text", text)
	_, err := c.call(ctx, "sendMessage", params)
	return err
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
