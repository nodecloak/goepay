package contract_test

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"encoding/xml"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Exercise the real XML adapter, rather than a fake TransferProvider: an
// authenticated return must reach the domain even when its fee proof is absent.
func TestWechatBankHTTPReturnWithoutFeePreservesPendingEvidence(t *testing.T) {
	s, pool, key := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"3"}' WHERE id=2; INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 2, OutTradeNo: "real-bank-http-fund", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "real-bank-http-fund"); e != nil {
		t.Fatal(e)
	}
	bankKey, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	const apiKey = "0123456789abcdef0123456789abcdef"
	const card, name = "6222020202020202020", "Synthetic Person"
	var mode atomic.Value
	mode.Store("SUCCESS")
	var writes atomic.Int32
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		params := map[string]string{}
		decoder := xml.NewDecoder(r.Body)
		for {
			token, err := decoder.Token()
			if err != nil {
				break
			}
			if start, ok := token.(xml.StartElement); ok && start.Name.Local != "xml" {
				var value string
				if err = decoder.DecodeElement(&value, &start); err != nil {
					t.Error(err)
				}
				params[start.Name.Local] = value
			}
		}
		if !adapter.VerifyWechatV2(params, apiKey, "MD5", params["sign"]) {
			t.Error("invalid request signature")
		}
		out := map[string]string{"return_code": "SUCCESS", "result_code": "SUCCESS", "mch_id": "mch", "partner_trade_no": params["partner_trade_no"], "payment_no": "real-bank-http", "amount": "100", "cmms_amt": "2"}
		if r.URL.Path == "/mmpaysptrans/pay_bank" {
			writes.Add(1)
		} else if r.URL.Path == "/mmpaysptrans/query_bank" {
			out["status"] = "SUCCESS"
			out["bank_no_md5"] = fmt.Sprintf("%X", md5.Sum([]byte(card)))
			out["true_name_md5"] = fmt.Sprintf("%X", md5.Sum([]byte(name)))
			current := mode.Load().(string)
			if current != "SUCCESS" {
				out["status"] = "BANK_FAIL"
			}
			switch current {
			case "missing":
				delete(out, "cmms_amt")
			case "invalid":
				out["cmms_amt"] = "not-money"
			case "wrong-card":
				out["bank_no_md5"] = "00000000000000000000000000000000"
			case "wrong-name":
				out["true_name_md5"] = "00000000000000000000000000000000"
			case "wrong-amount":
				out["amount"] = "101"
			case "wrong-merchant":
				out["mch_id"] = "another"
			case "wrong-order":
				out["partner_trade_no"] = "TF00000000000000000999"
			}
		} else {
			t.Error("unexpected endpoint", r.URL.Path)
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
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: provider}, AllowLive: true}
	s.MerchantChannels = map[int64][]int64{2: {7}}
	invoke := func(path string, p compat.Params) map[string]any {
		t.Helper()
		p["pid"] = "2"
		p["timestamp"] = fmt.Sprint(time.Now().Unix())
		p["sign_type"] = "RSA"
		p["sign"], _ = compat.SignRSA(p, key)
		v := url.Values{}
		for k, x := range p {
			v.Set(k, x)
		}
		_, out := call(t, s.Handler(), "POST", path, v)
		return out
	}
	first := invoke("/api/transfer/submit", compat.Params{"type": "bank", "account": card, "name": name, "bank_code": "1002", "money": "1.00", "remark": "synthetic", "out_biz_no": "real-bank-http", "channel_id": "7"})
	if first["code"] != float64(0) || first["status"] != float64(0) {
		t.Fatal(first)
	}
	s.ProviderFlow.AllowLive = false
	query := func() map[string]any {
		return invoke("/api/transfer/query", compat.Params{"out_biz_no": "real-bank-http"})
	}
	if out := query(); out["status"] != float64(1) {
		t.Fatal(out)
	}
	for _, invalid := range []string{"wrong-card", "wrong-name", "wrong-amount", "wrong-merchant", "wrong-order"} {
		mode.Store(invalid)
		out := query()
		if out["query_required"] != true || out["bank_lifecycle"] != "succeeded" {
			t.Fatal("unauthenticated identity reached return lifecycle", invalid, out)
		}
	}
	for _, fee := range []string{"missing", "invalid"} {
		mode.Store(fee)
		out := query()
		if out["status"] != float64(0) || out["bank_lifecycle"] != "reversal_pending" || out["ledger_status"] != "succeeded" {
			t.Fatal("verified return lost because fee proof absent", fee, out)
		}
		b, _ := s.Store.Balance(ctx, 2)
		if b.Available != 897 || b.Reserved != 0 {
			t.Fatal("return without fee credited funds", b)
		}
	}
	// A later stale success cannot erase the observed return.
	mode.Store("SUCCESS")
	if out := query(); out["bank_lifecycle"] != "reversal_pending" {
		t.Fatal("return evidence erased", out)
	}
	mode.Store("BANK_FAIL")
	for i := 0; i < 3; i++ {
		if out := query(); out["status"] != float64(2) || out["bank_lifecycle"] != "reversed" {
			t.Fatal(out)
		}
	}
	b, _ := s.Store.Balance(ctx, 2)
	if b.Available != 1000 || b.Reserved != 0 || writes.Load() != 1 {
		t.Fatal("return not credited exactly once", b, writes.Load())
	}
}
