package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

type incomeFundingRecord struct {
	HoldID, OrderID, MerchantID, AccountID, Amount, JournalID, LegacyJournalID int64
	Mode, ExternalNo                                                           string
	Actor, FreezeActor                                                         IncomeHoldActor
	Proof                                                                      []byte
	At                                                                         time.Time
}
type incomeReleaseRecord struct {
	HoldID, Amount, Consumed, JournalID int64
	Actor                               IncomeHoldActor
	Request, Proof                      []byte
	At                                  time.Time
}

func incomeAudit(ctx context.Context, tx pgx.Tx, resource string, id int64, op string, actor *IncomeHoldActor, changes any) error {
	role, name, aid := "system", "refund-engine", int64(0)
	if actor != nil {
		role, name, aid = "admin", actor.Name, actor.ID
	}
	raw, e := json.Marshal(changes)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,$4,$5,$6,$7)", role, aid, name, resource, id, op, raw)
	return e
}
func incomeJournal(ctx context.Context, tx pgx.Tx, key string, from, to, amount int64) (int64, error) {
	if amount == 0 {
		return 0, nil
	}
	if e := postByAccountIDs(ctx, tx, key, "original income funding", from, to, amount); e != nil {
		return 0, e
	}
	var id int64
	e := tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", key).Scan(&id)
	return id, e
}
func (s *Store) incomeFundingProof(f incomeFundingRecord, h OrderIncomeHold, o Order) ([]byte, error) {
	return s.referralDigest("income-hold-funding", []any{f.HoldID, f.OrderID, f.MerchantID, f.AccountID, f.Amount, f.Mode, f.ExternalNo, f.JournalID, f.LegacyJournalID, f.Actor, f.FreezeActor, f.At.UTC().Format(time.RFC3339Nano), h.RequestNo, h.MerchantNet, h.RefundMerchantAmount, h.ChannelSharingAmount, h.InternalAllocatedAmount, h.EvidenceVersion, h.CreatedAt.UTC().Format(time.RFC3339Nano), o.Amount, o.MerchantAmount, o.PlatformFee, o.RefundFeePayer})
}
func (s *Store) incomeReleaseProof(r incomeReleaseRecord) ([]byte, error) {
	return s.referralDigest("income-hold-release", []any{r.HoldID, r.Amount, r.Consumed, r.JournalID, r.Actor, r.Request, r.At.UTC().Format(time.RFC3339Nano)})
}
func (s *Store) scanIncomeFunding(ctx context.Context, q referralReader, h OrderIncomeHold) (incomeFundingRecord, error) {
	var f incomeFundingRecord
	e := q.QueryRow(ctx, `SELECT hold_id,order_id,merchant_id,account_id,amount,source_mode,external_no,funding_journal_id,COALESCE(legacy_pool_journal_id,0),actor_id,actor_name,actor_class,freeze_actor_id,freeze_actor_name,freeze_actor_class,request_hash,created_at FROM order_income_hold_fundings WHERE hold_id=$1`, h.ID).Scan(&f.HoldID, &f.OrderID, &f.MerchantID, &f.AccountID, &f.Amount, &f.Mode, &f.ExternalNo, &f.JournalID, &f.LegacyJournalID, &f.Actor.ID, &f.Actor.Name, &f.Actor.Class, &f.FreezeActor.ID, &f.FreezeActor.Name, &f.FreezeActor.Class, &f.Proof, &f.At)
	if errors.Is(e, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	if e != nil {
		return f, e
	}
	o, e := scanOrder(q.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", h.OrderID))
	if e != nil {
		return f, e
	}
	if _, _, e = s.authenticatedOrderChargeReader104(ctx, q, o); e != nil {
		return f, e
	}
	want, e := s.incomeFundingProof(f, h, o)
	if e != nil || !hmac.Equal(want, f.Proof) || f.OrderID != h.OrderID || f.MerchantID != h.MerchantID || f.Amount != h.Amount {
		return f, ErrConflict
	}
	var valid bool
	if e = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM order_income_hold_funding_evidence WHERE id=$1 AND valid IS TRUE)", h.ID).Scan(&valid); e != nil {
		return f, e
	}
	if !valid {
		return f, ErrConflict
	}
	var r incomeReleaseRecord
	e = q.QueryRow(ctx, `SELECT hold_id,amount,consumed_amount,COALESCE(journal_id,0),actor_id,actor_name,actor_class,request_hash,result_hash,created_at FROM order_income_hold_releases WHERE hold_id=$1`, h.ID).Scan(&r.HoldID, &r.Amount, &r.Consumed, &r.JournalID, &r.Actor.ID, &r.Actor.Name, &r.Actor.Class, &r.Request, &r.Proof, &r.At)
	if errors.Is(e, pgx.ErrNoRows) {
		if h.State == "released" {
			return f, ErrConflict
		}
		return f, nil
	}
	if e != nil {
		return f, e
	}
	want, e = s.incomeReleaseProof(r)
	if e != nil || h.State != "released" || !hmac.Equal(r.Request, f.Proof) || !hmac.Equal(want, r.Proof) {
		return f, ErrConflict
	}
	return f, nil
}
func (s *Store) incomeHoldSummary(ctx context.Context, q referralReader, h OrderIncomeHold, f incomeFundingRecord) (OrderIncomeHold, []byte, error) {
	h.RefundPendingCount = 0
	var used, pending, succeeded int64
	hash := f.Proof
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, q, "SELECT refund_id FROM refund_fundings WHERE hold_id=$1 AND refund_id>$2 ORDER BY refund_id LIMIT 100", h.ID, cursor)
		if e != nil {
			return h, nil, e
		}
		for _, id := range ids {
			r, e := scanRefund(q.QueryRow(ctx, "SELECT "+refundColumns+" FROM refunds WHERE id=$1", id))
			if e != nil {
				return h, nil, e
			}
			x, e := s.scanRefundFunding(ctx, q, r)
			if e != nil {
				return h, nil, e
			}
			if e = s.verifyProviderRefundResult(ctx, q, r); e != nil {
				return h, nil, e
			}
			if r.Status != "failed" {
				if x.Held > h.Amount-used {
					return h, nil, ErrConflict
				}
				used += x.Held
				if r.Status == "succeeded" {
					succeeded += x.Held
				} else {
					pending += x.Held
					h.RefundPendingCount++
				}
			}
			hash, e = s.referralDigest("income-hold-history", []any{hash, id, r.Status, x.Proof, x.Result.Proof})
			if e != nil {
				return h, nil, e
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	h.FundingID = f.HoldID
	h.SourceMode = f.Mode
	h.RefundReservedAmount = pending
	h.RefundSucceededAmount = succeeded
	h.RemainingAmount = h.Amount - used
	if h.State == "released" {
		h.RemainingAmount = 0
	}
	return h, hash, nil
}
func (s *Store) readIncomeHold(ctx context.Context, q referralReader, h OrderIncomeHold) (OrderIncomeHold, incomeFundingRecord, []byte, error) {
	// Historical migration tests read the original metadata immediately after
	// its old migration. A current database cannot use this fallback: the
	// immutable birth-version column persists even if its evidence table is lost.
	var current bool
	if e := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid='order_income_holds'::regclass AND attname='funding_evidence_version' AND NOT attisdropped)").Scan(&current); e != nil {
		return h, incomeFundingRecord{}, nil, e
	}
	if !current {
		h.SourceMode = "legacy_unprepared"
		if h.State == "held" {
			h.RemainingAmount = h.Amount
		}
		return h, incomeFundingRecord{}, nil, nil
	}
	f, e := s.scanIncomeFunding(ctx, q, h)
	if errors.Is(e, ErrNotFound) {
		var valid bool
		var version int
		if e = q.QueryRow(ctx, "SELECT funding_evidence_version FROM order_income_holds WHERE id=$1", h.ID).Scan(&version); e != nil {
			return h, f, nil, e
		}
		if e = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM order_income_hold_funding_evidence WHERE id=$1 AND valid IS TRUE)", h.ID).Scan(&valid); e != nil {
			return h, f, nil, e
		}
		if version != 1 || !valid {
			return h, f, nil, ErrConflict
		}
		h.SourceMode = "legacy_unprepared"
		if h.State == "held" {
			h.RemainingAmount = h.Amount
		}
		return h, f, nil, nil
	}
	if e != nil {
		return h, f, nil, e
	}
	h, hash, e := s.incomeHoldSummary(ctx, q, h, f)
	return h, f, hash, e
}
func (s *Store) createIncomeFunding(ctx context.Context, tx pgx.Tx, o Order, h OrderIncomeHold, actor IncomeHoldActor, mode, external string, beforePost ...func() error) (incomeFundingRecord, error) {
	f := incomeFundingRecord{HoldID: h.ID, OrderID: o.ID, MerchantID: o.MerchantID, Amount: h.Amount, Mode: mode, ExternalNo: external, Actor: actor}
	if e := tx.QueryRow(ctx, "SELECT actor_id,actor_name,actor_class FROM order_income_hold_events WHERE hold_id=$1 AND action='freeze'", h.ID).Scan(&f.FreezeActor.ID, &f.FreezeActor.Name, &f.FreezeActor.Class); e != nil {
		return f, e
	}
	if e := lockMerchantForeignKeySet(ctx, tx, []int64{o.MerchantID}); e != nil {
		return f, e
	}
	if e := tx.QueryRow(ctx, "INSERT INTO accounts(owner_id,kind,scope_id) VALUES($1,'order_income_reserve',$2) RETURNING id", o.MerchantID, h.ID).Scan(&f.AccountID); e != nil {
		return f, e
	}
	var from int64
	kind := "available"
	suffix := "freeze"
	if mode == "legacy_adopt" {
		kind = "order_income_hold"
		suffix = "adopt"
		if e := tx.QueryRow(ctx, "SELECT id FROM journals WHERE event_key=$1", fmt.Sprintf("order-income:%d:freeze", h.ID)).Scan(&f.LegacyJournalID); e != nil {
			return f, e
		}
	}
	if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind=$2 AND scope_id=0", o.MerchantID, kind).Scan(&from); e != nil {
		return f, e
	}
	if e := lockLedgerAccountSet(ctx, tx, []int64{from, f.AccountID}); e != nil {
		return f, e
	}
	for _, verify := range beforePost {
		if e := verify(); e != nil {
			return f, e
		}
	}
	var e error
	f.JournalID, e = incomeJournal(ctx, tx, fmt.Sprintf("order-income:%d:%s", h.ID, suffix), from, f.AccountID, h.Amount)
	if e != nil {
		return f, e
	}
	if e = tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&f.At); e != nil {
		return f, e
	}
	f.Proof, e = s.incomeFundingProof(f, h, o)
	if e != nil {
		return f, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO order_income_hold_fundings(hold_id,order_id,merchant_id,account_id,amount,source_mode,external_no,funding_journal_id,legacy_pool_journal_id,actor_id,actor_name,actor_class,request_hash,created_at,freeze_actor_id,freeze_actor_name,freeze_actor_class) VALUES($1,$2,$3,$4,$5,$6,$7,$8,NULLIF($9::bigint,0),$10,$11,$12,$13,$14,$15,$16,$17)`, f.HoldID, f.OrderID, f.MerchantID, f.AccountID, f.Amount, f.Mode, f.ExternalNo, f.JournalID, f.LegacyJournalID, f.Actor.ID, f.Actor.Name, f.Actor.Class, f.Proof, f.At, f.FreezeActor.ID, f.FreezeActor.Name, f.FreezeActor.Class); e != nil {
		return f, e
	}
	e = incomeAudit(ctx, tx, "income-fundings", h.ID, mode, &actor, map[string]any{"hold_id": fmt.Sprint(h.ID), "amount": fmt.Sprint(h.Amount), "external_no": external})
	return f, e
}
func (s *Store) releaseIncomeFunding(ctx context.Context, tx pgx.Tx, h OrderIncomeHold, f incomeFundingRecord, actor IncomeHoldActor) error {
	var unresolved bool
	if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM refund_fundings f JOIN refunds r ON r.id=f.refund_id WHERE f.hold_id=$1 AND r.status IN('pending','unknown'))", h.ID).Scan(&unresolved); e != nil {
		return e
	}
	if unresolved {
		return ErrState
	}
	if h.RefundReservedAmount != 0 {
		return ErrState
	}
	var av int64
	if e := tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", h.MerchantID).Scan(&av); e != nil {
		return e
	}
	if e := lockMerchantForeignKeySet(ctx, tx, []int64{h.MerchantID}); e != nil {
		return e
	}
	if e := lockLedgerAccountSet(ctx, tx, []int64{av, f.AccountID}); e != nil {
		return e
	}
	r := incomeReleaseRecord{HoldID: h.ID, Amount: h.RemainingAmount, Consumed: h.RefundSucceededAmount, Actor: actor, Request: f.Proof}
	var e error
	r.JournalID, e = incomeJournal(ctx, tx, fmt.Sprintf("order-income:%d:unfreeze", h.ID), f.AccountID, av, r.Amount)
	if e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, "SELECT transaction_timestamp()").Scan(&r.At); e != nil {
		return e
	}
	r.Proof, e = s.incomeReleaseProof(r)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO order_income_hold_releases(hold_id,amount,consumed_amount,journal_id,actor_id,actor_name,actor_class,request_hash,result_hash,created_at) VALUES($1,$2,$3,NULLIF($4::bigint,0),$5,$6,$7,$8,$9,$10)`, h.ID, r.Amount, r.Consumed, r.JournalID, actor.ID, actor.Name, actor.Class, r.Request, r.Proof, r.At); e != nil {
		return e
	}
	return incomeAudit(ctx, tx, "income-fundings", h.ID, "release", &actor, map[string]string{"amount": fmt.Sprint(r.Amount), "consumed_amount": fmt.Sprint(r.Consumed)})
}

