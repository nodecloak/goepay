package payment

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"time"
)

// CombineSub is one share of a combined order: its own out_trade_no and cents.
type CombineSub struct {
	TradeNo string
	Amount  int64
}

const (
	combineMinSubs = 3
	combineMaxSubs = 50
)

func validCombineSubs(r CreateRequest, subs []CombineSub) bool {
	if len(subs) < combineMinSubs || len(subs) > combineMaxSubs {
		return false
	}
	var sum int64
	seen := map[string]bool{}
	for _, s := range subs {
		if s.Amount <= 0 || !v3TradeNumber.MatchString(s.TradeNo) || seen[s.TradeNo] || s.TradeNo == r.TradeNo {
			return false
		}
		seen[s.TradeNo] = true
		sum += s.Amount
	}
	return sum == r.Amount
}

// CombineCreate sends a WeChat Pay v3 direct-merchant combined order
// (POST /v3/combine-transactions/{native|jsapi|h5|app}). The parent trade
// number is combine_out_trade_no; every sub-order carries this merchant's
// mchid. The response is mapped exactly like an ordinary create: an
// unexpected or unverifiable response is an UnknownOutcome, which callers
// must resolve by querying, never by sending again.
func (w *WechatV3) CombineCreate(ctx context.Context, r CreateRequest, subs []CombineSub) (CreateResult, error) {
	if e := w.ValidateCreate(r); e != nil {
		return CreateResult{}, e
	}
	if !validCombineSubs(r, subs) {
		return CreateResult{}, ErrInvalid
	}
	orders := make([]map[string]any, 0, len(subs))
	for _, s := range subs {
		sub := map[string]any{"mchid": w.config.MerchantID, "attach": "combine", "amount": map[string]any{"total_amount": s.Amount, "currency": "CNY"}, "out_trade_no": s.TradeNo, "description": r.Subject}
		if r.ProfitSharing {
			sub["settle_info"] = map[string]bool{"profit_sharing": true}
		}
		orders = append(orders, sub)
	}
	request := map[string]any{"combine_appid": w.config.AppID, "combine_mchid": w.config.MerchantID, "combine_out_trade_no": r.TradeNo, "sub_orders": orders, "notify_url": r.NotifyURL}
	scene := map[string]any{}
	if ip := net.ParseIP(r.ClientIP); ip != nil {
		scene["payer_client_ip"] = ip.String()
	}
	if r.Method == "h5" {
		scene["device_id"] = "10001"
		scene["h5_info"] = map[string]string{"type": "Wap", "app_name": w.config.H5SiteName, "app_url": w.config.H5SiteURL}
	}
	if len(scene) > 0 {
		request["scene_info"] = scene
	}
	if r.Method == "jsapi" {
		request["combine_payer_info"] = map[string]string{"openid": r.OpenID}
	}
	if r.ExpiresAt != nil {
		request["time_expire"] = r.ExpiresAt.Format(time.RFC3339)
	}
	target := "/v3/combine-transactions/" + r.Method
	raw, e := w.call(ctx, "POST", target, request)
	if e != nil {
		return CreateResult{}, e
	}
	return w.createMaterial(r, target, raw)
}

