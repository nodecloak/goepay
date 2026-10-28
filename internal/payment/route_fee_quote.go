package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/grouppolicy"
)

type routeFeeCost struct {
	Provider, AppID, AccountScope, Revision, Method string
	RateUnits                                       int64
}
type routeFeeFacts struct {
	CustomerCharge104                                                      *buyerChargeQuote104  `json:",omitempty"`
	Child                                                                  *routeChildCommitment `json:"child_execution,omitempty"`
	MerchantRevision                                                       int64
	GroupUpdatedAt                                                         time.Time
	Version                                                                int
	MerchantID, ChannelID, Amount, GroupID, ParentRevision, ParentRetained int64
	OutTradeNo, Name, PayType, Method, NotifyURL, ReturnURL, Param         string
	GroupSnapshot                                                          json.RawMessage
	GroupHash, ParentIdentity, AccountHash, RouteHash                      []byte
	ParentConfigured                                                       bool
	ParentProduct                                                          string
	GroupValidUntil                                                        time.Time
	QuotedAt                                                               time.Time
	FeePolicy, RefundPayer, Source                                         string
	FeeRate, FeeGroupID, Net, Fee                                          int64
	Cost                                                                   *routeFeeCost
	GlobalLimits                                                           *parentLimitFacts `json:",omitempty"`
}
type routeFeeQuote struct {
	facts routeFeeFacts
	proof []byte
}

// False retains every byte of the existing history encodings. New routed
// births add the private marker to independent pre-existing commitments, so
// deleting the fee tables and clearing the database flag cannot forge legacy.
func withRuntimeFeeBirthMarker(o Order, facts []any) []any {
	if o.routeFeeRequired {
		facts = append(facts, struct {
			Required bool `json:"runtime_fee_quote_required,omitempty"`
		}{true})
	}
	if o.actualChargeRequired104 {
		facts = append(facts, struct {
			Required bool   `json:"actual_charge_required104"`
			Charge   *int64 `json:"actual_charge_amount104"`
		}{true, o.actualChargeAmount104})
	}
	return facts
}

func (s *Store) routeFeeProof(f routeFeeFacts) ([]byte, error) {
	// jsonb can reorder nested keys; authenticate canonical decoded JSON.
	var value any
	d := json.NewDecoder(bytes.NewReader(f.GroupSnapshot))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return nil, ErrConflict
	}
	canonical, e := json.Marshal(value)
	if e != nil {
		return nil, e
	}
	f.GroupSnapshot = canonical
	return s.referralDigest("runtime-channel168/route-fee", f)
}
func (s *Store) routeOrderFeeProof(o Order, q routeFeeQuote) ([]byte, error) {
	return s.referralDigest("runtime-channel168/order-fee", []any{o.ID, o.TradeNo, o.MerchantID, o.OutTradeNo, o.Name, o.Type, o.Amount, o.NotifyURL, o.ReturnURL, o.Param, o.Version, o.FeePolicy, o.FeeRateBPS, o.FeeGroupID, o.MerchantAmount, o.PlatformFee, o.RefundFeePayer, o.routeFeeRequired, q.proof})
}

