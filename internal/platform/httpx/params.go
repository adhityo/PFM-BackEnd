package httpx

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"pfm-backend/internal/apperr"
)

const (
	DefaultPerPage = 20
	MaxPerPage     = 100
)

// Page is a validated offset pagination request.
type Page struct {
	Page    int
	PerPage int
}

func (p Page) Limit() int32  { return int32(p.PerPage) }
func (p Page) Offset() int32 { return int32((p.Page - 1) * p.PerPage) }

// ListQuery is the parsed, whitelisted query string of a list endpoint.
type ListQuery struct {
	Page    Page
	Sort    string
	Filters map[string]string
}

// ParseListQuery validates page/per_page/sort and rejects any query parameter
// that is not in allowedFilters (PRD §10.1).
func ParseListQuery(c fiber.Ctx, allowedSort []string, defaultSort string, allowedFilters ...string) (ListQuery, error) {
	q := ListQuery{Page: Page{Page: 1, PerPage: DefaultPerPage}, Sort: defaultSort, Filters: map[string]string{}}
	var fieldErrs []apperr.FieldError
	for k, v := range c.Queries() {
		switch k {
		case "page":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > 1_000_000 {
				fieldErrs = append(fieldErrs, apperr.FieldError{Field: "page", Rule: "min", Message: "harus bilangan bulat >= 1"})
				continue
			}
			q.Page.Page = n
		case "per_page":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > MaxPerPage {
				fieldErrs = append(fieldErrs, apperr.FieldError{Field: "per_page", Rule: "max", Message: "harus 1-" + strconv.Itoa(MaxPerPage)})
				continue
			}
			q.Page.PerPage = n
		case "sort":
			if !slices.Contains(allowedSort, v) {
				fieldErrs = append(fieldErrs, apperr.FieldError{Field: "sort", Rule: "oneof",
					Message: "harus salah satu dari: " + strings.Join(allowedSort, ", ")})
				continue
			}
			q.Sort = v
		default:
			if !slices.Contains(allowedFilters, k) {
				fieldErrs = append(fieldErrs, apperr.FieldError{Field: k, Rule: "unknown", Message: "parameter tidak dikenal"})
				continue
			}
			if len(v) > 255 {
				fieldErrs = append(fieldErrs, apperr.FieldError{Field: k, Rule: "max", Message: "maksimal 255 karakter"})
				continue
			}
			q.Filters[k] = v
		}
	}
	if len(fieldErrs) > 0 {
		return q, apperr.Validation(fieldErrs...)
	}
	return q, nil
}

// Text returns a trimmed filter value or nil.
func (q ListQuery) Text(key string) *string {
	v := strings.TrimSpace(q.Filters[key])
	if v == "" {
		return nil
	}
	return &v
}

// Like returns the filter value escaped for ILIKE ... ESCAPE '\'.
func (q ListQuery) Like(key string) *string {
	v := q.Text(key)
	if v == nil {
		return nil
	}
	e := EscapeLike(*v)
	return &e
}

// UUID parses an optional UUID filter.
func (q ListQuery) UUID(key string) (*uuid.UUID, error) {
	v := q.Text(key)
	if v == nil {
		return nil, nil
	}
	id, err := uuid.Parse(*v)
	if err != nil {
		return nil, apperr.Invalid(key, "uuid", "harus berupa UUID yang valid")
	}
	return &id, nil
}

// Int16 parses an optional small integer filter.
func (q ListQuery) Int16(key string) (*int16, error) {
	v := q.Text(key)
	if v == nil {
		return nil, nil
	}
	n, err := strconv.ParseInt(*v, 10, 16)
	if err != nil {
		return nil, apperr.Invalid(key, "numeric", "harus bilangan bulat")
	}
	x := int16(n)
	return &x, nil
}

// Time parses an optional RFC 3339 timestamp filter.
func (q ListQuery) Time(key string) (*time.Time, error) {
	v := q.Text(key)
	if v == nil {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, *v)
	if err != nil {
		return nil, apperr.Invalid(key, "datetime", "format harus RFC 3339")
	}
	return &t, nil
}

// EscapeLike escapes \, % and _ for use in LIKE/ILIKE with ESCAPE '\'.
func EscapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// UUIDParam parses a path parameter as UUID. Invalid IDs are reported as 404
// so the API does not distinguish malformed from unknown identifiers.
func UUIDParam(c fiber.Ctx, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params(name))
	if err != nil {
		return uuid.Nil, apperr.ErrNotFound
	}
	return id, nil
}
