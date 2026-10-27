package contract_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"goepay/internal/compat"
)

func TestMerchant174ClosingPolicyWinsBeforeBlockedNewFundsAdmission(t *testing.T) {
	for _, kind := range []string{"refund", "transfer"} {
		t.Run(kind, func(t *testing.T) {
			f, p, o := merchant174Runtime(t)
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			merchant174Set(t, f, 1, 1, 1)
			state := apiSwitchState174(t, f.request("GET", "api-switches", "", f.csrf))
			if _, e := f.p.Exec(ctx, `CREATE FUNCTION merchant174_policy_write_gate() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='merchant-api-switches-update' THEN PERFORM pg_advisory_xact_lock(hashtextextended(current_schema()||'/merchant174-close',0));END IF;RETURN NEW;END$$;CREATE TRIGGER merchant174_policy_write_gate BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION merchant174_policy_write_gate()`); e != nil {
				t.Fatal(e)
			}
			gate, e := f.p.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer gate.Rollback(context.Background())
			if _, e = gate.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended(current_schema()||'/merchant174-close',0))`); e != nil {
				t.Fatal(e)
			}
			closed := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				closed <- f.request("PATCH", "api-switches", apiSwitchBody174(state["fingerprint"].(string), 0, 0, 0), f.csrf)
			}()
			merchant174WaitForBlocker(t, f, gate.Conn().PgConn().PID(), ctx)
			var closerPID uint32
			if e = f.p.QueryRow(ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1::integer=ANY(pg_blocking_pids(pid)) LIMIT 1`, gate.Conn().PgConn().PID()).Scan(&closerPID); e != nil {
				t.Fatal(e)
			}
			result := make(chan map[string]any, 1)
			go func() {
				if kind == "refund" {
					result <- merchant174Invoke(t, f, "/api/pay/refund", compat.Params{"out_trade_no": o.OutTradeNo, "out_refund_no": "race-new-refund174", "money": "1.00"})
				} else {
					result <- merchant174Invoke(t, f, "/api/transfer/submit", compat.Params{"type": "alipay", "channel_id": "7", "out_biz_no": "race-new-transfer174", "account": "synthetic@example.invalid", "money": "1.00", "remark": "synthetic"})
				}
			}()
			merchant174WaitForBlocker(t, f, closerPID, ctx)
			if e = gate.Rollback(ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case w := <-closed:
				if w.Code != 200 {
					t.Fatal("valid closing policy did not commit", w.Code)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			select {
			case out := <-result:
				if out["code"] != float64(-1) {
					t.Fatal("new funds used stale pre-close personal setting", kind, out)
				}
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			var intents int
			if e = f.p.QueryRow(ctx, `SELECT (SELECT count(*) FROM refunds)+(SELECT count(*) FROM transfers)`).Scan(&intents); e != nil || intents != 0 || p.refundWrites.Load() != 0 || p.transferWrites.Load() != 0 {
				t.Fatal("closing race reserved/sent new funds", intents, e)
			}
		})
	}
}
