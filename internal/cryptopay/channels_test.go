package cryptopay

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5"
)

func channelTestService(t *testing.T) *Service {
	t.Helper()
	s := testService(t)
	_, e := s.Pool.Exec(t.Context(), `ALTER TABLE merchants ADD COLUMN base_group_id bigint NOT NULL DEFAULT 0, ADD COLUMN group_baseline_unresolved boolean NOT NULL DEFAULT false, ADD COLUMN pay integer NOT NULL DEFAULT 1;
	CREATE TABLE memberships(merchant_id bigint PRIMARY KEY,group_id bigint,expires_at timestamptz);
	CREATE TABLE backoffice_records(id bigint,resource text,body jsonb,PRIMARY KEY(resource,id));
	INSERT INTO backoffice_records VALUES(0,'groups','{"enabled":true,"info":{"usdt":{"channel":"-5","type":"channel"}}}'),(1,'pay-types','{"type":"usdt","enabled":true}');`)
	if e != nil {
		t.Fatal(e)
	}
	s.RequireChannels = true
	return s
}

func channelAuthority(ctx context.Context) context.Context {
	return WithAuthority(ctx, func(context.Context, pgx.Tx) error { return nil })
}

func saveTestChannel(t *testing.T, s *Service, name, network string) Channel {
	t.Helper()
	digest := sha256.Sum256([]byte(name + ":" + network))
	c, e := s.SaveChannel(channelAuthority(t.Context()), SaveChannelInput{ActorID: 1, RequestID: fmt.Sprintf("%x", digest[:16]), Name: name, Network: network, Enabled: true, FeeBPS: 100, MinAmount: "0", MaxAmount: "0", DayLimit: "0"})
	if e != nil {
		t.Fatal(e)
	}
	return c
}

