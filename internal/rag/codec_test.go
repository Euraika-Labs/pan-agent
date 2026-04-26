package rag

import (
	"bytes"
	"errors"
	"math"
	"testing"
)

// Phase 13 WS#13.B — codec primitives. Covers the BLOB pack/unpack
// round-trip, content-hash determinism, and cosine similarity edge
// cases. The next slice (storage helpers) builds on these guarantees.

func TestPackVector_RoundTrip(t *testing.T) {
	t.Parallel()
	in := []float32{0.0, 1.0, -1.0, 0.5, -0.5, 1e-6, 1e6, math.MaxFloat32, math.SmallestNonzeroFloat32}
	b, err := PackVector(in)
	if err != nil {
		t.Fatalf("PackVector: %v", err)
	}
	if len(b) != 4*len(in) {
		t.Errorf("len = %d, want %d", len(b), 4*len(in))
	}
	out, err := UnpackVector(b, len(in))
	if err != nil {
		t.Fatalf("UnpackVector: %v", err)
	}
	if len(out) != len(in) {
		t.Fatalf("decoded len = %d, want %d", len(out), len(in))
	}
	for i := range in {
		if math.Float32bits(out[i]) != math.Float32bits(in[i]) {
			t.Errorf("[%d] = %v, want %v (bits %x vs %x)",
				i, out[i], in[i],
				math.Float32bits(out[i]), math.Float32bits(in[i]))
		}
	}
}

func TestPackVector_PreservesSpecialValues(t *testing.T) {
	t.Parallel()
	// Construct -0.0 via bit-pattern — Go folds the literal -0.0 to
	// +0.0 at parse time, so we have to materialise the sign bit
	// explicitly to test it round-trips through PackVector.
	negZero := math.Float32frombits(1 << 31)
	specials := []float32{
		float32(math.NaN()),
		float32(math.Inf(1)),
		float32(math.Inf(-1)),
		0.0,
		negZero,
	}
	b, err := PackVector(specials)
	if err != nil {
		t.Fatalf("PackVector: %v", err)
	}
	out, err := UnpackVector(b, len(specials))
	if err != nil {
		t.Fatalf("UnpackVector: %v", err)
	}
	// NaN: compare bit pattern (Float32 NaN comparisons are always false).
	if math.Float32bits(out[0]) != math.Float32bits(specials[0]) {
		t.Errorf("NaN bits not preserved")
	}
	// Inf comparisons are well-defined.
	if !math.IsInf(float64(out[1]), 1) {
		t.Errorf("+Inf not preserved: %v", out[1])
	}
	if !math.IsInf(float64(out[2]), -1) {
		t.Errorf("-Inf not preserved: %v", out[2])
	}
	// Signed zero: compare bits (==0 ignores sign).
	if math.Float32bits(out[4]) != math.Float32bits(specials[4]) {
		t.Errorf("-0.0 sign bit lost")
	}
}

func TestPackVector_LittleEndian(t *testing.T) {
	t.Parallel()
	// 1.0 in IEEE 754 binary32 = 0x3F800000 = LE bytes 00 00 80 3F.
	b, err := PackVector([]float32{1.0})
	if err != nil {
		t.Fatalf("PackVector: %v", err)
	}
	want := []byte{0x00, 0x00, 0x80, 0x3F}
	if !bytes.Equal(b, want) {
		t.Errorf("LE encoding of 1.0 = %x, want %x", b, want)
	}
}

func TestPackVector_EmptyError(t *testing.T) {
	t.Parallel()
	_, err := PackVector(nil)
	if !errors.Is(err, ErrEmptyVector) {
		t.Errorf("err = %v, want ErrEmptyVector", err)
	}
	_, err = PackVector([]float32{})
	if !errors.Is(err, ErrEmptyVector) {
		t.Errorf("err = %v, want ErrEmptyVector", err)
	}
}

func TestUnpackVector_DimMismatch(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		blob []byte
		dim  int
	}{
		{"short_blob", []byte{0, 0, 0}, 1},
		{"long_blob", []byte{0, 0, 0, 0, 0}, 1},
		{"zero_dim", []byte{0, 0, 0, 0}, 0},
		{"negative_dim", []byte{0, 0, 0, 0}, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := UnpackVector(tc.blob, tc.dim)
			if !errors.Is(err, ErrDimMismatch) {
				t.Errorf("err = %v, want ErrDimMismatch", err)
			}
		})
	}
}

func TestContentHash_Deterministic(t *testing.T) {
	t.Parallel()
	a := ContentHash("hello world")
	b := ContentHash("hello world")
	if a != b {
		t.Errorf("hash drift: %q != %q", a, b)
	}
	if len(a) != 64 {
		t.Errorf("len = %d, want 64 (sha256 hex)", len(a))
	}
	// Known sha256 of "hello world" — pinning the exact hash so we
	// detect accidental normalisation drift.
	const want = "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if a != want {
		t.Errorf("hash = %q, want %q", a, want)
	}
}

func TestContentHash_Distinguishes(t *testing.T) {
	t.Parallel()
	if ContentHash("a") == ContentHash("b") {
		t.Error("distinct inputs collided")
	}
	// Whitespace matters — we hash raw bytes.
	if ContentHash("hello") == ContentHash(" hello") {
		t.Error("leading whitespace ignored — hash should be raw")
	}
}

func TestContentHash_Empty(t *testing.T) {
	t.Parallel()
	// sha256("") is well-known; lock it in so the empty case is documented.
	const want = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	if got := ContentHash(""); got != want {
		t.Errorf("empty hash = %q, want %q", got, want)
	}
}

func TestCosineSimilarity_Cases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		a, b []float32
		want float32
		eps  float32
	}{
		{"identical", []float32{1, 0, 0}, []float32{1, 0, 0}, 1.0, 1e-6},
		{"orthogonal", []float32{1, 0}, []float32{0, 1}, 0.0, 1e-6},
		{"opposite", []float32{1, 0}, []float32{-1, 0}, -1.0, 1e-6},
		{"scaled", []float32{2, 0}, []float32{5, 0}, 1.0, 1e-6},
		{"45deg", []float32{1, 0}, []float32{1, 1}, 0.7071, 1e-3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CosineSimilarity(tc.a, tc.b)
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			diff := got - tc.want
			if diff < 0 {
				diff = -diff
			}
			if diff > tc.eps {
				t.Errorf("got %v, want %v ± %v", got, tc.want, tc.eps)
			}
		})
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	t.Parallel()
	// Either side zero must return 0, not NaN — ranking code can't
	// recover from NaN gracefully.
	got, err := CosineSimilarity([]float32{0, 0, 0}, []float32{1, 1, 1})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != 0 {
		t.Errorf("zero vector → %v, want 0", got)
	}
	got, err = CosineSimilarity([]float32{1, 1}, []float32{0, 0})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != 0 {
		t.Errorf("zero vector (b) → %v, want 0", got)
	}
}

func TestCosineSimilarity_Errors(t *testing.T) {
	t.Parallel()
	_, err := CosineSimilarity([]float32{1, 0}, []float32{1, 0, 0})
	if !errors.Is(err, ErrDimMismatch) {
		t.Errorf("dim mismatch err = %v, want ErrDimMismatch", err)
	}
	_, err = CosineSimilarity([]float32{}, []float32{})
	if !errors.Is(err, ErrEmptyVector) {
		t.Errorf("empty err = %v, want ErrEmptyVector", err)
	}
}
