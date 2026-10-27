package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/migration"
)

const combinedHTTPSnapshot204 = `{"schema_version":1,"export_id":"combined-http204","source":"epay_pro","exported_at":"2026-10-23T12:00:00+08:00","merchants":[{"id":9001,"name":"Combined HTTP204","available_money":"12.34","reserved_money":"0.00"}],"orders":[{"trade_no":"204-parent","merchant_id":9001,"out_trade_no":"http204-parent","name":"Historical combined HTTP parent","type":"wxpay","money":"5.00","status":"paid","created_at":"2026-10-20T12:00:00+08:00","paid_at":"2026-10-20T12:01:00+08:00"},{"trade_no":"204-ordinary","merchant_id":9001,"out_trade_no":"http204-ordinary","name":"Ordinary HTTP204","type":"wxpay","money":"2.00","status":"created","created_at":"2026-10-20T12:00:00+08:00"}],"refunds":[],"transfers":[],"combined_orders":[{"trade_no":"204-parent","merchant_id":9001,"source_plugin":"wxpayn","currency":"CNY","charged_money":"5.25","settle":2,"sub_orders":[{"sub_trade_no":"204-parent1","api_trade_no":"WX204-1","money":"1.75","refundmoney":null,"status":2,"settle":1},{"sub_trade_no":"204-parent2","api_trade_no":"WX204-2","money":"1.75","refundmoney":"0.25","status":2,"settle":0},{"sub_trade_no":"204-parent3","api_trade_no":"WX204-3","money":"1.75","refundmoney":"0.00","status":1,"settle":0}]}]}`

type combinedHTTPFixture204 struct {
	channel168Fixture
	ownerToken, ownerCSRF, foreignToken, foreignCSRF, delegatedToken, delegatedCSRF string
}

