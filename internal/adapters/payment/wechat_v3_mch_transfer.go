package payment

import (
	"context"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

var v3TransferBillNo = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
var v3SceneID = regexp.MustCompile(`^[0-9]{1,8}$`)

// MerchantTransferProduct is a payout product whose wxpay bills may wait for
// the payee to confirm in WeChat (requestMerchantTransfer). Such bills need
// the site's scene/report settings, which the payment flow supplies.
type MerchantTransferProduct interface {
	UsesMerchantTransfer() bool
	MerchantTransferConfirmation(packageInfo string) (mchID, appID, pkg string)
}

// merchantTransferOn is the site switch 微信V3转账产品类型 = 商家转账（新）.
func (w *WechatV3) merchantTransferOn() bool {
	return w.config.MerchantTransfer != nil && w.config.MerchantTransfer()
}

// UsesMerchantTransfer reports whether this channel can carry 商家转账（新）
// bills at all. Queries stay possible after the site switch is turned off.
func (w *WechatV3) UsesMerchantTransfer() bool { return w.config.MerchantTransfer != nil }

// TransferProductIdentity names the 商家转账（新） product for payout authority.
func (w *WechatV3) TransferProductIdentity() string {
	if !w.UsesMerchantTransfer() {
		return ""
	}
	return "wechat.v3.mch-transfer.transfer-bills"
}

// TransferTypes offers wxpay payouts only while the site selects 商家转账（新）.
func (w *WechatV3) TransferTypes() []string {
	if !w.merchantTransferOn() {
		return nil
	}
	return []string{"wxpay"}
}

// ValidateTransfer checks a payout before any reservation. Scene and report
// settings are checked when the bill is sent, after the flow adds them.
func (w *WechatV3) ValidateTransfer(r TransferRequest) error {
	if !w.merchantTransferOn() {
		return ErrUnsupported
	}
	if !validMerchantTransferBase(r) {
		return ErrInvalid
	}
	return nil
}

func validMerchantTransferBase(r TransferRequest) bool {
	return r.Type == "wxpay" && v3TransferBillNo.MatchString(r.TransferNo) && r.Account != "" && len(r.Account) <= 128 && trustedBuyer(r.Account) && r.Amount > 0 && r.Name == "" &&
		r.Remark != "" && utf8.RuneCountInString(r.Remark) <= 32 && utf8.ValidString(r.Remark) && !strings.ContainsAny(r.Remark, "\x00\r\n")
}

func validMerchantTransfer(r TransferRequest) bool {
	if !validMerchantTransferBase(r) || !v3SceneID.MatchString(r.SceneID) {
		return false
	}
	return ValidTransferReportInfos(r.ReportTypes, r.ReportContents)
}

// ValidTransferReportInfos checks the original '|'-separated report types
// (each <= 15 chars) and contents (each <= 32 chars), at most 8 of each.
func ValidTransferReportInfos(types, contents string) bool {
	return ValidTransferReportList(types, 15) && ValidTransferReportList(contents, 32)
}

// ValidTransferReportList checks one '|'-separated report setting.
func ValidTransferReportList(value string, limit int) bool {
	parts := strings.Split(value, "|")
	if value == "" || len(parts) > 8 {
		return false
	}
	for _, v := range parts {
		if v == "" || !utf8.ValidString(v) || utf8.RuneCountInString(v) > limit || strings.ContainsAny(v, "\x00\r\n\t") {
			return false
		}
	}
	return true
}

// reportInfos mirrors the original: one entry per '|' type, with the
// same-index content or else the first content.
func reportInfos(r TransferRequest) []map[string]string {
	if r.ReportTypes == "" || r.ReportContents == "" {
		return nil
	}
	contents := strings.Split(r.ReportContents, "|")
	var out []map[string]string
	for i, typ := range strings.Split(r.ReportTypes, "|") {
		content := contents[0]
		if i < len(contents) {
			content = contents[i]
		}
		out = append(out, map[string]string{"info_type": typ, "info_content": content})
	}
	return out
}

// merchantTransferState maps a transfer-bill state. Everything before a
// terminal SUCCESS/FAIL/CANCELLED (including WAIT_USER_CONFIRM) is
// PROCESSING; an unrecognized state is UNKNOWN and must be queried.
func merchantTransferState(s string) string {
	switch s {
	case "SUCCESS":
		return "SUCCESS"
	case "FAIL", "CANCELLED":
		return "FAILED"
	case "ACCEPTED", "PROCESSING", "WAIT_USER_CONFIRM", "TRANSFERING", "CANCELING":
		return "PROCESSING"
	}
	return "UNKNOWN"
}

func (w *WechatV3) merchantTransferResult(raw []byte, r TransferRequest, created bool) (TransferResult, error) {
	out, e := v3Object(raw)
	if e != nil {
		return TransferResult{}, e
	}
	state := v3String(out, "state")
	bill := v3String(out, "transfer_bill_no")
	if v3String(out, "out_bill_no") != r.TransferNo || bill == "" || merchantTransferState(state) == "UNKNOWN" {
		return TransferResult{}, ErrAuthentication
	}
	if !created {
		if v3String(out, "mch_id") != "" && v3String(out, "mch_id") != w.config.MerchantID || v3String(out, "appid") != "" && v3String(out, "appid") != w.config.AppID ||
			v3Int(out, "transfer_amount") != r.Amount || v3String(out, "openid") != r.Account {
			return TransferResult{}, ErrAuthentication
		}
	}
	res := TransferResult{TransferNo: r.TransferNo, ProviderTransferNo: bill, State: merchantTransferState(state), Amount: r.Amount}
	if state == "SUCCESS" {
		res.PayDate = v3String(out, "update_time")
	}
	if state == "WAIT_USER_CONFIRM" {
		res.PackageInfo = v3Text(out, "package_info", 2048)
		if res.PackageInfo == "" && created {
			return TransferResult{}, ErrAuthentication
		}
	}
	return res, nil
}

// Transfer creates a 商家转账（新） bill (POST /v3/fund-app/mch-transfer/
// transfer-bills). A bill waiting for the payee carries package_info, which
// the payee confirms in WeChat via requestMerchantTransfer. A real-name
// check (encrypted user_name) is not offered; a name is rejected rather
// than silently dropped.
func (w *WechatV3) Transfer(ctx context.Context, r TransferRequest) (TransferResult, error) {
	if !w.merchantTransferOn() {
		return TransferResult{}, ErrUnsupported
	}
	if !validMerchantTransfer(r) {
		return TransferResult{}, ErrInvalid
	}
	infos := reportInfos(r)
	request := map[string]any{"appid": w.config.AppID, "out_bill_no": r.TransferNo, "transfer_scene_id": r.SceneID, "openid": r.Account, "transfer_amount": r.Amount, "transfer_remark": r.Remark, "transfer_scene_report_infos": infos}
	raw, e := w.call(ctx, "POST", "/v3/fund-app/mch-transfer/transfer-bills", request)
	if e != nil {
		return TransferResult{}, e
	}
	res, e := w.merchantTransferResult(raw, r, true)
	if e != nil {
		return TransferResult{}, &UnknownOutcome{Operation: "transfer", Cause: e}
	}
	return res, nil
}

// QueryTransfer reads a bill by its out_bill_no and binds it to the request.
func (w *WechatV3) QueryTransfer(ctx context.Context, r TransferRequest) (TransferResult, error) {
	if !w.UsesMerchantTransfer() {
		return TransferResult{}, ErrUnsupported
	}
	if r.Type != "wxpay" || !v3TransferBillNo.MatchString(r.TransferNo) || r.Account == "" || r.Amount <= 0 {
		return TransferResult{}, ErrInvalid
	}
	raw, e := w.call(ctx, "GET", "/v3/fund-app/mch-transfer/transfer-bills/out-bill-no/"+url.PathEscape(r.TransferNo), nil)
	if e != nil {
		return TransferResult{}, e
	}
	return w.merchantTransferResult(raw, r, false)
}

// MerchantTransferConfirmation is what the payee page passes to
// WeixinJSBridge requestMerchantTransfer.
func (w *WechatV3) MerchantTransferConfirmation(packageInfo string) (mchID, appID, pkg string) {
	return w.config.MerchantID, w.config.AppID, packageInfo
}

var _ TransferProvider = (*WechatV3)(nil)
var _ TransferValidator = (*WechatV3)(nil)
var _ MerchantTransferProduct = (*WechatV3)(nil)
