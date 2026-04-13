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
	Register(&imageGenTool{})
}

type imageGenTool struct{}

func (g *imageGenTool) Name() string { return "image_gen" }

func (g *imageGenTool) Description() string {
	return "Generate an image from a text prompt using an OpenAI-compatible " +
		"/images/generations endpoint. Returns the URL of the generated image."
}

func (g *imageGenTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"prompt": {
				"type": "string",
				"description": "Text description of the image to generate."
			},
			"size": {
				"type": "string",
				"description": "Image dimensions, e.g. \"1024x1024\", \"512x512\".",
				"default": "1024x1024"
			}
		},
		"required": ["prompt"]
	}`)
}

type imageGenParams struct {
	Prompt string `json:"prompt"`
	Size   string `json:"size"`
}

func (g *imageGenTool) Execute(ctx context.Context, params json.RawMessage) (*Result, error) {
	var p imageGenParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: "invalid parameters: " + err.Error()}, nil
	}
	if strings.TrimSpace(p.Prompt) == "" {
		return &Result{Error: "prompt is required"}, nil
	}
	if p.Size == "" {
		p.Size = "1024x1024"
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return &Result{Error: "OPENAI_API_KEY environment variable is not set — " +
			"set it to your OpenAI API key to use image generation"}, nil
	}

	reqBody := map[string]any{
		"prompt": p.Prompt,
		"n":      1,
		"size":   p.Size,
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return &Result{Error: "failed to build request: " + err.Error()}, nil
	}

	httpCtx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(httpCtx, http.MethodPost,
		"https://api.openai.com/v1/images/generations", bytes.NewReader(body))
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

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return &Result{Error: "failed to read response: " + err.Error()}, nil
	}

	if resp.StatusCode != http.StatusOK {
		return &Result{Error: fmt.Sprintf("images API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))}, nil
	}

	var imgResp struct {
		Data []struct {
			URL string `json:"url"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &imgResp); err != nil {
		return &Result{Error: "failed to parse response: " + err.Error()}, nil
	}
	if imgResp.Error != nil {
		return &Result{Error: "images API error: " + imgResp.Error.Message}, nil
	}
	if len(imgResp.Data) == 0 || imgResp.Data[0].URL == "" {
		return &Result{Error: "images API returned no image URL"}, nil
	}

	return &Result{Output: imgResp.Data[0].URL}, nil
}
