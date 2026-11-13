package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"goepay/internal/compat"
)

func cryptoGroupResourceRequest(t *testing.T, f channel168Fixture, method, resource string, body any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, "/console/v1/admin/resources/"+resource, strings.NewReader(string(raw)))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var out struct {
		Code int
		Data map[string]any
	}
	if err = json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.Code != 0 || w.Code != 200 && w.Code != 201 {
		t.Fatalf("group assignment resource %s %s: HTTP=%d body=%s err=%v", method, resource, w.Code, w.Body.String(), err)
	}
	return out.Data
}

func cryptoGroupSignedRequest(t *testing.T, f *email197Fixture, no string, channel int64) int {
	t.Helper()
	p := compat.Params{"pid": "1", "type": "usdt", "out_trade_no": no, "name": "group assignment", "money": "7.20", "sign_type": "MD5"}
	if channel > 0 {
		p["channel_id"] = strconv.FormatInt(channel, 10)
	}
	p["sign"] = compat.SignMD5(p, "test-secret")
	values := url.Values{}
	for key, value := range p {
		values.Set(key, value)
	}
	r := httptest.NewRequest("POST", "/mapi.php", strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	var out struct{ Code int }
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("signed group request returned invalid JSON: %s %v", w.Body.String(), err)
	}
	return out.Code
}

