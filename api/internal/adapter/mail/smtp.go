// Package mail sends the two plain-text emails Hearth's identity flows need:
// the magic-link sign-in link and the household invite. It is the adapter
// that owns the third-party mail dependency; usecase only ever sees the
// Mailer port.
package mail

import (
	"context"
	"fmt"
	"net"
	"strconv"

	gomail "github.com/wneessen/go-mail"
)

// SMTPMailer talks to a plain SMTP relay: no authentication, no TLS. In
// development addr points at Mailpit (SMTP_ADDR=mailpit:1025); in production
// it points at whatever relay the deployment trusts on its private network.
// Message bodies are plain text by design — there is no HTML template system
// to keep in sync with the design's copy.
type SMTPMailer struct {
	host    string
	port    int
	from    string
	baseURL string // currently unused: both callers already hand over a fully-built url.
}

// NewSMTPMailer builds a mailer from config values. It never reads the
// environment itself — config.Config already did that — so addr, from and
// baseURL are exactly config.SMTPAddr, config.SMTPFrom and config.AppBaseURL.
func NewSMTPMailer(addr, from, baseURL string) *SMTPMailer {
	host, port := splitAddr(addr)
	return &SMTPMailer{host: host, port: port, from: from, baseURL: baseURL}
}

// splitAddr defaults to port 25 if addr carries no port or an unparsable
// one, rather than failing construction — NewSMTPMailer cannot return an
// error, and a malformed SMTP_ADDR is caught by config.Load's own
// validation, not here.
func splitAddr(addr string) (host string, port int) {
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return addr, 25
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return h, 25
	}
	return h, n
}

// SendMagicLink emails the sign-in link. The subject and body are the
// design's voice, kept short: this is a transactional link, not marketing
// copy.
func (m *SMTPMailer) SendMagicLink(ctx context.Context, to, name, url string) error {
	body := fmt.Sprintf(
		"Hi %s,\n\n"+
			"Here's your sign-in link for Hearth:\n\n"+
			"%s\n\n"+
			"It expires in 15 minutes and works once. If you didn't ask for this, "+
			"you can ignore this email.\n",
		name, url,
	)
	return m.send(ctx, to, "Your Hearth sign-in link", body)
}

// SendInvite emails an invited member the link to join the household.
func (m *SMTPMailer) SendInvite(ctx context.Context, to, name, inviterName, url string) error {
	subject := fmt.Sprintf("%s invited you to Hearth", inviterName)
	body := fmt.Sprintf(
		"Hi %s,\n\n"+
			"%s has invited you to join their household on Hearth. Follow this "+
			"link to accept:\n\n"+
			"%s\n\n"+
			"If you weren't expecting this, you can ignore this email.\n",
		name, inviterName, url,
	)
	return m.send(ctx, to, subject, body)
}

func (m *SMTPMailer) send(ctx context.Context, to, subject, body string) error {
	msg := gomail.NewMsg()
	if err := msg.From(m.from); err != nil {
		return fmt.Errorf("set from address: %w", err)
	}
	if err := msg.To(to); err != nil {
		return fmt.Errorf("set to address: %w", err)
	}
	msg.Subject(subject)
	msg.SetBodyString(gomail.TypeTextPlain, body)

	client, err := gomail.NewClient(m.host, gomail.WithPort(m.port), gomail.WithTLSPolicy(gomail.NoTLS))
	if err != nil {
		return fmt.Errorf("create smtp client: %w", err)
	}
	if err := client.DialAndSendWithContext(ctx, msg); err != nil {
		return fmt.Errorf("send mail: %w", err)
	}
	return nil
}
