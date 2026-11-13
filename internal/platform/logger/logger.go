// Package logger builds the structured JSON logger with secret redaction.
package logger

import (
	"io"
	"log/slog"
	"strings"
)

// sensitiveKeys are attribute keys whose values are never written to logs.
var sensitiveKeys = map[string]struct{}{
	"authorization": {}, "password": {}, "new_password": {}, "old_password": {},
	"token": {}, "access_token": {}, "refresh_token": {}, "activation_key": {},
	"identity_number": {}, "npwp": {}, "bank_account_number": {}, "cookie": {}, "set-cookie": {},
}

// New returns a JSON logger at the given level ("debug", "info", "warn", "error").
func New(w io.Writer, level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: lvl,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if _, ok := sensitiveKeys[strings.ToLower(a.Key)]; ok {
				return slog.String(a.Key, "[REDACTED]")
			}
			return a
		},
	})
	return slog.New(h)
}