// combineTransaction verifies a decrypted combined transaction (notification
// or query): the combined identity is this merchant's, every recorded
// sub-order appears exactly once with this mchid, its exact amount in CNY and
// a provider transaction id when paid. Unlike the original, a combined
// notification is never trusted without these checks. State is SUCCESS only
// when every sub-order succeeded; NOTPAY when none has; anything mixed or
// unknown is an authentication failure for the caller to resolve by query.
func (w *WechatV3) combineTransaction(raw []byte, tradeNo string, expected []CombineSub) (PaymentEvent, error) {
	out, e := v3Object(raw)
	if e != nil {
		return PaymentEvent{}, e
	}
	if v3String(out, "combine_appid") != w.config.AppID || v3String(out, "combine_mchid") != w.config.MerchantID || v3String(out, "combine_out_trade_no") != tradeNo || tradeNo == "" {
		return PaymentEvent{}, ErrAuthentication
	}
	var subs []json.RawMessage
	if json.Unmarshal(out["sub_orders"], &subs) != nil || len(subs) != len(expected) {
		return PaymentEvent{}, ErrAuthentication
	}
	want := map[string]int64{}
	for _, s := range expected {
		want[s.TradeNo] = s.Amount
	}
	seen := map[string]bool{}
	var paid []CombinePaidSub
	var total int64
	states := map[string]int{}
	for _, raw := range subs {
		sub, e := v3Object(raw)
		if e != nil {
			return PaymentEvent{}, e
		}
		amount, e := v3Object(sub["amount"])
		if e != nil {
			return PaymentEvent{}, e
		}
		no, value := v3String(sub, "out_trade_no"), v3Int(amount, "total_amount")
		expectedAmount, ok := want[no]
		if !ok || seen[no] || v3String(sub, "mchid") != w.config.MerchantID || value != expectedAmount || v3String(amount, "currency") != "CNY" {
			return PaymentEvent{}, ErrAuthentication
		}
		seen[no] = true
		state := v3String(sub, "trade_state")
		switch state {
		case "SUCCESS", "REFUND":
			id := v3String(sub, "transaction_id")
			if id == "" {
				return PaymentEvent{}, ErrAuthentication
			}
			paid = append(paid, CombinePaidSub{TradeNo: no, TransactionID: id, Amount: value})
			state = "SUCCESS"
		case "NOTPAY", "CLOSED", "PAYERROR", "USERPAYING":
		default:
			return PaymentEvent{}, ErrAuthentication
		}
		states[state]++
		total += value
	}
	event := PaymentEvent{TradeNo: tradeNo, ProviderTradeNo: tradeNo, Currency: "CNY", Amount: total}
	switch {
	case states["SUCCESS"] == len(expected):
		event.State, event.CombinePaid = "SUCCESS", paid
		buyer := ""
		if value, exists := out["combine_payer_info"]; exists {
			payer, e := v3Object(value)
			if e != nil {
				return PaymentEvent{}, e
			}
			buyer = v3String(payer, "openid")
		}
		if buyer == "" || !trustedBuyer(buyer) {
			return PaymentEvent{}, ErrAuthentication
		}
		event.BuyerID = buyer
	case states["SUCCESS"] == 0 && len(states) == 1:
		for state := range states {
			event.State = state
		}
	default:
		return PaymentEvent{}, ErrAuthentication
	}
	return event, nil
}

// combineQuery reads GET /v3/combine-transactions/out-trade-no/{no} and
// verifies it exactly like a combined notification.
func (w *WechatV3) combineQuery(ctx context.Context, r QueryRequest) (QueryResult, error) {
	raw, e := w.call(ctx, "GET", "/v3/combine-transactions/out-trade-no/"+url.PathEscape(r.TradeNo), nil)
	if e != nil {
		return QueryResult{}, e
	}
	event, e := w.combineTransaction(raw, r.TradeNo, r.CombineSubs)
	if e != nil {
		return QueryResult{}, e
	}
	return QueryResult{TradeNo: event.TradeNo, ProviderTradeNo: event.ProviderTradeNo, State: event.State, Currency: event.Currency, BuyerID: event.BuyerID, Amount: event.Amount, CombinePaid: event.CombinePaid}, nil
}

// combineClose closes every sub-order through the combined close endpoint
// (the original closed each sub-order with the single-order API).
func (w *WechatV3) combineClose(ctx context.Context, r QueryRequest) error {
	subs := make([]map[string]string, 0, len(r.CombineSubs))
	for _, s := range r.CombineSubs {
		if !v3TradeNumber.MatchString(s.TradeNo) {
			return ErrInvalid
		}
		subs = append(subs, map[string]string{"mchid": w.config.MerchantID, "out_trade_no": s.TradeNo})
	}
	raw, e := w.call(ctx, "POST", "/v3/combine-transactions/out-trade-no/"+url.PathEscape(r.TradeNo)+"/close", map[string]any{"combine_appid": w.config.AppID, "sub_orders": subs})
	if e != nil {
		return e
	}
	if len(raw) != 0 {
		return &UnknownOutcome{Operation: "combine-close", Cause: ErrAuthentication}
	}
	return nil
}
