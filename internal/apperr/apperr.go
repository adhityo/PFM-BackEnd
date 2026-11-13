// Package apperr defines domain errors that are mapped to HTTP responses in a single place.
package apperr

import (
	"errors"
	"fmt"
)

// Kind classifies an error so the HTTP layer can map it to a status code.
type Kind int

const (
	KindInternal Kind = iota
	KindBadRequest
	KindUnauthenticated
	KindForbidden
	KindNotFound
	KindConflict
	KindPreconditionFailed
	KindPreconditionRequired
	KindValidation
	KindPayloadTooLarge
	KindUnsupportedMediaType
	KindRateLimited
	KindUnavailable
)

// FieldError describes a single invalid input field.
type FieldError struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

// Error is the application error type. Message is safe to show to clients;
// the wrapped cause is only logged.
type Error struct {
	Kind    Kind
	Code    string
	Message string
	Fields  []FieldError
	cause   error
}

func (e *Error) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.cause)
	}
	return e.Message
}

func (e *Error) Unwrap() error { return e.cause }

// Is lets errors.Is match sentinel errors by kind and code.
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Kind == t.Kind && e.Code == t.Code
}

func newErr(kind Kind, code, msg string, cause error) *Error {
	return &Error{Kind: kind, Code: code, Message: msg, cause: cause}
}

// Sentinel errors for errors.Is checks.
var (
	ErrNotFound             = newErr(KindNotFound, "NOT_FOUND", "data tidak ditemukan", nil)
	ErrPreconditionFailed   = newErr(KindPreconditionFailed, "PRECONDITION_FAILED", "data telah diubah oleh pengguna lain, muat ulang lalu coba lagi", nil)
	ErrPreconditionRequired = newErr(KindPreconditionRequired, "PRECONDITION_REQUIRED", "header If-Match wajib diisi", nil)
	ErrUnauthenticated      = newErr(KindUnauthenticated, "UNAUTHENTICATED", "autentikasi diperlukan", nil)
	ErrForbidden            = newErr(KindForbidden, "FORBIDDEN", "akses ditolak", nil)
)

func BadRequest(msg string, cause error) *Error {
	return newErr(KindBadRequest, "BAD_REQUEST", msg, cause)
}

func Unauthenticated(msg string) *Error {
	return newErr(KindUnauthenticated, "UNAUTHENTICATED", msg, nil)
}

func Forbidden(msg string) *Error {
	return newErr(KindForbidden, "FORBIDDEN", msg, nil)
}

func NotFound(msg string) *Error {
	return newErr(KindNotFound, "NOT_FOUND", msg, nil)
}

func Conflict(msg string) *Error {
	return newErr(KindConflict, "CONFLICT", msg, nil)
}

func PayloadTooLarge(msg string) *Error {
	return newErr(KindPayloadTooLarge, "PAYLOAD_TOO_LARGE", msg, nil)
}

func UnsupportedMediaType(msg string) *Error {
	return newErr(KindUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", msg, nil)
}

func Unavailable(msg string, cause error) *Error {
	return newErr(KindUnavailable, "SERVICE_UNAVAILABLE", msg, cause)
}

// Validation returns a 422 error carrying per-field details.
func Validation(fields ...FieldError) *Error {
	e := newErr(KindValidation, "VALIDATION_ERROR", "input tidak valid", nil)
	e.Fields = fields
	return e
}

// Invalid is a shortcut for a single-field business validation failure.
func Invalid(field, rule, msg string) *Error {
	return Validation(FieldError{Field: field, Rule: rule, Message: msg})
}

// As extracts *Error from err.
func As(err error) (*Error, bool) {
	var e *Error
	if errors.As(err, &e) {
		return e, true
	}
	return nil, false
}
