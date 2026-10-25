package payment

import (
	"context"
	"encoding/json"
	"strconv"
)

func (w *WechatV2) validSharingV2Return(r ChannelSharingReturnRequest) error {
	if e := w.validateSharingV2Transport(false); e != nil {
		return e
	}
	if !sharingOperationNo.MatchString(r.OrderNo) || !sharingOperationNo.MatchString(r.ReturnNo) || !sharingOperationNo.MatchString(r.ProviderOrderNo) || r.ReceiverType != "MERCHANT_ID" || !sharingText(r.ReceiverAccount, 64, true) || r.Amount <= 0 || r.Amount > 999999999999 || !sharingText(r.Description, 80, true) {
		return ErrInvalid
	}
	return nil
}

func (w *WechatV2) sharingV2ReturnResult(out map[string]string, r ChannelSharingReturnRequest) (ChannelSharingReturnResult, error) {
	if out["out_order_no"] != r.OrderNo || out["order_id"] != r.ProviderOrderNo || out["out_return_no"] != r.ReturnNo || out["return_account_type"] != r.ReceiverType || out["return_account"] != r.ReceiverAccount || out["return_amount"] != strconv.FormatInt(r.Amount, 10) || !sharingOperationNo.MatchString(out["return_no"]) {
		return ChannelSharingReturnResult{}, ErrAuthentication
	}
	state := ""
	switch out["result"] {
	case "PROCESSING":
		state = "pending"
	case "SUCCESS":
		state = "succeeded"
	case "FAILED":
		state = "failed"
	default:
		return ChannelSharingReturnResult{}, ErrAuthentication
	}
	return ChannelSharingReturnResult{OrderNo: r.OrderNo, ReturnNo: r.ReturnNo, ProviderReturnNo: out["return_no"], State: state, Amount: r.Amount}, nil
}

func (w *WechatV2) ReturnChannelSharing(ctx context.Context, r ChannelSharingReturnRequest) (ChannelSharingReturnResult, error) {
	if e := w.validSharingV2Return(r); e != nil {
		return ChannelSharingReturnResult{}, e
	}
	out, e := w.sharingV2Call(ctx, "/secapi/pay/profitsharingreturn", map[string]string{"out_order_no": r.OrderNo, "out_return_no": r.ReturnNo, "return_account_type": r.ReceiverType, "return_account": r.ReceiverAccount, "return_amount": strconv.FormatInt(r.Amount, 10), "description": r.Description}, true, true, true)
	if e != nil {
		return ChannelSharingReturnResult{}, e
	}
	result, e := w.sharingV2ReturnResult(out, r)
	if e != nil {
		return ChannelSharingReturnResult{}, &UnknownOutcome{Operation: "wechat-v2-sharing-return", Cause: e}
	}
	return result, nil
}

func (w *WechatV2) QueryChannelSharingReturn(ctx context.Context, r ChannelSharingReturnRequest) (ChannelSharingReturnResult, error) {
	if e := w.validSharingV2Return(r); e != nil {
		return ChannelSharingReturnResult{}, e
	}
	out, e := w.sharingV2Call(ctx, "/pay/profitsharingreturnquery", map[string]string{"out_order_no": r.OrderNo, "out_return_no": r.ReturnNo}, false, false, true)
	if e != nil {
		return ChannelSharingReturnResult{}, e
	}
	return w.sharingV2ReturnResult(out, r)
}

func (w *WechatV2) validSharingV2Finish(r ChannelSharingFinishRequest) error {
	if e := w.validateSharingV2Transport(false); e != nil {
		return e
	}
	if !sharingOperationNo.MatchString(r.OrderNo) || r.ProviderOrderNo != "" && !sharingOperationNo.MatchString(r.ProviderOrderNo) || !sharingText(r.ProviderTradeNo, 32, true) || !scanCancellationNumber.MatchString(r.ProviderTradeNo) || !sharingText(r.Description, 80, true) || r.ExpectedUnsplit <= 0 || r.ExpectedUnsplit > 999999999999 {
		return ErrInvalid
	}
	return nil
}

func (w *WechatV2) sharingV2FinishIdentity(out map[string]string, r ChannelSharingFinishRequest) (ChannelSharingFinishResult, error) {
	if out["out_order_no"] != r.OrderNo || out["transaction_id"] != r.ProviderTradeNo || !sharingOperationNo.MatchString(out["order_id"]) || r.ProviderOrderNo != "" && out["order_id"] != r.ProviderOrderNo {
		return ChannelSharingFinishResult{}, ErrAuthentication
	}
	return ChannelSharingFinishResult{OrderNo: r.OrderNo, ProviderOrderNo: out["order_id"], ProviderTradeNo: r.ProviderTradeNo, State: "pending"}, nil
}

func (w *WechatV2) FinishChannelSharing(ctx context.Context, r ChannelSharingFinishRequest) (ChannelSharingFinishResult, error) {
	if e := w.validSharingV2Finish(r); e != nil {
		return ChannelSharingFinishResult{}, e
	}
	out, e := w.sharingV2Call(ctx, "/secapi/pay/profitsharingfinish", map[string]string{"out_order_no": r.OrderNo, "transaction_id": r.ProviderTradeNo, "description": r.Description}, true, true, true)
	if e != nil {
		return ChannelSharingFinishResult{}, e
	}
	result, e := w.sharingV2FinishIdentity(out, r)
	if e != nil {
		return ChannelSharingFinishResult{}, &UnknownOutcome{Operation: "wechat-v2-sharing-finish", Cause: e}
	}
	// Acceptance is not completion. Only the original operation query can
	// supply the explicit released amount needed by the funding ledger.
	return result, nil
}

func (w *WechatV2) QueryChannelSharingFinish(ctx context.Context, r ChannelSharingFinishRequest) (ChannelSharingFinishResult, error) {
	if e := w.validSharingV2Finish(r); e != nil {
		return ChannelSharingFinishResult{}, e
	}
	out, e := w.sharingV2Call(ctx, "/pay/profitsharingquery", map[string]string{"out_order_no": r.OrderNo, "transaction_id": r.ProviderTradeNo}, false, false, false)
	if e != nil {
		return ChannelSharingFinishResult{}, e
	}
	result, e := w.sharingV2FinishIdentity(out, r)
	if e != nil {
		return ChannelSharingFinishResult{}, e
	}
	if out["status"] != "PROCESSING" && out["status"] != "FINISHED" || providerUniqueJSON([]byte(out["receivers"])) != nil {
		return ChannelSharingFinishResult{}, ErrAuthentication
	}
	var rows []json.RawMessage
	if json.Unmarshal([]byte(out["receivers"]), &rows) != nil {
		return ChannelSharingFinishResult{}, ErrAuthentication
	}
	finished := out["status"] == "FINISHED"
	if len(rows) == 0 && !finished {
		return result, nil
	}
	if len(rows) != 1 {
		return ChannelSharingFinishResult{}, ErrAuthentication
	}
	row, e := v3Object(rows[0])
	if e != nil || v3String(row, "type") != "MERCHANT_ID" || v3String(row, "account") != w.config.MerchantID || v3Int(row, "amount") != r.ExpectedUnsplit {
		return ChannelSharingFinishResult{}, ErrAuthentication
	}
	switch v3String(row, "result") {
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

var _ ChannelSharingProvider = (*WechatV2)(nil)
var _ ChannelSharingFinisher = (*WechatV2)(nil)
