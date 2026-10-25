package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
)

func (a *Alipay) ChannelSharingRelationAccount() string { return a.config.SellerID }

var alipaySharingUID = regexp.MustCompile(`^2088[0-9]{12}$`)
var alipayRelationOperationNo = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)

func (a *Alipay) ChannelSharingCapabilities() ChannelSharingCapabilities {
	if !a.config.EnableChannelSharing {
		return ChannelSharingCapabilities{}
	}
	return ChannelSharingCapabilities{Product: "alipay_direct", ReceiverTypes: []string{"userId", "loginName", "openId"}, ReturnSupported: a.config.EnableChannelSharingReturn, AutoFinishSupported: true}
}
func (a *Alipay) ChannelSharingRefundMode(method string) (string, error) {
	if !a.config.EnableChannelSharing || !enabled(method, a.config.Methods) {
		return "", ErrUnsupported
	}
	switch method {
	case "page", "wap", "app":
		return "merchant_funded", nil
	}
	return "", ErrUnsupported
}

// ChannelSharingRemaining reports the original transaction's unsettled sharing
// quota. Zero is not evidence that an independent finish operation succeeded.
func (a *Alipay) ChannelSharingRemaining(ctx context.Context, trade string) (int64, error) {
	if !a.config.EnableChannelSharing {
		return 0, ErrUnsupported
	}
	if !sharingText(trade, 64, true) {
		return 0, ErrInvalid
	}
	out, e := a.call(ctx, "alipay.trade.order.onsettle.query", map[string]any{"trade_no": trade, "royalty_source": "unsettled"}, false)
	if e != nil {
		return 0, e
	}
	var amount string
	if json.Unmarshal(out["unsettled_amount"], &amount) != nil || amount == "" {
		return 0, ErrAuthentication
	}
	n, e := sharingDecimal(out["unsettled_amount"])
	if e != nil || n < 0 || n > 999999999999 {
		return 0, ErrAuthentication
	}
	return n, nil
}
func (a *Alipay) validSharingReceiver(r SharingReceiver) bool {
	return sharingReceiverValid(r) && (r.Type == "userId" || r.Type == "loginName" || r.Type == "openId")
}
func (a *Alipay) ValidateChannelSharingReceiver(r SharingReceiver, remove bool) error {
	if !a.config.EnableChannelSharing {
		return ErrUnsupported
	}
	if !a.validSharingReceiver(r) || r.RelationType != "" || r.CustomRelation != "" || !remove && r.Type == "loginName" && r.Name == "" {
		return ErrInvalid
	}
	return nil
}
func (a *Alipay) ValidateChannelSharing(r ChannelSharingRequest) error {
	if !a.config.EnableChannelSharing {
		return ErrUnsupported
	}
	if !sharingRequestValid(r) {
		return ErrInvalid
	}
	for _, receiver := range r.Receivers {
		if !a.validSharingReceiver(receiver.SharingReceiver) {
			return ErrInvalid
		}
	}
	return nil
}
func (a *Alipay) sharingReceiver(ctx context.Context, operation string, r SharingReceiver, remove bool) error {
	if e := a.ValidateChannelSharingReceiver(r, remove); e != nil {
		return e
	}
	if !a.config.EnableChannelSharing {
		return ErrUnsupported
	}
	if !alipayRelationOperationNo.MatchString(operation) || !a.validSharingReceiver(r) {
		return ErrInvalid
	}
	method := "alipay.trade.royalty.relation.bind"
	receiver := map[string]string{"type": r.Type, "account": r.Account}
	if remove {
		method = "alipay.trade.royalty.relation.unbind"
	} else if r.Name != "" {
		receiver["name"] = r.Name
	}
	out, e := a.call(ctx, method, map[string]any{"out_request_no": operation, "receiver_list": []map[string]string{receiver}}, true)
	if e != nil {
		return e
	}
	switch text(out, "result_code") {
	case "SUCCESS":
		return nil
	case "FAIL":
		return &ProviderError{Code: "SHARING_RELATION_FAIL"}
	default:
		return &UnknownOutcome{Operation: method, Cause: ErrAuthentication}
	}
}
func (a *Alipay) BindChannelSharingReceiver(ctx context.Context, operation string, r SharingReceiver) error {
	return a.sharingReceiver(ctx, operation, r, false)
}
func (a *Alipay) UnbindChannelSharingReceiver(ctx context.Context, operation string, r SharingReceiver) error {
	return a.sharingReceiver(ctx, operation, r, true)
}
func pendingAliSharing(r ChannelSharingRequest) ChannelSharingResult {
	result := ChannelSharingResult{OrderNo: r.OrderNo, ProviderTradeNo: r.ProviderTradeNo, State: "pending"}
	for _, receiver := range r.Receivers {
		result.Receivers = append(result.Receivers, SharingReceiverResult{Type: receiver.Type, Account: receiver.Account, Amount: receiver.Amount, State: "pending"})
	}
	return result
}
func (a *Alipay) SubmitChannelSharing(ctx context.Context, r ChannelSharingRequest) (ChannelSharingResult, error) {
	if e := a.ValidateChannelSharing(r); e != nil {
		return ChannelSharingResult{}, e
	}
	receivers := []map[string]string{}
	for _, receiver := range r.Receivers {
		receivers = append(receivers, map[string]string{"trans_in_type": receiver.Type, "trans_in": receiver.Account, "amount": amountString(receiver.Amount), "desc": receiver.Description})
	}
	request := map[string]any{"out_request_no": r.OrderNo, "trade_no": r.ProviderTradeNo, "royalty_parameters": receivers, "royalty_mode": "async"}
	if r.UnfreezeUnsplit {
		request["extend_params"] = map[string]string{"royalty_finish": "true"}
	}
	out, e := a.call(ctx, "alipay.trade.order.settle", request, true)
	if e != nil {
		return ChannelSharingResult{}, e
	}
	number := text(out, "settle_no")
	if text(out, "trade_no") != r.ProviderTradeNo || !sharingText(number, 64, true) {
		return ChannelSharingResult{}, &UnknownOutcome{Operation: "sharing-submit", Cause: ErrAuthentication}
	}
	result := pendingAliSharing(r)
	result.ProviderOrderNo = number
	return result, nil
}
func sharingDecimal(raw json.RawMessage) (int64, error) {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		value = string(bytes.TrimSpace(raw))
	}
	if value == "0" || value == "0.0" || value == "0.00" {
		return 0, nil
	}
	return parseAmount(value)
}
func (a *Alipay) QueryChannelSharing(ctx context.Context, r ChannelSharingRequest) (ChannelSharingResult, error) {
	if e := a.ValidateChannelSharing(r); e != nil {
		return ChannelSharingResult{}, e
	}
	out, e := a.call(ctx, "alipay.trade.order.settle.query", map[string]any{"out_request_no": r.OrderNo, "trade_no": r.ProviderTradeNo}, false)
	if e != nil {
		return ChannelSharingResult{}, e
	}
	if text(out, "out_request_no") != r.OrderNo || text(out, "trade_no") != "" && text(out, "trade_no") != r.ProviderTradeNo {
		return ChannelSharingResult{}, ErrAuthentication
	}
	result := pendingAliSharing(r)
	result.ProviderOrderNo = text(out, "settle_no")
	expected := map[string]int{}
	for i, receiver := range r.Receivers {
		expected[sharingReceiverKey(receiver.Type, receiver.Account)] = i
	}
	var rows []json.RawMessage
	if json.Unmarshal(out["royalty_detail_list"], &rows) != nil {
		return ChannelSharingResult{}, ErrAuthentication
	}
	seen := map[string]bool{}
	allTerminal := true
	for _, row := range rows {
		receiver, e := v3Object(row)
		if e != nil {
			return ChannelSharingResult{}, e
		}
		kind, account := v3String(receiver, "trans_in_type"), v3String(receiver, "trans_in")
		if kind == "openId" && v3String(receiver, "trans_in_open_id") != "" {
			account = v3String(receiver, "trans_in_open_id")
		}
		key := sharingReceiverKey(kind, account)
		index, exists := expected[key]
		amount, e := sharingDecimal(receiver["amount"])
		if !exists || seen[key] || e != nil || amount != r.Receivers[index].Amount || v3String(receiver, "operation_type") != "transfer" {
			return ChannelSharingResult{}, ErrAuthentication
		}
		seen[key] = true
		canonical := ""
		switch v3String(receiver, "state") {
		case "SUCCESS":
			canonical = "succeeded"
		case "FAIL":
			canonical = "failed"
		case "PROCESSING", "INIT":
			canonical = "pending"
			allTerminal = false
		default:
			return ChannelSharingResult{}, ErrAuthentication
		}
		detail := v3String(receiver, "detail_id")
		if canonical == "succeeded" && detail == "" {
			return ChannelSharingResult{}, ErrAuthentication
		}
		result.Receivers[index].State = canonical
		result.Receivers[index].ProviderDetailNo = detail
		result.Receivers[index].FailureCode = v3String(receiver, "error_code")
	}
	result.State, e = sharingAggregate(result.Receivers, allTerminal && len(seen) == len(expected))
	return result, e
}
func (a *Alipay) validSharingReturn(r ChannelSharingReturnRequest) error {
	if !a.config.EnableChannelSharing || !a.config.EnableChannelSharingReturn {
		return ErrUnsupported
	}
	if !sharingOperationNo.MatchString(r.OrderNo) || !sharingOperationNo.MatchString(r.ReturnNo) || !sharingText(r.ProviderTradeNo, 64, true) || !a.validSharingReceiver(SharingReceiver{Type: r.ReceiverType, Account: r.ReceiverAccount}) || r.Amount <= 0 || r.Amount > 999999999999 || !sharingText(r.Description, 80, true) {
		return ErrInvalid
	}
	return nil
}
func (a *Alipay) ReturnChannelSharing(ctx context.Context, r ChannelSharingReturnRequest) (ChannelSharingReturnResult, error) {
	if e := a.validSharingReturn(r); e != nil {
		return ChannelSharingReturnResult{}, e
	}
	out, e := a.call(ctx, "alipay.trade.refund", map[string]any{"trade_no": r.ProviderTradeNo, "refund_amount": "0.00", "out_request_no": r.ReturnNo, "refund_reason": r.Description, "refund_royalty_parameters": []map[string]string{{"royalty_type": "transfer", "trans_out_type": r.ReceiverType, "trans_out": r.ReceiverAccount, "amount": amountString(r.Amount)}}}, true)
	if e != nil {
		return ChannelSharingReturnResult{}, e
	}
	amount, e := sharingDecimal(out["refund_fee"])
	if e != nil || amount != 0 || text(out, "trade_no") != r.ProviderTradeNo {
		return ChannelSharingReturnResult{}, &UnknownOutcome{Operation: "sharing-return", Cause: ErrAuthentication}
	}
	return ChannelSharingReturnResult{OrderNo: r.OrderNo, ReturnNo: r.ReturnNo, State: "pending", Amount: r.Amount}, nil
}
func (a *Alipay) QueryChannelSharingReturn(ctx context.Context, r ChannelSharingReturnRequest) (ChannelSharingReturnResult, error) {
	if e := a.validSharingReturn(r); e != nil {
		return ChannelSharingReturnResult{}, e
	}
	out, e := a.call(ctx, "alipay.trade.fastpay.refund.query", map[string]any{"trade_no": r.ProviderTradeNo, "out_request_no": r.ReturnNo}, false)
	if e != nil {
		return ChannelSharingReturnResult{}, e
	}
	amount, e := sharingDecimal(out["refund_amount"])
	if e != nil || amount != 0 || text(out, "trade_no") != r.ProviderTradeNo || text(out, "out_request_no") != r.ReturnNo || text(out, "refund_status") != "REFUND_SUCCESS" {
		return ChannelSharingReturnResult{}, ErrAuthentication
	}
	var rows []json.RawMessage
	if json.Unmarshal(out["refund_royaltys"], &rows) != nil || len(rows) != 1 {
		return ChannelSharingReturnResult{}, ErrAuthentication
	}
	royalty, e := v3Object(rows[0])
	if e != nil {
		return ChannelSharingReturnResult{}, e
	}
	returned, e := sharingDecimal(royalty["refund_amount"])
	if e != nil || returned != r.Amount || v3String(royalty, "royalty_type") != "" && v3String(royalty, "royalty_type") != "transfer" || v3String(royalty, "result_code") != "SUCCESS" {
		return ChannelSharingReturnResult{}, ErrAuthentication
	}
	actual := ""
	switch r.ReceiverType {
	case "userId":
		actual = v3String(royalty, "trans_out")
	case "loginName":
		actual = v3String(royalty, "trans_out_email")
	case "openId":
		actual = v3String(royalty, "ori_trans_out")
	}
	if actual != r.ReceiverAccount || v3String(royalty, "ori_trans_out") != "" && v3String(royalty, "ori_trans_out") != r.ReceiverAccount {
		return ChannelSharingReturnResult{}, ErrAuthentication
	}
	return ChannelSharingReturnResult{OrderNo: r.OrderNo, ReturnNo: r.ReturnNo, ProviderReturnNo: text(out, "refund_settlement_id"), State: "succeeded", Amount: r.Amount}, nil
}