func (s *Store) IncomeHoldReleaseSnapshot(ctx context.Context, q referralReader, mid int64, trade string, hid int64) (json.RawMessage, error) {
	if q == nil || mid <= 0 || hid <= 0 {
		return nil, ErrInvalid
	}
	h, e := scanIncomeHold(q.QueryRow(ctx, "SELECT "+incomeHoldColumns+" FROM order_income_holds h JOIN orders o ON o.id=h.order_id WHERE h.merchant_id=$1 AND o.trade_no=$2 AND h.id=$3", mid, trade, hid))
	if e != nil {
		return nil, e
	}
	h, f, history, e := s.readIncomeHold(ctx, q, h)
	if e != nil {
		return nil, e
	}
	var pending int64
	if e = q.QueryRow(ctx, "SELECT count(*) FROM refund_fundings f JOIN refunds r ON r.id=f.refund_id WHERE f.hold_id=$1 AND r.status IN('pending','unknown')", hid).Scan(&pending); e != nil {
		return nil, e
	}
	return json.Marshal(map[string]any{"hold": h, "funding_hash": f.Proof, "history_hash": history, "pending_refunds": fmt.Sprint(pending)})
}
func (s *Store) IncomeHoldAdoptionSnapshot(ctx context.Context, q referralReader, mid int64, trade string, hid int64, external string) (json.RawMessage, error) {
	if q == nil || mid <= 0 || hid <= 0 || external == "" || len(external) > 128 || strings.TrimSpace(external) != external || strings.ContainsAny(external, "\r\n\x00") {
		return nil, ErrInvalid
	}
	o, e := scanOrder(q.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE merchant_id=$1 AND trade_no=$2", mid, trade))
	if e != nil {
		return nil, e
	}
	h, e := scanIncomeHold(q.QueryRow(ctx, "SELECT "+incomeHoldColumns+" FROM order_income_holds h JOIN orders o ON o.id=h.order_id WHERE h.id=$1 AND h.order_id=$2", hid, o.ID))
	if e != nil {
		return nil, e
	}
	h, f, history, e := s.readIncomeHold(ctx, q, h)
	if e != nil {
		return nil, e
	}
	if f.HoldID > 0 {
		if f.Mode != "legacy_adopt" || f.ExternalNo != external {
			return nil, ErrConflict
		}
		return json.Marshal(map[string]any{"merchant_id": fmt.Sprint(mid), "order_id": fmt.Sprint(o.ID), "hold_id": fmt.Sprint(hid), "external_no": external, "gross": fmt.Sprint(o.Amount), "merchant_net": fmt.Sprint(o.MerchantAmount), "original_hold": fmt.Sprint(h.Amount), "source_mode": f.Mode, "funding_id": fmt.Sprint(f.HoldID), "proof": f.Proof, "history": history})
	}
	if h.State != "held" {
		return nil, ErrState
	}
	if e = s.verifyOrderSharingReturnEvidence(ctx, q, o.ID); e != nil {
		return nil, e
	}
	if e = s.verifyOrderSharingFinishEvidence(ctx, q, o.ID); e != nil {
		return nil, e
	}
	if e = s.verifyOrderRefundFundingHistory(ctx, q, o.ID); e != nil {
		return nil, e
	}
	var balance, expected, remaining, pending int64
	var valid bool
	var raw []byte
	if e = q.QueryRow(ctx, `SELECT a.balance,(SELECT COALESCE(sum(x.amount),0) FROM order_income_holds x WHERE x.merchant_id=$1 AND x.state='held' AND NOT EXISTS(SELECT 1 FROM order_income_hold_fundings f WHERE f.hold_id=x.id)),v.remaining,v.pending_refunds,v.valid,(SELECT jsonb_build_object('hold',to_jsonb(z),'source',to_jsonb(v),'freeze_journal',(SELECT to_jsonb(j)||jsonb_build_object('entries',(SELECT jsonb_agg(jsonb_build_object('entry_id',e.id::text,'account_id',a.id::text,'owner_id',a.owner_id::text,'kind',a.kind,'scope_id',a.scope_id::text,'amount',e.amount::text) ORDER BY e.id) FROM ledger_entries e JOIN accounts a ON a.id=e.account_id WHERE e.journal_id=j.id)) FROM journals j WHERE j.event_key='order-income:'||z.id||':freeze'),'events',(SELECT jsonb_agg(to_jsonb(ev) ORDER BY ev.id) FROM order_income_hold_events ev WHERE ev.hold_id=z.id),'freeze_audit',(SELECT to_jsonb(b) FROM backoffice_audit b JOIN order_income_hold_events ev ON ev.hold_id=z.id AND ev.action='freeze' AND (b.actor_role,b.actor_id,b.actor_name)=('admin',ev.actor_id,ev.actor_name) WHERE b.resource='orders' AND b.resource_id=z.order_id AND b.operation='freeze-income' AND b.changes->>'hold_id'=z.id::text AND b.changes->>'amount'=z.amount::text ORDER BY b.id LIMIT 1)) FROM order_income_holds z WHERE z.id=$2) FROM accounts a JOIN order_income_source_evidence v ON v.order_id=$3 WHERE(a.owner_id,a.kind,a.scope_id)=($1,'order_income_hold',0)`, mid, hid, o.ID).Scan(&balance, &expected, &remaining, &pending, &valid, &raw); e != nil {
		return nil, e
	}
	if !valid || pending != 0 || remaining != h.Amount || balance != expected {
		return nil, ErrConflict
	}
	return json.Marshal(map[string]any{"merchant_id": fmt.Sprint(mid), "order_id": fmt.Sprint(o.ID), "trade_no": trade, "hold_id": fmt.Sprint(hid), "external_no": external, "gross": fmt.Sprint(o.Amount), "merchant_net": fmt.Sprint(o.MerchantAmount), "platform_fee": fmt.Sprint(o.PlatformFee), "original_hold": fmt.Sprint(h.Amount), "source_mode": "legacy_unprepared", "funding_id": "0", "pool_balance": fmt.Sprint(balance), "pool_expected": fmt.Sprint(expected), "original_evidence": json.RawMessage(raw), "summary": "将此原订单冻结本金从历史混合池迁入独立账户；不会增加冻结金额"})
}
func (s *Store) AdoptOrderIncomeHold(ctx context.Context, mid int64, trade string, hid int64, external string, actor IncomeHoldActor) (OrderIncomeHold, error) {
	var out OrderIncomeHold
	if !actor.valid() || actor.Class != "super" {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	o, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE merchant_id=$1 AND trade_no=$2 FOR UPDATE", mid, trade))
	if e != nil {
		return out, e
	}
	if _, e = s.IncomeHoldAdoptionSnapshot(ctx, tx, mid, trade, hid, external); e != nil {
		return out, e
	}
	h, e := scanIncomeHold(tx.QueryRow(ctx, "SELECT "+incomeHoldColumns+" FROM order_income_holds h JOIN orders o ON o.id=h.order_id WHERE h.id=$1 AND h.order_id=$2 FOR UPDATE OF h", hid, o.ID))
	if e != nil {
		return h, e
	}
	h, f, _, e := s.readIncomeHold(ctx, tx, h)
	if e != nil {
		return h, e
	}
	if f.HoldID > 0 {
		return h, tx.Commit(ctx)
	}
	guard, _ := ctx.Value(fundsActionGuardKey{}).(FundsActionGuard)
	if guard == nil {
		return h, ErrInvalid
	}
	if _, e = s.createIncomeFunding(ctx, tx, o, h, actor, "legacy_adopt", external, func() error {
		// Re-read the exact original proof after any shared-pool account wait;
		// the request-local resolver must fingerprint this final transaction view.
		if _, e := s.IncomeHoldAdoptionSnapshot(ctx, tx, mid, trade, hid, external); e != nil {
			return e
		}
		return guard(ctx, tx, "income-hold-adopt", fmt.Sprint(hid))
	}); e != nil {
		return h, e
	}
	h, _, _, e = s.readIncomeHold(ctx, tx, h)
	if e != nil {
		return h, e
	}
	return h, tx.Commit(ctx)
}
