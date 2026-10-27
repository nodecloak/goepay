package payment

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// A registered parent is captured only at the first NEW route, under the
// existing canonical parent lock. Configuration reads and old orders never
// create an account version or change the original fee/on-off policy.
func (s *Store) captureParentBootstrap(ctx context.Context, tx pgx.Tx, b *parentExecutionBinding) error {
	if b == nil || len(b.bootstrapRaw) == 0 {
		return nil
	}
	h, e := s.readParentHead(ctx, tx, b.account.id)
	if e == nil {
		if !h.legacy || h.source != b.account.id || h.version != 1 || !hmac.Equal(h.fee.identity, b.fee.identity) {
			return ErrConflict
		}
		a, e := s.readParentAccount(ctx, tx, h.id, 1)
		if e != nil {
			return e
		}
		raw, e := s.openParentAccount(a)
		if e != nil {
			return e
		}
		if !hmac.Equal(raw, b.bootstrapRaw) {
			return ErrConflict
		}
		b.account = a
		b.headProof = h.proof
		b.bootstrapRaw = nil
		return nil
	}
	if !errors.Is(e, ErrNotFound) {
		return e
	}
	var origin bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='runtime-parent-channels' AND operation='runtime-parent-write' AND resource_id=$1)`, b.account.id).Scan(&origin); e != nil {
		return e
	}
	if origin {
		return ErrConflict
	}
	a, e := s.sealParentAccount(b.account.id, 1, b.account.id, b.account.kind, b.bootstrapExecution, b.bootstrapRaw)
	if e != nil {
		return e
	}
	h = parentChannelHead{id: a.id, source: a.id, kind: a.kind, name: fmt.Sprintf("原注册通道 #%d", a.id), revision: 1, version: 1, legacy: true, fee: b.fee}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&h.created); e != nil {
		return e
	}
	h.updated = h.created
	h.proof, e = s.parentHeadProof(h)
	if e != nil {
		return e
	}
	if e = s.insertParentHead(ctx, tx, h); e != nil {
		return e
	}
	if e = s.insertParentAccount(ctx, tx, a); e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('merchant',$1,'runtime-parent-router','runtime-parent-channels',$2,'runtime-parent-write','{"revision":"1","account_version":"1","action":"bootstrap"}'::jsonb)`, b.merchant, h.id); e != nil {
		return e
	}
	b.account = a
	b.headProof = h.proof
	b.bootstrapRaw = nil
	return nil
}
