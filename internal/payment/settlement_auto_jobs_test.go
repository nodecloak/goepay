package payment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"goepay/internal/platform/jobs"
)

func TestSettlementAuto73QueueBindsOriginalAuthorityAndDoesNotRearmClaim(t *testing.T) {
	s, g, provider, _, a, id := autoReady73(t)
	ctx := context.Background()
	if _, e := (ProviderFlow{}).QueueSettlementAuto(ctx, 20); !errors.Is(e, ErrInvalid) {
		t.Fatal("unconfigured queue read database", e)
	}
	g.AllowLive = false
	if _, e := g.QueueSettlementAuto(ctx, 20); !errors.Is(e, ErrLiveDisabled) {
		t.Fatal("disabled writes enqueued automatic payout", e)
	}
	g.AllowLive = true
	for i := 0; i < 2; i++ {
		n, e := g.QueueSettlementAuto(ctx, 20)
		if e != nil || n != int64(1-i) {
			t.Fatal("duplicate automatic job", n, e)
		}
	}
	q := jobs.Queue{Pool: s.pool}
	j, e := q.Claim(ctx, []string{SettlementAutoJobKind}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	var payload struct {
		RequestID       int64 `json:"request_id,string"`
		AuthorizationID int64 `json:"authorization_id,string"`
	}
	if e = json.Unmarshal(j.Payload, &payload); e != nil || payload.RequestID != id || payload.AuthorizationID != a.ID {
		t.Fatal("job did not bind original authorization/request", e)
	}
	sealed, e := s.readSettlementAuto(ctx, s.pool, a.ID)
	if e != nil {
		t.Fatal(e)
	}
	var day string
	if e = s.pool.QueryRow(ctx, "SELECT (clock_timestamp() AT TIME ZONE 'Asia/Shanghai')::date::text").Scan(&day); e != nil {
		t.Fatal(e)
	}
	if e = g.settlementAutoCurrentWindow(ctx, s.pool, sealed, day); e != nil {
		t.Fatal("original current business day denied", e)
	}
	if e = g.settlementAutoCurrentWindow(ctx, s.pool, sealed, "2000-01-01"); !errors.Is(e, ErrState) {
		t.Fatal("old pending run crossed original daily payout boundary", e)
	}
	if _, e = g.ExecuteSettlementAutoAuthorized(ctx, id, a.ID+1, nil); !errors.Is(e, ErrConflict) || provider.transfers.Load() != 0 {
		t.Fatal("stale/foreign job adopted current authority", e)
	}
	if _, e = g.ExecuteSettlementAutoAuthorized(ctx, id, a.ID, nil); e == nil || provider.transfers.Load() != 1 {
		t.Fatal("local unknown first send", e)
	}
	if _, e = g.ExecuteSettlementAutoAuthorized(ctx, id, a.ID, nil); e != nil || provider.transfers.Load() != 1 {
		t.Fatal("retried original job resent", e)
	}
	if n, e := g.QueueSettlementAuto(ctx, 20); e != nil || n != 0 {
		t.Fatal("claimed original request enqueued again", n, e)
	}
	if e = q.Complete(ctx, j); e != nil {
		t.Fatal(e)
	}
}
