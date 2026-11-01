package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

type parentTransferSource struct {
	Version                                                       int
	TransferID, Merchant, Channel, AccountVersion, Source, Amount int64
	OutNo, Kind, Product                                          string
	AccountProof, ScopeHash, RequestHash, ExecutionHash           []byte
}

func parentTransferProduct(p adapter.Provider, kind string) string {
	if kind == "bank" {
		return providerBankProduct(p)
	}
	product, ok := p.(adapter.TransferProductIdentity)
	if !ok {
		return ""
	}
	return product.TransferProductIdentity()
}

func (s *Store) parentTransferSchema(ctx context.Context, q fundsReader) (bool, error) {
	ready, e := s.parentPaymentSchema(ctx, q)
	if e != nil {
		return false, e
	}
	var table, origin bool
	if e = q.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.runtime_parent_transfer_sources') IS NOT NULL,EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-parent-transfer-intents')`).Scan(&table, &origin); e != nil {
		return false, e
	}
	if ready && table {
		return true, nil
	}
	if ready || table || origin {
		return false, ErrConflict
	}
	return false, nil
}

func (s *Store) sealParentTransferSource(ctx context.Context, tx pgx.Tx, t Transfer, b *parentExecutionBinding, p adapter.Provider) error {
	if b == nil {
		return nil
	}
	ready, e := s.parentTransferSchema(ctx, tx)
	if e != nil {
		return e
	}
	if !ready {
		return ErrConflict
	}
	product := parentTransferProduct(p, t.Type)
	if product == "" {
		return ErrConflict
	}
	v := parentTransferSource{Version: 1, TransferID: t.ID, Merchant: t.MerchantID, Channel: t.ChannelID, AccountVersion: b.account.version, Source: b.account.source, Amount: t.Amount, OutNo: t.TransferNo, Kind: p.ID(), Product: product, AccountProof: b.account.proof}
	if e = tx.QueryRow(ctx, `SELECT a.account_hash,p.request_hash FROM provider_transfer_accounts a LEFT JOIN provider_transfer_policies p ON p.transfer_id=a.transfer_id WHERE a.transfer_id=$1`, t.ID).Scan(&v.ScopeHash, &v.RequestHash); e != nil {
		return e
	}
	_, v.ExecutionHash, e = s.transferPolicyHashes(t.MerchantID, transferRequest(t))
	if e != nil {
		return e
	}
	proof, e := s.referralDigest("parentchannel180/transfer-source", v)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO runtime_parent_transfer_sources(transfer_id,channel_id,account_version,facts,proof_hash) VALUES($1,$2,$3,$4::jsonb,$5)`, t.ID, t.ChannelID, v.AccountVersion, string(raw), proof); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('merchant',$1,'runtime-parent-transfer','runtime-parent-transfer-intents',$2,'runtime-parent-transfer-intent',jsonb_build_object('channel_id',$3::text,'account_version',$4::text))`, t.MerchantID, t.ID, fmt.Sprint(t.ChannelID), fmt.Sprint(v.AccountVersion))
	return e
}

func (g ProviderFlow) parentTransferIntentTx(ctx context.Context, q fundsReader, t Transfer) (ProviderFlow, error) {
	ready, e := g.Store.parentTransferSchema(ctx, q)
	if e != nil || !ready {
		return g, e
	}
	var raw, proof []byte
	var channel, version int64
	e = q.QueryRow(ctx, `SELECT channel_id,account_version,facts,proof_hash FROM runtime_parent_transfer_sources WHERE transfer_id=$1`, t.ID).Scan(&channel, &version, &raw, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		var managed bool
		if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_parent_channels WHERE id=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-parent-channels' AND resource_id=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-parent-transfer-intents' AND resource_id=$2)`, t.ChannelID, t.ID).Scan(&managed); e != nil {
			return g, e
		}
		if managed {
			return g, ErrConflict
		}
		return g, nil
	}
	if e != nil {
		return g, e
	}
	var v parentTransferSource
	if len(raw) > 32768 || parentFactsJSON(raw, &v) != nil {
		return g, ErrConflict
	}
	want, e := g.Store.referralDigest("parentchannel180/transfer-source", v)
	if e != nil || !hmac.Equal(want, proof) || v.Version != 1 || v.TransferID != t.ID || v.Channel != channel || v.Channel != t.ChannelID || v.AccountVersion != version || v.Merchant != t.MerchantID || v.Amount != t.Amount || v.OutNo != t.TransferNo {
		return g, ErrConflict
	}
	var scope, request []byte
	var audit int64
	if e = q.QueryRow(ctx, `SELECT a.account_hash,p.request_hash,(SELECT count(*) FROM backoffice_audit WHERE resource='runtime-parent-transfer-intents' AND resource_id=$1 AND operation='runtime-parent-transfer-intent' AND changes->>'channel_id'=$2 AND changes->>'account_version'=$3) FROM provider_transfer_accounts a LEFT JOIN provider_transfer_policies p ON p.transfer_id=a.transfer_id WHERE a.transfer_id=$1`, t.ID, fmt.Sprint(channel), fmt.Sprint(version)).Scan(&scope, &request, &audit); e != nil {
		return g, ErrConflict
	}
	_, executionHash, hashErr := g.Store.transferPolicyHashes(t.MerchantID, transferRequest(t))
	if hashErr != nil || !hmac.Equal(executionHash, v.ExecutionHash) || audit != 1 || !hmac.Equal(scope, v.ScopeHash) || len(v.RequestHash) > 0 && !hmac.Equal(request, v.RequestHash) {
		return g, ErrConflict
	}
	// readParentAccount only needs a reader, so the immutable account can be
	// restored inside an existing batch/settlement transaction without nesting.
	a, e := g.Store.readParentAccountReader(ctx, q, channel, version)
	if e != nil {
		return g, e
	}
	if a.source != v.Source || a.kind != v.Kind || !hmac.Equal(a.proof, v.AccountProof) {
		return g, ErrConflict
	}
	execution, e := g.restoreParentExecution(a)
	if e != nil {
		return g, e
	}
	if parentTransferProduct(execution.Payment, t.Type) != v.Product {
		return g, ErrConflict
	}
	if e = g.Store.checkTransferAccount(ctx, q, t.ID, execution.Payment); e != nil {
		return g, e
	}
	return g.withParentExecution(&parentExecutionBinding{account: a, merchant: t.MerchantID}, execution), nil
}

