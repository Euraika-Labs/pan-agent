package saaslinks

import (
	"encoding/base64"
	"strings"
	"testing"
)

// Gmail
// ---------------------------------------------------------------------------

func TestGmail_HappyPath(t *testing.T) {
	url, ok := Gmail(0, "1869abc123def4567")
	if !ok {
		t.Fatal("Gmail returned ok=false on a valid threadID")
	}
	want := "https://mail.google.com/mail/u/0/#inbox/1869abc123def4567"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestGmail_MultiAccount(t *testing.T) {
	url, ok := Gmail(2, "abc")
	if !ok || !strings.Contains(url, "/u/2/") {
		t.Errorf("multi-account index not honoured: %q (ok=%v)", url, ok)
	}
}

func TestGmail_NegativeIndexClampsToZero(t *testing.T) {
	url, _ := Gmail(-1, "abc")
	if !strings.Contains(url, "/u/0/") {
		t.Errorf("negative accountIndex should clamp to 0, got %q", url)
	}
}

func TestGmail_RejectsInjection(t *testing.T) {
	for _, hostile := range []string{
		"foo/bar",         // path separator
		"foo bar",         // whitespace
		"foo?evil=1",      // query
		"foo#frag",        // fragment
		`"><script>alert`, // HTML
		"",                // empty
	} {
		if url, ok := Gmail(0, hostile); ok {
			t.Errorf("Gmail accepted hostile threadID %q → %q", hostile, url)
		}
	}
}

// Stripe
// ---------------------------------------------------------------------------

func TestStripe_LiveAndTestPaths(t *testing.T) {
	live, ok := Stripe(StripeLive, "ch_3OabcdEfgHIJklm")
	if !ok || live != "https://dashboard.stripe.com/payments/ch_3OabcdEfgHIJklm" {
		t.Errorf("live url = %q (ok=%v)", live, ok)
	}
	test, ok := Stripe(StripeTest, "pi_test_12345")
	if !ok || test != "https://dashboard.stripe.com/test/payments/pi_test_12345" {
		t.Errorf("test url = %q (ok=%v)", test, ok)
	}
}

func TestStripe_RejectsUnknownMode(t *testing.T) {
	if _, ok := Stripe(StripeMode("preview"), "ch_xyz"); ok {
		t.Error("Stripe accepted unknown mode")
	}
}

func TestStripe_RejectsBadIDs(t *testing.T) {
	for _, hostile := range []string{
		"ch/evil",
		"ch evil",
		"",
		"ch?x=1",
	} {
		if _, ok := Stripe(StripeLive, hostile); ok {
			t.Errorf("Stripe accepted hostile chargeID %q", hostile)
		}
	}
}

func TestStripeWithAccount(t *testing.T) {
	url, ok := StripeWithAccount(StripeLive, "acct_1Mxyz", "ch_3Oabc")
	if !ok || url != "https://dashboard.stripe.com/acct_1Mxyz/payments/ch_3Oabc" {
		t.Errorf("StripeWithAccount live = %q (ok=%v)", url, ok)
	}
	test, ok := StripeWithAccount(StripeTest, "acct_1Mxyz", "ch_3Oabc")
	if !ok || test != "https://dashboard.stripe.com/acct_1Mxyz/test/payments/ch_3Oabc" {
		t.Errorf("StripeWithAccount test = %q (ok=%v)", test, ok)
	}
}

func TestStripeWithAccount_RejectsBadAccount(t *testing.T) {
	if _, ok := StripeWithAccount(StripeLive, "acct/evil", "ch_x"); ok {
		t.Error("StripeWithAccount accepted hostile acctID")
	}
}

// Google Calendar
// ---------------------------------------------------------------------------

func TestGCal_HappyPath_Primary(t *testing.T) {
	url, ok := GCal("evt_abc", "")
	if !ok {
		t.Fatal("GCal returned ok=false")
	}
	if !strings.HasPrefix(url, "https://calendar.google.com/calendar/u/0/r/eventedit/") {
		t.Errorf("unexpected URL prefix: %q", url)
	}
	// The eid is base64url("evt_abc primary"); decoding round-trips.
	suffix := strings.TrimPrefix(url, "https://calendar.google.com/calendar/u/0/r/eventedit/")
	decoded, err := base64.RawURLEncoding.DecodeString(suffix)
	if err != nil {
		t.Fatalf("eid is not valid base64url: %v", err)
	}
	if string(decoded) != "evt_abc primary" {
		t.Errorf("decoded eid = %q, want %q", string(decoded), "evt_abc primary")
	}
}

func TestGCal_ServiceAccountCalendar(t *testing.T) {
	url, ok := GCal("evt_abc", "team@group.calendar.google.com")
	if !ok {
		t.Fatal("GCal rejected a service-account calendar ID")
	}
	suffix := strings.TrimPrefix(url, "https://calendar.google.com/calendar/u/0/r/eventedit/")
	decoded, err := base64.RawURLEncoding.DecodeString(suffix)
	if err != nil {
		t.Fatalf("eid is not valid base64url: %v", err)
	}
	if string(decoded) != "evt_abc team@group.calendar.google.com" {
		t.Errorf("decoded eid = %q", string(decoded))
	}
}

func TestGCal_RejectsBadEventOrCalendar(t *testing.T) {
	if _, ok := GCal("evt/x", ""); ok {
		t.Error("GCal accepted hostile eventID")
	}
	if _, ok := GCal("evt", "calendar with spaces"); ok {
		t.Error("GCal accepted hostile calendarID")
	}
	if _, ok := GCal("", ""); ok {
		t.Error("GCal accepted empty eventID")
	}
}

// base64URLNoPad — the tiny inline encoder backing GCal eids.
// ---------------------------------------------------------------------------

func TestBase64URLNoPad_MatchesStdlib(t *testing.T) {
	cases := [][]byte{
		nil,
		[]byte(""),
		[]byte("a"),
		[]byte("ab"),
		[]byte("abc"),
		[]byte("abcd"),
		[]byte("evt_abc primary"),
		[]byte{0xff, 0xfe, 0xfd, 0xfc, 0xfb},
		[]byte("Hello, world! 🌍"), // mixed-byte unicode
	}
	for _, c := range cases {
		got := base64URLNoPad(c)
		want := base64.RawURLEncoding.EncodeToString(c)
		if got != want {
			t.Errorf("base64URLNoPad(%q) = %q, want %q", c, got, want)
		}
	}
}
