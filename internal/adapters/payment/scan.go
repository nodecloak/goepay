package payment

import (
	"context"
	"net"
	"strings"
)

// AuthCode is a short-lived bearer authorization: callers must never persist,
// log, or reuse it to retry an uncertain financial request.
type ScanRequest struct {
	TradeNo, Subject, NotifyURL, ClientIP, AuthCode string
	Amount                                          int64
	ProfitSharing                                   bool
}
type ScanProvider interface {
	Scan(context.Context, ScanRequest) (PaymentEvent, error)
}
type ScanValidator interface{ ValidateScan(ScanRequest) error }

// ScanPaymentType recognizes the documented barcode products only. It does
// not accept OAuth codes, preauthorization numbers, or face-payment tokens.
func ScanPaymentType(code string) (string, error) {
	if !onlyDigits(code) {
		return "", ErrInvalid
	}
	if len(code) == 18 && code[:2] >= "10" && code[:2] <= "15" {
		return "wxpay", nil
	}
	if len(code) >= 16 && len(code) <= 24 && code[:2] >= "25" && code[:2] <= "30" {
		return "alipay", nil
	}
	return "", ErrInvalid
}
func validScan(r ScanRequest, provider string) bool {
	kind, e := ScanPaymentType(r.AuthCode)
	return e == nil && kind == provider && validCreate(CreateRequest{TradeNo: r.TradeNo, Subject: r.Subject, Amount: r.Amount}) && net.ParseIP(r.ClientIP) != nil
}
func trustedBuyer(s string) bool {
	return len(s) <= 256 && (s == "" || strings.TrimSpace(s) == s && !strings.HasPrefix(s, "@") && !strings.ContainsAny(s, "\r\n\x00"))
}
