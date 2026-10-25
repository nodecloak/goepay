package payment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"sync"
	"sync/atomic"
	"testing"
)

type profitProvider71 struct {
	adapter.Provider
	estimate adapter.PaymentCostEstimate
}

func TestReferralProfit71ConcurrentBirthSingleSourceAndClaim(t *testing.T) {
	s, _, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	yes, bps := true, int64(5000)
	policy := referralTestPolicy(&yes, &bps)
	policy.Basis = "profit"
	if _, e := s.SetReferralPolicy(ctx, "global", 0, 1, policy, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	quotes := []*PaymentCostQuote{profitQuote71(t, "0"), profitQuote71(t, "5")}
	ids := make(chan int64, 6)
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "profit71-race", Name: "synthetic", Type: "wxpay", Amount: 100, PaymentCost: quotes[i%2]})
			if e != nil {
				t.Error(e)
				return
			}
			ids <- o.ID
		}(i)
	}
	wg.Wait()
	close(ids)
	var id int64
	for got := range ids {
		if id != 0 && got != id {
			t.Fatal("multiple original orders", id, got)
		}
		id = got
	}
	if id == 0 {
		t.Fatal("no birth")
	}
	o, e := s.Order(ctx, mid, "profit71-race")
	if e != nil {
		t.Fatal(e)
	}
	var owners atomic.Int64
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, owner, e := s.BeginProviderAttemptWithAccount(ctx, o.TradeNo, 7, "native", "profit71-app", "", quotes[0].estimate.AccountScope)
			if e != nil {
				t.Error(e)
			}
			if owner {
				owners.Add(1)
			}
		}()
	}
	wg.Wait()
	if owners.Load() != 1 {
		t.Fatal("multiple send owners", owners.Load())
	}
	var costs, events int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM referral_profit_costs),(SELECT count(*) FROM referral_profit_cost_events)`).Scan(&costs, &events); e != nil || costs != 1 || events != 1 {
		t.Fatal(costs, events, e)
	}
	dailyLedgerClean70(t, s)
}

func TestReferralProfit71SQLNullBoundsAndBirthAuditRollback(t *testing.T) {
	s, o, _ := profitOrder71(t, "immediate")
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `ALTER TABLE referral_profit_costs DISABLE TRIGGER referral_profit_cost_immutable`); e != nil {
		t.Fatal(e)
	}
	for _, column := range []string{"merchant_id", "source_version", "channel_id", "method", "provider", "app_id", "account_hash", "config_revision", "rate_units", "amount", "platform_fee", "cost_whole", "cost_remainder", "profit_amount", "reward_amount", "terms_hash", "proof_hash"} {
		if _, e := s.pool.Exec(ctx, "UPDATE referral_profit_costs SET "+column+"=NULL WHERE order_id=$1", o.ID); e == nil {
			t.Fatal("SQL NULL accepted", column)
		}
	}
	for _, assignment := range []string{"rate_units=-1", "rate_units=100000001", "cost_whole=-1", "cost_remainder=100000000", "profit_amount=11", "reward_amount=11", "account_hash=decode('00','hex')"} {
		if _, e := s.pool.Exec(ctx, "UPDATE referral_profit_costs SET "+assignment+" WHERE order_id=$1", o.ID); e == nil {
			t.Fatal("SQL invalid cost accepted", assignment)
		}
	}
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION profit71_fail_audit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'synthetic audit fault';END$$;CREATE TRIGGER profit71_audit_fault BEFORE INSERT ON referral_profit_cost_events FOR EACH ROW EXECUTE FUNCTION profit71_fail_audit()`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: o.MerchantID, OutTradeNo: "profit71-fault", Name: "synthetic", Type: "wxpay", Amount: 100, PaymentCost: profitQuote71(t, "5")}); e == nil {
		t.Fatal("birth without audit committed")
	}
	var n int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM orders WHERE out_trade_no='profit71-fault'").Scan(&n); e != nil || n != 0 {
		t.Fatal("audit failure left order", n, e)
	}
	dailyLedgerClean70(t, s)
}

