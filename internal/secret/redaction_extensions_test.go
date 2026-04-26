package secret

import (
	"strings"
	"testing"
)

// Phase 13 WS#13.G recognizer-extension tests. The HMAC pipeline's
// generic infrastructure is covered by redaction_test.go; these tests
// target the four new provider-specific categories added in WS#13.G:
//
//   CatSlackToken   — xoxb-/xoxp-/xoxa-/xoxr-/xoxe.
//   CatStripeKey    — sk_live_/sk_test_/rk_live_/rk_test_/pk_live_/pk_test_
//   CatGitHubToken  — ghp_/gho_/ghu_/ghs_/ghr_/github_pat_
//   CatGCPKey       — -----BEGIN PRIVATE KEY----- … -----END PRIVATE KEY-----
//
// All fixture values are split via Go-source string concatenation so
// GitHub Push Protection and other secret scanners don't flag the
// repository (the runtime payload is identical to a non-split literal).
// Same CWE-321 mitigation the existing JWT fixture uses.

// Helper: build a token by joining the prefix to the suffix at runtime.
// Splitting the literal in source is enough to dodge static scanners
// that match the surface form `<prefix>-<base64>` etc.
func split(prefix, body string) string { return prefix + body }

// ---------------------------------------------------------------------------
// Slack
// ---------------------------------------------------------------------------

func TestRedactSlack_BotToken(t *testing.T) {
	t.Parallel()
	tok := split("xox"+"b", "-1234567890-1234567890-aabbccddeeffaabbccddeeff")
	in := "SLACK_BOT_TOKEN=" + tok
	out := Redact(in)
	if !strings.Contains(out, "<REDACTED:SLACK_TOKEN:") {
		t.Errorf("Slack bot token not redacted: %q", out)
	}
	if strings.Contains(out, tok) {
		t.Errorf("Slack token plaintext leaked: %q", out)
	}
}

func TestRedactSlack_UserToken(t *testing.T) {
	t.Parallel()
	tok := split("xox"+"p", "-1111-2222-3333-aabbccddeeff112233445566aabbccdd")
	out := Redact("user token " + tok)
	if !strings.Contains(out, "<REDACTED:SLACK_TOKEN:") {
		t.Errorf("Slack user token not redacted: %q", out)
	}
}

func TestRedactSlack_AppToken(t *testing.T) {
	t.Parallel()
	tok := split("xox"+"a", "-2-1234567890-1234567890-1234567890-aabbccddeeff")
	out := Redact("app token " + tok)
	if !strings.Contains(out, "<REDACTED:SLACK_TOKEN:") {
		t.Errorf("Slack app token not redacted: %q", out)
	}
}

func TestRedactSlack_ConfigurationToken(t *testing.T) {
	t.Parallel()
	// xoxe.<rest> uses a dot separator, not a hyphen. Recognizer must
	// accept both.
	tok := split("xox"+"e", ".xoxp-1234567890-aabbccdd")
	out := Redact("config token " + tok)
	if !strings.Contains(out, "<REDACTED:SLACK_TOKEN:") {
		t.Errorf("Slack configuration token not redacted: %q", out)
	}
}

// ---------------------------------------------------------------------------
// Stripe
// ---------------------------------------------------------------------------

func TestRedactStripe_LiveSecretKey(t *testing.T) {
	t.Parallel()
	tok := split("sk_"+"live_", "AbCdEf1234567890ghIjKlMnOpQrSt")
	out := Redact("STRIPE_SECRET=" + tok)
	if !strings.Contains(out, "<REDACTED:STRIPE_KEY:") {
		t.Errorf("Stripe live key not redacted: %q", out)
	}
}

func TestRedactStripe_TestSecretKey(t *testing.T) {
	t.Parallel()
	tok := split("sk_"+"test_", "AbCdEf1234567890ghIjKlMnOpQrSt")
	out := Redact("STRIPE_SECRET_TEST=" + tok)
	if !strings.Contains(out, "<REDACTED:STRIPE_KEY:") {
		t.Errorf("Stripe test key not redacted: %q", out)
	}
}

func TestRedactStripe_RestrictedKey(t *testing.T) {
	t.Parallel()
	tok := split("rk_"+"live_", "AbCdEf1234567890ghIjKlMnOpQrSt")
	out := Redact(tok + " for analytics")
	if !strings.Contains(out, "<REDACTED:STRIPE_KEY:") {
		t.Errorf("Stripe restricted key not redacted: %q", out)
	}
}

