package service

import (
	"bytes"
	"strings"

	"pfm-backend/internal/model"
)

// UploadPolicy is the per-purpose upload allowlist (PRD §8.1).
type UploadPolicy struct {
	Category string
	MIMEs    []string
	MaxBytes int64
}

const (
	megabyte = 1 << 20
	kilobyte = 1 << 10
)

// UploadPolicies maps purpose (<table>.<field>) to its allowlist.
var UploadPolicies = map[string]UploadPolicy{
	"configuration.qr_logo": {Category: model.FileCategoryImage, MIMEs: []string{"image/png", "image/jpeg", "image/webp"}, MaxBytes: 2 * megabyte},

	"document.document":                         {Category: model.FileCategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * megabyte},
	"member_document.document":                  {Category: model.FileCategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * megabyte},
	"payment_document.document":                 {Category: model.FileCategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * megabyte},
	"cash_settlement.document":                  {Category: model.FileCategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * megabyte},
	"investor_coupon_schedule.payment_document": {Category: model.FileCategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * megabyte},
	"agent_fee_transaction.payment_document":    {Category: model.FileCategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * megabyte},
	"agent_fee_transaction.tax_document":        {Category: model.FileCategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * megabyte},
	"newsletter.document":                       {Category: model.FileCategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * megabyte},
	"newsletter.email_template":                 {Category: model.FileCategoryDocument, MIMEs: []string{"text/html"}, MaxBytes: 512 * kilobyte},
}

// MaxUploadBytes is the largest allowed upload; the HTTP body limit derives from it.
const MaxUploadBytes = 20 * megabyte

func (p UploadPolicy) allows(mime string) bool {
	for _, m := range p.MIMEs {
		if m == mime {
			return true
		}
	}
	return false
}

// purposeDir turns "member_document.document" into "member_document-document"
// for use inside object keys.
func purposeDir(purpose string) string { return strings.ReplaceAll(purpose, ".", "-") }

// KeyMatchesPurpose reports whether an object key was uploaded for purpose.
func KeyMatchesPurpose(key, purpose string) bool {
	p, ok := UploadPolicies[purpose]
	if !ok {
		return false
	}
	prefix := "images/"
	if p.Category == model.FileCategoryDocument {
		prefix = "documents/"
	}
	return strings.HasPrefix(key, prefix+purposeDir(purpose)+"/")
}

// dangerousPDFNames are PDF name objects that enable active content.
var dangerousPDFNames = map[string]bool{
	"JavaScript": true, "JS": true, "Launch": true, "EmbeddedFile": true, "EmbeddedFiles": true,
	"RichMedia": true, "XFA": true, "SubmitForm": true, "ImportData": true,
}

// pdfHasActiveContent scans uncompressed PDF name tokens (decoding #xx
// escapes) for active-content features. Content hidden inside compressed
// object streams is not inspected; an antivirus scan covers that case.
func pdfHasActiveContent(b []byte) bool {
	for i := 0; i < len(b); i++ {
		if b[i] != '/' {
			continue
		}
		j := i + 1
		var name bytes.Buffer
		for j < len(b) && !isPDFDelimiter(b[j]) {
			if b[j] == '#' && j+2 < len(b) && isHex(b[j+1]) && isHex(b[j+2]) {
				name.WriteByte(unhex(b[j+1])<<4 | unhex(b[j+2]))
				j += 3
				continue
			}
			name.WriteByte(b[j])
			j++
		}
		if dangerousPDFNames[name.String()] {
			return true
		}
		i = j - 1
	}
	return false
}

func isPDFDelimiter(c byte) bool {
	switch c {
	case ' ', '\t', '\r', '\n', '\f', 0, '/', '<', '>', '[', ']', '(', ')', '{', '}', '%':
		return true
	}
	return false
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func unhex(c byte) byte {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	default:
		return c - 'A' + 10
	}
}
