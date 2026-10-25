package payment

import (
	"context"
	"net/url"
	"strings"
)

func (a *Alipay) ChannelBalance(ctx context.Context, userID string) (ChannelBalanceResult, error) {
	if userID == "" || len(userID) > 128 || strings.ContainsAny(userID, "\r\n") {
		return ChannelBalanceResult{}, ErrInvalid
	}
	biz := map[string]any{"account_type": "ACCTRANS_ACCOUNT"}
	if strings.HasPrefix(userID, "2088") && onlyDigits(userID) {
		biz["alipay_user_id"] = userID
	} else {
		biz["alipay_open_id"] = userID
	}
	out, e := a.call(ctx, "alipay.fund.account.query", biz, false)
	if e != nil {
		return ChannelBalanceResult{}, e
	}
	available, e := parseNonnegativeAmount(text(out, "available_amount"))
	if e != nil {
		return ChannelBalanceResult{}, ErrAuthentication
	}
	frozen, e := parseNonnegativeAmount(text(out, "freeze_amount"))
	if e != nil {
		return ChannelBalanceResult{}, ErrAuthentication
	}
	product, scene := "TRANS_ACCOUNT_NO_PWD", "DIRECT_TRANSFER"
	if a.certificates != nil {
		product, scene = "DEFAULT", "DEFAULT"
	}
	quota, e := a.call(ctx, "alipay.fund.quota.query", map[string]any{"product_code": product, "biz_scene": scene}, false)
	if e != nil {
		return ChannelBalanceResult{}, e
	}
	result := ChannelBalanceResult{Available: available, Frozen: frozen, Quota: map[string]string{}}
	for _, field := range []string{"new_quota_single_max", "new_quota_daily_remain", "new_quota_monthly_remain", "to_corporate_daily_available_amount", "to_private_daily_available_amount", "to_corporate_monthly_available_amount", "to_private_monthly_available_amount"} {
		if value := text(quota, field); value != "" {
			if _, e := parseNonnegativeAmount(value); e != nil {
				return ChannelBalanceResult{}, ErrAuthentication
			}
			result.Quota[field] = value
		}
	}
	return result, nil
}
func parseNonnegativeAmount(s string) (int64, error) {
	if s == "0" || s == "0.0" || s == "0.00" {
		return 0, nil
	}
	return parseAmount(s)
}

func (a *Alipay) ApplyTransferProof(ctx context.Context, providerID string) (TransferProofResult, error) {
	if providerID == "" || len(providerID) > 128 || strings.ContainsAny(providerID, "\r\n") {
		return TransferProofResult{}, ErrInvalid
	}
	out, e := a.call(ctx, "alipay.data.bill.ereceipt.apply", map[string]any{"type": "FUND_DETAIL", "key": providerID}, true)
	if e != nil {
		return TransferProofResult{}, e
	}
	fileID := text(out, "file_id")
	if fileID == "" || len(fileID) > 128 || strings.ContainsAny(fileID, "\r\n") {
		return TransferProofResult{}, &UnknownOutcome{"proof apply", ErrAuthentication}
	}
	return TransferProofResult{FileID: fileID, State: "PROCESSING"}, nil
}
func (a *Alipay) QueryTransferProof(ctx context.Context, fileID string) (TransferProofResult, error) {
	if fileID == "" || len(fileID) > 128 || strings.ContainsAny(fileID, "\r\n") {
		return TransferProofResult{}, ErrInvalid
	}
	out, e := a.call(ctx, "alipay.data.bill.ereceipt.query", map[string]any{"file_id": fileID}, false)
	if e != nil {
		return TransferProofResult{}, e
	}
	if returned := text(out, "file_id"); returned != "" && returned != fileID {
		return TransferProofResult{}, ErrAuthentication
	}
	result := TransferProofResult{FileID: fileID, State: transferState(text(out, "status"))}
	if result.State == "SUCCESS" {
		u, e := url.Parse(text(out, "download_url"))
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.ContainsAny(u.String(), "\r\n") {
			return TransferProofResult{}, ErrAuthentication
		}
		result.DownloadURL = u.String()
	}
	return result, nil
}

var _ ChannelBalanceProvider = (*Alipay)(nil)
var _ TransferProofProvider = (*Alipay)(nil)
