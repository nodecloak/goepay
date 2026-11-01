package payment

import (
	"context"
	"crypto/hmac"
	"time"

	"github.com/jackc/pgx/v5"
)

type WeixinApplicationCatalog struct {
	Specs       []WeixinApplicationSpec
	InvalidIDs  []int64
	InactiveIDs []int64
	ArchivedIDs []int64
}

// One coherent query reads the requested current heads, versions and revision
// counts. A bad row is quarantined by ID; it never invalidates unrelated apps.
// A nil selector means a management catalog, an empty selector means no work.
func (s *Store) ReadWeixinApplicationCatalog(ctx context.Context, ids []int64) (WeixinApplicationCatalog, error) {
	if s == nil || s.pool == nil || s.aead == nil || len(s.indexKey) < 32 {
		return WeixinApplicationCatalog{}, ErrState
	}
	if ids != nil && len(ids) == 0 {
		return WeixinApplicationCatalog{Specs: []WeixinApplicationSpec{}}, nil
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return WeixinApplicationCatalog{}, e
	}
	defer tx.Rollback(ctx)
	return s.readWeixinApplicationCatalogTx(ctx, tx, ids, true)
}

func (s *Store) readWeixinApplicationCatalogTx(ctx context.Context, tx pgx.Tx, ids []int64, skipInactive bool) (WeixinApplicationCatalog, error) {
	out := WeixinApplicationCatalog{Specs: []WeixinApplicationSpec{}, InvalidIDs: []int64{}, InactiveIDs: []int64{}, ArchivedIDs: []int64{}}
	if s.aead == nil || len(s.indexKey) < 32 {
		return out, ErrState
	}
	rows, e := tx.Query(ctx, `WITH totals AS (SELECT app_id,count(*) AS n FROM runtime_weixin_app_versions WHERE ($1::bigint[] IS NULL OR app_id=ANY($1)) GROUP BY app_id)
SELECT h.id,h.revision,h.proof_hash,h.created_at,h.updated_at,v.app_id,v.revision,v.source_channel_id,v.kind,v.nonce,v.ciphertext,v.proof_hash,v.created_at,v.enabled,v.archived,v.draft,COALESCE(t.n,0)
FROM runtime_weixin_app_heads h LEFT JOIN runtime_weixin_app_versions v ON v.app_id=h.id AND v.revision=h.revision LEFT JOIN totals t ON t.app_id=h.id
WHERE ($1::bigint[] IS NULL OR h.id=ANY($1)) ORDER BY h.id`, ids)
	if e != nil {
		return out, e
	}
	defer rows.Close()
	seen := map[int64]bool{}
	for rows.Next() {
		var id, rev, count int64
		var head []byte
		var born, updated time.Time
		var vid, vrev, source *int64
		var kind *string
		var at *time.Time
		var enabled, archived, draft *bool
		var nonce, ciphertext, proof []byte
		if e = rows.Scan(&id, &rev, &head, &born, &updated, &vid, &vrev, &source, &kind, &nonce, &ciphertext, &proof, &at, &enabled, &archived, &draft, &count); e != nil {
			return out, e
		}
		seen[id] = true
		if vid == nil || vrev == nil || source == nil || kind == nil || at == nil || enabled == nil || archived == nil || draft == nil || count != rev || rev <= 0 {
			out.InvalidIDs = append(out.InvalidIDs, id)
			continue
		}
		v := weixinApplicationVersion{id: *vid, revision: *vrev, source: *source, kind: *kind, nonce: nonce, ciphertext: ciphertext, proof: proof, created: *at, enabled: *enabled, archived: *archived, draft: *draft}
		want, err := s.weixinApplicationHeadProof(id, rev, proof, born, updated)
		if err != nil || !hmac.Equal(head, want) {
			out.InvalidIDs = append(out.InvalidIDs, id)
			continue
		}
		want, err = s.weixinApplicationVersionProof(v)
		if err != nil || !hmac.Equal(proof, want) || len(nonce) != s.aead.NonceSize() {
			out.InvalidIDs = append(out.InvalidIDs, id)
			continue
		}
		if v.archived {
			out.ArchivedIDs = append(out.ArchivedIDs, id)
		} else if !v.enabled || v.draft {
			out.InactiveIDs = append(out.InactiveIDs, id)
		}
		if skipInactive && (!v.enabled || v.archived || v.draft) {
			continue
		}
		p, err := s.openWeixinApplication(v)
		if err != nil {
			out.InvalidIDs = append(out.InvalidIDs, id)
			continue
		}
		out.Specs = append(out.Specs, p)
	}
	if e = rows.Err(); e != nil {
		return out, e
	}
	return out, nil
}
