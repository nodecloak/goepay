package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

type ParentBindingCatalog struct {
	ChannelID       int64 `json:"channel_id,string"`
	SourceChannelID int64 `json:"source_channel_id,string"`
	adapter.ParentWeixinCatalog
	Total int `json:"total"`
}

func (s *Store) ParentChannelBindingCatalog(ctx context.Context, actor ParentChannelActor, id int64, factories map[int64]adapter.ParentAccountFactory) (ParentBindingCatalog, error) {
	out := ParentBindingCatalog{ChannelID: id, SourceChannelID: id}
	if id <= 0 {
		return out, ErrInvalid
	}
	// The durable actor check holds row-share locks through its final clock
	// check. It performs no business writes, but PostgreSQL needs a regular
	// transaction for SELECT FOR SHARE, as on the existing parent catalog.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = actor.check(ctx, tx); err != nil {
		return out, err
	}
	if _, err = s.parentPaymentSchema(ctx, tx); err != nil {
		return out, err
	}
	head, err := s.readParentHead(ctx, tx, id)
	if err == nil {
		if head.archived {
			return out, ErrState
		}
		out.SourceChannelID = head.source
	} else if !errors.Is(err, ErrNotFound) {
		return out, err
	} else {
		var known bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-parent-channels' AND resource_id=$1)`, id).Scan(&known); err != nil {
			return out, err
		}
		if known {
			return out, ErrConflict
		}
	}
	factory := factories[out.SourceChannelID]
	catalog, ok := factory.(interface {
		ParentWeixinCatalog() adapter.ParentWeixinCatalog
	})
	if !ok || factory.PayType() != "wxpay" {
		return out, ErrNotFound
	}
	out.ParentWeixinCatalog = catalog.ParentWeixinCatalog()
	out.Total = len(out.Items)
	if len(out.Fingerprint) != 64 {
		return out, ErrState
	}
	if err = actor.check(ctx, tx); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
