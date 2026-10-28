package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettlementBatches84HTTPOriginalFieldsExportAndAuthority(t *testing.T) {
	s, pool, _ := fixture(t)
	// The original 205 merchant login writes its session row in the same
	// transaction as the captcha grant, so the HTTP flow needs the durable
	// persistent session repository rather than the fixture's in-memory default.
	s.Sessions = identity.NewPersistentSessions(pool, false)
	ctx := context.Background()
	h := s.Handler()
	o, err := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "seed-batches84-http", Amount: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Store.MarkPaid(ctx, o.TradeNo, "synthetic84"); err != nil {
		t.Fatal(err)
	}
	cfg, err := s.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "alipay", Account: "original84@example.test", Name: "测试收款"}, nil, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "merchant"})
	if err != nil {
		t.Fatal(err)
	}
	request, err := s.Store.RequestFundsWithSettlementRecipient(ctx, "settlements", 1, 0, "original-request84-http", 500, "merchant:1", nil, &cfg.ConfigID)
	if err != nil {
		t.Fatal(err)
	}
	login := func(role string) (*http.Cookie, string) {
		t.Helper()
		r := httptest.NewRequest("POST", "/console/v1/"+role+"/login", bytes.NewBufferString(`{"username":"`+role+`","password":"contract-password-123"}`))
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var out struct {
			Data struct {
				CSRF string `json:"csrf_token"`
			} `json:"data"`
		}
		if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || len(w.Result().Cookies()) != 1 || out.Data.CSRF == "" {
			t.Fatal("login", w.Code, err)
		}
		return w.Result().Cookies()[0], out.Data.CSRF
	}
	admin, csrf := login("admin")
	merchant, _ := login("merchant")
	invoke := func(method, path, body string, cookie *http.Cookie, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.AddCookie(cookie)
		if token != "" {
			r.Header.Set("X-CSRF-Token", token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	body := fmt.Sprintf(`{"external_no":"http84","request_ids":["%d"]}`, request.ID)
	if w := invoke("POST", "/console/v1/admin/settlement-batches", body, admin, ""); w.Code != 403 {
		t.Fatal("missing CSRF", w.Code)
	}
	if w := invoke("POST", "/console/v1/admin/settlement-batches", strings.TrimSuffix(body, "}")+`,"merchant_id":"2"}`, admin, csrf); w.Code != 400 {
		t.Fatal("caller owner override", w.Code)
	}
	w := invoke("POST", "/console/v1/admin/settlement-batches", body, admin, csrf)
	var created struct {
		Data payment.SettlementBatch `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &created); err != nil || w.Code != 200 || created.Data.AllMoney != "5.00" || created.Data.Count != 1 {
		t.Fatal("original batch fields", w.Code, w.Body.String(), err)
	}
	path := fmt.Sprintf("/console/v1/admin/settlement-batches/%d", created.Data.ID)
	if w = invoke("GET", strings.Replace(path, "/admin/", "/merchant/", 1), "", merchant, ""); w.Code != 403 {
		t.Fatal("merchant read administrator group", w.Code)
	}
	if w = invoke("GET", path+"/export?format=alipay", "", admin, ""); w.Code != 200 || w.Header().Get("Content-Type") != "text/csv; charset=gbk" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment;") || bytes.HasPrefix(w.Body.Bytes(), []byte{239, 187, 191}) || !bytes.Contains(w.Body.Bytes(), []byte("original84@example.test")) {
		t.Fatal("original GBK export", w.Code, w.Header())
	}
	if w = invoke("POST", path+"/complete", "{}", admin, csrf); w.Code != 409 {
		t.Fatal("unpaid batch completed", w.Code, w.Body.String())
	}
	if w = invoke("POST", path+"/archive", "{}", admin, csrf); w.Code != 200 {
		t.Fatal("archive", w.Code, w.Body.String())
	}
	var originalStatus string
	if err = pool.QueryRow(ctx, "SELECT status FROM ledger_requests WHERE id=$1", request.ID).Scan(&originalStatus); err != nil || originalStatus != "pending" {
		t.Fatal("classification changed original approval", originalStatus, err)
	}
}
