package payment

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func oauthTestBinding(t *testing.T, s *Store, o Order) OAuthBinding {
	t.Helper()
	b := OAuthBinding{TradeNo: o.TradeNo, MerchantID: o.MerchantID, ChannelID: 7, Provider: "alipay", AppID: "app", Method: "jsapi", Purpose: "payment-jsapi"}
	if e := s.PrepareOAuthPayment(context.Background(), b); e != nil {
		t.Fatal(e)
	}
	return b
}
func TestOAuthStateBindsOrderTenantChannelAppBrowserPurposeAndConsumesOnce(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	b := oauthTestBinding(t, s, o)
	ctx := context.Background()
	cookie := strings.Repeat("b", 64)
	state, e := s.IssueOAuthState(ctx, b, cookie)
	if e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{"trade", "merchant", "channel", "app", "method", "purpose", "provider"} {
		altered := b
		switch field {
		case "trade":
			altered.TradeNo = "different-order"
		case "merchant":
			altered.MerchantID++
		case "channel":
			altered.ChannelID++
		case "app":
			altered.AppID = "different-app"
		case "method":
			altered.Method = "mini"
		case "purpose":
			altered.Purpose = "login"
		case "provider":
			altered.Provider = "wxpay"
		}
		if e = s.ConsumeOAuthState(ctx, state, cookie, "authorization-code", altered); e == nil {
			t.Fatal("wrong binding accepted", field)
		}
	}
	if e = s.ConsumeOAuthState(ctx, state, strings.Repeat("x", 64), "authorization-code", b); e == nil {
		t.Fatal("foreign browser accepted")
	}
	if e = s.ConsumeOAuthState(ctx, state, cookie, "authorization-code", b); e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeOAuthState(ctx, state, cookie, "different-code", b); e == nil {
		t.Fatal("state replay accepted")
	}
	next, e := s.IssueOAuthState(ctx, b, cookie)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeOAuthState(ctx, next, cookie, "authorization-code", b); e == nil {
		t.Fatal("same code exchanged under new state")
	}
	if e = s.CreateMerchant(ctx, Merchant{ID: 71, Name: "other OAuth tenant", APIKey: "test-only-secret"}); e != nil {
		t.Fatal(e)
	}
	other, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 71, OutTradeNo: "other-oauth-tenant", Type: "alipay", Name: "item", Amount: 125})
	if e != nil {
		t.Fatal(e)
	}
	otherBinding := oauthTestBinding(t, s, other)
	otherState, e := s.IssueOAuthState(ctx, otherBinding, cookie)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeOAuthState(ctx, otherState, cookie, "authorization-code", otherBinding); e == nil {
		t.Fatal("code replay crossed OAuth tenants")
	}
	var raw string
	if e = s.pool.QueryRow(ctx, "SELECT COALESCE(jsonb_agg(to_jsonb(c)),'[]'::jsonb)::text FROM oauth_code_claims c").Scan(&raw); e != nil || strings.Contains(raw, "authorization-code") {
		t.Fatal("plaintext code persisted", raw, e)
	}
}

func TestOAuthStateExpiresWhileCallbackWaitsForOrderLock(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	b := oauthTestBinding(t, s, o)
	ctx := context.Background()
	browser := strings.Repeat("d", 64)
	original, e := s.IssueOAuthState(ctx, b, browser)
	if e != nil {
		t.Fatal(e)
	}
	soon := strings.Repeat("f", 64)
	if _, e = s.pool.Exec(ctx, `INSERT INTO oauth_states(state_hash,order_id,merchant_id,channel_id,provider,app_id,method,purpose,browser_hash,created_at,expires_at) SELECT $1,order_id,merchant_id,channel_id,provider,app_id,method,purpose,browser_hash,clock_timestamp(),clock_timestamp()+interval '500 milliseconds' FROM oauth_states WHERE state_hash=$2`, s.oauthHash("state", soon), s.oauthHash("state", original)); e != nil {
		t.Fatal(e)
	}
	blocker, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(ctx)
	var orderID, pid int64
	if e = blocker.QueryRow(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", o.ID).Scan(&orderID); e != nil {
		t.Fatal(e)
	}
	if e = blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() { result <- s.ConsumeOAuthState(ctx, soon, browser, "slow-code", b) }()
	waiting := false
	until := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(until) {
		if e = s.pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity a WHERE $1::int=ANY(pg_blocking_pids(a.pid)))", pid).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !waiting {
		t.Fatal("callback did not enter the controlled order-lock wait")
	}
	time.Sleep(550 * time.Millisecond)
	if e = blocker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-result; e == nil {
		t.Fatal("callback used transaction-start time to redeem an expired state after waiting")
	}
}
func TestOAuthStateExpirationConcurrentConsumptionAndExistingPaymentAttempt(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	b := oauthTestBinding(t, s, o)
	ctx := context.Background()
	cookie := strings.Repeat("c", 64)
	state, e := s.IssueOAuthState(ctx, b, cookie)
	if e != nil {
		t.Fatal(e)
	}
	expired := strings.Repeat("e", 64)
	if _, e = s.pool.Exec(ctx, `INSERT INTO oauth_states(state_hash,order_id,merchant_id,channel_id,provider,app_id,method,purpose,browser_hash,created_at,expires_at) SELECT $1,order_id,merchant_id,channel_id,provider,app_id,method,purpose,browser_hash,now()-interval '6 minutes',now()-interval '1 minute' FROM oauth_states WHERE state_hash=$2`, s.oauthHash("state", expired), s.oauthHash("state", state)); e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeOAuthState(ctx, expired, cookie, "expired-code", b); e == nil {
		t.Fatal("expired state accepted")
	}
	var wg sync.WaitGroup
	success := make(chan bool, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); success <- s.ConsumeOAuthState(ctx, state, cookie, "one-code", b) == nil }()
	}
	wg.Wait()
	close(success)
	owners := 0
	for ok := range success {
		if ok {
			owners++
		}
	}
	if owners != 1 {
		t.Fatal("multiple code exchanges granted", owners)
	}
	revoked, e := s.IssueOAuthState(ctx, b, cookie)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=$1", b.MerchantID); e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeOAuthState(ctx, revoked, cookie, "revoked-code", b); e == nil {
		t.Fatal("disabled merchant redeemed authorization")
	}
	if _, e = s.pool.Exec(ctx, "UPDATE merchants SET enabled=true WHERE id=$1", b.MerchantID); e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeOAuthState(ctx, revoked, cookie, "revoked-code", b); e == nil {
		t.Fatal("reenabled merchant revived consumed authorization")
	}
	if _, _, e = s.BeginProviderAttemptWithIdentity(ctx, o.TradeNo, 7, "jsapi", "app", "payer-a"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.IssueOAuthState(ctx, b, cookie); e == nil {
		t.Fatal("new authorization allowed replacing existing payment attempt payer")
	}
}
