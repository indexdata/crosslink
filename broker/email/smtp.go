package email

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/smtp"
	"slices"
	"strings"
	"time"
)

// Mailer submits a message and returns nil once SMTP accepts it.
type Mailer interface {
	SendMail(addr string, a smtp.Auth, from string, to []string, msg []byte) error
}

// DefaultMailer submits mail with the standard library SMTP client.
type DefaultMailer struct{}

// SendMail submits one message. The final response to DATA determines acceptance.
func (m *DefaultMailer) SendMail(addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	return m.SendMailContext(context.Background(), addr, auth, from, to, msg)
}

// SendMailContext bounds all SMTP exchanges, including the greeting and DATA reply.
func (m *DefaultMailer) SendMailContext(ctx context.Context, addr string, auth smtp.Auth, from string, to []string, msg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	hasLineBreak := func(address string) bool { return strings.ContainsAny(address, "\r\n") }
	if hasLineBreak(from) || slices.ContainsFunc(to, hasLineBreak) {
		return errors.New("SMTP envelope addresses must not contain line breaks")
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return err
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	stopClose := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopClose()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	if err = client.Hello("localhost"); err != nil {
		return err
	}
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err = client.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if auth != nil {
		if ok, _ := client.Extension("AUTH"); !ok {
			return errors.New("smtp: server doesn't support AUTH")
		}
		if err = client.Auth(auth); err != nil {
			return err
		}
	}
	if err = client.Mail(from); err != nil {
		return err
	}
	for _, recipient := range to {
		if err = client.Rcpt(recipient); err != nil {
			return err
		}
	}
	data, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = data.Write(msg); err != nil {
		return err
	}
	if err = data.Close(); err != nil {
		return err
	}

	// DATA was acknowledged with 250. QUIT is now bounded, best-effort cleanup.
	if err = conn.SetDeadline(minTime(deadline, time.Now().Add(time.Second))); err == nil {
		_ = client.Quit()
	}
	return nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
