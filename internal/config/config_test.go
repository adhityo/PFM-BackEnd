package config

import "testing"

func TestMailFrom(t *testing.T) {
	cases := []struct{ from, name, want string }{
		{"no-reply@pfm.local", "PFM", `"PFM" <no-reply@pfm.local>`},
		{"no-reply@pfm.local", "", "no-reply@pfm.local"},
		{"PFM <no-reply@pfm.local>", "Diabaikan", "PFM <no-reply@pfm.local>"}, // format lama tetap diterima
	}
	for _, c := range cases {
		if got := (Config{SMTPFrom: c.from, SMTPFromName: c.name}).MailFrom(); got != c.want {
			t.Errorf("MailFrom(%q, %q) = %q, want %q", c.from, c.name, got, c.want)
		}
	}
}

func TestWorkerDatabaseURLs(t *testing.T) {
	c := Config{DatabaseURLCore: "core-app", DatabaseURLSecurity: "sec-app", DatabaseURLLog: "log-reader",
		DatabaseURLDocument: "doc-app", WorkerURLLog: "log-writer"}
	got := c.WorkerDatabaseURLs()
	if got["log"] != "log-writer" || got["core"] != "core-app" || c.DatabaseURLs()["log"] != "log-reader" {
		t.Fatalf("worker DSN: %v", got)
	}
	if errs := c.ValidateWorkerDatabases(); len(errs) != 0 {
		t.Fatalf("valid: %v", errs)
	}
	c.WorkerURLCore = "sec-app"
	if errs := c.ValidateWorkerDatabases(); len(errs) != 1 {
		t.Fatalf("DSN ganda harus ditolak: %v", errs)
	}
}
