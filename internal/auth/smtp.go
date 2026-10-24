// SCOPE:core — Minimal SMTP mailer for verification/reset delivery.
//
// net/smtp keeps the template dependency-free. It uses STARTTLS when the
// server advertises it (port 587 style); implicit TLS (465) is intentionally
// out of scope — terminate there with a provider that speaks STARTTLS or use
// an HTTP mail API behind the same Mailer seam.
package auth

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// NewSMTPMailer returns a Mailer for addr (host:port), from (envelope
// sender), and optional AUTH credentials. Malformed recipient/subject
// headers are rejected before they reach the wire.
func NewSMTPMailer(addr, from, username, password string) Mailer {
	var auth smtp.Auth
	if username != "" {
		auth = smtp.PlainAuth("", username, password, hostOf(addr))
	}
	return func(to, subject, body string) error {
		return sendSMTP(addr, from, auth, to, subject, body)
	}
}

const (
	smtpDialTimeout = 10 * time.Second
	smtpIOTimeout   = 20 * time.Second
)

func sendSMTP(addr, from string, auth smtp.Auth, to, subject, body string) error {
	fromAddr, err := mailboxAddress(from)
	if err != nil {
		return fmt.Errorf("auth: smtp sender: %w", err)
	}
	rcpt, err := mailboxAddress(to)
	if err != nil {
		return fmt.Errorf("auth: smtp recipient: %w", err)
	}
	if strings.ContainsAny(subject, "\r\n") || strings.ContainsAny(rcpt, "\r\n") {
		return errors.New("auth: smtp header injection")
	}
	msg := "From: " + fromAddr + "\r\n" +
		"To: " + rcpt + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"\r\n" + body + "\r\n"
	client, err := openSMTP(addr, auth)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if err := deliverSMTP(client, fromAddr, rcpt, msg); err != nil {
		return err
	}
	return client.Quit()
}

func openSMTP(addr string, auth smtp.Auth) (*smtp.Client, error) {
	host := hostOf(addr)
	conn, err := (&net.Dialer{Timeout: smtpDialTimeout}).DialContext(context.Background(), "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("auth: smtp dial: %w", err)
	}
	_ = conn.SetDeadline(time.Now().Add(smtpIOTimeout))
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("auth: smtp client: %w", err)
	}
	if ok, _ := client.Extension("STARTTLS"); ok {
		if terr := client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); terr != nil {
			_ = client.Close()
			return nil, fmt.Errorf("auth: smtp starttls: %w", terr)
		}
	}
	if auth != nil {
		if aerr := client.Auth(auth); aerr != nil {
			_ = client.Close()
			return nil, fmt.Errorf("auth: smtp auth: %w", aerr)
		}
	}
	return client, nil
}

func deliverSMTP(client *smtp.Client, from, to, msg string) error {
	if err := client.Mail(from); err != nil {
		return fmt.Errorf("auth: smtp mail: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("auth: smtp rcpt: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("auth: smtp data: %w", err)
	}
	if _, err := io.WriteString(writer, msg); err != nil {
		return fmt.Errorf("auth: smtp write: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("auth: smtp close data: %w", err)
	}
	return nil
}

func hostOf(addr string) string {
	if i := strings.LastIndex(addr, ":"); i > 0 {
		return addr[:i]
	}
	return addr
}
