package payment

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"encoding/xml"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWechatBankProtocolToPostgreSQLLateReturn(t *testing.T) {
	store, flow, _ := transferFixture(t)
	ctx := context.Background()
	bankKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	apiKey := "0123456789abcdef0123456789abcdef"
	card, name := "6222020000000000000", "Synthetic Cardholder"
	state := "SUCCESS"
	submits, queries := 0, 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decoder := xml.NewDecoder(r.Body)
		params := map[string]string{}
		for {
			token, e := decoder.Token()
			if e != nil {
				break
			}
			if start, ok := token.(xml.StartElement); ok && start.Name.Local != "xml" {
				var value string
				if e = decoder.DecodeElement(&value, &start); e != nil {
					t.Error(e)
				}
				params[start.Name.Local] = value
			}
		}
		if !adapter.VerifyWechatV2(params, apiKey, "MD5", params["sign"]) {
			t.Error("bank request signature invalid")
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "mch_id": "mch", "partner_trade_no": params["partner_trade_no"], "payment_no": "synthetic-bank-flow", "amount": "100", "cmms_amt": "2"}
		switch r.URL.Path {
		case "/mmpaysptrans/pay_bank":
			submits++
			if len(params["partner_trade_no"]) < 8 || params["enc_bank_no"] == "" || params["enc_true_name"] == "" {
				t.Error("bank namespace/encryption missing")
			}
		case "/mmpaysptrans/query_bank":
			queries++
			out["status"] = state
			out["bank_no_md5"] = fmt.Sprintf("%X", md5.Sum([]byte(card)))
			out["true_name_md5"] = fmt.Sprintf("%X", md5.Sum([]byte(name)))
			out["pay_succ_time"] = "2026-10-23 12:00:00"
		default:
			t.Error(r.URL.Path)
		}
		out["sign"], _ = adapter.SignWechatV2(out, apiKey, "MD5")
		var body strings.Builder
		body.WriteString("<xml>")
		for k, v := range out {
			body.WriteString("<" + k + ">")
			xml.EscapeText(&body, []byte(v))
			body.WriteString("</" + k + ">")
		}
		body.WriteString("</xml>")
		w.Write([]byte(body.String()))
	}))
	defer upstream.Close()
	provider, e := adapter.NewWechatV2(adapter.WechatV2Config{AppID: "app", MerchantID: "mch", APIKey: apiKey, BankPublicKey: &bankKey.PublicKey, BaseURL: upstream.URL, HTTPClient: upstream.Client(), RefundClient: upstream.Client()})
	if e != nil {
		t.Fatal(e)
	}
	flow.Providers[7] = provider
	transfer, e := flow.Transfer(ctx, 501, 7, "bank-real-adapter", adapter.TransferRequest{Type: "bank", Account: card, Name: name, BankCode: "1002", Amount: 100, Remark: "synthetic"})
	if e != nil || transfer.Status == "succeeded" {
		t.Fatal("bank accepted state treated as paid", transfer, e)
	}
	b, _ := store.Balance(ctx, 501)
	if b.Available != 890 || b.Reserved != 110 {
		t.Fatal(b)
	}
	flow.AllowLive = false
	result, e := flow.QueryTransfer(ctx, 501, transfer.TransferNo)
	if e != nil || result.Status != "succeeded" {
		t.Fatal(result, e)
	}
	b, _ = store.Balance(ctx, 501)
	if b.Available != 890 || b.Reserved != 0 {
		t.Fatal(b)
	}
	state = "BANK_FAIL"
	for i := 0; i < 3; i++ {
		result, e = flow.QueryTransfer(ctx, 501, transfer.TransferNo)
		if e != nil || result.Status != "reversed" {
			t.Fatal(result, e)
		}
	}
	b, _ = store.Balance(ctx, 501)
	if b.Available != 1000 || b.Reserved != 0 || submits != 1 || queries != 2 {
		t.Fatal("verified return not exactly once", b, submits, queries)
	}
}
