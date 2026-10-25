package payment

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AuthorizationEvent is evidence of buyer authorization, never payment evidence.
type AuthorizationEvent struct {
	TradeNo, RequestNo, AuthNo, BuyerID, State, OrderStatus, OperationType string
	Amount, RestAmount                                                     int64
	RestAmountKnown                                                        bool
}
type AuthorizationRequest struct {
	TradeNo, RequestNo, AuthNo string
	Amount                     int64
}
type AuthorizationCaptureRequest struct {
	TradeNo, AuthNo, Subject, BuyerID, NotifyURL string
	Amount                                       int64
}
type PreauthorizationProvider interface {
	Freeze(context.Context, CreateRequest) (CreateResult, error)
	VerifyAuthorization(context.Context, http.Header, []byte, ExpectedPayment) (AuthorizationEvent, error)
	CaptureAuthorization(context.Context, AuthorizationCaptureRequest) (PaymentEvent, error)
	QueryAuthorization(context.Context, AuthorizationRequest) (AuthorizationEvent, error)
	UnfreezeAuthorization(context.Context, AuthorizationRequest) (AuthorizationEvent, error)
	CancelAuthorization(context.Context, AuthorizationRequest) (AuthorizationEvent, error)
}

func (a *Alipay) ValidateFreeze(r CreateRequest) error {
	if r.ProfitSharing {
		return ErrUnsupported
	}
	if e := a.validateCertificates(); e != nil {
		return e
	}
	if !enabled("preauth", a.config.Methods) {
		return ErrUnsupported
	}
	if !validCreate(r) || r.Method != "preauth" || !validPaymentURL(r.NotifyURL) || r.ClientIP != "" && net.ParseIP(r.ClientIP) == nil {
		return ErrInvalid
	}
	if a.config.PreauthVoucher && (r.Amount > 10000000000 || r.ExpiresAt != nil && (r.ExpiresAt.Sub(a.config.Now()) < time.Minute || r.ExpiresAt.Sub(a.config.Now()) > 15*24*time.Hour)) {
		return ErrInvalid
	}
	return nil
}
func (a *Alipay) Freeze(ctx context.Context, r CreateRequest) (CreateResult, error) {
	if e := a.ValidateFreeze(r); e != nil {
		return CreateResult{}, e
	}
	expire := a.config.Now().Add(2 * time.Hour)
	if r.ExpiresAt != nil {
		expire = *r.ExpiresAt
	}
	biz := map[string]any{"out_order_no": r.TradeNo, "out_request_no": r.TradeNo, "order_title": r.Subject, "amount": amountString(r.Amount), "product_code": "PREAUTH_PAY", "time_expire": expire.In(time.FixedZone("Asia/Shanghai", 8*3600)).Format("2006-01-02 15:04:05")}
	if a.config.SellerID != "" {
		biz["payee_user_id"] = a.config.SellerID
	}
	if r.ClientIP != "" {
		biz["business_params"] = map[string]string{"mc_create_trade_ip": r.ClientIP}
	}
	if a.config.PreauthVoucher {
		return a.freezeVoucher(ctx, r, biz)
	}
	p, e := a.params("alipay.fund.auth.order.app.freeze", biz)
	if e != nil {
		return CreateResult{}, e
	}
	p["notify_url"] = r.NotifyURL
	p["sign"], e = SignAlipayRSA2(p, a.config.PrivateKey)
	if e != nil {
		return CreateResult{}, e
	}
	v := url.Values{}
	for k, val := range p {
		v.Set(k, val)
	}
	return CreateResult{MaterialType: "preauth", Material: v.Encode()}, nil
}
func trustedAuthorizationText(s string) bool {
	return s != "" && len(s) <= 256 && strings.TrimSpace(s) == s && !strings.HasPrefix(s, "@") && !strings.ContainsAny(s, "\r\n\x00")
}
func (a *Alipay) VerifyAuthorization(_ context.Context, _ http.Header, body []byte, expected ExpectedPayment) (AuthorizationEvent, error) {
	if !enabled("preauth", a.config.Methods) {
		return AuthorizationEvent{}, ErrUnsupported
	}
	if len(body) > 1<<20 {
		return AuthorizationEvent{}, ErrInvalid
	}
	v, e := url.ParseQuery(string(body))
	if e != nil {
		return AuthorizationEvent{}, ErrInvalid
	}
	p := map[string]string{}
	for k, values := range v {
		if len(values) != 1 || strings.ContainsAny(k, "[]\x00") {
			return AuthorizationEvent{}, ErrInvalid
		}
		p[k] = values[0]
	}
	if !a.verifyNotification(p) {
		return AuthorizationEvent{}, ErrAuthentication
	}
	amount, e := parseAmount(p["amount"])
	if e != nil || amount != expected.Amount || p["out_order_no"] != expected.TradeNo || p["out_request_no"] != expected.TradeNo || p["app_id"] != a.config.AppID || expected.AppID != "" && p["app_id"] != expected.AppID || a.config.SellerID != "" && p["payee_user_id"] != a.config.SellerID || expected.MerchantID != "" && p["payee_user_id"] != expected.MerchantID || !trustedAuthorizationText(p["auth_no"]) || !trustedAuthorizationText(p["payer_user_id"]) || p["status"] != "SUCCESS" || p["operation_type"] != "FREEZE" || p["currency"] != "" && p["currency"] != "CNY" {
		return AuthorizationEvent{}, ErrAuthentication
	}
	return AuthorizationEvent{TradeNo: expected.TradeNo, RequestNo: expected.TradeNo, AuthNo: p["auth_no"], BuyerID: p["payer_user_id"], State: "SUCCESS", OrderStatus: "AUTHORIZED", Amount: amount, RestAmount: amount}, nil
}
func (a *Alipay) CaptureAuthorization(ctx context.Context, r AuthorizationCaptureRequest) (PaymentEvent, error) {
	if !enabled("preauth", a.config.Methods) {
		return PaymentEvent{}, ErrUnsupported
	}
	if !validCreate(CreateRequest{TradeNo: r.TradeNo, Subject: r.Subject, Amount: r.Amount}) || !trustedAuthorizationText(r.AuthNo) || !trustedAuthorizationText(r.BuyerID) || !validPaymentURL(r.NotifyURL) {
		return PaymentEvent{}, ErrInvalid
	}
	biz := map[string]any{"out_trade_no": r.TradeNo, "total_amount": amountString(r.Amount), "subject": r.Subject, "product_code": "PREAUTH_PAY", "auth_no": r.AuthNo, "auth_confirm_mode": "COMPLETE"}
	if a.config.SellerID != "" {
		biz["seller_id"] = a.config.SellerID
	}
	out, e := a.call(ctx, "alipay.trade.pay", biz, true, map[string]string{"notify_url": r.NotifyURL})
	if e != nil {
		return PaymentEvent{}, e
	}
	amount, e := parseAmount(text(out, "total_amount"))
	buyer := text(out, "buyer_user_id")
	if buyer == "" {
		buyer = text(out, "buyer_open_id")
	}
	if e != nil || amount != r.Amount || text(out, "out_trade_no") != r.TradeNo || !trustedAuthorizationText(text(out, "trade_no")) || buyer != r.BuyerID {
		return PaymentEvent{}, &UnknownOutcome{"preauth capture", ErrAuthentication}
	}
	return PaymentEvent{EventID: text(out, "trade_no"), TradeNo: r.TradeNo, ProviderTradeNo: text(out, "trade_no"), BuyerID: buyer, State: "SUCCESS", Currency: "CNY", Amount: amount}, nil
}
func (a *Alipay) QueryAuthorization(ctx context.Context, r AuthorizationRequest) (AuthorizationEvent, error) {
	if !enabled("preauth", a.config.Methods) {
		return AuthorizationEvent{}, ErrUnsupported
	}
	if r.TradeNo == "" || r.RequestNo == "" || r.Amount <= 0 {
		return AuthorizationEvent{}, ErrInvalid
	}
	out, e := a.call(ctx, "alipay.fund.auth.operation.detail.query", map[string]any{"out_order_no": r.TradeNo, "out_request_no": r.RequestNo}, false)
	if e != nil {
		return AuthorizationEvent{}, e
	}
	amount, e := parseAmount(text(out, "amount"))
	rest := int64(0)
	rawRest := text(out, "rest_amount")
	if rawRest != "0" && rawRest != "0.00" {
		var re error
		rest, re = parseAmount(rawRest)
		if re != nil {
			return AuthorizationEvent{}, ErrAuthentication
		}
	}
	if e != nil || amount != r.Amount || text(out, "out_order_no") != r.TradeNo || text(out, "out_request_no") != r.RequestNo || !trustedAuthorizationText(text(out, "auth_no")) || r.AuthNo != "" && r.AuthNo != text(out, "auth_no") || rest > amount {
		return AuthorizationEvent{}, ErrAuthentication
	}
	return AuthorizationEvent{TradeNo: r.TradeNo, RequestNo: r.RequestNo, AuthNo: text(out, "auth_no"), BuyerID: text(out, "payer_user_id"), Amount: amount, RestAmount: rest, RestAmountKnown: true, OperationType: text(out, "operation_type"), State: text(out, "status"), OrderStatus: text(out, "order_status")}, nil
}
func (a *Alipay) UnfreezeAuthorization(ctx context.Context, r AuthorizationRequest) (AuthorizationEvent, error) {
	if !enabled("preauth", a.config.Methods) {
		return AuthorizationEvent{}, ErrUnsupported
	}
	if !trustedAuthorizationText(r.AuthNo) || r.RequestNo == "" || r.Amount <= 0 {
		return AuthorizationEvent{}, ErrInvalid
	}
	out, e := a.call(ctx, "alipay.fund.auth.order.unfreeze", map[string]any{"auth_no": r.AuthNo, "out_request_no": r.RequestNo, "amount": amountString(r.Amount), "remark": "解冻资金"}, true)
	if e != nil {
		return AuthorizationEvent{}, e
	}
	amount, e := parseAmount(text(out, "amount"))
	if e != nil || amount != r.Amount || text(out, "auth_no") != r.AuthNo || text(out, "out_request_no") != r.RequestNo {
		return AuthorizationEvent{}, &UnknownOutcome{"preauth unfreeze", ErrAuthentication}
	}
	return AuthorizationEvent{TradeNo: r.TradeNo, RequestNo: r.RequestNo, AuthNo: r.AuthNo, State: "SUCCESS", Amount: amount}, nil
}
func (a *Alipay) CancelAuthorization(ctx context.Context, r AuthorizationRequest) (AuthorizationEvent, error) {
	if !enabled("preauth", a.config.Methods) {
		return AuthorizationEvent{}, ErrUnsupported
	}
	if r.TradeNo == "" || r.RequestNo == "" || !trustedAuthorizationText(r.AuthNo) {
		return AuthorizationEvent{}, ErrInvalid
	}
	out, e := a.call(ctx, "alipay.fund.auth.operation.cancel", map[string]any{"out_order_no": r.TradeNo, "out_request_no": r.RequestNo, "auth_no": r.AuthNo}, true)
	if e != nil {
		return AuthorizationEvent{}, e
	}
	if text(out, "auth_no") != r.AuthNo || text(out, "out_order_no") != r.TradeNo || text(out, "out_request_no") != r.RequestNo {
		return AuthorizationEvent{}, &UnknownOutcome{"preauth cancel", ErrAuthentication}
	}
	return AuthorizationEvent{TradeNo: r.TradeNo, RequestNo: r.RequestNo, AuthNo: r.AuthNo, State: "SUCCESS", Amount: r.Amount}, nil
}

var _ PreauthorizationProvider = (*Alipay)(nil)
