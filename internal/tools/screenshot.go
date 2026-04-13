package tools

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"os"

	"github.com/kbinani/screenshot"
)

func init() {
	Register(&screenshotTool{})
}

type screenshotTool struct{}

func (s *screenshotTool) Name() string { return "screenshot" }

func (s *screenshotTool) Description() string {
	return "Capture a screenshot of the specified display and return it as a base64 PNG data URI, " +
		"along with the path to the saved temp file."
}

func (s *screenshotTool) Parameters() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"display": {
				"type": "integer",
				"description": "Display index to capture (0 = primary). Defaults to 0.",
				"default": 0
			}
		}
	}`)
}

type screenshotParams struct {
	Display int `json:"display"`
}

// captureDisplay captures the given display index and returns the PNG bytes.
// It is shared by the screenshot and ocr tools.
func captureDisplay(displayIdx int) ([]byte, error) {
	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return nil, fmt.Errorf("no active displays found")
	}
	if displayIdx < 0 || displayIdx >= n {
		return nil, fmt.Errorf("display index %d out of range (0..%d)", displayIdx, n-1)
	}

	bounds := screenshot.GetDisplayBounds(displayIdx)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return nil, fmt.Errorf("capture failed: %w", err)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("png encode failed: %w", err)
	}
	return buf.Bytes(), nil
}

func (s *screenshotTool) Execute(_ context.Context, params json.RawMessage) (*Result, error) {
	var p screenshotParams
	if err := json.Unmarshal(params, &p); err != nil {
		return &Result{Error: "invalid parameters: " + err.Error()}, nil
	}

	pngBytes, err := captureDisplay(p.Display)
	if err != nil {
		return &Result{Error: err.Error()}, nil
	}

	// Save to temp file.
	tmp, err := os.CreateTemp("", "pan-screenshot-*.png")
	if err != nil {
		return &Result{Error: "failed to create temp file: " + err.Error()}, nil
	}
	if _, err := tmp.Write(pngBytes); err != nil {
		tmp.Close()
		return &Result{Error: "failed to write temp file: " + err.Error()}, nil
	}
	tmp.Close()

	dataURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes)

	output, _ := json.Marshal(map[string]string{
		"data_uri":  dataURI,
		"file_path": tmp.Name(),
	})
	return &Result{Output: string(output)}, nil
}
