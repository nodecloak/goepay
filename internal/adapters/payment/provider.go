package payment

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

var ErrUnsupported = errors.New("provider capability not configured")
var ErrInvalid = errors.New("invalid provider request")

// UnknownOutcome means a financial request may have reached the upstream.
// Query with the same stable business number before retrying or releasing funds.
type UnknownOutcome struct {
	Operation string
	Cause     error
}

func (e *UnknownOutcome) Error() string { return "provider outcome unknown: " + e.Operation }
func (e *UnknownOutcome) Unwrap() error { return e.Cause }

func uncertainProviderOutcome(err error) bool {
	var unknown *UnknownOutcome
	return errors.As(err, &unknown) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

type ProviderError struct {
	Code            string
	rejection       *authenticatedProviderRejection
	transferFailure *TransferFailureReceipt
	scanPending     *ScanPendingReceipt
}

func (e *ProviderError) Error() string { return "provider rejected request: " + e.Code }

type Capabilities struct {
	Methods                           []string
	Query, Close, Refund, RefundQuery bool
}
type CreateRequest struct {
	// Set only from a durable, trusted original-payment declaration.
	ProfitSharing                                                    bool
	TradeNo, Subject, Method, NotifyURL, ReturnURL, ClientIP, OpenID string
	Amount                                                           int64
	ExpiresAt                                                        *time.Time
}
type CreateResult struct{ MaterialType, Material, ProviderTradeNo string }
type QueryRequest struct{ TradeNo, ProviderTradeNo string }
type QueryResult struct {
	TradeNo, ProviderTradeNo, State, Currency string
	BuyerID                                   string
	Amount                                    int64
	scanObservation                           *ScanObservationReceipt
}
type RefundRequest struct {
	TradeNo, ProviderTradeNo, RefundNo, Reason string
	TotalAmount, Amount                        int64
}
type RefundResult struct {
	RefundNo, ProviderRefundNo, State string
	Amount                            int64
}
type ExpectedPayment struct {
	TradeNo, AppID, MerchantID string
	Amount                     int64
}
type PaymentEvent struct {
	EventID, TradeNo, ProviderTradeNo, BuyerID, State, Currency string
	Amount                                                      int64
}
type Provider interface {
	ID() string
	Capabilities() Capabilities
	Create(context.Context, CreateRequest) (CreateResult, error)
	Query(context.Context, QueryRequest) (QueryResult, error)
	Close(context.Context, QueryRequest) error
	Refund(context.Context, RefundRequest) (RefundResult, error)
	QueryRefund(context.Context, RefundRequest) (RefundResult, error)
}
type NotificationVerifier interface {
	VerifyNotification(context.Context, http.Header, []byte, ExpectedPayment) (PaymentEvent, error)
}

// ApplicationIdentity binds signed caller-supplied AppIDs to trusted channel
// configuration. It never permits callers to replace provider credentials.
type ApplicationIdentity interface{ ApplicationID() string }

// CreateValidator performs local validation without allocating a durable
// attempt or making an external call. Financial orchestration invokes it first.
type CreateValidator interface{ ValidateCreate(CreateRequest) error }

func validPaymentURL(raw string) bool {
	u, e := url.Parse(raw)
	return e == nil && u.Host != "" && u.User == nil && u.Fragment == "" && (u.Scheme == "https" || u.Scheme == "http")
}

func validCreate(r CreateRequest) bool {
	return r.TradeNo != "" && len(r.TradeNo) <= 64 && r.Subject != "" && r.Amount > 0 && r.Amount <= 999999999999
}
func amountString(n int64) string { return fmt.Sprintf("%d.%02d", n/100, n%100) }
func enabled(method string, methods []string) bool {
	for _, m := range methods {
		if m == method {
			return true
		}
	}
	return false
}

// boundedClient preserves the configured transport (including mTLS) while
// preventing redirects from replaying signed requests to another origin.
func boundedClient(input *http.Client) *http.Client {
	client := http.Client{}
	if input != nil {
		client = *input
	}
	if client.Timeout <= 0 || client.Timeout > 15*time.Second {
		client.Timeout = 15 * time.Second
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &client
}
