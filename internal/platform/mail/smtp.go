package mail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"time"
)

// TLS modes of an SMTP server.
const (
	TLSStartTLS = "starttls" // plain connection upgraded with STARTTLS (ports 587, 2525; Mailtrap)
	TLSImplicit = "tls"      // TLS from the first byte (port 465)
	TLSNone     = "none"     // no encryption: local catchers only (Mailpit)
)

// SMTPConfig configures the SMTP sender.
type SMTPConfig struct {
	Host, Username, Password string
	Port                     int
	TLS                      string
	From                     string // "Nama <alamat@domain>"
	Timeout                  time.Duration
}

// Sender delivers one message.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// SMTPSender sends mail through an SMTP server (net/smtp).
type SMTPSender struct {
	cfg  SMTPConfig
	from *mail.Address
}

func NewSMTPSender(cfg SMTPConfig) (*SMTPSender, error) {
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return nil, fmt.Errorf("SMTP_FROM tidak valid: %w", err)
	}
	switch cfg.TLS {
	case TLSStartTLS, TLSImplicit, TLSNone:
	default:
		return nil, fmt.Errorf("SMTP_TLS harus starttls, tls atau none (bukan %q)", cfg.TLS)
	}
	if cfg.Host == "" || cfg.Port <= 0 {
		return nil, errors.New("SMTP_HOST dan SMTP_PORT wajib diisi")
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	return &SMTPSender{cfg: cfg, from: from}, nil
}

func (s *SMTPSender) Send(ctx context.Context, m Message) error {
	msg, err := build(s.from, m, time.Now())
	if err != nil {
		return err
	}
	addr := net.JoinHostPort(s.cfg.Host, strconv.Itoa(s.cfg.Port))
	tlsConf := &tls.Config{ServerName: s.cfg.Host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: s.cfg.Timeout}
	var conn net.Conn
	if s.cfg.TLS == TLSImplicit {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsConf}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("smtp connect: %w", err)
	}
	deadline := time.Now().Add(s.cfg.Timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, s.cfg.Host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer func() { _ = c.Close() }()
	if s.cfg.TLS == TLSStartTLS {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("smtp: server tidak mendukung STARTTLS (cek SMTP_TLS/SMTP_PORT)")
		}
		if err := c.StartTLS(tlsConf); err != nil {
			return fmt.Errorf("smtp starttls: %w", err)
		}
	}
	if s.cfg.Username != "" {
		// PlainAuth refuses to send credentials over an unencrypted, non-local connection.
		if err := c.Auth(smtp.PlainAuth("", s.cfg.Username, s.cfg.Password, s.cfg.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	to, _ := mail.ParseAddress(m.To)
	if err := c.Mail(s.from.Address); err != nil {
		return fmt.Errorf("smtp mail from: %w", err)
	}
	if err := c.Rcpt(to.Address); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("smtp data end: %w", err)
	}
	return c.Quit()
}
