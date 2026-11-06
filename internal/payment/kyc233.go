package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/channelconfig"
	"goepay/internal/kyc"
)

var ErrCertificationRequired = errors.New("merchant real-name certification required")

// ResolveKYCChannel233 freezes the authenticated current account selected by
// the administrator. It never falls back to bootstrap credentials after a
// managed account is disabled, archived, rotated or damaged.
func (g ProviderFlow) ResolveKYCChannel233(ctx context.Context, tx pgx.Tx, id int64) (channelconfig.Channel, error) {
	if g.Store == nil || tx == nil || id <= 0 {
		return channelconfig.Channel{}, ErrInvalid
	}
	if err := parentChannelLock(ctx, tx, id); err != nil {
		return channelconfig.Channel{}, err
	}
	states, err := g.Store.readParentStates(ctx, tx)
	if err != nil {
		return channelconfig.Channel{}, err
	}
	for _, state := range states {
		h, a, p := state.head, state.account, state.policy
		if h.id != id {
			continue
		}
		if h.kind != "alipay" || h.archived || !p.enabled || p.mode != 0 {
			return channelconfig.Channel{}, ErrState
		}
		f, ok := g.ParentFactories[h.source].(*channelconfig.ParentChannelFactory)
		if !ok || f.OriginID() != h.source || f.PayType() != "alipay" {
			return channelconfig.Channel{}, ErrConflict
		}
		if _, err = g.restoreParentExecution(a); err != nil {
			return channelconfig.Channel{}, err
		}
		raw, err := g.Store.openParentAccount(a)
		if err != nil {
			return channelconfig.Channel{}, err
		}
		return f.CertificationSnapshot233(id, raw)
	}
	return channelconfig.Channel{}, ErrNotFound
}

func (s *Store) requireCertification233Tx(ctx context.Context, tx pgx.Tx, merchant int64) error {
	service, err := kyc.New(s.pool, s.confirmationKey231, nil, nil, "")
	if err != nil {
		return err
	}
	err = service.RequiredTx(ctx, tx, merchant)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCertificationRequired
	}
	return err
}