func combinedHTTPLocal204(t *testing.T) combinedHTTPFixture204 {
	t.Helper()
	f := combinedHTTPFixture204{channel168Fixture: channel168Local(t)}
	key := make([]byte, 32)
	if report, err := migration.Apply(t.Context(), f.p, key, []byte(combinedHTTPSnapshot204)); err != nil || !report.Applied {
		t.Fatal("real combined HTTP snapshot fixture failed", report.Issues, err)
	}
	reader, err := migration.NewCombinedOrderHistoryReader(f.p, key)
	if err != nil {
		t.Fatal(err)
	}
	f.s.CombinedOrderHistory204 = reader
	if _, err = f.p.Exec(t.Context(), `UPDATE merchants SET enabled=true WHERE id=9001;INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('combined-owner204','merchant',9001,'Combined owner204','unused-synthetic204'),('combined-foreign204','merchant',1,'Combined foreign204','unused-synthetic204')`); err != nil {
		t.Fatal(err)
	}
	f.ownerToken, f.ownerCSRF, err = f.s.Sessions.Create(identity.User{ID: 9001, Role: "merchant", Username: "combined-owner204", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	f.foreignToken, f.foreignCSRF, err = f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "combined-foreign204", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := f.p.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	f.delegatedToken, f.delegatedCSRF, _, err = f.s.Sessions.CreateDelegated(t.Context(), tx, identity.User{ID: 9001, Role: "merchant", Username: "combined-owner204", AuthVersion: 1}, identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1}, f.token)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f combinedHTTPFixture204) request(role, token, csrf, method, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/"+role+"/orders/"+path, strings.NewReader(`{}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	if token != "" {
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func combinedHTTPData204(t *testing.T, w *httptest.ResponseRecorder) migration.CombinedOrderHistory204 {
	t.Helper()
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("owner-bound historical suborders endpoint missing or cacheable: HTTP=%d body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Code int                               `json:"code"`
		Data migration.CombinedOrderHistory204 `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.Code != 0 {
		t.Fatal("invalid combined history HTTP envelope", err)
	}
	if body.Data.TradeNo != "204-parent" || !body.Data.ReadOnly || body.Data.VerifiedPaymentSource || body.Data.Source != "epay_pro" || body.Data.ExportID != "combined-http204" || body.Data.ParentSettle != 2 || body.Data.ChargedMoney != "5.25" || body.Data.ChildrenMoney != "5.25" || !body.Data.AmountsMatch || len(body.Data.Items) != 3 {
		t.Fatal("HTTP replaced original historical child assertions", body.Data)
	}
	return body.Data
}

func TestCombinedOrders204HTTPAllowsOnlyAuthorizedParentReadIncludingLegitimateDelegation(t *testing.T) {
	f := combinedHTTPLocal204(t)
	for _, caller := range []struct{ name, role, token, csrf string }{{"owner", "merchant", f.ownerToken, f.ownerCSRF}, {"administrator", "admin", f.token, f.csrf}, {"delegated own merchant", "merchant", f.delegatedToken, f.delegatedCSRF}} {
		t.Run(caller.name, func(t *testing.T) {
			w := f.request(caller.role, caller.token, caller.csrf, "GET", "204-parent/suborders")
			view := combinedHTTPData204(t, w)
			if view.Items[0].RefundMoney != nil || view.Items[1].RefundMoney == nil || *view.Items[1].RefundMoney != "0.25" || view.Items[2].RefundMoney == nil || *view.Items[2].RefundMoney != "0.00" {
				t.Fatal("HTTP flattened original NULL and zero refunds", view.Items)
			}
			for _, private := range []string{"proof_hash", "source_hash", "birth_xid", "test-secret", f.ownerToken, f.delegatedToken, f.token} {
				if strings.Contains(w.Body.String(), private) {
					t.Fatal("combined historical response exposed private authority")
				}
			}
		})
	}
	var orders, journals, effects int
	if err := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*) FROM orders),(SELECT count(*) FROM journals),(SELECT count(*) FROM jobs)+(SELECT count(*) FROM provider_attempts)+(SELECT count(*) FROM provider_operations)+(SELECT count(*) FROM refunds)`).Scan(&orders, &journals, &effects); err != nil || orders != 2 || journals != 1 || effects != 0 {
		t.Fatal("pure history read created ordinary child orders, money or execution work", orders, journals, effects, err)
	}
}

func TestCombinedOrders204HTTPForeignAndOrdinaryParentsHaveNoSuborderAuthority(t *testing.T) {
	f := combinedHTTPLocal204(t)
	for _, tc := range []struct{ name, role, token, path string }{{"foreign parent", "merchant", f.foreignToken, "204-parent/suborders"}, {"ordinary parent", "merchant", f.ownerToken, "204-ordinary/suborders"}, {"missing parent", "merchant", f.ownerToken, "204-missing/suborders"}} {
		t.Run(tc.name, func(t *testing.T) {
			w := f.request(tc.role, tc.token, "", "GET", tc.path)
			if w.Code != 404 || strings.Contains(w.Body.String(), "WX204-") {
				t.Fatal("foreign or ordinary parent exposed historical children", w.Code)
			}
		})
	}
	if w := f.request("merchant", "", "", "GET", "204-parent/suborders"); w.Code != 401 && w.Code != 403 {
		t.Fatal("anonymous child read admitted", w.Code)
	}
}

func TestCombinedOrders204HTTPDetailPublishesOnlyVerifiedRelationshipCapability(t *testing.T) {
	f := combinedHTTPLocal204(t)
	for _, caller := range []struct{ role, token, csrf string }{{"merchant", f.ownerToken, f.ownerCSRF}, {"merchant", f.delegatedToken, f.delegatedCSRF}, {"admin", f.token, f.csrf}} {
		for _, parent := range []struct {
			trade   string
			allowed bool
			combine int
		}{{"204-parent", true, 1}, {"204-ordinary", false, 0}} {
			w := f.request(caller.role, caller.token, caller.csrf, "GET", parent.trade)
			var envelope struct {
				Code int `json:"code"`
				Data struct {
					Allowed *bool `json:"can_view_suborders"`
					Order   struct {
						Trade   string `json:"trade_no"`
						Combine *int   `json:"combine"`
					} `json:"order"`
				} `json:"data"`
			}
			if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &envelope) != nil || envelope.Code != 0 || envelope.Data.Allowed == nil || *envelope.Data.Allowed != parent.allowed || envelope.Data.Order.Combine == nil || *envelope.Data.Order.Combine != parent.combine || envelope.Data.Order.Trade != parent.trade {
				t.Fatal("order detail omitted or invented the verified historical relationship capability", caller.role, parent.trade, w.Code)
			}
		}
	}
}

func TestCombinedOrders204HTTPRejectsQueryAuthorityAndAllMutatingMethods(t *testing.T) {
	f := combinedHTTPLocal204(t)
	// Confirm this is an implemented positive boundary before exercising denial.
	combinedHTTPData204(t, f.request("merchant", f.ownerToken, "", "GET", "204-parent/suborders"))
	for _, query := range []string{"?", "?merchant_id=1", "?merchant_id=9001&merchant_id=1", "?trade_no=204-ordinary", "?limit=51", "?unknown=value"} {
		w := f.request("merchant", f.ownerToken, "", "GET", "204-parent/suborders"+query)
		if w.Code != 400 || strings.Contains(w.Body.String(), "WX204-") {
			t.Fatal("suborders trusted query-provided scope", query, w.Code)
		}
	}
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE"} {
		if w := f.request("merchant", f.ownerToken, f.ownerCSRF, method, "204-parent/suborders"); w.Code != 405 {
			t.Fatal("historical suborder mutation exposed", method, w.Code)
		}
	}
	if w := f.request("merchant", f.ownerToken, f.ownerCSRF, "POST", "204-parent/suborders/refund"); w.Code != 404 {
		t.Fatal("historical refund assertion became executable", w.Code)
	}
}

func TestCombinedOrders204HTTPDamagedManifestIsDistinctFromAnOrdinaryMissingRelationship(t *testing.T) {
	f := combinedHTTPLocal204(t)
	combinedHTTPData204(t, f.request("merchant", f.ownerToken, "", "GET", "204-parent/suborders"))
	if _, err := f.p.Exec(t.Context(), `ALTER TABLE legacy_combined_export_manifests204 DISABLE TRIGGER USER;DELETE FROM legacy_combined_export_manifests204;ALTER TABLE legacy_combined_export_manifests204 ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	w := f.request("merchant", f.ownerToken, "", "GET", "204-parent/suborders")
	if w.Code != 409 || strings.Contains(w.Body.String(), "WX204-") {
		t.Fatal("damaged source became empty success, not-found, or stale child data", w.Code, w.Body.String())
	}
}
