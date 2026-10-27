package contract_test

import (
	"context"
	"fmt"
	"net/http/httptest"
	"testing"
	"time"

	"goepay/internal/compat"
)

func globalSetting179(t *testing.T, f signatureFixture163, key, value string) {
	t.Helper()
	_, e := f.p.Exec(context.Background(), `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text)) ON CONFLICT ((body->>'key')) WHERE resource='settings' DO UPDATE SET body=EXCLUDED.body`, key, value)
	if e != nil {
		t.Fatal(e)
	}
}

func TestGlobalAccess179PublicDefaultsAreActualAndNoStore(t *testing.T) {
	f := signatureFixture(t, 2)
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, httptest.NewRequest("GET", "/console/v1/public/merchant-access", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("public platform capabilities HTTP=%d cache=%q, want200/no-store", w.Code, w.Header().Get("Cache-Control"))
	}
	v := credentialData162(t, w)
	if len(v) != 2 || v["close_keylogin"] != float64(0) || v["user_refund"] != float64(1) {
		t.Fatal("platform capability shape/defaults", v)
	}
}

func TestGlobalAccess179CloseBlocksActualNewKeyLoginAndPreparedGrant(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		t.Run(fmt.Sprint(prepared), func(t *testing.T) {
			f := signatureFixture(t, 2)
			ctx := context.Background()
			m, e := f.s.Store.Merchant(ctx, 2)
			if e != nil {
				t.Fatal(e)
			}
			if prepared {
				g, e := f.s.Store.PrepareMerchantKeyLogin(ctx, 2, m.APIKey)
				if e != nil {
					t.Fatal("enabled positive prepare", e)
				}
				globalSetting179(t, f, "close_keylogin", "1")
				if _, _, _, e = f.s.Store.CompleteMerchantKeyLogin(ctx, g, f.s.Sessions); e == nil {
					t.Fatal("platform close accepted a previously prepared original credential grant")
				}
			} else {
				globalSetting179(t, f, "close_keylogin", "1")
				w := merchant174KeyLogin(f, "2", m.APIKey, "")
				if w.Code != 401 || len(w.Result().Cookies()) != 0 {
					t.Fatalf("platform close admitted new original merchant key login HTTP=%d cookies=%d", w.Code, len(w.Result().Cookies()))
				}
			}
		})
	}
}

func TestGlobalAccess179RefundCloseBlocksNewAndMerelyReservedSignedIntents(t *testing.T) {
	for _, reserved := range []bool{false, true} {
		t.Run(fmt.Sprint(reserved), func(t *testing.T) {
			f, p, o := merchant174Runtime(t)
			ctx := context.Background()
			if reserved {
				if _, e := f.s.Store.ReserveRefund(ctx, 2, o.OutTradeNo, "platform-refund179", 100); e != nil {
					t.Fatal(e)
				}
			}
			globalSetting179(t, f, "user_refund", "0")
			out := merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "platform-refund179", "money": "1.00"})
			var operations int
			if e := f.p.QueryRow(ctx, `SELECT count(*) FROM provider_operations WHERE kind='refund'`).Scan(&operations); e != nil {
				t.Fatal(e)
			}
			if out["code"] != float64(-1) || operations != 0 || p.refundWrites.Load() != 0 {
				t.Fatalf("platform disabled newly sent refund: code=%v operations=%d sends=%d reserved=%v", out["code"], operations, p.refundWrites.Load(), reserved)
			}
		})
	}
}

func TestGlobalAccess179RefundClosePreservesAcceptedOriginalRecovery(t *testing.T) {
	f, p, o := merchant174Runtime(t)
	params := compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "accepted-platform179", "money": "1.00"}
	if out := merchant174Invoke(t, f, "/api/pay/refund", params); out["code"] != float64(0) || p.refundWrites.Load() != 1 {
		t.Fatal("initial accepted refund positive", out, p.refundWrites.Load())
	}
	globalSetting179(t, f, "user_refund", "0")
	if out := merchant174Invoke(t, f, "/api/pay/refund", params); out["code"] != float64(0) || p.refundWrites.Load() != 1 {
		t.Fatal("closing platform gate lost accepted same-ID recovery or resent", out, p.refundWrites.Load())
	}
}

func TestGlobalAccess179CloseQueuedDuringReserveMustWinBeforeFirstSend(t *testing.T) {
	runQueuedRefund182(t, false)
}

func TestGlobalAccess182UnsentRejectionEvidenceFailureRollsBackAndSameIntentRecovers(t *testing.T) {
	runQueuedRefund182(t, true)
}

