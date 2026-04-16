package gateway

import (
	"bytes"
	_ "embed"
	"io"
	"net/http"
)

// openapiSpec is the hand-maintained OpenAPI YAML served at
// GET /v1/openapi.yaml. Embedded at compile time so the desktop frontend
// can fetch it without depending on the on-disk layout of the dev
// checkout. Kept deliberately partial — see the description block inside
// for scope — and a contract-check script (scripts/verify-api.sh) guards
// against drift as new routes are added.
//
//go:embed embedded_openapi.yaml
var openapiSpec []byte

func (s *Server) handleOpenAPISpec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	// io.Copy from an in-memory bytes.Reader — avoids the direct-Write
	// pattern the lint flags and keeps the response body contract symmetric
	// with sendSSE in chat.go.
	_, _ = io.Copy(w, bytes.NewReader(openapiSpec))
}