// SealProviderRouteFeeTx is called by trusted routing while its merchant/group,
// membership and canonical channel locks are still held. It never reselects.
func (s *Store) SealProviderRouteFeeTx(ctx context.Context, tx pgx.Tx, b RuntimeChannel, group GroupPaymentRouting, in CreateOrderInput, cost *PaymentCostQuote, admissions ...*ParentGlobalLimitAdmission) error {
	if tx == nil || in.MerchantID <= 0 || b.id <= 0 || in.Type != b.kind {
		return ErrInvalid
	}
	var historical bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM orders WHERE merchant_id=$1 AND out_trade_no=$2)`, in.MerchantID, in.OutTradeNo).Scan(&historical); e != nil {
		return e
	}
	if historical {
		return nil
	} // Never invent birth facts for an existing order.
	var channel, amount int64
	var method, kind string
	var routeHash []byte
	if e := tx.QueryRow(ctx, `SELECT channel_id,method,pay_type,amount,request_hash FROM provider_route_assignments WHERE merchant_id=$1 AND out_trade_no=$2`, in.MerchantID, in.OutTradeNo).Scan(&channel, &method, &kind, &amount, &routeHash); e != nil {
		return e
	}
	if channel != b.id || kind != in.Type || amount != in.Amount {
		return ErrConflict
	}
	current, e := s.GroupPaymentRoutingTx(ctx, tx, in.MerchantID, in.Type)
	if e != nil {
		return e
	}
	if current.GroupID != group.GroupID || !bytes.Equal(current.Snapshot, group.Snapshot) || !current.ValidUntil.Equal(group.ValidUntil) {
		return ErrConflict
	}
	if e = validateOriginalOrderAmount(ctx, tx, strconv.FormatInt(group.GroupID, 10), in.MerchantID, in.Amount); e != nil {
		return e
	}
	parent, configured, e := s.runtimePolicyTx(ctx, tx, b, false)
	if e != nil {
		return e
	}
	if !parent.enabled || parent.mode != 0 {
		return ErrState
	}
	var groupBody []byte
	var groupUpdatedAt time.Time
	var merchantRevision int64
	if e = tx.QueryRow(ctx, `SELECT config_revision FROM merchants WHERE id=$1 FOR SHARE`, in.MerchantID).Scan(&merchantRevision); e != nil {
		return e
	}
	if e = tx.QueryRow(ctx, `SELECT body,updated_at FROM backoffice_records WHERE id=$1 AND resource='groups' FOR SHARE`, group.GroupID).Scan(&groupBody, &groupUpdatedAt); e != nil {
		return e
	}
	groupHash, e := s.referralDigest("runtime-channel168/group", json.RawMessage(groupBody))
	if e != nil {
		return e
	}
	f, e := feePolicy(ctx, tx, strconv.FormatInt(group.GroupID, 10), in.Type, in.Amount)
	if e != nil {
		return e
	}
	f, source, e := effectiveRuntimeFee(f, configured, parent.retained, groupBody, in.Type, in.Amount)
	if e != nil {
		return e
	}
	f, e = s.quoteBuyerCharge104(ctx, tx, in.MerchantID, b.id, in.Amount, f)
	if e != nil {
		return e
	}
	facts := routeFeeFacts{CustomerCharge104: f.CustomerCharge104, Version: 1, MerchantID: in.MerchantID, ChannelID: b.id, Amount: in.Amount, GroupID: group.GroupID, ParentRevision: parent.revision, ParentRetained: parent.retained, OutTradeNo: in.OutTradeNo, Name: in.Name, PayType: in.Type, Method: method, NotifyURL: in.NotifyURL, ReturnURL: in.ReturnURL, Param: in.Param, GroupSnapshot: group.Snapshot, GroupHash: groupHash, ParentIdentity: b.identity, AccountHash: b.accountHash, RouteHash: routeHash, ParentConfigured: configured, GroupValidUntil: group.ValidUntil, FeePolicy: f.Policy, RefundPayer: f.Payer, Source: source, FeeRate: f.Rate, FeeGroupID: f.GroupID, Net: f.Net, Fee: f.Fee}
	facts.MerchantRevision, facts.GroupUpdatedAt = merchantRevision, groupUpdatedAt
	facts.ParentProduct = b.product
	if len(admissions) > 1 {
		return ErrInvalid
	}
	var admission *ParentGlobalLimitAdmission
	if len(admissions) == 1 {
		admission = admissions[0]
	}
	ready, e := s.channelLimitSchema(ctx, tx)
	if e != nil {
		return e
	}
	if ready {
		policy, configured, e := s.globalLimitPolicyTx(ctx, tx, b, false)
		if e != nil {
			return e
		}
		if configured {
			if admission == nil || policy.Revision != admission.facts.PolicyRevision || !hmac.Equal(policy.Proof, admission.facts.PolicyProof) {
				return ErrConflict
			}
		} else if admission != nil {
			return ErrConflict
		}
	}
	if e = s.validateLimitAdmission(b, in.Amount, admission); e != nil {
		return e
	}
	if admission != nil {
		copy := admission.facts
		facts.GlobalLimits = &copy
	}
	if b.child != nil {
		if len(b.child.proof) != 32 || b.child.merchant != in.MerchantID || b.child.parent != b.id {
			return ErrConflict
		}
		facts.Version = 2
		facts.Child = b.child.commitment()
	}
	if cost != nil {
		if cost.channelID != b.id || cost.method != normalizedRouteMethod(kind, method) {
			return ErrConflict
		}
		x := cost.estimate
		facts.Cost = &routeFeeCost{Provider: x.Provider, AppID: x.AppID, AccountScope: x.AccountScope, Revision: x.Revision, Method: cost.method, RateUnits: x.RateUnits}
	}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&facts.QuotedAt); e != nil {
		return e
	}
	if !facts.GroupValidUntil.IsZero() && !facts.QuotedAt.Before(facts.GroupValidUntil) {
		return ErrState
	}
	if e = limitDeadline(ctx, tx, admission.ValidUntil()); e != nil {
		return e
	}
	proof, e := s.routeFeeProof(facts)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(facts)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO provider_route_fee_quotes(merchant_id,out_trade_no,facts,proof_hash) VALUES($1,$2,$3::jsonb,$4)`, in.MerchantID, in.OutTradeNo, string(raw), proof)
	if e != nil {
		return e
	}
	return s.sealParentPaymentSource(ctx, tx, b, routeFeeQuote{facts: facts, proof: proof})
}
func normalizedRouteMethod(kind, method string) string {
	if method == "applet" {
		if kind == "wxpay" {
			return "jsapi"
		}
		return "mini"
	}
	return method
}

