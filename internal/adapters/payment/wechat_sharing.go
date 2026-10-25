package payment

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/url"
)

func (w *WechatV3) ChannelSharingRelationAccount() string { return w.config.MerchantID }

func (w *WechatV3) ChannelSharingCapabilities() ChannelSharingCapabilities {
	if !w.config.EnableChannelSharing {
		return ChannelSharingCapabilities{}
	}
	return ChannelSharingCapabilities{Product: "wxpay_v3_direct", ReceiverTypes: []string{"MERCHANT_ID", "PERSONAL_OPENID"}, ReturnSupported: true, FinishSupported: true, AutoFinishSupported: true}
}
func (w *WechatV3) ChannelSharingRefundMode(method string) (string, error) {
	if !w.config.EnableChannelSharing || !enabled(method, w.config.Methods) {
		return "", ErrUnsupported
	}
	switch method {
	case "native", "jsapi", "h5", "app":
		return "merchant_funded", nil
	}
	return "", ErrUnsupported
}
func (w *WechatV3) validSharingReceiver(r SharingReceiver) bool {
	return sharingReceiverValid(r) && (r.Type == "MERCHANT_ID" || r.Type == "PERSONAL_OPENID")
}
func (w *WechatV3) ValidateChannelSharingReceiver(r SharingReceiver, remove bool) error {
	if !w.config.EnableChannelSharing {
		return ErrUnsupported
	}
	if !w.validSharingReceiver(r) {
		return ErrInvalid
	}
	if remove {
		return nil
	}
	switch r.RelationType {
	case "SERVICE_PROVIDER", "STORE", "STAFF", "STORE_OWNER", "PARTNER", "HEADQUARTER", "BRAND", "DISTRIBUTOR", "USER", "SUPPLIER":
		if r.CustomRelation != "" {
			return ErrInvalid
		}
	case "CUSTOM":
		if !sharingText(r.CustomRelation, 30, true) {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	if r.Type == "MERCHANT_ID" && r.Name == "" {
		return ErrInvalid
	}
	key := w.config.VerificationKeys[w.config.EncryptionSerial]
	if r.Name != "" && (key == nil || len(r.Name) > key.Size()-42) {
		return ErrInvalid
	}
	return nil
}
func (w *WechatV3) ValidateChannelSharing(r ChannelSharingRequest) error {
	if !w.config.EnableChannelSharing {
		return ErrUnsupported
	}
	if !sharingRequestValid(r) || len(r.ProviderTradeNo) > 32 || !wechatV3Identifier.MatchString(r.ProviderTradeNo) {
		return ErrInvalid
	}
	for _, receiver := range r.Receivers {
		if !w.validSharingReceiver(receiver.SharingReceiver) {
			return ErrInvalid
		}
	}
	return nil
}
func (w *WechatV3) encryptSharingName(name string) (string, error) {
	key := w.config.VerificationKeys[w.config.EncryptionSerial]
	if key == nil || len(name) > key.Size()-42 {
		return "", ErrInvalid
	}
	encrypted, e := rsa.EncryptOAEP(sha1.New(), rand.Reader, key, []byte(name), nil)
	if e != nil {
		return "", ErrInvalid
	}
	return base64.StdEncoding.EncodeToString(encrypted), nil
}
func (w *WechatV3) sharingReceiver(ctx context.Context, operation string, r SharingReceiver, remove bool) error {
	if e := w.ValidateChannelSharingReceiver(r, remove); e != nil {
		return e
	}
	if !w.config.EnableChannelSharing {
		return ErrUnsupported
	}
	if !sharingOperationNo.MatchString(operation) || !w.validSharingReceiver(r) {
		return ErrInvalid
	}
	target := "/v3/profitsharing/receivers/add"
	body := map[string]any{"appid": w.config.AppID, "type": r.Type, "account": r.Account}
	if remove {
		target = "/v3/profitsharing/receivers/delete"
	} else {
		switch r.RelationType {
		case "SERVICE_PROVIDER", "STORE", "STAFF", "STORE_OWNER", "PARTNER", "HEADQUARTER", "BRAND", "DISTRIBUTOR", "USER", "SUPPLIER":
			if r.CustomRelation != "" {
				return ErrInvalid
			}
		case "CUSTOM":
			if !sharingText(r.CustomRelation, 30, true) {
				return ErrInvalid
			}
			body["custom_relation"] = r.CustomRelation
		default:
			return ErrInvalid
		}
		if r.Type == "MERCHANT_ID" && r.Name == "" {
			return ErrInvalid
		}
		body["relation_type"] = r.RelationType
		if r.Name != "" {
			encrypted, e := w.encryptSharingName(r.Name)
			if e != nil {
				return e
			}
			body["name"] = encrypted
		}
	}
	raw, e := w.call(ctx, "POST", target, body)
	if e != nil {
		return e
	}
	out, e := v3Object(raw)
	if e != nil || v3String(out, "type") != r.Type || v3String(out, "account") != r.Account || !remove && v3String(out, "relation_type") != r.RelationType {
		return &UnknownOutcome{Operation: target, Cause: ErrAuthentication}
	}
	return nil
}
func (w *WechatV3) BindChannelSharingReceiver(ctx context.Context, operation string, r SharingReceiver) error {
	return w.sharingReceiver(ctx, operation, r, false)
}
func (w *WechatV3) UnbindChannelSharingReceiver(ctx context.Context, operation string, r SharingReceiver) error {
	return w.sharingReceiver(ctx, operation, r, true)
}
func (w *WechatV3) sharingResult(raw []byte, r ChannelSharingRequest) (ChannelSharingResult, error) {
	return decodeWechatSharingResult(raw, r)
}

// Both Wechat API generations retain the same exact receiver/amount checks
// after their separately authenticated wire envelopes are normalized.
func decodeWechatSharingResult(raw []byte, r ChannelSharingRequest) (ChannelSharingResult, error) {
	out, e := v3Object(raw)
	if e != nil {
		return ChannelSharingResult{}, e
	}
	state := v3String(out, "state")
	providerNo := v3String(out, "order_id")
	if v3String(out, "out_order_no") != r.OrderNo || v3String(out, "transaction_id") != r.ProviderTradeNo || providerNo == "" || state != "PROCESSING" && state != "FINISHED" {
		return ChannelSharingResult{}, ErrAuthentication
	}
	result := ChannelSharingResult{OrderNo: r.OrderNo, ProviderOrderNo: providerNo, ProviderTradeNo: r.ProviderTradeNo}
	expected := map[string]int{}
	for i, receiver := range r.Receivers {
		expected[sharingReceiverKey(receiver.Type, receiver.Account)] = i
		result.Receivers = append(result.Receivers, SharingReceiverResult{Type: receiver.Type, Account: receiver.Account, Amount: receiver.Amount, State: "pending"})
	}
	var rows []json.RawMessage
	if value, exists := out["receivers"]; exists {
		if json.Unmarshal(value, &rows) != nil {
			return ChannelSharingResult{}, ErrAuthentication
		}
	}
	seen := map[string]bool{}
	for _, row := range rows {
		receiver, e := v3Object(row)
		if e != nil {
			return ChannelSharingResult{}, e
		}
		kind, account := v3String(receiver, "type"), v3String(receiver, "account")
		key := sharingReceiverKey(kind, account)
		index, exists := expected[key]
		if !exists || seen[key] || v3Int(receiver, "amount") != r.Receivers[index].Amount {
			return ChannelSharingResult{}, ErrAuthentication
		}
		seen[key] = true
		canonical := ""
		switch v3String(receiver, "result") {
		case "PENDING":
			canonical = "pending"
		case "SUCCESS":
			canonical = "succeeded"
		case "CLOSED":
			canonical = "failed"
		default:
			return ChannelSharingResult{}, ErrAuthentication
		}
		detail := v3String(receiver, "detail_id")
		if canonical == "succeeded" && detail == "" {
			return ChannelSharingResult{}, ErrAuthentication
		}
		result.Receivers[index].State = canonical
		result.Receivers[index].ProviderDetailNo = detail
		result.Receivers[index].FailureCode = v3String(receiver, "fail_reason")
	}
	if state == "FINISHED" && len(seen) != len(expected) {
		return ChannelSharingResult{}, ErrAuthentication
	}
	result.State, e = sharingAggregate(result.Receivers, state == "FINISHED")
	return result, e
}
func (w *WechatV3) SubmitChannelSharing(ctx context.Context, r ChannelSharingRequest) (ChannelSharingResult, error) {
	if e := w.ValidateChannelSharing(r); e != nil {
		return ChannelSharingResult{}, e
	}
	receivers := []map[string]any{}
	for _, receiver := range r.Receivers {
		item := map[string]any{"type": receiver.Type, "account": receiver.Account, "amount": receiver.Amount, "description": receiver.Description}
		if receiver.Type == "PERSONAL_OPENID" && receiver.Name != "" {
			name, e := w.encryptSharingName(receiver.Name)
			if e != nil {
				return ChannelSharingResult{}, e
			}
			item["name"] = name
		}
		receivers = append(receivers, item)
	}
	raw, e := w.call(ctx, "POST", "/v3/profitsharing/orders", map[string]any{"appid": w.config.AppID, "transaction_id": r.ProviderTradeNo, "out_order_no": r.OrderNo, "receivers": receivers, "unfreeze_unsplit": r.UnfreezeUnsplit})
	if e != nil {
		return ChannelSharingResult{}, e
	}
	result, e := w.sharingResult(raw, r)
	if e != nil {
		return ChannelSharingResult{}, &UnknownOutcome{Operation: "sharing-submit", Cause: e}
	}
	return result, nil
}
func (w *WechatV3) QueryChannelSharing(ctx context.Context, r ChannelSharingRequest) (ChannelSharingResult, error) {
	if e := w.ValidateChannelSharing(r); e != nil {
		return ChannelSharingResult{}, e
	}
	raw, e := w.call(ctx, "GET", "/v3/profitsharing/orders/"+url.PathEscape(r.OrderNo)+"?transaction_id="+url.QueryEscape(r.ProviderTradeNo), nil)
	if e != nil {
		return ChannelSharingResult{}, e
	}
	return w.sharingResult(raw, r)
}
func (w *WechatV3) validSharingReturn(r ChannelSharingReturnRequest) error {
	if !w.config.EnableChannelSharing {
		return ErrUnsupported
	}
	if !sharingOperationNo.MatchString(r.OrderNo) || !sharingOperationNo.MatchString(r.ReturnNo) || !sharingText(r.ProviderOrderNo, 64, true) || r.ReceiverType != "MERCHANT_ID" || !sharingText(r.ReceiverAccount, 32, true) || r.Amount <= 0 || r.Amount > 999999999999 || !sharingText(r.Description, 80, true) {
		return ErrInvalid
	}
	return nil
}
func (w *WechatV3) sharingReturnResult(raw []byte, r ChannelSharingReturnRequest) (ChannelSharingReturnResult, error) {
	out, e := v3Object(raw)
	if e != nil {
		return ChannelSharingReturnResult{}, e
	}
	providerNo := v3String(out, "return_id")
	if v3String(out, "order_id") != r.ProviderOrderNo || v3String(out, "out_order_no") != r.OrderNo || v3String(out, "out_return_no") != r.ReturnNo || v3String(out, "return_mchid") != r.ReceiverAccount || v3Int(out, "amount") != r.Amount || providerNo == "" {
		return ChannelSharingReturnResult{}, ErrAuthentication
	}
	state := ""
	switch v3String(out, "result") {
	case "PROCESSING":
		state = "pending"
	case "SUCCESS":
		state = "succeeded"
	case "FAILED":
		state = "failed"
	default:
		return ChannelSharingReturnResult{}, ErrAuthentication
	}
	return ChannelSharingReturnResult{OrderNo: r.OrderNo, ReturnNo: r.ReturnNo, ProviderReturnNo: providerNo, State: state, Amount: r.Amount}, nil
}
func (w *WechatV3) ReturnChannelSharing(ctx context.Context, r ChannelSharingReturnRequest) (ChannelSharingReturnResult, error) {
	if e := w.validSharingReturn(r); e != nil {
		return ChannelSharingReturnResult{}, e
	}
	raw, e := w.call(ctx, "POST", "/v3/profitsharing/return-orders", map[string]any{"out_order_no": r.OrderNo, "out_return_no": r.ReturnNo, "return_mchid": r.ReceiverAccount, "amount": r.Amount, "description": r.Description})
	if e != nil {
		return ChannelSharingReturnResult{}, e
	}
	result, e := w.sharingReturnResult(raw, r)
	if e != nil {
		return ChannelSharingReturnResult{}, &UnknownOutcome{Operation: "sharing-return", Cause: e}
	}
	return result, nil
}
func (w *WechatV3) QueryChannelSharingReturn(ctx context.Context, r ChannelSharingReturnRequest) (ChannelSharingReturnResult, error) {
	if e := w.validSharingReturn(r); e != nil {
		return ChannelSharingReturnResult{}, e
	}
	raw, e := w.call(ctx, "GET", "/v3/profitsharing/return-orders/"+url.PathEscape(r.ReturnNo)+"?out_order_no="+url.QueryEscape(r.OrderNo), nil)
	if e != nil {
		return ChannelSharingReturnResult{}, e
	}
	return w.sharingReturnResult(raw, r)
}

func (w *WechatV3) validSharingFinish(r ChannelSharingFinishRequest) error {
	if !w.config.EnableChannelSharing {
		return ErrUnsupported
	}
	if !sharingOperationNo.MatchString(r.OrderNo) || !sharingText(r.ProviderOrderNo, 64, false) || !sharingText(r.ProviderTradeNo, 32, true) || !wechatV3Identifier.MatchString(r.ProviderTradeNo) || !sharingText(r.Description, 80, true) || r.ExpectedUnsplit <= 0 || r.ExpectedUnsplit > 999999999999 {
		return ErrInvalid
	}
	return nil
}
func (w *WechatV3) sharingFinishIdentity(raw []byte, r ChannelSharingFinishRequest) (ChannelSharingFinishResult, map[string]json.RawMessage, error) {
	out, e := v3Object(raw)
	if e != nil {
		return ChannelSharingFinishResult{}, nil, e
	}
	number, state := v3String(out, "order_id"), v3String(out, "state")
	if number == "" || r.ProviderOrderNo != "" && number != r.ProviderOrderNo || v3String(out, "out_order_no") != r.OrderNo || v3String(out, "transaction_id") != r.ProviderTradeNo || state != "PROCESSING" && state != "FINISHED" {
		return ChannelSharingFinishResult{}, nil, ErrAuthentication
	}
	return ChannelSharingFinishResult{OrderNo: r.OrderNo, ProviderOrderNo: number, ProviderTradeNo: r.ProviderTradeNo, State: "pending"}, out, nil
}
func (w *WechatV3) FinishChannelSharing(ctx context.Context, r ChannelSharingFinishRequest) (ChannelSharingFinishResult, error) {
	if e := w.validSharingFinish(r); e != nil {
		return ChannelSharingFinishResult{}, e
	}
	raw, e := w.call(ctx, "POST", "/v3/profitsharing/orders/unfreeze", map[string]any{"transaction_id": r.ProviderTradeNo, "out_order_no": r.OrderNo, "description": r.Description})
	if e != nil {
		return ChannelSharingFinishResult{}, e
	}
	result, _, e := w.sharingFinishIdentity(raw, r)
	if e != nil {
		return ChannelSharingFinishResult{}, &UnknownOutcome{Operation: "sharing-finish", Cause: e}
	}
	return result, nil
}

func (w *WechatV3) ChannelSharingRemaining(ctx context.Context, transaction string) (int64, error) {
	if !w.config.EnableChannelSharing {
		return 0, ErrUnsupported
	}
	if !sharingText(transaction, 32, true) || !wechatV3Identifier.MatchString(transaction) {
		return 0, ErrInvalid
	}
	raw, e := w.call(ctx, "GET", "/v3/profitsharing/transactions/"+url.PathEscape(transaction)+"/amounts", nil)
	if e != nil {
		return 0, e
	}
	out, e := v3Object(raw)
	if e != nil || v3String(out, "transaction_id") != transaction {
		return 0, ErrAuthentication
	}
	var amount *int64
	if json.Unmarshal(out["unsplit_amount"], &amount) != nil || amount == nil || *amount < 0 || *amount > 999999999999 {
		return 0, ErrAuthentication
	}
	return *amount, nil
}
func (w *WechatV3) QueryChannelSharingFinish(ctx context.Context, r ChannelSharingFinishRequest) (ChannelSharingFinishResult, error) {
	if e := w.validSharingFinish(r); e != nil {
		return ChannelSharingFinishResult{}, e
	}
	raw, e := w.call(ctx, "GET", "/v3/profitsharing/orders/"+url.PathEscape(r.OrderNo)+"?transaction_id="+url.QueryEscape(r.ProviderTradeNo), nil)
	if e != nil {
		return ChannelSharingFinishResult{}, e
	}
	result, out, e := w.sharingFinishIdentity(raw, r)
	if e != nil {
		return ChannelSharingFinishResult{}, e
	}
	var rows []json.RawMessage
	if value, exists := out["receivers"]; exists && json.Unmarshal(value, &rows) != nil {
		return ChannelSharingFinishResult{}, ErrAuthentication
	}
	finished := v3String(out, "state") == "FINISHED"
	if len(rows) == 0 {
		if finished {
			return ChannelSharingFinishResult{}, ErrAuthentication
		}
		return result, nil
	}
	if len(rows) != 1 {
		return ChannelSharingFinishResult{}, ErrAuthentication
	}
	receiver, e := v3Object(rows[0])
	if e != nil || v3String(receiver, "type") != "MERCHANT_ID" || v3String(receiver, "account") != w.config.MerchantID || v3Int(receiver, "amount") != r.ExpectedUnsplit {
		return ChannelSharingFinishResult{}, ErrAuthentication
	}
	switch v3String(receiver, "result") {
	case "SUCCESS":
		if finished {
			result.State = "succeeded"
			result.UnsplitAmount = r.ExpectedUnsplit
		}
	case "CLOSED":
		if finished {
			result.State = "failed"
		}
	case "PENDING":
		if finished {
			return ChannelSharingFinishResult{}, ErrAuthentication
		}
	default:
		return ChannelSharingFinishResult{}, ErrAuthentication
	}
	return result, nil
}
