package payment

import (
	"context"
	"crypto/hmac"
	"errors"

	"github.com/jackc/pgx/v5"
)

type transferRoutingSelection228Key struct{}

// The selected configuration is request-local. A financial consent builder and
// the first durable reservation must observe this same policy, after any waits.
func WithTransferRoutingSelection228(ctx context.Context, fingerprint string) context.Context {
	return context.WithValue(ctx, transferRoutingSelection228Key{}, fingerprint)
}

func CheckTransferRoutingSelection228(ctx context.Context, state TransferRoutingState228) error {
	if selected, ok := ctx.Value(transferRoutingSelection228Key{}).(string); ok && !hmac.Equal([]byte(selected), []byte(state.Fingerprint)) {
		return ErrConflict
	}
	return nil
}

// TransferRouteSnapshot228 preserves the pre-existing flag meaning of 0/1.
// A channel ID is accepted only in the separately versioned route policy.
func (s *Store) TransferRouteSnapshot228(ctx context.Context, q fundsReader, kind string) (TransferRoutingState228, TransferRoute228, error) {
	state, err := s.TransferRoutingSnapshot228(ctx, q)
	if err != nil {
		return state, TransferRoute228{}, err
	}
	raw, exists := state.Routes[kind]
	if !exists {
		return state, TransferRoute228{}, ErrInvalid
	}
	route, err := ParseTransferRoute228(raw)
	if err != nil {
		return state, route, err
	}
	if route.Mode == "inherit" && kind != "alipay" {
		var value string
		err = q.QueryRow(ctx, `SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=$1`, "transfer_"+kind).Scan(&value)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return state, route, err
		}
		if value == "0" || value == "false" {
			route.Mode = "disabled"
		}
	}
	return state, route, nil
}

func (s *Store) admitTransferRouting228(ctx context.Context, tx pgx.Tx, kind string) error {
	if err := LockTransferRouting228(ctx, tx, false); err != nil {
		return err
	}
	state, route, err := s.TransferRouteSnapshot228(ctx, tx, kind)
	if err != nil {
		return err
	}
	if err = CheckTransferRoutingSelection228(ctx, state); err != nil {
		return err
	}
	if route.Mode == "disabled" || route.Mode == "manual" {
		return ErrState
	}
	return nil
}

func (g *ProviderFlow) TransferRoutingOriginal228(ctx context.Context, q ParentRuntimeReader, mid int64, no string) (ProviderFlow, Transfer, error) {
	t, err := g.Store.scanTransfer(q.QueryRow(ctx, "SELECT "+transferColumns+transferFrom+" WHERE t.merchant_id=$1 AND t.transfer_no=$2", mid, no))
	if err != nil {
		return *g, t, err
	}
	flow, err := g.parentTransferIntentTx(ctx, q, t)
	return flow, t, err
}