func (s *Store) readRouteFeeQuote(ctx context.Context, q fundsReader, mid int64, no string) (routeFeeQuote, bool, error) {
	var out routeFeeQuote
	var raw []byte
	var channel, amount int64
	var method, kind string
	var routeHash []byte
	e := q.QueryRow(ctx, `SELECT r.channel_id,r.method,r.pay_type,r.amount,r.request_hash,f.facts,f.proof_hash FROM provider_route_assignments r LEFT JOIN provider_route_fee_quotes f ON f.merchant_id=r.merchant_id AND f.out_trade_no=r.out_trade_no WHERE r.merchant_id=$1 AND r.out_trade_no=$2`, mid, no).Scan(&channel, &method, &kind, &amount, &routeHash, &raw, &out.proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, false, nil
	}
	if e != nil {
		return out, false, e
	}
	if len(raw) == 0 {
		return out, true, ErrState
	} // Historical assignment without a born order cannot acquire today's quote.
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out.facts) != nil {
		return out, true, ErrConflict
	}
	f := out.facts
	if e = s.authenticateRouteFeeFacts(out, mid, no, channel, method, kind, amount, routeHash); e != nil {
		return out, true, e
	}
	// An authenticated 99 admission independently proves this is not pre99.
	// Removing both its schema and migration registration cannot erase that
	// provenance or let later success bypass its original global accounting.
	if f.GlobalLimits != nil {
		ready, e := s.channelLimitSchema(ctx, q)
		if e != nil {
			return out, true, e
		}
		if !ready {
			return out, true, ErrConflict
		}
	}
	if f.Child != nil {
		child, e := s.readChildRoute(ctx, q, mid, no)
		if e != nil {
			return out, true, e
		}
		if child == nil || !childCommitmentEqual(f.Child, child.commitment()) {
			return out, true, ErrConflict
		}
	}
	return out, true, nil
}

