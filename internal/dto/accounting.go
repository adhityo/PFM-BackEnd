package dto

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"pfm-backend/internal/db/sqlc"
	"pfm-backend/internal/platform/crud"
	"pfm-backend/internal/platform/httpx"
)

// JournalStatus names finance_transaction.transaction_status codes.
var JournalStatus = []string{"draft", "posted", "cancelled", "reversed"}

// JournalLineRequest is one line of a manual journal: exactly one of debit/credit.
type JournalLineRequest struct {
	FinanceAccountUUID string `json:"finance_account_uuid" validate:"required,uuid"`
	Debit              string `json:"debit"                validate:"omitempty,decimal=20.4,dgte=0"`
	Credit             string `json:"credit"               validate:"omitempty,decimal=20.4,dgte=0"`
	Description        string `json:"description"          validate:"omitempty,max=255,safe_text"`
}

// JournalRequest creates a posted manual journal.
type JournalRequest struct {
	TransactionDate string               `json:"transaction_date" validate:"required,datetime=2006-01-02"`
	Description     string               `json:"description"      validate:"omitempty,max=10000,safe_text"`
	PortfolioUUID   string               `json:"portfolio_uuid"   validate:"omitempty,uuid"`
	Lines           []JournalLineRequest `json:"lines"            validate:"required,min=2,max=500,dive"`
}

func (r *JournalRequest) Normalize() {
	r.Description = strings.TrimSpace(r.Description)
	for i := range r.Lines {
		r.Lines[i].Debit = strings.TrimSpace(r.Lines[i].Debit)
		r.Lines[i].Credit = strings.TrimSpace(r.Lines[i].Credit)
		r.Lines[i].Description = strings.TrimSpace(r.Lines[i].Description)
	}
}

type JournalLineResponse struct {
	UUID                 uuid.UUID        `json:"uuid"`
	FinanceAccountUUID   uuid.UUID        `json:"finance_account_uuid"`
	FinanceAccountNumber *string          `json:"finance_account_number"`
	FinanceAccountName   *string          `json:"finance_account_name"`
	Debit                *decimal.Decimal `json:"debit"`
	Credit               *decimal.Decimal `json:"credit"`
	Description          *string          `json:"description"`
}

type JournalResponse struct {
	UUID                uuid.UUID             `json:"uuid"`
	TransactionDate     *httpx.Date           `json:"transaction_date"`
	TransactionNumber   *string               `json:"transaction_number"`
	TransactionValue    *decimal.Decimal      `json:"transaction_value"`
	Description         *string               `json:"description"`
	Status              *string               `json:"status"`
	PortfolioUUID       *uuid.UUID            `json:"portfolio_uuid"`
	StockTradeUUID      *uuid.UUID            `json:"stock_trade_uuid"`
	MutualFundTradeUUID *uuid.UUID            `json:"mutual_fund_trade_uuid"`
	CryptoTradeUUID     *uuid.UUID            `json:"crypto_trade_uuid"`
	CashSettlementUUID  *uuid.UUID            `json:"cash_settlement_uuid"`
	ReversalOfUUID      *uuid.UUID            `json:"reversal_of_uuid"`
	Lines               []JournalLineResponse `json:"lines"`
	CreatedAt           time.Time             `json:"created_at"`
	UpdatedAt           time.Time             `json:"updated_at"`
}

func JournalFromRows(h sqlc.GetJournalRow, lines []sqlc.ListJournalLinesRow) JournalResponse {
	out := JournalResponse{UUID: h.Uuid, TransactionDate: httpx.DateOf(h.TransactionDate), TransactionNumber: h.TransactionNumber,
		TransactionValue: h.TransactionValue, Description: h.Description, Status: crud.EnumName(h.TransactionStatus, JournalStatus),
		PortfolioUUID: h.PortfolioUuid, StockTradeUUID: h.StockTradeUuid, MutualFundTradeUUID: h.MutualFundTradeUuid,
		CryptoTradeUUID: h.CryptoTradeUuid, CashSettlementUUID: h.CashSettlementUuid, ReversalOfUUID: h.ReversalOfUuid,
		Lines: make([]JournalLineResponse, 0, len(lines)), CreatedAt: h.CreatedAt, UpdatedAt: h.UpdatedAt}
	for _, l := range lines {
		out.Lines = append(out.Lines, JournalLineResponse{UUID: l.Uuid, FinanceAccountUUID: l.FinanceAccountUuid,
			FinanceAccountNumber: l.FinanceAccountNumber, FinanceAccountName: l.FinanceAccountName, Debit: l.Debit,
			Credit: l.Credit, Description: l.Description})
	}
	return out
}

type JournalSummaryResponse struct {
	UUID              uuid.UUID        `json:"uuid"`
	TransactionDate   *httpx.Date      `json:"transaction_date"`
	TransactionNumber *string          `json:"transaction_number"`
	TransactionValue  *decimal.Decimal `json:"transaction_value"`
	Description       *string          `json:"description"`
	Status            *string          `json:"status"`
	PortfolioUUID     *uuid.UUID       `json:"portfolio_uuid"`
	ReversalOfUUID    *uuid.UUID       `json:"reversal_of_uuid"`
	CreatedAt         time.Time        `json:"created_at"`
}

func JournalSummaryFromRow(r sqlc.ListJournalsRow) JournalSummaryResponse {
	return JournalSummaryResponse{UUID: r.Uuid, TransactionDate: httpx.DateOf(r.TransactionDate), TransactionNumber: r.TransactionNumber,
		TransactionValue: r.TransactionValue, Description: r.Description, Status: crud.EnumName(r.TransactionStatus, JournalStatus),
		PortfolioUUID: r.PortfolioUuid, ReversalOfUUID: r.ReversalOfUuid, CreatedAt: r.CreatedAt}
}
