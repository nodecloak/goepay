package payment

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mchTransferProvider(t *testing.T, enabled bool, handler func(method, path string, body map[string]json.RawMessage) (int, string)) *WechatV3 {
	t.Helper()
	merchant, platform := testKey(t), testKey(t)
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]json.RawMessage
		if len(raw) > 0 {
			json.Unmarshal(raw, &body)
		}
		status, out := handler(r.Method, r.URL.Path, body)
		v3SignedResponse(t, w, platform, status, out, "PUB_KEY_ID_LOCAL")
	}))
	t.Cleanup(up.Close)
	p, e := NewWechatV3(WechatV3Config{AppID: "local-app", MerchantID: "local-mch", MerchantSerial: "MERCHANT_SERIAL", PrivateKey: merchant, APIv3Key: []byte("0123456789abcdef0123456789abcdef"), VerificationKeys: map[string]*rsa.PublicKey{"PUB_KEY_ID_LOCAL": &platform.PublicKey}, Methods: []string{"native"}, MerchantTransfer: func() bool { return enabled }, HTTPClient: &http.Client{Transport: v3TLSTransport{up, t}}})
	if e != nil {
		t.Fatal(e)
	}
	return p
}

func mchTransferRequest() TransferRequest {
	return TransferRequest{TransferNo: "TF207", Type: "wxpay", Account: "trusted-buyer", Remark: "佣金报酬", Amount: 101, SceneID: "1000", ReportTypes: "岗位类型|报酬说明", ReportContents: "外卖员|7月份配送费"}
}

func TestWechatV3MerchantTransferSendsOriginalBillAndReturnsPackage(t *testing.T) {
	p := mchTransferProvider(t, true, func(method, path string, b map[string]json.RawMessage) (int, string) {
		if method != "POST" || path != "/v3/fund-app/mch-transfer/transfer-bills" {
			t.Error("wrong endpoint", method, path)
		}
		var app, no, scene, openid, remark string
		var amount int64
		var infos []map[string]string
		json.Unmarshal(b["appid"], &app)
		json.Unmarshal(b["out_bill_no"], &no)
		json.Unmarshal(b["transfer_scene_id"], &scene)
		json.Unmarshal(b["openid"], &openid)
		json.Unmarshal(b["transfer_remark"], &remark)
		json.Unmarshal(b["transfer_amount"], &amount)
		json.Unmarshal(b["transfer_scene_report_infos"], &infos)
		if app != "local-app" || no != "TF207" || scene != "1000" || openid != "trusted-buyer" || remark != "佣金报酬" || amount != 101 || len(infos) != 2 || infos[1]["info_content"] != "7月份配送费" {
			t.Error("wrong transfer bill", b)
		}
		if _, named := b["user_name"]; named {
			t.Error("unrequested real-name check sent")
		}
		return 200, `{"out_bill_no":"TF207","transfer_bill_no":"1330000071100999991182020050700019480001","create_time":"2025-01-01T12:00:00+08:00","state":"WAIT_USER_CONFIRM","package_info":"affffddafdfafddffda=="}`
	})
	res, err := p.Transfer(context.Background(), mchTransferRequest())
	if err != nil || res.State != "PROCESSING" || res.PackageInfo != "affffddafdfafddffda==" || res.ProviderTransferNo == "" {
		t.Fatal("pending-confirm bill not mapped", res, err)
	}
	mch, app, pkg := p.MerchantTransferConfirmation(res.PackageInfo)
	if mch != "local-mch" || app != "local-app" || pkg != res.PackageInfo {
		t.Fatal("confirmation parameters wrong")
	}
}

