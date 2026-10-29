package payment

import (
	"context"
	"encoding/json"
	"time"

	provider "goepay/internal/adapters/payment"

	"github.com/jackc/pgx/v5"
)

// autoHandleComplaint mirrors epay_pro BaseComplain::autoHandle, driven by the
// complain_* site settings read BY NAME (ux-settings owns the keys; a missing
// key is off, so this is a no-op until an administrator enables one). It runs
// for a newly-seen OPEN complaint during a sync pull. The keys and the original
// behaviour:
//
//	complain_auto_black=1        blacklist the paying account (buyer) + mobile
//	complain_auto_ipblack=1|2|3  blacklist the order IP for 3|5|7 days
//	complain_freeze_order>=1     freeze the order (unfreeze on resolve)
//	complain_auto_refund=1       refund via GOEPAY's refund flow, capped by
//	                             complain_auto_refund_money
//
// SAFETY DEVIATION FROM THE ORIGINAL, stated deliberately: GOEPAY gates an
// admin refund behind the super-admin + offline-confirmed funds authorization
// (ManualRefund). An unattended sync job MUST NOT forge that authorization, so
// complain_freeze_order and complain_auto_refund are recorded as a pending
// handling INTENT on the complaint (handling.auto_freeze / handling.auto_refund)
// for an administrator to confirm, rather than silently moving funds. The
// blacklist actions, which move no money, are applied directly as the original
// does. This keeps the immutable ledger and the funds-authorization contract.
func (s *Store) autoHandleComplaint(ctx context.Context, channelID int64, rec provider.ComplaintRecord) error {
	if localComplaintState(rec.PayType, rec.Status) != "open" {
		return nil // auto-handle only acts on open complaints (status<2)
	}
	keys, e := s.complaintAutoSettings(ctx)
	if e != nil {
		return e
	}
	if len(keys) == 0 {
		return nil // feature off / no keys registered
	}
	// Resolve the linked order once (buyer/ip come from order_reporting; the
	// complainant's phone comes from the provider record).
	var buyer, ip string
	mobile := rec.Phone
	if len(rec.TradeNos) > 0 {
		row := s.pool.QueryRow(ctx, `SELECT COALESCE(b.buyer,''),COALESCE(b.client_ip,'')
 FROM orders o LEFT JOIN order_reporting b ON b.order_id=o.id
 WHERE o.trade_no=ANY($1) OR o.out_trade_no=ANY($1) LIMIT 1`, rec.TradeNos)
		if e := row.Scan(&buyer, &ip); e != nil && e != pgx.ErrNoRows {
			return e
		}
	}
	if keys["complain_auto_black"] == "1" && buyer != "" {
		if e := s.insertComplaintBlacklist(ctx, "buyer", buyer); e != nil {
			return e
		}
		if mobile != "" {
			if e := s.insertComplaintBlacklist(ctx, "buyer", mobile); e != nil {
				return e
			}
		}
	}
	if ip != "" {
		if days := complaintIPBlackDays(keys["complain_auto_ipblack"]); days > 0 {
			if e := s.insertComplaintIPBlacklist(ctx, ip, days); e != nil {
				return e
			}
		}
	}
	// Record freeze/refund INTENT (see the safety note above) rather than moving
	// funds from an unattended job.
	intent := map[string]any{}
	if n := keys["complain_freeze_order"]; n == "1" || n == "2" {
		intent["auto_freeze"] = true
	}
	if keys["complain_auto_refund"] == "1" {
		intent["auto_refund"] = true
		if cap := keys["complain_auto_refund_money"]; cap != "" {
			intent["auto_refund_cap"] = cap
		}
	}
	if len(intent) > 0 {
		intent["at"] = time.Now().UTC().Format(time.RFC3339)
		raw, _ := json.Marshal(intent)
		if _, e := s.pool.Exec(ctx, `UPDATE complaints133 SET handling=handling||$2::jsonb
 WHERE pay_type=$1 AND third_id=$3`, rec.PayType, string(raw), rec.ThirdID); e != nil {
			// handling edits are forward-only metadata; a race is non-fatal.
			return e
		}
	}
	return nil
}

func (s *Store) complaintAutoSettings(ctx context.Context) (map[string]string, error) {
	keys := []string{"complain_open", "complain_auto_black", "complain_auto_ipblack", "complain_freeze_order", "complain_auto_refund", "complain_auto_refund_money"}
	rows, e := s.pool.Query(ctx, `SELECT body->>'key',body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1) ORDER BY id`, keys)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if e = rows.Scan(&k, &v); e != nil {
			return nil, e
		}
		if _, seen := out[k]; !seen {
			out[k] = v
		}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	// Auto-handle is only active when the feature itself is on.
	if out["complain_open"] != "1" {
		return map[string]string{}, nil
	}
	return out, nil
}

func complaintIPBlackDays(v string) int {
	switch v {
	case "1":
		return 3
	case "2":
		return 5
	case "3":
		return 7
	}
	return 0
}

// insertComplaintBlacklist adds a buyer-account blacklist row if absent, as a
// backoffice 'blacklist' record ({kind,value,reason,enabled}) — the same shape
// the blacklist console and the order admission check use.
func (s *Store) insertComplaintBlacklist(ctx context.Context, kind, value string) error {
	_, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,owner_id,body)
 SELECT 'blacklist',NULL,jsonb_build_object('kind',$1::text,'value',$2::text,'reason','投诉自动拉黑','enabled',true)
 WHERE NOT EXISTS(SELECT 1 FROM backoffice_records WHERE resource='blacklist' AND body->>'kind'=$1 AND body->>'value'=$2)`, kind, value)
	return e
}

func (s *Store) insertComplaintIPBlacklist(ctx context.Context, ip string, days int) error {
	_, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,owner_id,body)
 SELECT 'blacklist',NULL,jsonb_build_object('kind','ip','value',$1::text,'reason','投诉自动拉黑','enabled',true,'expires_at',$2::text)
 WHERE NOT EXISTS(SELECT 1 FROM backoffice_records WHERE resource='blacklist' AND body->>'kind'='ip' AND body->>'value'=$1)`,
		ip, time.Now().UTC().AddDate(0, 0, days).Format(time.RFC3339))
	return e
}
