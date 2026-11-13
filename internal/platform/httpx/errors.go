package httpx

import (
	"context"
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v3"

	"pfm-backend/internal/apperr"
)

type errorBody struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Details []apperr.FieldError `json:"details,omitempty"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
	Meta  Meta      `json:"meta"`
}

var kindStatus = map[apperr.Kind]int{
	apperr.KindBadRequest:           fiber.StatusBadRequest,
	apperr.KindUnauthenticated:      fiber.StatusUnauthorized,
	apperr.KindForbidden:            fiber.StatusForbidden,
	apperr.KindNotFound:             fiber.StatusNotFound,
	apperr.KindConflict:             fiber.StatusConflict,
	apperr.KindPreconditionFailed:   fiber.StatusPreconditionFailed,
	apperr.KindPreconditionRequired: fiber.StatusPreconditionRequired,
	apperr.KindValidation:           fiber.StatusUnprocessableEntity,
	apperr.KindPayloadTooLarge:      fiber.StatusRequestEntityTooLarge,
	apperr.KindUnsupportedMediaType: fiber.StatusUnsupportedMediaType,
	apperr.KindRateLimited:          fiber.StatusTooManyRequests,
	apperr.KindUnavailable:          fiber.StatusServiceUnavailable,
}

var fiberCodes = map[int]string{
	fiber.StatusBadRequest:            "BAD_REQUEST",
	fiber.StatusUnauthorized:          "UNAUTHENTICATED",
	fiber.StatusForbidden:             "FORBIDDEN",
	fiber.StatusNotFound:              "NOT_FOUND",
	fiber.StatusMethodNotAllowed:      "METHOD_NOT_ALLOWED",
	fiber.StatusRequestEntityTooLarge: "PAYLOAD_TOO_LARGE",
	fiber.StatusUnsupportedMediaType:  "UNSUPPORTED_MEDIA_TYPE",
	fiber.StatusTooManyRequests:       "RATE_LIMITED",
	fiber.StatusRequestTimeout:        "REQUEST_TIMEOUT",
	fiber.StatusServiceUnavailable:    "SERVICE_UNAVAILABLE",
}

// ErrorHandler maps every error to the standard envelope. Internal details
// are logged with the request ID and never returned to the client.
func ErrorHandler(log *slog.Logger) fiber.ErrorHandler {
	return func(c fiber.Ctx, err error) error {
		rid := RequestID(c)
		status := fiber.StatusInternalServerError
		body := errorBody{Code: "INTERNAL_ERROR", Message: "terjadi kesalahan pada server"}

		var fe *fiber.Error
		switch e, ok := apperr.As(err); {
		case ok:
			if s, known := kindStatus[e.Kind]; known {
				status = s
				body = errorBody{Code: e.Code, Message: e.Message, Details: e.Fields}
			}
			if e.Kind == apperr.KindInternal || e.Kind == apperr.KindUnavailable {
				log.Error("request failed", "request_id", rid, "path", c.Path(), "err", err)
			}
		case errors.As(err, &fe):
			status = fe.Code
			code, known := fiberCodes[fe.Code]
			if !known {
				code = "ERROR"
			}
			body = errorBody{Code: code, Message: publicFiberMessage(fe)}
		case errors.Is(err, context.DeadlineExceeded):
			status = fiber.StatusServiceUnavailable
			body = errorBody{Code: "SERVICE_UNAVAILABLE", Message: "permintaan melebihi batas waktu"}
			log.Warn("request timeout", "request_id", rid, "path", c.Path())
		case errors.Is(err, context.Canceled):
			status = 499
			body = errorBody{Code: "CLIENT_CLOSED_REQUEST", Message: "permintaan dibatalkan"}
		default:
			log.Error("unhandled error", "request_id", rid, "method", c.Method(), "path", c.Path(), "err", err)
		}

		c.Set(fiber.HeaderCacheControl, "no-store")
		return c.Status(status).JSON(errorEnvelope{Error: body, Meta: Meta{RequestID: rid}})
	}
}

func publicFiberMessage(fe *fiber.Error) string {
	switch fe.Code {
	case fiber.StatusNotFound:
		return "endpoint tidak ditemukan"
	case fiber.StatusMethodNotAllowed:
		return "method tidak diizinkan"
	case fiber.StatusRequestEntityTooLarge:
		return "ukuran request melebihi batas"
	case fiber.StatusTooManyRequests:
		return "terlalu banyak permintaan, coba lagi nanti"
	default:
		if fe.Code >= 500 {
			return "terjadi kesalahan pada server"
		}
		return "permintaan tidak valid"
	}
}
