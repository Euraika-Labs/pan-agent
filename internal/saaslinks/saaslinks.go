// Package saaslinks builds public URLs that take the user from a
// pan-agent action receipt straight into the third-party SaaS UI where
// they can manually undo (or audit) the action.
//
// All builders are pure functions of (provider-shaped IDs) → URL string.
// Validating IDs against [A-Za-z0-9_-]+ before interpolating means callers
// can pass arbitrary user-controlled strings without risking URL or HTML
// injection downstream.
//
// Phase 12 / WS#2 audit-lane scope per docs/design/phase12.md:
//
//	v0.6.0 covers Gmail + Stripe + Google Calendar; broader providers
//	(Slack, Notion, Jira, ...) are explicitly deferred.
//
// Note: the actual Gmail / Stripe / Calendar tools that produce
// KindSaaSAPI receipts arrive in Phase 13. This library lands first so
// the URL contract is settled before the tool authors need to consume
// it (see SaaSAPIReverser in internal/recovery).
package saaslinks

import "regexp"

// safeID accepts the alphanumeric / dash / underscore set every
// SaaS in scope uses for its public IDs. Anything else is rejected so
// adversarial input can't terminate the URL or smuggle a fragment.
var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// StripeMode discriminates the live and test dashboards. Stripe never
// auto-detects mode from the charge ID (test charges use ch_test_…
// prefixes that the dashboard rewrites to /test/payments/ anyway, but
// the dashboard root for organisation-scoped accounts depends on it),
// so callers always pass it explicitly.
type StripeMode string

const (
	StripeLive StripeMode = "live"
	StripeTest StripeMode = "test"
)

// Gmail returns the canonical thread URL in a Gmail account.
//
//	https://mail.google.com/mail/u/<accountIndex>/#inbox/<threadID>
//
// accountIndex is the multi-account selector ("/u/0/" for the first
// signed-in account, "/u/1/" for the second, etc.). Callers thread
// the index through from whichever OAuth profile the tool authenticated
// against; pass 0 if unknown.
//
// Returns ("", false) when threadID fails the safeID validation.
func Gmail(accountIndex int, threadID string) (string, bool) {
	if !safeID.MatchString(threadID) {
		return "", false
	}
	if accountIndex < 0 {
		accountIndex = 0
	}
	return "https://mail.google.com/mail/u/" + itoa(accountIndex) +
		"/#inbox/" + threadID, true
}

// Stripe returns a payment-detail URL on the Stripe dashboard. Charge
// IDs (ch_…) auto-redirect to their PaymentIntent on the dashboard, so
// passing either ch_… or pi_… IDs works.
//
//	live:  https://dashboard.stripe.com/payments/<id>
//	test:  https://dashboard.stripe.com/test/payments/<id>
//
// For Connect platforms acting on behalf of a connected account, use
// StripeWithAccount instead.
func Stripe(mode StripeMode, chargeID string) (string, bool) {
	if !safeID.MatchString(chargeID) {
		return "", false
	}
	switch mode {
	case StripeLive:
		return "https://dashboard.stripe.com/payments/" + chargeID, true
	case StripeTest:
		return "https://dashboard.stripe.com/test/payments/" + chargeID, true
	default:
		return "", false
	}
}

// StripeWithAccount returns a payment-detail URL scoped to a specific
// Stripe Connect account. Used when pan-agent's tool acted on behalf of
// a connected account rather than the platform's own.
//
//	https://dashboard.stripe.com/<acctID>/payments/<id>
//	https://dashboard.stripe.com/<acctID>/test/payments/<id>
func StripeWithAccount(mode StripeMode, acctID, chargeID string) (string, bool) {
	if !safeID.MatchString(acctID) || !safeID.MatchString(chargeID) {
		return "", false
	}
	base := "https://dashboard.stripe.com/" + acctID
	switch mode {
	case StripeLive:
		return base + "/payments/" + chargeID, true
	case StripeTest:
		return base + "/test/payments/" + chargeID, true
	default:
		return "", false
	}
}

// GCal returns the canonical edit-event URL for a Google Calendar event.
// The eid query parameter is base64url(no-padding) of "<eventID> <calendarID>"
// — Google's own UI builds the same encoding.
//
//	https://calendar.google.com/calendar/u/0/r/eventedit/<eid>
//
// calendarID defaults to "primary" when empty (the most common case).
// Returns ("", false) when eventID fails validation; calendarID is
// allowed to contain '@' and '.' (typical for service-account calendars
// like "team-xyz@group.calendar.google.com") so it has its own laxer
// regex.
func GCal(eventID, calendarID string) (string, bool) {
	if !safeID.MatchString(eventID) {
		return "", false
	}
	if calendarID == "" {
		calendarID = "primary"
	}
	if !gcalCalendarRegex.MatchString(calendarID) {
		return "", false
	}
	eid := encodeGCalEID(eventID, calendarID)
	return "https://calendar.google.com/calendar/u/0/r/eventedit/" + eid, true
}

// gcalCalendarRegex accepts the {primary | email-style ID | service-
// account ID} surface Google uses for calendar identifiers.
var gcalCalendarRegex = regexp.MustCompile(`^[A-Za-z0-9._@-]+$`)

// encodeGCalEID produces the base64url-no-padding encoding of
// "<eventID> <calendarID>" that Google Calendar's URL scheme expects.
// Exposed for tests; otherwise considered an implementation detail.
func encodeGCalEID(eventID, calendarID string) string {
	raw := eventID + " " + calendarID
	return base64URLNoPad([]byte(raw))
}

// base64URLNoPad and itoa are tiny helpers kept inline rather than
// pulled from encoding/base64 + strconv just to keep this package's
// stdlib import surface aligned with its tiny scope. Both are
// well-trodden algorithms; the saaslinks_test.go fixtures verify the
// encoding against known-good Google URLs.
func base64URLNoPad(b []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_"
	var out []byte
	for i := 0; i < len(b); i += 3 {
		// Pull 1-3 bytes into a 24-bit accumulator, big-endian.
		n := 0
		j := 0
		for ; j < 3 && i+j < len(b); j++ {
			n = (n << 8) | int(b[i+j])
		}
		// Left-pad to 24 bits if we got 1 or 2 bytes only.
		n <<= (3 - j) * 8
		// Emit 2-4 base64 chars per group; 4 for full 3-byte groups,
		// 3 for 2 bytes, 2 for 1 byte.
		out = append(out, alphabet[(n>>18)&0x3F])
		out = append(out, alphabet[(n>>12)&0x3F])
		if j > 1 {
			out = append(out, alphabet[(n>>6)&0x3F])
		}
		if j > 2 {
			out = append(out, alphabet[n&0x3F])
		}
	}
	return string(out)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