func (p profitProvider71) PaymentCostEstimate() (adapter.PaymentCostEstimate, bool) {
	return p.estimate, true
}
func (p profitProvider71) ID() string                      { return "wxpay" }
func (p profitProvider71) ApplicationID() string           { return "profit71-app" }
func (p profitProvider71) TransferAccountIdentity() string { return "profit71-mch" }
func profitQuote71(t *testing.T, rate string) *PaymentCostQuote {
	t.Helper()
	p := profitProvider71{}
	e, err := adapter.NewPaymentCostEstimate(p, rate)
	if err != nil {
		t.Fatal(err)
	}
	p.estimate = e
	q, err := (ProviderFlow{Providers: map[int64]adapter.Provider{7: p}}).PaymentCostQuote(7, "native")
	if err != nil || q == nil {
		t.Fatal(err)
	}
	return q
}

func TestReferralProfit71ExplicitPolicyRequiresKnownBirthCost(t *testing.T) {
	s, _, mid, _ := referralRewardFixture(t)
	old := referralCreateTestOrder(t, s, mid, 100, "profit71-old-fee")
	var before []byte
	if e := s.pool.QueryRow(context.Background(), "SELECT proof_hash FROM referral_order_terms WHERE order_id=$1", old.ID).Scan(&before); e != nil {
		t.Fatal(e)
	}
	yes, rate := true, int64(5000)
	p := referralTestPolicy(&yes, &rate)
	p.Basis = "profit"
	if _, err := s.SetReferralPolicy(context.Background(), "global", 0, 1, p, referralPolicyAdmin); err != nil {
		t.Fatal("explicit profit policy rejected", err)
	}
	if _, err := s.CreateOrder(context.Background(), CreateOrderInput{MerchantID: mid, OutTradeNo: "profit71-missing", Type: "wxpay", Name: "synthetic", Amount: 100}); err == nil {
		t.Fatal("missing estimate was invented as zero")
	}
	referralPayTestOrder(t, s, old)
	var after []byte
	var reward int64
	if e := s.pool.QueryRow(context.Background(), "SELECT proof_hash,reward_amount FROM referral_order_terms WHERE order_id=$1", old.ID).Scan(&after, &reward); e != nil || !bytes.Equal(before, after) || reward != 5 {
		t.Fatal("profit policy changed historical fee entitlement", reward, e)
	}
}

func TestReferralProfit71BirthImmutableCostAndZeroStillMandatory(t *testing.T) {
	for _, rate := range []string{"1", "20"} {
		t.Run(rate, func(t *testing.T) {
			s, _, mid, _ := referralRewardFixture(t)
			ctx := context.Background()
			yes, bps := true, int64(5000)
			p := referralTestPolicy(&yes, &bps)
			p.Basis = "profit"
			if _, e := s.SetReferralPolicy(ctx, "global", 0, 1, p, referralPolicyAdmin); e != nil {
				t.Fatal(e)
			}
			q := profitQuote71(t, rate)
			o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "profit71-cost", Type: "wxpay", Name: "synthetic", Amount: 100, PaymentCost: q})
			if e != nil {
				t.Fatal(e)
			}
			tx, _ := s.pool.Begin(ctx)
			term, e := s.readReferralTerms(ctx, tx, o)
			tx.Rollback(ctx)
			want := int64(5)
			if rate == "20" {
				want = 0
			}
			if e != nil || term.RewardAmount != want {
				t.Fatal(term, e)
			}
			if _, _, e = s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native"); !errors.Is(e, ErrConflict) {
				t.Fatal("weak attempt account accepted", e)
			}
			if _, _, e = s.BeginProviderAttemptWithAccount(ctx, o.TradeNo, 8, "native", "profit71-app", "", q.estimate.AccountScope); !errors.Is(e, ErrConflict) {
				t.Fatal("wrong original route accepted", e)
			}
			if _, _, e = s.BeginProviderAttemptWithAccount(ctx, o.TradeNo, 7, "native", "profit71-app", "", q.estimate.AccountScope); e != nil {
				t.Fatal("original account rejected", e)
			}
			if _, e = s.pool.Exec(ctx, `ALTER TABLE referral_profit_costs DISABLE TRIGGER referral_profit_cost_immutable;ALTER TABLE referral_profit_cost_events DISABLE TRIGGER referral_profit_event_immutable;DELETE FROM referral_profit_cost_events;DELETE FROM referral_profit_costs`); e != nil {
				t.Fatal(e)
			}
			if e = s.markPaid(ctx, o.TradeNo, "profit71-missing-proof", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "LOCAL_" + o.TradeNo}); !errors.Is(e, ErrConflict) {
				t.Fatal("missing cost downgraded even zero reward", e)
			}
		})
	}
}

