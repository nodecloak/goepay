package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"

	"goepay/internal/riskpolicy"
)

type coreRiskSuccess struct {
	OrderID, Amount, ChannelID int64
	OriginHash                 []byte
	ProviderTradeNo, Day       string
	BuyerKey                   string
	BuyerKeys                  []string
	ObservedAt, PaidAt         time.Time
}

func (s *Store) coreRiskSuccessProof(f coreRiskSuccess) ([]byte, error) {
	return s.referralDigest("risk102/success", f)
}
func (s *Store) readCoreRiskSuccess(ctx context.Context, q referralReader, id int64, origin []byte) (*coreRiskSuccess, error) {
	var raw, proof []byte
	var day string
	e := q.QueryRow(ctx, `SELECT day::text,facts,proof_hash FROM core_risk_successes WHERE order_id=$1`, id).Scan(&day, &raw, &proof)
	if e == pgx.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	f, e := s.decodeCoreRiskSuccessFact(raw, proof, origin, id, day)
	if e != nil {
		return nil, e
	}
	var original bool
	if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='core-risk-success' AND resource_id=$1 AND operation='confirm' AND actor_role='system' AND actor_id=0 AND actor_name='core-risk' AND changes->>'source_hash'=$2 AND changes->>'day'=$3)`, id, hex.EncodeToString(proof), day).Scan(&original); e != nil || !original {
		return nil, ErrConflict
	}
	if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders o JOIN verified_payment_receipts v ON v.order_id=o.id WHERE o.id=$1 AND COALESCE((to_jsonb(o)->>'actual_charge_amount104')::bigint,o.amount)=$2 AND o.paid_at=$3 AND o.status='paid' AND v.channel_id=$4 AND v.provider_trade_no=$5)`, id, f.Amount, f.PaidAt, f.ChannelID, f.ProviderTradeNo).Scan(&original); e != nil || !original {
		return nil, ErrConflict
	}
	o, e := scanOrder(q.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", id))
	if e != nil {
		return nil, e
	}
	charge, e := s.authenticatedChargeValue104(ctx, q, o)
	if e != nil || charge != f.Amount {
		return nil, ErrConflict
	}
	return f, nil
}
func (s *Store) checkCoreRiskIP(ctx context.Context, tx pgx.Tx, settings riskpolicy.Settings, ip string, now time.Time) error {
	limit, interval := riskpolicy.Integer(settings, "pay_iplimit"), riskpolicy.Integer(settings, "pay_ip_interval")
	if limit == 0 && interval == 0 {
		return nil
	}
	ids, e := s.coreRiskCurrentIPCandidatesTx(ctx, tx, ip, now, limit, interval)
	if e != nil {
		return e
	}
	facts, e := s.coreRiskAdmissionFactsTx(ctx, tx, ids)
	if e != nil {
		return e
	}
	_, _, day := coreRiskBusinessDayBounds(now)
	var count int64
	if limit > 0 {
		count, _, e = s.coreRiskLegacyDaily(ctx, tx, ip, "", now)
		if e != nil {
			return e
		}
	}
	for _, id := range ids {
		fact := facts[id]
		origin := fact.Origin
		contains := false
		for _, value := range origin.IPs {
			contains = contains || value == ip
		}
		if !contains {
			return ErrConflict
		}
		if interval > 0 && now.Before(origin.AdmittedAt.Add(time.Duration(interval)*time.Second)) {
			return ErrState
		}
		if fact.Success != nil {
			if fact.Success.Day == day {
				count++
			}
			continue
		}
		if fact.AttemptState == "processing" || fact.AttemptState == "pending" || fact.AttemptState == "unknown" {
			count++
		}
	}
	if limit > 0 && count >= limit {
		return ErrState
	}
	return nil
}
func (s *Store) recordCoreRiskPaid(ctx context.Context, tx pgx.Tx, o Order, receipts []PaidReceipt) {
	// Risk statistics must not roll back authenticated payment/ledger/outbox.
	// Missing required statistics are detected by future new-payment admission.
	if len(receipts) != 1 || receipts[0].RealMoney <= 0 {
		return
	}
	child, e := tx.Begin(ctx)
	if e != nil {
		return
	}
	defer child.Rollback(ctx)
	var present bool
	if e = child.QueryRow(ctx, `SELECT to_regclass('core_risk_origins')IS NOT NULL`).Scan(&present); e != nil || !present {
		return
	}
	var exists bool
	if e = child.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM core_risk_origins WHERE order_id=$1)`, o.ID).Scan(&exists); e != nil || !exists {
		return
	}
	origin, proof, e := s.readCoreRiskOrigin(ctx, child, o.ID)
	if e != nil {
		return
	}
	r := receipts[0]
	charge, e := s.authenticatedChargeValue104(ctx, child, o)
	if e != nil {
		return
	}
	if r.RealMoney != charge || r.ChannelID != origin.ChannelID {
		return
	}
	old, e := s.readCoreRiskSuccess(ctx, child, o.ID, proof)
	if e != nil {
		return
	}
	if old != nil {
		if old.Amount == r.RealMoney && old.ProviderTradeNo == r.ProviderTradeNo {
			_ = child.Commit(ctx)
		}
		return
	}
	var paidAt *time.Time
	if e = child.QueryRow(ctx, `SELECT paid_at FROM orders WHERE id=$1 AND status='paid'`, o.ID).Scan(&paidAt); e != nil || paidAt == nil {
		return
	}
	now, e := coreRiskDBNow(ctx, child)
	if e != nil {
		return
	}
	f := coreRiskSuccess{OrderID: o.ID, Amount: r.RealMoney, ChannelID: r.ChannelID, OriginHash: proof, ProviderTradeNo: r.ProviderTradeNo, Day: paidAt.In(settlementScheduleZone).Format("2006-01-02"), ObservedAt: now, PaidAt: paidAt.UTC()}
	f.BuyerKey, e = s.coreRiskBuyerKey(o.Type, r.BuyerID)
	if e != nil {
		return
	}
	if f.BuyerKey == "" {
		f.BuyerKey = origin.BuyerKey
	}
	f.BuyerKeys = []string{}
	for _, key := range []string{f.BuyerKey, origin.BuyerKey} {
		if key != "" && !coreRiskKeyIn(f.BuyerKeys, key) {
			f.BuyerKeys = append(f.BuyerKeys, key)
		}
	}
	sort.Strings(f.BuyerKeys)
	hash, e := s.coreRiskSuccessProof(f)
	if e != nil {
		return
	}
	raw, e := json.Marshal(f)
	if e != nil {
		return
	}
	if _, e = child.Exec(ctx, `INSERT INTO core_risk_successes(order_id,day,facts,proof_hash)VALUES($1,$2::date,$3::jsonb,$4)`, o.ID, f.Day, string(raw), hash); e != nil {
		return
	}
	keyJSON, _ := json.Marshal(f.BuyerKeys)
	if _, e = child.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'core-risk','core-risk-success',$1,'confirm',jsonb_build_object('source_hash',$2::text,'buyer_key',$3::text,'buyer_keys',$4::jsonb,'day',$5::text))`, o.ID, hex.EncodeToString(hash), f.BuyerKey, string(keyJSON), f.Day); e != nil {
		return
	}
	_ = child.Commit(ctx)
}
func (s *Store) finalCoreRiskClock(ctx context.Context, tx pgx.Tx, f *coreRiskOrigin) error {
	if f == nil {
		return nil
	}
	now, e := coreRiskDBNow(ctx, tx)
	if e != nil {
		return e
	}
	if now.In(settlementScheduleZone).Format("2006-01-02") != f.AdmittedAt.In(settlementScheduleZone).Format("2006-01-02") {
		return fmt.Errorf("%w: risk business day changed", ErrState)
	}
	if f.Group != nil && !f.Group.ValidUntil.IsZero() && !now.Before(f.Group.ValidUntil) {
		return ErrState
	}
	return nil
}

func (s *Store) decodeCoreRiskSuccessFact(raw, proof, origin []byte, id int64, day string) (*coreRiskSuccess, error) {
	var f coreRiskSuccess
	if decodeCoreRiskFacts(raw, &f) != nil || f.OrderID != id || f.Day != day || !hmac.Equal(origin, f.OriginHash) || f.Amount <= 0 {
		return nil, ErrConflict
	}
	if len(f.BuyerKeys) > 2 || f.BuyerKey != "" && !coreRiskKeyIn(f.BuyerKeys, f.BuyerKey) {
		return nil, ErrConflict
	}
	for i, key := range f.BuyerKeys {
		decoded, e := hex.DecodeString(key)
		if e != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != key || i > 0 && f.BuyerKeys[i-1] >= key {
			return nil, ErrConflict
		}
	}
	expected, e := s.coreRiskSuccessProof(f)
	if e != nil || !hmac.Equal(expected, proof) {
		return nil, ErrConflict
	}
	return &f, nil
}
