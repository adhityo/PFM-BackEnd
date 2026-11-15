package service

import (
	"strings"

	"github.com/shopspring/decimal"
)

var (
	digitWords = []string{"", "Satu", "Dua", "Tiga", "Empat", "Lima", "Enam", "Tujuh", "Delapan", "Sembilan"}
	scaleWords = []string{"", "Ribu", "Juta", "Miliar", "Triliun", "Kuadriliun"}
)

// Terbilang spells a rupiah amount in Indonesian (rounded to whole rupiah),
// as stored in invoice.billing_description and *.value_description.
func Terbilang(amount decimal.Decimal) string {
	n := amount.Round(0).Abs().BigInt()
	if n.Sign() == 0 {
		return "Nol Rupiah"
	}
	digits := n.String()
	if len(digits) > 3*len(scaleWords) {
		return digits + " Rupiah"
	}
	var groups []int
	for len(digits) > 0 {
		cut := max(len(digits)-3, 0)
		g := 0
		for _, c := range digits[cut:] {
			g = g*10 + int(c-'0')
		}
		groups = append(groups, g)
		digits = digits[:cut]
	}
	var words []string
	for i := len(groups) - 1; i >= 0; i-- {
		g := groups[i]
		switch {
		case g == 0:
			continue
		case g == 1 && i == 1:
			words = append(words, "Seribu")
		default:
			words = append(words, hundreds(g))
			if scaleWords[i] != "" {
				words = append(words, scaleWords[i])
			}
		}
	}
	out := strings.Join(words, " ") + " Rupiah"
	if amount.IsNegative() {
		out = "Minus " + out
	}
	return out
}

// hundreds spells 1..999.
func hundreds(n int) string {
	var w []string
	switch h := n / 100; {
	case h == 1:
		w = append(w, "Seratus")
	case h > 1:
		w = append(w, digitWords[h], "Ratus")
	}
	switch r := n % 100; {
	case r == 0:
	case r < 10:
		w = append(w, digitWords[r])
	case r == 10:
		w = append(w, "Sepuluh")
	case r == 11:
		w = append(w, "Sebelas")
	case r < 20:
		w = append(w, digitWords[r-10], "Belas")
	default:
		w = append(w, digitWords[r/10], "Puluh")
		if r%10 != 0 {
			w = append(w, digitWords[r%10])
		}
	}
	return strings.Join(w, " ")
}
