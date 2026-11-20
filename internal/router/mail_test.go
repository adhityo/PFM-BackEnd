package router

import (
	"context"
	"net/url"
	"regexp"
	"testing"
)

type queuedMail struct{ to, subject, text, html string }

func (e *env) mails() []queuedMail {
	e.t.Helper()
	rows, err := e.secPool.Query(context.Background(), "SELECT recipient, subject, body_text, COALESCE(body_html, '') FROM mail_outbox ORDER BY id")
	if err != nil {
		e.t.Fatal(err)
	}
	defer rows.Close()
	var out []queuedMail
	for rows.Next() {
		var m queuedMail
		_ = rows.Scan(&m.to, &m.subject, &m.text, &m.html)
		out = append(out, m)
	}
	return out
}

var reLinkKey = regexp.MustCompile(`(https://pfm\.test/[a-z-]+)\?key=(\S+)`)

// linkKey returns the frontend page and the key of the link in a mail.
func linkKey(t *testing.T, m queuedMail) (page, key string) {
	t.Helper()
	g := reLinkKey.FindStringSubmatch(m.text)
	if g == nil {
		t.Fatalf("email tanpa tautan: %q", m.text)
	}
	k, err := url.QueryUnescape(g[2])
	if err != nil {
		t.Fatal(err)
	}
	return g[1], k
}

func (e *env) staffUser(admin string) map[string]any {
	e.t.Helper()
	role := e.create(admin, "/roles", map[string]any{"role_code": "STAF", "role_name": "Staf", "status": "active"})["uuid"]
	return e.expect(e.do("POST", "/api/v1/users", admin, map[string]any{"username": "budi", "email": "Budi@Example.test",
		"first_name": "Budi", "role_uuid": role, "status": "active"}), 201, "create user").data()
}

// New users and admin resets receive their link by email. (Auth endpoints
// allow 10 requests/minute, so each test stays below that.)
func TestActivationAndAdminResetEmails(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	created := e.staffUser(admin)
	mails := e.mails()
	if len(mails) != 1 || mails[0].to != "budi@example.test" || mails[0].subject != "Aktivasi akun PFM Anda" || mails[0].html == "" {
		t.Fatalf("email aktivasi: %+v", mails)
	}
	page, key := linkKey(t, mails[0])
	if page != "https://pfm.test/activate" || key != created["activation_key"] {
		t.Fatalf("tautan aktivasi %s, key sama dengan respons: %v", page, key == created["activation_key"])
	}
	e.expect(e.do("POST", "/api/v1/auth/activate", "", map[string]string{"activation_key": key, "new_password": "Pertama-Pass-123"}), 204, "activate from email")
	e.expect(e.do("POST", "/api/v1/auth/activate", "", map[string]string{"activation_key": key, "new_password": "Kedua-Pass-12345"}), 422, "link is single use")
	e.login("budi", "Pertama-Pass-123")

	uPath := "/users/" + created["user"].(map[string]any)["uuid"].(string)
	e.expect(e.do("POST", "/api/v1"+uPath+"/reset-password", admin, nil, "If-Match", e.etag(admin, uPath)), 200, "admin reset")
	mails = e.mails()
	if len(mails) != 2 || mails[1].subject != "Password akun PFM Anda telah direset" {
		t.Fatalf("email reset admin: %+v", mails)
	}
	if page, _ := linkKey(t, mails[1]); page != "https://pfm.test/reset-password" {
		t.Fatalf("tautan reset admin: %s", page)
	}
	e.expect(e.do("POST", "/api/v1/auth/login", "", map[string]string{"login": "budi", "password": "Pertama-Pass-123"}), 401, "password cleared by admin reset")
}

// Anyone can ask for a reset link without learning whether an email is
// registered; the old password works until the link is used.
func TestForgotPasswordEmail(t *testing.T) {
	e := setup(t)
	admin, _ := e.login("admin", adminPassword)
	created := e.staffUser(admin)
	e.expect(e.do("POST", "/api/v1/auth/activate", "", map[string]string{"activation_key": created["activation_key"].(string),
		"new_password": "Pertama-Pass-123"}), 204, "activate")
	_, oldRefresh := e.login("budi", "Pertama-Pass-123")

	e.expect(e.do("POST", "/api/v1/auth/forgot-password", "", map[string]string{"email": "tidak.ada@example.test"}), 204, "unknown email")
	if n := len(e.mails()); n != 1 {
		t.Fatalf("email tak dikenal tidak boleh mengirim apa pun (%d email)", n)
	}
	e.expect(e.do("POST", "/api/v1/auth/forgot-password", "", map[string]string{"email": "BUDI@example.test"}), 204, "known email")
	mails := e.mails()
	if len(mails) != 2 || mails[1].subject != "Atur ulang password PFM" {
		t.Fatalf("email reset: %+v", mails)
	}
	e.expect(e.do("POST", "/api/v1/auth/forgot-password", "", map[string]string{"email": "budi@example.test"}), 204, "throttled")
	if n := len(e.mails()); n != 2 {
		t.Fatalf("permintaan beruntun harus dibatasi (%d email)", n)
	}
	e.login("budi", "Pertama-Pass-123") // still valid until the link is used

	page, key := linkKey(t, mails[1])
	if page != "https://pfm.test/reset-password" {
		t.Fatalf("tautan reset: %s", page)
	}
	e.expect(e.do("POST", "/api/v1/auth/activate", "", map[string]string{"activation_key": key, "new_password": "Kedua-Pass-12345"}), 204, "reset from email")
	e.login("budi", "Kedua-Pass-12345")
	e.expect(e.do("POST", "/api/v1/auth/refresh", "", map[string]string{"refresh_token": oldRefresh}), 401, "sessions revoked by reset")
}
