// Package file implements uploads and downloads backed by file_object (PRD §8).
package file

import (
	"bytes"
	"strings"
)

// Category values of file_object.category.
const (
	CategoryImage    = "image"
	CategoryDocument = "document"
)

// Policy is the per-purpose upload allowlist (PRD §8.1).
type Policy struct {
	Category string
	MIMEs    []string
	MaxBytes int64
}

const (
	mb = 1 << 20
	kb = 1 << 10
)

// Policies maps purpose (<table>.<field>) to its allowlist.
var Policies = map[string]Policy{
	"configuration.qr_logo": {Category: CategoryImage, MIMEs: []string{"image/png", "image/jpeg", "image/webp"}, MaxBytes: 2 * mb},

	"document.document":                         {Category: CategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * mb},
	"member_document.document":                  {Category: CategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * mb},
	"payment_document.document":                 {Category: CategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * mb},
	"cash_settlement.document":                  {Category: CategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * mb},
	"investor_coupon_schedule.payment_document": {Category: CategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * mb},
	"agent_fee_transaction.payment_document":    {Category: CategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * mb},
	"agent_fee_transaction.tax_document":        {Category: CategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * mb},
	"newsletter.document":                       {Category: CategoryDocument, MIMEs: []string{"application/pdf"}, MaxBytes: 20 * mb},
	"newsletter.email_template":                 {Category: CategoryDocument, MIMEs: []string{"text/html"}, MaxBytes: 512 * kb},
}

// MaxUploadBytes is the largest allowed upload; the HTTP body limit derives from it.
const MaxUploadBytes = 20 * mb

func (p Policy) allows(mime string) bool {
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
	p, ok := Policies[purpose]
	if !ok {
		return false
	}
	prefix := "images/"
	if p.Category == CategoryDocument {
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
