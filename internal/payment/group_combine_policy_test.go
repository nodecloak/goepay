package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	adapter "goepay/internal/adapters/payment"
)

type groupCombineProvider struct {
	flowProvider
	combined atomic.Int32
	shares   []adapter.CombineSub
}

func TestGroupCombinePolicyRechecksDeadlineAndFirstGlobalInsertAfterOrderWait(t *testing.T) {
	original := testStore(t)
	clocked := settlementClockStore(t, original, "2042-06-01T10:00:00Z")
	config := clocked.pool.Config()
	config.ConnConfig.RuntimeParams["application_name"] = "group-combine-order-wait"
	pool, e := pgxpool.NewWithConfig(t.Context(), config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	s, e := NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	for index, name := range []string{"membership", "first-global"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			if _, e := original.pool.Exec(ctx, `UPDATE settlement_test_clock SET moment='2042-06-01T10:00:00Z'`); e != nil {
				t.Fatal(e)
			}
			groupCombineSettings(t, s, `{"enabled":true,"wxcombine_open":"1","wxcombine_submoney":"50"}`, map[string]string{"wxcombine_open": "1", "wxcombine_minmoney": "100"})
			mid := int64(920320 + index)
			if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "waiting combine", APIKey: "synthetic-waiting-combine"}); e != nil {
				t.Fatal(e)
			}
			if name == "membership" {
				var gid int64
				if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"waiting combine VIP","enabled":true,"wxcombine_open":"1","wxcombine_submoney":"50"}') RETURNING id`).Scan(&gid); e != nil {
					t.Fatal(e)
				}
				if _, e := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES($1,$2,'2042-06-01T10:00:01Z')`, mid, gid); e != nil {
					t.Fatal(e)
				}
			}
			o := groupCombineOrder(t, s, mid, "waiting-combine-"+name)
			blocker, e := original.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer blocker.Rollback(ctx)
			if _, e = blocker.Exec(ctx, `SELECT id FROM orders WHERE id=$1 FOR UPDATE`, o.ID); e != nil {
				t.Fatal(e)
			}
			p := &groupCombineProvider{}
			flow := ProviderFlow{Store: s}
			result := make(chan error, 1)
			go func() {
				_, err := flow.createMaybeCombined207(ctx, p, o, adapter.CreateRequest{TradeNo: o.TradeNo, Amount: 20000, Method: "native"})
				result <- err
			}()
			depositPolicyWaitForLock(t, ctx, s, "group-combine-order-wait")
			if name == "membership" {
				_, e = original.pool.Exec(ctx, `UPDATE settlement_test_clock SET moment='2042-06-01T10:00:02Z'`)
			} else {
				// Presence changes even though the group override keeps the
				// same effective cap. A pending policy snapshot is still stale.
				_, e = original.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"wxcombine_submoney","value":"50"}')`)
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = blocker.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-result:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if e == nil || p.calls.Load() != 0 || p.combined.Load() != 0 {
				t.Errorf("changed waiting combine policy reached upstream: err=%v ordinary=%d combined=%d", e, p.calls.Load(), p.combined.Load())
			}
			var subs int
			if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM combine_suborders207 WHERE order_id=$1`, o.ID).Scan(&subs); err != nil || subs != 0 {
				t.Errorf("stale policy retained split: count=%d err=%v", subs, err)
			}
		})
	}
}

func (p *groupCombineProvider) CombineCreate(_ context.Context, _ adapter.CreateRequest, shares []adapter.CombineSub) (adapter.CreateResult, error) {
	p.combined.Add(1)
	p.shares = append([]adapter.CombineSub(nil), shares...)
	if p.unknown {
		return adapter.CreateResult{}, &adapter.UnknownOutcome{Operation: "synthetic combine", Cause: errors.New("synthetic timeout")}
	}
	return adapter.CreateResult{MaterialType: "qrcode", Material: "weixin://synthetic-group-combine"}, nil
}

func groupCombineSettings(t *testing.T, s *Store, group string, global map[string]string) {
	t.Helper()
	if _, e := s.pool.Exec(t.Context(), `UPDATE backoffice_records SET body=$1::jsonb WHERE resource='groups' AND id=0`, group); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(t.Context(), `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key' IN ('wxcombine_open','wxcombine_minmoney','wxcombine_submoney')`); e != nil {
		t.Fatal(e)
	}
	for key, value := range global {
		if _, e := s.pool.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text))`, key, value); e != nil {
			t.Fatal(e)
		}
	}
}

func groupCombineOrder(t *testing.T, s *Store, mid int64, no string) Order {
	t.Helper()
	o, e := s.CreateOrder(t.Context(), CreateOrderInput{MerchantID: mid, OutTradeNo: no, Type: "wxpay", Name: "synthetic combine", Amount: 20000})
	if e != nil {
		t.Fatal(e)
	}
	return o
}

func TestGroupCombinePolicyControlsRealProviderCreateAndRejectsBadGroup(t *testing.T) {
	s := testStore(t)
	const mid int64 = 920310
	if e := s.CreateMerchant(t.Context(), Merchant{ID: mid, Name: "group combine", APIKey: "synthetic-combine"}); e != nil {
		t.Fatal(e)
	}
	for index, tc := range []struct {
		name, body, globalOpen string
		count                  int
		bad                    bool
	}{
		{"explicit-off", `{"enabled":true,"wxcombine_open":"0"}`, "1", 0, false},
		{"group-minimum", `{"enabled":true,"wxcombine_open":"1","wxcombine_minmoney":"300.00"}`, "1", 0, false},
		{"group-enable-and-cap", `{"enabled":true,"wxcombine_open":"1","wxcombine_minmoney":"100.00","wxcombine_submoney":"10.00"}`, "0", 20, false},
		{"inherit", `{"enabled":true,"wxcombine_open":"_","wxcombine_minmoney":"","wxcombine_submoney":"_"}`, "1", 4, false},
		{"explicit-zero-minimum", `{"enabled":true,"wxcombine_minmoney":"0"}`, "1", 0, false},
		{"bad-group", `{"enabled":true,"wxcombine_open":true}`, "1", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			groupCombineSettings(t, s, tc.body, map[string]string{"wxcombine_open": tc.globalOpen, "wxcombine_minmoney": "100", "wxcombine_submoney": "50"})
			o := groupCombineOrder(t, s, mid, fmt.Sprint("group-combine-", index))
			p := &groupCombineProvider{}
			flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, LocalSimulation: true, CallbackBaseURL: "https://synthetic.invalid"}
			_, e := flow.Submit(t.Context(), o.TradeNo, 7, "native", "192.0.2.207", "")
			if tc.bad {
				if e == nil || p.calls.Load() != 0 || p.combined.Load() != 0 {
					t.Errorf("bad group reached upstream: err=%v ordinary=%d combined=%d", e, p.calls.Load(), p.combined.Load())
				}
				plain := &flowProvider{}
				flow.Providers[7] = plain
				plainOrder := groupCombineOrder(t, s, mid, "unsupported-combine-provider")
				if _, e := flow.Submit(t.Context(), plainOrder.TradeNo, 7, "native", "192.0.2.207", ""); e != nil || plain.calls.Load() != 1 {
					t.Errorf("non-combining provider read irrelevant group policy: %v calls=%d", e, plain.calls.Load())
				}
				return
			}
			if e != nil || len(p.shares) != tc.count || (tc.count == 0 && p.calls.Load() != 1) || (tc.count > 0 && (p.calls.Load() != 0 || p.combined.Load() != 1)) {
				t.Errorf("effective combine policy not consumed: err=%v ordinary=%d combined=%d shares=%d want=%d", e, p.calls.Load(), p.combined.Load(), len(p.shares), tc.count)
			}
			var sum int64
			for _, sub := range p.shares {
				sum += sub.Amount
			}
			if tc.count > 0 && sum != 20000 {
				t.Errorf("split changed charged total: %d", sum)
			}
		})
	}
}

func TestGroupCombinePolicyUnknownAttemptRetainsOriginalSplitWithoutRetry(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	const mid int64 = 920312
	if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "unknown combine", APIKey: "synthetic-unknown-combine"}); e != nil {
		t.Fatal(e)
	}
	groupCombineSettings(t, s, `{"enabled":true,"wxcombine_open":"1","wxcombine_minmoney":"100","wxcombine_submoney":"50"}`, nil)
	o := groupCombineOrder(t, s, mid, "unknown-group-combine")
	p := &groupCombineProvider{flowProvider: flowProvider{unknown: true}}
	flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, LocalSimulation: true, CallbackBaseURL: "https://synthetic.invalid"}
	if _, e := flow.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.207", ""); e == nil {
		t.Fatal("unknown combined create was acknowledged as successful")
	}
	if _, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"wxcombine_open":false}'::jsonb WHERE resource='groups' AND id=0`); e != nil {
		t.Fatal(e)
	}
	replay, e := flow.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.207", "")
	subs, readErr := s.CombineSubOrders(ctx, o.ID)
	if e != nil || replay.State != "unknown" || readErr != nil || len(subs) != 4 || p.combined.Load() != 1 || p.calls.Load() != 0 {
		t.Fatalf("unknown original combine was retried or re-split: %+v err=%v read=%v subs=%d calls=%d/%d", replay, e, readErr, len(subs), p.combined.Load(), p.calls.Load())
	}
}

