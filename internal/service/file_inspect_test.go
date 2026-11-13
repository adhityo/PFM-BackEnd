package service

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"pfm-backend/internal/apperr"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func kind(err error) apperr.Kind {
	if e, ok := apperr.As(err); ok {
		return e.Kind
	}
	return -1
}

func TestPrepareImage(t *testing.T) {
	p := UploadPolicies["configuration.qr_logo"]

	// Trailing payload appended to a valid PNG is stripped by re-encoding.
	raw := append(pngBytes(t, 8, 8), []byte("<script>alert(1)</script>")...)
	out, err := prepareUpload(raw, p)
	if err != nil {
		t.Fatal(err)
	}
	if out.mime != "image/png" || bytes.Contains(out.data, []byte("<script>")) || len(out.checksum) != 64 {
		t.Fatalf("unexpected output: mime=%s len=%d", out.mime, len(out.data))
	}

	if _, err := prepareUpload(pngBytes(t, 5000, 1), p); kind(err) != apperr.KindValidation {
		t.Fatalf("oversized dimensions accepted: %v", err)
	}
	if _, err := prepareUpload([]byte("%PDF-1.7 not an image"), p); kind(err) != apperr.KindUnsupportedMediaType {
		t.Fatalf("pdf accepted as image: %v", err)
	}
	if _, err := prepareUpload(bytes.Repeat([]byte{0}, int(p.MaxBytes)+1), p); kind(err) != apperr.KindPayloadTooLarge {
		t.Fatalf("too large accepted: %v", err)
	}
}

func TestPreparePDF(t *testing.T) {
	p := UploadPolicies["member_document.document"]
	ok := []byte("%PDF-1.7\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n%%EOF")
	if _, err := prepareUpload(ok, p); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"javascript":  "%PDF-1.7\n1 0 obj << /OpenAction << /S /JavaScript /JS (app.alert(1)) >> >> endobj",
		"hex escaped": "%PDF-1.7\n1 0 obj << /S /J#61vaScript >> endobj",
		"launch":      "%PDF-1.7\n1 0 obj << /S /Launch /F (cmd.exe) >> endobj",
		"embedded":    "%PDF-1.7\n1 0 obj << /Type /EmbeddedFile >> endobj",
	} {
		if _, err := prepareUpload([]byte(body), p); kind(err) != apperr.KindValidation {
			t.Fatalf("%s: active PDF accepted: %v", name, err)
		}
	}
	if _, err := prepareUpload(pngBytes(t, 2, 2), p); kind(err) != apperr.KindUnsupportedMediaType {
		t.Fatalf("png accepted as pdf: %v", err)
	}
	// /JSON is not /JS.
	if pdfHasActiveContent([]byte("%PDF-1.7 << /JSON 1 >>")) {
		t.Fatal("false positive on /JSON")
	}
}

func TestSanitizeNameAndPurpose(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":       "passwd",
		`C:\Users\x\laporan.pdf`: "laporan.pdf",
		"a\x00b\"<c>.pdf":        "abc.pdf",
		"":                       "file",
		"..":                     "file",
	}
	for in, want := range cases {
		if got := sanitizeFileName(in); got != want {
			t.Fatalf("sanitizeFileName(%q) = %q, want %q", in, got, want)
		}
	}
	if !KeyMatchesPurpose("documents/member_document-document/2026/11/x.pdf", "member_document.document") {
		t.Fatal("matching key rejected")
	}
	if KeyMatchesPurpose("documents/newsletter-document/2026/11/x.pdf", "member_document.document") {
		t.Fatal("key of another purpose accepted")
	}
}
