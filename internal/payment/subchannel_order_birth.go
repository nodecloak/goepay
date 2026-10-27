package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
)

// Known born orders must prove their original fee source before any account is
// restored or contacted. Current table presence and equal account identity do
// not substitute for this independent birth proof.
func (s *Store) authenticateOriginalOrderBirth(ctx context.Context, q fundsReader, mid int64, no string) error {
	o, e := scanOrder(q.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE merchant_id=$1 AND out_trade_no=$2`, mid, no))
	if errors.Is(e, ErrNotFound) {
		return nil
	}
	if e != nil {
		return e
	}
	reader, ok := q.(referralReader)
	if !ok {
		return ErrInvalid
	}
	_, e = s.validateOrderRouteFeeTerms(ctx, reader, o)
	return e
}

func (s *Store) routeFeeSchemaForProjection(ctx context.Context, q fundsReader) (bool, error) {
	var tables int
	var marker, migrations bool
	if e := q.QueryRow(ctx, `SELECT num_nonnulls(to_regclass(current_schema()||'.runtime_channel_policies'),to_regclass(current_schema()||'.provider_route_fee_quotes'),to_regclass(current_schema()||'.order_route_fee_terms')),EXISTS(SELECT 1 FROM pg_attribute WHERE attrelid=to_regclass(current_schema()||'.orders') AND attname='runtime_fee_quote_required' AND NOT attisdropped),to_regclass(current_schema()||'.schema_migrations') IS NOT NULL`).Scan(&tables, &marker, &migrations); e != nil {
		return false, e
	}
	if tables == 3 && marker {
		return true, nil
	}
	if tables != 0 || marker {
		return false, ErrConflict
	}
	if migrations {
		var applied bool
		if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000096_runtime_channel_fees.up.sql')`).Scan(&applied); e != nil {
			return false, e
		}
		if applied {
			return false, ErrConflict
		}
	}
	return false, nil
}
func (s *Store) projectedOrderBirthValid(o Order, t referralOrderTerms, versions []byte, sourceVersion int, sourceHash, costProof []byte) bool {
	if t.OrderID == 0 {
		return sourceVersion == 0 && len(sourceHash) == 0 && !o.routeFeeRequired
	}
	if t.OrderID != o.ID || t.MerchantID != o.MerchantID || t.PlatformFee != o.PlatformFee || t.RefundPolicy != referralRefundPolicy || json.Unmarshal(versions, &t.PolicyVersions) != nil {
		return false
	}
	proof, e := s.referralTermsProof(o, t)
	if e != nil || !hmac.Equal(proof, t.ProofHash) {
		return false
	}
	if sourceVersion == 0 {
		return len(sourceHash) == 0 && !o.routeFeeRequired
	}
	if sourceVersion != 3 {
		return false
	}
	expected, e := s.referralOrderSourceProof(o, t.ProofHash, costProof)
	return e == nil && hmac.Equal(expected, sourceHash)
}
