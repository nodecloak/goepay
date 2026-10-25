package payment

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func miniTestBinding(t *testing.T, s *Store, o Order) MiniLaunchBinding {
	t.Helper()
	b := MiniLaunchBinding{TradeNo: o.TradeNo, MerchantID: o.MerchantID, ChannelID: 7, Provider: "alipay", AppID: "app", Method: "mini", Purpose: "mini-payment", Path: "pages/pay/index"}
	if e := s.PrepareMiniPayment(context.Background(), b); e != nil {
		t.Fatal(e)
	}
	return b
}
func TestMiniOAuthStateBindingOneUseAndGlobalWebCodeClaim(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	b := miniTestBinding(t, s, o)
	state, e := s.IssueMiniState(ctx, b)
	if e != nil {
		t.Fatal(e)
	}
	for _, field := range []string{"merchant", "channel", "app", "method", "purpose", "path", "provider", "trade"} {
		bad := b
		switch field {
		case "merchant":
			bad.MerchantID++
		case "channel":
			bad.ChannelID++
		case "app":
			bad.AppID = "other"
		case "method":
			bad.Method = "jsapi"
		case "purpose":
			bad.Purpose = "login"
		case "path":
			bad.Path = "pages/other"
		case "provider":
			bad.Provider = "wxpay"
		case "trade":
			bad.TradeNo = "other"
		}
		if e = s.ConsumeMiniState(ctx, state, "mini-code", bad); e == nil {
			t.Fatal("foreign mini binding accepted", field)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- s.ConsumeMiniState(ctx, state, "mini-code", b) }()
	}
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatal("consumed more than once", successes)
	}
	web := oauthTestBinding(t, s, o)
	cookie := strings.Repeat("b", 64)
	webstate, e := s.IssueOAuthState(ctx, web, cookie)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeOAuthState(ctx, webstate, cookie, "mini-code", web); e == nil {
		t.Fatal("mini code replay crossed to web")
	}
	webstate, e = s.IssueOAuthState(ctx, web, cookie)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeOAuthState(ctx, webstate, cookie, "web-code", web); e != nil {
		t.Fatal(e)
	}
	miniReplay, e := s.IssueMiniState(ctx, b)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeMiniState(ctx, miniReplay, "web-code", b); e == nil {
		t.Fatal("web code replay crossed into mini")
	}
	webstate, e = s.IssueOAuthState(ctx, web, cookie)
	if e != nil {
		t.Fatal(e)
	}
	miniRace, e := s.IssueMiniState(ctx, b)
	if e != nil {
		t.Fatal(e)
	}
	race := make(chan error, 2)
	go func() { race <- s.ConsumeOAuthState(ctx, webstate, cookie, "racing-shared-code", web) }()
	go func() { race <- s.ConsumeMiniState(ctx, miniRace, "racing-shared-code", b) }()
	owners := 0
	for i := 0; i < 2; i++ {
		if <-race == nil {
			owners++
		}
	}
	if owners != 1 {
		t.Fatal("web/mini race granted multiple exchanges", owners)
	}
	next, e := s.IssueMiniState(ctx, b)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "INSERT INTO oauth_code_claims(provider,app_id,code_hash,mini_state_hash) VALUES('alipay','app',$1,$2)", s.oauthHash("code", "foreign-state-code"), s.oauthHash("mini-state", strings.Repeat("a", 64))); e == nil {
		t.Fatal("code claim accepted absent mini state")
	}
	if _, e = s.pool.Exec(ctx, "INSERT INTO oauth_code_claims(provider,app_id,code_hash,mini_state_hash) VALUES('alipay','foreign-app',$1,$2)", s.oauthHash("code", "foreign-app-code"), s.oauthHash("mini-state", next)); e == nil {
		t.Fatal("mini claim attached to a state from another application")
	}
	if _, e = s.pool.Exec(ctx, "UPDATE oauth_code_claims SET mini_state_hash=$1 WHERE mini_state_hash=$2", s.oauthHash("mini-state", next), s.oauthHash("mini-state", state)); e == nil {
		t.Fatal("code claim rebound to another state")
	}
	if _, e = s.pool.Exec(ctx, "INSERT INTO oauth_code_claims(provider,app_id,code_hash,state_hash,mini_state_hash) VALUES('alipay','app',$1,$2,$3)", s.oauthHash("code", "two-purposes"), s.oauthHash("state", webstate), s.oauthHash("mini-state", next)); e == nil {
		t.Fatal("one code claim linked to two purposes")
	}
	var raw string
	if e = s.pool.QueryRow(ctx, "SELECT jsonb_agg(to_jsonb(c))::text FROM oauth_code_claims c").Scan(&raw); e != nil || strings.Contains(raw, "mini-code") {
		t.Fatal("plaintext code stored", e)
	}
}

func TestMiniOAuthExpiresWhileWaitingForOrderLock(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	b := miniTestBinding(t, s, o)
	original, e := s.IssueMiniState(ctx, b)
	if e != nil {
		t.Fatal(e)
	}
	soon := strings.Repeat("f", 64)
	if _, e = s.pool.Exec(ctx, `INSERT INTO mini_oauth_states(state_hash,order_id,merchant_id,channel_id,provider,app_id,method,purpose,path,created_at,expires_at) SELECT $1,order_id,merchant_id,channel_id,provider,app_id,method,purpose,path,clock_timestamp(),clock_timestamp()+interval '500 milliseconds' FROM mini_oauth_states WHERE state_hash=$2`, s.oauthHash("mini-state", soon), s.oauthHash("mini-state", original)); e != nil {
		t.Fatal(e)
	}
	blocker, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(ctx)
	var id, pid int64
	if e = blocker.QueryRow(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", o.ID).Scan(&id); e != nil {
		t.Fatal(e)
	}
	if e = blocker.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&pid); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() { result <- s.ConsumeMiniState(ctx, soon, "slow-mini-code", b) }()
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
		t.Fatal("mini callback never entered controlled lock wait")
	}
	time.Sleep(550 * time.Millisecond)
	if e = blocker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	if e = <-result; e == nil {
		t.Fatal("expired mini state redeemed after lock wait")
	}
}
func TestMiniOAuthRejectsAttemptIdentityChangeAndDisabledConsumedState(t *testing.T) {
	s := testStore(t)
	o := providerTestOrder(t, s)
	ctx := context.Background()
	b := miniTestBinding(t, s, o)
	state, e := s.IssueMiniState(ctx, b)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE merchants SET enabled=false WHERE id=$1", o.MerchantID); e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeMiniState(ctx, state, "disabled-code", b); e == nil {
		t.Fatal("disabled merchant accepted")
	}
	if _, e = s.pool.Exec(ctx, "UPDATE merchants SET enabled=true WHERE id=$1", o.MerchantID); e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeMiniState(ctx, state, "fresh-code", b); e == nil {
		t.Fatal("disabled state revived")
	}
	state, e = s.IssueMiniState(ctx, b)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.BeginProviderAttemptWithIdentity(ctx, o.TradeNo, 7, "mini", "app", "payer-original"); e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeMiniState(ctx, state, "another-payer-code", b); e == nil {
		t.Fatal("existing attempt switched identity")
	}
	if _, e = s.IssueMiniState(ctx, b); e == nil {
		t.Fatal("new state after provider attempt")
	}
}
