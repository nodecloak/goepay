package server

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/compat"
	"sync"
	"testing"
	"time"
)

func child168BRoll(t *testing.T, f channel168Fixture, roll int) {
	t.Helper()
	w := f.childHTTP("GET", "/policy?merchant_id=1", "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &out)
	b, _ := json.Marshal(map[string]any{"roll_type": roll, "expected_fingerprint": out.Data["fingerprint"]})
	w = f.childHTTP("PATCH", "/policy?merchant_id=1", string(b))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}
func child168BParams(no, money string) compat.Params {
	return compat.Params{"type": "alipay", "method": "native", "name": "Synthetic route", "money": money, "out_trade_no": no, "clientip": "192.0.2.1", "channel_id": "7"}
}
func TestSubchannel168BActualSelectionEveryModeUsesOnlyEligibleChildAndOriginalMoney(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	a := child168BCreate(t, f, "range-one", true)
	b := child168BCreate(t, f, "range-two", true)
	for i, row := range []map[string]any{a, b} {
		amount := fmt.Sprint((i + 1) * 100)
		body, _ := json.Marshal(map[string]any{"expected_fingerprint": row["fingerprint"], "min_amount": amount, "max_amount": amount, "weight": 99 - i*98})
		w := f.childHTTP("PATCH", "/"+row["id"].(string), string(body))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	ctx := context.Background()
	for roll := 0; roll <= 3; roll++ {
		child168BRoll(t, f, roll)
		for i, money := range []string{"1.00", "2.00"} {
			no := fmt.Sprintf("mode168b-%d-%d", roll, i)
			if _, _, e := f.s.providerRoutedRequest(ctx, 1, child168BParams(no, money)); e != nil {
				t.Fatal(roll, money, e)
			}
			var child string
			if e := f.p.QueryRow(ctx, `SELECT subchannel_id::text FROM payment_subchannel_routes WHERE out_trade_no=$1`, no).Scan(&child); e != nil || child != []map[string]any{a, b}[i]["id"] {
				t.Fatal("mode selected ineligible child", roll, child, e)
			}
		}
		for i, money := range []string{"0.99", "1.01", "1.99", "2.01"} {
			if _, _, e := f.s.providerRoutedRequest(ctx, 1, child168BParams(fmt.Sprintf("outside168b-%d-%d", roll, i), money)); e == nil {
				t.Fatal("outside child single amount was admitted", roll, money)
			}
		}
	}
	var routes, childSources, quotes, parentHeads int
	if e := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments),(SELECT count(*) FROM payment_subchannel_routes),(SELECT count(*) FROM provider_route_fee_quotes),(SELECT count(*) FROM provider_channel_last_used)`).Scan(&routes, &childSources, &quotes, &parentHeads); e != nil || routes != 8 || childSources != 8 || quotes != 8 || parentHeads != 0 {
		t.Fatal("child rotation leaked into parent -4 or incomplete sources", routes, childSources, quotes, parentHeads, e)
	}
}
func TestSubchannel168BSequentialGlobalChildTimeAndConcurrentOriginalIntent(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	a := child168BCreate(t, f, "sequence-one", true)
	b := child168BCreate(t, f, "sequence-two", true)
	child168BRoll(t, f, 1)
	ctx := context.Background()
	for i, want := range []any{a["id"], b["id"], a["id"]} {
		no := fmt.Sprintf("sequence168b-%d", i)
		if _, _, e := f.s.providerRoutedRequest(ctx, 1, child168BParams(no, "1.00")); e != nil {
			t.Fatal(e)
		}
		var got string
		if e := f.p.QueryRow(ctx, `SELECT subchannel_id::text FROM payment_subchannel_routes WHERE out_trade_no=$1`, no).Scan(&got); e != nil || got != want {
			t.Fatal("child global usetime/id order changed", i, got, want, e)
		}
		time.Sleep(3 * time.Millisecond)
	}
	var before int
	if e := f.p.QueryRow(ctx, `SELECT count(*) FROM payment_subchannel_routes`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, e := f.s.providerRoutedRequest(ctx, 1, child168BParams("concurrent168b-same", "1.00"))
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal("concurrent replay", e)
		}
	}
	var after, heads int
	if e := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM payment_subchannel_routes),(SELECT count(*) FROM payment_subchannel_last_used)`).Scan(&after, &heads); e != nil || after != before+1 || heads != 2 {
		t.Fatal("same intent advanced or assigned more than once", after, heads, e)
	}
	var snapshot string
	if e := f.p.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(h) ORDER BY subchannel_id)::text FROM payment_subchannel_last_used h`).Scan(&snapshot); e != nil {
		t.Fatal(e)
	}
	if _, _, e := f.s.providerRoutedRequest(ctx, 1, child168BParams("concurrent168b-same", "1.00")); e != nil {
		t.Fatal(e)
	}
	var replay string
	f.p.QueryRow(ctx, `SELECT jsonb_agg(to_jsonb(h) ORDER BY subchannel_id)::text FROM payment_subchannel_last_used h`).Scan(&replay)
	if snapshot != replay {
		t.Fatal("replay advanced child usetime")
	}
}
func TestSubchannel168BExplicitChildNeverAuthorizesWrongOwnerTypeOrApp(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	row := child168BCreate(t, f, "scoped-child", true)
	ctx := context.Background()
	for i, extra := range []compat.Params{{"subchannel_id": "9223372036854775807"}, {"type": "wxpay"}, {"sub_appid": "app168-seven"}, {"channel_id": "8"}} {
		p := child168BParams(fmt.Sprintf("badchild168b-%d", i), "1.00")
		for k, v := range extra {
			p[k] = v
		}
		if _, _, e := f.s.providerRoutedRequest(ctx, 1, p); e == nil {
			t.Fatal("wrong owner/type/app namespace admitted", extra)
		}
	}
	if _, e := f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"info":{"alipay":{"channel":"7","type":"channel"}}}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	p := child168BParams("closedgroup168b", "1.00")
	p["subchannel_id"] = row["id"].(string)
	if _, _, e := f.s.providerRoutedRequest(ctx, 1, p); e == nil {
		t.Fatal("explicit child bypassed actual effective group -2")
	}
}
func TestSubchannel168BValidOldLastUseSourceCannotRollChildRotationBack(t *testing.T) {
	f, _ := subchannel168BLocal(t)
	a := child168BCreate(t, f, "usage-old-one", true)
	child168BCreate(t, f, "usage-old-two", true)
	child168BRoll(t, f, 1)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, _, e := f.s.providerRoutedRequest(ctx, 1, child168BParams(fmt.Sprintf("usage168b-%d", i), "1.00")); e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			if _, e := f.p.Exec(ctx, `CREATE TABLE subchannel168b_old_usage AS SELECT * FROM payment_subchannel_last_used WHERE subchannel_id=$1`, a["id"]); e != nil {
				t.Fatal(e)
			}
		}
		time.Sleep(3 * time.Millisecond)
	}
	if _, e := f.p.Exec(ctx, `UPDATE payment_subchannel_last_used h SET (merchant_id,out_trade_no,used_millis)=(SELECT merchant_id,out_trade_no,used_millis FROM subchannel168b_old_usage p WHERE p.subchannel_id=h.subchannel_id) WHERE subchannel_id=$1`, a["id"]); e != nil {
		t.Fatal(e)
	}
	if _, _, e := f.s.providerRoutedRequest(ctx, 1, child168BParams("usage168b-after-rollback", "1.00")); e == nil {
		t.Fatal("valid older immutable source silently rolled back child usage head")
	}
}
