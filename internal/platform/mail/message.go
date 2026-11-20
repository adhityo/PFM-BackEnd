// Package mail queues and sends email. Services queue messages in the
// mail_outbox of their database inside the business transaction (Queue);
// the worker delivers them over SMTP outside any transaction (Deliverer).
package mail

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
)

// Message is one email to one recipient.
type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string // optional alternative
}

var errHeaderInjection = errors.New("mail: header berisi baris baru")

func (m Message) validate() error {
	if strings.ContainsAny(m.To, "\r\n") || strings.ContainsAny(m.Subject, "\r\n") {
		return errHeaderInjection
	}
	if _, err := mail.ParseAddress(m.To); err != nil {
		return fmt.Errorf("mail: penerima tidak valid: %w", err)
	}
	return nil
}

func randomToken() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// build renders the RFC 5322 message (multipart/alternative when HTML is set).
func build(from *mail.Address, m Message, now time.Time) ([]byte, error) {
	if err := m.validate(); err != nil {
		return nil, err
	}
	to, _ := mail.ParseAddress(m.To)
	domain := "localhost"
	if _, d, ok := strings.Cut(from.Address, "@"); ok {
		domain = d
	}
	var b bytes.Buffer
	h := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	h("From", from.String())
	h("To", to.String())
	h("Subject", mime.QEncoding.Encode("UTF-8", m.Subject))
	h("Date", now.Format(time.RFC1123Z))
	h("Message-ID", "<"+randomToken()+"@"+domain+">")
	h("MIME-Version", "1.0")
	part := func(contentType, body string) error {
		b.WriteString("Content-Type: " + contentType + "; charset=UTF-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
		w := quotedprintable.NewWriter(&b)
		if _, err := w.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n"))); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		b.WriteString("\r\n")
		return nil
	}
	if m.HTML == "" {
		if err := part("text/plain", m.Text); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}
	boundary := "pfm-" + randomToken()
	h("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	for _, p := range [][2]string{{"text/plain", m.Text}, {"text/html", m.HTML}} {
		b.WriteString("--" + boundary + "\r\n")
		if err := part(p[0], p[1]); err != nil {
			return nil, err
		}
	}
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes(), nil
}