func TestReferralProfit71DisabledPolicyDoesNotBlockPaymentOrRepriceOldTerms(t *testing.T) {
	s, _, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	no, rate := false, int64(5000)
	p := referralTestPolicy(&no, &rate)
	p.Basis = "profit"
	if _, e := s.SetReferralPolicy(ctx, "global", 0, 1, p, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	o := referralCreateTestOrder(t, s, mid, 100, "profit71-disabled")
	referralPayTestOrder(t, s, o)
	var count int
	if e := s.pool.QueryRow(ctx, "SELECT count(*) FROM referral_profit_costs").Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
	yes := true
	p.Enabled = &yes
	if _, e := s.SetReferralPolicy(ctx, "global", 0, 2, p, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	tx, _ := s.pool.Begin(ctx)
	term, e := s.readReferralTerms(ctx, tx, o)
	tx.Rollback(ctx)
	if e != nil || term.Enabled || term.Reason != "disabled" {
		t.Fatal("old disabled repriced", term, e)
	}
	dailyLedgerClean70(t, s)
}

func profitOrder71(t *testing.T, mode string) (*Store, Order, int64) {
	t.Helper()
	s, promoter, mid, _ := referralRewardFixture(t)
	yes, rate := true, int64(5000)
	p := referralTestPolicy(&yes, &rate)
	p.Basis = "profit"
	p.Mode = mode
	ctx := context.Background()
	if _, e := s.SetReferralPolicy(ctx, "global", 0, 1, p, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	q := profitQuote71(t, "5")
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "profit71-lifecycle", Type: "wxpay", Name: "synthetic", Amount: 100, PaymentCost: q})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.BeginProviderAttemptWithAccount(ctx, o.TradeNo, 7, "native", "profit71-app", "", q.estimate.AccountScope); e != nil {
		t.Fatal(e)
	}
	if e = s.markPaid(ctx, o.TradeNo, "profit71-local-paid", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "LOCAL_" + o.TradeNo}); e != nil {
		t.Fatal(e)
	}
	return s, o, promoter
}

func TestReferralProfit71BothModesRefundUnknownFailedThenSuccess(t *testing.T) {
	for _, mode := range []string{"immediate", "daily"} {
		for _, awardFirst := range []bool{false, true} {
			t.Run(mode+fmt.Sprint(awardFirst), func(t *testing.T) {
				s, o, promoter := profitOrder71(t, mode)
				ctx := context.Background()
				day, clock := dailyFutureClock70()
				if mode == "daily" && awardFirst {
					if r, e := s.tickReferralDailyAt(ctx, day, 10, clock); e != nil || r.Awarded != 1 {
						t.Fatal(r, e)
					}
				}
				dailyLedgerClean70(t, s)
				q, e := s.ReferralRefundPreview(ctx, s.pool, o.ID, "profit71-refund-failed", 98)
				if e != nil || q.Amount != 3 || q.OriginalFee != 10 {
					t.Fatal(q, e)
				}
				r, e := s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "profit71-refund-failed", 98)
				if e != nil {
					t.Fatal(e)
				}
				if e = s.ResolveRefund(ctx, r.ID, "unknown"); e != nil {
					t.Fatal(e)
				}
				dailyLedgerClean70(t, s)
				if e = s.ResolveRefund(ctx, r.ID, "failed"); e != nil {
					t.Fatal(e)
				}
				dailyLedgerClean70(t, s)
				r, e = s.ReserveRefund(ctx, o.MerchantID, o.OutTradeNo, "profit71-refund-success", 1)
				if e != nil {
					t.Fatal(e)
				}
				if e = s.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
					t.Fatal(e)
				}
				if mode == "daily" {
					if r, e := s.tickReferralDailyAt(ctx, day, 10, clock); e != nil || r.Awarded != 0 {
						t.Fatal(r, e)
					}
				}
				b, e := s.Balance(ctx, promoter)
				if e != nil || b.Available != 0 || b.Reserved != 0 {
					t.Fatal(b, e)
				}
				dailyLedgerClean70(t, s)
			})
		}
	}
}

