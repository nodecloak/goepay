package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"reflect"
)

type buyerPolicyBinding104 struct {
	Facts buyerPolicyFacts104
	Proof []byte
}
type buyerChargeQuote104 struct {
	Version                                                  int
	MerchantID, ParentID, Original, Charge, Net, Fee, FeeBPS int64
	Mode                                                     int
	BuyerRate                                                *string
	Minimum                                                  buyerMinimum104
	MerchantPolicy, GlobalPolicy, ParentPolicy               *buyerPolicyBinding104
}
type buyerChargeSource104 struct {
	Version    int
	Quote      buyerChargeQuote104
	RouteProof []byte
}
type orderChargeCommitment104 struct {
	Charge      int64
	SourceProof []byte
}

func (s *Store) authenticateFrozenCharge104(ctx context.Context, q referralReader, o Order, c *orderChargeCommitment104) error {
	charge, proof, e := s.authenticatedOrderChargeReader104(ctx, q, o)
	if e != nil {
		return e
	}
	if c == nil {
		if len(proof) != 0 {
			return ErrConflict
		}
		return nil
	}
	if c.Charge != charge || len(proof) != 32 || !hmac.Equal(proof, c.SourceProof) {
		return ErrConflict
	}
	return nil
}

func buyerBinding104(p buyerPolicyRecord104) *buyerPolicyBinding104 {
	if !p.Configured {
		return nil
	}
	return &buyerPolicyBinding104{p.Facts, p.Proof}
}

func (s *Store) buyerChargeSchema104(ctx context.Context, q referralReader) (bool, error) {
	var tables, columns int
	var migrations bool
	e := q.QueryRow(ctx, `SELECT num_nonnulls(to_regclass(current_schema()||'.customer_charge_policies104'),to_regclass(current_schema()||'.order_customer_charges104'),to_regclass(current_schema()||'.customer_charge_policy_witnesses104')),(SELECT count(*) FROM pg_attribute WHERE attrelid=to_regclass(current_schema()||'.orders') AND attname IN ('actual_charge_amount104','actual_charge_required104') AND NOT attisdropped),to_regclass(current_schema()||'.schema_migrations') IS NOT NULL`).Scan(&tables, &columns, &migrations)
	if e != nil {
		return false, e
	}
	var migrated bool
	if migrations {
		if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000104_order_customer_charges.up.sql')`).Scan(&migrated); e != nil {
			return false, e
		}
	}
	if tables == 3 && columns == 2 {
		if migrations && !migrated {
			return false, ErrConflict
		}
		return true, nil
	}
	if tables != 0 || columns != 0 || migrated {
		return false, ErrConflict
	}
	return false, nil
}

func (s *Store) quoteBuyerCharge104(ctx context.Context, tx pgx.Tx, mid, parent, money int64, f feeSnapshot) (feeSnapshot, error) {
	ready, e := s.buyerChargeSchema104(ctx, tx)
	if e != nil {
		return f, e
	}
	if !ready {
		return f, ErrConflict
	}
	merchant, e := s.buyerPolicyTx104(ctx, tx, "merchant", mid, false)
	if e != nil {
		return f, e
	}
	global, e := s.buyerPolicyTx104(ctx, tx, "global", 0, false)
	if e != nil {
		return f, e
	}
	var pp buyerPolicyRecord104
	if parent > 0 {
		pp, e = s.buyerPolicyTx104(ctx, tx, "parent", parent, false)
		if e != nil {
			return f, e
		}
	}
	c := &buyerChargeQuote104{Version: 1, MerchantID: mid, ParentID: parent, Original: money, FeeBPS: f.Rate, MerchantPolicy: buyerBinding104(merchant), GlobalPolicy: buyerBinding104(global), ParentPolicy: buyerBinding104(pp)}
	if merchant.Configured {
		c.Mode = merchant.Facts.MerchantMode
		c.BuyerRate = merchant.Facts.BuyerRate
	}
	if global.Configured {
		c.Minimum = buyerMinimum104{*global.Facts.Threshold, *global.Facts.Cost}
	}
	if pp.Configured {
		c.Minimum = selectBuyerMinimum104(c.Minimum, pp.Facts.Threshold, pp.Facts.Cost)
	}
	a, e := calculateBuyerChargeRate104(money, f.Rate, c.Mode, c.BuyerRate, c.Minimum)
	if e != nil {
		return f, e
	}
	c.Charge, c.Net, c.Fee = a.Charge, a.Net, a.Fee
	f.Net, f.Fee, f.CustomerCharge104 = a.Net, a.Fee, c
	if c.Mode == 1 && (c.BuyerRate == nil || *c.BuyerRate != "0") {
		f.Policy = "buyer_additive"
	} else if a.Fee > 0 {
		f.Policy = "merchant_deduct"
	}
	return f, nil
}

