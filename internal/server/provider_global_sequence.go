package server

import (
	"context"
	"sort"

	"github.com/jackc/pgx/v5"
	"goepay/internal/backoffice"
	"goepay/internal/payment"
)

// The caller holds all parent advisory locks in numeric order. Unseen parents
// start at zero; no historical last-use time is invented during rollout.
func orderGlobalSequence(ctx context.Context, tx pgx.Tx, channels []backoffice.PayRollChannel) error {
	ids := make([]int64, 0, len(channels))
	for _, c := range channels {
		ids = append(ids, c.ID)
	}
	rows, e := tx.Query(ctx, `SELECT h.channel_id,h.last_used_millis,COALESCE(source.channel_id,0),
 COALESCE(floor(extract(epoch FROM source.created_at)*1000)::bigint,-1),
 COALESCE(source.rule_snapshot->'payment_info'->source.pay_type->>'channel',''),COALESCE(latest.millis,-1)
 FROM provider_channel_last_used h
 LEFT JOIN provider_route_assignments source ON source.merchant_id=h.merchant_id AND source.out_trade_no=h.out_trade_no
 LEFT JOIN LATERAL (
  SELECT floor(extract(epoch FROM a.created_at)*1000)::bigint millis
  FROM provider_route_assignments a WHERE a.channel_id=h.channel_id
   AND a.rule_snapshot->'payment_info'->a.pay_type->>'channel'='-4'
  ORDER BY a.created_at DESC LIMIT 1
 ) latest ON true WHERE h.channel_id=ANY($1::bigint[])`, ids)
	if e != nil {
		return e
	}
	defer rows.Close()
	times := map[int64]int64{}
	for rows.Next() {
		var id, millis, sourceChannel, sourceMillis, latest int64
		var mode string
		if e = rows.Scan(&id, &millis, &sourceChannel, &sourceMillis, &mode, &latest); e != nil {
			return e
		}
		// Restoring a complete earlier FK source is still a rollback. Only
		// original -4 facts participate; absent heads remain unseen historical
		// parents at zero, and equal Pro milliseconds remain equivalent.
		if sourceChannel != id || sourceMillis != millis || mode != "-4" || latest != millis {
			return payment.ErrConflict
		}
		times[id] = millis
	}
	if e = rows.Err(); e != nil {
		return e
	}
	sort.Slice(channels, func(i, j int) bool {
		a, b := channels[i].ID, channels[j].ID
		if times[a] != times[b] {
			return times[a] < times[b]
		}
		return a < b
	})
	return nil
}

// The immutable route's database clock is the fact source. Assignment and head
// commit together; any failure, including a deferred commit failure, rolls back
// both. Original replays return before this helper and never advance a parent.
func recordGlobalSequence(ctx context.Context, tx pgx.Tx, mid int64, outTradeNo string, channel int64) error {
	tag, e := tx.Exec(ctx, `INSERT INTO provider_channel_last_used(channel_id,merchant_id,out_trade_no,last_used_millis)
 SELECT channel_id,merchant_id,out_trade_no,floor(extract(epoch FROM created_at)*1000)::bigint
 FROM provider_route_assignments WHERE merchant_id=$1 AND out_trade_no=$2 AND channel_id=$3
 ON CONFLICT(channel_id) DO UPDATE SET merchant_id=EXCLUDED.merchant_id,out_trade_no=EXCLUDED.out_trade_no,last_used_millis=EXCLUDED.last_used_millis`, mid, outTradeNo, channel)
	if e != nil {
		return e
	}
	if tag.RowsAffected() != 1 {
		return payment.ErrState
	}
	return nil
}
