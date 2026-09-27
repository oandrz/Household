package mail

import (
	"testing"

	gomail "github.com/wneessen/go-mail"
)

// TestTLSPolicyFromMode pins the mapping config.Config.SMTPTLSMode's three
// accepted strings resolve to. Don't hardcode this to NoTLS: no hosted relay
// accepts unencrypted SMTP.
func TestTLSPolicyFromMode(t *testing.T) {
	cases := []struct {
		mode string
		want gomail.TLSPolicy
	}{
		{"mandatory", gomail.TLSMandatory},
		{"opportunistic", gomail.TLSOpportunistic},
		{"none", gomail.NoTLS},
		// Anything else falls back to TLSMandatory, never NoTLS -- config.Load
		// already rejects other values, so this is purely defensive, and a
		// default that guesses must guess the side that refuses plain text.
		{"unexpected", gomail.TLSMandatory},
	}
	for _, tc := range cases {
		if got := tlsPolicyFromMode(tc.mode); got != tc.want {
			t.Errorf("tlsPolicyFromMode(%q) = %v, want %v", tc.mode, got, tc.want)
		}
	}
}

// TestNewSMTPMailerWiresEveryConfigValue pins that NewSMTPMailer's
// parameters land on the fields send() actually reads -- host/port from
// addr, username/password/tls carried through unchanged. Don't let a
// refactor silently drop one, as these were once hardcoded instead.
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
