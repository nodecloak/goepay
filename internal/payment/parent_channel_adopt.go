package payment

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
)

// AdoptParentChannel explicitly versions an already authorized registered
// account for administration. It does not invent an old execution source or
// change the legacy fee policy, and never contacts a provider.
func (s *Store) AdoptParentChannel(ctx context.Context, actor ParentChannelActor, t ParentChannelTemplate, expected string) (ParentChannelView, error) {
	var out ParentChannelView
	if t.source <= 0 || t.factory == nil || t.binding.id != t.source || !hmac.Equal([]byte(t.fingerprint), []byte(expected)) {
		return out, ErrConflict
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = parentChannelLock(ctx, tx, t.source); e != nil {
		return out, e
	}
	if e = actor.check(ctx, tx); e != nil {
		return out, e
	}
	if _, e = s.readParentHead(ctx, tx, t.source); !errors.Is(e, ErrNotFound) {
		if e == nil {
			return out, ErrConflict
		}
		return out, e
	}
	var origin bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-parent-channels' AND resource_id=$1)`, t.source).Scan(&origin); e != nil {
		return out, e
	}
	if origin {
		return out, ErrConflict
	}
	execution, raw, e := t.factory.BootstrapSnapshot(t.source)
	if e != nil {
		return out, ErrState
	}
	account, e := s.sealParentAccount(t.source, 1, t.source, t.kind, execution, raw)
	if e != nil {
		return out, e
	}
	head := parentChannelHead{id: t.source, source: t.source, kind: t.kind, name: fmt.Sprintf("原注册通道 #%d", t.source), revision: 1, version: 1, legacy: true, fee: t.binding}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&head.created); e != nil {
		return out, e
	}
	head.updated = head.created
	head.proof, e = s.parentHeadProof(head)
	if e != nil {
		return out, e
	}
	if e = s.insertParentHead(ctx, tx, head); e != nil {
		return out, e
	}
	if e = s.insertParentAccount(ctx, tx, account); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'runtime-parent-channels',$3,'runtime-parent-write','{"revision":"1","account_version":"1","action":"adopt"}'::jsonb)`, actor.User.ID, actor.User.Username, t.source); e != nil {
		return out, e
	}
	policy, _, e := s.runtimePolicyTx(ctx, tx, head.fee, false)
	if e != nil {
		return out, e
	}
	out, e = s.parentView(head, account, t.factory, policy)
	if e != nil {
		return out, e
	}
	if e = actor.check(ctx, tx); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
