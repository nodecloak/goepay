package payment

import (
	"context"
	adapter "goepay/internal/adapters/payment"
	"sort"
)

type ParentWeixinBatchItem struct {
	ID                         int64
	Expected, ExpectedBindings string
}

func (s *Store) BatchBindParentWeixin(ctx context.Context, actor ParentChannelActor, kind int, profile int64, items []ParentWeixinBatchItem, factories map[int64]adapter.ParentAccountFactory) ([]ParentChannelView, error) {
	if kind != 0 && kind != 1 || profile < 0 || len(items) < 1 || len(items) > 100 {
		return nil, ErrInvalid
	}
	rows := append([]ParentWeixinBatchItem(nil), items...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	for i, item := range rows {
		if item.ID <= 0 || len(item.Expected) != 64 || len(item.ExpectedBindings) != 64 || i > 0 && rows[i-1].ID == item.ID {
			return nil, ErrInvalid
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	for _, item := range rows {
		if err = parentChannelLock(ctx, tx, item.ID); err != nil {
			return nil, err
		}
	}
	out := []ParentChannelView{}
	for _, item := range rows {
		change := adapter.ParentChannelAccountChange{ExpectedBindings: item.ExpectedBindings}
		if kind == 0 {
			change.AppWXMP = &profile
		} else {
			change.AppWXA = &profile
		}
		view, err := s.updateParentChannelTx(ctx, tx, actor, item.ID, ParentChannelPatch{Expected: item.Expected, Account: &change}, factories)
		if err != nil {
			return nil, err
		}
		out = append(out, view)
	}
	if err = actor.check(ctx, tx); err != nil {
		return nil, err
	}
	return out, tx.Commit(ctx)
}
