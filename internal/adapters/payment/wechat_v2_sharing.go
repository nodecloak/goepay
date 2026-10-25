package payment

import (
	"context"
	"encoding/json"
	"strconv"
)

func (w *WechatV2) ChannelSharingRefundMode(method string) (string, error) {
	if w == nil || !w.config.EnableChannelSharing || !enabled(method, w.config.Methods) {
		return "", ErrUnsupported
	}
	switch method {
	case "native", "jsapi", "h5", "app", "scan":
		return "merchant_funded", nil
	}
	return "", ErrUnsupported
}

func (w *WechatV2) validateSharingV2Request(r ChannelSharingRequest) error {
	if e := w.validateSharingV2Transport(false); e != nil {
		return e
	}
	if !r.UnfreezeUnsplit || len(r.ProviderTradeNo) > 32 || !scanCancellationNumber.MatchString(r.ProviderTradeNo) {
		return ErrInvalid
	}
	copy := r
	copy.Receivers = append([]SharingAllocation(nil), r.Receivers...)
	for i, item := range r.Receivers {
		if item.Type != "MERCHANT_ID" && item.Type != "PERSONAL_OPENID" || !sharingText(item.Account, 64, true) {
			return ErrInvalid
		}
		if item.Type == "MERCHANT_ID" {
			if item.Account == w.config.MerchantID || !sharingText(item.Name, 1024, false) {
				return ErrInvalid
			}
			// Merchant names belong to receiver binding. This endpoint accepts
			// a personal name only; it never changes the original recipient.
			copy.Receivers[i].Name = ""
		} else if !sharingText(item.Name, 64, false) {
			return ErrInvalid
		}
	}
	if !sharingRequestValid(copy) {
		return ErrInvalid
	}
	return nil
}

func (w *WechatV2) ValidateChannelSharing(r ChannelSharingRequest) error {
	if e := w.validateSharingV2Request(r); e != nil {
		return e
	}
	return w.validateSharingV2Transport(true)
}

func (w *WechatV2) sharingV2Result(out map[string]string, r ChannelSharingRequest) (ChannelSharingResult, error) {
	if !sharingOperationNo.MatchString(out["order_id"]) || out["out_order_no"] != r.OrderNo || out["transaction_id"] != r.ProviderTradeNo || providerUniqueJSON([]byte(out["receivers"])) != nil {
		return ChannelSharingResult{}, ErrAuthentication
	}
	var rows []json.RawMessage
	if json.Unmarshal([]byte(out["receivers"]), &rows) != nil || len(rows) > 50 {
		return ChannelSharingResult{}, ErrAuthentication
	}
	normalized := make([]map[string]json.RawMessage, 0, len(rows))
	for _, raw := range rows {
		row, e := v3Object(raw)
		if e != nil {
			return ChannelSharingResult{}, e
		}
		account := ""
		for _, field := range []string{"account", "receiver_account"} {
			if value, present := row[field]; present {
				var got string
				if json.Unmarshal(value, &got) != nil || !sharingText(got, 64, true) || account != "" && account != got {
					return ChannelSharingResult{}, ErrAuthentication
				}
				account = got
			}
		}
		if account == "" {
			return ChannelSharingResult{}, ErrAuthentication
		}
		if value, present := row["receiver_mchid"]; present {
			var got string
			if json.Unmarshal(value, &got) != nil || got != "" && (v3String(row, "type") != "MERCHANT_ID" || got != account) {
				return ChannelSharingResult{}, ErrAuthentication
			}
		}
		row["account"], _ = json.Marshal(account)
		normalized = append(normalized, row)
	}
	raw, e := json.Marshal(map[string]any{"state": out["status"], "out_order_no": out["out_order_no"], "transaction_id": out["transaction_id"], "order_id": out["order_id"], "receivers": normalized})
	if e != nil {
		return ChannelSharingResult{}, ErrAuthentication
	}
	return decodeWechatSharingResult(raw, r)
}

func (w *WechatV2) SubmitChannelSharing(ctx context.Context, r ChannelSharingRequest) (ChannelSharingResult, error) {
	if e := w.ValidateChannelSharing(r); e != nil {
		return ChannelSharingResult{}, e
	}
	receivers := make([]map[string]any, 0, len(r.Receivers))
	for _, receiver := range r.Receivers {
		item := map[string]any{"type": receiver.Type, "account": receiver.Account, "amount": receiver.Amount, "description": receiver.Description}
		if receiver.Type == "PERSONAL_OPENID" && receiver.Name != "" {
			item["name"] = receiver.Name
		}
		receivers = append(receivers, item)
	}
	raw, e := json.Marshal(receivers)
	if e != nil {
		return ChannelSharingResult{}, ErrInvalid
	}
	out, e := w.sharingV2Call(ctx, "/secapi/pay/profitsharing", map[string]string{"out_order_no": r.OrderNo, "transaction_id": r.ProviderTradeNo, "receivers": string(raw)}, true, true, true)
	if e != nil {
		return ChannelSharingResult{}, e
	}
	result, e := w.sharingV2Result(out, r)
	if e != nil {
		return ChannelSharingResult{}, &UnknownOutcome{Operation: "wechat-v2-sharing-submit", Cause: e}
	}
	return result, nil
}

func (w *WechatV2) QueryChannelSharing(ctx context.Context, r ChannelSharingRequest) (ChannelSharingResult, error) {
	if e := w.validateSharingV2Request(r); e != nil {
		return ChannelSharingResult{}, e
	}
	out, e := w.sharingV2Call(ctx, "/pay/profitsharingquery", map[string]string{"out_order_no": r.OrderNo, "transaction_id": r.ProviderTradeNo}, false, false, false)
	if e != nil {
		return ChannelSharingResult{}, e
	}
	return w.sharingV2Result(out, r)
}

func (w *WechatV2) ChannelSharingRemaining(ctx context.Context, providerTradeNo string) (int64, error) {
	if len(providerTradeNo) > 32 || !scanCancellationNumber.MatchString(providerTradeNo) {
		return 0, ErrInvalid
	}
	out, e := w.sharingV2Call(ctx, "/pay/profitsharingorderamountquery", map[string]string{"transaction_id": providerTradeNo}, false, false, false)
	if e != nil {
		return 0, e
	}
	amount, e := strconv.ParseInt(out["unsplit_amount"], 10, 64)
	if e != nil || amount < 0 || amount > 999999999999 || out["transaction_id"] != providerTradeNo {
		return 0, ErrAuthentication
	}
	return amount, nil
}
