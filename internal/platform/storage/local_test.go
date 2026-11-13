package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalImageStorage(t *testing.T) {
	dir := t.TempDir()
	s, err := NewLocalImageStorage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()

	if err := s.Put(ctx, "images/2026/11/a.png", strings.NewReader("png-bytes")); err != nil {
		t.Fatal(err)
	}
	rc, err := s.Open(ctx, "images/2026/11/a.png")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(b) != "png-bytes" {
		t.Fatalf("content = %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "images/2026/11/a.png.tmp")); !os.IsNotExist(err) {
		t.Fatal("temp file left behind")
	}

	for _, bad := range []string{"../escape.png", "/etc/passwd", "images/../../x", "", "a/./b"} {
		if err := s.Put(ctx, bad, strings.NewReader("x")); err == nil {
			t.Fatalf("key %q accepted", bad)
		}
	}

	// A symlink pointing outside the root must not be followed.
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(ctx, "link/evil.png", strings.NewReader("x")); err == nil {
		t.Fatal("write through symlink escaping root accepted")
	}

	if err := s.Delete(ctx, "images/2026/11/a.png"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, "images/2026/11/a.png"); err != nil {
		t.Fatal("delete of missing file should be idempotent")
	}
}
