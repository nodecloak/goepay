package contract_test

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestTransferBatchConsoleTLSPrepareExecuteUnknownQueryCancelAndOwnership(t *testing.T) {
	s, pool, merchantKey := fixture(t)
	ctx := context.Background()
	if _, e := pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true,"transfer_rate":"3"}' WHERE id=1; INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "batch-http-funds", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "batch-http-funds"); e != nil {
		t.Fatal(e)
	}
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	var writes, reads atomic.Int32
	var firstTF string
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		params := map[string]string{}
		for k, v := range r.PostForm {
			params[k] = v[0]
		}
		sig, e := adapter.SignAlipayRSA2(params, merchantKey)
		if e != nil || sig != params["sign"] || params["app_id"] != "batch-app" {
			t.Error("unsigned batch request", e)
		}
		var biz map[string]any
		json.Unmarshal([]byte(params["biz_content"]), &biz)
		tf, _ := biz["out_biz_no"].(string)
		var audits int
		switch params["method"] {
		case "alipay.fund.trans.toaccount.transfer":
			writes.Add(1)
			firstTF = tf
			if e = pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE resource='transfer-batches' AND operation='batch-claim'").Scan(&audits); e != nil || audits == 0 {
				t.Error("write before actor intent", e)
			}
			if biz["amount"] != "1.00" || biz["payee_account"] != "private-batch-recipient" {
				t.Error("mutated batch recipient/amount", biz)
			}
			w.WriteHeader(503)
			return
		case "alipay.fund.trans.common.query":
			reads.Add(1)
			if tf != firstTF {
				t.Error("query changed stable TF", tf, firstTF)
			}
		default:
			t.Error("unexpected upstream", params["method"])
			w.WriteHeader(500)
			return
		}
		node, _ := json.Marshal(map[string]any{"code": "10000", "out_biz_no": tf, "order_id": "batch-provider-id", "status": "SUCCESS", "trans_amount": "1.00"})
		digest := sha256.Sum256(node)
		signed, _ := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		json.NewEncoder(w).Encode(map[string]any{"alipay_fund_trans_common_query_response": json.RawMessage(node), "sign": base64.StdEncoding.EncodeToString(signed)})
	}))
	defer upstream.Close()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "batch-app", PrivateKey: merchantKey, PublicKey: &platform.PublicKey, HTTPClient: &http.Client{Transport: oauthContractTLSTransport{upstream, t}}})
	if e != nil {
		t.Fatal(e)
	}
	s.ProviderFlow = &payment.ProviderFlow{Store: s.Store, Providers: map[int64]adapter.Provider{7: ali}, AllowLive: true}
	s.MerchantChannels = map[int64][]int64{1: {7}}
	token, csrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1})
	if e != nil {
		t.Fatal(e)
	}
	invoke := func(method, path, body, csrfValue string) (int, map[string]any) {
		t.Helper()
		r := httptest.NewRequest(method, "/console/v1/merchant/transfer-batches"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: token})
		r.Header.Set("X-CSRF-Token", csrfValue)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		var out map[string]any
		if e := json.Unmarshal(w.Body.Bytes(), &out); e != nil {
			t.Fatal(w.Body.String())
		}
		return w.Code, out
	}
	body := `{"external_no":"http-stable-batch","items":[{"channel_id":"7","type":"alipay","account":"private-batch-recipient","name":"private-batch-name","amount":"1.00","remark":"local"},{"channel_id":"7","type":"alipay","account":"private-batch-recipient","name":"private-batch-name","amount":"1.00","remark":"local"}]}`
	if code, _ := invoke("POST", "", body, ""); code != 403 {
		t.Fatal("CSRF", code)
	}
	code, out := invoke("POST", "", body, csrf)
	if code != 200 {
		t.Fatal(code, out)
	}
	data := out["data"].(map[string]any)
	id := data["id"].(string)
	if data["cost_amount"] != "206" || writes.Load() != 0 || reads.Load() != 0 {
		t.Fatal(data, writes.Load(), reads.Load())
	}
	raw, _ := json.Marshal(data)
	if strings.Contains(string(raw), "private-batch-") {
		t.Fatal("recipient leaked", string(raw))
	}
	if code, out = invoke("GET", "?external_no=http-stable-batch", "", csrf); code != 200 || out["data"].(map[string]any)["total"] != float64(1) {
		t.Fatal(code, out)
	}
	if code, out = invoke("POST", "/"+id+"/execute", `{"limit":1}`, csrf); code != 200 || writes.Load() != 1 || out["data"].(map[string]any)["all_succeeded"] != false {
		t.Fatal(code, out, writes.Load())
	}
	s.ProviderFlow.AllowLive = false
	if code, _ = invoke("POST", "/"+id+"/execute", `{"limit":1}`, csrf); code != 503 || writes.Load() != 1 {
		t.Fatal("stop ignored", code, writes.Load())
	}
	if code, out = invoke("POST", "/"+id+"/cancel-unsent", `{}`, csrf); code != 200 || out["data"].(map[string]any)["summary"].(map[string]any)["cancelled"] != float64(1) {
		t.Fatal(code, out)
	}
	if code, out = invoke("POST", "/"+id+"/query", `{}`, csrf); code != 200 || reads.Load() != 1 || writes.Load() != 1 {
		t.Fatal(code, out, reads.Load(), writes.Load())
	}
	balance, _ := s.Store.Balance(ctx, 1)
	if balance.Available != 897 || balance.Reserved != 0 {
		t.Fatal(balance)
	}
	// The batch owner is immutable even when a caller submits a forged body.
	if code, _ = invoke("POST", "/"+id+"/execute", `{"limit":1,"merchant_id":"2"}`, csrf); code != 400 {
		t.Fatal("mutable action owner", code)
	}
	token, csrf, e = s.Sessions.Create(identity.User{Username: "other", Role: "merchant", ID: 2})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('other','merchant',2,'Other','unused')`); e != nil {
		t.Fatal(e)
	}
	if code, _ = invoke("GET", "/"+id, "", csrf); code != 404 {
		t.Fatal("foreign detail", code)
	}
	if code, _ = invoke("POST", "/"+id+"/query", `{}`, csrf); code != 404 || reads.Load() != 1 {
		t.Fatal("foreign query", code)
	}
	_ = fmt.Sprint(id)
}
