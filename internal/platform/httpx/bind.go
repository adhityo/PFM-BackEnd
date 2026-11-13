package httpx

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/apperr"
)

// MaxJSONBody is the body limit for JSON endpoints (PRD §11.1).
const MaxJSONBody = 1 << 20

// Validator validates a decoded DTO.
type Validator interface {
	Validate(out any) error
}

// Normalizer is implemented by DTOs that trim/canonicalise input before validation.
type Normalizer interface {
	Normalize()
}

// StrictJSONDecode rejects unknown fields and trailing data.
func StrictJSONDecode(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return apperr.BadRequest(jsonErrorMessage(err), err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return apperr.BadRequest("body harus berisi tepat satu objek JSON", nil)
	}
	return nil
}

func jsonErrorMessage(err error) string {
	var typeErr *json.UnmarshalTypeError
	var syntaxErr *json.SyntaxError
	switch {
	case errors.As(err, &typeErr):
		return fmt.Sprintf("tipe data field %q tidak sesuai", typeErr.Field)
	case errors.As(err, &syntaxErr), errors.Is(err, io.ErrUnexpectedEOF):
		return "JSON tidak valid"
	case errors.Is(err, io.EOF):
		return "body tidak boleh kosong"
	case strings.HasPrefix(err.Error(), "json: unknown field"):
		return "field tidak dikenal: " + strings.TrimPrefix(err.Error(), "json: unknown field ")
	default:
		return "JSON tidak valid"
	}
}

// BindJSON enforces Content-Type and size, decodes strictly, normalizes and validates.
func BindJSON(c fiber.Ctx, v Validator, out any) error {
	ct := strings.ToLower(strings.TrimSpace(strings.SplitN(c.Get(fiber.HeaderContentType), ";", 2)[0]))
	if ct != fiber.MIMEApplicationJSON {
		return apperr.UnsupportedMediaType("Content-Type harus application/json")
	}
	body := c.Body()
	if len(body) > MaxJSONBody {
		return apperr.PayloadTooLarge("body JSON maksimal 1 MB")
	}
	if err := StrictJSONDecode(body, out); err != nil {
		return err
	}
	if n, ok := out.(Normalizer); ok {
		n.Normalize()
	}
	return v.Validate(out)
}
