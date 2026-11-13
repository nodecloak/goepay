package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"goepay/internal/identity"
)

func cryptoChannelHTTP(f channel168Fixture, method, path, raw string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/console/v1/admin/payment-channels"+path, strings.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func TestCryptoChannelConfigurationIsUnifiedWithoutEnablingMainnet(t *testing.T) {
	f := channel168Local(t)
	f.s.Crypto = nil
	body := `{"name":"USDT TRON","network":"tron","enabled":true,"archived":false,"fee_bps":75,"min_amount":"0.000001","max_amount":"100","day_limit":"200","merchant_ids":["1"],"expected_revision":"0","request_id":"aaaabbbbccccddddeeeeffff00001111"}`
	w := cryptoChannelHTTP(f, "POST", "/crypto", body)
	if w.Code != 200 {
		t.Fatal("unified USDT configuration unavailable", w.Code, w.Body.String())
	}
	var created struct{ Data map[string]any }
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id, ok := created.Data["id"].(string)
	if !ok || created.Data["asset"] != "USDT" || created.Data["pay_type"] != "usdt" || created.Data["plugin"] != "usdt_tron" || created.Data["enabled"] != true || created.Data["runtime_enabled"] != false || f.s.Crypto != nil {
		t.Fatal("configured channel activated unavailable mainnet or lost identity", created.Data)
	}
	w = cryptoChannelHTTP(f, "GET", "?type=usdt&plugin=usdt_tron", "")
	var list struct {
		Data struct {
			Items []map[string]any
			Total int
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != 200 || list.Data.Total != 1 || list.Data.Items[0]["id"] != id {
		t.Fatal("USDT missing from common filtered channel list", w.Code, w.Body.String(), err)
	}
	flow := f.s.ProviderFlow
	f.s.ProviderFlow = nil
	w = cryptoChannelHTTP(f, "GET", "?type=usdt", "")
	f.s.ProviderFlow = flow
	if w.Code != 200 {
		t.Fatal("USDT catalog requires unrelated CNY provider", w.Code, w.Body.String())
	}
	for _, detail := range []string{"/" + id, "/crypto/" + id} {
		w = cryptoChannelHTTP(f, "GET", detail, "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"asset":"USDT"`) {
			t.Fatal("shared detail", detail, w.Code, w.Body.String())
		}
	}
	// Unknown save results can replay the original request but not allocate a second channel.
	w = cryptoChannelHTTP(f, "POST", "/crypto", body)
	var repeated struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &repeated) != nil || w.Code != 200 || repeated.Data["id"] != id {
		t.Fatal("config replay duplicated channel", w.Code, w.Body.String())
	}
	update := `{"name":"TRON renamed","network":"tron","enabled":false,"archived":false,"fee_bps":80,"min_amount":"0","max_amount":"100","day_limit":"200","merchant_ids":["1"],"expected_revision":"1"}`
	w = cryptoChannelHTTP(f, "PATCH", "/crypto/"+id, update)
	if w.Code != 200 {
		t.Fatal("config update", w.Code, w.Body.String())
	}
	w = cryptoChannelHTTP(f, "PATCH", "/crypto/"+id, update)
	if w.Code != 409 {
		t.Fatal("stale revision accepted", w.Code, w.Body.String())
	}
	var invoices, withdrawals, cny int
	if err := f.p.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM crypto_invoices),(SELECT count(*) FROM crypto_withdrawals),(SELECT count(*) FROM orders)`).Scan(&invoices, &withdrawals, &cny); err != nil || invoices != 0 || withdrawals != 0 || cny != 0 {
		t.Fatal("config save moved funds", invoices, withdrawals, cny, err)
	}
}

func TestCryptoChannelConfigurationRejectsCSRFOperatorAndAmbiguousFields(t *testing.T) {
	f := channel168Local(t)
	body := `{"name":"USDT","network":"tron","enabled":false,"archived":false,"fee_bps":0,"min_amount":"0","max_amount":"0","day_limit":"0","merchant_ids":["1"],"expected_revision":"0","request_id":"bbbbccccddddeeeeffff000011112222"}`
	for _, raw := range []string{strings.Replace(body, `"fee_bps":0`, `"fee_bps":0,"fee_bps":1`, 1), strings.Replace(body, `"fee_bps":0`, `"fee_bps":0,"private_key":"not-accepted"`, 1), strings.Replace(body, `"merchant_ids":["1"]`, `"merchant_ids":[1]`, 1), strings.Replace(body, `"enabled":false,`, "", 1), strings.Replace(body, `"enabled":false`, `"enabled":null`, 1), strings.Replace(body, `"fee_bps":0`, `"fee_bps":null`, 1)} {
		w := cryptoChannelHTTP(f, "POST", "/crypto", raw)
		if w.Code != 400 {
			t.Fatal("ambiguous configuration accepted", w.Code, w.Body.String())
		}
	}
	copy := f
	copy.csrf = "wrong"
	if w := cryptoChannelHTTP(copy, "POST", "/crypto", body); w.Code != 403 {
		t.Fatal("missing CSRF admitted", w.Code)
	}
	if _, err := f.p.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class,permissions) SELECT 'crypto-operator','admin',170,'Crypto operator',password_hash,'operator','[]'::jsonb FROM console_users WHERE role='admin' LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	operator := f
	var err error
	operator.token, operator.csrf, err = f.s.Sessions.Create(identity.User{ID: 170, Username: "crypto-operator", Role: "admin", AdminClass: "operator", AuthVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	if w := cryptoChannelHTTP(operator, "POST", "/crypto", body); w.Code == 200 {
		t.Fatal("operator created crypto channel")
	}
	var count int
	if err := f.p.QueryRow(t.Context(), `SELECT count(*) FROM crypto_channels`).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected writes mutated config", count, err)
	}
}

func TestParentAndCryptoChannelIDsShareOneAllocationNamespace(t *testing.T) {
	f, body := parent180ReadyCreate(t)
	w := cryptoChannelHTTP(f, "POST", "/crypto", `{"name":"shared-id-crypto","network":"tron","enabled":false,"archived":false,"fee_bps":0,"min_amount":"0","max_amount":"0","day_limit":"0","merchant_ids":["1"],"expected_revision":"0","request_id":"ccccddddeeeeffff0000111122223333"}`)
	var created struct{ Data map[string]any }
	if json.Unmarshal(w.Body.Bytes(), &created) != nil || w.Code != 200 {
		t.Fatal("crypto setup", w.Code, w.Body.String())
	}
	channelID, err := strconv.ParseInt(created.Data["id"].(string), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	// Reset the allocator to reproduce stale imported sequence state; the allocator must also inspect stored channels.
	if _, err = f.p.Exec(t.Context(), `SELECT setval('runtime_parent_channel_ids',1,false)`); err != nil {
		t.Fatal(err)
	}
	parent := parent180Post(t, f, body)
	id, err := strconv.ParseInt(parent["id"].(string), 10, 64)
	if err != nil || id <= channelID {
		t.Fatal("CNY allocation collided with crypto channel", id, channelID, err)
	}
}