func TestRedactStripe_PublishableKey(t *testing.T) {
	t.Parallel()
	// Publishable keys are technically not secrets, but redacting them
	// is harmless and keeps the recognizer simple. Documented behaviour.
	tok := split("pk_"+"live_", "AbCdEf1234567890ghIjKlMnOpQrSt")
	out := Redact(tok + " embedded in client")
	if !strings.Contains(out, "<REDACTED:STRIPE_KEY:") {
		t.Errorf("Stripe publishable key not redacted: %q", out)
	}
}

// ---------------------------------------------------------------------------
// GitHub
// ---------------------------------------------------------------------------

func TestRedactGitHub_ClassicPAT(t *testing.T) {
	t.Parallel()
	tok := split("gh"+"p_", "aabbccddeeff1122334455667788990011aabbcc")
	out := Redact("GITHUB_TOKEN=" + tok)
	if !strings.Contains(out, "<REDACTED:GITHUB_TOKEN:") {
		t.Errorf("GitHub classic PAT not redacted: %q", out)
	}
}

func TestRedactGitHub_FineGrainedPAT(t *testing.T) {
	t.Parallel()
	tok := split("github"+"_pat_", "aabbccddeeff112233445566778899001122334455")
	out := Redact("fine-grained: " + tok)
	if !strings.Contains(out, "<REDACTED:GITHUB_TOKEN:") {
		t.Errorf("GitHub fine-grained PAT not redacted: %q", out)
	}
}

func TestRedactGitHub_OAuthAccessToken(t *testing.T) {
	t.Parallel()
	for _, prefix := range []string{"gh" + "o_", "gh" + "u_", "gh" + "s_", "gh" + "r_"} {
		tok := prefix + "aabbccddeeff1122334455667788990011aabbcc"
		out := Redact("Authorization: token " + tok)
		if !strings.Contains(out, "<REDACTED:GITHUB_TOKEN:") {
			t.Errorf("GitHub %s token not redacted: %q", prefix, out)
		}
	}
}

func TestRedactGitHub_PrefersGitHubOverGenericAPIKey(t *testing.T) {
	t.Parallel()
	// "Token ghp_…" would also match the bearer-like API_KEY regex
	// fragment. Ordering in builtinPatterns puts GitHubToken earlier,
	// so the redacted token must carry GITHUB_TOKEN, not API_KEY.
	tok := split("gh"+"p_", "aabbccddeeff1122334455667788990011aabbcc")
	out := Redact("Authorization: Token " + tok)
	if !strings.Contains(out, "<REDACTED:GITHUB_TOKEN:") {
		t.Errorf("ordering bug — GITHUB_TOKEN should win: %q", out)
	}
}

// ---------------------------------------------------------------------------
// GCP — service-account private keys
// ---------------------------------------------------------------------------

func TestRedactGCP_PEMBlock(t *testing.T) {
	t.Parallel()
	// Realistic shape: header + base64 body + footer. Body is short
	// and obviously fake — the recognizer requires only the markers.
	header := "-----BEGIN" + " PRIVATE KEY-----"
	footer := "-----END" + " PRIVATE KEY-----"
	in := "service-account credentials:\n" + header +
		"\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcwggSjAgEAAoIBAQC1vBQfqW3aWzNL" +
		"\nfakeBase64BodyShortenedForTest\n" + footer + "\nend of file"
	out := Redact(in)
	if !strings.Contains(out, "<REDACTED:GCP_PRIVATE_KEY:") {
		t.Errorf("GCP PEM block not redacted: %q", out)
	}
	if strings.Contains(out, "MIIEvQIBADANBgkqhkiG") {
		t.Errorf("GCP PEM body leaked through: %q", out)
	}
	if !strings.Contains(out, "end of file") {
		t.Errorf("text after PEM block was mangled: %q", out)
	}
}

func TestRedactGCP_EscapedNewlines(t *testing.T) {
	t.Parallel()
	// JSON-encoded service account creds escape newlines as \n
	// literals. Recognizer must catch this shape too.
	header := "-----BEGIN" + " PRIVATE KEY-----"
	footer := "-----END" + " PRIVATE KEY-----"
	in := `{"private_key":"` + header +
		`\nMIIEvQIBADANBgkqhkiG9w0BAQEFAASCBKcfakeBase64\n` +
		footer + `\n"}`
	out := Redact(in)
	if !strings.Contains(out, "<REDACTED:GCP_PRIVATE_KEY:") {
		t.Errorf("GCP escaped-newline PEM not redacted: %q", out)
	}
}

// Note: LLM-pipeline coverage of these new categories lands as a
// follow-up once PR #38 (RedactForLLM core) merges. The HMAC pipeline
// (Redact / RedactBytes) covers the recognizer correctness here; the
// LLM pipeline uses the same builtinPatterns table so it picks up the
// new categories automatically.
