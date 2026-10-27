package payment

import (
	"context"
	"crypto/hmac"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"math"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

type ParentChannelCreate struct {
	Name, Rate, ExpectedSource, RequestID string
	Mode                                  int
	Account                               adapter.ParentChannelAccountChange
	CopyFrom                              int64
	ExpectedCopy                          string
}

func parentRequestID(s string) bool {
	if len(s) != 32 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
func validParentName(name string) bool {
	if name == "" || len(name) > 120 || !utf8.ValidString(name) {
		return false
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}
func (s *Store) insertParentHead(ctx context.Context, tx pgx.Tx, h parentChannelHead) error {
	_, e := tx.Exec(ctx, `INSERT INTO runtime_parent_channels(id,source_channel_id,pay_type,name,revision,account_version,archived,legacy_origin,fee_identity,fee_account_hash,fee_product,fee_methods,proof_hash,created_at,updated_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, h.id, h.source, h.kind, h.name, h.revision, h.version, h.archived, h.legacy, h.fee.identity, h.fee.accountHash, h.fee.product, h.fee.methods, h.proof, h.created, h.updated)
	return e
}
func (s *Store) insertParentAccount(ctx context.Context, tx pgx.Tx, a parentChannelAccount) error {
	_, e := tx.Exec(ctx, `INSERT INTO runtime_parent_channel_accounts(channel_id,version,source_channel_id,pay_type,nonce,ciphertext,execution_identity,account_hash,product,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, a.id, a.version, a.source, a.kind, a.nonce, a.ciphertext, a.identity, a.accountHash, a.product, a.proof)
	return e
}
func (s *Store) CreateParentChannel(ctx context.Context, actor ParentChannelActor, t ParentChannelTemplate, in ParentChannelCreate, reserved []int64) (ParentChannelView, error) {
	var out ParentChannelView
	in.Name = strings.TrimSpace(in.Name)
	retained, e := ParseChannelRetainedRate(in.Rate)
	if e != nil || in.Mode != 0 || !validParentName(in.Name) || !parentRequestID(in.RequestID) || t.source <= 0 || t.factory == nil || len(t.merchants) == 0 || len(in.ExpectedSource) != 64 || !hmac.Equal([]byte(in.ExpectedSource), []byte(t.fingerprint)) {
		return out, ErrInvalid
	}
	if in.CopyFrom < 0 || in.CopyFrom > 0 && len(in.ExpectedCopy) != 64 || in.CopyFrom == 0 && in.ExpectedCopy != "" {
		return out, ErrInvalid
	}
	request, e := s.referralDigest("parentchannel180/create-request", []any{actor.User.ID, actor.User.Username, in.RequestID, t.fingerprint, in.Name, retained, in.Mode, in.Account, in.CopyFrom, in.ExpectedCopy})
	if e != nil {
		return out, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('goepay/runtime-parent/catalog',0))`); e != nil {
		return out, e
	}
	var previous int64
	var oldRequest []byte
	e = tx.QueryRow(ctx, `SELECT channel_id,request_hash FROM runtime_parent_channel_requests WHERE actor_name=$1 AND request_key=$2`, actor.User.Username, in.RequestID).Scan(&previous, &oldRequest)
	if e == nil {
		if e = parentChannelLock(ctx, tx, previous); e != nil {
			return out, e
		}
		if e = actor.check(ctx, tx); e != nil {
			return out, e
		}
		if !hmac.Equal(request, oldRequest) {
			return out, ErrConflict
		}
		h, e := s.readParentHead(ctx, tx, previous)
		if e != nil {
			return out, e
		}
		if h.revision != 1 || h.archived {
			return out, ErrConflict
		}
		a, e := s.readParentAccount(ctx, tx, h.id, h.version)
		if e != nil {
			return out, e
		}
		policy, configured, e := s.runtimePolicyTx(ctx, tx, h.fee, false)
		if e != nil {
			return out, e
		}
		if !configured || policy.revision != 1 {
			return out, ErrConflict
		}
		out, e = s.parentView(h, a, t.factory, policy)
		if e != nil {
			return out, e
		}
		if e = actor.check(ctx, tx); e != nil {
			return out, e
		}
		return out, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	// Lock merchant identities before parent/session locks, matching ordinary
	// merchant edits and route admission. No future uncreated merchant is granted.
	rows, e := tx.Query(ctx, `SELECT id FROM merchants WHERE id=ANY($1::bigint[]) ORDER BY id FOR KEY SHARE`, t.merchants)
	if e != nil {
		return out, e
	}
	merchants := []int64{}
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return out, e
		}
		merchants = append(merchants, id)
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return out, e
	}
	if len(merchants) == 0 {
		return out, ErrState
	}
	var highest, last int64
	var called bool
	for _, id := range reserved {
		if id > highest {
			highest = id
		}
	}
	var stored int64
	if e = tx.QueryRow(ctx, `SELECT COALESCE(max(id),0) FROM runtime_parent_channels`).Scan(&stored); e != nil {
		return out, e
	}
	if stored > highest {
		highest = stored
	}
	if e = tx.QueryRow(ctx, `SELECT last_value,is_called FROM runtime_parent_channel_ids`).Scan(&last, &called); e != nil {
		return out, e
	}
	if !called {
		last--
	}
	if last > highest {
		highest = last
	}
	if highest == math.MaxInt64 {
		return out, ErrInvalid
	}
	id := highest + 1
	if _, e = tx.Exec(ctx, `SELECT setval('runtime_parent_channel_ids',$1,false)`, id); e != nil {
		return out, e
	}
	if e = tx.QueryRow(ctx, `SELECT nextval('runtime_parent_channel_ids')`).Scan(&id); e != nil {
		return out, e
	}
	locks := []int64{t.source, id}
	if in.CopyFrom > 0 && in.CopyFrom != t.source {
		locks = append(locks, in.CopyFrom)
	}
	sort.Slice(locks, func(i, j int) bool { return locks[i] < locks[j] })
	for _, target := range locks {
		if e = parentChannelLock(ctx, tx, target); e != nil {
			return out, e
		}
	}
	if e = actor.check(ctx, tx); e != nil {
		return out, e
	}
	var execution adapter.ParentChannelExecution
	var raw []byte
	if in.CopyFrom == 0 {
		execution, raw, e = t.factory.NewSnapshot(id, in.Account)
	} else {
		original, err := s.readParentHead(ctx, tx, in.CopyFrom)
		if err != nil {
			return out, err
		}
		if original.archived || original.source != t.source || original.kind != t.kind {
			return out, ErrConflict
		}
		account, err := s.readParentAccount(ctx, tx, original.id, original.version)
		if err != nil {
			return out, err
		}
		policy, _, err := s.runtimePolicyTx(ctx, tx, original.fee, false)
		if err != nil {
			return out, err
		}
		view, err := s.parentView(original, account, t.factory, policy)
		if err != nil {
			return out, err
		}
		if !hmac.Equal([]byte(view.Fingerprint), []byte(in.ExpectedCopy)) {
			return out, ErrConflict
		}
		// A copy keeps the original managed channel's owner snapshot. The
		// source's current operator list may have grown since that channel was
		// created; it authorizes new creation, not expansion during a copy.
		if !original.legacy {
			rows, err := tx.Query(ctx, `SELECT merchant_id,source_channel_id,proof_hash FROM runtime_parent_channel_grants WHERE channel_id=$1 ORDER BY merchant_id`, original.id)
			if err != nil {
				return out, err
			}
			granted := map[int64]bool{}
			for rows.Next() {
				var mid, source int64
				var proof []byte
				if err = rows.Scan(&mid, &source, &proof); err != nil {
					rows.Close()
					return out, err
				}
				want, hashErr := s.referralDigest("parentchannel180/grant", []any{original.id, mid, original.source})
				if hashErr != nil || source != original.source || !hmac.Equal(want, proof) {
					rows.Close()
					return out, ErrConflict
				}
				granted[mid] = true
			}
			rows.Close()
			if err = rows.Err(); err != nil {
				return out, err
			}
			if len(granted) == 0 {
				return out, ErrConflict
			}
			retainedOwners := make([]int64, 0, len(merchants))
			for _, mid := range merchants {
				if granted[mid] {
					retainedOwners = append(retainedOwners, mid)
				}
			}
			if len(retainedOwners) == 0 {
				return out, ErrState
			}
			merchants = retainedOwners
		}
		originalRaw, err := s.openParentAccount(account)
		if err != nil {
			return out, err
		}
		execution, raw, e = t.factory.CopySnapshot(original.id, id, originalRaw, in.Account)
	}
	if e != nil {
		return out, ErrInvalid
	}
	account, e := s.sealParentAccount(id, 1, t.source, t.kind, execution, raw)
	if e != nil {
		return out, e
	}
	fee, e := (ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{id: execution.Payment}}).RuntimeChannel(id)
	if e != nil {
		return out, e
	}
	head := parentChannelHead{id: id, source: t.source, kind: t.kind, name: in.Name, revision: 1, version: 1, fee: fee}
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
	proofs := make([][]byte, 0, len(merchants))
	for _, mid := range merchants {
		proof, e := s.referralDigest("parentchannel180/grant", []any{id, mid, t.source})
		if e != nil {
			return out, e
		}
		proofs = append(proofs, proof)
	}
	if _, e = tx.Exec(ctx, `INSERT INTO runtime_parent_channel_grants(channel_id,merchant_id,source_channel_id,proof_hash) SELECT $1,mid,$3,proof FROM unnest($2::bigint[],$4::bytea[]) AS grants(mid,proof)`, id, merchants, t.source, proofs); e != nil {
		return out, e
	}
	// New IDs are registered but explicitly off. Reuse the exact 96 policy/MAC
	// representation; parent account sources do not change fee proof encoding.
	policy := runtimeChannelPolicy{channel: id, kind: t.kind, identity: fee.identity, retained: retained, mode: 0, enabled: false, revision: 1}
	policy.proof, e = s.runtimePolicyProof(policy)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO runtime_channel_policies(channel_id,pay_type,identity_hash,retained_bps,mode,enabled,revision,proof_hash) VALUES($1,$2,$3,$4,0,false,1,$5)`, id, t.kind, fee.identity, retained, policy.proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'runtime-channel-policies',$3,'runtime-channel-policy-update',jsonb_build_object('retained_bps',$4::text,'revision','1','enabled',false,'mode',0))`, actor.User.ID, actor.User.Username, id, fmt.Sprint(retained)); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO runtime_parent_channel_requests(actor_name,request_key,channel_id,request_hash) VALUES($1,$2,$3,$4)`, actor.User.Username, in.RequestID, id, request); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'runtime-parent-channels',$3,'runtime-parent-write',jsonb_build_object('revision','1','account_version','1','source_channel_id',$4::text,'action','create'))`, actor.User.ID, actor.User.Username, id, fmt.Sprint(t.source)); e != nil {
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