func (g ProviderFlow) prepareParentTransferChannelTx(ctx context.Context, tx pgx.Tx, mid, channel int64) error {
	b := g.parentBindings[channel]
	if b == nil || !b.granted {
		return nil
	}
	if e := parentChannelLock(ctx, tx, channel); e != nil {
		return e
	}
	if e := g.Store.captureParentBootstrap(ctx, tx, b); e != nil {
		return e
	}
	if e := g.Store.checkParentExecutionCurrent(ctx, tx, b, mid); e != nil {
		return e
	}
	p, _, e := g.Store.runtimePolicyTx(ctx, tx, b.fee, false)
	if e != nil {
		return e
	}
	if !p.enabled {
		return ErrState
	}
	return nil
}

func (g ProviderFlow) checkParentTransferWriteTx(ctx context.Context, tx pgx.Tx, t Transfer) error {
	b := g.parentBindings[t.ChannelID]
	if b == nil {
		return nil
	}
	if !g.paymentWritesEnabled() {
		return ErrLiveDisabled
	}
	return g.Store.checkParentRuntimeWriteTx(ctx, tx, b, t.MerchantID, t.ChannelID)
}

func (s *Store) checkParentRuntimeWriteTx(ctx context.Context, tx pgx.Tx, b *parentExecutionBinding, mid, channel int64) error {
	if b == nil {
		return nil
	}
	if b.merchant != mid || b.account.id != channel {
		return ErrConflict
	}
	if b.live != nil && !b.live() {
		return ErrLiveDisabled
	}
	if b.authorized != nil && !b.authorized() {
		return ErrConflict
	}
	h, e := s.readParentHead(ctx, tx, channel)
	if e != nil {
		return e
	}
	if h.archived || h.source != b.account.source || h.kind != b.account.kind {
		return ErrState
	}
	p, _, e := s.runtimePolicyTx(ctx, tx, h.fee, false)
	if e != nil {
		return e
	}
	if !p.enabled {
		return ErrState
	}
	if !h.legacy {
		var source int64
		var proof []byte
		if e = tx.QueryRow(ctx, `SELECT source_channel_id,proof_hash FROM runtime_parent_channel_grants WHERE channel_id=$1 AND merchant_id=$2`, channel, mid).Scan(&source, &proof); e != nil {
			return ErrConflict
		}
		want, e := s.referralDigest("parentchannel180/grant", []any{h.id, mid, h.source})
		if e != nil || source != h.source || !hmac.Equal(want, proof) {
			return ErrConflict
		}
	}
	return nil
}

func (g ProviderFlow) parentCurrentChannel(ctx context.Context, mid, channel int64) (ProviderFlow, error) {
	if len(g.ParentFactories) == 0 {
		return g, nil
	}
	out, allowed, e := g.RuntimeParentChannels(ctx, mid, g.MerchantChannels)
	if e != nil {
		return g, e
	}
	if channel > 0 && !parentHasChannel(allowed, channel) {
		return g, ErrConflict
	}
	return out, nil
}

