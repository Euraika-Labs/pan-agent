package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func init() {
	Register(&ttsTool{})
}

type ttsTool struct{}

func (t *ttsTool) Name() string { return "tts" }

func (t *ttsTool) Description() string {
	return "Convert text to speech using the OpenAI /audio/speech endpoint. " +
		"Saves the audio to a temporary file and returns the file path."
}

func (t *ttsTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"text": {
				"type": "string",
				"description": "The text to convert to speech."
			},
			"voice": {
				"type": "string",
				"description": "Voice to use: alloy, echo, fable, onyx, nova, or shimmer.",
				"default": "alloy"
			}
		},
		"required": ["text"]
	}`)
}

type ttsParams struct {
	Text  string `json:"text"`
	Voice string `json:"voice"`
}

func (t *ttsTool) Execute(ctx context.Context, params json.RawMessage) (*Result, error) {
	var p ttsParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: "invalid parameters: " + err.Error()}, nil
	}
	if strings.TrimSpace(p.Text) == "" {
		return &Result{Error: "text is required"}, nil
	}
	if p.Voice == "" {
		p.Voice = "alloy"
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return &Result{Error: "OPENAI_API_KEY environment variable is not set — " +
			"set it to your OpenAI API key to use text-to-speech"}, nil
	}

	reqBody := map[string]any{
		"model": "tts-1",
		"input": p.Text,
		"voice": p.Voice,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return &Result{Error: "failed to build request: " + err.Error()}, nil
	}

	httpCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(httpCtx, http.MethodPost,
		"https://api.openai.com/v1/audio/speech", bytes.NewReader(body))
	if err != nil {
		return &Result{Error: "failed to create request: " + err.Error()}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return &Result{Error: "request failed: " + err.Error()}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &Result{Error: fmt.Sprintf("audio API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))}, nil
	}

	// Write audio bytes to a temp file.
	tmpFile, err := os.CreateTemp("", "pan-tts-*.mp3")
	if err != nil {
		return &Result{Error: "failed to create temp file: " + err.Error()}, nil
	}
	defer tmpFile.Close()

	if _, err := io.Copy(tmpFile, resp.Body); err != nil {
		os.Remove(tmpFile.Name())
		return &Result{Error: "failed to write audio file: " + err.Error()}, nil
	}

	return &Result{Output: tmpFile.Name()}, nil
}
