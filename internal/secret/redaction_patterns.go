package secret

import "regexp"

// Pattern sources are cited per Presidio recognizer so upstream audits are easy.
// Ordering is load-bearing: more specific patterns must appear before broader ones
// so that "AKIA…" lines are tagged AWS_KEY_ID rather than API_KEY.

var (
	// presidio-analyzer/.../email_recognizer.py
	emailRe = regexp.MustCompile(
		`(?i)[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)
	// Reject reserved/example addresses (RFC 2606).
	docsEmailRe = regexp.MustCompile(
		`(?i)@example\.(com|org|net)$|@(test|invalid|localhost)`)

	// presidio-analyzer/.../phone_recognizer.py (E.164 + NANP + international)
	phoneRe = regexp.MustCompile(
		`(?:\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}`)

	// presidio-analyzer/.../us_ssn_recognizer.py
	// Go's regexp is RE2-based and does not support lookaheads; the Presidio
	// reserved-prefix filtering (000/666/9xx area, 00 group, 0000 serial) is
	// expressed as a negative regex applied after the primary match.
	ssnRe = regexp.MustCompile(
		`\b\d{3}[- ]\d{2}[- ]\d{4}\b`)
	invalidSSNRe = regexp.MustCompile(
		`^(?:000|666|9\d{2})[- ]|[- ]00[- ]|[- ]0000$`)

	// presidio-analyzer/.../credit_card_recognizer.py
	// Matches major card patterns (Visa/MC/Amex/Discover). Luhn validation
	// is left to humans — regex alone gives enough signal for redaction.
	ccRe = regexp.MustCompile(
		`\b(?:4[0-9]{12}(?:[0-9]{3})?|5[1-5][0-9]{14}|3[47][0-9]{13}|6(?:011|5[0-9]{2})[0-9]{12}|(?:2131|1800|35\d{3})\d{11})\b`)
	// Luhn-valid test card numbers commonly used in docs/tests.
	testCCRe = regexp.MustCompile(
		`\b(?:4111111111111111|4012888888881881|4242424242424242|5555555555554444|5500005555555552|371449635398431|378282246310005|6011111111111117)\b`)

	// presidio-analyzer/.../aws_recognizer.py
	awsKeyIDRe = regexp.MustCompile(
		`(?:A3T[A-Z0-9]|AKIA|AGPA|AIDA|AROA|AIPA|ANPA|ANVA|ASIA)[A-Z0-9]{16}`)

	// presidio-analyzer/.../azure_recognizer.py (JWT — three Base64url segments)
	jwtRe = regexp.MustCompile(
		`eyJ[A-Za-z0-9_-]+\.eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)

	// Bearer token in Authorization header value (anything after "Bearer ").
	bearerRe = regexp.MustCompile(
		`(?i)bearer\s+([A-Za-z0-9\-._~+/]+=*)`)

	// presidio-analyzer/.../api_key_recognizer.py
	// Generic high-entropy API key patterns: key/token/secret/password =<value>.
	apiKeyRe = regexp.MustCompile(
		`(?i)(?:api[_-]?key|token|secret|password|passwd|pwd)\s*[:=]\s*["']?([A-Za-z0-9\-._~+/!@#$%^&*]{20,})["']?`)

	// Phase 13 WS#13.G — provider-specific recognizers added per the
	// security-engineer plan. Each is intentionally tighter than the
	// generic API_KEY catch-all so the redacted token surfaces the
	// right CATEGORY tag (helps both the audit-lane UI and a human
	// reviewer noticing "ah, a real Slack bot token leaked").

	// Slack tokens — bot/user/app/legacy. Format documented at
	// https://api.slack.com/authentication/token-types
	//   xoxb-<10+>-<10+>-<24+>           bot tokens
	//   xoxp-<10+>-<10+>-<10+>-<32+>    user tokens
	//   xoxa-<10+>-<10+>-<10+>-<32+>    workspace app tokens
	//   xoxr-<10+>-<10+>-<10+>-<32+>    refresh tokens
	//   xoxe.<rest>                      configuration tokens (newer)
	slackTokenRe = regexp.MustCompile(
		`xox[baprse][-.][A-Za-z0-9-]{10,}`)

	// Stripe API keys — restricted (rk_), secret (sk_), publishable
	// (pk_), test (sk_test_). Stripe's keys are 32-99 chars after the
	// prefix (varies by environment + restricted-key shape).
	// https://stripe.com/docs/keys
	stripeKeyRe = regexp.MustCompile(
		`(?:sk_live_|sk_test_|rk_live_|rk_test_|pk_live_|pk_test_)[A-Za-z0-9]{24,}`)

	// GitHub tokens — fine-grained personal access tokens (github_pat_),
	// classic personal access tokens (ghp_), OAuth access tokens (gho_),
	// user-to-server (ghu_), server-to-server (ghs_), refresh tokens (ghr_).
	// https://github.blog/2021-04-05-behind-githubs-new-authentication-token-formats/
	githubTokenRe = regexp.MustCompile(
		`(?:ghp_|gho_|ghu_|ghs_|ghr_|github_pat_)[A-Za-z0-9_]{20,}`)

	// GCP service-account private keys — appear as JSON values inside
	// service-account credential files. We match the literal PEM
	// header/footer that always wraps the private key body, plus
	// everything between, even when escaped (\n) for JSON. The body
	// itself is base64 + line breaks; the surrounding markers are the
	// stable signal.
	//
	// Match (?s) so . matches newlines (a real PEM block spans many
	// lines; escaped JSON form has \n literals so . suffices but we
	// keep (?s) for robustness across both shapes).
	gcpPrivateKeyRe = regexp.MustCompile(
		`(?s)-----BEGIN PRIVATE KEY-----[^-]+-----END PRIVATE KEY-----`)
)