func TestReferralProfit71TamperingCannotBecomeDisabledOrFreeCost(t *testing.T) {
	for _, mutation := range []string{
		`ALTER TABLE referral_profit_costs DISABLE TRIGGER referral_profit_cost_immutable;UPDATE referral_profit_costs SET config_revision=repeat('a',64)`,
		`ALTER TABLE referral_profit_cost_events DISABLE TRIGGER referral_profit_event_immutable;DELETE FROM referral_profit_cost_events`,
		`ALTER TABLE referral_order_terms DISABLE TRIGGER referral_terms_immutable;UPDATE referral_order_terms SET enabled=false,reason='disabled',reward_amount=0;ALTER TABLE referral_profit_cost_events DISABLE TRIGGER referral_profit_event_immutable;DELETE FROM referral_profit_cost_events;ALTER TABLE referral_profit_costs DISABLE TRIGGER referral_profit_cost_immutable;DELETE FROM referral_profit_costs`,
		`ALTER TABLE provider_attempt_accounts DISABLE TRIGGER ALL;UPDATE provider_attempt_accounts SET account_hash=decode(repeat('00',32),'hex')`,
	} {
		t.Run(fmt.Sprint(len(mutation)), func(t *testing.T) {
			s, o, _ := profitOrder71(t, "immediate")
			ctx := context.Background()
			if _, e := s.pool.Exec(ctx, mutation); e != nil {
				t.Fatal(e)
			}
			if e := s.markPaid(ctx, o.TradeNo, "profit71-tampered-replay", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "LOCAL_" + o.TradeNo}); !errors.Is(e, ErrConflict) {
				t.Fatal("tamper accepted", e)
			}
			r, e := s.CheckLedger(ctx)
			if e != nil || r.InvalidReferralTerms == 0 {
				t.Fatal("tampered cost escaped checker", r, e)
			}
		})
	}
}

