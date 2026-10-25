package payment

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWechatBankEncryptsRecipientAndVerifiesQueryIdentity(t *testing.T) {
	bankKey := testKey(t)
	apiKey := "0123456789abcdef0123456789abcdef"
	state := "SUCCESS"
	mismatch := false
	calls := 0
	card, name := "6222020000000000000", "测试收款人"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		raw, _ := io.ReadAll(r.Body)
		p, e := decodeXML(raw)
		if e != nil || !VerifyWechatV2(p, apiKey, "MD5", p["sign"]) || p["mch_id"] != "mch" || p["partner_trade_no"] != "TF00000000000000000042" || p["appid"] != "" {
			t.Error("bank request identity/signature", p, e)
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "mch_id": "mch", "partner_trade_no": "TF00000000000000000042", "payment_no": "bank42", "amount": "100", "cmms_amt": "5"}
		switch r.URL.Path {
		case "/mmpaysptrans/pay_bank":
			if bytes.Contains(raw, []byte(card)) || bytes.Contains(raw, []byte(name)) {
				t.Error("recipient leaked in plaintext")
			}
			for field, want := range map[string]string{"enc_bank_no": card, "enc_true_name": name} {
				cipher, e := base64.StdEncoding.DecodeString(p[field])
				if e != nil {
					t.Error(e)
				}
				plain, e := rsa.DecryptOAEP(sha1.New(), rand.Reader, bankKey, cipher, nil)
				if e != nil || string(plain) != want {
					t.Error("wrong legacy OAEP payload", field, e)
				}
			}
			if p["bank_code"] != "1002" {
				t.Error("wrong explicit bank code")
			}
		case "/mmpaysptrans/query_bank":
			out["bank_no_md5"] = fmt.Sprintf("%X", md5.Sum([]byte(card)))
			out["true_name_md5"] = fmt.Sprintf("%X", md5.Sum([]byte(name)))
			out["status"] = state
			out["pay_succ_time"] = "2026-10-23 12:00:00"
			if mismatch {
				out["bank_no_md5"] = "00000000000000000000000000000000"
			}
		default:
			t.Error(r.URL.Path)
		}
		out["sign"], _ = SignWechatV2(out, apiKey, "MD5")
		w.Write(encodeXML(out))
	}))
	defer upstream.Close()
	p, e := NewWechatV2(WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: apiKey, BankPublicKey: &bankKey.PublicKey, BaseURL: upstream.URL, HTTPClient: upstream.Client(), RefundClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	r := TransferRequest{TransferNo: "TF42", Type: "bank", Account: card, Name: name, BankCode: "1002", Amount: 100, Remark: "test"}
	result, e := p.Transfer(context.Background(), r)
	if e != nil || result.State != "PROCESSING" || result.TransferNo != "TF42" || result.Amount != 100 || !result.BankFeeKnown || result.BankFeeAmount != 5 {
		t.Fatal("bank acceptance falsely settled", result, e)
	}
	result, e = p.QueryTransfer(context.Background(), r)
	if e != nil || result.State != "SUCCESS" || result.TransferNo != "TF42" || !result.BankFeeKnown || result.BankFeeAmount != 5 {
		t.Fatal(result, e)
	}
	state = "BANK_FAIL"
	result, e = p.QueryTransfer(context.Background(), r)
	if e != nil || result.State != "BANK_FAIL" {
		t.Fatal("bank returned payment normalized to ordinary failure", result, e)
	}
	mismatch = true
	if _, e = p.QueryTransfer(context.Background(), r); e == nil {
		t.Fatal("different bank recipient accepted")
	}
	if calls != 4 {
		t.Fatal(calls)
	}
}

func TestWechatBankPreflightAndCapabilityFailClosed(t *testing.T) {
	key := testKey(t)
	config := WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: "0123456789abcdef0123456789abcdef"}
	p, e := NewWechatV2(config)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.TransferTypes()) != 0 {
		t.Fatal("bank advertised without mTLS")
	}
	config.RefundClient = &http.Client{}
	p, e = NewWechatV2(config)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.TransferTypes()) != 1 {
		t.Fatal("bank advertised without RSA key")
	}
	config.BankPublicKey = &key.PublicKey
	p, e = NewWechatV2(config)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.TransferTypes()) != 2 || p.TransferTypes()[1] != "bank" {
		t.Fatal(p.TransferTypes())
	}
	valid := TransferRequest{TransferNo: "TF0", Type: "bank", Account: "6222020000000000000", Name: "test", BankCode: "1002", Amount: 100}
	if e = p.ValidateTransfer(valid); e != nil {
		t.Fatal("pure preflight placeholder rejected", e)
	}
	for _, mutate := range []func(*TransferRequest){func(r *TransferRequest) { r.BankCode = "" }, func(r *TransferRequest) { r.BankCode = "9999" }, func(r *TransferRequest) { r.Account = "not-a-card" }, func(r *TransferRequest) { r.Name = "" }} {
		r := valid
		mutate(&r)
		if e = p.ValidateTransfer(r); e == nil {
			t.Fatal("invalid bank request accepted", r.BankCode)
		}
	}
}
