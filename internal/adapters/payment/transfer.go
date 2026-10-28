package payment

import "context"

// TransferNo is a platform-generated immutable identifier shared with the
// provider, never an unscoped merchant business number. Amount is integer cents.
type TransferRequest struct {
	TransferNo, Type, Account, Name, Title, Remark, BankCode string
	Amount                                                   int64
	// WeChat 商家转账（新）: scene id and report background, exactly the original
	// transfer_wxpay_scene_id / transfer_wxpay_info_type / _content settings
	// ('|'-separated). Kept as text so the request stays comparable. These are
	// json omitempty so an ordinary transfer serializes (and therefore hashes
	// and encrypts) byte-for-byte as before these fields existed.
	SceneID        string `json:"SceneID,omitempty"`
	ReportTypes    string `json:"ReportTypes,omitempty"`
	ReportContents string `json:"ReportContents,omitempty"`
}
type TransferResult struct {
	AlipayBank                                     *AlipayBankEvidence
	BankFeeAmount                                  int64
	BankFeeKnown                                   bool
	TransferNo, ProviderTransferNo, State, PayDate string
	Amount                                         int64
	// PackageInfo is WeChat's package_info for a bill waiting for the payee
	// to confirm (WAIT_USER_CONFIRM) with requestMerchantTransfer.
	PackageInfo string
}

const BankProductAlipay = "alipay_bankcard"
const BankProductWechat = "wxpay_v2"

type BankTransferProduct interface{ BankTransferProduct() string }

// Alipay's order_fee is an estimate, never proof of a settled provider charge.
// AmountKnown is false when a signed FAIL/REFUND omits its amount.
type AlipayBankEvidence struct {
	Product                                         string
	SubmissionBound, AmountKnown, EstimatedFeeKnown bool
	EstimatedFee                                    int64
}
type TransferProvider interface {
	Transfer(context.Context, TransferRequest) (TransferResult, error)
	QueryTransfer(context.Context, TransferRequest) (TransferResult, error)
}
type TransferCapabilities interface{ TransferTypes() []string }

// TransferValidator validates local prerequisites before any ledger reservation.
// Callers may use TF0 as a preflight placeholder; actual writes require TF{id>0}.
type TransferValidator interface{ ValidateTransfer(TransferRequest) error }

type TransferProofResult struct{ FileID, State, DownloadURL string }
type TransferProofProvider interface {
	ApplyTransferProof(context.Context, string) (TransferProofResult, error)
	QueryTransferProof(context.Context, string) (TransferProofResult, error)
}
type ChannelBalanceResult struct {
	Available, Frozen int64
	Quota             map[string]string
}
type ChannelBalanceProvider interface {
	ChannelBalance(context.Context, string) (ChannelBalanceResult, error)
}

func validTransfer(r TransferRequest) bool {
	return r.TransferNo != "" && len(r.TransferNo) <= 32 && r.Account != "" && len(r.Account) <= 128 && r.Amount > 0 && len(r.Name) <= 128 && len(r.Title) <= 128 && len(r.Remark) <= 200
}
func transferState(s string) string {
	switch s {
	case "SUCCESS":
		return "SUCCESS"
	case "PROCESSING", "DEALING", "WAIT_PAY":
		return "PROCESSING"
	case "FAILED", "FAIL", "BANK_FAIL", "CLOSED":
		return "FAILED"
	default:
		return "UNKNOWN"
	}
}
