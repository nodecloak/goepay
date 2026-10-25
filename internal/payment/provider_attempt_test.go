package payment

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func providerTestOrder(t *testing.T, s *Store) Order {
	t.Helper()
	ctx := context.Background()
	if err := s.CreateMerchant(ctx, Merchant{ID: 70, Name: "provider-attempt-test", APIKey: "secret-123"}); err != nil {
		t.Fatal(err)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 70, OutTradeNo: "provider", Name: "goods", Amount: 125, Type: "alipay"})
	if e != nil {
		t.Fatal(e)
	}
	return o
}
func TestProviderAttemptConcurrentOwnerAndConfiguration(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	var wg sync.WaitGroup
	var mu sync.Mutex
	owners := 0
	ids := make(map[int64]bool)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, owner, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "page")
			if e != nil {
				t.Errorf("begin: %v", e)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			if owner {
				owners++
			}
			ids[a.ID] = true
		}()
	}
	wg.Wait()
	if owners != 1 || len(ids) != 1 {
		t.Fatalf("owners=%d distinct attempts=%d", owners, len(ids))
	}
	got, e := s.OrderByTradeNo(ctx, o.TradeNo)
	if e != nil || got.Status != "submitting" {
		t.Fatalf("order state %+v %v", got, e)
	}
	for _, in := range []struct {
		channel int64
		method  string
	}{{8, "page"}, {7, "wap"}} {
		if _, _, e = s.BeginProviderAttempt(ctx, o.TradeNo, in.channel, in.method); !errors.Is(e, ErrConflict) {
			t.Errorf("config conflict %v", e)
		}
	}
	b, e := s.Balance(ctx, 70)
	if e != nil || b.Available != 0 || b.Reserved != 0 {
		t.Fatalf("attempt credited money %+v %v", b, e)
	}
}
func TestProviderAttemptUnknownCannotRetryAndResultStable(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	a, owner, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native")
	if e != nil || !owner {
		t.Fatalf("begin %v", e)
	}
	if e = s.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", ""); e != nil {
		t.Fatal(e)
	}
	same, owner, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native")
	if e != nil || owner || same.ID != a.ID || same.State != "unknown" {
		t.Fatalf("unsafe retry %+v %v %v", same, owner, e)
	}
	if e = s.RecordProviderAttemptResult(ctx, a.ID, "unknown", "", "", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordProviderAttemptResult(ctx, a.ID, "pending", "qrcode", "new", ""); !errors.Is(e, ErrConflict) {
		t.Fatalf("unknown replaced %v", e)
	}
	got, e := s.ProviderAttempt(ctx, o.TradeNo)
	if e != nil || got.State != "unknown" {
		t.Fatalf("query %+v %v", got, e)
	}
	order, e := s.OrderByTradeNo(ctx, o.TradeNo)
	if e != nil || order.Status != "unknown" {
		t.Fatalf("order %+v %v", order, e)
	}
}
func TestProviderAttemptMaterialIdempotencyAndPaidRace(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	a, _, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "page")
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "verified-provider-event"); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordProviderAttemptResult(ctx, a.ID, "pending", "html", "<form>signed public material</form>", "ali1"); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordProviderAttemptResult(ctx, a.ID, "pending", "html", "<form>signed public material</form>", "ali1"); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordProviderAttemptResult(ctx, a.ID, "pending", "html", "changed", "ali1"); !errors.Is(e, ErrConflict) {
		t.Fatalf("material overwritten %v", e)
	}
	again, owner, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "page")
	if e != nil || owner || again.Material != "<form>signed public material</form>" {
		t.Fatalf("duplicate material %+v %v", again, e)
	}
	order, e := s.OrderByTradeNo(ctx, o.TradeNo)
	if e != nil || order.Status != "paid" {
		t.Fatalf("paid overwritten %+v %v", order, e)
	}
	b, e := s.Balance(ctx, 70)
	if e != nil || b.Available != 125 {
		t.Fatalf("paid balance %+v %v", b, e)
	}
}
func TestProviderAttemptRejectsInvalidAndClosed(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	for _, in := range []struct {
		channel int64
		method  string
	}{{0, "page"}, {-1, "page"}, {1, ""}, {1, "bad method"}} {
		if _, _, e := s.BeginProviderAttempt(ctx, o.TradeNo, in.channel, in.method); !errors.Is(e, ErrInvalid) {
			t.Errorf("invalid accepted %v", e)
		}
	}
	if e := s.CloseOrder(ctx, 70, o.OutTradeNo); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "page"); !errors.Is(e, ErrState) {
		t.Fatalf("closed submitted %v", e)
	}
}
func TestProviderAttemptFailureDoesNotGrantNewOwner(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	a, owner, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "page")
	if e != nil || !owner {
		t.Fatalf("begin %v", e)
	}
	if e = s.RecordProviderAttemptResult(ctx, a.ID, "failed", "", "", ""); e != nil {
		t.Fatal(e)
	}
	again, owner, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "page")
	if e != nil || owner || again.ID != a.ID || again.State != "failed" {
		t.Fatalf("failed retried %+v %v %v", again, owner, e)
	}
	if e = s.RecordProviderAttemptResult(ctx, a.ID, "paid", "", "", ""); !errors.Is(e, ErrInvalid) {
		t.Fatalf("money state accepted %v", e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE provider_attempts SET state='processing' WHERE id=$1", a.ID); e == nil {
		t.Fatal("failed attempt reset directly")
	}
	if _, e = s.pool.Exec(ctx, "UPDATE provider_attempts SET channel_id=8 WHERE id=$1", a.ID); e == nil {
		t.Fatal("channel rerouted directly")
	}
	if _, e = s.pool.Exec(ctx, "DELETE FROM provider_attempts WHERE id=$1", a.ID); e == nil {
		t.Fatal("attempt deletion allowed retry")
	}
}
