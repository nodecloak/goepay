package server

import (
	"goepay/internal/identity"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChannelSharingFundsTargetProtectsOnlyCanonicalFinancialSubmit(t *testing.T) {
	d, ok := fundsTarget("/console/v1/admin/channel-sharing/orders")
	if !ok || d.guard != "channel-sharing-submit" || d.category != "channel-sharing" {
		t.Fatalf("sharing submit has no funds guard: %#v %v", d, ok)
	}
	if d, ok := fundsTarget("/console/v1/admin/channel-sharing/returns"); !ok || d.guard != "channel-sharing-return" {
		t.Fatal("sharing returns missing financial authorization")
	}
	if d, ok := fundsTarget("/console/v1/admin/channel-sharing/finishes"); !ok || d.guard != "channel-sharing-finish" {
		t.Fatal("sharing finish missing financial authorization")
	}
	for _, target := range []string{"/console/v1/admin/channel-sharing/orders/query", "/console/v1/admin/channel-sharing/orders?merchant_id=7", "/console/v1/admin/channel-sharing/../orders", "/console/v1/merchant/channel-sharing/orders"} {
		if _, ok := fundsTarget(target); ok {
			t.Fatalf("unexpected protected target %q", target)
		}
	}
}

func TestChannelSharingStrictRuleFundsBodyAndLocalBoundary(t *testing.T) {
	d, _ := fundsTarget("/console/v1/admin/channel-sharing/rules/7")
	valid := `{"merchant_id":"1","expected":"0","enabled":true,"minimum_amount":"0","methods":["native"],"items":[{"receiver_id":"9","mode":"rate","rate_bps":"100","fixed_amount":"0"}]}`
	if _, _, e := normalizeSharingFundsBody([]byte(valid), d); e != nil {
		t.Fatal("typed nested rule rejected", e)
	}
	for _, raw := range []string{strings.Replace(valid, `"rate_bps":"100"`, `"rate_bps":"100","rate_bps":"200"`, 1), strings.Replace(valid, `"mode":"rate"`, `"mode":"rate","account":"unexpected"`, 1), strings.Replace(valid, `"enabled":true`, `"enabled":null`, 1), strings.Replace(valid, `"expected":"0"`, `"expected":"00"`, 1)} {
		if _, _, e := normalizeSharingFundsBody([]byte(raw), d); e == nil {
			t.Fatal("ambiguous rule accepted", raw)
		}
	}
	s := &Server{}
	for _, item := range []struct {
		role, csrf string
		user       identity.User
		want       int
	}{{"admin", "", identity.User{Role: "admin", ID: 1, Username: "operator", AdminClass: "operator"}, 403}, {"merchant", "csrf", identity.User{Role: "admin", ID: 1, Username: "admin", AdminClass: "super"}, 403}} {
		r := httptest.NewRequest("POST", "/console/v1/"+item.role+"/channel-sharing/orders", strings.NewReader(`{}`))
		r.Header.Set("X-CSRF-Token", item.csrf)
		w := httptest.NewRecorder()
		if !s.channelSharingConsole(w, r, item.role, identity.Session{User: item.user, CSRF: "csrf"}, []string{item.role, "channel-sharing", "orders"}) || w.Code != item.want {
			t.Fatal("direct hook bypass", w.Code, w.Body.String())
		}
	}
	var out sharingConsoleInput
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"merchant_id":"1","external_no":"x","account":"forbidden"}`))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	if sharingConsoleBody(w, r, []string{"admin", "channel-sharing", "receivers", "9", "bind"}, &out) {
		t.Fatal("irrelevant private receiver field accepted")
	}
}