func (s *Store) validateBuyerQuote104(c *buyerChargeQuote104) error {
	if c == nil || c.Version != 1 || c.MerchantID <= 0 || c.ParentID < 0 {
		return ErrConflict
	}
	for _, p := range []struct {
		b     *buyerPolicyBinding104
		kind  string
		owner int64
	}{{c.MerchantPolicy, "merchant", c.MerchantID}, {c.GlobalPolicy, "global", 0}, {c.ParentPolicy, "parent", c.ParentID}} {
		if p.b == nil {
			continue
		}
		f := p.b.Facts
		if !buyerPolicyValid104(f) || f.Kind != p.kind || f.OwnerID != p.owner {
			return ErrConflict
		}
		expected, e := s.buyerPolicyProof104(f)
		if e != nil {
			return e
		}
		if !hmac.Equal(expected, p.b.Proof) {
			return ErrConflict
		}
	}
	mode := 0
	var bps *string
	var minimum buyerMinimum104
	if p := c.MerchantPolicy; p != nil {
		mode = p.Facts.MerchantMode
		bps = p.Facts.BuyerRate
	}
	if p := c.GlobalPolicy; p != nil {
		minimum = buyerMinimum104{*p.Facts.Threshold, *p.Facts.Cost}
	}
	if p := c.ParentPolicy; p != nil {
		minimum = selectBuyerMinimum104(minimum, p.Facts.Threshold, p.Facts.Cost)
	}
	if mode != c.Mode || (bps == nil) != (c.BuyerRate == nil) || bps != nil && *bps != *c.BuyerRate || minimum != c.Minimum {
		return ErrConflict
	}
	a, e := calculateBuyerChargeRate104(c.Original, c.FeeBPS, c.Mode, c.BuyerRate, c.Minimum)
	if e != nil || a.Charge != c.Charge || a.Net != c.Net || a.Fee != c.Fee {
		return ErrConflict
	}
	return nil
}
func (s *Store) buyerSourceProof104(o Order, f buyerChargeSource104) ([]byte, error) {
	return s.referralDigest("customer-charge104/order-birth", []any{o.ID, o.TradeNo, o.MerchantID, o.OutTradeNo, o.Name, o.Type, o.Amount, o.NotifyURL, o.ReturnURL, o.Param, o.Version, o.FeePolicy, o.FeeRateBPS, o.FeeGroupID, o.MerchantAmount, o.PlatformFee, o.RefundFeePayer, o.CreatedAt.UTC(), o.routeFeeRequired, o.actualChargeRequired104, o.actualChargeAmount104, f})
}

func exactBuyerJSON104(raw []byte, value any) bool {
	if len(raw) > 1<<20 {
		return false
	}
	expected, e := json.Marshal(value)
	if e != nil {
		return false
	}
	decode := func(b []byte) (any, error) {
		d := json.NewDecoder(bytes.NewReader(b))
		d.UseNumber()
		var v any
		e := d.Decode(&v)
		return v, e
	}
	a, e := decode(raw)
	if e != nil {
		return false
	}
	b, e := decode(expected)
	return e == nil && reflect.DeepEqual(a, b)
}
func (s *Store) insertBuyerSource104(ctx context.Context, tx pgx.Tx, o Order, c *buyerChargeQuote104, routeProof []byte) error {
	if c == nil {
		if o.actualChargeRequired104 || o.actualChargeAmount104 != nil {
			return ErrConflict
		}
		return nil
	}
	if e := s.validateBuyerQuote104(c); e != nil {
		return e
	}
	if !o.actualChargeRequired104 || o.actualChargeAmount104 == nil || *o.actualChargeAmount104 != c.Charge || o.Amount != c.Original || o.MerchantID != c.MerchantID || o.MerchantAmount != c.Net || o.PlatformFee != c.Fee {
		return ErrConflict
	}
	f := buyerChargeSource104{Version: 1, Quote: *c, RouteProof: routeProof}
	proof, e := s.buyerSourceProof104(o, f)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(f)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO order_customer_charges104(order_id,merchant_id,facts,proof_hash) VALUES($1,$2,$3,$4)`, o.ID, o.MerchantID, raw, proof); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'order-birth','order-customer-charges104',$1,'create',jsonb_build_object('commitment',$2::text))`, o.ID, buyerPolicyCommitment104(proof))
	return e
}

// Authenticate the private leaf before consuming C. Source failure never falls
// back to an unchecked nullable column or reprices an existing order.
func (s *Store) readBuyerSource104(ctx context.Context, q referralReader, o Order) (int64, []byte, error) {
	ready, e := s.buyerChargeSchema104(ctx, q)
	if e != nil {
		return 0, nil, e
	}
	if !ready {
		if o.actualChargeRequired104 || o.actualChargeAmount104 != nil {
			return 0, nil, ErrConflict
		}
		return o.Amount, nil, nil
	}
	var mid int64
	var raw, proof []byte
	e = q.QueryRow(ctx, `SELECT merchant_id,facts,proof_hash FROM order_customer_charges104 WHERE order_id=$1`, o.ID).Scan(&mid, &raw, &proof)
	found := e == nil
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return 0, nil, e
	}
	var audits int64
	var commitment string
	if e = q.QueryRow(ctx, `SELECT count(*),COALESCE(max(changes->>'commitment'),'') FROM backoffice_audit WHERE resource='order-customer-charges104' AND resource_id=$1 AND operation='create'`, o.ID).Scan(&audits, &commitment); e != nil {
		return 0, nil, e
	}
	if !found {
		if o.actualChargeRequired104 || o.actualChargeAmount104 != nil || audits != 0 || o.FeePolicy == "buyer_additive" {
			return 0, nil, ErrConflict
		}
		return o.Amount, nil, nil
	}
	var routeProof []byte
	if o.routeFeeRequired {
		quote, found, e := s.readRouteFeeQuote(ctx, q, o.MerchantID, o.OutTradeNo)
		if e != nil {
			if errors.Is(e, ErrState) {
				return 0, nil, ErrConflict
			}
			return 0, nil, e
		}
		if !found {
			return 0, nil, ErrConflict
		}
		routeProof = quote.proof
	}
	return s.authenticateBuyerSourceRow104(o, mid, raw, proof, audits, commitment, routeProof)
}

