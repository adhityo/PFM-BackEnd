package dto

// PeriodRequest selects a month (benefit payout generation).
type PeriodRequest struct {
	Year  int32 `json:"year"  validate:"required,min=2000,max=2100"`
	Month int32 `json:"month" validate:"required,min=1,max=12"`
}

// InvoiceGenerateRequest generates (or completes) a client's monthly invoice.
type InvoiceGenerateRequest struct {
	ClientUUID string `json:"client_uuid" validate:"required,uuid"`
	Year       int32  `json:"year"        validate:"required,min=2000,max=2100"`
	Month      int32  `json:"month"       validate:"required,min=1,max=12"`
}

// PremiumPaymentRequest records a member's premium payment; paid_nominal
// defaults to the billed amount.
type PremiumPaymentRequest struct {
	PaymentDate string `json:"payment_date" validate:"required,datetime=2006-01-02"`
	PaidNominal string `json:"paid_nominal" validate:"omitempty,decimal=20.4,dgt=0"`
}

// PayoutPaymentRequest marks an approved benefit payout batch as paid.
type PayoutPaymentRequest struct {
	PayoutDate string `json:"payout_date" validate:"required,datetime=2006-01-02"`
}
