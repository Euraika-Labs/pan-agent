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

// Slack — Phase 13 WS#13.F
// ---------------------------------------------------------------------------

func TestSlack_ChannelOnly(t *testing.T) {
	url, ok := Slack("euraika", "C01ABC23DEF", "")
	if !ok {
		t.Fatal("Slack returned ok=false on a valid channel")
	}
	want := "https://euraika.slack.com/archives/C01ABC23DEF"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestSlack_WithThread(t *testing.T) {
	url, ok := Slack("euraika", "C01ABC23DEF", "1672531200.123456")
	if !ok {
		t.Fatal("Slack returned ok=false on a valid thread ts")
	}
	// Slack's canonical form: dot stripped, "p" prefix.
	want := "https://euraika.slack.com/archives/C01ABC23DEF/p1672531200123456"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestSlack_RejectsHostileWorkspace(t *testing.T) {
	for _, w := range []string{
		"euraika.evil", // dot smuggles a subdomain
		"euraika/evil", // path smuggle
		"EURAIKA",      // uppercase not allowed
		"",             // empty
		"euraika evil", // whitespace
	} {
		if _, ok := Slack(w, "C123", ""); ok {
			t.Errorf("Slack accepted hostile workspace %q", w)
		}
	}
}

func TestSlack_RejectsHostileChannelOrThread(t *testing.T) {
	if _, ok := Slack("euraika", "C123/evil", ""); ok {
		t.Error("Slack accepted hostile channelID")
	}
	if _, ok := Slack("euraika", "C123", "1672531200"); ok {
		t.Error("Slack accepted thread ts without microseconds (no dot)")
	}
	if _, ok := Slack("euraika", "C123", "1672531200.abcdef"); ok {
		t.Error("Slack accepted non-numeric thread ts")
	}
	if _, ok := Slack("euraika", "C123", "1672531200.123.456"); ok {
		t.Error("Slack accepted multi-dot thread ts")
	}
}

// Notion — Phase 13 WS#13.F
// ---------------------------------------------------------------------------

func TestNotion_BareHexID(t *testing.T) {
	url, ok := Notion("a1b2c3d4e5f60718293a4b5c6d7e8f90")
	if !ok {
		t.Fatal("Notion returned ok=false on a valid 32-hex ID")
	}
	want := "https://www.notion.so/a1b2c3d4e5f60718293a4b5c6d7e8f90"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestNotion_DashedUUID(t *testing.T) {
	url, ok := Notion("a1b2c3d4-e5f6-0718-293a-4b5c6d7e8f90")
	if !ok {
		t.Fatal("Notion returned ok=false on a valid dashed UUID")
	}
	// Dashed and bare forms must map to the same canonical URL.
	want := "https://www.notion.so/a1b2c3d4e5f60718293a4b5c6d7e8f90"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestNotion_RejectsBadID(t *testing.T) {
	for _, id := range []string{
		"abc123",                               // too short
		"a1b2c3d4e5f60718293a4b5c6d7e8f9z",     // non-hex
		"a1b2c3d4-e5f6-0718-293a-4b5c6d7e8f9",  // wrong dashed length
		"a1b2c3d4_e5f6_0718_293a_4b5c6d7e8f90", // underscores not dashes
		"",                                     // empty
		"a1b2c3d4-e5f6/evil",                   // path smuggle
	} {
		if url, ok := Notion(id); ok {
			t.Errorf("Notion accepted bad id %q → %q", id, url)
		}
	}
}

// Jira — Phase 13 WS#13.F
// ---------------------------------------------------------------------------

func TestJira_AtlassianCloud(t *testing.T) {
	url, ok := Jira("acme.atlassian.net", "PAN-123")
	if !ok {
		t.Fatal("Jira returned ok=false on cloud host + valid issue")
	}
	want := "https://acme.atlassian.net/browse/PAN-123"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestJira_SelfHosted(t *testing.T) {
	url, ok := Jira("jira.internal.example.com", "ABC123-456")
	if !ok {
		t.Fatal("Jira returned ok=false on self-hosted FQDN")
	}
	want := "https://jira.internal.example.com/browse/ABC123-456"
	if url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestJira_RejectsHostileHost(t *testing.T) {
	for _, h := range []string{
		"acme.atlassian.net/evil", // path smuggle
		"acme.atlassian.net?x=1",  // query smuggle
		"-acme.atlassian.net",     // leading dash invalid by jiraHostRegex
		"acme.atlassian.net-",     // trailing dash invalid
		"",                        // empty
		"acme atlassian net",      // whitespace
	} {
		if _, ok := Jira(h, "PAN-1"); ok {
			t.Errorf("Jira accepted hostile host %q", h)
		}
	}
}

func TestJira_RejectsHostileIssueKey(t *testing.T) {
	for _, k := range []string{
		"pan-123",      // lowercase project key
		"PAN_123",      // underscore not dash
		"PAN-",         // missing number
		"-123",         // missing project
		"PAN-12.3",     // dot in number
		"PAN-123/evil", // path smuggle
		"123-456",      // numeric project (regex requires letter prefix)
		"",             // empty
	} {
		if _, ok := Jira("acme.atlassian.net", k); ok {
			t.Errorf("Jira accepted hostile issue key %q", k)
		}
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
		{0xff, 0xfe, 0xfd, 0xfc, 0xfb},
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