func TestCryptoChannelsGroupAssignmentFreezesOriginalRoute(t *testing.T) {
	s := channelTestService(t)
	c := saveTestChannel(t, s, "TRON collection", "tron")
	if e := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); e != nil {
		t.Fatal(e)
	}
	base := CreateInvoiceInput{MerchantID: 1, OutTradeNo: "route-freeze", Amount: "10", ChannelID: c.ID}
	v, e := s.CreateInvoice(t.Context(), base)
	if e != nil || v.ChannelID != c.ID || v.Network != "tron" || v.ChannelRevision != 1 || v.Fee != "0.1" || v.FeeBPS != 100 || v.FeeSource != "channel" {
		t.Fatalf("channel route %+v: %v", v, e)
	}
	_, e = s.SaveChannel(channelAuthority(t.Context()), SaveChannelInput{ID: c.ID, ExpectedRevision: 1, ActorID: 1, Name: c.Name, Network: c.Network, Enabled: false, FeeBPS: 500, MinAmount: "0", MaxAmount: "0", DayLimit: "0"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Pool.Exec(t.Context(), `UPDATE backoffice_records SET body='{"enabled":true,"info":{"usdt":{"channel":"0","type":"channel","rate":"80"}}}' WHERE resource='groups'`); e != nil {
		t.Fatal(e)
	}
	replayed, e := s.CreateInvoice(t.Context(), base)
	if e != nil || replayed.ID != v.ID || replayed.Fee != v.Fee || replayed.ChannelRevision != 1 {
		t.Fatalf("original changed %+v: %v", replayed, e)
	}
	base.ChannelID = c.ID + 1
	if _, e = s.CreateInvoice(t.Context(), base); !errors.Is(e, ErrConflict) {
		t.Fatalf("conflicting channel accepted: %v", e)
	}
	base.ChannelID = c.ID
	base.Network = "ethereum"
	if _, e = s.CreateInvoice(t.Context(), base); !errors.Is(e, ErrConflict) {
		t.Fatalf("conflicting chain accepted: %v", e)
	}
	var cny int64
	if e = s.Pool.QueryRow(t.Context(), "SELECT balance FROM accounts WHERE owner_id=1").Scan(&cny); e != nil || cny != 12345 {
		t.Fatalf("CNY touched: %d %v", cny, e)
	}
}

func TestCryptoChannelsAdmissionRejectsBeforeAddressAllocation(t *testing.T) {
	for _, tc := range []struct {
		name, sql, network      string
		merchant, channelOffset int64
	}{
		{name: "merchant-group-closed", merchant: 2, sql: `INSERT INTO backoffice_records VALUES(2,'groups','{"enabled":true,"info":{"usdt":{"channel":"0","type":"channel"}}}'); UPDATE merchants SET base_group_id=2 WHERE id=2`},
		{name: "missing-channel", merchant: 1, channelOffset: 999},
		{name: "network-mismatch", merchant: 1, network: "ethereum"},
		{name: "pay-type-closed", merchant: 1, sql: `UPDATE backoffice_records SET body='{"type":"usdt","enabled":false}' WHERE resource='pay-types'`},
		{name: "group-disabled", merchant: 1, sql: `UPDATE backoffice_records SET body='{"enabled":true,"info":{"usdt":{"channel":"0","type":"channel"}}}' WHERE resource='groups'`},
		{name: "unsupported-roll", merchant: 1, sql: `UPDATE backoffice_records SET body='{"enabled":true,"info":{"usdt":{"channel":"-3","type":"roll"}}}' WHERE resource='groups'`},
		{name: "merchant-closed", merchant: 1, sql: `UPDATE merchants SET pay=0 WHERE id=1`},
		{name: "channel-closed", merchant: 1, sql: `UPDATE crypto_channels SET enabled=false`},
		{name: "too-small", merchant: 1, sql: `UPDATE crypto_channels SET min_amount=11000000`},
		{name: "too-large", merchant: 1, sql: `UPDATE crypto_channels SET max_amount=9000000`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := channelTestService(t)
			c := saveTestChannel(t, s, "TRON collection", "tron")
			if e := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); e != nil {
				t.Fatal(e)
			}
			if tc.sql != "" {
				if _, e := s.Pool.Exec(t.Context(), tc.sql); e != nil {
					t.Fatal(e)
				}
			}
			if _, e := s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: tc.merchant, ChannelID: c.ID + tc.channelOffset, Network: tc.network, OutTradeNo: "rejected", Amount: "10"}); e == nil {
				t.Fatal("unauthorized creation succeeded")
			}
			var used, invoices int
			if e := s.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM crypto_addresses WHERE used),(SELECT count(*) FROM crypto_invoices)`).Scan(&used, &invoices); e != nil || used != 0 || invoices != 0 {
				t.Fatalf("allocated rejected intent %d %d %v", used, invoices, e)
			}
		})
	}
}

func TestCryptoChannelsGroupRateAndSelection(t *testing.T) {
	s := channelTestService(t)
	c := saveTestChannel(t, s, "TRON collection", "tron")
	if _, e := s.Pool.Exec(t.Context(), `UPDATE backoffice_records SET body=jsonb_build_object('enabled',true,'info',jsonb_build_object('usdt',jsonb_build_object('channel',$1::text,'type','channel','rate','98.50'))) WHERE resource='groups'`, fmt.Sprint(c.ID)); e != nil {
		t.Fatal(e)
	}
	if e := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); e != nil {
		t.Fatal(e)
	}
	v, e := s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 1, OutTradeNo: "group-fee", Amount: "10"})
	if e != nil || v.Fee != "0.15" || v.FeeBPS != 150 || v.FeeSource != "group" || v.ChannelID != c.ID {
		t.Fatalf("group rate %+v %v", v, e)
	}
	if _, e = s.Pool.Exec(t.Context(), `UPDATE crypto_invoices SET channel_revision=2 WHERE id=$1`, v.ID); e == nil {
		t.Fatal("historical route mutable")
	}
	if _, e = s.Pool.Exec(t.Context(), `UPDATE crypto_invoices SET state='expired' WHERE id=$1`, v.ID); e != nil {
		t.Fatal("state progression blocked", e)
	}
	available, e := s.AvailableChannels(t.Context(), 1, "")
	if e != nil || len(available) != 1 {
		t.Fatalf("available %+v %v", available, e)
	}
	s.Networks = map[string]NetworkConfig{"tron": {Enabled: false}}
	available, e = s.AvailableChannels(t.Context(), 1, "")
	if e != nil || len(available) != 0 {
		t.Fatalf("disabled mainnet advertised %+v %v", available, e)
	}
}

func TestCryptoChannelsSaveIsRevisionCheckedAndCreateIdempotent(t *testing.T) {
	s := channelTestService(t)
	in := SaveChannelInput{ActorID: 1, RequestID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Name: "TRON", Network: "tron", FeeBPS: 100, MinAmount: "0", MaxAmount: "0", DayLimit: "0", ReservedIDs: []int64{100}}
	a, e := s.SaveChannel(channelAuthority(t.Context()), in)
	if e != nil || a.ID <= 100 || a.Enabled {
		t.Fatalf("create %+v %v", a, e)
	}
	b, e := s.SaveChannel(channelAuthority(t.Context()), in)
	if e != nil || a.ID != b.ID {
		t.Fatalf("duplicate %+v %v", b, e)
	}
	in.Name = "different"
	if _, e = s.SaveChannel(channelAuthority(t.Context()), in); !errors.Is(e, ErrConflict) {
		t.Fatal("request mutated", e)
	}
	in.ID = a.ID
	in.ExpectedRevision = 1
	in.RequestID = ""
	in.Name = a.Name
	in.Enabled = true
	b, e = s.SaveChannel(channelAuthority(t.Context()), in)
	if e != nil || b.Revision != 2 || !b.Enabled {
		t.Fatalf("update %+v %v", b, e)
	}
	if _, e = s.SaveChannel(channelAuthority(t.Context()), in); !errors.Is(e, ErrConflict) {
		t.Fatal("stale update", e)
	}
	in.ExpectedRevision = 2
	in.Network = "ethereum"
	if _, e = s.SaveChannel(channelAuthority(t.Context()), in); !errors.Is(e, ErrConflict) {
		t.Fatal("chain changed", e)
	}
}

func TestCryptoChannelsDailyLimitSerializesConcurrentMerchants(t *testing.T) {
	s := channelTestService(t)
	c := saveTestChannel(t, s, "TRON collection", "tron")
	if _, e := s.Pool.Exec(t.Context(), `UPDATE crypto_channels SET day_limit=10000000`); e != nil {
		t.Fatal(e)
	}
	for _, a := range []string{depositOne, depositTwo} {
		if e := s.AddAddress(t.Context(), "tron", a, "deposit"); e != nil {
			t.Fatal(e)
		}
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, mid := range []int64{1, 2} {
		wg.Add(1)
		go func(mid int64) {
			defer wg.Done()
			_, e := s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: mid, ChannelID: c.ID, OutTradeNo: "concurrent-limit", Amount: "10"})
			results <- e
		}(mid)
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("quota admissions=%d", success)
	}
}

func TestCryptoChannelAuthorityCompositionPreservesOriginalGuard(t *testing.T) {
	order := []string{}
	ctx := WithAuthority(t.Context(), func(context.Context, pgx.Tx) error { order = append(order, "session"); return nil })
	ctx = WithAuthority(ctx, func(context.Context, pgx.Tx) error { order = append(order, "policy"); return nil })
	if e := authorize(ctx, nil); e != nil || fmt.Sprint(order) != "[session policy]" {
		t.Fatalf("composed proof %v %v", order, e)
	}
	order = nil
	ctx = WithAuthority(t.Context(), func(context.Context, pgx.Tx) error { order = append(order, "session"); return ErrConflict })
	ctx = WithAuthority(ctx, func(context.Context, pgx.Tx) error { order = append(order, "policy"); return nil })
	if e := authorize(ctx, nil); !errors.Is(e, ErrConflict) || fmt.Sprint(order) != "[session]" {
		t.Fatalf("failed session continued %v %v", order, e)
	}
}

func TestCryptoChannelsRejectIncompleteDatabaseRoute(t *testing.T) {
	s := channelTestService(t)
	c := saveTestChannel(t, s, "TRON collection", "tron")
	if e := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); e != nil {
		t.Fatal(e)
	}
	_, e := s.Pool.Exec(t.Context(), `INSERT INTO crypto_invoices(id,merchant_id,out_trade_no,network,address,amount,fee,token_hash,token_cipher,expires_at,channel_id,channel_revision,fee_bps,fee_source,channel_snapshot) VALUES('bad-snapshot',1,'bad-snapshot','tron',$1,1000000,0,''::bytea,''::bytea,clock_timestamp()+interval '1 hour',$2,1,NULL,'channel','{"channel_id":"1"}')`, depositOne, c.ID)
	if e == nil {
		t.Fatal("NULL fee accepted as valid bound channel snapshot")
	}
}

func TestCryptoChannelInvoiceFilterPrecedesPaginationAndPreservesTenant(t *testing.T) {
	s := channelTestService(t)
	c := saveTestChannel(t, s, "TRON selected", "tron")
	if e := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); e != nil {
		t.Fatal(e)
	}
	v, e := s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 1, ChannelID: c.ID, OutTradeNo: "original-channel", Amount: "1"})
	if e != nil {
		t.Fatal(e)
	}
	other := saveTestChannel(t, s, "TRON alternate", "tron")
	_, e = s.Pool.Exec(t.Context(), `INSERT INTO crypto_addresses(network,address,role,used) SELECT 'tron','synthetic-list-address-'||i,'deposit',true FROM generate_series(1,201) i;
	INSERT INTO crypto_invoices(id,merchant_id,out_trade_no,network,address,amount,fee,token_hash,token_cipher,expires_at,channel_id,channel_revision,fee_bps,fee_source,channel_snapshot) SELECT 'synthetic-list-invoice-'||i,1,'synthetic-list-order-'||i,'tron','synthetic-list-address-'||i,1000000,0,''::bytea,''::bytea,clock_timestamp()+interval '1 hour',`+fmt.Sprint(other.ID)+`,1,0,'channel','{"synthetic":true}' FROM generate_series(1,201) i`)
	if e != nil {
		t.Fatal(e)
	}
	all, e := s.ListInvoices(t.Context(), 1)
	if e != nil || len(all) != 200 {
		t.Fatalf("unfiltered %d %v", len(all), e)
	}
	for _, row := range all {
		if row.ID == v.ID {
			t.Fatal("fixture target unexpectedly in first page")
		}
	}
	filtered, e := s.ListChannelInvoices(t.Context(), 1, c.ID)
	if e != nil || len(filtered) != 1 || filtered[0].ID != v.ID {
		t.Fatalf("filtered %+v %v", filtered, e)
	}
	filtered, e = s.ListChannelInvoices(t.Context(), 2, c.ID)
	if e != nil || len(filtered) != 0 {
		t.Fatalf("cross merchant leak %+v %v", filtered, e)
	}
}

func TestCryptoChannelMembershipDeadlineAfterFinalAuthorityWait(t *testing.T) {
	s := channelTestService(t)
	c := saveTestChannel(t, s, "TRON deadline", "tron")
	if e := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); e != nil {
		t.Fatal(e)
	}
	_, e := s.Pool.Exec(t.Context(), `INSERT INTO backoffice_records VALUES(1,'groups','{"enabled":true,"info":{"usdt":{"rate":"98"}}}'); INSERT INTO memberships VALUES(1,1,clock_timestamp()+interval '2 seconds')`)
	if e != nil {
		t.Fatal(e)
	}
	checks := 0
	ctx := WithAuthority(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		checks++
		if checks == 3 {
			_, e := tx.Exec(ctx, `SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM(expires_at-clock_timestamp())))+0.05) FROM memberships WHERE merchant_id=1`)
			return e
		}
		return nil
	})
	_, e = s.CreateInvoice(ctx, CreateInvoiceInput{MerchantID: 1, ChannelID: c.ID, OutTradeNo: "deadline-after-authority", Amount: "1"})
	if !errors.Is(e, ErrConflict) {
		t.Fatalf("expired membership survived final authority wait: checks=%d err=%v", checks, e)
	}
	var count, used int
	if e = s.Pool.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM crypto_invoices),(SELECT count(*) FROM crypto_addresses WHERE used)`).Scan(&count, &used); e != nil || count != 0 || used != 0 {
		t.Fatalf("expired authorization allocated invoice/address: %d %d %v", count, used, e)
	}
}
