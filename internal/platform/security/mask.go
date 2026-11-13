package security

import "strings"

// MaskMiddle keeps the first `head` and last `tail` runes and masks the rest,
// e.g. MaskMiddle("3271010101010001", 4, 4) = "3271********0001".
func MaskMiddle(s string, head, tail int) string {
	r := []rune(s)
	if len(r) <= head+tail {
		return strings.Repeat("*", len(r))
	}
	return string(r[:head]) + strings.Repeat("*", len(r)-head-tail) + string(r[len(r)-tail:])
}

// MaskPII masks a PII value unless canRead is true. nil stays nil.
func MaskPII(v *string, canRead bool) *string {
	if v == nil || canRead {
		return v
	}
	m := MaskMiddle(*v, 4, 4)
	return &m
}
