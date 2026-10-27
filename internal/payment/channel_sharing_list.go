package payment

import (
	"context"
	"slices"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

type ChannelSharingReceiverPage struct {
	Items      []ChannelSharingReceiverView `json:"items"`
	NextCursor int64                        `json:"next_cursor,string"`
}
type ChannelSharingOrderPage struct {
	Items      []ChannelSharingOrderView `json:"items"`
	NextCursor int64                     `json:"next_cursor,string"`
}
type ChannelSharingChannelView struct {
	ChannelID       int64    `json:"channel_id,string"`
	Provider        string   `json:"provider"`
	Product         string   `json:"product"`
	Methods         []string `json:"methods"`
	ReceiverTypes   []string `json:"receiver_types"`
	ReturnSupported bool     `json:"return_supported"`
	FinishSupported bool     `json:"finish_supported"`
}

func sharingPageIDs(ctx context.Context, tx pgx.Tx, query string, mid, cursor int64, limit int) ([]int64, int64, error) {
	rows, e := tx.Query(ctx, query, mid, cursor, limit+1)
	if e != nil {
		return nil, 0, e
	}
	defer rows.Close()
	ids := make([]int64, 0, limit+1)
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			return nil, 0, e
		}
		ids = append(ids, id)
	}
	if e = rows.Err(); e != nil {
		return nil, 0, e
	}
	if len(ids) > limit {
		return ids[:limit], ids[limit-1], nil
	}
	return ids, 0, nil
}

func (s *Store) ListChannelSharingReceivers(ctx context.Context, mid, cursor int64, limit int, actor TransferBatchActor) (ChannelSharingReceiverPage, error) {
	page := ChannelSharingReceiverPage{Items: []ChannelSharingReceiverView{}}
	if !actor.valid(mid) || cursor < 0 || limit < 1 || limit > 100 {
		return page, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return page, e
	}
	defer tx.Rollback(ctx)
	ids, next, e := sharingPageIDs(ctx, tx, "SELECT id FROM channel_sharing_receivers WHERE merchant_id=$1 AND id>$2 ORDER BY id LIMIT $3", mid, cursor, limit)
	if e != nil {
		return page, e
	}
	for _, id := range ids {
		r, e := scanSharingReceiver(tx.QueryRow(ctx, "SELECT "+sharingReceiverColumns+" FROM channel_sharing_receivers WHERE merchant_id=$1 AND id=$2", mid, id))
		if e != nil {
			return page, e
		}
		input, e := s.openSharingReceiverRecord(r)
		if e != nil {
			return page, e
		}
		view := sharingReceiverView(r, input)
		if r.LastOperation > 0 {
			op, e := scanSharingReceiverOperation(tx.QueryRow(ctx, "SELECT "+sharingReceiverOperationColumns+" FROM channel_sharing_receiver_operations WHERE id=$1 AND receiver_id=$2", r.LastOperation, r.ID))
			if e != nil {
				return page, e
			}
			view.Operation = &op
		}
		page.Items = append(page.Items, view)
	}
	page.NextCursor = next
	return page, tx.Commit(ctx)
}

func (s *Store) ListChannelSharingOrders(ctx context.Context, mid, cursor int64, limit int, actor TransferBatchActor) (ChannelSharingOrderPage, error) {
	page := ChannelSharingOrderPage{Items: []ChannelSharingOrderView{}}
	if !actor.valid(mid) || cursor < 0 || limit < 1 || limit > 100 {
		return page, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return page, e
	}
	defer tx.Rollback(ctx)
	ids, next, e := sharingPageIDs(ctx, tx, "SELECT id FROM channel_sharing_orders WHERE merchant_id=$1 AND id>$2 ORDER BY id LIMIT $3", mid, cursor, limit)
	if e != nil {
		return page, e
	}
	for _, id := range ids {
		r, e := s.scanSharingOrder(ctx, tx, tx.QueryRow(ctx, "SELECT "+sharingOrderColumns+" FROM channel_sharing_orders WHERE merchant_id=$1 AND id=$2", mid, id))
		if e != nil {
			return page, e
		}
		page.Items = append(page.Items, r.ChannelSharingOrderView)
	}
	page.NextCursor = next
	return page, tx.Commit(ctx)
}

func (g ProviderFlow) ChannelSharingChannels(ctx context.Context, mid int64, actor TransferBatchActor) ([]ChannelSharingChannelView, error) {
	views := []ChannelSharingChannelView{}
	if g.Store == nil || !actor.valid(mid) {
		return views, ErrInvalid
	}
	resolved, resolveErr := g.parentCurrentChannel(ctx, mid, 0)
	if resolveErr != nil {
		return nil, resolveErr
	}
	g = resolved
	if e := ctx.Err(); e != nil {
		return views, e
	}
	ids := make([]int64, 0, len(g.Providers))
	for id := range g.Providers {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		p, b, e := g.sharingChannel(mid, id)
		if e != nil {
			continue
		}
		profile, ok := p.(adapter.ChannelSharingPaymentProfile)
		if !ok || !p.ChannelSharingCapabilities().AutoFinishSupported {
			continue
		}
		methods := []string{}
		for _, method := range g.Providers[id].Capabilities().Methods {
			if mode, e := profile.ChannelSharingRefundMode(method); e == nil && mode == "merchant_funded" {
				methods = append(methods, method)
			}
		}
		slices.Sort(methods)
		methods = slices.Compact(methods)
		if len(methods) > 0 {
			caps := p.ChannelSharingCapabilities()
			views = append(views, ChannelSharingChannelView{ChannelID: id, Provider: b.Provider, Product: b.Product, Methods: methods, ReceiverTypes: append([]string{}, caps.ReceiverTypes...), ReturnSupported: caps.ReturnSupported, FinishSupported: caps.FinishSupported})
		}
	}
	return views, nil
}
