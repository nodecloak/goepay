package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
)

func TestMerchantScan192OwnershipCSRFDelegateAndCodeCannotBeReplaced(t *testing.T) {
	setup, f := merchantScan192Ready(t)
	const key = "b290539a-178a-4bae-9d75-c78c16d57563"
	body := merchantScan192Prepare(t, f, key)
	if e := f.s.Store.CreateMerchant(t.Context(), payment.Merchant{ID: 2, Name: "other synthetic merchant", APIKey: "other-local-synthetic"}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash)VALUES('scan192-other','merchant',2,'Other synthetic','unused-hash')`); e != nil {
		t.Fatal(e)
	}
	other := f
	var e error
	other.merchantToken, other.merchantCSRF, e = f.s.Sessions.Create(identity.User{ID: 2, Role: "merchant", Username: "scan192-other", AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ method, path, body string }{{"GET", "/intents/" + key, ""}, {"POST", "/intents/" + key + "/apply", `{"auth_code":"134567890123456789"}`}, {"POST", "/intents/" + key + "/query", "{}"}} {
		if w := merchantScan192Request(other, tc.method, tc.path, tc.body); w.Code != 404 {
			t.Fatal("cross-merchant original request visible", tc.method, w.Code)
		}
	}
	// The same UUID belongs to a separate namespace for the other merchant.
	channel168Data(t, merchantScan192Request(other, "POST", "/intents/"+key+"/close", "{}"))
	if state := channel168Data(t, merchantScan192Request(f, "GET", "/intents/"+key, "")); state["state"] != "prepared" {
		t.Fatal("other merchant closed owner's request", state)
	}
	wrong := f
	wrong.merchantCSRF = "not-the-original-csrf"
	if w := merchantScan192Request(wrong, "POST", "/intents/"+key+"/apply", `{"auth_code":"134567890123456789"}`); w.Code != 403 {
		t.Fatal("wrong CSRF accepted", w.Code)
	}
	for _, tc := range []struct{ path, body string }{{"/prepare", strings.Replace(body, "134567890123456789", "135567890123456789", 1)}, {"/intents/" + key + "/apply", `{"auth_code":"135567890123456789"}`}} {
		if w := merchantScan192Request(f, "POST", tc.path, tc.body); w.Code != 409 {
			t.Fatal("original bearer identity replaced", w.Code)
		}
	}
	tx, e := f.p.Begin(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(t.Context())
	delegated := f
	delegated.merchantToken, delegated.merchantCSRF, _, e = f.s.Sessions.CreateDelegated(t.Context(), tx, identity.User{ID: 1, Role: "merchant", Username: "buyer104-merchant", AuthVersion: 1}, identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1}, f.token)
	if e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(t.Context()); e != nil {
		t.Fatal(e)
	}
	if w := merchantScan192Request(delegated, "GET", "/capabilities", ""); w.Code != 403 {
		t.Fatal("delegated cashier capability admitted", w.Code)
	}
	if w := merchantScan192Request(delegated, "POST", "/prepare", body); w.Code != 403 {
		t.Fatal("delegated cashier send admitted", w.Code)
	}
	var attempts int
	if e = f.p.QueryRow(t.Context(), `SELECT count(*)FROM provider_attempts`).Scan(&attempts); e != nil || attempts != 0 || setup.creates.Load() != 0 {
		t.Fatal("rejected authority produced charge", attempts, setup.creates.Load(), e)
	}
}

func TestMerchantScan192UnkeyedLookalikeParamRemainsOrdinaryWireCompatible(t *testing.T) {
	setup, f := merchantScan192Ready(t)
	p := compat.Params{"pid": "1", "out_trade_no": "ordinary-scan192-lookalike", "type": "wxpay", "name": "ordinary synthetic payment", "money": "100.00", "channel_id": setup.id, "method": "scan", "auth_code": "134567890123456789", "param": "merchant-scan:de1c4ef4-cd8b-4219-a1e2-bd542c9a8865:" + strings.Repeat("0", 64), "sign_type": "MD5"}
	p["sign"] = compat.SignMD5(p, "test-secret")
	form := url.Values{}
	for k, v := range p {
		form.Set(k, v)
	}
	r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var reply map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &reply); e != nil || reply["code"] != float64(1) {
		t.Fatal("ordinary caller prefix reserved incorrectly", w.Code, reply, e)
	}
	if setup.creates.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("ordinary original scan did not execute exactly once", setup.creates.Load(), setup.bad.Load())
	}
	var cashierIntents int
	if e := f.p.QueryRow(t.Context(), `SELECT count(*)FROM merchant_scan_intents106`).Scan(&cashierIntents); e != nil || cashierIntents != 0 {
		t.Fatal("ordinary API allocated console intent", cashierIntents, e)
	}
}

func TestMerchantScan192ConcurrentCloseAndApplyCannotBothWin(t *testing.T) {
	setup, f := merchantScan192Ready(t)
	const key = "250cb5ad-e12e-41c8-a91c-dce122febf2f"
	merchantScan192Prepare(t, f, key)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var closeStatus, applyStatus int
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		closeStatus = merchantScan192Request(f, "POST", "/intents/"+key+"/close", "{}").Code
	}()
	go func() {
		defer wg.Done()
		<-start
		applyStatus = merchantScan192Request(f, "POST", "/intents/"+key+"/apply", `{"auth_code":"134567890123456789"}`).Code
	}()
	close(start)
	wg.Wait()
	state := channel168Data(t, merchantScan192Request(f, "GET", "/intents/"+key, ""))
	sends := setup.creates.Load()
	if applyStatus != 200 || setup.bad.Load() != 0 {
		t.Fatal("racing original outcome unreadable", applyStatus, setup.bad.Load())
	}
	switch state["state"] {
	case "paid":
		if sends != 1 || closeStatus != 409 {
			t.Fatal("both close and charge won", sends, closeStatus)
		}
	case "closed":
		if sends != 0 || closeStatus != 200 {
			t.Fatal("closed intent charged", sends, closeStatus)
		}
	default:
		t.Fatal("unexpected local signed outcome", state)
	}
}
