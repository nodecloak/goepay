package pagetemplate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"image"
)

type Actor struct {
	Role, Class, Name string
	ID                int64
}
type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type ListView struct {
	Type     string     `json:"type"`
	Revision int64      `json:"revision,string"`
	Selected string     `json:"selected"`
	Items    []Template `json:"items"`
}
type ConfigView struct {
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	Revision int64          `json:"revision,string"`
	Config   map[string]any `json:"config"`
	Fields   []Field        `json:"fields"`
	Fallback string         `json:"fallback,omitempty"`
}

func allowed(a Actor) bool {
	return a.Role == "admin" && a.Class == "super" && a.ID > 0 && a.Name != ""
}

type reader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Store) List(ctx context.Context, kind string, a Actor) (ListView, error) {
	v := ListView{Type: kind}
	if !allowed(a) {
		return v, ErrForbidden
	}
	list, e := Templates(kind)
	if e != nil {
		return v, e
	}
	v.Items = list
	if s.pool == nil {
		return v, ErrNotFound
	}
	e = s.pool.QueryRow(ctx, "SELECT revision,selected_name FROM page_template_heads WHERE kind=$1", kind).Scan(&v.Revision, &v.Selected)
	return v, e
}
func (s *Store) Config(ctx context.Context, kind, name string, a Actor) (ConfigView, error) {
	v := ConfigView{Type: kind, Name: name, Fields: Fields(kind)}
	if !allowed(a) {
		return v, ErrForbidden
	}
	if _, ok := Lookup(kind, name); !ok {
		return v, ErrInvalid
	}
	var raw []byte
	if s.pool == nil {
		return v, ErrNotFound
	}
	if e := s.pool.QueryRow(ctx, `SELECT h.revision,c.config FROM page_template_heads h LEFT JOIN page_template_configs c ON c.kind=h.kind AND c.name=$2 WHERE h.kind=$1`, kind, name).Scan(&v.Revision, &raw); e != nil {
		return v, e
	}
	cfg := DefaultConfig(kind, name)
	if len(raw) > 0 {
		parsed, e := ParseConfig(kind, name, raw)
		if e != nil {
			v.Fallback = "invalid_config"
		} else {
			cfg = parsed
		}
	}
	v.Config = cfg.Public(kind)
	return v, nil
}
func audit(ctx context.Context, tx pgx.Tx, a Actor, kind, name, operation string, revision int64, digest string) error {
	id := map[string]int64{"home": 1, "pay": 2, "qrpay": 3}[kind]
	changes, _ := json.Marshal(map[string]any{"type": kind, "name": name, "revision": revision, "digest": digest})
	_, e := tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES($1,$2,$3,'page-templates',$4,$5,$6::jsonb)`, a.Role, a.ID, a.Name, id, operation, string(changes))
	return e
}
func (s *Store) SaveConfig(ctx context.Context, kind, name string, expected int64, raw json.RawMessage, a Actor) (ConfigView, error) {
	var v ConfigView
	if !allowed(a) {
		return v, ErrForbidden
	}
	cfg, e := ParseConfig(kind, name, raw)
	if e != nil || expected < 0 {
		return v, ErrInvalid
	}
	if s.pool == nil {
		return v, ErrNotFound
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return v, e
	}
	defer tx.Rollback(ctx)
	var revision int64
	if e = tx.QueryRow(ctx, "SELECT revision FROM page_template_heads WHERE kind=$1 FOR UPDATE", kind).Scan(&revision); e != nil {
		return v, e
	}
	if revision != expected {
		return v, ErrConflict
	}
	if cfg.LogoAssetID != "" {
		if _, e = readAsset(ctx, tx, cfg.LogoAssetID); e != nil {
			return v, ErrInvalid
		}
	}
	normalized, _ := json.Marshal(cfg.Public(kind))
	next := revision + 1
	if next <= revision {
		return v, ErrInvalid
	}
	if _, e = tx.Exec(ctx, `INSERT INTO page_template_configs(kind,name,config,revision)VALUES($1,$2,$3::jsonb,$4) ON CONFLICT(kind,name)DO UPDATE SET config=EXCLUDED.config,revision=EXCLUDED.revision,updated_at=now()`, kind, name, string(normalized), next); e != nil {
		return v, e
	}
	if _, e = tx.Exec(ctx, "UPDATE page_template_heads SET revision=$2,updated_at=now() WHERE kind=$1", kind, next); e != nil {
		return v, e
	}
	digest := sha256.Sum256(normalized)
	if e = audit(ctx, tx, a, kind, name, "configure", next, hex.EncodeToString(digest[:])); e != nil {
		return v, e
	}
	if e = tx.Commit(ctx); e != nil {
		return v, e
	}
	return ConfigView{Type: kind, Name: name, Revision: next, Config: cfg.Public(kind), Fields: Fields(kind)}, nil
}
func (s *Store) Select(ctx context.Context, kind, name string, expected int64, a Actor) (ListView, error) {
	var v ListView
	if !allowed(a) {
		return v, ErrForbidden
	}
	list, e := Templates(kind)
	if e != nil || expected < 0 {
		return v, ErrInvalid
	}
	if _, ok := Lookup(kind, name); !ok {
		return v, ErrInvalid
	}
	if s.pool == nil {
		return v, ErrNotFound
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return v, e
	}
	defer tx.Rollback(ctx)
	var revision int64
	if e = tx.QueryRow(ctx, "SELECT revision FROM page_template_heads WHERE kind=$1 FOR UPDATE", kind).Scan(&revision); e != nil {
		return v, e
	}
	if revision != expected {
		return v, ErrConflict
	}
	var raw []byte
	e = tx.QueryRow(ctx, "SELECT config FROM page_template_configs WHERE kind=$1 AND name=$2", kind, name).Scan(&raw)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return v, e
	}
	if len(raw) > 0 {
		cfg, err := ParseConfig(kind, name, raw)
		if err != nil {
			return v, ErrConflict
		}
		if cfg.LogoAssetID != "" {
			if _, e = readAsset(ctx, tx, cfg.LogoAssetID); e != nil {
				return v, ErrConflict
			}
		}
	}
	next := revision + 1
	if next <= revision {
		return v, ErrInvalid
	}
	if _, e = tx.Exec(ctx, "UPDATE page_template_heads SET selected_name=$2,revision=$3,updated_at=now() WHERE kind=$1", kind, name, next); e != nil {
		return v, e
	}
	if e = audit(ctx, tx, a, kind, name, "select", next, ""); e != nil {
		return v, e
	}
	if e = tx.Commit(ctx); e != nil {
		return v, e
	}
	return ListView{Type: kind, Revision: next, Selected: name, Items: list}, nil
}
func fallback(kind, reason string, revision int64) Resolved {
	return Resolved{Type: kind, Name: "default", Revision: revision, Config: DefaultConfig(kind, "default"), Fallback: reason}
}
func (s *Store) Resolve(ctx context.Context, kind string) (Resolved, error) {
	if !validKind(kind) {
		return Resolved{}, ErrInvalid
	}
	if s.pool == nil {
		return fallback(kind, "store_unavailable", 0), nil
	}
	var revision int64
	var name string
	var raw []byte
	e := s.pool.QueryRow(ctx, `SELECT h.revision,h.selected_name,c.config FROM page_template_heads h LEFT JOIN page_template_configs c ON c.kind=h.kind AND c.name=h.selected_name WHERE h.kind=$1`, kind).Scan(&revision, &name, &raw)
	if e != nil {
		return fallback(kind, "store_unavailable", 0), nil
	}
	if _, ok := Lookup(kind, name); !ok {
		return fallback(kind, "unknown_template", revision), nil
	}
	cfg := DefaultConfig(kind, name)
	if len(raw) > 0 {
		cfg, e = ParseConfig(kind, name, raw)
		if e != nil {
			return fallback(kind, "invalid_config", revision), nil
		}
	}
	if cfg.LogoAssetID != "" {
		if _, e = readAsset(ctx, s.pool, cfg.LogoAssetID); e != nil {
			return fallback(kind, "asset_unavailable", revision), nil
		}
	}
	return Resolved{Type: kind, Name: name, Revision: revision, Config: cfg}, nil
}
func readAsset(ctx context.Context, q reader, id string) (Asset, error) {
	var a Asset
	if !assetPattern.MatchString(id) {
		return a, ErrNotFound
	}
	e := q.QueryRow(ctx, "SELECT id,mime,width,height,content FROM page_template_assets WHERE id=$1", id).Scan(&a.ID, &a.MIME, &a.Width, &a.Height, &a.Bytes)
	if errors.Is(e, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if e != nil {
		return a, e
	}
	digest := sha256.Sum256(a.Bytes)
	cfg, format, e := image.DecodeConfig(bytes.NewReader(a.Bytes))
	if e != nil || hex.EncodeToString(digest[:]) != a.ID || len(a.Bytes) > MaxImageOutput || cfg.Width != a.Width || cfg.Height != a.Height || a.Width < 1 || a.Height < 1 || a.Width > 2048 || a.Height > 2048 || int64(a.Width)*int64(a.Height) > 1048576 || (format != "png" && format != "jpeg") || a.MIME != "image/"+format {
		return Asset{}, ErrInvalid
	}
	return a, nil
}
func (s *Store) Upload(ctx context.Context, raw []byte, a Actor) (Asset, error) {
	if !allowed(a) {
		return Asset{}, ErrForbidden
	}
	asset, e := NormalizeImage(raw)
	if e != nil {
		return Asset{}, e
	}
	if s.pool == nil {
		return Asset{}, ErrNotFound
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Asset{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "INSERT INTO page_template_assets(id,mime,width,height,content)VALUES($1,$2,$3,$4,$5)ON CONFLICT(id)DO NOTHING", asset.ID, asset.MIME, asset.Width, asset.Height, asset.Bytes); e != nil {
		return Asset{}, e
	}
	if e = audit(ctx, tx, a, "", "", "upload-image", 0, asset.ID); e != nil {
		return Asset{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return Asset{}, e
	}
	return s.Asset(ctx, asset.ID, a)
}
func (s *Store) Asset(ctx context.Context, id string, a Actor) (Asset, error) {
	if !allowed(a) {
		return Asset{}, ErrForbidden
	}
	if s.pool == nil {
		return Asset{}, ErrNotFound
	}
	return readAsset(ctx, s.pool, id)
}
func (s *Store) PublishedAsset(ctx context.Context, id string) (Asset, error) {
	if !assetPattern.MatchString(id) {
		return Asset{}, ErrNotFound
	}
	for _, kind := range []string{"home", "pay", "qrpay"} {
		r, e := s.Resolve(ctx, kind)
		if e != nil {
			return Asset{}, e
		}
		if r.Fallback == "" && r.Config.LogoAssetID == id {
			return readAsset(ctx, s.pool, id)
		}
	}
	return Asset{}, ErrNotFound
}
