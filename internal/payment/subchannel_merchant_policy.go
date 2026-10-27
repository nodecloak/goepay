package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/grouppolicy"
	"time"
)

type SubchannelMerchantPolicy struct {
	MerchantID  int64  `json:"merchant_id,string"`
	RollType    int    `json:"roll_type"`
	Revision    int64  `json:"revision,string"`
	Fingerprint string `json:"fingerprint"`
	configured  bool
	proof       []byte
}

func (s *Store) childMerchantPolicyTx(ctx context.Context, tx pgx.Tx, mid int64, write bool) (SubchannelMerchantPolicy, error) {
	p := SubchannelMerchantPolicy{MerchantID: mid}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	e := tx.QueryRow(ctx, `SELECT roll_type,revision,proof_hash FROM payment_subchannel_merchant_policy WHERE merchant_id=$1`+lock, mid).Scan(&p.RollType, &p.Revision, &p.proof)
	if errors.Is(e, pgx.ErrNoRows) {
		var originated bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='payment-subchannel-policies' AND resource_id=$1 AND operation='payment-subchannel-policy-update')`, mid).Scan(&originated); e != nil {
			return p, e
		}
		if originated {
			return p, ErrConflict
		}
	} else if e != nil {
		return p, e
	} else {
		expected, e := s.referralDigest("payment-subchannel168/merchant-policy", []any{mid, p.RollType, p.Revision})
		if e != nil || p.RollType < 0 || p.RollType > 3 || p.Revision <= 0 || !hmac.Equal(expected, p.proof) {
			return p, ErrConflict
		}
		var updates int64
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='payment-subchannel-policies' AND resource_id=$1 AND operation='payment-subchannel-policy-update'`, mid).Scan(&updates); e != nil {
			return p, e
		}
		if updates != p.Revision {
			return p, ErrConflict
		}
		p.configured = true
	}
	fingerprint, e := s.referralDigest("payment-subchannel168/merchant-policy-state", []any{mid, p.RollType, p.Revision, p.configured, p.proof})
	if e != nil {
		return p, e
	}
	p.Fingerprint = hex.EncodeToString(fingerprint)
	return p, nil
}
func (s *Store) SubchannelPolicy(ctx context.Context, a SubchannelActor, mid int64) (SubchannelMerchantPolicy, error) {
	var out SubchannelMerchantPolicy
	if !a.allows(mid) {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = a.check(ctx, tx); e != nil {
		return out, e
	}
	out, e = s.childMerchantPolicyTx(ctx, tx, mid, false)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) UpdateSubchannelPolicy(ctx context.Context, a SubchannelActor, mid int64, roll int, expected string) (SubchannelMerchantPolicy, error) {
	var out SubchannelMerchantPolicy
	if !a.allows(mid) || roll < 0 || roll > 3 || len(expected) != 64 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = a.check(ctx, tx); e != nil {
		return out, e
	}
	var deadline time.Time
	eligible := false
	for _, kind := range []string{"alipay", "wxpay"} {
		group, e := s.GroupPaymentRoutingTx(ctx, tx, mid, kind)
		if e != nil {
			return out, e
		}
		if group.Choice.Kind() == grouppolicy.Subchannels {
			eligible = true
			deadline = group.ValidUntil
		}
	}
	if !eligible {
		return out, ErrState
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/route/child-policy/%d", mid)); e != nil {
		return out, e
	}
	current, e := s.childMerchantPolicyTx(ctx, tx, mid, true)
	if e != nil {
		return out, e
	}
	if !hmac.Equal([]byte(expected), []byte(current.Fingerprint)) {
		return out, ErrConflict
	}
	revision := current.Revision + 1
	proof, e := s.referralDigest("payment-subchannel168/merchant-policy", []any{mid, roll, revision})
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO payment_subchannel_merchant_policy(merchant_id,roll_type,revision,proof_hash) VALUES($1,$2,$3,$4) ON CONFLICT(merchant_id) DO UPDATE SET roll_type=EXCLUDED.roll_type,revision=EXCLUDED.revision,proof_hash=EXCLUDED.proof_hash`, mid, roll, revision, proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'payment-subchannel-policies',$4,'payment-subchannel-policy-update',jsonb_build_object('roll_type',$5::int,'revision',$6::text))`, a.User.Role, a.User.ID, a.User.Username, mid, roll, fmt.Sprint(revision)); e != nil {
		return out, e
	}
	if e = a.check(ctx, tx); e != nil {
		return out, e
	}
	if !deadline.IsZero() {
		var now time.Time
		if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			return out, e
		}
		if !now.Before(deadline) {
			return out, ErrState
		}
	}
	out, e = s.childMerchantPolicyTx(ctx, tx, mid, false)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
