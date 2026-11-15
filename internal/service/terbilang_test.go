package service

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestTerbilang(t *testing.T) {
	cases := map[string]string{
		"0":             "Nol Rupiah",
		"1":             "Satu Rupiah",
		"10":            "Sepuluh Rupiah",
		"11":            "Sebelas Rupiah",
		"15":            "Lima Belas Rupiah",
		"21":            "Dua Puluh Satu Rupiah",
		"100":           "Seratus Rupiah",
		"110":           "Seratus Sepuluh Rupiah",
		"999":           "Sembilan Ratus Sembilan Puluh Sembilan Rupiah",
		"1000":          "Seribu Rupiah",
		"1001":          "Seribu Satu Rupiah",
		"1100":          "Seribu Seratus Rupiah",
		"21000":         "Dua Puluh Satu Ribu Rupiah",
		"1000000":       "Satu Juta Rupiah",
		"1500000":       "Satu Juta Lima Ratus Ribu Rupiah",
		"2000001000":    "Dua Miliar Seribu Rupiah",
		"1250000.4999":  "Satu Juta Dua Ratus Lima Puluh Ribu Rupiah",
		"3000000000000": "Tiga Triliun Rupiah",
		"-5000":         "Minus Lima Ribu Rupiah",
	}
	for in, want := range cases {
		if got := Terbilang(decimal.RequireFromString(in)); got != want {
			t.Errorf("Terbilang(%s) = %q, want %q", in, got, want)
		}
	}
}
