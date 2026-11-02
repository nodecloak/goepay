package payment

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5"
)

type parentSuccessEvent struct {
	OrderID, Channel, MerchantID, RealMoney, Revision int64
	TradeNo, Day, ProviderTradeNo                     string
	ObservedAt                                        time.Time
	Previous, Proof                                   []byte
}
type parentSuccessDay struct {
	Channel, Revision, Count, LastOrder int64
	Day, Money                          string
	LastHash, Proof                     []byte
}

func (s *Store) parentSuccessProof(v parentSuccessEvent) ([]byte, error) {
	return s.referralDigest("channel168c/success", []any{v.OrderID, v.Channel, v.MerchantID, v.TradeNo, v.Day, v.RealMoney, v.ObservedAt.UTC().Format(time.RFC3339Nano), v.ProviderTradeNo, v.Revision, v.Previous})
}
func (s *Store) parentSuccessDayProof(v parentSuccessDay) ([]byte, error) {
	return s.referralDigest("channel168c/day", []any{v.Channel, v.Day, v.Revision, v.Count, v.Money, v.LastOrder, v.LastHash})
}
func (s *Store) readParentSuccessDay(ctx context.Context, tx pgx.Tx, channel int64, day string) (parentSuccessDay, bool, error) {
	head := parentSuccessDay{Channel: channel, Day: day, Money: "0"}
	e := tx.QueryRow(ctx, `SELECT revision,success_count,realmoney::text,last_order_id,last_hash,proof_hash FROM runtime_channel_success_days WHERE channel_id=$1 AND day=$2::date FOR SHARE`, channel, day).Scan(&head.Revision, &head.Count, &head.Money, &head.LastOrder, &head.LastHash, &head.Proof)
	found := true
	if errors.Is(e, pgx.ErrNoRows) {
		found = false
	} else if e != nil {
		return head, false, e
	}
	// Authenticate the complete ordered event set before using its order IDs
	// to exclude historical ledger sources. A valid head and aggregate alone
	// cannot detect a non-last event reassigned to another baseline order.
	rows, e := tx.Query(ctx, `SELECT order_id,channel_id,merchant_id,trade_no,day::text,realmoney,observed_at,provider_trade_no,revision,previous_hash,proof_hash FROM runtime_channel_success_events WHERE channel_id=$1 AND day=$2::date ORDER BY revision`, channel, day)
	if e != nil {
		return head, false, e
	}
	defer rows.Close()
	var count, audits, lastOrder int64
	var lastHash []byte
	sum := new(big.Int)
	for rows.Next() {
		var event parentSuccessEvent
		if e = rows.Scan(&event.OrderID, &event.Channel, &event.MerchantID, &event.TradeNo, &event.Day, &event.RealMoney, &event.ObservedAt, &event.ProviderTradeNo, &event.Revision, &event.Previous, &event.Proof); e != nil {
			return head, false, e
		}
		proof, e := s.parentSuccessProof(event)
		if e != nil {
			return head, false, e
		}
		if event.Revision != count+1 || event.RealMoney <= 0 || !hmac.Equal(event.Previous, lastHash) || !hmac.Equal(proof, event.Proof) {
			return head, false, ErrConflict
		}
		count++
		sum.Add(sum, big.NewInt(event.RealMoney))
		lastOrder, lastHash = event.OrderID, event.Proof
	}
	if e = rows.Err(); e != nil {
		return head, false, e
	}
	rows.Close()
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='runtime-channel-success' AND operation='runtime-channel-success' AND changes->>'channel_id'=$1::bigint::text AND changes->>'day'=$2`, channel, day).Scan(&audits); e != nil {
		return head, false, e
	}
	if !found {
		if count != 0 || audits != 0 {
			return head, false, ErrConflict
		}
		return head, false, nil
	}
	proof, e := s.parentSuccessDayProof(head)
	if e != nil {
		return head, false, e
	}
	if head.Count <= 0 || head.Revision != head.Count || head.Count != count || audits != count || head.Money != sum.String() || !hmac.Equal(proof, head.Proof) {
		return head, false, ErrConflict
	}
	if lastOrder != head.LastOrder || !hmac.Equal(lastHash, head.LastHash) {
		return head, false, ErrConflict
	}
	return head, true, nil
}

// Existing success remains a read-only baseline, never a fabricated 99 event.
// The original amount here is the original upstream payable gross, corroborated
// by its immutable net+fee payment journals and original provider attempt.
func (s *Store) globalSuccessfulTotal(ctx context.Context, tx pgx.Tx, channel int64, day string, start, end time.Time) (int64, *big.Int, error) {
	head, _, e := s.readParentSuccessDay(ctx, tx, channel, day)
	if e != nil {
		return 0, nil, e
	}
	var baselineCount int64
	var baselineMoney string
	e = tx.QueryRow(ctx, `SELECT count(*),COALESCE(sum(source.gross),0)::text FROM (
 SELECT o.id,sum(credited.amount) AS gross FROM orders o JOIN provider_attempts a ON a.order_id=o.id
 JOIN journals j ON j.event_key IN ('payment:'||o.trade_no,'payment-fee:'||o.trade_no)
 JOIN ledger_entries credited ON credited.journal_id=j.id AND credited.amount>0
 WHERE a.channel_id=$1 AND o.status='paid' AND o.paid_at >= $2 AND o.paid_at < $3
 AND NOT EXISTS(SELECT 1 FROM runtime_channel_success_events e WHERE e.order_id=o.id)
 GROUP BY o.id) source`, channel, start, end).Scan(&baselineCount, &baselineMoney)
	if e != nil {
		return 0, nil, e
	}
	var missing int64
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM orders o JOIN provider_attempts a ON a.order_id=o.id JOIN provider_route_fee_quotes q ON q.merchant_id=o.merchant_id AND q.out_trade_no=o.out_trade_no WHERE a.channel_id=$1 AND o.status='paid' AND o.paid_at >= $2 AND o.paid_at < $3 AND q.facts ? 'GlobalLimits' AND NOT EXISTS(SELECT 1 FROM runtime_channel_success_events e WHERE e.order_id=o.id)`, channel, start, end).Scan(&missing); e != nil {
		return 0, nil, e
	}
	if missing != 0 {
		return 0, nil, ErrConflict
	}
	total, ok := new(big.Int).SetString(head.Money, 10)
	if !ok || total.Sign() < 0 {
		return 0, nil, ErrConflict
	}
	old, ok := new(big.Int).SetString(baselineMoney, 10)
	if !ok || old.Sign() < 0 {
		return 0, nil, ErrConflict
	}
	total.Add(total, old)
	return head.Count + baselineCount, total, nil
}

