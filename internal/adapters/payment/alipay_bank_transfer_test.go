package payment

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAlipayBankCertificateCapabilityAndAuthenticatedEvidence(t *testing.T) {
	cfg, key, cert, _ := alipayCertificateFixture(t)
	r := TransferRequest{TransferNo: "TF42", Type: "bank", Account: "6222020000000000", Name: "Synthetic Person", Amount: 100, Remark: "local test"}
	a, e := NewAlipay(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = a.ValidateTransfer(r); !errors.Is(e, ErrUnsupported) {
		t.Fatal("bank enabled by default", e)
	}
	cfg.EnableBankTransfer = true
	cfg.BankTransferScene = "企业退款"
	cfg.BankTransferReports = []AlipayTransferSceneReport{{InfoType: "退款原因", InfoContent: "本地测试"}}
	sn, _ := alipayCertSN(cert)
	var phase atomic.Value
	phase.Store("submit")
	var calls atomic.Int32
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		r.ParseForm()
		params := map[string]string{}
		for k, v := range r.PostForm {
			params[k] = v[0]
		}
		want, _ := SignAlipayRSA2(params, cfg.PrivateKey)
		if params["sign"] != want || params["app_cert_sn"] == "" {
			t.Error("request not certificate-signed")
		}
		var biz map[string]any
		json.Unmarshal([]byte(params["biz_content"]), &biz)
		if biz["out_biz_no"] != "TF42" || biz["product_code"] != "TRANS_BANKCARD_NO_PWD" || biz["biz_scene"] != "DIRECT_TRANSFER" {
			t.Error("wrong bank product", biz)
		}
		out := map[string]any{"code": "10000", "out_biz_no": "TF42", "order_id": "ali-bank42", "status": "SUCCESS"}
		if phase.Load().(string) == "submit" {
			payee := biz["payee_info"].(map[string]any)
			if params["method"] != "alipay.fund.trans.uni.transfer" || payee["identity_type"] != "BANKCARD_ACCOUNT" || payee["identity"] != "6222020000000000" || payee["name"] != "Synthetic Person" || payee["bankcard_ext_info"].(map[string]any)["account_type"] != "2" || biz["transfer_scene_name"] != "企业退款" {
				t.Error("bank identity not fixed", biz)
			}
			out["trans_date"] = "2026-10-23 12:00:00"
		} else {
			if params["method"] != "alipay.fund.trans.common.query" {
				t.Error(params["method"])
			}
			out["trans_amount"] = "1.00"
			out["order_fee"] = "0.12"
			out["pay_date"] = "2026-10-23 12:00:00"
			switch phase.Load().(string) {
			case "refund":
				out["status"] = "REFUND"
				delete(out, "trans_amount")
			case "bad-amount":
				out["trans_amount"] = "2.00"
			case "bad-tf":
				out["out_biz_no"] = "TF99"
			case "zero-fee":
				out["order_fee"] = "0.00"
			}
		}
		node, _ := json.Marshal(out)
		sign, _ := signRSA(node, key)
		json.NewEncoder(w).Encode(map[string]any{strings.ReplaceAll(params["method"], ".", "_") + "_response": json.RawMessage(node), "sign": sign, "alipay_cert_sn": sn})
	}))
	defer up.Close()
	cfg.GatewayURL = up.URL
	cfg.HTTPClient = up.Client()
	a, e = NewAlipay(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if len(a.TransferTypes()) != 2 || a.BankTransferProduct() != BankProductAlipay {
		t.Fatal("bank capability absent")
	}
	if e = a.ValidateTransfer(r); e != nil {
		t.Fatal(e)
	}
	bad := r
	bad.BankCode = "1002"
	if e = a.ValidateTransfer(bad); e == nil {
		t.Fatal("WeChat bank code accepted by Ali")
	}
	result, e := a.Transfer(context.Background(), r)
	if e != nil || result.AlipayBank == nil || !result.AlipayBank.SubmissionBound || result.AlipayBank.AmountKnown || result.State != "PROCESSING" {
		t.Fatal("synchronous ack fabricated final amount", result, e)
	}
	phase.Store("query")
	result, e = a.QueryTransfer(context.Background(), r)
	if e != nil || !result.AlipayBank.AmountKnown || result.State != "SUCCESS" || result.BankFeeKnown || !result.AlipayBank.EstimatedFeeKnown || result.AlipayBank.EstimatedFee != 12 {
		t.Fatal(result, e)
	}
	phase.Store("refund")
	result, e = a.QueryTransfer(context.Background(), r)
	if e != nil || result.State != "REFUND" || result.AlipayBank.AmountKnown {
		t.Fatal("refund without amount was discarded or finalized", result, e)
	}
	for _, value := range []string{"bad-amount", "bad-tf"} {
		phase.Store(value)
		if _, e = a.QueryTransfer(context.Background(), r); !errors.Is(e, ErrAuthentication) {
			t.Fatal("mismatch accepted", value, e)
		}
	}
	if calls.Load() != 5 {
		t.Fatal(calls.Load())
	}
	phase.Store("zero-fee")
	result, e = a.QueryTransfer(context.Background(), r)
	if e != nil || !result.AlipayBank.EstimatedFeeKnown || result.AlipayBank.EstimatedFee != 0 || result.State != "SUCCESS" {
		t.Fatal("valid zero fee rejected", result, e)
	}
	phase.Store("query")
	disabled := cfg
	disabled.EnableBankTransfer = false
	readOnly, e := NewAlipay(disabled)
	if e != nil {
		t.Fatal(e)
	}
	if len(readOnly.TransferTypes()) != 1 {
		t.Fatal("disabled bank advertised")
	}
	if _, e = readOnly.QueryTransfer(context.Background(), r); e != nil {
		t.Fatal("write switch disabled recovery query", e)
	}
	zero := r
	zero.TransferNo = "TF0"
	if _, e = readOnly.QueryTransfer(context.Background(), zero); !errors.Is(e, ErrInvalid) {
		t.Fatal("preflight placeholder reached provider query", e)
	}
	pub := cfg
	pub.CertificateMode = nil
	pub.PublicKey = &key.PublicKey
	if _, e = NewAlipay(pub); e == nil {
		t.Fatal("unverified public-mode bank capability enabled")
	}
}
