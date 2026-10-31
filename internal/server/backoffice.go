package server

import (
	"context"
	"errors"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/backoffice"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
	"net"
	"net/http"
	"strings"
)

// errRechargeClosed is returned by the recharge validate/start hooks when the
// merchant recharge feature is turned off; backoffice.writeError surfaces it as
// a 403 with the original merchant-facing message.
var errRechargeClosed = backoffice.FeatureClosed("未开启余额充值功能")

// paymentPolicyMessage extracts the merchant-facing refusal text from a
// CheckPayment error: the configured blackmsg / forceqq / blockalert carried by
// a PaymentRefusedError, else the generic policy message for a plain denial.
func paymentPolicyMessage(e error) string {
	var refused backoffice.PaymentRefusedError
	if errors.As(e, &refused) && refused.Message != "" {
		return refused.Message
	}
	return "payment rejected by configured policy"
}

func (s *Server) backoffice(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || (parts[1] != "resources" && parts[1] != "manage" && parts[1] != "workflows") {
		return false
	}
	service := backoffice.New(s.Pool, s.Store)
	service.SettlementConfirmURLs = s.transferConfirmURLs
	service.GroupRoutingChannels = s.groupPaymentChannels
	service.GroupChannelAllowed = func(id int64, kind string) bool {
		return s.ProviderFlow != nil && s.ProviderFlow.Providers[id] != nil && s.ProviderFlow.Providers[id].ID() == kind && len(s.ProviderFlow.Providers[id].Capabilities().Methods) > 0
	}

	if s.ProviderFlow != nil && s.ProviderFlow.AllowLive && len(s.CheckoutKey) >= 32 {
		service.RechargeValidate = func(ctx context.Context, mid int64, params map[string]string) error {
			if on, e := service.MerchantFeatureVisible(ctx, "recharge"); e != nil {
				return e
			} else if !on {
				return errRechargeClosed
			}
			p := compat.Params{}
			for k, v := range params {
				p[k] = v
			}
			p["sub_openid"] = p["open_id"]
			ip, _, e := net.SplitHostPort(r.RemoteAddr)
			if e != nil {
				ip = r.RemoteAddr
			}
			if e = service.CheckPayment(ctx, backoffice.PaymentContext{MerchantID: mid, ClientIP: ip, Buyer: p["sub_openid"]}); e != nil {
				return e
			}
			amount, e := ledger.Parse(params["amount"])
			if e != nil || amount <= 0 {
				return backoffice.ErrInvalid
			}
			p["name"] = "balance recharge"
			p["money"] = ledger.Format(amount)
			p["clientip"] = ip
			channel, method, e := s.providerRoutedBrowserRequest(ctx, mid, p)
			if e != nil {
				return backoffice.ErrInvalid
			}
			resolved, e := s.paymentIntentServer(ctx, mid, p["out_trade_no"], channel)
			if e != nil {
				return backoffice.ErrInvalid
			}
			if validator, ok := resolved.ProviderFlow.Providers[channel].(provider.CreateValidator); ok {
				base := strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/")
				if e = validator.ValidateCreate(provider.CreateRequest{TradeNo: "preflight", Subject: "balance recharge", Method: method, ClientIP: ip, OpenID: p["sub_openid"], Amount: amount, NotifyURL: base + "/callbacks/providers/preflight", ReturnURL: base + "/"}); e != nil {
					return backoffice.ErrInvalid
				}
			}
			return nil
		}
		service.RechargeStart = func(ctx context.Context, mid int64, trade string, params map[string]string) (string, error) {
			if on, e := service.MerchantFeatureVisible(ctx, "recharge"); e != nil {
				return "", e
			} else if !on {
				return "", errRechargeClosed
			}
			p := compat.Params{}
			for k, v := range params {
				p[k] = v
			}
			p["sub_openid"] = p["open_id"]
			o, e := s.Store.OrderByTradeNo(ctx, trade)
			if e != nil || o.MerchantID != mid {
				return "", payment.ErrNotFound
			}
			ip, _, e := net.SplitHostPort(r.RemoteAddr)
			if e != nil {
				ip = r.RemoteAddr
			}
			if e := service.CheckPayment(ctx, backoffice.PaymentContext{MerchantID: mid, ClientIP: ip, Buyer: p["sub_openid"]}); e != nil {
				return "", e
			}
			p["name"], p["out_trade_no"], p["money"], p["clientip"] = o.Name, o.OutTradeNo, ledger.Format(o.Amount), ip
			p["notify_url"], p["return_url"], p["param"] = o.NotifyURL, o.ReturnURL, o.Param
			channel, method, e := s.providerRoutedBrowserRequest(ctx, mid, p)
			if e != nil {
				return "", e
			}
			a, e := s.ProviderFlow.Submit(ctx, trade, channel, method, ip, p["sub_openid"])
			if e != nil {
				return "", e
			}
			if a.State != "pending" || a.Material == "" {
				return "", payment.ErrState
			}
			return s.checkoutURL(trade, "checkout"), nil
		}
	}
	principal := backoffice.Principal{Role: role, SubjectID: session.User.ID, Username: session.User.Username, AdminClass: session.User.AdminClass}
	if parts[1] == "workflows" {
		service.ServeWorkflows(w, r, principal, parts[2:])
		return true
	}
	service.Serve(w, r, principal, parts[2:])
	return true
}
