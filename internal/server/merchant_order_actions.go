package server

import (
	"context"
	"strconv"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

// One owner-bound metadata read for the entire page. No provider calls, writes,
// per-row detail requests, or private origin fields enter the public projection.
func (s *Server) merchantOrderActions(ctx context.Context, session identity.Session, items []map[string]any) error {
	ids := make([]int64, 0, len(items))
	byID := map[int64]map[string]any{}
	for _, item := range items {
		raw, ok := item["id"].(string)
		if !ok {
			return payment.ErrInvalid
		}
		id, e := strconv.ParseInt(raw, 10, 64)
		if e != nil || id <= 0 {
			return payment.ErrInvalid
		}
		ids = append(ids, id)
		byID[id] = item
		item["merchant_actions"] = map[string]bool{"query": false, "notify": false, "refund": false, "print": false}
	}
	if len(ids) == 0 {
		return nil
	}
	if e := s.consoleOrderCharges104(ctx, session.User.ID, items); e != nil {
		return e
	}
	originalCaps := map[int64]struct{ Query, Refund bool }{}
	if s.ProviderFlow != nil {
		resolved, e := s.ProviderFlow.OriginalOrderCapabilities(ctx, session.User.ID, ids)
		if e != nil {
			return e
		}
		for id, c := range resolved {
			originalCaps[id] = struct{ Query, Refund bool }{c.Query, c.Refund}
		}
	}
	refundAllowed := false
	if s.Store != nil {
		platform, e := s.Store.PublicMerchantAccess(ctx)
		if e != nil {
			return e
		}
		refundAllowed = platform.UserRefund == 1
	}
	rows, e := s.Pool.Query(ctx, `SELECT o.id,o.merchant_id,o.type,o.status,o.notify_url,o.amount,
 COALESCE((SELECT sum(r.amount) FROM refunds r WHERE r.order_id=o.id AND r.status='succeeded'),0),
 COALESCE(a.channel_id,0),COALESCE(z.kind,''),COALESCE(z.collection_code_id,0),z.proof_hash,
 EXISTS(SELECT 1 FROM order_income_holds h WHERE h.order_id=o.id AND h.state='held'),
 EXISTS(SELECT 1 FROM order_income_holds h JOIN order_income_hold_fundings f ON f.hold_id=h.id AND f.order_id=o.id AND f.merchant_id=o.merchant_id WHERE h.order_id=o.id AND h.state='held' AND f.source_mode IN ('fresh','legacy_adopt')),
 EXISTS(SELECT 1 FROM refunds r WHERE r.order_id=o.id AND r.status IN ('pending','unknown'))
 FROM orders o LEFT JOIN provider_attempts a ON a.order_id=o.id LEFT JOIN order_print_origins z ON z.order_id=o.id
 WHERE o.merchant_id=$1 AND o.id=ANY($2::bigint[])`, session.User.ID, ids)
	if e != nil {
		return e
	}
	defer rows.Close()
	for rows.Next() {
		var id, mid, amount, refunded, channel, collection int64
		var kind, state, notify, origin string
		var proof []byte
		var held, funded, pending bool
		if e = rows.Scan(&id, &mid, &kind, &state, &notify, &amount, &refunded, &channel, &origin, &collection, &proof, &held, &funded, &pending); e != nil {
			return e
		}
		item := byID[id]
		if item == nil {
			continue
		}
		own := session.Issuer == nil
		actual, chargeError := strconv.ParseInt(buyerText104(item["realmoney"]), 10, 64)
		chargeKnown := item["charge_verified104"] == true && chargeError == nil && actual > 0
		caps := map[string]bool{"query": false, "notify": false, "refund": false, "print": own && state == "paid" && s.Printing != nil}
		if s.Store != nil {
			caps["notify"] = own && state == "paid" && notify != "" && s.Store.VerifiedOrdinaryOrderOrigin(id, mid, collection, origin, proof)
		}
		if s.ProviderFlow != nil {
			if capability, ok := originalCaps[id]; ok {
				caps["query"] = chargeKnown && own && state == "pending" && capability.Query
				caps["refund"] = chargeKnown && refundAllowed && own && state == "paid" && refunded < actual && !pending && (!held || funded) && s.ProviderFlow.AllowLive && capability.Refund
			}
		}
		item["merchant_actions"] = caps
		if _, exists := item["income_status"]; !exists {
			if held {
				item["income_status"] = "held"
			} else {
				item["income_status"] = "none"
			}
		}
		item["can_refund_held_income"] = held && funded && caps["refund"]
	}
	return rows.Err()
}
