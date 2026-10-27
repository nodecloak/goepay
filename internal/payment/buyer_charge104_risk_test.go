package payment

import (
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/riskpolicy"
	"testing"
	"time"
)

func TestBuyerCharge104RiskSuccessUsesAuthenticatedActualAndBatchesSources(t *testing.T) {
	s := testStore(t)
	const mid = 10431
	feeMerchant(t, s, mid, 600, "platform")
	buyer104PolicyFixture(t, s, "merchant", mid, 1, nil, nil)
	ctx := withCoreRiskBuyer(WithCoreRiskRequest(t.Context(), CoreRiskRequest{PeerIP: "192.0.2.104", BuyerIP: "192.0.2.104", Source: "direct_submit"}), "BUYER104")
	ids := []int64{}
	for i := 0; i < 4; i++ {
		o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: fmt.Sprint("risk-C104-", i), Type: "alipay", Amount: 100})
		if e != nil {
			t.Fatal(e)
		}
		a, owner, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "page")
		if e != nil || !owner {
			t.Fatal(owner, e)
		}
		trade := fmt.Sprint("LOCAL104RISK", i)
		if e = s.RecordProviderAttemptResult(ctx, a.ID, "pending", "html", "synthetic", trade); e != nil {
			t.Fatal(e)
		}
		if e = s.markPaid(ctx, o.TradeNo, "synthetic104-"+trade, true, PaidReceipt{RealMoney: 106, ChannelID: 7, ProviderTradeNo: trade, BuyerID: "BUYER104"}); e != nil {
			t.Fatal(e)
		}
		var amount int64
		if e = s.pool.QueryRow(ctx, `SELECT (facts->>'Amount')::bigint FROM core_risk_successes WHERE order_id=$1`, o.ID).Scan(&amount); e != nil || amount != 106 {
			t.Fatal("successful daily amount substituted merchant M", amount, e)
		}
		ids = append(ids, o.ID)
	}
	counter := &buyerQueryCounter104{}
	cfg := s.pool.Config()
	cfg.ConnConfig.Tracer = counter
	p, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Close()
	reader, e := NewWithKey(p, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	read := func(selected []int64) (int32, error) {
		tx, e := p.Begin(ctx)
		if e != nil {
			return 0, e
		}
		defer tx.Rollback(ctx)
		counter.selects.Store(0)
		facts, e := reader.coreRiskAdmissionFactsTx(ctx, tx, selected)
		if e == nil {
			for _, f := range facts {
				if f.Origin.Amount != 100 || f.Success == nil || f.Success.Amount != 106 {
					t.Fatal("M and C risk facts collapsed", f)
				}
			}
		}
		return counter.selects.Load(), e
	}
	one, e := read(ids[:1])
	if e != nil {
		t.Fatal(e)
	}
	four, e := read(ids)
	if e != nil || one != four {
		t.Fatal("risk amount reads grew per order", one, four, e)
	}
	key, e := s.coreRiskBuyerKey("alipay", "BUYER104")
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	settings := riskpolicy.Defaults()
	settings["pay_daymoney"] = "4.20"
	if e = s.checkCoreRiskBuyer(ctx, tx, settings, key, now); !errors.Is(e, ErrState) {
		t.Fatal("confirmed C424 >=420 did not stop next admission", e)
	}
	tx.Rollback(ctx)
	if _, e = s.pool.Exec(ctx, `ALTER TABLE order_customer_charges104 DISABLE TRIGGER USER;DELETE FROM order_customer_charges104 WHERE order_id=`+fmt.Sprint(ids[0])+`;ALTER TABLE order_customer_charges104 ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = read(ids); !errors.Is(e, ErrConflict) {
		t.Fatal("risk admitted after source loss", e)
	}
	t.Logf("one/four source authentication remains %d/%d SELECTs", one, four)
}
