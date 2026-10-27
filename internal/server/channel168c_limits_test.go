package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	adapter "goepay/internal/adapters/payment"
)

func TestChannel168CGlobalLimitsPolicyOriginalFieldsAndIndependentCAS(t *testing.T) {
	f := channel168Local(t)
	feeBefore := channel168Data(t, f.admin("GET", "/7", "", f.csrf))
	before := channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))
	if before["configured"] != false || before["revision"] != "0" || before["daytop"] != "0.00" || before["daymaxorder"] != "0" || before["timezone"] != "UTC+08:00" {
		t.Fatal("unconfigured parent limits default", before)
	}
	body, _ := json.Marshal(map[string]any{"daytop": "100.00", "daymaxorder": "10", "paymin": "0.01", "paymax": "100.00", "timestart": "23", "timestop": "2", "rate_limit_seconds": "30", "rate_limit_count": "3", "expected_fingerprint": before["fingerprint"]})
	saved := channel168Data(t, f.admin("PATCH", "/7/limits", string(body), f.csrf))
	if saved["configured"] != true || saved["revision"] != "1" || saved["daytop"] != "100.00" || saved["paymin"] != "0.01" || saved["timestart"] != "23" || saved["timestop"] != "2" {
		t.Fatal("original global fields not persisted", saved)
	}
	if w := f.admin("PATCH", "/7/limits", string(body), f.csrf); w.Code != 409 {
		t.Fatal("stale global limits CAS did not conflict", w.Code)
	}
	feeAfter := channel168Data(t, f.admin("GET", "/7", "", f.csrf))
	if feeAfter["configured"] != false || feeAfter["fingerprint"] != feeBefore["fingerprint"] {
		t.Fatal("global limits mutated fee policy")
	}
}

func channel168CSetLimits(t *testing.T, f channel168Fixture, fields map[string]string) map[string]any {
	t.Helper()
	state := channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))
	body := map[string]any{"daytop": "0", "daymaxorder": "0", "paymin": "0", "paymax": "0", "timestart": "", "timestop": "", "rate_limit_seconds": "0", "rate_limit_count": "0", "expected_fingerprint": state["fingerprint"]}
	for k, v := range fields {
		body[k] = v
	}
	raw, _ := json.Marshal(body)
	return channel168Data(t, f.admin("PATCH", "/7/limits", string(raw), f.csrf))
}

type channel168CConfirmedProvider struct {
	*channel168Provider
	mu       sync.Mutex
	requests map[string]adapter.CreateRequest
	states   map[string]string
}

func (p *channel168CConfirmedProvider) Create(ctx context.Context, r adapter.CreateRequest) (adapter.CreateResult, error) {
	p.mu.Lock()
	p.requests[r.TradeNo] = r
	p.mu.Unlock()
	return p.channel168Provider.Create(ctx, r)
}
func (p *channel168CConfirmedProvider) Query(_ context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
	p.mu.Lock()
	request, ok := p.requests[r.TradeNo]
	state := p.states[r.TradeNo]
	p.mu.Unlock()
	if !ok {
		return adapter.QueryResult{}, adapter.ErrInvalid
	}
	if state == "" {
		state = "SUCCESS"
	}
	return adapter.QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: "paidc" + r.TradeNo, State: state, Currency: "CNY", Amount: request.Amount}, nil
}
func channel168CConfirmedLocal(t *testing.T) channel168Fixture {
	f := channel168Local(t)
	f.s.ProviderFlow.Providers[7] = &channel168CConfirmedProvider{channel168Provider: &channel168Provider{app: "channel168c-confirmed"}, requests: map[string]adapter.CreateRequest{}}
	return f
}

