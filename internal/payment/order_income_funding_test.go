package payment

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func incomeFundingFixture(t *testing.T, legacy bool) *Store {
	t.Helper()
	dsn := os.Getenv("GOEPAY_INCOME_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("GOEPAY_TEST_DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("local income PG required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, dsn)
	if e != nil {
		t.Fatal(e)
	}
	nonce := make([]byte, 8)
	if _, e = rand.Read(nonce); e != nil {
		t.Fatal(e)
	}
	schema := "income63_" + hex.EncodeToString(nonce)
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA "+quoted); e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	p, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		p.Close()
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+quoted+" CASCADE")
		admin.Close()
	})
	if legacy {
		if _, e = p.Exec(ctx, "CREATE TABLE schema_migrations(name text PRIMARY KEY,sha256 text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())"); e != nil {
			t.Fatal(e)
		}
		files, e := filepath.Glob("../../db/migrations/*.up.sql")
		if e != nil {
			t.Fatal(e)
		}
		for _, file := range files {
			if strings.Compare(filepath.Base(file), "000063_") >= 0 {
				continue
			}
			raw, e := os.ReadFile(file)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = p.Exec(ctx, string(raw)); e != nil {
				t.Fatal(file, e)
			}
			if _, e = p.Exec(ctx, "INSERT INTO schema_migrations(name,sha256) VALUES($1,$2)", filepath.Base(file), fmt.Sprintf("%x", sha256.Sum256(raw))); e != nil {
				t.Fatal(e)
			}
		}
	} else if e = db.Migrate(ctx, p); e != nil {
		t.Fatal(e)
	}
	s, e := NewWithKey(p, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func incomeLegacyHold(t *testing.T, s *Store, mid int64, no string) (Order, OrderIncomeHold) {
	t.Helper()
	ctx := context.Background()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	// Historical setup must use the schema62 writer, not today's Store API.
	// Every original birth, immutable and balanced-ledger guard stays enabled.
	var latest string
	if e = tx.QueryRow(ctx, "SELECT max(name) FROM schema_migrations").Scan(&latest); e != nil || !strings.HasPrefix(latest, "000062_") {
		t.Fatal("historical fixture requires exactly schema62", latest, e)
	}
	o, e := scanOrder(tx.QueryRow(ctx, `INSERT INTO orders(trade_no,merchant_id,out_trade_no,name,type,amount,merchant_amount) VALUES(nextval('trade_no_seq')::text,$1,$2,'item','',100,100) RETURNING `+orderColumns, mid, no))
	if e != nil {
		t.Fatal(e)
	}
	terms := referralOrderTerms{OrderID: o.ID, MerchantID: mid, Purpose: "payment", Reason: "no_relation", RefundPolicy: referralRefundPolicy}
	proof, e := s.referralTermsProof(o, terms)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `INSERT INTO referral_order_terms(order_id,merchant_id,purpose,enabled,reason,rate_bps,platform_fee,reward_amount,refund_policy,policy_versions,proof_hash)VALUES($1,$2,'payment',false,'no_relation',0,0,0,$3,'[0,0,0,0,0]',$4)`, o.ID, mid, referralRefundPolicy, proof); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `INSERT INTO inbox(provider,event_id,order_id)VALUES('payment',$1,$2)`, "paid-"+no, o.ID); e != nil {
		t.Fatal(e)
	}
	if e = post(ctx, tx, "payment:"+o.TradeNo, "payment net credit", 0, "clearing", mid, "available", 100); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `UPDATE orders SET status='paid',paid_at=statement_timestamp(),provider_event_id=$2 WHERE id=$1`, o.ID, "paid-"+no); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `INSERT INTO outbox(event_key,kind,payload)VALUES($1,'payment.paid',jsonb_build_object('trade_no',$2::text,'merchant_id',$3::bigint))`, "payment:"+o.TradeNo, o.TradeNo, mid); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "INSERT INTO accounts(owner_id,kind) VALUES($1,'order_income_hold') ON CONFLICT DO NOTHING", mid); e != nil {
		t.Fatal(e)
	}
	h, e := scanIncomeHold(tx.QueryRow(ctx, "WITH ins AS(INSERT INTO order_income_holds(order_id,merchant_id,request_no,merchant_net,refund_merchant_amount,amount) VALUES($1,$2,$3,$4,0,$4) RETURNING *) SELECT "+incomeHoldColumns+" FROM ins h JOIN orders o ON o.id=h.order_id", o.ID, mid, "legacy-"+no, o.MerchantAmount))
	if e != nil {
		t.Fatal(e)
	}
	if e = post(ctx, tx, fmt.Sprintf("order-income:%d:freeze", h.ID), "genuine pre63 hold", mid, "available", mid, "order_income_hold", h.Amount); e != nil {
		t.Fatal(e)
	}
	if e = incomeHoldAudit(ctx, tx, h, "freeze", incomeTestActor); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	return o, h
}
func TestIncomeFundingFreshHeldRefundLifecycle(t *testing.T) {
	for _, state := range []string{"unknown", "failed", "succeeded"} {
		t.Run(state, func(t *testing.T) {
			s := incomeFundingFixture(t, false)
			ctx := context.Background()
			feeMerchant(t, s, 6301, 1000, "platform")
			o := incomePaidOrder(t, s, 6301, "lifecycle-"+state)
			h, e := s.FreezeOrderIncome(ctx, 6301, o.TradeNo, "original-hold", incomeTestActor)
			if e != nil || h.SourceMode != "fresh" || h.RemainingAmount != 90 {
				t.Fatal(h, e)
			}
			p, e := s.RefundFundingPreview(ctx, s.pool, o.ID, "refund-original", 98)
			if e != nil || p.HeldAmount != 90 || p.AvailableAmount != 0 || p.FeeAmount != 8 {
				t.Fatal(p, e)
			}
			r, e := s.ReserveRefund(ctx, 6301, o.OutTradeNo, "refund-original", 98)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.ResolveRefund(ctx, r.ID, state); e != nil {
				t.Fatal(e)
			}
			h, e = s.OrderIncomeHold(ctx, 6301, o.TradeNo)
			if e != nil {
				t.Fatal(e)
			}
			expected := int64(0)
			if state == "failed" {
				expected = 90
			}
			if h.RemainingAmount != expected {
				t.Fatal(h)
			}
			released, e := s.UnfreezeOrderIncome(ctx, 6301, o.TradeNo, h.ID, incomeTestActor)
			if state == "unknown" {
				if !errors.Is(e, ErrState) {
					t.Fatal("unknown released", released, e)
				}
			} else if e != nil || released.State != "released" {
				t.Fatal(released, e)
			}
			b, e := s.Balance(ctx, 6301)
			if e != nil || b.Available != expected {
				t.Fatal(b, e)
			}
		})
	}
}
func TestIncomeFundingLegacyAdoptionRequiresSuperAndMandatoryGuard(t *testing.T) {
	s := incomeFundingFixture(t, true)
	ctx := context.Background()
	if e := s.CreateMerchant(ctx, Merchant{ID: 6302, Name: "legacy", APIKey: "local-only"}); e != nil {
		t.Fatal(e)
	}
	o, h := incomeLegacyHold(t, s, 6302, "adopt")
	if e := db.Migrate(ctx, s.pool); e != nil {
		t.Fatal(e)
	}
	if _, e := s.ReserveRefund(ctx, 6302, o.OutTradeNo, "blocked", 10); !errors.Is(e, ErrState) {
		t.Fatal("unprepared legacy refunded", e)
	}
	super := IncomeHoldActor{ID: 1, Name: "local-super", Class: "super"}
	if _, e := s.AdoptOrderIncomeHold(ctx, 6302, o.TradeNo, h.ID, "adopt-one", super); !errors.Is(e, ErrInvalid) {
		t.Fatal("missing mandatory guard", e)
	}
	var calls int
	guarded := WithFundsActionGuard(ctx, func(_ context.Context, _ pgx.Tx, kind, key string) error {
		calls++
		if kind != "income-hold-adopt" || key != fmt.Sprint(h.ID) {
			return ErrInvalid
		}
		return nil
	})
	if _, e := s.AdoptOrderIncomeHold(guarded, 6302, o.TradeNo, h.ID, "adopt-one", incomeTestActor); !errors.Is(e, ErrInvalid) {
		t.Fatal("operator adopted", e)
	}
	v, e := s.AdoptOrderIncomeHold(guarded, 6302, o.TradeNo, h.ID, "adopt-one", super)
	if e != nil || v.SourceMode != "legacy_adopt" || v.Amount != 100 {
		t.Fatal(v, e)
	}
	if _, e = s.AdoptOrderIncomeHold(ctx, 6302, o.TradeNo, h.ID, "adopt-one", super); e != nil {
		t.Fatal("same immutable adoption replay failed", e)
	}
	if calls != 1 {
		t.Fatal("replay consumed grant", calls)
	}
	r, e := s.ReserveRefund(ctx, 6302, o.OutTradeNo, "adopt-refund", 40)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
		t.Fatal(e)
	}
	v, e = s.UnfreezeOrderIncome(ctx, 6302, o.TradeNo, h.ID, super)
	if e != nil || v.State != "released" {
		t.Fatal(v, e)
	}
	b, _ := s.Balance(ctx, 6302)
	if b.Available != 60 {
		t.Fatal("legacy original remainder", b)
	}
}
