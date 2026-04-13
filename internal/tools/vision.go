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

	"github.com/euraika-labs/pan-agent/internal/config"
)

func init() {
	Register(&visionTool{})
}

type visionTool struct{}

func (v *visionTool) Name() string { return "vision" }

func (v *visionTool) Description() string {
	return "Analyze an image via multimodal LLM. Supply an image URL and a prompt; " +
		"the configured LLM will describe or answer questions about the image."
}

func (v *visionTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"image_url": {
				"type": "string",
				"description": "Public URL of the image to analyze."
			},
			"prompt": {
				"type": "string",
				"description": "What to ask or describe about the image."
			}
		},
		"required": ["image_url", "prompt"]
	}`)
}

type visionParams struct {
	ImageURL string `json:"image_url"`
	Prompt   string `json:"prompt"`
}

func (v *visionTool) Execute(ctx context.Context, params json.RawMessage) (*Result, error) {
	var p visionParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: "invalid parameters: " + err.Error()}, nil
	}
	if strings.TrimSpace(p.ImageURL) == "" {
		return &Result{Error: "image_url is required — provide a public URL of the image to analyze"}, nil
	}
	if strings.TrimSpace(p.Prompt) == "" {
		p.Prompt = "Describe this image."
	}

	// Resolve base URL and API key from the active model config, mirroring
	// the same lookup order used by the gateway server.
	mc := config.GetModelConfig("")
	baseURL := mc.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("OPENAI_BASE_URL")
	}
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	apiKey := ""
	if env, err := config.ReadProfileEnv(""); err == nil {
		apiKey = env["REGOLO_API_KEY"]
		if apiKey == "" {
			apiKey = env["OPENAI_API_KEY"]
		}
		if apiKey == "" {
			apiKey = env["API_KEY"]
		}
	}
	if apiKey == "" {
		apiKey = os.Getenv("OPENAI_API_KEY")
	}

	model := mc.Model
	if model == "" {
		model = "gpt-4o"
	}

	// Build a multimodal message with the image URL and text prompt.
	reqBody := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": p.ImageURL,
						},
					},
					{
						"type": "text",
						"text": p.Prompt,
					},
				},
			},
		},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return &Result{Error: "failed to build request: " + err.Error()}, nil
	}

	httpCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(httpCtx, http.MethodPost,
		baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return &Result{Error: "failed to create request: " + err.Error()}, nil
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return &Result{Error: "request failed: " + err.Error()}, nil
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return &Result{Error: "failed to read response: " + err.Error()}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return &Result{Error: fmt.Sprintf("LLM returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))}, nil
	}

	// Parse the OpenAI-compatible chat completions response.
	var chatResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &chatResp); err != nil {
		return &Result{Error: "failed to parse response: " + err.Error()}, nil
	}
	if chatResp.Error != nil {
		return &Result{Error: "LLM error: " + chatResp.Error.Message}, nil
	}
	if len(chatResp.Choices) == 0 {
		return &Result{Error: "LLM returned no choices"}, nil
	}

	return &Result{Output: chatResp.Choices[0].Message.Content}, nil
}
