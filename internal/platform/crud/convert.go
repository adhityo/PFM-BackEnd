package crud

import (
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/database"
)

// Conversions from validated request strings to nullable column values.
// Empty input means NULL.

func Text(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// Decimal parses a string already checked by the "decimal" validator.
func Decimal(s string) *decimal.Decimal {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	d, err := decimal.NewFromString(s)
	if err != nil {
		return nil
	}
	return &d
}

// Date parses YYYY-MM-DD already checked by the validator.
func Date(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.DateOnly, s)
	if err != nil {
		return nil
	}
	return &t
}

// Time parses an RFC 3339 timestamp already checked by the validator.
func Time(s string) *time.Time {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	t = t.UTC()
	return &t
}

// NewID returns a time-ordered UUIDv7.
func NewID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

// Affected maps an :execrows result of a conditional UPDATE: zero rows means
// the row changed since it was read.
func Affected(op string, n int64, err error) error {
	if err != nil {
		return database.MapError(op, err)
	}
	if n == 0 {
		return apperr.ErrPreconditionFailed
	}
	return nil
}

// IP parses an address already checked by the "ip" validator.
func IP(s string) *netip.Addr {
	if s == "" {
		return nil
	}
	a, err := netip.ParseAddr(s)
	if err != nil {
		return nil
	}
	return &a
}

// Prefixes parses entries already checked by "cidr_list"; a bare address
// becomes a single-host prefix.
func Prefixes(in []string) []netip.Prefix {
	if len(in) == 0 {
		return nil
	}
	out := make([]netip.Prefix, 0, len(in))
	for _, s := range in {
		if p, err := netip.ParsePrefix(strings.TrimSpace(s)); err == nil {
			out = append(out, p.Masked())
			continue
		}
		if a, err := netip.ParseAddr(strings.TrimSpace(s)); err == nil {
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
		}
	}
	return out
}

// EnumName maps a SMALLINT status to its API name; unknown codes stay numeric.
func EnumName(code *int16, names []string) *string {
	if code == nil {
		return nil
	}
	var out string
	if int(*code) >= 0 && int(*code) < len(names) {
		out = names[*code]
	} else {
		out = strconv.Itoa(int(*code))
	}
	return &out
}
