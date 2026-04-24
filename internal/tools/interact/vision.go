package interact

import (
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"io"

	"github.com/kbinani/screenshot"
)

const maxScreenshotWidth = 1024

// Vision captures screenshots and encodes them for LLM consumption.
// Supports two backends: base64 image_url (current-gen) and provider
// computer_use passthrough (future).
type Vision struct{}

// NewVision creates a Vision backend.
func NewVision() *Vision {
	return &Vision{}
}

// Capture takes a screenshot of the primary display and returns it as a
// base64-encoded PNG, resized to at most maxScreenshotWidth pixels wide.
func (v *Vision) Capture(ctx context.Context) (string, error) {
	n := screenshot.NumActiveDisplays()
	if n == 0 {
		return "", fmt.Errorf("vision: no active displays")
	}

	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return "", fmt.Errorf("vision: capture: %w", err)
	}

	resized := resizeIfNeeded(img, maxScreenshotWidth)

	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(png.Encode(pw, resized))
	}()

	data, err := io.ReadAll(pr)
	if err != nil {
		return "", fmt.Errorf("vision: encode: %w", err)
	}

	return base64.StdEncoding.EncodeToString(data), nil
}

// resizeIfNeeded scales an image down to maxWidth if it exceeds it,
// preserving aspect ratio. Uses nearest-neighbor for speed.
func resizeIfNeeded(img *image.RGBA, maxWidth int) image.Image {
	b := img.Bounds()
	if b.Dx() <= maxWidth {
		return img
	}

	ratio := float64(maxWidth) / float64(b.Dx())
	newH := int(float64(b.Dy()) * ratio)

	dst := image.NewRGBA(image.Rect(0, 0, maxWidth, newH))
	for y := 0; y < newH; y++ {
		srcY := int(float64(y) / ratio)
		for x := 0; x < maxWidth; x++ {
			srcX := int(float64(x) / ratio)
			dst.Set(x, y, img.At(srcX, srcY))
		}
	}
	return dst
}