func (s *Store) lockParentSuccessfulPayment(ctx context.Context, tx pgx.Tx, o Order, receipts []PaidReceipt) (bool, error) {
	ready, e := s.channelLimitSchema(ctx, tx)
	if e != nil || !ready {
		return ready, e
	}
	var channel int64
	if len(receipts) == 1 {
		channel = receipts[0].ChannelID
	} else {
		e = tx.QueryRow(ctx, `SELECT COALESCE((SELECT channel_id FROM provider_attempts WHERE order_id=$1),0)`, o.ID).Scan(&channel)
		if e != nil {
			return false, e
		}
	}
	if channel > 0 {
		_, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/route/channel/%d", channel))
	}
	return true, e
}

func (s *Store) recordParentSuccessfulPayment(ctx context.Context, tx pgx.Tx, o Order, receipts []PaidReceipt, fresh bool) error {
	var original parentSuccessEvent
	e := tx.QueryRow(ctx, `SELECT order_id,channel_id,merchant_id,trade_no,day::text,realmoney,observed_at,provider_trade_no,revision,previous_hash,proof_hash FROM runtime_channel_success_events WHERE order_id=$1`, o.ID).Scan(&original.OrderID, &original.Channel, &original.MerchantID, &original.TradeNo, &original.Day, &original.RealMoney, &original.ObservedAt, &original.ProviderTradeNo, &original.Revision, &original.Previous, &original.Proof)
	if e == nil {
		proof, e := s.parentSuccessProof(original)
		if e != nil {
			return e
		}
		if !hmac.Equal(proof, original.Proof) || original.MerchantID != o.MerchantID || original.TradeNo != o.TradeNo {
			return ErrConflict
		}
		if len(receipts) == 1 && (receipts[0].ChannelID != original.Channel || receipts[0].ProviderTradeNo != original.ProviderTradeNo || receipts[0].RealMoney > 0 && receipts[0].RealMoney != original.RealMoney) {
			return ErrConflict
		}
		_, _, e = s.readParentSuccessDay(ctx, tx, original.Channel, original.Day)
		return e
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return e
	}
	var originated, required bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-channel-success' AND resource_id=$1),EXISTS(SELECT 1 FROM provider_route_fee_quotes WHERE merchant_id=$2 AND out_trade_no=$3 AND facts ? 'GlobalLimits')`, o.ID, o.MerchantID, o.OutTradeNo).Scan(&originated, &required); e != nil {
		return e
	}
	if originated || !fresh && required {
		return ErrConflict
	}
	if !fresh {
		return nil
	}
	if !required {
		var configured bool
		var channel int64
		if len(receipts) == 1 {
			channel = receipts[0].ChannelID
		}
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_channel_limits WHERE channel_id=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-channel-limits' AND resource_id=$1)`, channel).Scan(&configured); e != nil {
			return e
		}
		if !configured {
			return nil
		}
	}
	if len(receipts) != 1 || receipts[0].RealMoney <= 0 {
		if required {
			return ErrState
		}
		return nil
	}
	r := receipts[0]
	// Every caller already authenticated this exact actual provider gross. Keep
	// the upstream expected payable binding; merchant net is never substituted.
	charge104, _, e := s.authenticatedOrderCharge104(ctx, tx, o)
	if e != nil {
		return e
	}
	if r.RealMoney != charge104 {
		return ErrConflict
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	day, _, _ := globalDay(now)
	head, _, e := s.readParentSuccessDay(ctx, tx, r.ChannelID, day)
	if e != nil {
		return e
	}
	next := parentSuccessEvent{OrderID: o.ID, Channel: r.ChannelID, MerchantID: o.MerchantID, TradeNo: o.TradeNo, Day: day, RealMoney: r.RealMoney, ObservedAt: now, ProviderTradeNo: r.ProviderTradeNo, Revision: head.Revision + 1, Previous: head.LastHash}
	next.Proof, e = s.parentSuccessProof(next)
	if e != nil {
		return e
	}
	var noticesReady bool
	if e = tx.QueryRow(ctx, `SELECT to_regclass('notification_sources231') IS NOT NULL`).Scan(&noticesReady); e != nil {
		return e
	}
	extraColumn, extraValue := "", ""
	if noticesReady {
		extraColumn = ",notification_transaction231"
		extraValue = ",pg_current_xact_id()"
	}
	if _, e = tx.Exec(ctx, `INSERT INTO runtime_channel_success_events(order_id,channel_id,merchant_id,trade_no,day,realmoney,observed_at,provider_trade_no,revision,previous_hash,proof_hash`+extraColumn+`) VALUES($1,$2,$3,$4,$5::date,$6,$7,$8,$9,$10,$11`+extraValue+`)`, o.ID, r.ChannelID, o.MerchantID, o.TradeNo, day, r.RealMoney, now, r.ProviderTradeNo, next.Revision, next.Previous, next.Proof); e != nil {
		return e
	}
	money, ok := new(big.Int).SetString(head.Money, 10)
	if !ok {
		return ErrConflict
	}
	money.Add(money, big.NewInt(r.RealMoney))
	head.Revision++
	head.Count++
	head.Money = money.String()
	head.LastOrder = o.ID
	head.LastHash = next.Proof
	head.Proof, e = s.parentSuccessDayProof(head)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO runtime_channel_success_days(channel_id,day,revision,success_count,realmoney,last_order_id,last_hash,proof_hash) VALUES($1,$2::date,$3,$4,$5::numeric,$6,$7,$8) ON CONFLICT(channel_id,day) DO UPDATE SET revision=EXCLUDED.revision,success_count=EXCLUDED.success_count,realmoney=EXCLUDED.realmoney,last_order_id=EXCLUDED.last_order_id,last_hash=EXCLUDED.last_hash,proof_hash=EXCLUDED.proof_hash`, r.ChannelID, day, head.Revision, head.Count, head.Money, o.ID, head.LastHash, head.Proof); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'payment-engine','runtime-channel-success',$1,'runtime-channel-success',jsonb_build_object('channel_id',$2::bigint::text,'day',$3::text,'realmoney',$4::bigint::text,'revision',$5::bigint::text))`, o.ID, r.ChannelID, day, r.RealMoney, head.Revision)
	if e == nil && noticesReady {
		s.recordChannelLimitNotice231(ctx, tx, next)
	}
	return e
}