// builtinPatterns is the ordered classifier table. Order is load-bearing
// because pass 2 of redactInternal runs classifiers sequentially —
// earlier classifiers' tokenisation shields their region from later
// classifier regexes. Two ordering rules:
//
//  1. Tokens with **unambiguous prefixes** (xoxb-, sk_live_, ghp_,
//     BEGIN PRIVATE KEY, AKIA…) come first. A Stripe key's digits
//     would otherwise be redacted as PHONE first, leaving an
//     `<REDACTED:PHONE:…>` token inside the key that breaks the
//     Stripe regex's character class.
//  2. Within each band (provider-specific → AWS → bearer/JWT/API_KEY
//     → PII) more specific shapes precede broader ones (CC before
//     Phone within PII; AWS_KEY_ID before generic API_KEY within
//     credentials).
var builtinPatterns = []classifier{
	// Provider-specific credential shapes — unambiguous prefixes,
	// must run first so digit runs inside them don't get tagged as
	// Phone/SSN/CC by the PII band below.
	{category: CatGCPKey, re: gcpPrivateKeyRe, minLen: 64, maxLen: 8192},
	{category: CatSlackToken, re: slackTokenRe, minLen: 14, maxLen: 256},
	{category: CatStripeKey, re: stripeKeyRe, minLen: 27, maxLen: 256},
	{category: CatGitHubToken, re: githubTokenRe, minLen: 20, maxLen: 256},
	{category: CatAWSKeyID, re: awsKeyIDRe, minLen: 20, maxLen: 20},

	// Generic credential shapes — JWT and Bearer have distinctive
	// shape signals (eyJ… for JWT, "Bearer " prefix for Bearer) that
	// don't overlap with PII; API_KEY is the catch-all and must come
	// last among credential matchers.
	{category: CatJWT, re: jwtRe, minLen: 20, maxLen: 4096},
	{category: CatBearer, re: bearerRe, minLen: 20, maxLen: 4096},
	{category: CatAPIKey, re: apiKeyRe, minLen: 20, maxLen: 512},

	// PII — specificity descending. Runs LAST among the regex bands
	// so digit-only PII patterns (Phone, SSN, CC) don't carve into
	// provider-specific credentials whose values may contain long
	// digit runs.
	{category: CatEmail, re: emailRe, negative: docsEmailRe, minLen: 6, maxLen: 254},
	{category: CatCreditCard, re: ccRe, negative: testCCRe, minLen: 13, maxLen: 19},
	{category: CatSSN, re: ssnRe, negative: invalidSSNRe, minLen: 9, maxLen: 11},
	{category: CatPhone, re: phoneRe, minLen: 10, maxLen: 20},
}
