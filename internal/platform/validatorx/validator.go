// Package validatorx wraps go-playground/validator with project rules and
// Indonesian error messages.
package validatorx

import (
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"

	"pfm-backend/internal/apperr"
)

// Validator validates DTO structs. It satisfies fiber.StructValidator.
type Validator struct {
	v *validator.Validate
}

// New builds a validator with all custom rules registered.
func New() *Validator {
	v := validator.New(validator.WithRequiredStructEnabled())
	v.RegisterTagNameFunc(func(f reflect.StructField) string {
		name := strings.SplitN(f.Tag.Get("json"), ",", 2)[0]
		if name == "-" || name == "" {
			return f.Name
		}
		return name
	})
	registerRules(v)
	return &Validator{v: v}
}

// Validate returns an *apperr.Error (422) listing every invalid field.
func (x *Validator) Validate(out any) error {
	err := x.v.Struct(out)
	if err == nil {
		return nil
	}
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return apperr.BadRequest("input tidak dapat divalidasi", err)
	}
	fields := make([]apperr.FieldError, 0, len(verrs))
	for _, fe := range verrs {
		fields = append(fields, apperr.FieldError{
			Field:   fieldPath(fe),
			Rule:    fe.Tag(),
			Message: message(fe),
		})
	}
	return apperr.Validation(fields...)
}

// fieldPath drops the root struct name: "CreateUserRequest.role_uuid" -> "role_uuid".
func fieldPath(fe validator.FieldError) string {
	ns := fe.Namespace()
	if i := strings.IndexByte(ns, '.'); i >= 0 {
		return ns[i+1:]
	}
	return fe.Field()
}

func message(fe validator.FieldError) string {
	p := fe.Param()
	isString := fe.Kind() == reflect.String
	switch fe.Tag() {
	case "required", "required_without", "required_with":
		return "wajib diisi"
	case "max":
		if isString {
			return fmt.Sprintf("maksimal %s karakter", p)
		}
		return fmt.Sprintf("maksimal %s", p)
	case "min":
		if isString {
			return fmt.Sprintf("minimal %s karakter", p)
		}
		return fmt.Sprintf("minimal %s", p)
	case "len":
		return fmt.Sprintf("panjang harus %s", p)
	case "email":
		return "format email tidak valid"
	case "uuid", "uuid4":
		return "harus berupa UUID yang valid"
	case "oneof":
		return "harus salah satu dari: " + strings.ReplaceAll(p, " ", ", ")
	case "datetime":
		if p == "2006-01-02" {
			return "format tanggal harus YYYY-MM-DD"
		}
		return "format waktu tidak valid"
	case "numeric":
		return "hanya boleh berisi angka"
	case "bic":
		return "format SWIFT/BIC tidak valid"
	case "nik":
		return "NIK harus 16 digit"
	case "npwp":
		return "NPWP harus 15 atau 16 digit"
	case "phone_id":
		return "format nomor telepon tidak valid"
	case "decimal":
		return "format angka desimal tidak valid atau melebihi presisi " + p
	case "dgt":
		return "harus lebih besar dari " + p
	case "dgte":
		return "harus lebih besar atau sama dengan " + p
	case "dlte":
		return "harus lebih kecil atau sama dengan " + p
	case "code":
		return "hanya huruf kapital, angka, '_', '.', '-' (maks 45 karakter)"
	case "perm_code":
		return "format harus <tabel>:<aksi> dengan huruf kecil"
	case "stock_code":
		return "kode saham 1-6 huruf kapital/angka"
	case "username":
		return "3-50 karakter: huruf kecil, angka, '.', '_', '-'"
	case "safe_text":
		return "mengandung karakter yang tidak diizinkan"
	case "cidr_list":
		return "setiap elemen harus IP atau CIDR yang valid"
	case "gte":
		return "harus >= " + p
	case "lte":
		return "harus <= " + p
	case "gt":
		return "harus > " + p
	case "lt":
		return "harus < " + p
	case "unique":
		return "tidak boleh berisi duplikat"
	case "dive":
		return "elemen tidak valid"
	default:
		return "tidak valid"
	}
}
