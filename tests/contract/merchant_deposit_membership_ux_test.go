package contract_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

// B13 #9 + B15 #24: admin can configure user_deposit / user_deposit_min /
// user_deposit_day, the merchant deposit info surfaces the minimum, shortfall
// and day window, the withdrawal rule is enforced server-side, and the feature
// switch gates the merchant deposit endpoint (default ON).
func TestMerchantDepositSettingsInfoAndWithdrawGate(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(pool, false)
	admin, acsrf, e := s.Sessions.Create(identity.User{Username: "admin", Role: "admin", ID: 1, AuthVersion: 1, AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	merchant, mcsrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}

	// Fund a real synthetic ledger before enabling the payment deposit minimum.
	// History checks below must retain a genuine hold, not an empty fixture.
	paid, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "deposit-history-income", Amount: 30000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, paid.TradeNo, "deposit-history-paid"); e != nil {
		t.Fatal(e)
	}
	// --- B13 #9: admin saves the three keys through the generic settings resource.
	for _, kv := range [][2]string{{"user_deposit", "1"}, {"user_deposit_min", "100.00"}, {"user_deposit_day", "7"}} {
		body := fmt.Sprintf(`{"key":"%s","value":"%s"}`, kv[0], kv[1])
		w := lookupHTTP(t, s.Handler(), "POST", "/console/v1/admin/resources/settings", body, admin, "admin", acsrf)
		if w.Code != 201 && w.Code != 200 {
			t.Fatalf("admin save %s=%s: %d %s", kv[0], kv[1], w.Code, w.Body.String())
		}
	}
	// A bad value is rejected.
	if w := lookupHTTP(t, s.Handler(), "POST", "/console/v1/admin/resources/settings", `{"key":"user_deposit_day","value":"-3"}`, admin, "admin", acsrf); w.Code == 200 || w.Code == 201 {
		t.Fatalf("invalid user_deposit_day accepted: %d", w.Code)
	}

	// --- B15 #24: merchant deposit info surfaces min / shortfall / day.
	w := lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/workflows/deposits", "", merchant, "merchant", mcsrf)
	if w.Code != 200 {
		t.Fatalf("deposit info: %d %s", w.Code, w.Body.String())
	}
	data := lookupData(t, w)
	if data["deposit_min_money"] != "100.00" {
		t.Errorf("deposit_min_money=%v want 100.00", data["deposit_min_money"])
	}
	// held balance is 0, so shortfall equals the minimum.
	if data["deposit_shortfall_money"] != "100.00" {
		t.Errorf("deposit_shortfall_money=%v want 100.00", data["deposit_shortfall_money"])
	}
	if fmt.Sprint(data["deposit_day"]) != "7" {
		t.Errorf("deposit_day=%v want 7", data["deposit_day"])
	}

	// --- B15 #24 withdraw rule: a recent order blocks a release (withdrawal).
	if _, e = pool.Exec(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,status) VALUES('dep-gate-order',1,'dep-gate-out','probe','alipay',100,'paid')`); e != nil {
		t.Fatal(e)
	}
	// First fund the deposit so a release has something to draw on.
	hold := `{"action":"hold","amount":"200.00","external_no":"dep-hold-1"}`
	if w = lookupHTTP(t, s.Handler(), "POST", "/console/v1/merchant/workflows/deposits", hold, merchant, "merchant", mcsrf); w.Code != 201 {
		t.Fatalf("funded deposit hold: %d %s", w.Code, w.Body.String())
	}
	heldID := lookupData(t, w)["id"]
	release := `{"action":"release","amount":"10.00","external_no":"dep-release-blocked"}`
	w = lookupHTTP(t, s.Handler(), "POST", "/console/v1/merchant/workflows/deposits", release, merchant, "merchant", mcsrf)
	if w.Code == 201 {
		t.Fatalf("withdrawal allowed despite a recent order within deposit_day window: %s", w.Body.String())
	}

	// Closing new deposits must retain genuine balances and existing actions.
	if _, e = pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"0"') WHERE resource='settings' AND body->>'key'='user_deposit'`); e != nil {
		t.Fatal(e)
	}
	if w = lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/workflows/deposits", "", merchant, "merchant", mcsrf); w.Code != 200 {
		t.Fatalf("deposit feature off erased history: %d %s", w.Code, w.Body.String())
	}
	data = lookupData(t, w)
	items, ok := data["items"].([]any)
	if data["deposit_enabled"] != false || data["deposit_money"] != "200.00" || data["pending_release_money"] != "0.00" || !ok || len(items) != 1 || items[0].(map[string]any)["id"] != heldID {
		t.Fatal("disabled deposit lost its actual balance/history", data)
	}
	var before, after [3]int64
	count := func(out *[3]int64) {
		t.Helper()
		if e := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM deposit_actions),(SELECT count(*) FROM deposit_action_events),(SELECT count(*) FROM journals)`).Scan(&out[0], &out[1], &out[2]); e != nil {
			t.Fatal(e)
		}
	}
	count(&before)
	if w = lookupHTTP(t, s.Handler(), "POST", "/console/v1/merchant/workflows/deposits", hold, merchant, "merchant", mcsrf); w.Code != 201 || lookupData(t, w)["id"] != heldID {
		t.Fatal("closed feature invalidated an original deposit replay", w.Code, w.Body.String())
	}
	newHold := strings.Replace(hold, "dep-hold-1", "dep-hold-after-disable", 1)
	if w = lookupHTTP(t, s.Handler(), "POST", "/console/v1/merchant/workflows/deposits", newHold, merchant, "merchant", mcsrf); w.Code != 403 {
		t.Fatalf("deposit feature off did not gate the merchant endpoint: %d", w.Code)
	}
	newRelease := strings.Replace(release, "dep-release-blocked", "dep-release-after-disable", 1)
	if w = lookupHTTP(t, s.Handler(), "POST", "/console/v1/merchant/workflows/deposits", newRelease, merchant, "merchant", mcsrf); w.Code != 403 {
		t.Fatalf("closed deposit admitted a new release: %d", w.Code)
	}
	count(&after)
	if before != after {
		t.Fatal("history/replay/refused new deposits created financial effects", before, after)
	}
	if held, pending, err := s.Store.DepositBalance(ctx, 1); err != nil || held != 20000 || pending != 0 {
		t.Fatal("closed deposit history/replay changed real funds", held, pending, err)
	}
	// Admin review listing stays available regardless of the merchant toggle.
	if w = lookupHTTP(t, s.Handler(), "GET", "/console/v1/admin/workflows/deposits", "", admin, "admin", acsrf); w.Code != 200 {
		t.Fatalf("admin deposit listing gated by merchant toggle: %d %s", w.Code, w.Body.String())
	}

	// --- feature default ON: with the key removed, the page is available again.
	if _, e = pool.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'='user_deposit'`); e != nil {
		t.Fatal(e)
	}
	if w = lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/workflows/deposits", "", merchant, "merchant", mcsrf); w.Code != 200 {
		t.Fatalf("missing user_deposit row must default ON: %d %s", w.Code, w.Body.String())
	}
}

