package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type weixinApplicationVersion struct {
	id, revision, source     int64
	kind                     string
	nonce, ciphertext, proof []byte
	created                  time.Time
	enabled, archived, draft bool
}

func (s *Store) weixinApplicationVersionProof(v weixinApplicationVersion) ([]byte, error) {
	return s.referralDigest("weixin-app186/version", []any{v.id, v.revision, v.source, v.kind, v.nonce, v.ciphertext, v.created.UTC().Format(time.RFC3339Nano), v.enabled, v.archived, v.draft})
}
func weixinApplicationAAD(v weixinApplicationVersion) []byte {
	raw, _ := json.Marshal([]any{"goepay:weixin-app:186", v.id, v.revision, v.source, v.kind})
	return raw
}

func (s *Store) sealWeixinApplication(p WeixinApplicationSpec, at time.Time) (weixinApplicationVersion, error) {
	v := weixinApplicationVersion{id: p.ID, revision: p.Revision, source: p.SourceChannelID, kind: p.Kind, created: at, enabled: p.Enabled, archived: p.Archived, draft: p.Draft()}
	if s.aead == nil || len(s.indexKey) < 32 || validateWeixinApplication(p) != nil {
		return v, ErrState
	}
	raw, e := json.Marshal(struct {
		Spec      WeixinApplicationSpec
		Merchants []int64
		Secret    string
	}{p, p.MerchantIDs, p.AppSecret})
	if e != nil {
		return v, e
	}
	v.nonce = make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(v.nonce); e != nil {
		return v, e
	}
	v.ciphertext = s.aead.Seal(nil, v.nonce, raw, weixinApplicationAAD(v))
	v.proof, e = s.weixinApplicationVersionProof(v)
	return v, e
}

func (s *Store) openWeixinApplication(v weixinApplicationVersion) (WeixinApplicationSpec, error) {
	var p WeixinApplicationSpec
	expected, e := s.weixinApplicationVersionProof(v)
	if e != nil || s.aead == nil || len(v.nonce) != s.aead.NonceSize() || !hmac.Equal(expected, v.proof) {
		return p, ErrConflict
	}
	raw, e := s.aead.Open(nil, v.nonce, v.ciphertext, weixinApplicationAAD(v))
	if e != nil {
		return p, ErrConflict
	}
	var payload struct {
		Spec      WeixinApplicationSpec
		Merchants []int64
		Secret    string
	}
	if json.Unmarshal(raw, &payload) != nil {
		return p, ErrConflict
	}
	p = payload.Spec
	p.MerchantIDs = payload.Merchants
	p.AppSecret = payload.Secret
	if p.ID != v.id || p.Revision != v.revision || p.SourceChannelID != v.source || p.Kind != v.kind || p.Enabled != v.enabled || p.Archived != v.archived || p.Draft() != v.draft || validateWeixinApplication(p) != nil {
		return WeixinApplicationSpec{}, ErrConflict
	}
	p.Fingerprint = hex.EncodeToString(v.proof)
	return p, nil
}

func (s *Store) readWeixinApplicationVersion(ctx context.Context, q fundsReader, id, revision int64) (WeixinApplicationSpec, weixinApplicationVersion, error) {
	var v weixinApplicationVersion
	e := q.QueryRow(ctx, `SELECT app_id,revision,source_channel_id,kind,nonce,ciphertext,proof_hash,created_at,enabled,archived,draft FROM runtime_weixin_app_versions WHERE app_id=$1 AND revision=$2`, id, revision).Scan(&v.id, &v.revision, &v.source, &v.kind, &v.nonce, &v.ciphertext, &v.proof, &v.created, &v.enabled, &v.archived, &v.draft)
	if errors.Is(e, pgx.ErrNoRows) {
		return WeixinApplicationSpec{}, v, ErrNotFound
	}
	if e != nil {
		return WeixinApplicationSpec{}, v, e
	}
	p, e := s.openWeixinApplication(v)
	return p, v, e
}

func (s *Store) weixinApplicationHeadProof(id, revision int64, version []byte, created, updated time.Time) ([]byte, error) {
	return s.referralDigest("weixin-app186/head", []any{id, revision, version, created.UTC().Format(time.RFC3339Nano), updated.UTC().Format(time.RFC3339Nano)})
}

func (s *Store) readWeixinApplicationHead(ctx context.Context, q fundsReader, id int64, lock bool) (WeixinApplicationSpec, weixinApplicationVersion, time.Time, error) {
	var rev, count int64
	var proof []byte
	var created, updated time.Time
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	e := q.QueryRow(ctx, `SELECT revision,proof_hash,created_at,updated_at FROM runtime_weixin_app_heads WHERE id=$1`+suffix, id).Scan(&rev, &proof, &created, &updated)
	if errors.Is(e, pgx.ErrNoRows) {
		return WeixinApplicationSpec{}, weixinApplicationVersion{}, created, ErrNotFound
	}
	if e != nil {
		return WeixinApplicationSpec{}, weixinApplicationVersion{}, created, e
	}
	p, v, e := s.readWeixinApplicationVersion(ctx, q, id, rev)
	if e != nil {
		return p, v, created, e
	}
	expected, e := s.weixinApplicationHeadProof(id, rev, v.proof, created, updated)
	if e != nil || !hmac.Equal(proof, expected) {
		return WeixinApplicationSpec{}, v, created, ErrConflict
	}
	if e = q.QueryRow(ctx, `SELECT count(*) FROM runtime_weixin_app_versions WHERE app_id=$1`, id).Scan(&count); e != nil {
		return p, v, created, e
	}
	if count != rev {
		return WeixinApplicationSpec{}, v, created, ErrConflict
	}
	return p, v, created, nil
}

func (s *Store) weixinApplicationView(p WeixinApplicationSpec, v weixinApplicationVersion) WeixinApplicationView {
	ids := []string{}
	for _, id := range p.MerchantIDs {
		ids = append(ids, strconv.FormatInt(id, 10))
	}
	fingerprint := p.Fingerprint
	if fingerprint == "" {
		fingerprint = hex.EncodeToString(v.proof)
	}
	return WeixinApplicationView{ID: p.ID, Revision: p.Revision, Name: p.Name, Kind: p.Kind, AppID: p.AppID, Enabled: p.Enabled, Archived: p.Archived, SourceChannelID: p.SourceChannelID, MerchantIDs: ids, Path: p.Path, Fingerprint: fingerprint, SecretConfigured: p.AppSecret != "", Draft: p.Draft(), ExecutionReady: p.ExecutionReady(), Origin: "managed"}
}

// This is a trusted internal loader, not an HTTP credential reveal operation.
// New catalogs require a coherent authenticated head; historical payment reads
// restore their already-sealed profile without calling this current loader.
func (s *Store) CurrentWeixinApplicationSpecs(ctx context.Context) ([]WeixinApplicationSpec, error) {
	if s == nil || s.pool == nil || s.aead == nil || len(s.indexKey) < 32 {
		return nil, ErrState
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	return s.currentWeixinApplicationSpecsTx(ctx, tx)
}

func (s *Store) currentWeixinApplicationSpecsTx(ctx context.Context, tx pgx.Tx) ([]WeixinApplicationSpec, error) {
	catalog, e := s.readWeixinApplicationCatalogTx(ctx, tx, nil, false)
	if e != nil {
		return nil, e
	}
	if len(catalog.InvalidIDs) > 0 {
		return nil, ErrConflict
	}
	return catalog.Specs, nil
}
