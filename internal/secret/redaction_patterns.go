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
)

// builtinPatterns is the ordered classifier table. More specific patterns
// must come before broader ones — e.g. CreditCard (16 digits, specific BIN
// prefixes) before Phone (10 digits, generic), and AWSKeyID before APIKey.
var builtinPatterns = []classifier{
	// PII — specificity descending.
	{category: CatEmail, re: emailRe, negative: docsEmailRe, minLen: 6, maxLen: 254},
	{category: CatCreditCard, re: ccRe, negative: testCCRe, minLen: 13, maxLen: 19},
	{category: CatSSN, re: ssnRe, negative: invalidSSNRe, minLen: 9, maxLen: 11},
	{category: CatPhone, re: phoneRe, minLen: 10, maxLen: 20},

	// Credentials — order matters: AWS before generic API_KEY.
	{category: CatAWSKeyID, re: awsKeyIDRe, minLen: 20, maxLen: 20},
	{category: CatJWT, re: jwtRe, minLen: 20, maxLen: 4096},
	{category: CatBearer, re: bearerRe, minLen: 20, maxLen: 4096},
	{category: CatAPIKey, re: apiKeyRe, minLen: 20, maxLen: 512},
}