// B15 #20: the merchant 购买会员 page exposes the current level, expiry and a
// renew flag from the existing membership/group tables.
func TestMerchantMembershipCurrentHeader(t *testing.T) {
	s, pool, _ := fixture(t)
	ctx := context.Background()
	s.Sessions = identity.NewPersistentSessions(pool, false)
	merchant, mcsrf, e := s.Sessions.Create(identity.User{Username: "merchant", Role: "merchant", ID: 1, AuthVersion: 1})
	if e != nil {
		t.Fatal(e)
	}
	var vip int64
	if e = pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"黄金会员","enabled":true,"sale_enabled":true,"price_cents":"100","duration_days":"30"}') RETURNING id`).Scan(&vip); e != nil {
		t.Fatal(e)
	}
	// No active membership yet: header carries no current level.
	w := lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/membership-sales", "", merchant, "merchant", mcsrf)
	if w.Code != 200 {
		t.Fatalf("catalog: %d %s", w.Code, w.Body.String())
	}
	current := lookupData(t, w)["current"].(map[string]any)
	if current["can_renew"] != false || current["endtime"] != "" {
		t.Fatalf("baseline merchant fabricated a renewal/expiry: %v", current)
	}

	// Give the merchant an active, non-permanent membership in the sale group.
	if _, e = pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(1,$1,now()+interval '10 day')`, vip); e != nil {
		t.Fatal(e)
	}
	w = lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/membership-sales", "", merchant, "merchant", mcsrf)
	if w.Code != 200 {
		t.Fatalf("catalog with membership: %d %s", w.Code, w.Body.String())
	}
	current = lookupData(t, w)["current"].(map[string]any)
	if current["my_group"] != "黄金会员" {
		t.Errorf("my_group=%v want 黄金会员", current["my_group"])
	}
	if current["my_gid"] != strconv.FormatInt(vip, 10) {
		t.Errorf("my_gid=%v want %d", current["my_gid"], vip)
	}
	if current["can_renew"] != true {
		t.Errorf("can_renew=%v want true", current["can_renew"])
	}
	if fmt.Sprint(current["endtime"]) == "" || current["endtime"] == "永久" {
		t.Errorf("endtime not a concrete expiry: %v", current["endtime"])
	}

	// Disabling the group's sale turns off 续期 while keeping the current level.
	if _, e = pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{sale_enabled}','false') WHERE id=$1`, vip); e != nil {
		t.Fatal(e)
	}
	w = lookupHTTP(t, s.Handler(), "GET", "/console/v1/merchant/membership-sales", "", merchant, "merchant", mcsrf)
	current = lookupData(t, w)["current"].(map[string]any)
	if current["my_group"] != "黄金会员" || current["can_renew"] != false {
		t.Fatalf("sale-disabled group still renewable or lost level: %v", current)
	}
}
