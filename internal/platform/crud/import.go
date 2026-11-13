package crud

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"

	"pfm-backend/internal/apperr"
	"pfm-backend/internal/platform/httpx"
)

// CSV import limits (PRD §10.4 TIMESERIES).
const (
	MaxImportRows  = 10000
	MaxImportBytes = 5 << 20
	maxRowErrors   = 50
)

// parseCSV turns a CSV whose header row holds the JSON field names of Req into
// validated requests. Every row is checked before anything is written; errors
// are reported as rows[<line>].<field>.
func parseCSV[Req any](body []byte, v httpx.Validator) ([]Req, error) {
	if len(body) > MaxImportBytes {
		return nil, apperr.PayloadTooLarge("file CSV maksimal 5 MB")
	}
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf")) // UTF-8 BOM from spreadsheets
	r := csv.NewReader(bytes.NewReader(body))
	r.FieldsPerRecord = 0
	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, apperr.Invalid("file", "required", "CSV kosong")
	}
	if err != nil {
		return nil, apperr.BadRequest("CSV tidak valid", err)
	}
	kinds, err := fieldKinds[Req](header)
	if err != nil {
		return nil, err
	}

	var (
		out    []Req
		fields []apperr.FieldError
	)
	for line := 2; ; line++ {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, apperr.BadRequest(fmt.Sprintf("CSV tidak valid di baris %d", line), err)
		}
		if len(out) >= MaxImportRows {
			return nil, apperr.PayloadTooLarge(fmt.Sprintf("maksimal %d baris per import", MaxImportRows))
		}
		req, err := decodeRow[Req](header, kinds, rec)
		if err == nil {
			if n, ok := any(&req).(httpx.Normalizer); ok {
				n.Normalize()
			}
			err = v.Validate(&req)
		}
		if err != nil {
			fields = append(fields, prefixFields(line, err)...)
			if len(fields) >= maxRowErrors {
				break
			}
			continue
		}
		out = append(out, req)
	}
	if len(fields) > 0 {
		return nil, apperr.Validation(fields...)
	}
	if len(out) == 0 {
		return nil, apperr.Invalid("file", "required", "CSV tidak memiliki baris data")
	}
	return out, nil
}

// fieldKinds maps each CSV column to the JSON kind of the matching Req field.
func fieldKinds[Req any](header []string) ([]reflect.Kind, error) {
	t := reflect.TypeOf((*Req)(nil)).Elem()
	byJSON := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
		if name != "" && name != "-" {
			byJSON[name] = f.Type
		}
	}
	kinds := make([]reflect.Kind, len(header))
	seen := map[string]bool{}
	for i, h := range header {
		h = strings.TrimSpace(h)
		header[i] = h
		ft, ok := byJSON[h]
		if !ok {
			return nil, apperr.Invalid(h, "unknown", "kolom CSV tidak dikenal")
		}
		if seen[h] {
			return nil, apperr.Invalid(h, "unique", "kolom CSV duplikat")
		}
		seen[h] = true
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		kinds[i] = ft.Kind()
	}
	return kinds, nil
}

// decodeRow builds a JSON object with properly typed values and decodes it
// strictly into Req, so CSV and JSON input follow the same rules.
func decodeRow[Req any](header []string, kinds []reflect.Kind, rec []string) (Req, error) {
	var req Req
	if len(rec) != len(header) {
		return req, apperr.Invalid("row", "columns", fmt.Sprintf("jumlah kolom %d, seharusnya %d", len(rec), len(header)))
	}
	obj := make(map[string]any, len(header))
	for i, raw := range rec {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		switch kinds[i] {
		case reflect.Int, reflect.Int16, reflect.Int32, reflect.Int64:
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return req, apperr.Invalid(header[i], "numeric", "harus bilangan bulat")
			}
			obj[header[i]] = n
		case reflect.Bool:
			b, err := strconv.ParseBool(raw)
			if err != nil {
				return req, apperr.Invalid(header[i], "boolean", "harus true/false")
			}
			obj[header[i]] = b
		case reflect.Slice:
			obj[header[i]] = strings.Split(raw, ";")
		default:
			obj[header[i]] = raw
		}
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return req, err
	}
	return req, httpx.StrictJSONDecode(b, &req)
}

// prefixFields rewrites field errors as rows[<line>].<field>.
func prefixFields(line int, err error) []apperr.FieldError {
	prefix := fmt.Sprintf("rows[%d]", line)
	e, ok := apperr.As(err)
	if !ok {
		return []apperr.FieldError{{Field: prefix, Rule: "invalid", Message: "baris tidak valid"}}
	}
	if len(e.Fields) == 0 {
		return []apperr.FieldError{{Field: prefix, Rule: "invalid", Message: e.Message}}
	}
	out := make([]apperr.FieldError, 0, len(e.Fields))
	for _, f := range e.Fields {
		f.Field = prefix + "." + f.Field
		out = append(out, f)
	}
	return out
}

// rowError attributes a write-time error (FK, uniqueness, hook) to its row.
func rowError(index int, err error) error {
	e, ok := apperr.As(err)
	if !ok {
		return err
	}
	line := index + 2 // header is line 1
	switch e.Kind {
	case apperr.KindValidation:
		return apperr.Validation(prefixFields(line, err)...)
	case apperr.KindConflict:
		return apperr.Conflict(fmt.Sprintf("baris %d: %s", line, e.Message))
	default:
		return err
	}
}
