package backoffice

import (
	"encoding/json"
	"fmt"
	"goepay/internal/payment"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGroupDepositSettingsCreateAtomicallyAndKeepClosedHistoryReadable(t *testing.T) {
	s := setup(t)
	ctx := t.Context()
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "synthetic-deposit"}
	group, e := s.Create(ctx, admin, "groups", input(`{"name":"deposit group","enabled":true,"user_deposit":"1","user_deposit_min":"12.34","user_deposit_day":"7"}`))
	if e != nil {
		t.Fatal("reference deposit group fields must create atomically", e)
	}
	o, e := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 101, OutTradeNo: "deposit-http-fund", Amount: 10000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.store.MarkPaid(ctx, o.TradeNo, "deposit-http-fund-paid"); e != nil {
		t.Fatal(e)
	}
	held, e := s.store.Deposit(ctx, 101, "hold", "deposit-http-original", 1000, "merchant:101")
	if e != nil {
		t.Fatal(e)
	}
	released, e := s.store.Deposit(ctx, 101, "release", "deposit-http-review", 100, "merchant:101")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=101`, group.ID); e != nil {
		t.Fatal(e)
	}
	request := func(method, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest(method, "/deposits", strings.NewReader(body))
		if !s.serveBusiness(w, r, merchant, []string{"deposits"}) {
			t.Fatal("deposit route not dispatched")
		}
		return w
	}
	w := request("GET", "")
	var envelope struct {
		Data struct {
			Minimum string `json:"deposit_min_money"`
			Days    int    `json:"deposit_day"`
		}
	}
	if json.Unmarshal(w.Body.Bytes(), &envelope) != nil || w.Code != 200 || envelope.Data.Minimum != "12.34" || envelope.Data.Days != 7 {
		t.Fatalf("merchant summary did not apply effective deposit policy: %d %s", w.Code, w.Body)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"user_deposit":"0"}'::jsonb WHERE resource='groups' AND id=$1`, group.ID); e != nil {
		t.Fatal(e)
	}
	seedSetting(t, s, "user_deposit", "0")
	if w = request("GET", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "deposit-http-original") {
		t.Fatalf("closed feature hid original GET/history: %d %s", w.Code, w.Body)
	}
	if w = request("POST", `{"action":"hold","external_no":"deposit-http-original","amount":"10.00"}`); w.Code != 201 || !strings.Contains(w.Body.String(), fmt.Sprintf(`"id":"%d"`, held.ID)) {
		t.Fatalf("closed feature blocked original POST replay: %d %s", w.Code, w.Body)
	}
	if w = request("POST", `{"action":"hold","external_no":"deposit-http-new","amount":"1.00"}`); w.Code < 400 {
		t.Fatal("closed feature accepted a new hold")
	}
	if _, e = s.store.ReviewDeposit(ctx, released.ID, true, "admin:test"); e != nil {
		t.Fatal("closed feature blocked original administrator approval", e)
	}
	for _, bad := range []string{`{"name":"bad","user_deposit":"2"}`, `{"name":"bad","user_deposit_min":"-1"}`, `{"name":"bad","user_deposit_day":"3661"}`} {
		if _, e = s.Create(ctx, admin, "groups", input(bad)); e == nil {
			t.Fatal("bad group deposit policy accepted", bad)
		}
	}
}

func TestGroupDepositHistoricalProjectionKeepsDisabledAndBadPolicyRows(t *testing.T) {
	s := setup(t)
	ctx := t.Context()
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "history-only"}
	o, e := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 101, OutTradeNo: "deposit-projection-fund", Amount: 10000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.store.MarkPaid(ctx, o.TradeNo, "deposit-projection-paid"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.store.Deposit(ctx, 101, "hold", "deposit-projection-original", 1000, "merchant:101"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET enabled=false WHERE id=101; UPDATE backoffice_records SET body=body||'{"user_deposit_min":"1.00"}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	if cfg, e := s.DepositSettingsForMerchant(ctx, 101); e != nil || cfg.MinCents != 100 {
		t.Errorf("disabled merchant read-only policy unavailable: %+v %v", cfg, e)
	}
	read := func(p Principal) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.listBusiness(w, httptest.NewRequest("GET", "/deposits", nil), p, "deposits")
		return w
	}
	if w := read(merchant); w.Code != 200 || !strings.Contains(w.Body.String(), "deposit-projection-original") {
		t.Errorf("disabled merchant history hidden by policy summary: %d %s", w.Code, w.Body)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"user_deposit_min":true}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	w := read(merchant)
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &envelope) != nil || !strings.Contains(w.Body.String(), "deposit-projection-original") || string(envelope.Data["deposit_policy_available"]) != "false" {
		t.Errorf("bad current policy swallowed historical rows or invented known limits: %d %s", w.Code, w.Body)
	}
	if _, exists := envelope.Data["deposit_min_money"]; exists {
		t.Error("unavailable policy advertised a numeric deposit minimum")
	}
	if w = read(admin); w.Code != 200 || !strings.Contains(w.Body.String(), "deposit-projection-original") {
		t.Errorf("administrator history hidden: %d %s", w.Code, w.Body)
	}
	if _, e = s.store.Deposit(ctx, 101, "hold", "disabled-projection-new", 100, "merchant:101"); e == nil {
		t.Error("read-only projection relaxed disabled merchant new writes")
	}
}
