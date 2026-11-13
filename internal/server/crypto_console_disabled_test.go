package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"goepay/internal/cryptopay"
	"goepay/internal/identity"
)

func TestCryptoConsoleUnconfiguredReportsClosedCapability(t *testing.T) {
	for _, role := range []string{"merchant", "admin"} {
		t.Run(role, func(t *testing.T) {
			s := &Server{}
			w := httptest.NewRecorder()
			s.cryptoConsole(w, httptest.NewRequest("GET", "/console/v1/"+role+"/crypto/config", nil), role, identity.Session{User: identity.User{ID: 1, AdminClass: "super"}}, []string{role, "crypto", "config"}, "")
			var out struct {
				Code int
				Data struct {
					Configured *bool
					Enabled    bool
					Networks   []json.RawMessage
				}
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || out.Code != 0 || out.Data.Configured == nil || *out.Data.Configured || out.Data.Enabled || out.Data.Networks == nil || len(out.Data.Networks) != 0 {
				t.Fatal("unconfigured service capability", w.Code, w.Body.String(), err)
			}
		})
	}
}

func TestCryptoConsoleUnconfiguredRejectsReadsAndWritesWithClosedMessage(t *testing.T) {
	for _, role := range []string{"merchant", "admin"} {
		routes := []struct{ method, resource string }{{"GET", "balances"}, {"GET", "invoices"}, {"GET", "withdrawals"}, {"GET", "recipients"}, {"POST", "invoices"}, {"POST", "withdrawals"}, {"POST", "recipients"}, {"POST", "refunds"}, {"POST", "held-refunds"}}
		if role == "admin" {
			routes = append(routes, struct{ method, resource string }{"GET", "reconciliation"}, struct{ method, resource string }{"POST", "addresses"})
		}
		for _, route := range routes {
			t.Run(role+"/"+route.method+"/"+route.resource, func(t *testing.T) {
				w := httptest.NewRecorder()
				(&Server{}).cryptoConsole(w, httptest.NewRequest(route.method, "/console/v1/"+role+"/crypto/"+route.resource, nil), role, identity.Session{User: identity.User{ID: 1, AdminClass: "super"}}, []string{role, "crypto", route.resource}, "")
				var out struct {
					Code    int
					Message string
					Data    json.RawMessage
				}
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 503 || out.Code != -1 || out.Message != "USDT 收款与结算尚未启用，当前无法处理此操作" || string(out.Data) != "null" {
					t.Fatal("unconfigured service must not imply a pending financial request", w.Code, w.Body.String(), err)
				}
			})
		}
	}
}

func TestCryptoConsoleUnconfiguredPreservesSuperAdministratorGate(t *testing.T) {
	for _, resource := range []string{"config", "balances", "invoices"} {
		w := httptest.NewRecorder()
		(&Server{}).cryptoConsole(w, httptest.NewRequest("GET", "/console/v1/admin/crypto/"+resource, nil), "admin", identity.Session{User: identity.User{AdminClass: "operator"}}, []string{"admin", "crypto", resource}, "")
		if w.Code != 403 {
			t.Fatal("unconfigured service bypassed administrator authority", resource, w.Code, w.Body.String())
		}
	}
}

func TestCryptoConsoleDisabledNetworksPreserveScopedHistoryAndRejectNewFunds(t *testing.T) {
	f := email197Local(t)
	svc := &cryptopay.Service{Pool: f.p, TokenKey: make([]byte, 32), Networks: map[string]cryptopay.NetworkConfig{"tron": {Enabled: true}, "ethereum": {Enabled: false}}}
	f.s.Crypto = svc
	const depositAddress = "TJRabPrwbZy45sbavfcjinPJC18kjpRTv8"
	const recipientAddress = "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE"
	if err := svc.AddAddress(t.Context(), "tron", depositAddress, "deposit"); err != nil {
		t.Fatal(err)
	}
	invoice, err := svc.CreateInvoice(t.Context(), cryptopay.CreateInvoiceInput{MerchantID: 1, OutTradeNo: "disabled-history-invoice", Network: "tron", Amount: "10"})
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.RecordDeposit(t.Context(), cryptopay.Deposit{Network: "tron", TxID: "synthetic-disabled-history", BlockHash: "synthetic-disabled-block", BlockHeight: 1, Address: depositAddress, Amount: "10", OccurredAt: invoice.ExpiresAt.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	recipient, err := svc.RequestRecipient(t.Context(), 1, "tron", recipientAddress)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = svc.ApproveRecipient(t.Context(), recipient.ID, 168); err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.Exec(t.Context(), "UPDATE crypto_recipients SET usable_at=clock_timestamp()-interval '1 second' WHERE id=$1", recipient.ID); err != nil {
		t.Fatal(err)
	}
	outgoing, err := svc.RequestWithdrawal(t.Context(), cryptopay.WithdrawalInput{MerchantID: 1, RequestID: "disabled-history-withdrawal", Network: "tron", Address: recipientAddress, Amount: "1"})
	if err != nil {
		t.Fatal(err)
	}
	svc.Networks["tron"] = cryptopay.NetworkConfig{Enabled: false}
	request := func(role, token, csrf, method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/"+role+"/crypto"+path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.197:12345"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		r.AddCookie(&http.Cookie{Name: "goepay_" + role, Value: token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	for _, actor := range []struct{ role, token, csrf string }{{"merchant", f.token, f.csrf}, {"admin", f.channel168Fixture.token, f.channel168Fixture.csrf}} {
		w := request(actor.role, actor.token, actor.csrf, "GET", "/config", "")
		var config struct {
			Data struct {
				Configured *bool
				Enabled    bool
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &config); err != nil || w.Code != 200 || config.Data.Configured == nil || !*config.Data.Configured || config.Data.Enabled {
			t.Fatal("configured closed networks lost read capability", actor.role, w.Code, w.Body.String(), err)
		}
		for _, history := range []struct{ path, evidence string }{{"/balances", `"available":"9"`}, {"/invoices", invoice.ID}, {"/withdrawals", outgoing.ID}, {"/recipients", recipient.ID}, {"/withdrawals/by-request?merchant_id=1&request_id=disabled-history-withdrawal", outgoing.ID}} {
			w = request(actor.role, actor.token, actor.csrf, "GET", history.path, "")
			if w.Code != 200 || !strings.Contains(w.Body.String(), history.evidence) || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("closed networks hid original scoped history", actor.role, history.path, w.Code, w.Body.String())
			}
		}
	}
	for _, path := range []string{"/balances?merchant_id=2", "/invoices?merchant_id=2", "/withdrawals?merchant_id=2", "/recipients?merchant_id=2"} {
		if w := request("merchant", f.token, f.csrf, "GET", path, ""); w.Code != 403 {
			t.Fatal("closed networks weakened merchant scope", path, w.Code, w.Body.String())
		}
	}
	for _, mutation := range []struct{ path, body string }{
		{"/invoices", `{"out_trade_no":"closed-new-invoice","network":"tron","amount":"1"}`},
		{"/withdrawals", `{"request_id":"closed-new-withdrawal","network":"tron","address":"` + recipientAddress + `","amount":"1","current_password":"local-email197-password"}`},
		{"/recipients", `{"network":"tron","address":"` + recipientAddress + `","current_password":"local-email197-password"}`},
	} {
		if w := request("merchant", f.token, f.csrf, "POST", mutation.path, mutation.body); w.Code != 422 {
			t.Fatal("closed network accepted a new funds request", mutation.path, w.Code, w.Body.String())
		}
	}
	tx, err := f.p.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	delegatedToken, delegatedCSRF, _, err := f.s.Sessions.CreateDelegated(t.Context(), tx, identity.User{ID: 1, Role: "merchant", Username: "email197", AuthVersion: 1}, identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super", AuthVersion: 1}, f.channel168Fixture.token)
	if err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(t.Context()); err != nil {
		t.Fatal(err)
	}
	if w := request("merchant", delegatedToken, delegatedCSRF, "GET", "/invoices", ""); w.Code != 200 || !strings.Contains(w.Body.String(), invoice.ID) {
		t.Fatal("delegated account lost read-only history", w.Code, w.Body.String())
	}
	if w := request("merchant", delegatedToken, delegatedCSRF, "POST", "/invoices", `{"out_trade_no":"delegated-closed-invoice","network":"tron","amount":"1"}`); w.Code != 403 {
		t.Fatal("delegated account gained funds write authority", w.Code, w.Body.String())
	}
	var invoices, withdrawals, recipients, journals, reserved int
	// The original deposit, invoice credit and settlement reserve each have a journal.
	if err = f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM crypto_invoices),(SELECT count(*) FROM crypto_withdrawals),(SELECT count(*) FROM crypto_recipients),(SELECT count(*) FROM crypto_journals),(SELECT balance FROM crypto_accounts WHERE owner_id=1 AND network='tron' AND kind='reserved')`).Scan(&invoices, &withdrawals, &recipients, &journals, &reserved); err != nil || invoices != 1 || withdrawals != 1 || recipients != 1 || journals != 3 || reserved != 1000000 {
		t.Fatal("rejected operations changed original financial records", invoices, withdrawals, recipients, journals, reserved, err)
	}
}
