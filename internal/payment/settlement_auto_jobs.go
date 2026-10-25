package payment

import (
	"context"
	"fmt"
)

const SettlementAutoJobKind = "settlement-auto.payout"

// QueueSettlementAuto selects only original generated requests covered by a
// currently sealed independent authority. It queues no manual requests and
// never calls a provider. The handler rechecks the exact authority at claim.
func (g ProviderFlow) QueueSettlementAuto(ctx context.Context, limit int) (int64, error) {
	if g.Store == nil || limit < 1 || limit > 100 {
		return 0, ErrInvalid
	}
	if !g.AllowLive {
		return 0, ErrLiveDisabled
	}
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	rows, e := tx.Query(ctx, `SELECT r.plan_id,h.authorization_id,i.request_id
 FROM settlement_auto_heads h JOIN settlement_auto_authorizations a ON a.id=h.authorization_id
 JOIN settlement_plans p ON p.id=h.plan_id JOIN settlement_plan_runs r ON r.plan_id=p.id AND r.authorization_id=a.generation_authorization_id AND r.plan_revision=a.plan_revision
 JOIN settlement_plan_items i ON i.run_id=r.id AND i.state='created' JOIN ledger_requests l ON l.id=i.request_id
 LEFT JOIN settlement_payouts sp ON sp.request_id=l.id
 WHERE p.enabled AND p.revision=a.plan_revision AND p.last_authorization_id=a.generation_authorization_id
 AND r.business_date=(clock_timestamp() AT TIME ZONE 'Asia/Shanghai')::date
 AND (a.input->>'valid_from')::timestamptz<=clock_timestamp() AND (a.input->>'expires_at')::timestamptz>clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM settlement_auto_revocations WHERE authorization_id=a.id)
 AND NOT EXISTS(SELECT 1 FROM settlement_plan_revocations WHERE authorization_id=a.generation_authorization_id)
 AND l.status IN('pending','approved') AND (sp.request_id IS NULL OR sp.send_state='unsent')
 AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.event_key='settlement-auto:'||a.id::text||':'||l.id::text)
 ORDER BY l.id LIMIT $1`, limit)
	if e != nil {
		return 0, e
	}
	type candidate struct{ plan, authority, request int64 }
	items := []candidate{}
	for rows.Next() {
		var c candidate
		if e = rows.Scan(&c.plan, &c.authority, &c.request); e != nil {
			rows.Close()
			return 0, e
		}
		items = append(items, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return 0, e
	}
	var count int64
	for _, c := range items {
		a, e := g.Store.currentSettlementAuto(ctx, tx, c.plan)
		if e != nil {
			return 0, e
		}
		if a.View.ID != c.authority {
			return 0, ErrConflict
		}
		if e = g.validateSettlementAutoCurrent(ctx, tx, a); e != nil {
			return 0, e
		}
		if e = g.Store.verifyScheduledRequestSource(ctx, tx, c.request); e != nil {
			return 0, e
		}
		tag, e := tx.Exec(ctx, `INSERT INTO jobs(event_key,kind,payload)VALUES($1,$2,jsonb_build_object('request_id',$3::text,'authorization_id',$4::text)) ON CONFLICT(event_key) DO NOTHING`, fmt.Sprintf("settlement-auto:%d:%d", c.authority, c.request), SettlementAutoJobKind, fmt.Sprint(c.request), fmt.Sprint(c.authority))
		if e != nil {
			return 0, e
		}
		count += tag.RowsAffected()
	}
	return count, tx.Commit(ctx)
}
