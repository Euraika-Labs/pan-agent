package secret

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
)

// Category names the family of detected secret.
type Category string

const (
	CatEmail      Category = "EMAIL"
	CatPhone      Category = "PHONE"
	CatSSN        Category = "SSN"
	CatCreditCard Category = "CC"
	CatAPIKey     Category = "API_KEY"
	CatJWT        Category = "JWT"
	CatAWSKeyID   Category = "AWS_KEY_ID"
	CatBearer     Category = "BEARER_TOKEN"
)

// ErrRedactionUnavailable is returned by Ready() when the redaction subsystem
// failed to initialise (keyring unavailable, key generation error, etc.).
var ErrRedactionUnavailable = errors.New("secret: redaction subsystem unavailable")

// classifier pairs a regex with optional negative lookahead and span bounds.
type classifier struct {
	category Category
	re       *regexp.Regexp
	// negative, if non-nil, cancels a match when it also matches the candidate.
	// Mirrors approval/patterns.go NegativeRegex posture.
	negative       *regexp.Regexp
	minLen, maxLen int
}

type redactor struct {
	mu         sync.RWMutex
	key        []byte
	keyOnce    sync.Once
	keyInitErr error
	patterns   []classifier
}

// global is the process-wide redactor instance.
var global = &redactor{
	patterns: builtinPatterns,
}

// SetKey forces the HMAC key. Exported so tests can inject a deterministic key
// without touching the OS keyring. Production code never calls this directly.
func SetKey(key []byte) {
	global.mu.Lock()
	defer global.mu.Unlock()
	k := make([]byte, len(key))
	copy(k, key)
	global.key = k
	// Reset keyOnce so subsequent initKey calls observe the new key.
	global.keyOnce = sync.Once{}
	global.keyInitErr = nil
}

// Ready reports whether the redaction subsystem has a valid HMAC key.
// Returns ErrRedactionUnavailable if key initialisation failed.
func Ready() error {
	global.initKey()
	global.mu.RLock()
	defer global.mu.RUnlock()
	if global.keyInitErr != nil {
		return fmt.Errorf("%w: %v", ErrRedactionUnavailable, global.keyInitErr)
	}
	return nil
}

// Redact returns text with every detected secret replaced by a
// category-tagged deterministic token: "<REDACTED:EMAIL:a1b2c3>".
// Same input → same output across calls (cross-receipt correlation).
func Redact(text string) string {
	out, _ := redactInternal(text, false)
	return out
}

// RedactWithMap returns the redacted text plus a map from token → original
// plaintext. The map is returned to the caller only; it is never persisted.
func RedactWithMap(text string) (string, map[string]string) {
	return redactInternal(text, true)
}

// RedactBytes is the []byte convenience wrapper for large JSON blobs.
func RedactBytes(b []byte) []byte {
	return []byte(Redact(string(b)))
}

