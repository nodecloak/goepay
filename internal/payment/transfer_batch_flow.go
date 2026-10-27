package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"slices"
	"time"
)

func (g ProviderFlow) batchProviderMatches(item TransferBatchItem) error {
	p := g.Providers[item.Transfer.ChannelID]
	if p == nil || p.ID() != item.ProviderKind {
		return ErrConflict
	}
	identity, ok := p.(adapter.ApplicationIdentity)
	if !ok || identity.ApplicationID() != item.AppID {
		return ErrConflict
	}
	caps, ok := p.(adapter.TransferCapabilities)
	if !ok {
		return adapter.ErrUnsupported
	}
	supported := false
	for _, typ := range caps.TransferTypes() {
		if typ == item.Transfer.Type {
			supported = true
		}
	}
	if !supported {
		return adapter.ErrUnsupported
	}
	if item.Transfer.Type == "bank" && providerBankProduct(p) != item.BankProduct {
		return ErrConflict
	}
	if v, ok := p.(adapter.TransferValidator); ok {
		if e := v.ValidateTransfer(transferRequest(item.Transfer)); e != nil {
			return e
		}
	}
	return nil
}
func (g ProviderFlow) batchTransferLocal(ctx context.Context, t Transfer) (Transfer, bool, error) {
	var item TransferBatchItem
	item.Transfer = t
	e := g.Store.pool.QueryRow(ctx, "SELECT state,provider_kind,app_id,bank_product FROM transfer_batch_items WHERE transfer_id=$1", t.ID).Scan(&item.SendState, &item.ProviderKind, &item.AppID, &item.BankProduct)
	if errors.Is(e, pgx.ErrNoRows) {
		return t, false, nil
	}
	if e != nil {
		return t, true, e
	}
	if item.SendState == "unsent" || item.SendState == "cancelled" {
		t.Status = item.SendState
		return t, true, nil
	}
	// Read-only recovery does not require write-enabled validators, but it must
	// never query a different application after channel reconfiguration.
	p := g.Providers[t.ChannelID]
	if p == nil || p.ID() != item.ProviderKind {
		return t, true, ErrConflict
	}
	identity, ok := p.(adapter.ApplicationIdentity)
	if !ok || identity.ApplicationID() != item.AppID {
		return t, true, ErrConflict
	}
	return t, false, nil
}
func (g ProviderFlow) claimBatchItem(ctx context.Context, mid, batchID int64, item TransferBatchItem, actor TransferBatchActor, check TransferBatchCheck, execution *batchExecution) (Transfer, bool, error) {
	if !g.paymentWritesEnabled() {
		return Transfer{}, false, ErrLiveDisabled
	}
	if e := g.Store.checkTransferAccount(ctx, g.Store.pool, item.Transfer.ID, g.Providers[item.Transfer.ChannelID]); e != nil {
		return Transfer{}, false, e
	}
	if e := g.batchProviderMatches(item); e != nil {
		return Transfer{}, false, e
	}
	if check != nil {
		if e := check(ctx, item.Transfer.ChannelID); e != nil {
			return Transfer{}, false, e
		}
	}
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return Transfer{}, false, e
	}
	defer tx.Rollback(ctx)
	if g.parentBindings[item.Transfer.ChannelID] != nil {
		var parentOwner int64
		if e = tx.QueryRow(ctx, `SELECT id FROM merchants WHERE id=$1 FOR NO KEY UPDATE`, mid).Scan(&parentOwner); e != nil {
			return Transfer{}, false, e
		}
		if e = parentChannelLock(ctx, tx, item.Transfer.ChannelID); e != nil {
			return Transfer{}, false, e
		}
		if e = g.checkParentTransferWriteTx(ctx, tx, item.Transfer); e != nil {
			return Transfer{}, false, e
		}
	}
	var active bool
	var raw []byte
	if e = tx.QueryRow(ctx, "SELECT enabled,profile FROM merchants WHERE id=$1 FOR NO KEY UPDATE", mid).Scan(&active, &raw); e != nil {
		return Transfer{}, false, e
	}
	var cfg struct {
		Enabled bool `json:"transfer_api_enabled"`
	}
	if json.Unmarshal(raw, &cfg) != nil || !active || !cfg.Enabled {
		return Transfer{}, false, ErrState
	}
	policy, e := readManualTransferPolicy(ctx, tx, mid)
	if e != nil {
		return Transfer{}, false, e
	}
	if !policy.Enabled {
		return Transfer{}, false, ErrState
	}
	t, e := g.Store.scanTransfer(tx.QueryRow(ctx, "SELECT "+transferColumns+transferFrom+" WHERE t.id=$1 AND t.merchant_id=$2 FOR UPDATE OF t", item.Transfer.ID, mid))
	if e != nil {
		return t, false, e
	}
	var state string
	if e = tx.QueryRow(ctx, "SELECT i.state FROM transfer_batch_items i JOIN transfer_batches b ON b.id=i.batch_id WHERE i.transfer_id=$1 AND i.batch_id=$2 AND b.merchant_id=$3 FOR UPDATE OF i", t.ID, batchID, mid).Scan(&state); e != nil {
		return t, false, e
	}
	if state != "unsent" {
		return t, false, nil
	}
	if e = g.Store.checkTransferAccount(ctx, tx, t.ID, g.Providers[t.ChannelID]); e != nil {
		return t, false, e
	}
	if t.LedgerStatus != "pending" || t.AttemptState != "processing" || t.ProviderTransferNo != "" {
		return t, false, ErrState
	}
	if e = g.batchProviderMatches(item); e != nil {
		return t, false, e
	}
	if check != nil {
		if e = check(ctx, t.ChannelID); e != nil {
			return t, false, e
		}
	}
	if e = execution.claim(ctx, tx, mid, batchID, t.ID, actor); e != nil {
		return t, false, e
	}
	if _, e = tx.Exec(ctx, "UPDATE transfer_batch_items SET state='claimed',claimed_at=clock_timestamp() WHERE transfer_id=$1 AND state='unsent'", t.ID); e != nil {
		return t, false, e
	}
	if e = batchEventTx(ctx, tx, batchID, t.ID, "claim", "processing", actor); e != nil {
		return t, false, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO jobs(event_key,kind,payload,run_at) VALUES($1,'transfer.reconcile',jsonb_build_object('transfer_id',$2::bigint),statement_timestamp()+interval '30 seconds') ON CONFLICT DO NOTHING", fmt.Sprintf("batch-transfer:%d:reconcile", t.ID), t.ID); e != nil {
		return t, false, e
	}
	if e = g.checkParentTransferWriteTx(ctx, tx, t); e != nil {
		return t, false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return t, false, e
	}
	return t, true, nil
}
func (s *Store) batchResultEvent(ctx context.Context, batchID, transferID int64, action, state string, actor TransferBatchActor) error {
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, e := s.pool.Begin(work)
	if e != nil {
		return e
	}
	defer tx.Rollback(work)
	if e = batchEventTx(work, tx, batchID, transferID, action, state, actor); e != nil {
		return e
	}
	return tx.Commit(work)
}
func (g ProviderFlow) ExecuteTransferBatch(ctx context.Context, mid, id int64, limit int, actor TransferBatchActor, check TransferBatchCheck) (TransferBatch, error) {
	if g.Store == nil || !actor.valid(mid) || limit < 1 || limit > 100 {
		return TransferBatch{}, ErrInvalid
	}
	if !g.paymentWritesEnabled() {
		return TransferBatch{}, ErrLiveDisabled
	}
	b, e := g.Store.TransferBatch(ctx, mid, id)
	if e != nil {
		return b, e
	}
	execution, e := newBatchExecution(ctx, b, limit)
	if e != nil {
		return b, e
	}
	attempted := 0
	var operationErr error
	for _, item := range b.Items {
		if item.SendState != "unsent" {
			continue
		}
		if execution != nil && !slices.Contains(execution.ids, item.Transfer.ID) {
			continue
		}
		if attempted >= limit {
			break
		}
		if ctx.Err() != nil {
			operationErr = ctx.Err()
			break
		}
		original, err := g.ParentTransferIntent(ctx, item.Transfer)
		if err != nil {
			operationErr = err
			break
		}
		t, owner, err := original.claimBatchItem(ctx, mid, id, item, actor, check, execution)
		if err != nil {
			operationErr = err
			break
		}
		if !owner {
			continue
		}
		attempted++
		if check != nil {
			if err = check(ctx, t.ChannelID); err != nil {
				operationErr = err
				break
			}
		}
		if err = g.Store.checkTransferAccount(ctx, g.Store.pool, t.ID, original.Providers[t.ChannelID]); err != nil {
			operationErr = err
			break
		}
		if err = original.batchProviderMatches(item); err != nil {
			operationErr = err
			break
		}
		p, ok := original.Providers[t.ChannelID].(adapter.TransferProvider)
		if !ok {
			operationErr = adapter.ErrUnsupported
			break
		}
		current, sendErr := original.sendClaimedTransfer(ctx, t, p)
		state := current.Status
		if state == "" {
			state = "unknown"
		}
		if err = g.Store.batchResultEvent(ctx, id, t.ID, "result", state, actor); err != nil {
			operationErr = errors.Join(sendErr, err)
			break
		}
		// Known failures and unknown outcomes belong to this original item. They
		// never gain another send; remaining untouched items are still executable.
	}
	read, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	current, readErr := g.Store.TransferBatch(read, mid, id)
	return current, errors.Join(operationErr, readErr)
}
func (g ProviderFlow) QueryTransferBatch(ctx context.Context, mid, id int64, actor TransferBatchActor) (TransferBatch, error) {
	if g.Store == nil || !actor.valid(mid) {
		return TransferBatch{}, ErrInvalid
	}
	b, e := g.Store.TransferBatch(ctx, mid, id)
	if e != nil {
		return b, e
	}
	var queryErr error
	for _, item := range b.Items {
		if item.SendState != "claimed" {
			continue
		}
		if ctx.Err() != nil {
			queryErr = errors.Join(queryErr, ctx.Err())
			break
		}
		if e = g.Store.batchResultEvent(ctx, id, item.Transfer.ID, "query", "checking", actor); e != nil {
			queryErr = errors.Join(queryErr, e)
			break
		}
		t, err := g.QueryTransfer(ctx, mid, item.Transfer.TransferNo)
		if err != nil {
			queryErr = errors.Join(queryErr, err)
		}
		if e = g.Store.batchResultEvent(ctx, id, item.Transfer.ID, "query", t.Status, actor); e != nil {
			queryErr = errors.Join(queryErr, e)
			break
		}
		if ctx.Err() != nil {
			break
		}
	}
	read, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	current, readErr := g.Store.TransferBatch(read, mid, id)
	return current, errors.Join(queryErr, readErr)
}