func (s *Store) authenticateBuyerSourceRow104(o Order, mid int64, raw, proof []byte, audits int64, commitment string, routeProof []byte) (int64, []byte, error) {
	if len(raw) == 0 {
		if mid != 0 || len(proof) != 0 || o.actualChargeRequired104 || o.actualChargeAmount104 != nil || audits != 0 || o.FeePolicy == "buyer_additive" {
			return 0, nil, ErrConflict
		}
		return o.Amount, nil, nil
	}
	var f buyerChargeSource104
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil || len(keys) != 3 || keys["Version"] == nil || keys["Quote"] == nil || keys["RouteProof"] == nil || json.Unmarshal(raw, &f) != nil || f.Version != 1 || !exactBuyerJSON104(raw, f) {
		return 0, nil, ErrConflict
	}
	if e := s.validateBuyerQuote104(&f.Quote); e != nil {
		return 0, nil, e
	}
	c := f.Quote
	if mid != o.MerchantID || c.MerchantID != o.MerchantID || !o.actualChargeRequired104 || o.actualChargeAmount104 == nil || *o.actualChargeAmount104 != c.Charge || c.Original != o.Amount || c.Net != o.MerchantAmount || c.Fee != o.PlatformFee || c.FeeBPS != o.FeeRateBPS || audits != 1 || commitment != buyerPolicyCommitment104(proof) {
		return 0, nil, ErrConflict
	}
	expected, e := s.buyerSourceProof104(o, f)
	if e != nil {
		return 0, nil, e
	}
	if !hmac.Equal(expected, proof) {
		return 0, nil, ErrConflict
	}
	if o.routeFeeRequired && (len(routeProof) != 32 || !hmac.Equal(routeProof, f.RouteProof)) || !o.routeFeeRequired && len(f.RouteProof) != 0 {
		return 0, nil, ErrConflict
	}
	return c.Charge, proof, nil
}

func (s *Store) authenticatedOrderCharge104(ctx context.Context, tx pgx.Tx, o Order) (int64, []byte, error) {
	return s.authenticatedOrderChargeReader104(ctx, tx, o)
}
func (s *Store) authenticatedChargeValue104(ctx context.Context, q referralReader, o Order) (int64, error) {
	c, _, e := s.authenticatedOrderChargeReader104(ctx, q, o)
	return c, e
}
func (s *Store) authenticatedOrderChargeReader104(ctx context.Context, tx referralReader, o Order) (int64, []byte, error) {
	c, proof, e := s.readBuyerSource104(ctx, tx, o)
	if e != nil {
		return 0, nil, e
	}
	if _, e = s.validateOrderRouteFeeTerms(ctx, tx, o); e != nil {
		return 0, nil, e
	}
	if len(proof) == 0 {
		legacy, err := s.beforeReferralChargeSchema190(ctx, tx, o)
		if err != nil {
			return 0, nil, err
		}
		if legacy {
			return c, nil, nil
		}
	}
	if _, e = s.readReferralTermsBase(ctx, tx, o); e != nil && !errors.Is(e, ErrNotFound) {
		return 0, nil, e
	}
	return c, proof, nil
}
func (s *Store) AuthenticatedOrderCharge104(ctx context.Context, o Order) (int64, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback(ctx)
	current, e := scanOrder(tx.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1 FOR SHARE", o.ID))
	if e != nil {
		return 0, e
	}
	if current.TradeNo != o.TradeNo || current.MerchantID != o.MerchantID || current.OutTradeNo != o.OutTradeNo {
		return 0, ErrConflict
	}
	c, _, e := s.authenticatedOrderCharge104(ctx, tx, current)
	if e != nil {
		return 0, e
	}
	return c, tx.Commit(ctx)
}

func (s *Store) authenticatedOrderIDCharge104(ctx context.Context, q referralReader, id int64) (int64, error) {
	o, e := scanOrder(q.QueryRow(ctx, "SELECT "+orderColumns+" FROM orders WHERE id=$1", id))
	if e != nil {
		return 0, e
	}
	return s.authenticatedChargeValue104(ctx, q, o)
}
