package mail

import (
	"testing"

	gomail "github.com/wneessen/go-mail"
)

// TestTLSPolicyFromMode pins the mapping config.Config.SMTPTLSMode's three
// accepted strings resolve to. This used to be hardcoded to NoTLS
// unconditionally (a production blocker: no hosted relay accepts that), so
// this test exists to catch a regression back to a single hardcoded policy.
func TestTLSPolicyFromMode(t *testing.T) {
	cases := []struct {
		mode string
		want gomail.TLSPolicy
	}{
		{"mandatory", gomail.TLSMandatory},
		{"opportunistic", gomail.TLSOpportunistic},
		{"none", gomail.NoTLS},
		// Anything else falls back to NoTLS rather than panicking --
		// config.Load already rejects any other value before it ever
		// reaches here, so this is a defensive default, not a documented
		// input.
		{"unexpected", gomail.NoTLS},
	}
	for _, tc := range cases {
		if got := tlsPolicyFromMode(tc.mode); got != tc.want {
			t.Errorf("tlsPolicyFromMode(%q) = %v, want %v", tc.mode, got, tc.want)
		}
	}
}

// TestNewSMTPMailerWiresEveryConfigValue pins that NewSMTPMailer's
// parameters land on the fields send() actually reads -- host/port parsed
// from addr, and username/password/tls carried through unchanged -- so a
// future refactor of the constructor can't silently drop one of them the way
// TLS policy and credentials were dropped (hardcoded) before this fix.
func TestNewSMTPMailerWiresEveryConfigValue(t *testing.T) {
	m := NewSMTPMailer("smtp.example.com:587", "Hearth <noreply@hearth.example>",
		"http://localhost:5173", "relay-user", "relay-pass", "mandatory")

	if m.host != "smtp.example.com" || m.port != 587 {
		t.Fatalf("host/port = %q/%d, want %q/%d", m.host, m.port, "smtp.example.com", 587)
	}
	if m.from != "Hearth <noreply@hearth.example>" {
		t.Fatalf("from = %q", m.from)
	}
	if m.username != "relay-user" || m.password != "relay-pass" {
		t.Fatalf("username/password = %q/%q, want %q/%q", m.username, m.password, "relay-user", "relay-pass")
	}
	if m.tls != gomail.TLSMandatory {
		t.Fatalf("tls = %v, want %v", m.tls, gomail.TLSMandatory)
	}
}
