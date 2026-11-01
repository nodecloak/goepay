package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"goepay/internal/identity"
)

func recipientMenuOperator(t *testing.T, f channel168Fixture, name string, id int64, grants []string) (identity.User, string, string) {
	t.Helper()
	raw, err := json.Marshal(grants)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class,permissions) VALUES($1,'admin',$2,$1,'unused-local-operator-password','operator',$3::jsonb)`, name, id, raw); err != nil {
		t.Fatal(err)
	}
	user := identity.User{ID: id, Username: name, Role: "admin", AdminClass: "operator", AuthVersion: 1, Permissions: raw}
	token, csrf, err := f.s.Sessions.Create(user)
	if err != nil {
		t.Fatal(err)
	}
	return user, token, csrf
}

func recipientMenuRequest(f channel168Fixture, token, csrf, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin"+path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Origin", "https://pay.example.invalid")
	r.Header.Set("X-CSRF-Token", csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func recipientMenuData(t *testing.T, w *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	if w.Code != status {
		t.Fatalf("recipient HTTP=%d want%d: %s", w.Code, status, w.Body.String())
	}
	var out struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Code != 0 || out.Data == nil {
		t.Fatal("invalid recipient response", err, w.Body.String())
	}
	return out.Data
}

func TestSettlementRecipientMenuMappingIsExactAndWriteScoped(t *testing.T) {
	for _, p := range [][]string{{"admin", "workflows", "settlement-recipient"}, {"admin", "workflows", "settlement-recipient", "allowed-types"}} {
		if routeMenuKey(p) != "MerchantList" || !adminRequestAllowed(adminUser("operator", []string{"MerchantList"}), p) {
			t.Error("merchant menu cannot reach its recipient dependency", p)
		}
		for _, grants := range [][]string{{"SettleList"}, {"GroupList"}, {"OrderList"}, {}} {
			if adminRequestAllowed(adminUser("operator", grants), p) {
				t.Error("conservative write check widened beyond MerchantList", grants, p)
			}
		}
	}
	for _, p := range [][]string{{"admin", "workflows", "settlement-recipient", "unknown"}, {"admin", "workflows", "settlement-recipient", "allowed-types", "extra"}} {
		if routeMenuKey(p) != "" || adminRequestAllowed(adminUser("operator", []string{"MerchantList", "SettleList"}), p) {
			t.Error("unknown recipient path received a page grant", p)
		}
	}
}

func TestSettlementRecipientExplicitMerchantGrantCreatesInitialRecipientHTTP(t *testing.T) {
	f := channel168Local(t)
	_, token, csrf := recipientMenuOperator(t, f, "recipient-menu-creator", 186831, []string{"MerchantList"})
	recipientMenuData(t, recipientMenuRequest(f, token, csrf, "GET", "/resources/merchants/schema", ""), 200)
	preview := recipientMenuData(t, recipientMenuRequest(f, token, csrf, "GET", "/workflows/settlement-recipient/allowed-types?group_id=0", ""), 200)
	if preview["can_write"] != true {
		t.Fatal("persistent merchant-list grant did not advertise its own write permission", preview)
	}
	body := `{"creation_mode":"account","email":"menu-owner@example.test","login_password":"synthetic-menu-owner-password","group_id":"0","settlement_type":"alipay","settlement_account":"private-menu-payee@example.test","settlement_name":"Synthetic Private Payee"}`
	created := recipientMenuRequest(f, token, csrf, "POST", "/resources/merchants", body)
	data := recipientMenuData(t, created, 201)
	id, err := strconv.ParseInt(fmt.Sprint(data["id"]), 10, 64)
	if err != nil || id <= 1 {
		t.Fatal("contact merchant was not actually allocated", data, err)
	}
	for _, private := range []string{"private-menu-payee@example.test", "Synthetic Private Payee", "synthetic-menu-owner-password"} {
		if strings.Contains(created.Body.String(), private) {
			t.Fatal("creation response exposed private setup input")
		}
	}
	view := recipientMenuData(t, recipientMenuRequest(f, token, csrf, "GET", fmt.Sprintf("/workflows/settlement-recipient?merchant_id=%d", id), ""), 200)
	if view["state"] != "configured" || view["can_write"] != true || !strings.HasPrefix(fmt.Sprint(view["account_masked"]), "***") {
		t.Fatal("initial recipient was not actually recorded and masked", view)
	}
	var configs, logins, orders, attempts int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM settlement_recipient_configs WHERE merchant_id=$1),(SELECT count(*) FROM console_users WHERE role='merchant' AND subject_id=$1),(SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts)`, id).Scan(&configs, &logins, &orders, &attempts); err != nil || configs != 1 || logins != 1 || orders != 0 || attempts != 0 {
		t.Fatal("merchant/recipient creation created the wrong durable state", configs, logins, orders, attempts, err)
	}
	balance, err := f.s.Store.Balance(t.Context(), id)
	if err != nil || balance.Available != 0 || balance.Reserved != 0 {
		t.Fatal("initial recipient creation moved funds", balance, err)
	}
	if w := recipientMenuRequest(f, token, csrf, "GET", "/resources/groups", ""); w.Code != 403 {
		t.Fatal("MerchantList acquired GroupList", w.Code)
	}
}

