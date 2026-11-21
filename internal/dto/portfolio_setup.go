package dto

import "strings"

// PortfolioSetupCashAccount is the cash account of one trade type
// (portfolio_accounting_structure).
type PortfolioSetupCashAccount struct {
	TradeTypeUUID      string `json:"trade_type_uuid"      validate:"required,uuid"`
	FinanceAccountUUID string `json:"finance_account_uuid" validate:"required,uuid"`
	Description        string `json:"description"          validate:"omitempty,max=10000,safe_text"`
}

// PortfolioSetupAssetClass maps the accounts of one asset class
// (asset_class_finance_account) and its cash accounts per trade type.
type PortfolioSetupAssetClass struct {
	AssetClassUUID            string                      `json:"asset_class_uuid"             validate:"required,uuid"`
	BuyAccountUUID            string                      `json:"buy_account_uuid"             validate:"omitempty,uuid"`
	SellAccountUUID           string                      `json:"sell_account_uuid"            validate:"omitempty,uuid"`
	CapitalGainAccountUUID    string                      `json:"capital_gain_account_uuid"    validate:"omitempty,uuid"`
	CapitalLossAccountUUID    string                      `json:"capital_loss_account_uuid"    validate:"omitempty,uuid"`
	InterestCouponAccountUUID string                      `json:"interest_coupon_account_uuid" validate:"omitempty,uuid"`
	CashAccounts              []PortfolioSetupCashAccount `json:"cash_accounts"                validate:"max=20,unique=TradeTypeUUID,dive"`
}

// PortfolioSetupBank is a bank account of the portfolio (portfolio_bank).
type PortfolioSetupBank struct {
	BankUUID       string `json:"bank_uuid"        validate:"required,uuid"`
	AccountNumber  string `json:"account_number"   validate:"required,max=50,safe_text"`
	AccountName    string `json:"account_name"     validate:"required,max=255,safe_text"`
	BankBranchName string `json:"bank_branch_name" validate:"omitempty,max=255,safe_text"`
	Description    string `json:"description"      validate:"omitempty,max=10000,safe_text"`
	CurrencyTypeID *int64 `json:"currency_type_id" validate:"omitempty,min=0"`
}

// PortfolioSetupRequest creates a portfolio with its account mapping and bank
// accounts in one transaction (POST /portfolios/setup). Item tags repeat the
// rules of their CRUD requests (for the docs and early errors); the service
// validates the built CRUD requests again, so those rules stay authoritative.
type PortfolioSetupRequest struct {
	Portfolio    PortfolioRequest           `json:"portfolio"     validate:"required"`
	AssetClasses []PortfolioSetupAssetClass `json:"asset_classes" validate:"max=20,unique=AssetClassUUID,dive"`
	Banks        []PortfolioSetupBank       `json:"banks"         validate:"max=20,dive"`
}

func (r *PortfolioSetupRequest) Normalize() {
	r.Portfolio.Normalize()
	for i := range r.AssetClasses {
		a := &r.AssetClasses[i]
		a.AssetClassUUID = strings.TrimSpace(a.AssetClassUUID)
		for j := range a.CashAccounts {
			a.CashAccounts[j].TradeTypeUUID = strings.TrimSpace(a.CashAccounts[j].TradeTypeUUID)
		}
	}
}

// Mapping is the asset-class-finance-accounts request of this item.
func (a PortfolioSetupAssetClass) Mapping(portfolioUUID string) AssetClassFinanceAccountRequest {
	r := AssetClassFinanceAccountRequest{AssetClassUUID: a.AssetClassUUID, PortfolioUUID: portfolioUUID,
		BuyAccountUUID: a.BuyAccountUUID, SellAccountUUID: a.SellAccountUUID, CapitalGainAccountUUID: a.CapitalGainAccountUUID,
		CapitalLossAccountUUID: a.CapitalLossAccountUUID, InterestCouponAccountUUID: a.InterestCouponAccountUUID}
	r.Normalize()
	return r
}

// Structure is the portfolio-accounting-structures request of this cash account.
func (c PortfolioSetupCashAccount) Structure(portfolioUUID, assetClassUUID string) PortfolioAccountingStructureRequest {
	r := PortfolioAccountingStructureRequest{PortfolioUUID: portfolioUUID, AssetClassUUID: assetClassUUID,
		TradeTypeUUID: c.TradeTypeUUID, FinanceAccountUUID: c.FinanceAccountUUID, Description: c.Description}
	r.Normalize()
	return r
}

// Request is the portfolio-banks request of this bank account.
func (b PortfolioSetupBank) Request(portfolioUUID string) PortfolioBankRequest {
	r := PortfolioBankRequest{PortfolioUUID: portfolioUUID, BankUUID: b.BankUUID, AccountNumber: b.AccountNumber,
		AccountName: b.AccountName, BankBranchName: b.BankBranchName, Description: b.Description, CurrencyTypeID: b.CurrencyTypeID}
	r.Normalize()
	return r
}

// PortfolioSetupResponse lists everything POST /portfolios/setup created.
type PortfolioSetupResponse struct {
	Portfolio                     PortfolioResponse                      `json:"portfolio"`
	AssetClassFinanceAccounts     []AssetClassFinanceAccountResponse     `json:"asset_class_finance_accounts"`
	PortfolioAccountingStructures []PortfolioAccountingStructureResponse `json:"portfolio_accounting_structures"`
	PortfolioBanks                []PortfolioBankResponse                `json:"portfolio_banks"`
}
