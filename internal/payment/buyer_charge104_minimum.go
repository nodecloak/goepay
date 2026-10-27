package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"strconv"
)

type BuyerMinimumState104 struct {
	Kind               string `json:"kind"`
	ChannelID          int64  `json:"channel_id,string"`
	Threshold          string `json:"payfee_lessthan"`
	Cost               string `json:"payfee_mincost"`
	EffectiveThreshold string `json:"effective_payfee_lessthan"`
	EffectiveCost      string `json:"effective_payfee_mincost"`
	Configured         bool   `json:"configured"`
	Revision           string `json:"revision"`
	Fingerprint        string `json:"fingerprint"`
	CanUpdate          bool   `json:"can_update"`
}

func minimumText104(v *int64) string {
	if v == nil {
		return ""
	}
	return ledger.Format(*v)
}
func (s *Store) minimumState104(ctx context.Context, tx pgx.Tx, b *RuntimeChannel, a MerchantCredentialActor, write bool) (BuyerMinimumState104, buyerPolicyRecord104, error) {
	var out BuyerMinimumState104
	var own buyerPolicyRecord104
	if e := identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, own, e
	}
	kind, owner := "global", int64(0)
	if b != nil {
		if b.id <= 0 || len(b.identity) != 32 {
			return out, own, ErrInvalid
		}
		kind, owner = "parent", b.id
	}
	global, e := s.buyerPolicyTx104(ctx, tx, "global", 0, write && b == nil)
	if e != nil {
		return out, own, e
	}
	own = global
	if b != nil {
		own, e = s.buyerPolicyTx104(ctx, tx, kind, owner, write)
		if e != nil {
			return out, own, e
		}
	}
	effective := buyerMinimum104{}
	if global.Configured {
		effective = buyerMinimum104{*global.Facts.Threshold, *global.Facts.Cost}
	}
	if b != nil && own.Configured {
		effective = selectBuyerMinimum104(effective, own.Facts.Threshold, own.Facts.Cost)
	}
	out = BuyerMinimumState104{Kind: kind, ChannelID: owner, Threshold: minimumText104(own.Facts.Threshold), Cost: minimumText104(own.Facts.Cost), EffectiveThreshold: ledger.Format(effective.Threshold), EffectiveCost: ledger.Format(effective.Cost), Configured: own.Configured, Revision: strconv.FormatInt(own.Facts.Revision, 10), CanUpdate: true}
	if b == nil && !own.Configured {
		out.Threshold, out.Cost = "0.00", "0.00"
	}
	var parentIdentity []byte
	if b != nil {
		parentIdentity = b.identity
	}
	proof, e := s.referralDigest("customer-charge104/minimum-cas", []any{kind, owner, parentIdentity, global.Configured, global.Facts, global.Proof, own.Configured, own.Facts, own.Proof})
	if e != nil {
		return out, own, e
	}
	out.Fingerprint = hex.EncodeToString(proof)
	return out, own, nil
}
func (s *Store) BuyerMinimumSettings104(ctx context.Context, b *RuntimeChannel, a MerchantCredentialActor) (BuyerMinimumState104, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return BuyerMinimumState104{}, e
	}
	defer tx.Rollback(ctx)
	out, _, e := s.minimumState104(ctx, tx, b, a, false)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) UpdateBuyerMinimumSettings104(ctx context.Context, b *RuntimeChannel, a MerchantCredentialActor, threshold, cost, expected string) (BuyerMinimumState104, error) {
	var out BuyerMinimumState104
	parse := func(text string) (*int64, error) {
		if text == "" {
			if b == nil {
				return nil, ErrInvalid
			}
			return nil, nil
		}
		v, e := parseLimitMoney(text)
		if e != nil {
			return nil, e
		}
		return &v, nil
	}
	th, e := parse(threshold)
	if e != nil {
		return out, e
	}
	mc, e := parse(cost)
	if e != nil || len(expected) != 64 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	before, previous, e := s.minimumState104(ctx, tx, b, a, true)
	if e != nil {
		return out, e
	}
	if !hmac.Equal([]byte(expected), []byte(before.Fingerprint)) {
		return out, ErrConflict
	}
	_, e = s.writeBuyerPolicy104(ctx, tx, previous, buyerPolicyFacts104{Kind: before.Kind, OwnerID: before.ChannelID, Threshold: th, Cost: mc}, MerchantRSAActor{User: a.User})
	if e != nil {
		return out, e
	}
	out, _, e = s.minimumState104(ctx, tx, b, a, true)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
