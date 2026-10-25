package payment

import (
	"context"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Receiver types are explicit protocol values; identifiers are never classified
// from a prefix. These private wire values must be encrypted by persistence.
type SharingReceiver struct{ Type, Account, Name, RelationType, CustomRelation string }
type SharingAllocation struct {
	SharingReceiver
	Amount      int64
	Description string
}
type ChannelSharingRequest struct {
	OrderNo, ProviderTradeNo string
	TotalAmount              int64
	Receivers                []SharingAllocation
	UnfreezeUnsplit          bool
}
type SharingReceiverResult struct {
	Type, Account, State, ProviderDetailNo, FailureCode string
	Amount                                              int64
}
type ChannelSharingResult struct {
	OrderNo, ProviderOrderNo, ProviderTradeNo, State string
	Receivers                                        []SharingReceiverResult
}
type ChannelSharingReturnRequest struct {
	OrderNo, ProviderOrderNo, ProviderTradeNo, ReturnNo, ReceiverType, ReceiverAccount, Description string
	Amount                                                                                          int64
}
type ChannelSharingReturnResult struct {
	OrderNo, ReturnNo, ProviderReturnNo, State string
	Amount                                     int64
}
type ChannelSharingFinishRequest struct {
	OrderNo, ProviderOrderNo, ProviderTradeNo, Description string
	ExpectedUnsplit                                        int64
}
type ChannelSharingFinishResult struct {
	OrderNo, ProviderOrderNo, ProviderTradeNo, State string
	UnsplitAmount                                    int64
}
type ChannelSharingFinisher interface {
	FinishChannelSharing(context.Context, ChannelSharingFinishRequest) (ChannelSharingFinishResult, error)
	QueryChannelSharingFinish(context.Context, ChannelSharingFinishRequest) (ChannelSharingFinishResult, error)
}
type ChannelSharingRemainingProvider interface {
	ChannelSharingRemaining(context.Context, string) (int64, error)
}
type ChannelSharingReceiverValidator interface {
	ValidateChannelSharingReceiver(SharingReceiver, bool) error
}
type ChannelSharingPaymentProfile interface{ ChannelSharingRefundMode(string) (string, error) }

// Relationship ownership may span several applications for one upstream
// merchant. It must not be inferred from the local tenant or channel number.
type ChannelSharingRelationIdentity interface{ ChannelSharingRelationAccount() string }
type ChannelSharingCapabilities struct {
	Product             string
	ReceiverTypes       []string
	ReturnSupported     bool
	FinishSupported     bool
	AutoFinishSupported bool
}
type ChannelSharingProvider interface {
	ChannelSharingCapabilities() ChannelSharingCapabilities
	ValidateChannelSharing(ChannelSharingRequest) error
	BindChannelSharingReceiver(context.Context, string, SharingReceiver) error
	UnbindChannelSharingReceiver(context.Context, string, SharingReceiver) error
	SubmitChannelSharing(context.Context, ChannelSharingRequest) (ChannelSharingResult, error)
	QueryChannelSharing(context.Context, ChannelSharingRequest) (ChannelSharingResult, error)
	ReturnChannelSharing(context.Context, ChannelSharingReturnRequest) (ChannelSharingReturnResult, error)
	QueryChannelSharingReturn(context.Context, ChannelSharingReturnRequest) (ChannelSharingReturnResult, error)
}

var sharingOperationNo = regexp.MustCompile(`^[A-Za-z0-9_\-|*@]{1,64}$`)

func sharingText(value string, limit int, required bool) bool {
	return (!required || value != "") && len(value) <= limit && utf8.ValidString(value) && strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n")
}
func sharingReceiverValid(r SharingReceiver) bool {
	return sharingText(r.Type, 32, true) && sharingText(r.Account, 128, true) && sharingText(r.Name, 256, false)
}
func sharingReceiverKey(kind, account string) string { return kind + "\x00" + account }
func sharingRequestValid(r ChannelSharingRequest) bool {
	if !sharingOperationNo.MatchString(r.OrderNo) || !sharingText(r.ProviderTradeNo, 64, true) || r.TotalAmount <= 0 || r.TotalAmount > 999999999999 || len(r.Receivers) == 0 || len(r.Receivers) > 50 {
		return false
	}
	sum := int64(0)
	seen := map[string]bool{}
	for _, receiver := range r.Receivers {
		key := sharingReceiverKey(receiver.Type, receiver.Account)
		if !sharingReceiverValid(receiver.SharingReceiver) || !sharingText(receiver.Description, 80, true) || seen[key] || receiver.Amount <= 0 || receiver.Amount > r.TotalAmount-sum {
			return false
		}
		seen[key] = true
		sum += receiver.Amount
	}
	return true
}
func sharingAggregate(receivers []SharingReceiverResult, finished bool) (string, error) {
	success, failed, pending := 0, 0, 0
	for _, r := range receivers {
		switch r.State {
		case "succeeded":
			success++
		case "failed":
			failed++
		case "pending":
			pending++
		default:
			return "", ErrAuthentication
		}
	}
	if !finished {
		return "pending", nil
	}
	if pending != 0 || len(receivers) == 0 {
		return "", ErrAuthentication
	}
	if failed == 0 {
		return "succeeded", nil
	}
	if success == 0 {
		return "failed", nil
	}
	return "partial", nil
}
