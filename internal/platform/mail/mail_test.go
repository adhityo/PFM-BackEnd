package mail_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"pfm-backend/internal/db"
	"pfm-backend/internal/platform/database"
	"pfm-backend/internal/platform/mail"
	"pfm-backend/internal/testutil"
)

// fakeSMTP is a minimal SMTP server (no TLS, no auth) like Mailpit.
type fakeSMTP struct {
	addr string
	msgs chan string
}

func startFakeSMTP(t *testing.T) *fakeSMTP {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &fakeSMTP{addr: ln.Addr().String(), msgs: make(chan string, 10)}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeSMTP) serve(conn net.Conn) {
	tp := textproto.NewConn(conn)
	defer func() { _ = tp.Close() }()
	_ = tp.PrintfLine("220 fake ESMTP")
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		switch cmd := strings.ToUpper(line); {
		case strings.HasPrefix(cmd, "EHLO"):
			_ = tp.PrintfLine("250-fake")
			_ = tp.PrintfLine("250 8BITMIME")
		case cmd == "DATA":
			_ = tp.PrintfLine("354 kirim")
			lines, _ := tp.ReadDotLines()
			f.msgs <- strings.Join(lines, "\n")
			_ = tp.PrintfLine("250 OK")
		case cmd == "QUIT":
			_ = tp.PrintfLine("221 bye")
			return
		default:
			_ = tp.PrintfLine("250 OK")
		}
	}
}

func TestSMTPSenderDeliversMultipartMessage(t *testing.T) {
	srv := startFakeSMTP(t)
	host, port, _ := net.SplitHostPort(srv.addr)
	var p int
	for _, c := range port {
		p = p*10 + int(c-'0')
	}
	s, err := mail.NewSMTPSender(mail.SMTPConfig{Host: host, Port: p, TLS: mail.TLSNone, From: "PFM <no-reply@pfm.test>", Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	err = s.Send(context.Background(), mail.Message{To: "budi@example.test", Subject: "Aktivasi akun — PFM",
		Text: "Halo Budi,\nbuka tautan ini", HTML: "<p>Halo Budi</p>"})
	if err != nil {
		t.Fatal(err)
	}
	msg := <-srv.msgs
	for _, want := range []string{"From: \"PFM\" <no-reply@pfm.test>", "To: <budi@example.test>", "Subject: =?UTF-8?q?",
		"multipart/alternative", "text/plain; charset=UTF-8", "text/html; charset=UTF-8", "Halo Budi"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("pesan tidak memuat %q:\n%s", want, msg)
		}
	}
	if err := s.Send(context.Background(), mail.Message{To: "x@example.test\r\nBcc: korban@example.test", Subject: "a"}); err == nil {
		t.Fatal("header injection harus ditolak")
	}
	if _, err := mail.NewSMTPSender(mail.SMTPConfig{Host: host, Port: p, TLS: "ssl", From: "x@y.z"}); err == nil {
		t.Fatal("mode TLS tidak dikenal harus ditolak")
	}
}

type failingSender struct{ calls int }

func (f *failingSender) Send(context.Context, mail.Message) error {
	f.calls++
	return errors.New("smtp 451 coba lagi")
}

type recordingSender struct{ got []mail.Message }

func (r *recordingSender) Send(_ context.Context, m mail.Message) error {
	r.got = append(r.got, m)
	return nil
}

// Messages are delivered once and removed; failures are retried later and the
// body (which may hold a secret link) is scrubbed after the last attempt.
func TestDelivererRetriesAndScrubs(t *testing.T) {
	sec := database.NewTxManager(testutil.NewDB(t, db.Security))
	ctx := context.Background()
	q := mail.NewQueue(sec)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	count := func(where string) int {
		var n int
		if err := sec.Executor(ctx).QueryRow(ctx, "SELECT count(*) FROM mail_outbox WHERE "+where).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	if err := q.Enqueue(ctx, mail.Message{To: "a@example.test", Subject: "Satu", Text: "rahasia ?key=abc"}); err != nil {
		t.Fatal(err)
	}
	failing := &failingSender{}
	d := mail.NewDeliverer(map[string]*database.TxManager{db.Security: sec}, failing, log)
	if n, err := d.DeliverDue(ctx); err != nil || n != 0 || failing.calls != 1 {
		t.Fatalf("gagal kirim: n=%d err=%v calls=%d", n, err, failing.calls)
	}
	if count("attempts = 1 AND last_error IS NOT NULL AND next_attempt_at > now()") != 1 {
		t.Fatal("kegagalan harus dicatat dan dijadwalkan ulang")
	}
	if n, _ := d.DeliverDue(ctx); n != 0 || failing.calls != 1 {
		t.Fatal("pesan belum jatuh tempo tidak boleh dikirim ulang")
	}

	// Last attempt: the secret body is removed.
	if _, err := sec.Executor(ctx).Exec(ctx, "UPDATE mail_outbox SET attempts = $1, next_attempt_at = now()", mail.MaxAttempts-1); err != nil {
		t.Fatal(err)
	}
	_, _ = d.DeliverDue(ctx)
	if count("body_text NOT LIKE '%key=%' AND body_html IS NULL") != 1 {
		t.Fatal("isi email harus dikosongkan setelah percobaan terakhir")
	}

	// A healthy server delivers and removes due messages.
	if _, err := sec.Executor(ctx).Exec(ctx, "DELETE FROM mail_outbox"); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, mail.Message{To: "b@example.test", Subject: "Dua", Text: "isi", HTML: "<p>isi</p>"}); err != nil {
		t.Fatal(err)
	}
	ok := &recordingSender{}
	d = mail.NewDeliverer(map[string]*database.TxManager{db.Security: sec}, ok, log)
	if n, err := d.DeliverDue(ctx); err != nil || n != 1 || len(ok.got) != 1 || ok.got[0].HTML == "" {
		t.Fatalf("kirim: n=%d err=%v got=%v", n, err, ok.got)
	}
	if count("true") != 0 {
		t.Fatal("pesan terkirim harus dihapus dari antrean")
	}
}
