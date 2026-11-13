package validatorx

import (
	"testing"

	"pfm-backend/internal/apperr"
)

type sample struct {
	Name  string   `json:"name"    validate:"required,max=10,safe_text"`
	NIK   string   `json:"nik"     validate:"omitempty,nik"`
	NPWP  string   `json:"npwp"    validate:"omitempty,npwp"`
	Phone string   `json:"phone"   validate:"omitempty,phone_id"`
	Price string   `json:"price"   validate:"omitempty,decimal=20.4,dgt=0"`
	Rate  string   `json:"rate"    validate:"omitempty,decimal=12.6,dgte=0,dlte=100"`
	Code  string   `json:"code"    validate:"omitempty,code"`
	Perm  string   `json:"perm"    validate:"omitempty,perm_code"`
	User  string   `json:"user"    validate:"omitempty,username"`
	IPs   []string `json:"ips"     validate:"omitempty,cidr_list"`
	Ref   string   `json:"ref"     validate:"omitempty,uuid"`
}

func TestValidatorRules(t *testing.T) {
	v := New()
	tests := []struct {
		name      string
		in        sample
		wantField string
	}{
		{"valid", sample{Name: "ok", NIK: "3271010101010001", NPWP: "012345678901234", Phone: "+6281234567890",
			Price: "1500000.1234", Rate: "6.5", Code: "CLIENT-ADMIN", Perm: "bond_trade:approve", User: "admin-riscon",
			IPs: []string{"10.0.0.0/8", "192.168.1.10"}, Ref: "0193a3c4-5b6d-7e8f-9a0b-1c2d3e4f5a6b"}, ""},
		{"required", sample{}, "name"},
		{"control char", sample{Name: "a\x00b"}, "name"},
		{"nik length", sample{Name: "x", NIK: "123"}, "nik"},
		{"npwp length", sample{Name: "x", NPWP: "1234567890123"}, "npwp"},
		{"phone", sample{Name: "x", Phone: "12345"}, "phone"},
		{"price scale", sample{Name: "x", Price: "1.12345"}, "price"},
		{"price precision", sample{Name: "x", Price: "1234567890123456789"}, "price"},
		{"price zero", sample{Name: "x", Price: "0"}, "price"},
		{"price not number", sample{Name: "x", Price: "1e5"}, "price"},
		{"rate over 100", sample{Name: "x", Rate: "100.5"}, "rate"},
		{"code lowercase", sample{Name: "x", Code: "admin"}, "code"},
		{"perm format", sample{Name: "x", Perm: "BondTrade.approve"}, "perm"},
		{"username upper", sample{Name: "x", User: "Admin"}, "user"},
		{"cidr", sample{Name: "x", IPs: []string{"10.0.0.0/33"}}, "ips"},
		{"uuid", sample{Name: "x", Ref: "1"}, "ref"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := v.Validate(&tt.in)
			if tt.wantField == "" {
				if err != nil {
					t.Fatalf("expected valid, got %v", err)
				}
				return
			}
			e, ok := apperr.As(err)
			if !ok || e.Kind != apperr.KindValidation {
				t.Fatalf("expected validation error, got %v", err)
			}
			for _, f := range e.Fields {
				if f.Field == tt.wantField {
					return
				}
			}
			t.Fatalf("expected error on %q, got %+v", tt.wantField, e.Fields)
		})
	}
}
