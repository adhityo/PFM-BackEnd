package dto

import (
	"strings"
	"time"

	"github.com/google/uuid"

	"pfm-backend/internal/db/sqlc"
)

// ConfigurationRequest is the PUT body of the configuration singleton.
// Counters and legacy filesystem paths are intentionally absent.
type ConfigurationRequest struct {
	SystemUsageType           *int16 `json:"system_usage_type"           validate:"omitempty,min=0"`
	CompanyName               string `json:"company_name"                validate:"required,max=255,safe_text"`
	CompanyAddress            string `json:"company_address"             validate:"omitempty,max=10000,safe_text"`
	DefaultProrateDays        *int32 `json:"default_prorate_days"        validate:"omitempty,min=0,max=366"`
	AgentNumberTemplate       string `json:"agent_number_template"       validate:"omitempty,max=255,number_template"`
	CertificateNumberTemplate string `json:"certificate_number_template" validate:"omitempty,max=255,number_template"`
	InvestorNumberTemplate    string `json:"investor_number_template"    validate:"omitempty,max=255,number_template"`
	EmitentCode               string `json:"emitent_code"                validate:"omitempty,max=255,code"`
	ProxyAPIURL               string `json:"proxy_api_url"               validate:"omitempty,max=255,https_url"`
	IsUsingQRLogo             bool   `json:"is_using_qr_logo"`
	QRLogoFileUUID            string `json:"qr_logo_file_uuid"           validate:"required_if=IsUsingQRLogo true,omitempty,uuid"`
	IsUsingAgreement          bool   `json:"is_using_agreement"`
}

func (r *ConfigurationRequest) Normalize() {
	for _, s := range []*string{&r.CompanyName, &r.CompanyAddress, &r.AgentNumberTemplate, &r.CertificateNumberTemplate,
		&r.InvestorNumberTemplate, &r.EmitentCode, &r.ProxyAPIURL, &r.QRLogoFileUUID} {
		*s = strings.TrimSpace(*s)
	}
}

// ConfigurationResponse is the singleton as returned by the API.
type ConfigurationResponse struct {
	UUID                        uuid.UUID  `json:"uuid"`
	SystemUsageType             *int16     `json:"system_usage_type"`
	CompanyName                 *string    `json:"company_name"`
	CompanyAddress              *string    `json:"company_address"`
	DefaultProrateDays          *int32     `json:"default_prorate_days"`
	AgentNumberTemplate         *string    `json:"agent_number_template"`
	CertificateNumberTemplate   *string    `json:"certificate_number_template"`
	InvestorNumberTemplate      *string    `json:"investor_number_template"`
	CertificateLatestCounter    *int32     `json:"certificate_latest_counter"`
	LatestAgentCounter          *int32     `json:"latest_agent_counter"`
	LatestInvestorCounter       *int32     `json:"latest_investor_counter"`
	LatestAgreementCounter      *int32     `json:"latest_agreement_counter"`
	LatestGeneratedInvoiceMonth *int32     `json:"latest_generated_invoice_month"`
	LatestGeneratedInvoiceYear  *int32     `json:"latest_generated_invoice_year"`
	LatestGeneratedPaymentMonth *int32     `json:"latest_generated_payment_month"`
	LatestGeneratedPaymentYear  *int32     `json:"latest_generated_payment_year"`
	EmitentCode                 *string    `json:"emitent_code"`
	ProxyAPIURL                 *string    `json:"proxy_api_url"`
	IsUsingQRLogo               bool       `json:"is_using_qr_logo"`
	QRLogoFileUUID              *uuid.UUID `json:"qr_logo_file_uuid"`
	IsUsingAgreement            bool       `json:"is_using_agreement"`
	UpdatedAt                   time.Time  `json:"updated_at"`
}

func ConfigurationFromRow(r sqlc.GetConfigurationRow) ConfigurationResponse {
	return ConfigurationResponse{UUID: r.Uuid, SystemUsageType: r.SystemUsageType, CompanyName: r.CompanyName,
		CompanyAddress: r.CompanyAddress, DefaultProrateDays: r.DefaultProrateDays, AgentNumberTemplate: r.AgentNumberTemplate,
		CertificateNumberTemplate: r.CertificateNumberTemplate, InvestorNumberTemplate: r.InvestorNumberTemplate,
		CertificateLatestCounter: r.CertificateLatestCounter, LatestAgentCounter: r.LatestAgentCounter,
		LatestInvestorCounter: r.LatestInvestorCounter, LatestAgreementCounter: r.LatestAgreementCounter,
		LatestGeneratedInvoiceMonth: r.LatestGeneratedInvoiceMonth, LatestGeneratedInvoiceYear: r.LatestGeneratedInvoiceYear,
		LatestGeneratedPaymentMonth: r.LatestGeneratedPaymentMonth, LatestGeneratedPaymentYear: r.LatestGeneratedPaymentYear,
		EmitentCode: r.EmitentCode, ProxyAPIURL: r.ProxyApiUrl, IsUsingQRLogo: r.IsUsingQrLogo, QRLogoFileUUID: r.QrLogoFileUuid,
		IsUsingAgreement: r.IsUsingAgreement, UpdatedAt: r.UpdatedAt}
}
