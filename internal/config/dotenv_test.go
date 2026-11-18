package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := "# komentar\n\nPFM_T_PLAIN=postgres://u:p@h:5432/db?sslmode=disable\nexport PFM_T_EXPORT=ya\n" +
		"PFM_T_DQ=\"a # bukan komentar\"\nPFM_T_SQ='b'\nPFM_T_COMMENT=nilai # komentar\nPFM_T_EMPTY=\nPFM_T_KEEP=dari-file\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PFM_T_KEEP", "dari-env")
	for _, k := range []string{"PFM_T_PLAIN", "PFM_T_EXPORT", "PFM_T_DQ", "PFM_T_SQ", "PFM_T_COMMENT", "PFM_T_EMPTY"} {
		t.Setenv(k, "") // registers cleanup, then unset so the file can fill it
		_ = os.Unsetenv(k)
	}
	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"PFM_T_PLAIN": "postgres://u:p@h:5432/db?sslmode=disable", "PFM_T_EXPORT": "ya", "PFM_T_DQ": "a # bukan komentar",
		"PFM_T_SQ": "b", "PFM_T_COMMENT": "nilai", "PFM_T_EMPTY": "", "PFM_T_KEEP": "dari-env",
	}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if err := loadDotEnv(filepath.Join(t.TempDir(), "tidak-ada.env")); err != nil {
		t.Fatalf("missing file must be ignored: %v", err)
	}
	bad := filepath.Join(t.TempDir(), "bad.env")
	_ = os.WriteFile(bad, []byte("BUKAN FORMAT\n"), 0o600)
	if err := loadDotEnv(bad); err == nil {
		t.Fatal("invalid line must be reported")
	}
}