func TestCryptoGroupAssignmentControlsConsoleSignatureCashierAndRevocation(t *testing.T) {
	f := cryptoAdmissionFixture(t)
	tron, ethereum := cryptoAdmissionChannels(t, f)
	admin := f.channel168Fixture
	for _, address := range []string{"0x1111111111111111111111111111111111111111", "0x2222222222222222222222222222222222222222"} {
		if err := f.s.Crypto.AddAddress(t.Context(), "ethereum", address, "deposit"); err != nil {
			t.Fatal(err)
		}
	}
	policy := func(channel, rate string) map[string]any {
		return map[string]any{"info": map[string]any{"usdt": map[string]string{"channel": channel, "type": "channel", "rate": rate}}}
	}
	assign := func(id string) {
		cryptoGroupResourceRequest(t, admin, "PATCH", "merchants/1", map[string]any{"group_id": id, "confirm_group_assignment": true})
	}
	choices := func(want ...int64) {
		t.Helper()
		w := cryptoAdmissionConsole(f, "GET", "/channels", "")
		var out struct {
			Data struct {
				Items []struct {
					ID string `json:"id"`
				}
			}
		}
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || w.Code != 200 || len(out.Data.Items) != len(want) {
			t.Fatalf("group channel choices: %d %s %v", w.Code, w.Body.String(), err)
		}
		for i, id := range want {
			if out.Data.Items[i].ID != strconv.FormatInt(id, 10) {
				t.Fatal("group assignment exposed another channel", out.Data.Items)
			}
		}
	}
	// Empty default group is not an implicit grant to every enabled channel.
	cryptoGroupResourceRequest(t, admin, "PATCH", "groups/0", map[string]any{"info": map[string]any{}})
	choices()
	if code := cryptoGroupSignedRequest(t, f, "group-unassigned", tron.ID); code != -1 {
		t.Fatal("unassigned explicit channel bypassed default group", code)
	}
	if w := cryptoAdmissionConsole(f, "POST", "/invoices", `{"out_trade_no":"group-unassigned-console","channel_id":"`+strconv.FormatInt(tron.ID, 10)+`","amount":"1"}`); w.Code < 400 {
		t.Fatal("unassigned console channel bypassed group")
	}
	cryptoPolicyNoAllocation(t, f)
	group := policy(strconv.FormatInt(tron.ID, 10), "99.5")
	group["name"] = "TRON assigned group"
	created := cryptoGroupResourceRequest(t, admin, "POST", "groups", group)
	groupID, ok := created["id"].(string)
	if !ok {
		t.Fatal("group ID not preserved as a string")
	}
	assign(groupID)
	choices(tron.ID)
	// The old CNY-only environment mapping never grants these newly created IDs.
	if len(f.s.MerchantChannels[1]) != 2 || f.s.MerchantChannels[1][0] == tron.ID {
		t.Fatal("fixture accidentally supplied an environment channel grant")
	}
	if code := cryptoGroupSignedRequest(t, f, "group-wrong-explicit", ethereum.ID); code != -1 {
		t.Fatal("explicit channel escaped its assigned group", code)
	}
	w := cryptoAdmissionConsole(f, "POST", "/invoices", `{"out_trade_no":"group-console-original","channel_id":"`+strconv.FormatInt(tron.ID, 10)+`","amount":"1"}`)
	if w.Code != 200 {
		t.Fatal("group assignment alone did not enable console collection", w.Code, w.Body.String())
	}
	// The same existing group editor can change the assigned channel and rate.
	cryptoGroupResourceRequest(t, admin, "PATCH", "groups/"+groupID, policy(strconv.FormatInt(ethereum.ID, 10), "99.25"))
	choices(ethereum.ID)
	if code := cryptoGroupSignedRequest(t, f, "group-revoked-tron", tron.ID); code != -1 {
		t.Fatal("revoked assignment remained usable for a new payment", code)
	}
	if code := cryptoGroupSignedRequest(t, f, "group-signed-original", ethereum.ID); code != 1 {
		t.Fatal("assigned group did not admit signed payment", code)
	}
	// An inherited route reads default group 0, while this group's fee remains its own.
	cryptoGroupResourceRequest(t, admin, "PATCH", "groups/0", policy(strconv.FormatInt(ethereum.ID, 10), "90"))
	cryptoGroupResourceRequest(t, admin, "PATCH", "groups/"+groupID, map[string]any{"info": map[string]any{"usdt": map[string]string{"rate": "99.25"}}})
	choices(ethereum.ID)
	intent := cashierIntent{Version: "v1", MerchantID: 1, OutTradeNo: "group-cashier-original", Name: "group assignment", Money: "7.20", ClientIP: "192.0.2.206", Expires: time.Now().Add(time.Minute).Unix()}
	endpoint := f.s.cashierURL(intent)
	w = cryptoPolicyCashierRequest(f, endpoint)
	if w.Code != http.StatusFound {
		t.Fatal("inherited group did not admit cashier collection", w.Code, w.Body.String())
	}
	invoice, err := f.s.Crypto.FindInvoice(t.Context(), 1, "", intent.OutTradeNo)
	if err != nil || invoice.ChannelID != ethereum.ID || invoice.Fee != "0.0075" || invoice.FeeSource != "group" {
		t.Fatal("inherited route overwrote current group fee or channel", invoice, err)
	}
	cryptoGroupResourceRequest(t, admin, "PATCH", "groups/0", policy("0", "90"))
	choices()
	if code := cryptoGroupSignedRequest(t, f, "group-default-disabled", ethereum.ID); code != -1 {
		t.Fatal("closed inherited default still admitted new payment", code)
	}
	if code := cryptoGroupSignedRequest(t, f, "group-signed-original", ethereum.ID); code != 1 {
		t.Fatal("revoked assignment lost original signed order replay", code)
	}
	if w = cryptoPolicyCashierRequest(f, endpoint); w.Code != http.StatusFound || !strings.Contains(w.Header().Get("Location"), invoice.ID) {
		t.Fatal("revoked assignment lost original cashier route", w.Code)
	}
	assign("0")
	choices()
	var grants, cnyOrders, invoices int
	if err = f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM crypto_channel_grants),(SELECT count(*) FROM orders),(SELECT count(*) FROM crypto_invoices)`).Scan(&grants, &cnyOrders, &invoices); err != nil || grants != 0 || cnyOrders != 0 || invoices != 3 {
		t.Fatal("group assignment created inverse grants or rejected payment state", grants, cnyOrders, invoices, err)
	}
}
