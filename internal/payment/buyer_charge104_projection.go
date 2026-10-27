package payment

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
)

// OriginalOrderCharges104 authenticates a bounded page in one joined data read.
// An absent result is unconfirmed, never permission to substitute requested M.
// The old route/referral encodings are verified before accepting legacy C=M.
func (s *Store) OriginalOrderCharges104(ctx context.Context, mid int64, ids []int64) (map[int64]int64, error) {
	if mid < 0 || len(ids) > 100 {
		return nil, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	facts, e := s.orderChargesTx104(ctx, tx, mid, ids)
	if e != nil {
		return nil, e
	}
	out := map[int64]int64{}
	for id, f := range facts {
		out[id] = f.Charge
	}
	return out, tx.Commit(ctx)
}

// Same-transaction bulk authentication for risk and outgoing-funds consumers.
// This does not lock orders after accounts or issue one query per order.
func (s *Store) orderChargesTx104(ctx context.Context, tx pgx.Tx, mid int64, ids []int64) (map[int64]orderChargeCommitment104, error) {
	out := map[int64]orderChargeCommitment104{}
	if mid < 0 || len(ids) > 100000 {
		return nil, ErrInvalid
	}
	if len(ids) == 0 {
		return out, nil
	}
	ready, e := s.buyerChargeSchema104(ctx, tx)
	if e != nil {
		return nil, e
	}
	sourceCols := `0::bigint,NULL::jsonb,NULL::bytea,0::bigint,''::text`
	sourceJoin := ""
	if ready {
		sourceCols = `COALESCE(charge.merchant_id,0),charge.facts,charge.proof_hash,(SELECT count(*) FROM backoffice_audit WHERE resource='order-customer-charges104' AND resource_id=o.id AND operation='create'),COALESCE((SELECT max(changes->>'commitment') FROM backoffice_audit WHERE resource='order-customer-charges104' AND resource_id=o.id AND operation='create'),'')`
		sourceJoin = ` LEFT JOIN order_customer_charges104 charge ON charge.order_id=o.id `
	}
	rows, e := tx.Query(ctx, `SELECT o.id,o.trade_no,o.merchant_id,o.out_trade_no,o.name,o.type,o.amount,o.notify_url,o.return_url,o.param,o.version,o.merchant_amount,o.platform_fee,o.fee_rate_bps,o.fee_group_id,o.fee_policy,o.refund_fee_payer,o.created_at,
 COALESCE((to_jsonb(o)->>'runtime_fee_quote_required')::boolean,false),(to_jsonb(o)->>'actual_charge_amount104')::bigint,COALESCE((to_jsonb(o)->>'actual_charge_required104')::boolean,false),
 COALESCE((to_jsonb(o)->>'referral_source_version')::int,0),(to_jsonb(o)->>'referral_source_hash')::bytea,
 COALESCE(t.order_id,0),COALESCE(t.merchant_id,0),COALESCE(t.relation_id,0),COALESCE(t.promoter_id,0),COALESCE(t.purpose,''),COALESCE(t.enabled,false),COALESCE(t.reason,''),COALESCE(t.rate_bps,0),COALESCE(t.platform_fee,0),COALESCE(t.reward_amount,0),COALESCE(t.refund_policy,''),COALESCE(t.policy_versions,'[]'::jsonb),t.proof_hash,cost.proof_hash,
 q.facts,q.proof_hash,terms.proof_hash,COALESCE(origin.channel_id,0),COALESCE(origin.method,''),COALESCE(origin.pay_type,''),COALESCE(origin.amount,0),origin.request_hash,`+sourceCols+`
 FROM orders o LEFT JOIN referral_order_terms t ON t.order_id=o.id LEFT JOIN referral_profit_costs cost ON cost.order_id=o.id
 LEFT JOIN provider_route_fee_quotes q ON q.merchant_id=o.merchant_id AND q.out_trade_no=o.out_trade_no LEFT JOIN order_route_fee_terms terms ON terms.order_id=o.id
 LEFT JOIN provider_route_assignments origin ON origin.merchant_id=o.merchant_id AND origin.out_trade_no=o.out_trade_no `+sourceJoin+`
 WHERE ($1::bigint=0 OR o.merchant_id=$1) AND o.id=ANY($2::bigint[])`, mid, ids)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var o Order
		var t referralOrderTerms
		var sourceVersion int
		var sourceHash, versions, costProof, rawQuote, quoteProof, termsProof, originHash []byte
		var originParent, originAmount int64
		var originMethod, originType string
		var chargeMid, audits int64
		var rawCharge, chargeProof []byte
		var commitment string
		if e = rows.Scan(&o.ID, &o.TradeNo, &o.MerchantID, &o.OutTradeNo, &o.Name, &o.Type, &o.Amount, &o.NotifyURL, &o.ReturnURL, &o.Param, &o.Version, &o.MerchantAmount, &o.PlatformFee, &o.FeeRateBPS, &o.FeeGroupID, &o.FeePolicy, &o.RefundFeePayer, &o.CreatedAt, &o.routeFeeRequired, &o.actualChargeAmount104, &o.actualChargeRequired104,
			&sourceVersion, &sourceHash, &t.OrderID, &t.MerchantID, &t.RelationID, &t.PromoterID, &t.Purpose, &t.Enabled, &t.Reason, &t.RateBPS, &t.PlatformFee, &t.RewardAmount, &t.RefundPolicy, &versions, &t.ProofHash, &costProof,
			&rawQuote, &quoteProof, &termsProof, &originParent, &originMethod, &originType, &originAmount, &originHash, &chargeMid, &rawCharge, &chargeProof, &audits, &commitment); e != nil {
			return nil, e
		}
		if !s.projectedOrderBirthValid(o, t, versions, sourceVersion, sourceHash, costProof) {
			continue
		}
		if o.routeFeeRequired {
			var f routeFeeFacts
			if json.Unmarshal(rawQuote, &f) != nil {
				continue
			}
			q := routeFeeQuote{f, quoteProof}
			if s.authenticateRouteFeeFacts(q, o.MerchantID, o.OutTradeNo, originParent, originMethod, originType, originAmount, originHash) != nil || s.authenticateOrderRouteFeeBinding(o, termsProof, q) != nil {
				continue
			}
		} else if len(termsProof) != 0 || len(quoteProof) != 0 {
			continue
		}
		charge, sourceProof, e := s.authenticateBuyerSourceRow104(o, chargeMid, rawCharge, chargeProof, audits, commitment, quoteProof)
		if e != nil {
			continue
		}
		out[o.ID] = orderChargeCommitment104{Charge: charge, SourceProof: sourceProof}
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	return out, nil
}