// Both single-intent execution and the batch capability projection consume the
// same version/MAC/route/financial facts validator. Optional future admissions
// must be authenticated here for either quote version, before child restoration.
func (s *Store) authenticateRouteFeeFacts(out routeFeeQuote, mid int64, no string, channel int64, method, kind string, amount int64, routeHash []byte) error {
	f := out.facts
	if f.GlobalLimits != nil {
		if e := s.validateLimitAdmission(RuntimeChannel{id: f.ChannelID, kind: f.PayType, identity: f.ParentIdentity}, f.Amount, &ParentGlobalLimitAdmission{facts: *f.GlobalLimits}); e != nil {
			return e
		}
	}
	if f.MerchantRevision <= 0 || f.GroupUpdatedAt.IsZero() {
		return ErrConflict
	}
	proof, e := s.routeFeeProof(f)
	if e != nil {
		return e
	}
	if ((f.Version != 1 || f.Child != nil) && (f.Version != 2 || f.Child == nil)) || f.MerchantID != mid || f.OutTradeNo != no || f.ChannelID != channel || f.Method != method || f.PayType != kind || f.Amount != amount || f.GroupID < 0 || f.FeeGroupID < 0 || len(f.ParentIdentity) != 32 || len(f.AccountHash) != 32 || len(f.GroupHash) != 32 || !hmac.Equal(f.RouteHash, routeHash) || !hmac.Equal(proof, out.proof) || f.Net < 0 || f.Fee < 0 {
		return ErrConflict
	}
	if f.RefundPayer != "platform" && f.RefundPayer != "merchant" {
		return ErrConflict
	}
	if f.CustomerCharge104 != nil {
		c := f.CustomerCharge104
		if e := s.validateBuyerQuote104(c); e != nil {
			return e
		}
		if c.MerchantID != f.MerchantID || c.ParentID != f.ChannelID || c.Original != f.Amount || c.Net != f.Net || c.Fee != f.Fee || c.FeeBPS != f.FeeRate || (f.FeePolicy != "none" && f.FeePolicy != "merchant_deduct" && f.FeePolicy != "buyer_additive") {
			return ErrConflict
		}
		return nil
	}
	net, fee, e := calculateFee(f.Amount, f.FeeRate)
	if e != nil || net != f.Net || fee != f.Fee || (f.FeePolicy != "none" && f.FeePolicy != "merchant_deduct") || (f.RefundPayer != "platform" && f.RefundPayer != "merchant") {
		return ErrConflict
	}
	return nil
}
func (q routeFeeQuote) matches(in CreateOrderInput) bool {
	f := q.facts
	return f.MerchantID == in.MerchantID && f.OutTradeNo == in.OutTradeNo && f.Name == in.Name && f.PayType == in.Type && f.Amount == in.Amount && f.NotifyURL == in.NotifyURL && f.ReturnURL == in.ReturnURL && f.Param == in.Param
}
func (q routeFeeQuote) snapshot() feeSnapshot {
	f := q.facts
	return feeSnapshot{CustomerCharge104: f.CustomerCharge104, Policy: f.FeePolicy, Payer: f.RefundPayer, Rate: f.FeeRate, GroupID: f.FeeGroupID, Net: f.Net, Fee: f.Fee}
}
func (q routeFeeQuote) costQuote() *PaymentCostQuote {
	f := q.facts
	if f.Cost == nil {
		return nil
	}
	x := f.Cost
	return &PaymentCostQuote{channelID: f.ChannelID, method: x.Method, estimate: adapter.PaymentCostEstimate{Provider: x.Provider, AppID: x.AppID, AccountScope: x.AccountScope, Revision: x.Revision, RateUnits: x.RateUnits}}
}
func (q routeFeeQuote) deadline(ctx context.Context, tx pgx.Tx) error {
	if q.facts.GlobalLimits != nil {
		if e := limitDeadline(ctx, tx, q.facts.GlobalLimits.ValidUntil); e != nil {
			return e
		}
	}
	if q.facts.GroupValidUntil.IsZero() {
		return nil
	}
	var now time.Time
	if e := tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	if !now.Before(q.facts.GroupValidUntil) {
		return ErrState
	}
	return nil
}
func (s *Store) insertOrderRouteFeeTerms(ctx context.Context, tx pgx.Tx, o Order, q routeFeeQuote) error {
	proof, e := s.routeOrderFeeProof(o, q)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO order_route_fee_terms(order_id,merchant_id,out_trade_no,proof_hash) VALUES($1,$2,$3,$4)`, o.ID, o.MerchantID, o.OutTradeNo, proof)
	return e
}
func (s *Store) validateOrderRouteFeeTerms(ctx context.Context, q referralReader, o Order) (*routeFeeQuote, error) {
	// Historical migration fixtures and actual pre96 data have no new schema.
	// A partial/lost current schema is not evidence of historical provenance.
	var tables int
	var marker, migrations bool
	if e := q.QueryRow(ctx, `SELECT num_nonnulls(to_regclass(current_schema()||'.runtime_channel_policies'),to_regclass(current_schema()||'.provider_route_fee_quotes'),to_regclass(current_schema()||'.order_route_fee_terms')),EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass(current_schema()||'.orders') AND attname='runtime_fee_quote_required' AND NOT attisdropped),to_regclass(current_schema()||'.schema_migrations') IS NOT NULL`).Scan(&tables, &marker, &migrations); e != nil {
		return nil, e
	}
	if tables != 3 || !marker {
		var migrated bool
		if migrations {
			if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000096_runtime_channel_fees.up.sql')`).Scan(&migrated); e != nil {
				return nil, e
			}
		}
		if tables != 0 || marker || migrated || o.routeFeeRequired {
			return nil, ErrConflict
		}
		return nil, nil
	}
	var mid int64
	var no string
	var proof []byte
	e := q.QueryRow(ctx, `SELECT merchant_id,out_trade_no,proof_hash FROM order_route_fee_terms WHERE order_id=$1`, o.ID).Scan(&mid, &no, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		if o.routeFeeRequired {
			return nil, ErrConflict
		}
		if _, e = s.readReferralTermsBase(ctx, q, o); e != nil && !errors.Is(e, ErrNotFound) {
			return nil, e
		}
		var hasQuote bool
		if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM provider_route_fee_quotes WHERE merchant_id=$1 AND out_trade_no=$2)`, o.MerchantID, o.OutTradeNo).Scan(&hasQuote); e != nil {
			return nil, e
		}
		if hasQuote {
			return nil, ErrConflict
		}
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	if mid != o.MerchantID || no != o.OutTradeNo {
		return nil, ErrConflict
	}
	quote, found, e := s.readRouteFeeQuote(ctx, q, mid, no)
	if e != nil {
		return nil, e
	}
	if !found {
		return nil, ErrConflict
	}
	if e = s.authenticateOrderRouteFeeBinding(o, proof, quote); e != nil {
		return nil, e
	}
	return &quote, nil
}

func (s *Store) authenticateOrderRouteFeeBinding(o Order, proof []byte, quote routeFeeQuote) error {
	expected, e := s.routeOrderFeeProof(o, quote)
	if e != nil {
		return e
	}
	if !hmac.Equal(expected, proof) || !quote.matches(CreateOrderInput{MerchantID: o.MerchantID, OutTradeNo: o.OutTradeNo, Name: o.Name, Type: o.Type, Amount: o.Amount, NotifyURL: o.NotifyURL, ReturnURL: o.ReturnURL, Param: o.Param}) {
		return ErrConflict
	}
	f := quote.facts
	if f.CustomerCharge104 != nil {
		if !o.actualChargeRequired104 || o.actualChargeAmount104 == nil || *o.actualChargeAmount104 != f.CustomerCharge104.Charge {
			return ErrConflict
		}
	} else if o.actualChargeRequired104 || o.actualChargeAmount104 != nil {
		return ErrConflict
	}
	if o.FeePolicy != f.FeePolicy || o.FeeRateBPS != f.FeeRate || o.FeeGroupID != f.FeeGroupID || o.MerchantAmount != f.Net || o.PlatformFee != f.Fee || o.RefundFeePayer != f.RefundPayer {
		return ErrConflict
	}
	return nil
}
func (s *Store) validateRouteFeeAttempt(ctx context.Context, tx pgx.Tx, o Order, channel int64, method, scope string, bound *RuntimeChannel) error {
	quote, e := s.validateOrderRouteFeeTerms(ctx, tx, o)
	if e != nil {
		return e
	}
	if e = s.validateGlobalLimitFirstAttempt(ctx, tx, channel, quote); e != nil {
		return e
	}
	executionAccount, e := s.parentAttemptAccount(ctx, tx, o, bound, quote)
	if e != nil {
		return e
	}
	if quote == nil {
		var childSchema bool
		if e = tx.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.payment_subchannel_routes') IS NOT NULL`).Scan(&childSchema); e != nil {
			return e
		}
		if childSchema && catalogPayType(o.Type) {
			group, e := s.GroupPaymentRoutingTx(ctx, tx, o.MerchantID, o.Type)
			if e != nil {
				return e
			}
			if group.Choice.Kind() == grouppolicy.Subchannels {
				return ErrState
			}
		}
		var table bool
		if e = tx.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.runtime_channel_policies') IS NOT NULL`).Scan(&table); e != nil {
			return e
		}
		if !table {
			return nil
		} // validateOrderRouteFeeTerms already proved a true pre96 schema.
		var configured, originated bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_channel_policies WHERE channel_id=$1),EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-channel-policies' AND operation='runtime-channel-policy-update' AND resource_id=$1)`, channel).Scan(&configured, &originated); e != nil {
			return e
		}
		if configured {
			return ErrState
		}
		if originated {
			return ErrConflict
		}
		return nil
	}
	f := quote.facts
	if bound != nil {
		if bound.id != channel || bound.product != f.ParentProduct || !hmac.Equal(bound.identity, f.ParentIdentity) {
			return ErrConflict
		}
	} else if f.ParentConfigured {
		return ErrConflict
	}
	if f.Child != nil && (bound == nil || bound.child == nil || !childCommitmentEqual(f.Child, bound.child.commitment())) {
		return ErrConflict
	}
	if f.Child != nil {
		if e = s.checkChildFirstSendTx(ctx, tx, o, bound); e != nil {
			return e
		}
	}
	if f.Child == nil && bound != nil && bound.child != nil {
		return ErrConflict
	}
	if channel != f.ChannelID || method != normalizedRouteMethod(f.PayType, f.Method) {
		return ErrConflict
	}
	if scope == "" {
		if f.ParentConfigured {
			return ErrConflict
		}
	} else {
		hash, e := s.providerAccountHash(scope)
		if e != nil || !hmac.Equal(hash, executionAccount) {
			return ErrConflict
		}
	}
	current, configured, e := s.runtimePolicyTx(ctx, tx, RuntimeChannel{id: channel, kind: f.PayType, identity: f.ParentIdentity}, false)
	if e != nil {
		return e
	}
	if !configured && f.ParentConfigured {
		return ErrConflict
	}
	if !current.enabled || current.mode != 0 {
		return ErrState
	}
	return nil
}

func routeOrderLock(ctx context.Context, tx pgx.Tx, mid int64, no string) error {
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/route/order/%d/%s", mid, no))
	return e
}

func (s *Store) CheckProviderRouteFeeQuoteTx(ctx context.Context, tx pgx.Tx, mid int64, no string) error {
	quote, found, e := s.readRouteFeeQuote(ctx, tx, mid, no)
	if e != nil {
		return e
	}
	if !found {
		return ErrConflict
	}
	return quote.deadline(ctx, tx)
}
