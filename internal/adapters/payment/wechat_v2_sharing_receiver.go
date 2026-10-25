package payment

import (
	"context"
	"encoding/json"
	"unicode/utf8"
)

func (w *WechatV2) ChannelSharingRelationAccount() string { return w.config.MerchantID }

func (w *WechatV2) ChannelSharingCapabilities() ChannelSharingCapabilities {
	if w == nil || !w.config.EnableChannelSharing {
		return ChannelSharingCapabilities{}
	}
	return ChannelSharingCapabilities{Product: "wxpay_v2_single_direct", ReceiverTypes: []string{"MERCHANT_ID", "PERSONAL_OPENID"}, ReturnSupported: true, FinishSupported: true, AutoFinishSupported: true}
}

func (w *WechatV2) ValidateChannelSharingReceiver(r SharingReceiver, remove bool) error {
	if e := w.validateSharingV2Transport(false); e != nil {
		return e
	}
	if r.Type != "MERCHANT_ID" && r.Type != "PERSONAL_OPENID" || !sharingText(r.Account, 64, true) || !sharingText(r.Name, 1024, false) {
		return ErrInvalid
	}
	if remove {
		return nil
	}
	if r.Type == "MERCHANT_ID" && r.Name == "" {
		return ErrInvalid
	}
	switch r.RelationType {
	case "SERVICE_PROVIDER", "STORE", "STAFF", "STORE_OWNER", "PARTNER", "HEADQUARTER", "BRAND", "DISTRIBUTOR", "USER", "SUPPLIER":
		if r.CustomRelation != "" {
			return ErrInvalid
		}
	case "CUSTOM":
		if !sharingText(r.CustomRelation, 40, true) || utf8.RuneCountInString(r.CustomRelation) > 10 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

func (w *WechatV2) sharingV2Receiver(ctx context.Context, operation string, r SharingReceiver, remove bool) error {
	if !sharingOperationNo.MatchString(operation) {
		return ErrInvalid
	}
	if e := w.ValidateChannelSharingReceiver(r, remove); e != nil {
		return e
	}
	endpoint := "/pay/profitsharingaddreceiver"
	receiver := map[string]string{"type": r.Type, "account": r.Account}
	if remove {
		endpoint = "/pay/profitsharingremovereceiver"
	} else {
		receiver["relation_type"] = r.RelationType
		if r.Name != "" {
			receiver["name"] = r.Name
		}
		if r.CustomRelation != "" {
			receiver["custom_relation"] = r.CustomRelation
		}
	}
	raw, e := json.Marshal(receiver)
	if e != nil {
		return ErrInvalid
	}
	out, e := w.sharingV2Call(ctx, endpoint, map[string]string{"receiver": string(raw)}, true, false, true)
	if e != nil {
		return e
	}
	if providerUniqueJSON([]byte(out["receiver"])) != nil {
		return &UnknownOutcome{Operation: "wechat-v2-sharing-receiver", Cause: ErrAuthentication}
	}
	var got map[string]json.RawMessage
	if json.Unmarshal([]byte(out["receiver"]), &got) != nil {
		return &UnknownOutcome{Operation: "wechat-v2-sharing-receiver", Cause: ErrAuthentication}
	}
	var kind, account string
	if json.Unmarshal(got["type"], &kind) != nil || json.Unmarshal(got["account"], &account) != nil || kind != r.Type || account != r.Account {
		return &UnknownOutcome{Operation: "wechat-v2-sharing-receiver", Cause: ErrAuthentication}
	}
	return nil
}

func (w *WechatV2) BindChannelSharingReceiver(ctx context.Context, operation string, r SharingReceiver) error {
	return w.sharingV2Receiver(ctx, operation, r, false)
}
func (w *WechatV2) UnbindChannelSharingReceiver(ctx context.Context, operation string, r SharingReceiver) error {
	return w.sharingV2Receiver(ctx, operation, r, true)
}