func runQueuedRefund182(t *testing.T, failEvidence bool) {
	t.Helper()
	f, provider, o := merchant174Runtime(t)
	globalSetting179(t, f, "user_refund", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if failEvidence {
		if _, e := f.p.Exec(ctx, `CREATE FUNCTION reject_result182() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'synthetic result failure';END$$;CREATE TRIGGER reject_result182 BEFORE INSERT ON provider_refund_result_evidence FOR EACH ROW EXECUTE FUNCTION reject_result182()`); e != nil {
			t.Fatal(e)
		}
	}
	holder, e := f.p.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer holder.Release()
	if _, e = holder.Exec(ctx, `SELECT pg_advisory_lock(17901001)`); e != nil {
		t.Fatal(e)
	}
	defer holder.Exec(context.Background(), `SELECT pg_advisory_unlock(17901001)`)
	if _, e = f.p.Exec(ctx, `CREATE FUNCTION access179_hold_reserve() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN PERFORM pg_advisory_xact_lock(17901001);RETURN NEW;END$$;CREATE TRIGGER access179_hold_reserve BEFORE INSERT ON refunds FOR EACH ROW EXECUTE FUNCTION access179_hold_reserve()`); e != nil {
		t.Fatal(e)
	}
	result := make(chan map[string]any, 1)
	go func() {
		result <- merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "reserve-race179", "money": "1.00"})
	}()
	waitBlocked := func(pid uint32) {
		t.Helper()
		for {
			var waiting bool
			if e := f.p.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::int=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock')`, int(pid)).Scan(&waiting); e != nil {
				t.Fatal(e)
			}
			if waiting {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("refund never reached controlled reservation lock")
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
	waitBlocked(holder.Conn().PgConn().PID())
	closer, e := f.p.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer closer.Release()
	closePID := closer.Conn().PgConn().PID()
	closed := make(chan error, 1)
	go func() {
		tx, err := closer.Begin(ctx)
		if err != nil {
			closed <- err
			return
		}
		defer tx.Rollback(context.Background())
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7170140001)`); err == nil {
			_, err = tx.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"0"') WHERE resource='settings' AND body->>'key'='user_refund'`)
		}
		if err == nil {
			err = tx.Commit(ctx)
		}
		closed <- err
	}()
	for {
		var waiting bool
		if e = f.p.QueryRow(ctx, `SELECT cardinality(pg_blocking_pids($1::int))>0`, int(closePID)).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("platform close did not queue behind the active admission")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, e = holder.Exec(ctx, `SELECT pg_advisory_unlock(17901001)`); e != nil {
		t.Fatal(e)
	}
	if e = <-closed; e != nil {
		t.Fatal(e)
	}
	var out map[string]any
	select {
	case out = <-result:
	case <-ctx.Done():
		t.Fatal("original refund request did not finish")
	}
	var operations int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM provider_operations WHERE kind='refund'`).Scan(&operations); e != nil {
		t.Fatal(e)
	}
	if out["code"] != float64(-1) || operations != 0 || provider.refundWrites.Load() != 0 {
		t.Fatalf("reserve→first-send bypassed queued platform close: code=%v operations=%d sends=%d", out["code"], operations, provider.refundWrites.Load())
	}
	r, err := f.s.Store.Refund(ctx, 2, "reserve-race179")
	b, balanceErr := f.s.Store.Balance(ctx, 2)
	if failEvidence {
		if err != nil || balanceErr != nil || r.Status != "pending" || b.Available != 900 || b.Reserved != 100 {
			t.Fatal("failed local result partially released funds", r.Status, b, err, balanceErr)
		}
		var results, releases int
		if e := f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM provider_refund_result_evidence WHERE refund_id=$1),(SELECT count(*) FROM journals WHERE event_key=$2)`, r.ID, fmt.Sprintf("refund:%d:resolve", r.ID)).Scan(&results, &releases); e != nil || results != 0 || releases != 0 {
			t.Fatal(results, releases, e)
		}
		if _, e := f.p.Exec(ctx, `DROP TRIGGER reject_result182 ON provider_refund_result_evidence;DROP FUNCTION reject_result182()`); e != nil {
			t.Fatal(e)
		}
		globalSetting179(t, f, "user_refund", "1")
		out := merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": r.RefundNo, "money": "1.00"})
		if out["code"] != float64(0) || provider.refundWrites.Load() != 1 {
			t.Fatal("same reserved intent did not recover exactly once", out, provider.refundWrites.Load())
		}
		if _, e := f.s.ProviderFlow.QueryRefund(ctx, 2, r.RefundNo); e != nil {
			t.Fatal(e)
		}
		if _, e := f.s.Store.CheckLedger(ctx); e != nil {
			t.Fatal(e)
		}
		return
	}
	if err != nil || balanceErr != nil || r.Status != "failed" || b.Available != 1000 || b.Reserved != 0 {
		t.Fatalf("rejected unsent refund retained funds: status=%s available=%d reserved=%d err=%v balanceErr=%v", r.Status, b.Available, b.Reserved, err, balanceErr)
	}
	queried, err := f.s.ProviderFlow.QueryRefund(ctx, 2, r.RefundNo)
	if err != nil || queried.Status != "failed" || provider.refundWrites.Load() != 0 {
		t.Fatal("local definitive rejection is not queryable without upstream I/O", queried.Status, err)
	}
	if _, err = f.s.Store.CheckLedger(ctx); err != nil {
		t.Fatal("released reservation lost financial evidence", err)
	}
}
