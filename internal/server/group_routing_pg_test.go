package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/hex"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
	"os"
	"testing"
	"time"
)

func groupRoutingFixture152(t *testing.T) (*Server, *pgxpool.Pool) {
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	nonce := make([]byte, 8)
	rand.Read(nonce)
	schema := "routes_" + hex.EncodeToString(nonce)
	if _, e = pool.Exec(ctx, "CREATE SCHEMA "+schema); e != nil {
		t.Fatal(e)
	}
	config, _ := pgxpool.ParseConfig(dsn)
	config.ConnConfig.RuntimeParams["search_path"] = schema
	scoped, e := pgxpool.NewWithConfig(ctx, config)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { scoped.Close(); pool.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE") })
	if e = db.Migrate(ctx, scoped); e != nil {
		t.Fatal(e)
	}
	store, e := payment.NewWithKey(scoped, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	if e = store.CreateMerchant(ctx, payment.Merchant{ID: 1, Name: "merchant", APIKey: "test-secret"}); e != nil {
		t.Fatal(e)
	}
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "app", PrivateKey: key, PublicKey: &key.PublicKey, Methods: []string{"native"}})
	if e != nil {
		t.Fatal(e)
	}
	s := &Server{Pool: scoped, Store: store, CheckoutKey: bytes.Repeat([]byte{1}, 32), MerchantChannels: map[int64][]int64{1: {7, 8}}, ProviderFlow: &payment.ProviderFlow{Store: store, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid", Providers: map[int64]adapter.Provider{7: ali, 8: ali}}}
	return s, scoped
}

func TestOriginalGroupRoutingCannotCommitExpiredMembershipAfterChannelQuotaLockWait(t *testing.T) {
	s, pool := groupRoutingFixture152(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var base, vip int64
	for i, id := range []*int64{&base, &vip} {
		channel := "7"
		if i == 1 {
			channel = "8"
		}
		if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups',jsonb_build_object('enabled',true,'info',jsonb_build_object('alipay',jsonb_build_object('channel',$1::text,'type','channel')))) RETURNING id`, channel).Scan(id); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=1;`, base); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(1,$1,clock_timestamp()+interval '2 seconds')`, vip); e != nil {
		t.Fatal(e)
	}
	block, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer block.Rollback(context.Background())
	if _, e = block.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('goepay/route/channel/8',0))`); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() {
		_, _, e := s.providerRoutedRequest(ctx, 1, compat.Params{"type": "alipay", "method": "native", "name": "item", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": "expired-after-quota"})
		done <- e
	}()
	for {
		var waiting bool
		if e = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE 'SELECT pg_advisory_xact_lock%')`).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case early := <-done:
			t.Fatal("route never waited on channel quota", early)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, e = pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))::double precision+0.05) FROM memberships WHERE merchant_id=1`); e != nil {
		t.Fatal(e)
	}
	if e = block.Rollback(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if e == nil {
			t.Fatal("expired membership committed a new route after quota wait")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	var routes, attempts int
	if e = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_route_assignments WHERE out_trade_no='expired-after-quota'),(SELECT count(*) FROM provider_attempts)`).Scan(&routes, &attempts); e != nil || routes != 0 || attempts != 0 {
		t.Fatal("expired member allocated payment intent", routes, attempts, e)
	}
}

func TestOriginalGroupRoutingChoosesActualConfiguredChannelAndPreservesOriginalReplay(t *testing.T) {
	s, pool := groupRoutingFixture152(t)
	ctx := context.Background()
	var gid int64
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"enabled":true,"info":{"alipay":{"channel":"8","type":"channel"}}}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=1`, gid); e != nil {
		t.Fatal(e)
	}
	params := func(no string) compat.Params {
		return compat.Params{"type": "alipay", "method": "native", "name": "item", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": no}
	}
	p := params("group-first")
	if got, _, e := s.providerRoutedRequest(ctx, 1, p); e != nil || got != 8 {
		t.Fatal("group channel ignored", got, e)
	}
	if _, e := pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info,alipay,channel}','"7"') WHERE id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	if got, _, e := s.providerRoutedRequest(ctx, 1, p); e != nil || got != 8 {
		t.Fatal("old durable route changed", got, e)
	}
	p = params("conflicting-explicit")
	p["channel_id"] = "8"
	if _, _, e := s.providerRoutedRequest(ctx, 1, p); e == nil {
		t.Fatal("explicit channel bypassed group")
	}
	var count int
	if e := pool.QueryRow(ctx, `SELECT count(*) FROM provider_route_assignments WHERE out_trade_no='conflicting-explicit'`).Scan(&count); e != nil || count != 0 {
		t.Fatal("rejected route persisted", count, e)
	}
	s.MerchantChannels[1] = []int64{8}
	if _, _, e := s.providerRoutedRequest(ctx, 1, params("unauthorized")); e == nil {
		t.Fatal("group expanded merchant allowlist")
	}
	s.MerchantChannels[1] = []int64{7, 8}
	if _, e := pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false') WHERE id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	if _, _, e := s.providerRoutedRequest(ctx, 1, params("disabled-new")); e == nil {
		t.Fatal("disabled group assigned new route")
	}
	if got, _, e := s.providerRoutedRequest(ctx, 1, params("group-first")); e != nil || got != 8 {
		t.Fatal("disabled group broke original route", got, e)
	}
}

func TestOriginalGroupRoutingRollAndFirstUseExistingQuotaAndIndependentPermissions(t *testing.T) {
	s, pool := groupRoutingFixture152(t)
	ctx := context.Background()
	var gid, roll int64
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('pay-rolls','{"name":"group-roll","merchant_id":"0","type":"alipay","strategy":"first","channel_ids":"8:1","enabled":true,"limits":"{\"daily_count\":1}"}') RETURNING id`).Scan(&roll); e != nil {
		t.Fatal(e)
	}
	if e := pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups',jsonb_build_object('enabled',true,'info',jsonb_build_object('alipay',jsonb_build_object('channel',$1::text,'type','roll')))) RETURNING id`, fmt.Sprint(roll)).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := pool.Exec(ctx, `UPDATE merchants SET base_group_id=$1 WHERE id=1`, gid); e != nil {
		t.Fatal(e)
	}
	params := func(no string) compat.Params {
		return compat.Params{"type": "alipay", "method": "native", "name": "item", "money": "1.00", "clientip": "192.0.2.1", "out_trade_no": no}
	}
	if got, _, e := s.providerRoutedRequest(ctx, 1, params("roll-one")); e != nil || got != 8 {
		t.Fatal("group roll ignored", got, e)
	}
	if _, _, e := s.providerRoutedRequest(ctx, 1, params("roll-exhausted")); e == nil {
		t.Fatal("group roll quota bypassed")
	}
	if _, e := pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{info,alipay}','{"channel":"-5","type":"channel"}') WHERE id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	s.MerchantChannels[1] = []int64{8, 7}
	if got, _, e := s.providerRoutedRequest(ctx, 1, params("first-configured")); e != nil || got != 7 {
		t.Fatal("first did not select lowest eligible ID independently of exhausted global roll", got, e)
	}
	p := params("first-explicit-bypass")
	p["channel_id"] = "8"
	if _, _, e := s.providerRoutedRequest(ctx, 1, p); e == nil {
		t.Fatal("explicit request bypassed first policy")
	}
}