func TestReferralProfit71OriginalCostReplayAndStrongAppBeforeClaim(t *testing.T) {
	s, _, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	yes, rate := true, int64(5000)
	p := referralTestPolicy(&yes, &rate)
	p.Basis = "profit"
	if _, e := s.SetReferralPolicy(ctx, "global", 0, 1, p, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	q := profitQuote71(t, "5")
	in := CreateOrderInput{MerchantID: mid, OutTradeNo: "profit71-replay", Name: "synthetic", Type: "wxpay", Amount: 100, PaymentCost: q}
	o, e := s.CreateOrder(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	in.PaymentCost = profitQuote71(t, "0")
	replay, e := s.CreateOrder(ctx, in)
	if e != nil || replay.ID != o.ID {
		t.Fatal(replay, e)
	}
	if _, _, e = s.BeginProviderAttemptWithAccount(ctx, o.TradeNo, 7, "native", "wrong-app", "", q.estimate.AccountScope); !errors.Is(e, ErrConflict) {
		t.Fatal("different trusted application claimed original cost", e)
	}
	var n int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM provider_attempts WHERE order_id=$1", o.ID).Scan(&n); e != nil || n != 0 {
		t.Fatal("rejected identity consumed claim", n, e)
	}
	tx, _ := s.pool.Begin(ctx)
	term, e := s.readReferralTerms(ctx, tx, o)
	tx.Rollback(ctx)
	if e != nil || term.RewardAmount != 3 || term.profit.RateUnits != 5000000 {
		t.Fatal("replay repriced original cost", term, e)
	}
}

func TestReferralProfit71UnauthenticatedBasisCannotBypassCostClaim(t *testing.T) {
	s, _, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	yes, rate := true, int64(5000)
	p := referralTestPolicy(&yes, &rate)
	p.Basis = "profit"
	if _, e := s.SetReferralPolicy(ctx, "global", 0, 1, p, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	q := profitQuote71(t, "5")
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "profit71-policy-tamper", Name: "synthetic", Type: "wxpay", Amount: 100, PaymentCost: q})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE referral_policies DISABLE TRIGGER referral_policy_immutable;UPDATE referral_policies SET basis='fee' WHERE basis='profit';ALTER TABLE referral_profit_cost_events DISABLE TRIGGER referral_profit_event_immutable;DELETE FROM referral_profit_cost_events;ALTER TABLE referral_profit_costs DISABLE TRIGGER referral_profit_cost_immutable;DELETE FROM referral_profit_costs`); e != nil {
		t.Fatal(e)
	}
	if _, owner, e := s.BeginProviderAttemptWithAccount(ctx, o.TradeNo, 7, "native", "profit71-app", "", q.estimate.AccountScope); !errors.Is(e, ErrConflict) || owner {
		t.Fatal("unauthenticated policy bypassed cost before external claim", owner, e)
	}
}

func TestReferralProfit71ProvenZeroRewardNeedsReceiptAndKeepsFullRefund(t *testing.T) {
	for _, mode := range []string{"immediate", "daily"} {
		t.Run(mode, func(t *testing.T) {
			s, promoter, mid, _ := referralRewardFixture(t)
			ctx := context.Background()
			yes, rate := true, int64(5000)
			policy := referralTestPolicy(&yes, &rate)
			policy.Basis = "profit"
			policy.Mode = mode
			if _, e := s.SetReferralPolicy(ctx, "global", 0, 1, policy, referralPolicyAdmin); e != nil {
				t.Fatal(e)
			}
			q := profitQuote71(t, "20")
			o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "profit71-known-negative", Name: "synthetic", Type: "wxpay", Amount: 100, PaymentCost: q})
			if e != nil {
				t.Fatal(e)
			}
			if _, _, e = s.BeginProviderAttemptWithAccount(ctx, o.TradeNo, 7, "native", "profit71-app", "", q.estimate.AccountScope); e != nil {
				t.Fatal(e)
			}
			if e = s.MarkPaid(ctx, o.TradeNo, "profit71-unverified-zero"); !errors.Is(e, ErrState) {
				t.Fatal("R0 bypassed receipt provenance", e)
			}
			if e = s.markPaid(ctx, o.TradeNo, "profit71-verified-zero", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "LOCAL_" + o.TradeNo}); e != nil {
				t.Fatal(e)
			}
			b, e := s.Balance(ctx, promoter)
			if e != nil || b.Available != 0 {
				t.Fatal(b, e)
			}
			r, e := s.ReserveRefund(ctx, mid, o.OutTradeNo, "profit71-negative-full-refund", 100)
			if e != nil {
				t.Fatal(e)
			}
			if e = s.ResolveRefund(ctx, r.ID, "succeeded"); e != nil {
				t.Fatal(e)
			}
			dailyLedgerClean70(t, s)
		})
	}
}

func TestReferralProfit71EntireZeroRewardSourceLossCannotBecomeHistorical(t *testing.T) {
	s, _, mid, _ := referralRewardFixture(t)
	ctx := context.Background()
	yes, rate := true, int64(5000)
	policy := referralTestPolicy(&yes, &rate)
	policy.Basis = "profit"
	if _, e := s.SetReferralPolicy(ctx, "global", 0, 1, policy, referralPolicyAdmin); e != nil {
		t.Fatal(e)
	}
	q := profitQuote71(t, "20")
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "profit71-entire-source-loss", Name: "synthetic", Type: "wxpay", Amount: 100, PaymentCost: q})
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, `SET LOCAL session_replication_role=replica`); e != nil {
		t.Fatal(e)
	}
	for _, table := range []string{"referral_profit_cost_events", "referral_profit_costs", "referral_order_terms"} {
		if _, e = tx.Exec(ctx, "DELETE FROM "+table+" WHERE order_id=$1", o.ID); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	r, e := s.CheckLedger(ctx)
	if e != nil || r.InvalidReferralTerms == 0 {
		t.Errorf("new parent with all source children missing escaped checker: %+v err=%v", r, e)
	}
	if _, owner, e := s.BeginProviderAttemptWithAccount(ctx, o.TradeNo, 7, "native", "profit71-app", "", q.estimate.AccountScope); !errors.Is(e, ErrConflict) || owner {
		t.Errorf("new R0 parent became historical: owner=%v err=%v", owner, e)
	}
	if _, e = s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: o.OutTradeNo, Name: o.Name, Type: o.Type, Amount: o.Amount}); !errors.Is(e, ErrConflict) {
		t.Errorf("new R0 replay became historical: %v", e)
	}
}

func TestReferralProfit71CheckerFindsCostWithoutOriginalParent(t *testing.T) {
	s, o, _ := profitOrder71(t, "immediate")
	ctx := context.Background()
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "SET LOCAL session_replication_role=replica"); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "DELETE FROM referral_order_terms WHERE order_id=$1", o.ID); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	r, e := s.CheckLedger(ctx)
	if e != nil || r.InvalidReferralTerms == 0 {
		t.Fatal("orphan cost escaped checker", r, e)
	}
}
