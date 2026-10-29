package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"time"
)

type parentChannelHead struct {
	id, source, revision, version int64
	kind, name                    string
	archived, legacy              bool
	fee                           RuntimeChannel
	proof                         []byte
	created, updated              time.Time
}
type parentChannelAccount struct {
	id, version, source                             int64
	kind, product                                   string
	nonce, ciphertext, identity, accountHash, proof []byte
}
type ParentChannelView struct {
	Configured      bool                             `json:"configured"`
	ID              int64                            `json:"id,string"`
	SourceChannelID int64                            `json:"source_channel_id,string"`
	Name            string                           `json:"name"`
	PayType         string                           `json:"pay_type"`
	Plugin          string                           `json:"plugin"`
	Revision        int64                            `json:"revision,string"`
	AccountVersion  int64                            `json:"account_version,string"`
	Enabled         bool                             `json:"enabled"`
	Archived        bool                             `json:"archived"`
	Mode            int                              `json:"mode"`
	Rate            *string                          `json:"rate"`
	FeeRateBPS      *int64                           `json:"fee_rate_bps"`
	Fingerprint     string                           `json:"fingerprint"`
	Account         adapter.ParentChannelAccountView `json:"account"`
	CreatedAt       time.Time                        `json:"created_at"`
	UpdatedAt       time.Time                        `json:"updated_at"`
}

func (s *Store) parentHeadProof(h parentChannelHead) ([]byte, error) {
	return s.referralDigest("parentchannel180/head", []any{h.id, h.source, h.kind, h.name, h.revision, h.version, h.archived, h.legacy, h.fee.identity, h.fee.accountHash, h.fee.product, h.fee.methods, h.created, h.updated})
}
func (s *Store) parentAccountAAD(a parentChannelAccount) ([]byte, error) {
	return json.Marshal([]any{"parentchannel180/account", a.id, a.version, a.source, a.kind})
}
func (s *Store) parentAccountProof(a parentChannelAccount) ([]byte, error) {
	return s.referralDigest("parentchannel180/account-source", []any{a.id, a.version, a.source, a.kind, a.nonce, a.ciphertext, a.identity, a.accountHash, a.product})
}
func (s *Store) sealParentAccount(id, version, source int64, kind string, execution adapter.ParentChannelExecution, raw []byte) (parentChannelAccount, error) {
	a := parentChannelAccount{id: id, version: version, source: source, kind: kind}
	if s.aead == nil || len(raw) == 0 || len(raw) > 1048576 || execution.Payment == nil || execution.Payment.ID() != kind {
		return a, ErrInvalid
	}
	binding, e := (ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{id: execution.Payment}}).RuntimeChannel(id)
	if e != nil {
		return a, e
	}
	a.identity, a.accountHash, a.product = binding.identity, binding.accountHash, binding.product
	a.nonce = make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(a.nonce); e != nil {
		return a, e
	}
	aad, e := s.parentAccountAAD(a)
	if e != nil {
		return a, e
	}
	a.ciphertext = s.aead.Seal(nil, a.nonce, raw, aad)
	a.proof, e = s.parentAccountProof(a)
	return a, e
}
func (s *Store) openParentAccount(a parentChannelAccount) ([]byte, error) {
	expected, e := s.parentAccountProof(a)
	if e != nil || s.aead == nil || len(a.nonce) != s.aead.NonceSize() || !hmac.Equal(expected, a.proof) {
		return nil, ErrConflict
	}
	aad, e := s.parentAccountAAD(a)
	if e != nil {
		return nil, e
	}
	raw, e := s.aead.Open(nil, a.nonce, a.ciphertext, aad)
	if e != nil {
		return nil, ErrConflict
	}
	return raw, nil
}

const parentHeadColumns = `id,source_channel_id,pay_type,name,revision,account_version,archived,legacy_origin,fee_identity,fee_account_hash,fee_product,fee_methods,proof_hash,created_at,updated_at`