func (g ProviderFlow) checkParentTransferWrite(ctx context.Context, t Transfer) error {
	if g.parentBindings[t.ChannelID] == nil {
		return nil
	}
	tx, e := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = g.checkParentTransferWriteTx(ctx, tx, t); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func (g ProviderFlow) parentTransferBatchRuntime(ctx context.Context, mid int64, no string, inputs []TransferBatchInput) (ProviderFlow, error) {
	var oldID int64
	e := g.Store.pool.QueryRow(ctx, `SELECT id FROM transfer_batches WHERE merchant_id=$1 AND external_no=$2`, mid, no).Scan(&oldID)
	if e == nil {
		batch, e := g.Store.TransferBatch(ctx, mid, oldID)
		if e != nil {
			return g, e
		}
		for _, item := range batch.Items {
			g, e = g.ParentTransferIntent(ctx, item.Transfer)
			if e != nil {
				return g, e
			}
		}
		return g, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return g, e
	}
	if len(g.ParentFactories) == 0 {
		return g, nil
	}
	out, allowed, e := g.RuntimeParentChannels(ctx, mid, g.MerchantChannels)
	if e != nil {
		return g, e
	}
	for _, input := range inputs {
		if !parentHasChannel(allowed, input.ChannelID) {
			return g, ErrConflict
		}
	}
	return out, nil
}

func (g ProviderFlow) ParentTransferIntent(ctx context.Context, t Transfer) (ProviderFlow, error) {
	if g.Store == nil {
		return g, ErrInvalid
	}
	tx, e := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return g, e
	}
	defer tx.Rollback(ctx)
	out, e := g.parentTransferIntentTx(ctx, tx, t)
	if e != nil {
		return g, e
	}
	return out, tx.Commit(ctx)
}

func (g ProviderFlow) parentTransferRuntime(ctx context.Context, mid, channel int64, no string) (ProviderFlow, error) {
	t, e := g.Store.Transfer(ctx, mid, no)
	if e == nil {
		return g.ParentTransferIntent(ctx, t)
	}
	if !errors.Is(e, ErrNotFound) {
		return g, e
	}
	if len(g.ParentFactories) == 0 {
		return g, nil
	}
	out, allowed, e := g.RuntimeParentChannels(ctx, mid, g.MerchantChannels)
	if e != nil {
		return g, e
	}
	if !parentHasChannel(allowed, channel) || out.Providers[channel] == nil {
		return g, ErrConflict
	}
	return out, nil
}

func (g ProviderFlow) beginRuntimeParentTransfer(ctx context.Context, mid, channel int64, no string, r adapter.TransferRequest, validate func(adapter.TransferRequest) error, product, scope string, api bool) (Transfer, bool, error) {
	b := g.parentBindings[channel]
	if b == nil || !b.granted {
		return g.Store.beginTransfer(ctx, mid, channel, no, r, validate, product, scope, api)
	}
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return Transfer{}, false, e
	}
	defer tx.Rollback(ctx)
	var ownerID int64
	if e = tx.QueryRow(ctx, `SELECT id FROM merchants WHERE id=$1 FOR NO KEY UPDATE`, mid).Scan(&ownerID); e != nil {
		return Transfer{}, false, e
	}
	if e = parentChannelLock(ctx, tx, channel); e != nil {
		return Transfer{}, false, e
	}
	if e = g.Store.captureParentBootstrap(ctx, tx, b); e != nil {
		return Transfer{}, false, e
	}
	// Existing durable intents are resolved before this path. A new transfer
	// always rechecks the current version under the canonical parent lock.
	if e = g.Store.checkParentExecutionCurrent(ctx, tx, b, mid); e != nil {
		return Transfer{}, false, e
	}
	p, _, e := g.Store.runtimePolicyTx(ctx, tx, b.fee, false)
	if e != nil || !p.enabled {
		if e == nil {
			e = ErrState
		}
		return Transfer{}, false, e
	}
	var selectedFee *merchantTransferFeeSelection
	t, owner, e := g.Store.beginTransferTxWithFeeSelection(ctx, tx, mid, channel, no, r, validate, product, scope, &selectedFee, api)
	if e != nil {
		return t, false, e
	}
	if owner {
		if e = g.Store.sealParentTransferSource(ctx, tx, t, b, g.Providers[channel]); e != nil {
			return t, false, e
		}
	}
	if selectedFee != nil {
		if e = selectedFee.checkDeadline(ctx, tx); e != nil {
			return t, false, e
		}
	}
	if !g.paymentWritesEnabled() || b.authorized != nil && !b.authorized() {
		return t, false, ErrLiveDisabled
	}
	return t, owner, tx.Commit(ctx)
}

func (g ProviderFlow) parentSettlementRuntime(ctx context.Context, id, channel int64) (ProviderFlow, error) {
	p, e := g.Store.SettlementPayout(ctx, id)
	if e == nil {
		return g.ParentTransferIntent(ctx, p.Transfer)
	}
	if !errors.Is(e, ErrNotFound) {
		return g, e
	}
	if len(g.ParentFactories) == 0 {
		return g, nil
	}
	var mid int64
	if e = g.Store.pool.QueryRow(ctx, `SELECT merchant_id FROM ledger_requests WHERE id=$1 AND kind='settlements'`, id).Scan(&mid); e != nil {
		return g, e
	}
	out, allowed, e := g.RuntimeParentChannels(ctx, mid, g.MerchantChannels)
	if e != nil {
		return g, e
	}
	if channel > 0 && !parentHasChannel(allowed, channel) {
		return g, ErrConflict
	}
	return out, nil
}
