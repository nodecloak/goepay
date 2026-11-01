package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

func weixinApplicationRequestID(value string) bool {
	raw, e := hex.DecodeString(value)
	return e == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
}

func (s *Store) weixinApplicationRequestHash(id int64, in WeixinApplicationInput, archive bool) ([]byte, error) {
	return s.referralDigest("weixin-app186/request-input", struct {
		ID        int64
		Input     WeixinApplicationInput
		Secret    string
		Merchants []int64
		Archive   bool
	}{id, in, in.AppSecret, in.MerchantIDs, archive})
}

func (scope WeixinApplicationScope) validate(ctx context.Context, tx pgx.Tx, p WeixinApplicationSpec, expected string) error {
	if p.SourceChannelID == 0 {
		if len(p.MerchantIDs) != 0 || expected != "" {
			return ErrInvalid
		}
		return nil
	}
	want, ok := scope.SourceFingerprints[p.SourceChannelID]
	if !ok || want == "" || want != expected || scope.CurrentSource == nil || scope.CurrentSource(p.SourceChannelID) != want || scope.Allows == nil {
		return ErrConflict
	}
	for _, mid := range p.MerchantIDs {
		if !scope.Allows(mid, p.SourceChannelID) {
			return ErrState
		}
		var enabled bool
		if e := tx.QueryRow(ctx, `SELECT enabled FROM merchants WHERE id=$1 FOR SHARE`, mid).Scan(&enabled); e != nil {
			return e
		}
		if !enabled {
			return ErrState
		}
	}
	return nil
}

type weixinApplicationReceipt struct {
	request             string
	actor, id, revision int64
	hash, proof         []byte
	created             time.Time
}

func (s *Store) weixinApplicationReceiptProof(r weixinApplicationReceipt) ([]byte, error) {
	return s.referralDigest("weixin-app186/receipt", []any{r.request, r.actor, r.id, r.revision, r.hash, r.created.UTC().Format(time.RFC3339Nano)})
}

func (s *Store) readWeixinApplicationReceipt(ctx context.Context, tx pgx.Tx, actor ParentChannelActor, request string, hash []byte) (WeixinApplicationView, error) {
	var r weixinApplicationReceipt
	e := tx.QueryRow(ctx, `SELECT request_id,actor_id,app_id,revision,request_hash,proof_hash,created_at FROM runtime_weixin_app_requests WHERE request_id=$1`, request).Scan(&r.request, &r.actor, &r.id, &r.revision, &r.hash, &r.proof, &r.created)
	if errors.Is(e, pgx.ErrNoRows) {
		return WeixinApplicationView{}, ErrNotFound
	}
	if e != nil {
		return WeixinApplicationView{}, e
	}
	expected, e := s.weixinApplicationReceiptProof(r)
	if e != nil || r.actor != actor.User.ID || !hmac.Equal(expected, r.proof) || hash != nil && !hmac.Equal(hash, r.hash) {
		return WeixinApplicationView{}, ErrConflict
	}
	p, v, e := s.readWeixinApplicationVersion(ctx, tx, r.id, r.revision)
	if e != nil {
		return WeixinApplicationView{}, e
	}
	return s.weixinApplicationView(p, v), nil
}