func TestWechatV3MerchantTransferQueryBindsBillAndStates(t *testing.T) {
	state := "SUCCESS"
	amount := "101"
	p := mchTransferProvider(t, true, func(method, path string, _ map[string]json.RawMessage) (int, string) {
		if method != "GET" || path != "/v3/fund-app/mch-transfer/transfer-bills/out-bill-no/TF207" {
			t.Error("wrong query", method, path)
		}
		return 200, `{"mch_id":"local-mch","appid":"local-app","out_bill_no":"TF207","transfer_bill_no":"1330000071100999991182020050700019480001","state":"` + state + `","transfer_amount":` + amount + `,"openid":"trusted-buyer","update_time":"2025-01-01T12:05:00+08:00"}`
	})
	for s, want := range map[string]string{"SUCCESS": "SUCCESS", "FAIL": "FAILED", "CANCELLED": "FAILED", "WAIT_USER_CONFIRM": "PROCESSING", "TRANSFERING": "PROCESSING"} {
		state = s
		res, err := p.QueryTransfer(context.Background(), mchTransferRequest())
		if err != nil || res.State != want {
			t.Error(s, res, err)
		}
	}
	state, amount = "SUCCESS", "102"
	if _, err := p.QueryTransfer(context.Background(), mchTransferRequest()); !errors.Is(err, ErrAuthentication) {
		t.Fatal("query with a different amount accepted", err)
	}
}

func TestWechatV3MerchantTransferRequiresChannelAndOriginalSettings(t *testing.T) {
	calls := 0
	off := mchTransferProvider(t, false, func(string, string, map[string]json.RawMessage) (int, string) { calls++; return 500, "" })
	if off.TransferTypes() != nil {
		t.Fatal("disabled channel offers transfers")
	}
	if _, err := off.Transfer(context.Background(), mchTransferRequest()); !errors.Is(err, ErrUnsupported) {
		t.Fatal("disabled channel transferred", err)
	}
	on := mchTransferProvider(t, true, func(string, string, map[string]json.RawMessage) (int, string) { calls++; return 500, "" })
	for name, mutate := range map[string]func(*TransferRequest){
		"no-scene":   func(r *TransferRequest) { r.SceneID = "" },
		"no-report":  func(r *TransferRequest) { r.ReportTypes = "" },
		"no-remark":  func(r *TransferRequest) { r.Remark = "" },
		"real-name":  func(r *TransferRequest) { r.Name = "张三" },
		"bad-number": func(r *TransferRequest) { r.TransferNo = "TF-1" },
		"long-type":  func(r *TransferRequest) { r.ReportTypes = "一二三四五六七八九十一二三四五六" },
	} {
		r := mchTransferRequest()
		mutate(&r)
		if _, err := on.Transfer(context.Background(), r); !errors.Is(err, ErrInvalid) {
			t.Error(name, "accepted", err)
		}
	}
	if calls != 0 {
		t.Fatal("invalid transfer reached WeChat")
	}
	preflight := mchTransferRequest()
	preflight.TransferNo, preflight.SceneID, preflight.ReportTypes = "TF0", "", ""
	if err := on.ValidateTransfer(preflight); err != nil || !on.UsesMerchantTransfer() {
		t.Fatal("preflight without site scene refused", err)
	}
	if err := off.ValidateTransfer(preflight); !errors.Is(err, ErrUnsupported) || !off.UsesMerchantTransfer() {
		t.Fatal("switched-off product validated", err)
	}
}

func TestWechatV3MerchantTransferSwitchOffStillQueriesExistingBills(t *testing.T) {
	queried := 0
	off := mchTransferProvider(t, false, func(method, path string, _ map[string]json.RawMessage) (int, string) {
		queried++
		return 200, `{"mch_id":"local-mch","appid":"local-app","out_bill_no":"TF207","transfer_bill_no":"1330000071100999991182020050700019480001","state":"SUCCESS","transfer_amount":101,"openid":"trusted-buyer","update_time":"2025-01-01T12:05:00+08:00"}`
	})
	res, err := off.QueryTransfer(context.Background(), mchTransferRequest())
	if err != nil || res.State != "SUCCESS" || queried != 1 {
		t.Fatal("pending bill cannot be settled after switch-off", res, err)
	}
	for _, ok := range []bool{ValidTransferReportInfos("岗位类型|报酬说明", "外卖员|7月份配送费"), ValidTransferReportInfos("岗位类型", "外卖员")} {
		if !ok {
			t.Fatal("original report settings rejected")
		}
	}
	for _, pair := range [][2]string{{"", "x"}, {"a", ""}, {"a||b", "x"}, {"a", "x\ny"}, {"1|2|3|4|5|6|7|8|9", "x"}} {
		if ValidTransferReportInfos(pair[0], pair[1]) {
			t.Fatal("bad report settings accepted", pair)
		}
	}
}
