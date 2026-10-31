package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

// The capability must follow the same persisted WeChat package used by the
// real confirmation link, never approval alone. No provider network is used.
func TestSettlementReceiptCapability212(t *testing.T) {
	f, provider, _, _ := wxtransFixture(t)
	ctx := t.Context()
	if err := f.s.Store.CreateMerchant(ctx, payment.Merchant{ID: 2, Name: "Other synthetic merchant", APIKey: "synthetic-other212"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('receiptmerchant212','merchant',1,'Synthetic Merchant','unused-synthetic-only','operator'),('receiptother212','merchant',2,'Other Merchant','unused-synthetic-only','operator')`); err != nil {
		t.Fatal(err)
	}
	own, ownCSRF, err := f.s.Sessions.Create(identity.User{ID: 1, Role: "merchant", Username: "receiptmerchant212", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := f.s.Sessions.Create(identity.User{ID: 2, Role: "merchant", Username: "receiptother212", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	config, err := f.s.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "wxpay", AppID: "app168-seven", Account: "recipient212", Name: "Synthetic Recipient"}, nil, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "merchant212"})
	if err != nil {
		t.Fatal(err)
	}
	req, err := f.s.Store.RequestFundsWithSettlementRecipient(ctx, "settlements", 1, 0, "receipt212", 100, "merchant:1", nil, &config.ConfigID)
	if err != nil {
		t.Fatal(err)
	}
	if req, err = f.s.Store.ReviewFundsRequest(ctx, req.ID, true, "admin:168"); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatInt(req.ID, 10)
	check := func(want bool) {
		t.Helper()
		for _, who := range [][3]string{{"admin", f.token, f.csrf}, {"merchant", own, ownCSRF}} {
			list := wxtransConsole(f, who[0], who[1], who[2], "/console/v1/"+who[0]+"/workflows/settlements")
			items, _ := list["items"].([]any)
			if len(items) != 1 {
				t.Fatalf("expected own settlement, got %d", len(items))
			}
			row := items[0].(map[string]any)
			if got, ok := row["can_confirm_receipt"].(bool); !ok || got != want {
				t.Fatalf("list capability=%v present=%v want=%v", got, ok, want)
			}
			if _, leak := row["confirm_url"]; leak {
				t.Fatal("list exposed confirmation URL")
			}
			detail := wxtransConsole(f, who[0], who[1], who[2], "/console/v1/"+who[0]+"/settlement-payouts/"+id)
			if got := detail["confirm_url"] != ""; got != want {
				t.Fatalf("link availability=%v capability=%v", got, want)
			}
		}
	}
	check(false) // approval is not an actual sent transfer
	actor := payment.TransferBatchActor{Role: "admin", Class: "super", ID: 168, Name: "admin168"}
	if _, err = f.s.ProviderFlow.PrepareSettlementPayout(ctx, req.ID, 7, actor, nil); err != nil {
		t.Fatal(err)
	}
	check(false) // prepared but unsent has no provider confirmation package
	if _, err = f.s.ProviderFlow.ExecuteSettlementPayout(ctx, req.ID, actor, nil); err != nil {
		t.Fatal(err)
	}
	check(true)
	key := f.s.CheckoutKey
	f.s.CheckoutKey = nil
	check(false)
	f.s.CheckoutKey = key
	// A different merchant cannot discover the request or its capability.
	r := httptest.NewRequest(http.MethodGet, "/console/v1/admin/workflows/settlements?merchant_id=2", nil)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal("scoped list", w.Code)
	}
	var scoped struct {
		Data struct {
			Items []any `json:"items"`
		} `json:"data"`
	}
	if err = json.Unmarshal(w.Body.Bytes(), &scoped); err != nil || len(scoped.Data.Items) != 0 {
		t.Fatal("scoped list leaked another merchant settlement", err)
	}
	foreign := wxtransConsole(f, "merchant", other, "", "/console/v1/merchant/workflows/settlements")
	if items, _ := foreign["items"].([]any); len(items) != 0 {
		t.Fatal("another merchant can see settlement capability")
	}
	denied := httptest.NewRequest(http.MethodGet, "/console/v1/merchant/settlement-payouts/"+id, nil)
	denied.AddCookie(&http.Cookie{Name: "goepay_merchant", Value: other})
	forbidden := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(forbidden, denied)
	if forbidden.Code != 404 {
		t.Fatalf("other merchant detail status=%d want404", forbidden.Code)
	}
	provider.set("SUCCESS")
	if _, err = f.s.ProviderFlow.QuerySettlementPayout(ctx, req.ID, actor); err != nil {
		t.Fatal(err)
	}
	check(false)
}
