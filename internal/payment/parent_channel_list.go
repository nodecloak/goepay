package payment

import (
	"context"
	"crypto/hmac"
	"github.com/jackc/pgx/v5"
)

type parentChannelStored struct {
	head       parentChannelHead
	account    parentChannelAccount
	policy     runtimeChannelPolicy
	configured bool
}

func (s *Store) readParentStates(ctx context.Context, tx ParentRuntimeReader) ([]parentChannelStored, error) {
	states := []parentChannelStored{}
	var e error
	var tables int
	var migrations bool
	if e = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM unnest(ARRAY['runtime_parent_channels','runtime_parent_channel_accounts','runtime_parent_channel_grants','runtime_parent_channel_requests']) t(name) WHERE to_regclass(current_schema()||'.'||name) IS NOT NULL),to_regclass(current_schema()||'.schema_migrations') IS NOT NULL`).Scan(&tables, &migrations); e != nil {
		return nil, e
	}
	if tables != 4 {
		var registered, origin bool
		if migrations {
			if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000103_runtime_parent_channel_accounts.up.sql')`).Scan(&registered); e != nil {
				return nil, e
			}
		}
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-parent-channels' AND operation='runtime-parent-write')`).Scan(&origin); e != nil {
			return nil, e
		}
		if tables != 0 || registered || origin {
			return nil, ErrConflict
		}
		return states, nil
	}

	rows, e := tx.Query(ctx, `WITH history AS (SELECT resource_id,count(*) n FROM backoffice_audit WHERE resource='runtime-parent-channels' AND operation='runtime-parent-write' GROUP BY resource_id),policies AS (SELECT resource_id,count(*) n FROM backoffice_audit WHERE resource='runtime-channel-policies' AND operation='runtime-channel-policy-update' GROUP BY resource_id)
 SELECT h.id,h.source_channel_id,h.pay_type,h.name,h.revision,h.account_version,h.archived,h.legacy_origin,h.fee_identity,h.fee_account_hash,h.fee_product,h.fee_methods,h.proof_hash,h.created_at,h.updated_at,COALESCE(history.n,0),
 a.channel_id,a.version,a.source_channel_id,a.pay_type,a.nonce,a.ciphertext,a.execution_identity,a.account_hash,a.product,a.proof_hash,
 COALESCE(p.channel_id,0),COALESCE(p.pay_type,''),COALESCE(p.identity_hash,''::bytea),COALESCE(p.retained_bps,10000),COALESCE(p.mode,0),COALESCE(p.enabled,true),COALESCE(p.revision,0),COALESCE(p.proof_hash,''::bytea),COALESCE(policies.n,0)
 FROM runtime_parent_channels h LEFT JOIN runtime_parent_channel_accounts a ON a.channel_id=h.id AND a.version=h.account_version LEFT JOIN runtime_channel_policies p ON p.channel_id=h.id LEFT JOIN history ON history.resource_id=h.id LEFT JOIN policies ON policies.resource_id=h.id ORDER BY h.id LIMIT 1001`)
	if e != nil {
		return nil, e
	}

	for rows.Next() {
		var h parentChannelHead
		var a parentChannelAccount
		var p runtimeChannelPolicy
		var headUpdates, policyUpdates int64
		if e = rows.Scan(&h.id, &h.source, &h.kind, &h.name, &h.revision, &h.version, &h.archived, &h.legacy, &h.fee.identity, &h.fee.accountHash, &h.fee.product, &h.fee.methods, &h.proof, &h.created, &h.updated, &headUpdates, &a.id, &a.version, &a.source, &a.kind, &a.nonce, &a.ciphertext, &a.identity, &a.accountHash, &a.product, &a.proof, &p.channel, &p.kind, &p.identity, &p.retained, &p.mode, &p.enabled, &p.revision, &p.proof, &policyUpdates); e != nil {
			rows.Close()
			return nil, ErrConflict
		}
		h.fee.id, h.fee.kind = h.id, h.kind
		configured := p.channel != 0
		if !configured {
			if !h.legacy || h.source != h.id || policyUpdates != 0 {
				rows.Close()
				return nil, ErrConflict
			}
			p = runtimeChannelPolicy{channel: h.id, kind: h.kind, identity: h.fee.identity, retained: 10000, enabled: true}
		}
		hp, he := s.parentHeadProof(h)
		ap, ae := s.parentAccountProof(a)
		pp, pe := s.runtimePolicyProof(p)
		if he != nil || ae != nil || pe != nil || h.revision != headUpdates || p.revision != policyUpdates || h.revision <= 0 || configured && p.revision <= 0 || p.mode != 0 || p.retained < 0 || p.retained > 10000 || p.channel != h.id || p.kind != h.kind || a.id != h.id || a.version != h.version || a.source != h.source || a.kind != h.kind || !hmac.Equal(hp, h.proof) || !hmac.Equal(ap, a.proof) || configured && !hmac.Equal(pp, p.proof) || !hmac.Equal(p.identity, h.fee.identity) {
			rows.Close()
			return nil, ErrConflict
		}

		states = append(states, parentChannelStored{head: h, account: a, policy: p, configured: configured})
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return nil, e
	}
	if len(states) > 1000 {
		return nil, ErrState
	}
	return states, nil
}
func (g ProviderFlow) ParentChannelViews(ctx context.Context, actor ParentChannelActor) ([]ParentChannelView, error) {
	out := []ParentChannelView{}
	if g.Store == nil {
		return nil, ErrInvalid
	}
	tx, e := g.Store.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = actor.check(ctx, tx); e != nil {
		return nil, e
	}
	states, e := g.Store.readParentStates(ctx, tx)
	if e != nil {
		return nil, e
	}
	for _, state := range states {
		h, a, p := state.head, state.account, state.policy
		view, e := g.Store.parentView(h, a, g.ParentFactories[h.source], p)
		if e != nil {
			return nil, e
		}
		view.Enabled = view.Enabled && !h.archived
		out = append(out, view)
	}
	if e = actor.check(ctx, tx); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
