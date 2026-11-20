package service

import (
	"bytes"
	"context"
	"fmt"
	htmltemplate "html/template"
	"net/url"
	"strings"
	texttemplate "text/template"
	"time"

	"pfm-backend/internal/platform/mail"
)

// Frontend pages that consume the links; both call POST /auth/activate
// with the key from the query string.
const (
	ActivationPath    = "/activate"
	PasswordResetPath = "/reset-password"
)

type accountMail struct {
	subject, path string
	intro         string
}

var (
	mailActivation = accountMail{subject: "Aktivasi akun PFM Anda", path: ActivationPath,
		intro: "Akun PFM Anda telah dibuat dengan username {{.Username}}. Atur password Anda untuk mulai menggunakan aplikasi."}
	mailAdminReset = accountMail{subject: "Password akun PFM Anda telah direset", path: PasswordResetPath,
		intro: "Administrator telah mereset password akun {{.Username}}. Semua sesi login sebelumnya telah diakhiri. Atur password baru Anda."}
	mailForgotPassword = accountMail{subject: "Atur ulang password PFM", path: PasswordResetPath,
		intro: "Kami menerima permintaan untuk mengatur ulang password akun {{.Username}}. Password lama tetap berlaku sampai Anda menggantinya."}
)

const mailText = `Halo {{.Name}},

{{.Intro}}

{{.Link}}

Tautan ini hanya dapat dipakai sekali dan berlaku selama {{.Validity}}.
Jika Anda tidak merasa meminta ini, abaikan email ini.

PFM`

var mailHTML = htmltemplate.Must(htmltemplate.New("mail").Parse(`<!doctype html>
<html lang="id"><body style="margin:0;padding:24px;background:#f5f6f8;font-family:Arial,Helvetica,sans-serif;color:#1f2937">
<table role="presentation" width="100%" style="max-width:560px;margin:0 auto;background:#ffffff;border-radius:8px;padding:32px">
<tr><td>
<p style="margin:0 0 16px">Halo {{.Name}},</p>
<p style="margin:0 0 24px;line-height:1.5">{{.Intro}}</p>
<p style="margin:0 0 24px"><a href="{{.Link}}" style="display:inline-block;background:#2563eb;color:#ffffff;text-decoration:none;padding:12px 20px;border-radius:6px">{{.Action}}</a></p>
<p style="margin:0 0 8px;font-size:13px;color:#6b7280">Atau buka tautan ini: <a href="{{.Link}}" style="color:#2563eb;word-break:break-all">{{.Link}}</a></p>
<p style="margin:0 0 8px;font-size:13px;color:#6b7280">Tautan hanya dapat dipakai sekali dan berlaku selama {{.Validity}}.</p>
<p style="margin:0;font-size:13px;color:#6b7280">Jika Anda tidak merasa meminta ini, abaikan email ini.</p>
</td></tr></table></body></html>`))

// AccountMailer queues account emails in pfm_security's mail_outbox, inside
// the caller's transaction; the worker delivers them.
type AccountMailer struct {
	queue   *mail.Queue
	baseURL string
}

func NewAccountMailer(queue *mail.Queue, frontendBaseURL string) *AccountMailer {
	return &AccountMailer{queue: queue, baseURL: strings.TrimRight(frontendBaseURL, "/")}
}

// validity spells a TTL in Indonesian ("24 jam", "30 menit").
func validity(ttl time.Duration) string {
	if ttl >= time.Hour && ttl%time.Hour == 0 {
		return fmt.Sprintf("%d jam", int(ttl.Hours()))
	}
	return fmt.Sprintf("%d menit", int(ttl.Minutes()))
}

func (m *AccountMailer) queueMail(ctx context.Context, kind accountMail, to, name, username, rawKey string, ttl time.Duration) error {
	if name == "" {
		name = username
	}
	intro, err := texttemplate.New("intro").Parse(kind.intro)
	if err != nil {
		return err
	}
	var introBuf bytes.Buffer
	if err := intro.Execute(&introBuf, map[string]string{"Username": username}); err != nil {
		return err
	}
	action := "Atur password"
	if kind.path == PasswordResetPath {
		action = "Atur ulang password"
	}
	data := map[string]string{"Name": name, "Intro": introBuf.String(), "Validity": validity(ttl), "Action": action,
		"Link": m.baseURL + kind.path + "?key=" + url.QueryEscape(rawKey)}
	var text, html bytes.Buffer
	if err := texttemplate.Must(texttemplate.New("text").Parse(mailText)).Execute(&text, data); err != nil {
		return err
	}
	if err := mailHTML.Execute(&html, data); err != nil {
		return err
	}
	return m.queue.Enqueue(ctx, mail.Message{To: to, Subject: kind.subject, Text: text.String(), HTML: html.String()})
}

func (m *AccountMailer) Activation(ctx context.Context, to, name, username, rawKey string, ttl time.Duration) error {
	return m.queueMail(ctx, mailActivation, to, name, username, rawKey, ttl)
}

func (m *AccountMailer) AdminReset(ctx context.Context, to, name, username, rawKey string, ttl time.Duration) error {
	return m.queueMail(ctx, mailAdminReset, to, name, username, rawKey, ttl)
}

func (m *AccountMailer) ForgotPassword(ctx context.Context, to, name, username, rawKey string, ttl time.Duration) error {
	return m.queueMail(ctx, mailForgotPassword, to, name, username, rawKey, ttl)
}