func (s *Store) readParentHead(ctx context.Context, tx fundsReader, id int64) (parentChannelHead, error) {
	var h parentChannelHead
	var updates int64
	e := tx.QueryRow(ctx, `SELECT `+parentHeadColumns+`,(SELECT count(*) FROM backoffice_audit WHERE resource='runtime-parent-channels' AND operation='runtime-parent-write' AND resource_id=$1) FROM runtime_parent_channels WHERE id=$1`, id).Scan(&h.id, &h.source, &h.kind, &h.name, &h.revision, &h.version, &h.archived, &h.legacy, &h.fee.identity, &h.fee.accountHash, &h.fee.product, &h.fee.methods, &h.proof, &h.created, &h.updated, &updates)
	if errors.Is(e, pgx.ErrNoRows) {
		return h, ErrNotFound
	}
	if e != nil {
		return h, e
	}
	h.fee.id, h.fee.kind = h.id, h.kind
	proof, e := s.parentHeadProof(h)
	if e != nil || updates != h.revision || !hmac.Equal(proof, h.proof) {
		return h, ErrConflict
	}
	return h, nil
}
func (s *Store) readParentAccount(ctx context.Context, tx pgx.Tx, id, version int64) (parentChannelAccount, error) {
	return s.readParentAccountReader(ctx, tx, id, version)
}
func (s *Store) readParentAccountReader(ctx context.Context, tx fundsReader, id, version int64) (parentChannelAccount, error) {
	var a parentChannelAccount
	e := tx.QueryRow(ctx, `SELECT channel_id,version,source_channel_id,pay_type,nonce,ciphertext,execution_identity,account_hash,product,proof_hash FROM runtime_parent_channel_accounts WHERE channel_id=$1 AND version=$2`, id, version).Scan(&a.id, &a.version, &a.source, &a.kind, &a.nonce, &a.ciphertext, &a.identity, &a.accountHash, &a.product, &a.proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return a, ErrConflict
	}
	if e != nil {
		return a, e
	}
	proof, e := s.parentAccountProof(a)
	if e != nil || !hmac.Equal(proof, a.proof) {
		return a, ErrConflict
	}
	return a, nil
}
func (s *Store) parentView(h parentChannelHead, a parentChannelAccount, factory adapter.ParentAccountFactory, policy runtimeChannelPolicy) (ParentChannelView, error) {
	out := ParentChannelView{Configured: true, ID: h.id, SourceChannelID: h.source, Name: h.name, PayType: h.kind, Plugin: h.kind, Revision: h.revision, AccountVersion: h.version, Enabled: policy.enabled, Archived: h.archived, CreatedAt: h.created, UpdatedAt: h.updated}
	raw, e := s.openParentAccount(a)
	if e != nil {
		return out, e
	}
	if factory != nil {
		out.Account, e = factory.Describe(h.id, raw)
		if e != nil {
			return out, ErrConflict
		}
	} else {
		out.Account = adapter.ParentChannelAccountView{Plugin: h.kind, AppTypes: []string{}, Fields: []adapter.ParentChannelFieldState{}}
	}
	dto, e := s.runtimePolicyDTO(h.fee, policy, policy.revision > 0)
	if e != nil {
		return out, e
	}
	out.Mode, out.Rate, out.FeeRateBPS = 0, dto.Rate, dto.FeeRateBPS
	// pay_type and plugin are distinct: h.kind is the channel's pay type, while
	// the account view reports the plugin name (the dedicated adapter's name for
	// a native channel, or the ported plugin's name). The original console shows
	// 支付方式 (pay type) and 支付插件 (plugin) as separate columns/fields.
	if out.Account.Plugin != "" {
		out.Plugin = out.Account.Plugin
	}
	hash, e := s.referralDigest("parentchannel180/public-state", []any{h.proof, a.proof, policy.proof})
	out.Fingerprint = hex.EncodeToString(hash)
	return out, e
}
func parentChannelLock(ctx context.Context, tx pgx.Tx, id int64) error {
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/route/channel/%d", id))
	return e
}
