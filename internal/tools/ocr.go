package tools

import (
	"bytes"
	"context"
	"encoding/base64"
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
	Register(&ocrTool{})
}

type ocrTool struct{}

func (o *ocrTool) Name() string { return "ocr" }

func (o *ocrTool) Description() string {
	return "Take a screenshot of the specified display and extract visible text via a multimodal LLM. " +
		"Returns the text found on screen."
}

func (o *ocrTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"prompt": {
				"type": "string",
				"description": "Instruction for the LLM. Defaults to extracting all visible text."
			},
			"display": {
				"type": "integer",
				"description": "Display index to capture (0 = primary). Defaults to 0.",
				"default": 0
			}
		}
	}`)
}

type ocrParams struct {
	Prompt  string `json:"prompt"`
	Display int    `json:"display"`
}

func (o *ocrTool) Execute(ctx context.Context, params json.RawMessage) (*Result, error) {
	var p ocrParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: "invalid parameters: " + err.Error()}, nil
	}
	if strings.TrimSpace(p.Prompt) == "" {
		p.Prompt = "Extract all visible text from this screenshot. Return the text exactly as shown."
	}

	// Capture the display.
	pngBytes, err := captureDisplay(p.Display)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)

	// Resolve LLM config (mirrors vision.go).
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

	// Build multimodal chat request using a base64 data URI (no remote URL needed).
	reqBody := map[string]any{
		"model": model,
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{
						"type": "image_url",
						"image_url": map[string]string{
							"url": dataURI,
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