func TestGroupCombinePolicyMembershipExpiryKeepsOriginalAttemptAndSplit(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	const mid int64 = 920311
	if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "member combine", APIKey: "synthetic-member-combine"}); e != nil {
		t.Fatal(e)
	}
	groupCombineSettings(t, s, `{"enabled":true,"wxcombine_open":"0"}`, nil)
	var gid int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"combine VIP","enabled":true,"wxcombine_open":"1","wxcombine_minmoney":"100","wxcombine_submoney":"50"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES($1,$2,clock_timestamp()+interval '1 hour')`, mid, gid); e != nil {
		t.Fatal(e)
	}
	o := groupCombineOrder(t, s, mid, "member-combine-original")
	p := &groupCombineProvider{}
	flow := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, LocalSimulation: true, CallbackBaseURL: "https://synthetic.invalid"}
	first, e := flow.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.207", "")
	if e != nil || p.combined.Load() != 1 || len(p.shares) != 4 {
		t.Errorf("active VIP did not control combined create: %+v %v shares=%d", first, e, len(p.shares))
	}
	subs, e := s.CombineSubOrders(ctx, o.ID)
	if e != nil {
		t.Fatal(e)
	}
	original, _ := json.Marshal(subs)
	if _, e = s.pool.Exec(ctx, `UPDATE memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE merchant_id=$1`, mid); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"wxcombine_minmoney":true}'::jsonb WHERE resource='groups' AND id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	if replay, e := flow.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.207", ""); e != nil || replay.ID != first.ID || p.combined.Load() != 1 || p.calls.Load() != 0 {
		t.Errorf("current group changed original attempt replay: %+v %v calls=%d/%d", replay, e, p.combined.Load(), p.calls.Load())
	}
	after, _ := s.CombineSubOrders(ctx, o.ID)
	raw, _ := json.Marshal(after)
	if string(raw) != string(original) {
		t.Error("current policy rewrote original split")
	}
	newOrder := groupCombineOrder(t, s, mid, "member-combine-expired-new")
	if _, e := flow.Submit(ctx, newOrder.TradeNo, 7, "native", "192.0.2.207", ""); e != nil || p.calls.Load() != 1 || p.combined.Load() != 1 {
		t.Errorf("expired membership did not return to base group: %v calls=%d/%d", e, p.combined.Load(), p.calls.Load())
	}
}
