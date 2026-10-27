package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

// OriginalOrderCapabilities resolves a bounded page with one owner-bound joined
// read. Offline factories restore immutable account versions; no upstream calls,
// order writes, current child heads or per-order database reads are involved.
func (g ProviderFlow) OriginalOrderCapabilities(ctx context.Context, mid int64, ids []int64) (map[int64]adapter.Capabilities, error) {
	out := map[int64]adapter.Capabilities{}
	if g.Store == nil || mid <= 0 || len(ids) > 100 {
		return out, ErrInvalid
	}
	if len(ids) == 0 {
		return out, nil
	}
	tx, e := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	schema, e := g.Store.childSchemaPresent(ctx, tx, mid, "")
	if e != nil {
		return nil, e
	}
	feeSchema := true
	if !schema {
		feeSchema, e = g.Store.routeFeeSchemaForProjection(ctx, tx)
		if e != nil {
			return nil, e
		}
	}
	if !schema && !feeSchema {
		rows, e := tx.Query(ctx, `SELECT o.id,o.type,a.channel_id FROM orders o JOIN provider_attempts a ON a.order_id=o.id WHERE o.merchant_id=$1 AND o.id=ANY($2::bigint[])`, mid, ids)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		for rows.Next() {
			var id, parent int64
			var kind string
			if e = rows.Scan(&id, &kind, &parent); e != nil {
				return nil, e
			}
			if p := g.Providers[parent]; p != nil && p.ID() == kind {
				out[id] = p.Capabilities()
			}
		}
		if e = rows.Err(); e != nil {
			return nil, e
		}
		rows.Close()
		return out, tx.Commit(ctx)
	}
	chargeReady104, e := g.Store.buyerChargeSchema104(ctx, tx)
	if e != nil {
		return nil, e
	}
	chargeColumns104 := `0::bigint,NULL::jsonb,NULL::bytea,0::bigint,''::text`
	chargeJoin104 := ""
	if chargeReady104 {
		chargeColumns104 = `COALESCE(charge.merchant_id,0),charge.facts,charge.proof_hash,(SELECT count(*) FROM backoffice_audit WHERE resource='order-customer-charges104' AND resource_id=o.id AND operation='create'),COALESCE((SELECT max(changes->>'commitment') FROM backoffice_audit WHERE resource='order-customer-charges104' AND resource_id=o.id AND operation='create'),'')`
		chargeJoin104 = ` LEFT JOIN order_customer_charges104 charge ON charge.order_id=o.id `
	}
	globalLimitSchema, e := g.Store.channelLimitSchema(ctx, tx)
	if e != nil {
		return nil, e
	}
	childColumns := `COALESCE(r.subchannel_id,0),COALESCE(r.account_version,0),COALESCE(r.account_proof,''::bytea),COALESCE(r.execution_identity,''::bytea),COALESCE(r.account_hash,''::bytea),COALESCE(r.product,''),COALESCE(r.proof_hash,''::bytea),COALESCE(r.selected_at,'epoch'::timestamptz),COALESCE(v.merchant_id,0),COALESCE(v.parent_id,0),COALESCE(v.pay_type,''),COALESCE(v.schema_hash,''),COALESCE(v.nonce,''::bytea),COALESCE(v.ciphertext,''::bytea),COALESCE(v.proof_hash,''::bytea)`
	childJoins := ` LEFT JOIN payment_subchannel_routes r ON r.merchant_id=o.merchant_id AND r.out_trade_no=o.out_trade_no LEFT JOIN payment_subchannel_accounts v ON v.subchannel_id=r.subchannel_id AND v.version=r.account_version `
	if !schema {
		childColumns = `0::bigint,0::bigint,''::bytea,''::bytea,''::bytea,''::text,''::bytea,'epoch'::timestamptz,0::bigint,0::bigint,''::text,''::text,''::bytea,''::bytea,''::bytea`
		childJoins = ""
	}
	rows, e := tx.Query(ctx, `SELECT o.id,o.out_trade_no,o.type,a.channel_id,`+childColumns+`,COALESCE(q.facts,'{}'::jsonb),COALESCE(q.proof_hash,''::bytea),
 o.trade_no,o.name,o.amount,o.notify_url,o.return_url,o.param,o.version,o.merchant_amount,o.platform_fee,o.fee_rate_bps,o.fee_group_id,o.fee_policy,o.refund_fee_payer,COALESCE((to_jsonb(o)->>'runtime_fee_quote_required')::boolean,false),(to_jsonb(o)->>'actual_charge_amount104')::bigint,COALESCE((to_jsonb(o)->>'actual_charge_required104')::boolean,false),o.created_at,COALESCE((to_jsonb(o)->>'referral_source_version')::int,0),(to_jsonb(o)->>'referral_source_hash')::bytea,
 COALESCE(t.order_id,0),COALESCE(t.merchant_id,0),COALESCE(t.relation_id,0),COALESCE(t.promoter_id,0),COALESCE(t.purpose,''),COALESCE(t.enabled,false),COALESCE(t.reason,''),COALESCE(t.rate_bps,0),COALESCE(t.platform_fee,0),COALESCE(t.reward_amount,0),COALESCE(t.refund_policy,''),COALESCE(t.policy_versions,'[]'::jsonb),t.proof_hash,cost.proof_hash,terms.proof_hash,
 COALESCE(origin.channel_id,0),COALESCE(origin.method,''),COALESCE(origin.pay_type,''),COALESCE(origin.amount,0),origin.request_hash,`+chargeColumns104+`
 FROM orders o JOIN provider_attempts a ON a.order_id=o.id
 `+childJoins+`
 LEFT JOIN provider_route_fee_quotes q ON q.merchant_id=o.merchant_id AND q.out_trade_no=o.out_trade_no
 LEFT JOIN referral_order_terms t ON t.order_id=o.id
 LEFT JOIN referral_profit_costs cost ON cost.order_id=o.id
 LEFT JOIN order_route_fee_terms terms ON terms.order_id=o.id
 LEFT JOIN provider_route_assignments origin ON origin.merchant_id=o.merchant_id AND origin.out_trade_no=o.out_trade_no
 `+chargeJoin104+` WHERE o.merchant_id=$1 AND o.id=ANY($2::bigint[])`, mid, ids)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var id, parent int64
		var no, kind string
		var b ChildExecutionBinding
		var a subchannelAccount
		var raw, quoteProof []byte
		var o Order
		var t referralOrderTerms
		var sourceVersion int
		var sourceHash, policyVersions, costProof, termsProof, originHash []byte
		var originParent, originAmount int64
		var originMethod, originKind string
		var chargeMerchant104, chargeAudits104 int64
		var rawCharge104, chargeProof104 []byte
		var chargeCommitment104 string
		if e = rows.Scan(&id, &no, &kind, &parent, &b.child, &b.version, &b.accountProof, &b.identity, &b.accountHash, &b.product, &b.proof, &b.selectedAt, &a.Merchant, &a.Parent, &a.Kind, &a.Schema, &a.Nonce, &a.Ciphertext, &a.Proof, &raw, &quoteProof,
			&o.TradeNo, &o.Name, &o.Amount, &o.NotifyURL, &o.ReturnURL, &o.Param, &o.Version, &o.MerchantAmount, &o.PlatformFee, &o.FeeRateBPS, &o.FeeGroupID, &o.FeePolicy, &o.RefundFeePayer, &o.routeFeeRequired, &o.actualChargeAmount104, &o.actualChargeRequired104, &o.CreatedAt, &sourceVersion, &sourceHash,
			&t.OrderID, &t.MerchantID, &t.RelationID, &t.PromoterID, &t.Purpose, &t.Enabled, &t.Reason, &t.RateBPS, &t.PlatformFee, &t.RewardAmount, &t.RefundPolicy, &policyVersions, &t.ProofHash, &costProof, &termsProof, &originParent, &originMethod, &originKind, &originAmount, &originHash, &chargeMerchant104, &rawCharge104, &chargeProof104, &chargeAudits104, &chargeCommitment104); e != nil {
			return nil, e
		}
		o.ID = id
		o.MerchantID = mid
		o.OutTradeNo = no
		o.Type = kind
		if !g.Store.projectedOrderBirthValid(o, t, policyVersions, sourceVersion, sourceHash, costProof) {
			continue
		}
		var facts routeFeeFacts
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&facts) != nil {
			continue
		}
		quote := routeFeeQuote{facts: facts, proof: quoteProof}
		if facts.GlobalLimits != nil && !globalLimitSchema {
			continue
		}
		if o.routeFeeRequired {
			if g.Store.authenticateRouteFeeFacts(quote, mid, no, originParent, originMethod, originKind, originAmount, originHash) != nil || facts.ChannelID != parent || g.Store.authenticateOrderRouteFeeBinding(o, termsProof, quote) != nil {
				continue
			}
		} else if facts.Version != 0 || b.child != 0 || len(termsProof) != 0 {
			continue
		}
		if _, _, err := g.Store.authenticateBuyerSourceRow104(o, chargeMerchant104, rawCharge104, chargeProof104, chargeAudits104, chargeCommitment104, quoteProof); err != nil {
			continue
		}
		if b.child == 0 {
			if facts.Child != nil {
				continue
			}
			if p := g.Providers[parent]; p != nil && p.ID() == kind {
				if o.routeFeeRequired {
					actual, e := g.RuntimeChannel(parent)
					if e != nil || !hmac.Equal(actual.identity, facts.ParentIdentity) || !hmac.Equal(actual.accountHash, facts.AccountHash) || actual.product != facts.ParentProduct {
						continue
					}
				}
				out[id] = p.Capabilities()
			}
			continue
		}
		b.merchant = mid
		b.parent = parent
		a.Child = b.child
		a.Version = b.version
		proof, err := g.Store.childRouteProof(mid, no, b)
		ap, ae := g.Store.accountProof(a)
		qp, qe := g.Store.routeFeeProof(facts)
		if err != nil || ae != nil || qe != nil || facts.Version != 2 || facts.MerchantID != mid || facts.OutTradeNo != no || facts.ChannelID != parent || facts.PayType != kind || !hmac.Equal(qp, quoteProof) || !hmac.Equal(proof, b.proof) || !hmac.Equal(ap, a.Proof) || !hmac.Equal(a.Proof, b.accountProof) || a.Merchant != mid || a.Parent != parent || a.Kind != kind || !childCommitmentEqual(facts.Child, b.commitment()) {
			continue
		}
		c := PaymentSubchannel{ID: b.child, MerchantID: mid, ParentID: parent, PayType: kind, AccountVersion: b.version, account: a}
		flow, execution, err := g.withSubchannelAccountSource(ctx, tx, c, true)
		if err != nil {
			continue
		}
		actual := flow.boundParents[parent].child
		if actual.product != b.product || !hmac.Equal(actual.identity, b.identity) || !hmac.Equal(actual.accountHash, b.accountHash) {
			continue
		}
		out[id] = execution.Payment.Capabilities()
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close()
	return out, tx.Commit(ctx)
}