// redactInternal implements both Redact and RedactWithMap.
//
// A span that matches a classifier's negative regex is globally protected
// — substituted with a non-matching placeholder before any classifier runs,
// restored after all have run. This prevents a broader classifier (e.g. Phone
// matching a 10-digit subsequence of a 16-digit CC) from redacting a span
// that a more specific classifier has already flagged as known-safe.
func redactInternal(text string, buildMap bool) (string, map[string]string) {
	global.initKey()
	global.mu.RLock()
	key := global.key
	initErr := global.keyInitErr
	global.mu.RUnlock()

	if initErr != nil {
		return "<REDACTED:ERR>", nil
	}

	var revealMap map[string]string
	if buildMap {
		revealMap = make(map[string]string)
	}

	// Pass 1: protect spans that hit a negative regex. Placeholders use
	// NUL bytes which no classifier regex can match, so they pass through
	// classification untouched.
	type protected struct{ placeholder, original string }
	var guards []protected
	working := text
	for _, c := range global.patterns {
		if c.negative == nil {
			continue
		}
		working = c.re.ReplaceAllStringFunc(working, func(match string) string {
			span := match
			if subs := c.re.FindStringSubmatch(match); len(subs) > 1 {
				span = subs[1]
			}
			if !c.negative.MatchString(span) {
				return match
			}
			ph := fmt.Sprintf("\x00PROT%d\x00", len(guards))
			guards = append(guards, protected{placeholder: ph, original: match})
			return ph
		})
	}

	// Pass 2: run classifier loop on the placeholdered text. The negative
	// check remains as a defensive no-op — spans that would have matched
	// are already placeholder-substituted and no longer look like anything.
	result := working
	for _, c := range global.patterns {
		result = c.re.ReplaceAllStringFunc(result, func(match string) string {
			span := match
			subs := c.re.FindStringSubmatch(match)
			if len(subs) > 1 {
				span = subs[1]
			}

			l := len(span)
			if l < c.minLen || (c.maxLen > 0 && l > c.maxLen) {
				return match
			}
			if c.negative != nil && c.negative.MatchString(span) {
				return match
			}

			token := makeToken(key, c.category, span)
			if buildMap {
				revealMap[token] = span
			}

			if len(subs) > 1 {
				subIdx := c.re.FindStringSubmatchIndex(match)
				if len(subIdx) >= 4 {
					prefix := match[:subIdx[2]]
					suffix := match[subIdx[3]:]
					return prefix + token + suffix
				}
			}
			return token
		})
	}

	// Pass 3: restore protected spans in their original form.
	for _, g := range guards {
		result = strings.Replace(result, g.placeholder, g.original, 1)
	}
	return result, revealMap
}

// makeToken returns "<REDACTED:CATEGORY:xxxxxx>" where xxxxxx is the first 6
// hex chars of HMAC-SHA256(key, category+":"+plaintext).
func makeToken(key []byte, cat Category, plaintext string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(string(cat)))
	h.Write([]byte(":"))
	h.Write([]byte(plaintext))
	sum := h.Sum(nil)
	return "<REDACTED:" + string(cat) + ":" + hex.EncodeToString(sum)[:6] + ">"
}

// makeTokenForTest is a package-internal helper so tests can compute expected
// tokens without reimplementing makeToken.
func makeTokenForTest(key []byte, cat Category, plaintext string) string {
	return makeToken(key, cat, plaintext)
}

// redactorKey returns the current HMAC key bytes for test assertions.
func redactorKey() []byte {
	global.mu.RLock()
	defer global.mu.RUnlock()
	k := make([]byte, len(global.key))
	copy(k, global.key)
	return k
}

// initKey loads the HMAC key from the OS keyring on first call. If absent,
// a new 32-byte random key is minted and persisted. Thread-safe via sync.Once.
func (r *redactor) initKey() {
	r.keyOnce.Do(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		// SetKey may have already supplied a key (tests).
		if len(r.key) > 0 {
			return
		}
		const keyName = "redaction-hmac-key"
		val, err := Get(keyName)
		if err == nil {
			r.key = []byte(val)
			return
		}
		if !IsNotFound(err) && !errors.Is(err, ErrKeyringUnavailable) &&
			!errors.Is(err, ErrUnsupportedPlatform) {
			r.keyInitErr = err
			log.Printf("secret: redaction key init failed: %v", err)
			return
		}
		// Key absent or keyring temporarily down — mint a fresh in-memory key.
		buf := make([]byte, 32)
		if _, err2 := rand.Read(buf); err2 != nil {
			r.keyInitErr = err2
			log.Printf("secret: redaction key generation failed: %v", err2)
			return
		}
		encoded := hex.EncodeToString(buf)
		if setErr := Set(keyName, encoded); setErr != nil {
			// Log once; Ready() will surface ErrRedactionUnavailable to health checks.
			// We still have a usable in-memory key for this process lifetime.
			log.Printf("secret: redaction key persistence failed (using ephemeral key): %v", setErr)
		}
		r.key = []byte(encoded)
		// keyInitErr stays nil — we have a working key even if persistence failed.
	})
}
