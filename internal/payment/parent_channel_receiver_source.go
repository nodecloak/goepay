package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
)

type parentReceiverSource struct {
	Version                                             int
	Receiver, Merchant, Channel, AccountVersion, Source int64
	Kind, Product                                       string
	AccountProof, InputHash, ScopeHash, RelationHash    []byte
}

func (s *Store) parentReceiverSchema(ctx context.Context, q fundsReader) (bool, error) {
	ready, e := s.parentPaymentSchema(ctx, q)
	if e != nil {
		return false, e
	}
	var table, origin bool
	if e = q.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.runtime_parent_receiver_sources') IS NOT NULL,EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-parent-receiver-intents')`).Scan(&table, &origin); e != nil {
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
func (s *Store) sealParentReceiverSource(ctx context.Context, tx pgx.Tx, r sharingReceiverRecord, b *parentExecutionBinding, actor TransferBatchActor) error {
	if b == nil {
		return nil
	}
	ready, e := s.parentReceiverSchema(ctx, tx)
	if e != nil {
		return e
	}
	if !ready {
		return ErrConflict
	}
	v := parentReceiverSource{Version: 1, Receiver: r.ID, Merchant: r.Binding.MerchantID, Channel: r.Binding.ChannelID, AccountVersion: b.account.version, Source: b.account.source, Kind: r.Binding.Provider, Product: r.Binding.Product, AccountProof: b.account.proof, InputHash: r.InputHash, ScopeHash: r.AccountHash, RelationHash: r.RelationAccountHash}
	proof, e := s.referralDigest("parentchannel180/receiver-source", v)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO runtime_parent_receiver_sources(receiver_id,channel_id,account_version,facts,proof_hash) VALUES($1,$2,$3,$4::jsonb,$5)`, r.ID, v.Channel, v.AccountVersion, string(raw), proof); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'runtime-parent-receiver-intents',$4,'runtime-parent-receiver-intent',jsonb_build_object('channel_id',$5::text,'account_version',$6::text))`, actor.Role, actor.ID, actor.Name, r.ID, fmt.Sprint(v.Channel), fmt.Sprint(v.AccountVersion))
	return e
}
func (g ProviderFlow) parentReceiverIntentTx(ctx context.Context, q fundsReader, r sharingReceiverRecord) (ProviderFlow, error) {
	ready, e := g.Store.parentReceiverSchema(ctx, q)
	if e != nil || !ready {
		return g, e
	}
	var raw, proof []byte
	var channel, version int64
	e = q.QueryRow(ctx, `SELECT channel_id,account_version,facts,proof_hash FROM runtime_parent_receiver_sources WHERE receiver_id=$1`, r.ID).Scan(&channel, &version, &raw, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		var managed bool
		if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_parent_channels WHERE id=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-parent-channels' AND resource_id=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-parent-receiver-intents' AND resource_id=$2)`, r.Binding.ChannelID, r.ID).Scan(&managed); e != nil {
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
	var v parentReceiverSource
	if len(raw) > 32768 || parentFactsJSON(raw, &v) != nil {
		return g, ErrConflict
	}
	want, e := g.Store.referralDigest("parentchannel180/receiver-source", v)
	if e != nil || !hmac.Equal(want, proof) || v.Version != 1 || v.Receiver != r.ID || v.Merchant != r.Binding.MerchantID || v.Channel != channel || v.Channel != r.Binding.ChannelID || v.AccountVersion != version || v.Kind != r.Binding.Provider || v.Product != r.Binding.Product || !hmac.Equal(v.InputHash, r.InputHash) || !hmac.Equal(v.ScopeHash, r.AccountHash) || !hmac.Equal(v.RelationHash, r.RelationAccountHash) {
		return g, ErrConflict
	}
	var audits int64
	if e = q.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='runtime-parent-receiver-intents' AND operation='runtime-parent-receiver-intent' AND resource_id=$1 AND changes->>'channel_id'=$2 AND changes->>'account_version'=$3`, r.ID, fmt.Sprint(channel), fmt.Sprint(version)).Scan(&audits); e != nil {
		return g, e
	}
	if audits != 1 {
		return g, ErrConflict
	}
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
	out := g.withParentExecution(&parentExecutionBinding{account: a, merchant: v.Merchant}, execution)
	_, binding, e := out.sharingChannel(v.Merchant, v.Channel)
	if e != nil || !g.Store.sharingReceiverBindingMatches(r, binding) {
		return g, ErrConflict
	}
	return out, nil
}
func (g ProviderFlow) parentReceiverIntent(ctx context.Context, r sharingReceiverRecord) (ProviderFlow, error) {
	tx, e := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return g, e
	}
	defer tx.Rollback(ctx)
	out, e := g.parentReceiverIntentTx(ctx, tx, r)
	if e != nil {
		return g, e
	}
	return out, tx.Commit(ctx)
}
