package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"goepay/internal/identity"
)

type buyer104MerchantFixture struct {
	channel168Fixture
	merchantToken, merchantCSRF string
}

func buyer104MerchantLocal(t *testing.T) buyer104MerchantFixture {
	t.Helper()
	f := channel168Local(t)
	return buyer190MerchantOn(t, f)
}

func buyer190MerchantOn(t *testing.T, f channel168Fixture) buyer104MerchantFixture {
	t.Helper()
	if _, e := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('buyer104-merchant','merchant',1,'Synthetic buyer policy merchant','unused-local-hash','operator')`); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "buyer104-merchant", AdminClass: "operator", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	return buyer104MerchantFixture{f, token, csrf}
}
func (f buyer104MerchantFixture) buyer(method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/merchant/account/buyer-fees"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.merchantCSRF)
	r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: f.merchantToken})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}
func TestBuyerCharge104OriginalPersonalFieldsAndDurableCAS(t *testing.T) {
	f := buyer104MerchantLocal(t)
	w := f.buyer("GET", "", "")
	if w.Code != 200 {
		t.Fatal("original buyer charge settings unavailable", w.Code, w.Body.String())
	}
	var v struct {
		Data struct {
			MerchantID  string `json:"merchant_id"`
			Mode        int    `json:"mode"`
			Rate        string `json:"mode_feerate"`
			Configured  bool   `json:"configured"`
			Revision    string `json:"revision"`
			Fingerprint string `json:"fingerprint"`
			CanUpdate   bool   `json:"can_update"`
		}
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil || v.Data.MerchantID != "1" || v.Data.Mode != 0 || v.Data.Rate != "" || v.Data.Configured || v.Data.Revision != "0" || len(v.Data.Fingerprint) != 64 || !v.Data.CanUpdate {
		t.Fatal("original no-policy default not truthful", w.Body.String(), e)
	}
	body, _ := json.Marshal(map[string]any{"mode": 1, "mode_feerate": "", "expected_fingerprint": v.Data.Fingerprint})
	w = f.buyer("PATCH", "", string(body))
	if w.Code != 200 {
		t.Fatal("actual full buyer mode save", w.Code, w.Body.String())
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil || v.Data.Mode != 1 || !v.Data.Configured || v.Data.Revision != "1" {
		t.Fatal(w.Body.String(), e)
	}
	for _, needle := range []string{"proof_hash", f.merchantToken, f.merchantCSRF} {
		if strings.Contains(w.Body.String(), needle) {
			t.Fatal("successful policy response leaked private material")
		}
	}
	if w = f.buyer("PATCH", "", string(body)); w.Code != 409 {
		t.Fatal("stale original pricing CAS accepted", w.Code, w.Body.String())
	}
	if w = f.buyer("GET", "?", ""); w.Code != 400 {
		t.Fatal("query context accepted", w.Code, w.Body.String())
	}
	for _, needle := range []string{"proof_hash", f.merchantToken, f.merchantCSRF} {
		if strings.Contains(w.Body.String(), needle) {
			t.Fatal("private price proof or credential exposed")
		}
	}
	var orders, attempts int
	if e := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts)`).Scan(&orders, &attempts); e != nil || orders != 0 || attempts != 0 {
		t.Fatal("setting changed payment state", orders, attempts, e)
	}
}

func TestBuyerCharge104PersonalHTTPStrictInputsAndNoSideEffects(t *testing.T) {
	f := buyer104MerchantLocal(t)
	var got struct {
		Data struct {
			Fingerprint string `json:"fingerprint"`
		}
	}
	w := f.buyer("GET", "", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	fp := got.Data.Fingerprint
	for _, body := range []string{
		`{}`, `{"mode":1,"mode_feerate":null,"expected_fingerprint":"` + fp + `"}`,
		`{"mode":1,"mode_feerate":"","expected_fingerprint":"` + fp + `","merchant_id":"2"}`,
		`{"mode":1,"Mode":0,"mode_feerate":"","expected_fingerprint":"` + fp + `"}`,
		`{"mode":1,"mode":0,"mode_feerate":"","expected_fingerprint":"` + fp + `"}`,
		`{"mode":"1","mode_feerate":"","expected_fingerprint":"` + fp + `"}`,
		`{"mode":1,"mode_feerate":"","expected_fingerprint":"` + fp + `"} {}`,
	} {
		if w = f.buyer("PATCH", "", body); w.Code != 400 {
			t.Fatal("ambiguous policy accepted", body, w.Code, w.Body.String())
		}
	}
	for _, rate := range []string{"101", "-1", " 0", "1e2", "0.0000000000001", "00.1", "100.000000000001"} {
		body, _ := json.Marshal(map[string]any{"mode": 1, "mode_feerate": rate, "expected_fingerprint": fp})
		if w = f.buyer("PATCH", "", string(body)); w.Code != 422 {
			t.Fatal("invalid decimal buyer rate entered pricing", rate, w.Code, w.Body.String())
		}
	}
	var n int
	if e := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM customer_charge_policies104)+(SELECT count(*) FROM orders)+(SELECT count(*) FROM provider_attempts)+(SELECT count(*) FROM backoffice_audit WHERE resource='customer-charge-policies104')`).Scan(&n); e != nil || n != 0 {
		t.Fatal("rejected settings side effects", n, e)
	}
}

func TestBuyerCharge104PersonalConcurrentCASAndLostHead(t *testing.T) {
	f := buyer104MerchantLocal(t)
	var got struct {
		Data struct {
			Fingerprint string `json:"fingerprint"`
		}
	}
	w := f.buyer("GET", "", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &got) != nil {
		t.Fatal(w.Code, w.Body.String())
	}
	body := fmt.Sprintf(`{"mode":1,"mode_feerate":"","expected_fingerprint":%q}`, got.Data.Fingerprint)
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- f.buyer("PATCH", "", body).Code }()
	}
	wg.Wait()
	close(results)
	success, conflict := 0, 0
	for code := range results {
		switch code {
		case 200:
			success++
		case 409:
			conflict++
		default:
			t.Fatal("concurrent pricing CAS", code)
		}
	}
	if success != 1 || conflict != 1 {
		t.Fatal(success, conflict)
	}
	if _, e := f.p.Exec(t.Context(), `ALTER TABLE customer_charge_policies104 DISABLE TRIGGER USER; DELETE FROM customer_charge_policies104`); e != nil {
		t.Fatal(e)
	}
	if w = f.buyer("GET", "", ""); w.Code != 409 {
		t.Fatal("lost original policy silently defaulted", w.Code, w.Body.String())
	}
	if w = f.buyer("PATCH", "", body); w.Code != 409 {
		t.Fatal("lost policy repaired from request", w.Code, w.Body.String())
	}
}