func TestChannel168CParentFrequencySharedAcrossEffectiveGroupsAndDirectRoutes(t *testing.T) {
	f := channel168Local(t)
	ctx := context.Background()
	channel168CSetLimits(t, f, map[string]string{"rate_limit_seconds": "60", "rate_limit_count": "1"})
	if out := f.pay(t, "global-start-first", "1.00", 7); out["code"] != float64(1) {
		t.Fatal(out)
	}
	var gid int64
	if e := f.p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"7","type":"channel"}}}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := f.p.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=1`, gid); e != nil {
		t.Fatal(e)
	}
	if out := f.pay(t, "global-start-other-group", "1.00", 7); out["code"] == float64(1) {
		t.Fatal("same parent frequency reset by effective group change")
	}
	if out := f.pay(t, "global-start-first", "1.00", 7); out["code"] != float64(1) {
		t.Fatal("original intent consumed a second global start", out)
	}
}

func TestChannel168CSuccessfulDayThresholdNeverReservesInflightGross(t *testing.T) {
	for _, field := range []string{"daytop", "daymaxorder"} {
		t.Run(field, func(t *testing.T) {
			f := channel168CConfirmedLocal(t)
			ctx := context.Background()
			limit := "100.00"
			if field == "daymaxorder" {
				limit = "1"
			}
			channel168CSetLimits(t, f, map[string]string{field: limit})
			for _, tc := range []struct{ no, money string }{{"pending-large-c", "150.00"}, {"pending-small-c", "50.00"}} {
				if out := f.pay(t, tc.no, tc.money, 7); out["code"] != float64(1) {
					t.Fatal("success daily threshold was used as inflight reservation", tc.no, out)
				}
			}
			large, e := f.s.Store.Order(ctx, 1, "pending-large-c")
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.s.ProviderFlow.Query(ctx, 1, large.TradeNo); e != nil {
				t.Fatal("credible synthetic success", e)
			}
			if out := f.pay(t, "after-success-threshold-c", "1.00", 7); out["code"] == float64(1) {
				t.Fatal("successful global daily threshold did not pause new routing")
			}
			small, e := f.s.Store.Order(ctx, 1, "pending-small-c")
			if e != nil {
				t.Fatal(e)
			}
			for i := 0; i < 2; i++ {
				if _, e = f.s.ProviderFlow.Query(ctx, 1, small.TradeNo); e != nil {
					t.Fatal("already sent payment refused or duplicated after threshold", e)
				}
			}
			state := channel168Data(t, f.admin("GET", "/7/limits", "", f.csrf))
			usage, ok := state["usage"].(map[string]any)
			if !ok || usage["successful_amount"] != "200.00" || usage["successful_count"] != "2" {
				t.Fatal("daily success did not count confirmed actual gross exactly once", state)
			}
			balance, e := f.s.Store.Balance(ctx, 1)
			if e != nil || balance.Available != 20000 {
				t.Fatal("real paid ledger rejected or duplicated known success", balance, e)
			}
		})
	}
}

func TestChannel168COriginalSinglePaymentAndWholeHourBoundariesHTTP(t *testing.T) {
	f := channel168Local(t)
	channel168CSetLimits(t, f, map[string]string{"paymin": "10.00", "paymax": "100.00"})
	for i, tc := range []struct {
		money   string
		allowed bool
	}{{"9.99", false}, {"10.00", true}, {"100.00", true}, {"100.01", false}} {
		out := f.pay(t, fmt.Sprintf("global-amount-%d", i), tc.money, 7)
		if (out["code"] == float64(1)) != tc.allowed {
			t.Errorf("parent actual gross boundary %s allowed=%v", tc.money, out)
		}
	}
	now := time.Now().In(time.FixedZone("UTC+8", 8*3600)).Hour()
	closedStart, closedStop := (now+3)%24, (now+4)%24
	channel168CSetLimits(t, f, map[string]string{"timestart": fmt.Sprint(closedStart), "timestop": fmt.Sprint(closedStop)})
	if out := f.pay(t, "closed-parent-hour-c", "1.00", 7); out["code"] == float64(1) {
		t.Error("parent closed hours ignored")
	}
	for i, hour := range []int{0, 13, (now + 7) % 24} {
		channel168CSetLimits(t, f, map[string]string{"timestart": fmt.Sprint(hour), "timestop": fmt.Sprint(hour)})
		if out := f.pay(t, fmt.Sprintf("equal-parent-hour-%d", i), "1.00", 7); out["code"] != float64(1) {
			t.Fatal("equal hour range must be original all-day unrestricted", hour, out)
		}
	}
}
