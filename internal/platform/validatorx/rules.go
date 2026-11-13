package validatorx

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/go-playground/validator/v10"
	"github.com/shopspring/decimal"
)

var (
	reDigits16   = regexp.MustCompile(`^[0-9]{16}$`)
	reNPWP       = regexp.MustCompile(`^([0-9]{15}|[0-9]{16})$`)
	rePhoneID    = regexp.MustCompile(`^(\+62|62|0)[0-9]{8,13}$`)
	reCode       = regexp.MustCompile(`^[A-Z0-9][A-Z0-9_.-]{0,44}$`)
	rePermCode   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,99}:[a-z][a-z0-9_]{0,49}$`)
	reStockCode  = regexp.MustCompile(`^[A-Z0-9]{1,6}$`)
	reUsername   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,49}$`)
	reDecimalStr = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)
)

func registerRules(v *validator.Validate) {
	mustRegister(v, "nik", regexRule(reDigits16))
	mustRegister(v, "npwp", regexRule(reNPWP))
	mustRegister(v, "phone_id", regexRule(rePhoneID))
	mustRegister(v, "code", regexRule(reCode))
	mustRegister(v, "perm_code", regexRule(rePermCode))
	mustRegister(v, "stock_code", regexRule(reStockCode))
	mustRegister(v, "username", regexRule(reUsername))
	mustRegister(v, "safe_text", safeText)
	mustRegister(v, "decimal", decimalPrecision)
	mustRegister(v, "dgt", decimalCompare(func(c int) bool { return c > 0 }))
	mustRegister(v, "dgte", decimalCompare(func(c int) bool { return c >= 0 }))
	mustRegister(v, "dlte", decimalCompare(func(c int) bool { return c <= 0 }))
	mustRegister(v, "cidr_list", cidrList)
}

func mustRegister(v *validator.Validate, tag string, fn validator.Func) {
	if err := v.RegisterValidation(tag, fn); err != nil {
		panic(err)
	}
}

func regexRule(re *regexp.Regexp) validator.Func {
	return func(fl validator.FieldLevel) bool {
		return re.MatchString(fl.Field().String())
	}
}

// safeText rejects invalid UTF-8 and control characters other than \n, \r, \t.
func safeText(fl validator.FieldLevel) bool {
	s := fl.Field().String()
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if unicode.IsControl(r) || r == ' ' || r == ' ' {
			return false
		}
	}
	return true
}

// decimalPrecision validates a decimal string against NUMERIC(P,S): tag "decimal=20.4".
func decimalPrecision(fl validator.FieldLevel) bool {
	s := fl.Field().String()
	if !reDecimalStr.MatchString(s) {
		return false
	}
	precision, scale, ok := parsePS(fl.Param())
	if !ok {
		return false
	}
	s = strings.TrimPrefix(s, "-")
	intPart, frac, _ := strings.Cut(s, ".")
	intPart = strings.TrimLeft(intPart, "0")
	if len(frac) > scale {
		return false
	}
	return len(intPart) <= precision-scale
}

func parsePS(param string) (int, int, bool) {
	p, s, found := strings.Cut(param, ".")
	if !found {
		return 0, 0, false
	}
	precision, err1 := strconv.Atoi(p)
	scale, err2 := strconv.Atoi(s)
	if err1 != nil || err2 != nil || scale > precision {
		return 0, 0, false
	}
	return precision, scale, true
}

func decimalCompare(ok func(cmp int) bool) validator.Func {
	return func(fl validator.FieldLevel) bool {
		val, err := decimal.NewFromString(fl.Field().String())
		if err != nil {
			return false
		}
		ref, err := decimal.NewFromString(fl.Param())
		if err != nil {
			return false
		}
		return ok(val.Cmp(ref))
	}
}

func cidrList(fl validator.FieldLevel) bool {
	f := fl.Field()
	for i := 0; i < f.Len(); i++ {
		s := f.Index(i).String()
		if _, err := netip.ParsePrefix(s); err == nil {
			continue
		}
		if _, err := netip.ParseAddr(s); err != nil {
			return false
		}
	}
	return true
}
