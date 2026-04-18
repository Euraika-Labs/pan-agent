package secret

import (
	"errors"
	"fmt"
	"testing"
)

// ---------------------------------------------------------------------------
// validateKey table
// ---------------------------------------------------------------------------

func TestValidateKey(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		key     string
		wantErr bool
	}{
		// valid keys — must not be rejected
		{name: "simple", key: "browser-profile-key", wantErr: false},
		{name: "alphanumeric", key: "key123", wantErr: false},
		{name: "dots-and-underscores", key: "pan.agent_key", wantErr: false},
		{name: "max-length-128", key: "a" + repeat('b', 127), wantErr: false},

		// invalid keys — must return ErrInvalidKey
		{name: "empty", key: "", wantErr: true},
		{name: "leading-dash", key: "-badkey", wantErr: true},
		{name: "over-128-chars", key: "a" + repeat('b', 128), wantErr: true},
		{name: "unicode-fullwidth-A", key: "Ａkey", wantErr: true},     // U+FF21 fullwidth A
		{name: "unicode-fullwidth-dot", key: "key．name", wantErr: true}, // U+FF0E fullwidth full stop
		{name: "path-traversal", key: "../foo", wantErr: true},
		{name: "path-traversal-deep", key: "a/../../etc/passwd", wantErr: true},
		{name: "shell-semicolon", key: "key;rm -rf /", wantErr: true},
		{name: "shell-backtick", key: "key`id`", wantErr: true},
		{name: "shell-dollar-paren", key: "key$(whoami)", wantErr: true},
		{name: "shell-dollar-brace", key: "key${PATH}", wantErr: true},
		{name: "shell-pipe", key: "key|cat", wantErr: true},
		{name: "shell-ampersand", key: "key&cmd", wantErr: true},
		{name: "space", key: "key name", wantErr: true},
		{name: "newline", key: "key\nname", wantErr: true},
		{name: "null-byte", key: "key\x00name", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := validateKey(tc.key)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("validateKey(%q): want error, got nil", tc.key)
				}
				if !errors.Is(err, ErrInvalidKey) {
					t.Fatalf("validateKey(%q): want ErrInvalidKey, got %v", tc.key, err)
				}
			} else {
				if err != nil {
					t.Fatalf("validateKey(%q): unexpected error: %v", tc.key, err)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// IsNotFound
// ---------------------------------------------------------------------------

func TestIsNotFound(t *testing.T) {
	t.Parallel()

	t.Run("direct-sentinel", func(t *testing.T) {
		if !IsNotFound(ErrNotFound) {
			t.Error("IsNotFound(ErrNotFound) = false, want true")
		}
	})

	t.Run("wrapped-one-level", func(t *testing.T) {
		wrapped := fmt.Errorf("outer: %w", ErrNotFound)
		if !IsNotFound(wrapped) {
			t.Errorf("IsNotFound(wrapped ErrNotFound) = false, want true")
		}
	})

	t.Run("wrapped-two-levels", func(t *testing.T) {
		inner := fmt.Errorf("inner: %w", ErrNotFound)
		outer := fmt.Errorf("outer: %w", inner)
		if !IsNotFound(outer) {
			t.Errorf("IsNotFound(double-wrapped ErrNotFound) = false, want true")
		}
	})

	t.Run("nil-returns-false", func(t *testing.T) {
		if IsNotFound(nil) {
			t.Error("IsNotFound(nil) = true, want false")
		}
	})

	t.Run("other-error-returns-false", func(t *testing.T) {
		if IsNotFound(ErrUnsupportedPlatform) {
			t.Error("IsNotFound(ErrUnsupportedPlatform) = true, want false")
		}
	})

	t.Run("plain-error-returns-false", func(t *testing.T) {
		if IsNotFound(errors.New("some other error")) {
			t.Error("IsNotFound(plain error) = true, want false")
		}
	})
}

// ---------------------------------------------------------------------------
// Smoke test using fake in-memory backend
//
// The coder implemented setBackend(b backend) where backend is the unexported
// interface satisfied by platformBackend (production) and fakeBackend (tests).
// Passing nil restores the platform default. The hook is package-internal
// (unexported) and compiled unconditionally because tests live in the same
// package (package secret).
// ---------------------------------------------------------------------------

// fakeBackend is a minimal in-memory backend for smoke tests.
// It satisfies the package-internal backend interface:
//
//	type backend interface {
//	    set(key, value string) error
//	    get(key string) (string, error)
//	    delete(key string) error
//	}
//
// Injected via setBackend(b backend); nil restores the platform default.
type fakeBackend struct {
	store map[string]string
}

func newFakeBackend() *fakeBackend {
	return &fakeBackend{store: make(map[string]string)}
}

func (f *fakeBackend) set(key, value string) error {
	f.store[key] = value
	return nil
}

func (f *fakeBackend) get(key string) (string, error) {
	v, ok := f.store[key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (f *fakeBackend) delete(key string) error {
	if _, ok := f.store[key]; !ok {
		return ErrNotFound
	}
	delete(f.store, key)
	return nil
}

func TestSmokeWithFakeBackend(t *testing.T) {
	fake := newFakeBackend()
	setBackend(fake)
	t.Cleanup(func() { setBackend(nil) }) // nil restores platform default

	const key = "smoke-test-key"
	const value = "smoke-value-123"

	t.Run("get-missing-returns-ErrNotFound", func(t *testing.T) {
		_, err := Get(key)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get missing key: want ErrNotFound, got %v", err)
		}
	})

	t.Run("set-then-get", func(t *testing.T) {
		if err := Set(key, value); err != nil {
			t.Fatalf("Set: %v", err)
		}
		got, err := Get(key)
		if err != nil {
			t.Fatalf("Get after Set: %v", err)
		}
		if got != value {
			t.Errorf("Get = %q, want %q", got, value)
		}
	})

	t.Run("overwrite-value", func(t *testing.T) {
		const value2 = "overwritten-value"
		if err := Set(key, value2); err != nil {
			t.Fatalf("Set overwrite: %v", err)
		}
		got, err := Get(key)
		if err != nil {
			t.Fatalf("Get after overwrite: %v", err)
		}
		if got != value2 {
			t.Errorf("Get = %q, want %q", got, value2)
		}
	})

	t.Run("delete-then-get-returns-ErrNotFound", func(t *testing.T) {
		// Ensure key exists first.
		_ = Set(key, value)
		if err := Delete(key); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		_, err := Get(key)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Get after Delete: want ErrNotFound, got %v", err)
		}
	})

	t.Run("delete-missing-returns-ErrNotFound", func(t *testing.T) {
		err := Delete("definitely-not-there")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("Delete missing: want ErrNotFound, got %v", err)
		}
	})

	t.Run("invalid-key-rejected-before-backend", func(t *testing.T) {
		err := Set("../traversal", "val")
		if !errors.Is(err, ErrInvalidKey) {
			t.Fatalf("Set invalid key: want ErrInvalidKey, got %v", err)
		}
		// Confirm the fake backend was never touched.
		if _, ok := fake.store["../traversal"]; ok {
			t.Error("invalid key should not reach backend store")
		}
	})

	t.Run("empty-value-allowed", func(t *testing.T) {
		if err := Set("empty-val-key", ""); err != nil {
			t.Fatalf("Set empty value: %v", err)
		}
		got, err := Get("empty-val-key")
		if err != nil {
			t.Fatalf("Get empty value: %v", err)
		}
		if got != "" {
			t.Errorf("Get empty value = %q, want empty string", got)
		}
	})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// repeat returns a string of n copies of c.
func repeat(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