// MutateWeixinApplication requires a write-only secret on creation. An empty
// edit retains it only for the same application identity and kind. Archive is
// a new immutable version and remains possible after a source grant is revoked.
func (s *Store) MutateWeixinApplication(ctx context.Context, actor ParentChannelActor, id int64, in WeixinApplicationInput, scope WeixinApplicationScope, archive bool) (WeixinApplicationView, error) {
	var out WeixinApplicationView
	if s == nil || s.pool == nil || s.aead == nil || len(s.indexKey) < 32 || id < 0 || !weixinApplicationRequestID(in.RequestID) || archive && id == 0 {
		return out, ErrInvalid
	}
	in.MerchantIDs = canonicalWeixinApplicationMerchants(in.MerchantIDs)
	hash, e := s.weixinApplicationRequestHash(id, in, archive)
	if e != nil {
		return out, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = actor.check(ctx, tx); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,186))`, in.RequestID); e != nil {
		return out, e
	}
	if out, e = s.readWeixinApplicationReceipt(ctx, tx, actor, in.RequestID, hash); e == nil {
		if e = actor.check(ctx, tx); e != nil {
			return WeixinApplicationView{}, e
		}
		return out, nil
	} else if !errors.Is(e, ErrNotFound) {
		return out, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('goepay/weixin-app186/catalog',186))`); e != nil {
		return out, e
	}
	if id != 0 && scope.ReservedIDs[id] && !archive {
		return out, ErrConflict
	}
	// Merchant rows precede the application head lock. Final scope validation
	// repeats the root callbacks and expiry checks after the last waiting write.
	p := WeixinApplicationSpec{ID: id, Name: in.Name, Kind: in.Kind, AppID: in.AppID, AppSecret: in.AppSecret, Enabled: in.Enabled, SourceChannelID: in.SourceChannelID, MerchantIDs: in.MerchantIDs, Path: in.Path}
	if !archive {
		if e = scope.validate(ctx, tx, p, in.ExpectedSourceFingerprint); e != nil {
			return out, e
		}
	}
	var created, at time.Time
	if id == 0 {
		var count int
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM runtime_weixin_app_heads`).Scan(&count); e != nil {
			return out, e
		}
		if count >= 1000 {
			return out, ErrState
		}
		if in.ExpectedRevision != 0 || in.ExpectedFingerprint != "" || !validWeixinApplicationSecret(in.AppSecret) {
			return out, ErrInvalid
		}
		for attempts := 0; attempts < 10000; attempts++ {
			if e = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('runtime_weixin_app_heads','id'))`).Scan(&p.ID); e != nil {
				return out, e
			}
			if !scope.ReservedIDs[p.ID] {
				break
			}
			p.ID = 0
		}
		if p.ID <= 0 {
			return out, ErrState
		}
		p.Revision = 1
	} else {
		old, version, born, err := s.readWeixinApplicationHead(ctx, tx, id, true)
		if err != nil {
			return out, err
		}
		if old.Revision != in.ExpectedRevision || hex.EncodeToString(version.proof) != in.ExpectedFingerprint || old.Revision == math.MaxInt64 {
			return out, ErrConflict
		}
		if old.Archived {
			return out, ErrState
		}
		created = born
		p.Revision = old.Revision + 1
		if archive {
			p = old
			p.Revision++
			p.Archived = true
			p.Enabled = false
		} else if in.AppSecret == "" {
			if old.AppID != in.AppID || old.Kind != in.Kind {
				return out, ErrInvalid
			}
			p.AppSecret = old.AppSecret
		}
	}
	if validateWeixinApplication(p) != nil {
		return out, ErrInvalid
	}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
		return out, e
	}
	if created.IsZero() {
		created = at
	}
	v, e := s.sealWeixinApplication(p, at)
	if e != nil {
		return out, e
	}
	p.Fingerprint = hex.EncodeToString(v.proof)
	if p.ExecutionReady() && !archive {
		if scope.ValidateProfiles == nil {
			return out, ErrState
		}
		catalog, err := s.readWeixinApplicationCatalogTx(ctx, tx, nil, true)
		if err != nil {
			return out, err
		}
		combined := []WeixinApplicationSpec{}
		for _, other := range catalog.Specs {
			if other.ID != p.ID {
				combined = append(combined, other)
			}
		}
		combined = append(combined, p)
		if e = scope.ValidateProfiles(combined); e != nil {
			return out, e
		}
	}
	head, e := s.weixinApplicationHeadProof(p.ID, p.Revision, v.proof, created, at)
	if e != nil {
		return out, e
	}
	if id == 0 {
		if _, e = tx.Exec(ctx, `INSERT INTO runtime_weixin_app_heads(id,revision,proof_hash,created_at,updated_at) VALUES($1,$2,$3,$4,$5)`, p.ID, p.Revision, head, created, at); e != nil {
			return out, e
		}
	} else {
		if _, e = tx.Exec(ctx, `UPDATE runtime_weixin_app_heads SET revision=$2,proof_hash=$3,updated_at=$4 WHERE id=$1`, p.ID, p.Revision, head, at); e != nil {
			return out, e
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO runtime_weixin_app_versions(app_id,revision,source_channel_id,kind,nonce,ciphertext,proof_hash,created_at,enabled,archived,draft) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, v.id, v.revision, v.source, v.kind, v.nonce, v.ciphertext, v.proof, v.created, v.enabled, v.archived, v.draft); e != nil {
		return out, e
	}
	r := weixinApplicationReceipt{request: in.RequestID, actor: actor.User.ID, id: p.ID, revision: p.Revision, hash: hash, created: at}
	r.proof, e = s.weixinApplicationReceiptProof(r)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO runtime_weixin_app_requests(request_id,actor_id,app_id,revision,request_hash,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, r.request, r.actor, r.id, r.revision, r.hash, r.proof, r.created); e != nil {
		return out, e
	}
	action := "update"
	if id == 0 {
		action = "create"
	}
	if archive {
		action = "archive"
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'weixin-applications',$3,$4,jsonb_build_object('revision',$5::text,'request_id',$6::text))`, actor.User.ID, actor.User.Username, p.ID, action, strings.TrimSpace(jsonNumber(p.Revision)), in.RequestID); e != nil {
		return out, e
	}
	if !archive {
		if e = scope.validate(ctx, tx, p, in.ExpectedSourceFingerprint); e != nil {
			return out, e
		}
	}
	if e = actor.check(ctx, tx); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return s.weixinApplicationView(p, v), nil
}

func jsonNumber(n int64) string { raw, _ := json.Marshal(n); return string(raw) }

func (s *Store) WeixinApplicationReceipt(ctx context.Context, actor ParentChannelActor, request string) (WeixinApplicationView, error) {
	if !weixinApplicationRequestID(request) {
		return WeixinApplicationView{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return WeixinApplicationView{}, e
	}
	defer tx.Rollback(ctx)
	if e = actor.check(ctx, tx); e != nil {
		return WeixinApplicationView{}, e
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,186))`, request); e != nil {
		return WeixinApplicationView{}, e
	}
	out, e := s.readWeixinApplicationReceipt(ctx, tx, actor, request, nil)
	if e != nil {
		return out, e
	}
	if e = actor.check(ctx, tx); e != nil {
		return WeixinApplicationView{}, e
	}
	return out, nil
}

func (s *Store) ListWeixinApplications(ctx context.Context, actor ParentChannelActor) ([]WeixinApplicationView, error) {
	out, invalid, e := s.WeixinApplicationDirectory(ctx, actor)
	if e != nil {
		return nil, e
	}
	if len(invalid) > 0 {
		return nil, ErrConflict
	}
	return out, nil
}

func (s *Store) WeixinApplicationDirectory(ctx context.Context, actor ParentChannelActor) ([]WeixinApplicationView, []int64, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return nil, nil, e
	}
	defer tx.Rollback(ctx)
	check := func() error {
		return identity.CheckOriginalAdminSessionTx(ctx, tx, actor.User, actor.Token, actor.Delegated)
	}
	if e = check(); e != nil {
		return nil, nil, e
	}
	catalog, e := s.readWeixinApplicationCatalogTx(ctx, tx, nil, false)
	if e != nil {
		return nil, nil, e
	}
	out := []WeixinApplicationView{}
	for _, p := range catalog.Specs {
		out = append(out, s.weixinApplicationView(p, weixinApplicationVersion{}))
	}
	if e = check(); e != nil {
		return nil, nil, e
	}
	return out, catalog.InvalidIDs, nil
}
