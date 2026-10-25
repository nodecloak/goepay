package payment

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestIdentity64MiniPaymentCannotRedeemStandaloneWebAlipayCode(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	token := strings.Repeat("i", 43)
	lookupTestSession(t, s, token, "realm64", 964)
	q, e := s.CreateIdentityLookup(ctx, token, "merchant", IdentityLookupBinding{MerchantID: 964, ChannelID: 7, Provider: "alipay", AppID: "app", Scope: "auth_base", AccountScope: "local-account"})
	if e != nil {
		t.Fatal(e)
	}
	browser := strings.Repeat("d", 64)
	_, state, e := s.StartIdentityLookup(ctx, q.AuthorizationToken, browser)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.ConsumeIdentityLookup(ctx, q.ID, "alipay", 7, state, browser, "same-real-exchange-code"); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 964, OutTradeNo: "mini-realm", Name: "local", Type: "alipay", Amount: 1})
	if e != nil {
		t.Fatal(e)
	}
	b := miniTestBinding(t, s, o)
	st, e := s.IssueMiniState(ctx, b)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ConsumeMiniState(ctx, st, "same-real-exchange-code", b); e == nil {
		t.Fatal("Alipay mini consumed code already claimed by independent web lookup")
	}
}

func TestIdentityProfile64CodeRealmAcrossPurposeAndHistoricalClaims(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	claim := func(wallet, app, domain, code, marker string) bool {
		tx, e := s.pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		ok, e := s.claimOAuthAuthorizationCode(ctx, tx, wallet, app, domain, code, s.oauthHash("claim", marker))
		if e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		return ok
	}
	if !claim("wxpay", "wx-app", "web", "same-text", "wxweb") || !claim("wxpay", "wx-app", "mini", "same-text", "wxmini") {
		t.Fatal("distinct WeChat exchange realms collapsed")
	}
	if !claim("alipay", "ali-app", "mini", "same-text", "alimini") || claim("alipay", "ali-app", "web", "same-text", "aliweb") {
		t.Fatal("Alipay SDK/web did not share real exchange realm")
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO oauth_authorization_code_claims(provider,app_id,domain,code_hash,claim_hash)VALUES('alipay','old-app','mini',$1,$2)`, s.oauthHash("code", "historical"), s.oauthHash("claim", "old")); e != nil {
		t.Fatal(e)
	}
	if claim("alipay", "old-app", "web", "historical", "new") {
		t.Fatal("historical mini global claim bypassed")
	}
	token := strings.Repeat("r", 43)
	lookupTestSession(t, s, token, "profile-realm64", 971)
	b := profileBinding64(971, "mini")
	q1, e := s.CreateProfileIdentityLookup(ctx, token, "merchant", b)
	if e != nil {
		t.Fatal(e)
	}
	q2, e := s.CreateProfileIdentityLookup(ctx, token, "merchant", b)
	if e != nil {
		t.Fatal(e)
	}
	var wins atomic.Int32
	var wg sync.WaitGroup
	for _, q := range []ProfileIdentityLookup{q1, q2} {
		q := q
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.ConsumeProfileIdentityLookup(ctx, q.ID, q.State, "", "concurrent-real-code", b); e == nil {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("cross-request code claims", wins.Load())
	}
}
