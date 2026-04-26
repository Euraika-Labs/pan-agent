// Package rag is the Phase 13 WS#13.B retrieval-augmented-generation
// substrate. The schema lives in internal/storage (rag_embeddings +
// rag_state); this package owns the small primitives that sit on top —
// vector BLOB encoding, content-hash gating, the embedder interface,
// and (in follow-up slices) the index wrapper, watcher, and search.
//
// Design choices documented at the package level so future slices
// don't have to re-derive them:
//
//  1. Vectors are stored as packed little-endian float32 (4 bytes per
//     dim). SQLite BLOBs are byte-addressable; the pack/unpack pair
//     is symmetrical and round-trips byte-for-byte. Choosing
//     little-endian matches the dominant CPU architecture for
//     desktop deployment (x86_64 + aarch64 macOS) so the BLOB can
//     be mmap'd into a tensor library without a byte-swap pass when
//     we eventually layer sqlite-vec on top.
//
//  2. Content hashes are sha256 hex of the un-trimmed input string.
//     Identical text → identical hash → idx_rag_hash hits and we
//     skip a round-trip to the embedder. The hash is NOT a security
//     primitive (no HMAC); it's a dedup key.
//
//  3. The embedder is an interface (Embedder.Embed) so tests can
//     inject a fake without standing up an HTTP server, and
//     providers other than the OpenAI-compatible /v1/embeddings
//     shape can be added behind the same interface.
package rag

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
)

// ErrDimMismatch reports that a vector's length didn't match the
// caller-asserted dimensionality. Returned by UnpackVector when the
// BLOB length is not exactly 4*dim, and by validation paths that
// compare against the rag_embeddings.dim column.
var ErrDimMismatch = errors.New("rag: vector dimension mismatch")

// ErrEmptyVector reports that PackVector was handed a zero-length
// slice. Empty vectors aren't legal in rag_embeddings (the schema
// requires NOT NULL on dim and vector); this surfaces the misuse at
// the encode boundary instead of the SQLite write site.
var ErrEmptyVector = errors.New("rag: empty vector")

// PackVector encodes a float32 slice into little-endian bytes
// suitable for the rag_embeddings.vector BLOB column. Length is
// exactly 4*len(v). NaN and ±Inf are preserved bit-for-bit (we use
// math.Float32bits, not a comparison-based encoder).
func PackVector(v []float32) ([]byte, error) {
	if len(v) == 0 {
		return nil, ErrEmptyVector
	}
	out := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(f))
	}
	return out, nil
}

// UnpackVector decodes a 4*dim byte BLOB back into a float32 slice.
// Returns ErrDimMismatch when len(b) != 4*dim — callers always know
// the expected dim from the rag_embeddings row, so a length mismatch
// indicates corruption (or a model-swap that didn't cascade).
func UnpackVector(b []byte, dim int) ([]float32, error) {
	if dim <= 0 {
		return nil, fmt.Errorf("%w: dim must be positive, got %d", ErrDimMismatch, dim)
	}
	if len(b) != 4*dim {
		return nil, fmt.Errorf("%w: blob len = %d, want %d", ErrDimMismatch, len(b), 4*dim)
	}
	out := make([]float32, dim)
	for i := 0; i < dim; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out, nil
}

// ContentHash returns the sha256 hex digest of text, used as the
// dedup key in rag_embeddings.content_hash. The hash is computed
// over the raw bytes — callers that want pre-trim normalisation
// should normalise before calling this.
func ContentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// CosineSimilarity returns the cosine similarity of a and b. Returns
// 0 when either vector is all-zero (the alternative — NaN from a 0/0
// — propagates badly through ranking code). Returns ErrDimMismatch
// when the lengths differ.
//
// Used by the next-slice search path; landed here alongside the codec
// so the package's vector primitives form a coherent unit.
func CosineSimilarity(a, b []float32) (float32, error) {
	if len(a) != len(b) {
		return 0, fmt.Errorf("%w: a=%d b=%d", ErrDimMismatch, len(a), len(b))
	}
	if len(a) == 0 {
		return 0, ErrEmptyVector
	}
	var dot, na, nb float64
	for i := range a {
		x, y := float64(a[i]), float64(b[i])
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0, nil
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb))), nil
}
