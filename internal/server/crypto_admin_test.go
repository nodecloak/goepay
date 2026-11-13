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

func TestCryptoSuperAdministratorHTTPAuthorityAndApprovals(t *testing.T) {
	f := email197Local(t)
	f.s.Crypto = &cryptopay.Service{Pool: f.p, TokenKey: make([]byte, 32), Networks: map[string]cryptopay.NetworkConfig{"tron": {Enabled: true}}}
	if _, e := f.p.Exec(t.Context(), "UPDATE console_users SET password_hash=(SELECT password_hash FROM console_users WHERE username='email197' AND role='merchant') WHERE username='admin168' AND role='admin'"); e != nil {
		t.Fatal(e)
	}
	user := identity.User{ID: 168, Role: "admin", Username: "admin168", AdminClass: "super"}
	if e := f.p.QueryRow(t.Context(), "SELECT auth_version FROM console_users WHERE username='admin168' AND role='admin'").Scan(&user.AuthVersion); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := f.s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	request := func(path, body, proof string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/console/v1/admin/crypto"+path, strings.NewReader(body))
		r.RemoteAddr = "192.0.2.170:12345"
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", proof)
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	const address = "TJRabPrwbZy45sbavfcjinPJC18kjpRTv8"
	const recipientAddress = "TQn9Y2khEsLJW1ChVWFMSMeRDow5KcbLSE"
	body := `{"network":"tron","address":"` + address + `","role":"deposit"}`
	if w := request("/addresses", body, csrf); w.Code != 422 {
		t.Fatal("missing current password admitted", w.Code, w.Body.String())
	}
	body = strings.TrimSuffix(body, "}") + `,"current_password":"local-email197-password"}`
	if w := request("/addresses", body, "wrong-csrf"); w.Code != 403 {
		t.Fatal("super CSRF bypass", w.Code)
	}
	if w := request("/addresses", body, csrf); w.Code != 200 {
		t.Fatal("super address registration", w.Code, w.Body.String())
	}
	invoice, e := f.s.Crypto.CreateInvoice(t.Context(), cryptopay.CreateInvoiceInput{MerchantID: 1, OutTradeNo: "admin-http", Network: "tron", Amount: "10"})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Crypto.RecordDeposit(t.Context(), cryptopay.Deposit{Network: "tron", TxID: "admin-http-deposit", BlockHeight: 1, BlockHash: "admin-block", Address: address, Amount: "10", OccurredAt: invoice.ExpiresAt.Add(-time.Minute)}); e != nil {
		t.Fatal(e)
	}
	recipient, e := f.s.Crypto.RequestRecipient(t.Context(), 1, "tron", recipientAddress)
	if e != nil {
		t.Fatal(e)
	}
	password := `{"current_password":"local-email197-password"}`
	w := request("/recipients/"+recipient.ID+"/approve", password, csrf)
	if w.Code != 200 {
		t.Fatal("super recipient approval", w.Code, w.Body.String())
	}
	var result struct{ Data cryptopay.Recipient }
	if e = json.Unmarshal(w.Body.Bytes(), &result); e != nil || result.Data.State != "approved" || !result.Data.UsableAt.Equal(recipient.UsableAt) {
		t.Fatal("approval shortened cooling period", result, e)
	}
	if _, e = f.p.Exec(t.Context(), "UPDATE crypto_recipients SET usable_at=clock_timestamp()-interval '1 second' WHERE id=$1", recipient.ID); e != nil {
		t.Fatal(e)
	}
	outgoing, e := f.s.Crypto.RequestWithdrawal(t.Context(), cryptopay.WithdrawalInput{MerchantID: 1, RequestID: "admin-http-outgoing", Network: "tron", Address: recipientAddress, Amount: "1"})
	if e != nil {
		t.Fatal(e)
	}
	w = request("/withdrawals/"+outgoing.ID+"/approve", password, csrf)
	if w.Code != 200 {
		t.Fatal("super withdrawal approval", w.Code, w.Body.String())
	}
	approved, e := f.s.Crypto.GetWithdrawal(t.Context(), outgoing.ID)
	if e != nil || approved.State != "approved" || approved.ApprovedBy != 168 {
		t.Fatal("approval actor not recorded", approved, e)
	}
	w = request("/sweeps/retry", `{"network":"tron","address":"`+address+`","current_password":"local-email197-password"}`, csrf)
	if w.Code != 200 {
		t.Fatal("super sweep resume", w.Code, w.Body.String())
	}
	if _, e = f.p.Exec(t.Context(), "UPDATE console_users SET admin_class='operator' WHERE username='admin168' AND role='admin'"); e != nil {
		t.Fatal(e)
	}
	if w = request("/addresses", body, csrf); w.Code != 401 && w.Code != 403 {
		t.Fatal("revoked super authority survived", w.Code, w.Body.String())
	}
	user.AdminClass = "operator"
	if e = f.p.QueryRow(t.Context(), "SELECT auth_version FROM console_users WHERE username='admin168' AND role='admin'").Scan(&user.AuthVersion); e != nil {
		t.Fatal(e)
	}
	token, csrf, e = f.s.Sessions.Create(user)
	if e != nil {
		t.Fatal(e)
	}
	if w = request("/addresses", body, csrf); w.Code != 403 {
		t.Fatal("operator entered super-only USDT family", w.Code, w.Body.String())
	}
}
