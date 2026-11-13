package file

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	_ "golang.org/x/image/webp" // register WebP decoder

	"pfm-backend/internal/apperr"
)

const maxImageDimension = 4096

// prepared is a validated upload ready to be stored.
type prepared struct {
	data     []byte
	mime     string
	ext      string
	checksum string
}

// detectMIME uses magic bytes only; the client's Content-Type is ignored.
func detectMIME(b []byte) string {
	mime := http.DetectContentType(b)
	mime, _, _ = strings.Cut(mime, ";")
	return mime
}

// prepare validates content against the policy and sanitises it.
func prepare(raw []byte, p Policy) (prepared, error) {
	if len(raw) == 0 {
		return prepared{}, apperr.Invalid("file", "required", "file kosong")
	}
	if int64(len(raw)) > p.MaxBytes {
		return prepared{}, apperr.PayloadTooLarge("ukuran file melebihi batas untuk keperluan ini")
	}
	mime := detectMIME(raw)
	if mime == "text/plain" && p.allows("text/html") {
		mime = "text/html"
	}
	if !p.allows(mime) {
		return prepared{}, apperr.UnsupportedMediaType("jenis file tidak diizinkan: " + mime)
	}

	var out prepared
	switch p.Category {
	case CategoryImage:
		img, err := sanitizeImage(raw, mime)
		if err != nil {
			return prepared{}, err
		}
		out = img
	default:
		switch mime {
		case "application/pdf":
			if !bytes.HasPrefix(raw, []byte("%PDF-")) {
				return prepared{}, apperr.UnsupportedMediaType("file bukan PDF yang valid")
			}
			if pdfHasActiveContent(raw) {
				return prepared{}, apperr.Invalid("file", "pdf_active_content", "PDF berisi JavaScript/aksi/lampiran tersemat yang tidak diizinkan")
			}
			out = prepared{data: raw, mime: mime, ext: "pdf"}
		case "text/html":
			if !utf8.Valid(raw) {
				return prepared{}, apperr.Invalid("file", "utf8", "template harus UTF-8")
			}
			out = prepared{data: raw, mime: "text/html", ext: "html"}
		default:
			return prepared{}, apperr.UnsupportedMediaType("jenis file tidak diizinkan")
		}
	}
	sum := sha256.Sum256(out.data)
	out.checksum = hex.EncodeToString(sum[:])
	return out, nil
}

// sanitizeImage checks dimensions before decoding (decompression bombs) and
// re-encodes the pixels, dropping metadata and any appended payload.
// WebP is re-encoded as PNG because the standard library has no WebP encoder.
func sanitizeImage(raw []byte, mime string) (prepared, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return prepared{}, apperr.UnsupportedMediaType("gambar tidak dapat dibaca")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxImageDimension || cfg.Height > maxImageDimension {
		return prepared{}, apperr.Invalid("file", "dimension", "dimensi gambar maksimal 4096x4096 piksel")
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return prepared{}, apperr.UnsupportedMediaType("gambar rusak")
	}
	var buf bytes.Buffer
	switch mime {
	case "image/jpeg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
			return prepared{}, err
		}
		return prepared{data: buf.Bytes(), mime: "image/jpeg", ext: "jpg"}, nil
	default:
		if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img); err != nil {
			return prepared{}, err
		}
		return prepared{data: buf.Bytes(), mime: "image/png", ext: "png"}, nil
	}
}

// sanitizeName keeps a display-safe base name (never used as a storage path).
func sanitizeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, `\`, "/"))
	var b strings.Builder
	for _, r := range name {
		if unicode.IsControl(r) || r == '/' || r == '"' || r == '\'' || r == '<' || r == '>' {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		out = "file"
	}
	for len(out) > 255 {
		_, size := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-size]
	}
	return out
}
