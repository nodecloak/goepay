package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// CheckParentChannelManagementTx admits a typed configuration action against
// the same authenticated real parent ID. It never returns execution secrets.
func (s *Store) CheckParentChannelManagementTx(ctx context.Context, tx pgx.Tx, a ParentChannelActor, id int64) error {
	if id <= 0 {
		return ErrInvalid
	}
	// Match every parent writer: parent lock precedes the durable session lock.
	// This avoids a session revocation queued between opposing lock orders.
	if e := parentChannelLock(ctx, tx, id); e != nil {
		return e
	}
	if e := a.check(ctx, tx); e != nil {
		return e
	}
	h, e := s.readParentHead(ctx, tx, id)
	if e != nil {
		return e
	}
	if h.archived {
		return ErrNotFound
	}
	_, e = s.readParentAccount(ctx, tx, id, h.version)
	return e
}
