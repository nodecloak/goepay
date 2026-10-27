package payment

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/riskpolicy"
	"testing"
	"time"
)

// This fixture authors only terminal failed local attempts, never financial
// success. A DB-derived future observation exercises old-history selection;
// production admission always supplies its actual database clock.
func TestRisk177IPWindowBatchesProofsAndDoesNotPermanentlyRejectLargeOldHistory(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	const ip = "192.0.2.177"
	if e := s.CreateMerchant(ctx, Merchant{ID: 177, Name: "synthetic-risk-history", APIKey: "synthetic-only"}); e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e := s.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	add := func(start, n int64, state string) {
		t.Helper()
		tx, e := s.pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer tx.Rollback(ctx)
		var xid string
		if e = tx.QueryRow(ctx, `SELECT pg_current_xact_id()::text`).Scan(&xid); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO orders(id,trade_no,merchant_id,out_trade_no,name,type,amount,status) SELECT i,'risk-history-'||i,177,'risk-history-'||i,'synthetic','alipay',100,'failed' FROM generate_series($1::bigint,$2::bigint)i`, start, start+n-1); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.Exec(ctx, `INSERT INTO provider_attempts(id,order_id,channel_id,method,state,core_risk_required) SELECT i,i,7,'page',$3,true FROM generate_series($1::bigint,$2::bigint)i`, start, start+n-1, state); e != nil {
			t.Fatal(e)
		}
		key, e := s.coreRiskIPKey(ip)
		if e != nil {
			t.Fatal(e)
		}
		originRows := make([][]any, 0, n)
		auditRows := make([][]any, 0, n)
		for id := start; id < start+n; id++ {
			f := coreRiskOrigin{OrderID: id, AttemptID: id, MerchantID: 177, ChannelID: 7, Amount: 100, Type: "alipay", Method: "page", Transaction: xid, IPs: []string{ip}, AdmittedAt: now}
			proof, e := s.coreRiskOriginProof(f)
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := json.Marshal(f)
			witness, _ := json.Marshal(map[string]any{"attempt_id": fmt.Sprint(id), "source_hash": hex.EncodeToString(proof), "subject_keys": []string{key}, "buyer_key": "", "admitted_at": coreRiskTimeKey(now)})
			originRows = append(originRows, []any{id, id, []string{ip}, now, raw, proof})
			auditRows = append(auditRows, []any{"system", int64(0), "core-risk", "core-risk-origins", id, "admit", witness})
		}
		if _, e = tx.CopyFrom(ctx, pgx.Identifier{"core_risk_origins"}, []string{"order_id", "attempt_id", "ips", "admitted_at", "facts", "proof_hash"}, pgx.CopyFromRows(originRows)); e != nil {
			t.Fatal(e)
		}
		if _, e = tx.CopyFrom(ctx, pgx.Identifier{"backoffice_audit"}, []string{"actor_role", "actor_id", "actor_name", "resource", "resource_id", "operation", "changes"}, pgx.CopyFromRows(auditRows)); e != nil {
			t.Fatal(e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
	}
	counter := &risk177QueryCounter{}
	cfg := s.pool.Config()
	cfg.ConnConfig.Tracer = counter
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	reader, e := NewWithKey(pool, make([]byte, 32))
	if e != nil {
		t.Fatal(e)
	}
	settings := riskpolicy.Defaults()
	settings["pay_iplimit"] = "1000"
	settings["pay_ip_interval"] = "1"
	read := func() (int64, error) {
		tx, e := pool.Begin(ctx)
		if e != nil {
			return 0, e
		}
		defer tx.Rollback(ctx)
		counter.n.Store(0)
		e = reader.checkCoreRiskIP(ctx, tx, settings, ip, now.Add(48*time.Hour))
		return counter.n.Load(), e
	}
	add(1, 2, "failed")
	small, e := read()
	if e != nil {
		t.Fatal(e)
	}
	add(3, 99999, "failed")
	before := time.Now()
	large, e := read()
	elapsed := time.Since(before)
	if e != nil || small != large {
		t.Fatal("old failed history permanently rejected or added SQL", small, large, e)
	}
	add(100002, 2, "pending")
	two, e := read()
	if e != nil {
		t.Fatal(e)
	}
	add(100004, 18, "pending")
	twenty, e := read()
	if e != nil || two != twenty {
		t.Fatal("pending proof read grew per source", two, twenty, e)
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE core_risk_origins DISABLE TRIGGER USER; DELETE FROM core_risk_origins WHERE order_id=100002; ALTER TABLE core_risk_origins ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if _, e = read(); !errors.Is(e, ErrConflict) {
		t.Fatal("missing unresolved origin admitted", e)
	}
	t.Logf("old terminal history 2 vs 100001 = %d vs %d SQL; 100001 elapsed %s; pending 2 vs 20 = %d vs %d SQL; missing private source rejected", small, large, elapsed, two, twenty)
}

func TestRisk177IPWindowUsesOriginalPaidDayAndNeverCountsOldPendingAttemptAsUnpaid(t *testing.T) {
	s := testStore(t)
	ctx := WithCoreRiskRequest(context.Background(), CoreRiskRequest{PeerIP: "192.0.2.178", BuyerIP: "192.0.2.178", Source: "direct_submit"})
	if e := s.CreateMerchant(ctx, Merchant{ID: 178, Name: "synthetic-paid-window", APIKey: "synthetic-only"}); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 178, OutTradeNo: "paid-window", Name: "synthetic", Type: "alipay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	a, owner, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "page")
	if e != nil || !owner {
		t.Fatal(e, owner)
	}
	if e = s.RecordProviderAttemptResult(ctx, a.ID, "pending", "html", "synthetic", "SYNTHETIC_PAID_WINDOW"); e != nil {
		t.Fatal(e)
	}
	if e = s.markPaid(ctx, o.TradeNo, "synthetic-paid-window", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "SYNTHETIC_PAID_WINDOW", RealMoney: 100}); e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e = s.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	settings := riskpolicy.Defaults()
	settings["pay_iplimit"] = "1"
	read := func(at time.Time) error {
		tx, e := s.pool.Begin(ctx)
		if e != nil {
			return e
		}
		defer tx.Rollback(ctx)
		return s.checkCoreRiskIP(ctx, tx, settings, "192.0.2.178", at)
	}
	if e = read(now); !errors.Is(e, ErrState) {
		t.Fatal("original paid day did not consume IP slot", e)
	}
	if e = read(now.Add(48 * time.Hour)); e != nil {
		t.Fatal("old verified success with original pending attempt occupied future unpaid slot", e)
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE core_risk_successes DISABLE TRIGGER USER; DELETE FROM core_risk_successes; ALTER TABLE core_risk_successes ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	if e = read(now); !errors.Is(e, ErrConflict) {
		t.Fatal("missing current-day success source admitted", e)
	}
}
