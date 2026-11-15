package dto

import "strings"

// DecisionRequest carries an optional note for approve/cancel/reject actions.
type DecisionRequest struct {
	Note string `json:"note" validate:"omitempty,max=255,safe_text"`
}

func (r *DecisionRequest) Normalize() { r.Note = strings.TrimSpace(r.Note) }

// CouponPaymentRequest pays every unpaid investor coupon of a schedule.
type CouponPaymentRequest struct {
	SettlementDate string `json:"settlement_date" validate:"required,datetime=2006-01-02"`
}

// InvestorCouponPaymentRequest pays one investor coupon with proof of transfer.
type InvestorCouponPaymentRequest struct {
	PaymentSettlementDate   string `json:"payment_settlement_date"    validate:"required,datetime=2006-01-02"`
	PaymentReferenceNumber  string `json:"payment_reference_number"   validate:"omitempty,max=255,safe_text"`
	PaymentDocumentFileUUID string `json:"payment_document_file_uuid" validate:"required,uuid"`
}

// AgentFeePaymentRequest settles an agent fee.
type AgentFeePaymentRequest struct {
	PaymentDate  string `json:"payment_date"   validate:"required,datetime=2006-01-02"`
	PaidValue    string `json:"paid_value"     validate:"required,decimal=20.4,dgt=0"`
	TaxPaidValue string `json:"tax_paid_value" validate:"omitempty,decimal=20.4,dgte=0"`
}

// MarketingAgentChangeRequest is a full replacement of the agent's personal
// data, applied after approval (the leader tree is changed by ADMIN only).
type MarketingAgentChangeRequest struct {
	MarketingAgentName string `json:"marketing_agent_name" validate:"required,max=255,safe_text"`
	Status             *int16 `json:"status" validate:"omitempty,min=0"`
	Description        string `json:"description" validate:"omitempty,max=255,safe_text"`
	Location           string `json:"location" validate:"omitempty,max=255,safe_text"`
	NPWP               string `json:"npwp" validate:"omitempty,max=255,safe_text,npwp"`
	Address            string `json:"address" validate:"omitempty,max=255,safe_text"`
	Birthdate          string `json:"birthdate" validate:"omitempty,datetime=2006-01-02"`
	Email              string `json:"email" validate:"omitempty,max=255,safe_text,email"`
	Handphone          string `json:"handphone" validate:"omitempty,max=255,safe_text,phone_id"`
	IdentityNumber     string `json:"identity_number" validate:"omitempty,max=255,safe_text"`
	Telephone          string `json:"telephone" validate:"omitempty,max=255,safe_text"`
	Bank               string `json:"bank" validate:"omitempty,max=255,safe_text"`
	BankAccountName    string `json:"bank_account_name" validate:"omitempty,max=255,safe_text"`
	BankAccountNumber  string `json:"bank_account_number" validate:"omitempty,max=255,safe_text"`
	BankBranch         string `json:"bank_branch" validate:"omitempty,max=255,safe_text"`
}

func (r *MarketingAgentChangeRequest) Normalize() {
	for _, f := range []*string{&r.MarketingAgentName, &r.Description, &r.Location, &r.NPWP, &r.Address, &r.Birthdate,
		&r.Email, &r.Handphone, &r.IdentityNumber, &r.Telephone, &r.Bank, &r.BankAccountName, &r.BankAccountNumber,
		&r.BankBranch} {
		*f = strings.TrimSpace(*f)
	}
}