func TestSettlementRecipientPersistentGrantMatrixAndRequestIsolationHTTP(t *testing.T) {
	f := channel168Local(t)
	_, mt, mc := recipientMenuOperator(t, f, "recipient-menu-manager", 186832, []string{"MerchantList"})
	_, st, sc := recipientMenuOperator(t, f, "recipient-menu-reviewer", 186833, []string{"SettleList"})
	base := "/workflows/settlement-recipient"
	for _, session := range []struct {
		token, csrf string
		write       bool
	}{{mt, mc, true}, {st, sc, false}} {
		for _, path := range []string{base + "?merchant_id=1", base + "/allowed-types?merchant_id=1"} {
			if got := recipientMenuData(t, recipientMenuRequest(f, session.token, session.csrf, "GET", path, ""), 200); got["can_write"] != session.write {
				t.Fatal("read-only reviewer and destination editor permissions were conflated", got)
			}
		}
	}
	// Independent persistent cookies interleave their request-owned Service
	// injections. No mutable Server field may carry a previous caller's grant.
	errors := make(chan string, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			token, csrf, expected := mt, mc, true
			if i%2 != 0 {
				token, csrf, expected = st, sc, false
			}
			path := base + "?merchant_id=1"
			if i%4 >= 2 {
				path = base + "/allowed-types?merchant_id=1"
			}
			w := recipientMenuRequest(f, token, csrf, "GET", path, "")
			var out struct {
				Data map[string]any `json:"data"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data["can_write"] != expected {
				errors <- fmt.Sprintf("cookie=%d HTTP=%d capability=%v", i, w.Code, out.Data["can_write"])
			}
		}(i)
	}
	wg.Wait()
	close(errors)
	for problem := range errors {
		t.Error(problem)
	}
	valid := `{"merchant_id":"1","type":"alipay","account":"local-recipient-menu@example.test","name":"Synthetic Recipient","expected_config_id":"0"}`
	for _, denied := range []struct{ token, csrf, body string }{{st, sc, valid}, {st, sc, strings.TrimSuffix(valid, "}") + `,"can_write":true}`}, {mt, "wrong-csrf", valid}} {
		if w := recipientMenuRequest(f, denied.token, denied.csrf, "POST", base, denied.body); w.Code != 403 {
			t.Fatal("read-only or forged capability bypassed the destination gate", w.Code, w.Body.String())
		}
	}
	configured := recipientMenuData(t, recipientMenuRequest(f, mt, mc, "POST", base, valid), 200)
	if _, exists := configured["can_write"]; exists {
		t.Fatal("POST changed the shared recipient-view contract")
	}
	if w := recipientMenuRequest(f, mt, mc, "POST", base, valid); w.Code != 409 {
		t.Fatal("stale recipient CAS accepted", w.Code)
	}
	for _, path := range []string{base, base + "?merchant_id=0", base + "?merchant_id=1&merchant_id=2", base + "/allowed-types?group_id=00", base + "/allowed-types?group_id=0&merchant_id=1", base + "/allowed-types?extra=1"} {
		if w := recipientMenuRequest(f, st, sc, "GET", path, ""); w.Code != 422 {
			t.Error("read bridge bypassed owner/query validation", path, w.Code)
		}
	}
	for _, path := range []string{base + "/unknown", base + "/allowed-types/extra"} {
		if w := recipientMenuRequest(f, st, sc, "GET", path, ""); w.Code != 403 {
			t.Error("read bridge admitted unknown descendant", path, w.Code)
		}
	}
	if w := recipientMenuRequest(f, st, sc, "POST", base+"/allowed-types", `{}`); w.Code != 403 {
		t.Fatal("GET bridge admitted a mutation method", w.Code)
	}
	for _, grants := range []string{`["GroupList"]`, `["OrderList"]`, `[]`} {
		if _, err := f.p.Exec(t.Context(), `UPDATE console_users SET permissions=$1::jsonb WHERE username='recipient-menu-reviewer'`, grants); err != nil {
			t.Fatal(err)
		}
		if w := recipientMenuRequest(f, st, sc, "GET", base+"/allowed-types?group_id=0", ""); w.Code != 403 {
			t.Error("persistent grant revocation did not affect existing cookie", grants, w.Code)
		}
	}
	var groupID int64
	if err := f.p.QueryRow(t.Context(), `INSERT INTO backoffice_records(resource,body)VALUES('groups','{"name":"recipient-menu-restricted-rails","enabled":true,"settle_alipay":"0"}')RETURNING id`).Scan(&groupID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(t.Context(), `UPDATE console_users SET permissions='["MerchantList","GroupList"]'::jsonb WHERE username='recipient-menu-manager'`); err != nil {
		t.Fatal(err)
	}
	recipientMenuData(t, recipientMenuRequest(f, mt, mc, "GET", "/resources/groups", ""), 200)
	preview := recipientMenuData(t, recipientMenuRequest(f, mt, mc, "GET", fmt.Sprintf("%s/allowed-types?group_id=%d", base, groupID), ""), 200)
	for _, typ := range preview["settlement"].([]any) {
		if typ == "alipay" {
			t.Error("custom group preview ignored its actual rail restriction")
		}
	}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash)VALUES('recipient-menu-owner','merchant',1,'Owner','unused-local-owner-password')`); err != nil {
		t.Fatal(err)
	}
	ownerToken, _, err := f.s.Sessions.Create(identity.User{ID: 1, Username: "recipient-menu-owner", Role: "merchant", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"", 200}, {"?merchant_id=2", 403}, {"/allowed-types", 200}, {"/allowed-types?group_id=0", 422}} {
		r := httptest.NewRequest("GET", "/console/v1/merchant"+base+tc.path, nil)
		r.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: ownerToken})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Error("merchant recipient owner/query boundary changed", tc.path, w.Code, w.Body.String())
		}
	}
	var configs, journals int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM settlement_recipient_configs),(SELECT count(*) FROM journals)`).Scan(&configs, &journals); err != nil || configs != 1 || journals != 0 {
		t.Fatal("read-only/malformed requests created financial effects", configs, journals, err)
	}
}

func TestSettlementRecipientHostPredicateDeniesTrustedReadOnlyDispatch(t *testing.T) {
	f := channel168Local(t)
	u, _, csrf := recipientMenuOperator(t, f, "recipient-menu-dispatch", 186834, []string{"SettleList"})
	r := httptest.NewRequest("POST", "/console/v1/admin/workflows/settlement-recipient", strings.NewReader(`{"merchant_id":"1","type":"alipay","account":"local-denied@example.test","name":"Synthetic","expected_config_id":"0"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	w := httptest.NewRecorder()
	if !f.s.backoffice(w, r, "admin", identity.Session{User: u, CSRF: csrf}, []string{"admin", "workflows", "settlement-recipient"}) || w.Code != 403 {
		t.Error("request-owned write predicate was not enforced", w.Code, w.Body.String())
	}
	var configs int
	if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM settlement_recipient_configs`).Scan(&configs); err != nil || configs != 0 {
		t.Error("trusted read-only dispatch wrote a recipient", configs, err)
	}
}
