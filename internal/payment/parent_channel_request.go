package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

// ReadParentChannelRequest waits for the original catalog transaction, then
// reads only this administrator's stable creation request. It never retries it.
func (s *Store) ReadParentChannelRequest(ctx context.Context, actor ParentChannelActor, key string, factories map[int64]adapter.ParentAccountFactory) (ParentChannelView, error) {
	var out ParentChannelView
	if !parentRequestID(key) {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('goepay/runtime-parent/catalog',0))`); e != nil {
		return out, e
	}
	if e = actor.check(ctx, tx); e != nil {
		return out, e
	}
	var id int64
	e = tx.QueryRow(ctx, `SELECT channel_id FROM runtime_parent_channel_requests WHERE actor_name=$1 AND request_key=$2`, actor.User.Username, key).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if e != nil {
		return out, e
	}
	h, e := s.readParentHead(ctx, tx, id)
	if e != nil {
		return out, e
	}
	a, e := s.readParentAccount(ctx, tx, id, h.version)
	if e != nil {
		return out, e
	}
	p, _, e := s.runtimePolicyTx(ctx, tx, h.fee, false)
	if e != nil {
		return out, e
	}
	out, e = s.parentView(h, a, factories[h.source], p)
	if e != nil {
		return out, e
	}
	if e = actor.check(ctx, tx); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
