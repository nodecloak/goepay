package sitebrand

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
)

var ErrInvalid = errors.New("invalid branding image")
var ErrConflict = errors.New("branding source conflict")
var ErrNotFound = errors.New("branding image not configured")

const MaxInput = 2 << 20
const settingsLock = int64(7170140001)

type Actor struct {
	User      identity.User
	Token     string
	Delegated bool
}
type Asset struct {
	ID     string `json:"id"`
	MIME   string `json:"mime"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Bytes  []byte `json:"-"`
}
type View struct {
	Revision    string `json:"revision"`
	Fingerprint string `json:"fingerprint"`
	Logo        *Asset `json:"logo"`
	Icon        *Asset `json:"icon"`
}
type snapshot struct {
	Version    int
	Revision   int64
	Logo, Icon string
	ActorID    int64
	ActorName  string
	ChangedAt  time.Time
}
type Store struct {
	pool *pgxpool.Pool
	key  []byte
}

func New(pool *pgxpool.Pool, key []byte) *Store {
	return &Store{pool: pool, key: append([]byte(nil), key...)}
}

type reader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func (s *Store) digest(purpose string, v any) ([]byte, error) {
	if len(s.key) < 32 {
		return nil, ErrConflict
	}
	h := hmac.New(sha256.New, s.key)
	h.Write([]byte("goepay/sitebrand193/" + purpose + "\x00"))
	if e := json.NewEncoder(h).Encode(v); e != nil {
		return nil, e
	}
	return h.Sum(nil), nil
}

type boundedBuffer struct{ bytes.Buffer }

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if len(p) > MaxInput-b.Len() {
		return 0, ErrInvalid
	}
	return b.Buffer.Write(p)
}
func Normalize(raw []byte) (Asset, error) {
	var out Asset
	if len(raw) == 0 || len(raw) >= MaxInput {
		return out, ErrInvalid
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(raw))
	if e != nil || (format != "png" && format != "jpeg" && format != "gif") || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > 2048 || cfg.Height > 2048 || int64(cfg.Width)*int64(cfg.Height) > 1048576 {
		return out, ErrInvalid
	}
	decoded, actual, e := image.Decode(bytes.NewReader(raw))
	if e != nil || actual != format || decoded.Bounds().Dx() != cfg.Width || decoded.Bounds().Dy() != cfg.Height {
		return out, ErrInvalid
	}
	var b boundedBuffer
	if format == "jpeg" {
		out.MIME = "image/jpeg"
		e = jpeg.Encode(&b, decoded, &jpeg.Options{Quality: 85})
	} else {
		out.MIME = "image/png"
		e = png.Encode(&b, decoded)
	}
	if e != nil {
		return Asset{}, ErrInvalid
	}
	sum := sha256.Sum256(b.Bytes())
	out.ID = hex.EncodeToString(sum[:])
	out.Width = cfg.Width
	out.Height = cfg.Height
	out.Bytes = b.Bytes()
	return out, nil
}
func readAsset(ctx context.Context, q reader, id string) (*Asset, error) {
	if id == "" {
		return nil, nil
	}
	if len(id) != 64 {
		return nil, ErrConflict
	}
	a := &Asset{ID: id}
	if e := q.QueryRow(ctx, `SELECT mime,width,height,content FROM site_brand_assets193 WHERE id=$1`, id).Scan(&a.MIME, &a.Width, &a.Height, &a.Bytes); e != nil {
		return nil, ErrConflict
	}
	if len(a.Bytes) == 0 || len(a.Bytes) > MaxInput {
		return nil, ErrConflict
	}
	sum := sha256.Sum256(a.Bytes)
	if hex.EncodeToString(sum[:]) != id {
		return nil, ErrConflict
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(a.Bytes))
	if e != nil || (format != "png" && format != "jpeg") || "image/"+format != a.MIME || cfg.Width != a.Width || cfg.Height != a.Height || cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 2048 || cfg.Height > 2048 || int64(cfg.Width)*int64(cfg.Height) > 1048576 {
		return nil, ErrConflict
	}
	return a, nil
}
func (s *Store) load(ctx context.Context, q reader) (View, snapshot, error) {
	var out View
	var state snapshot
	var tables int
	var registry bool
	if e := q.QueryRow(ctx, `SELECT num_nonnulls(to_regclass(current_schema()||'.site_brand_assets193'),to_regclass(current_schema()||'.site_brand_revisions193'),to_regclass(current_schema()||'.site_brand_head193')),to_regclass(current_schema()||'.schema_migrations')IS NOT NULL`).Scan(&tables, &registry); e != nil {
		return out, state, e
	}
	registered := false
	if registry {
		if e := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name='000107_site_branding.up.sql')`).Scan(&registered); e != nil {
			return out, state, e
		}
	}
	if tables == 0 && !registered {
		return out, state, ErrNotFound
	}
	if tables != 3 || (registry && !registered) {
		return out, state, ErrConflict
	}
	var revision, rows, maximum, audits int64
	if e := q.QueryRow(ctx, `SELECT revision,(SELECT count(*)FROM site_brand_revisions193),(SELECT coalesce(max(revision),0)FROM site_brand_revisions193),(SELECT count(*)FROM backoffice_audit WHERE resource='site-branding193')FROM site_brand_head193 WHERE singleton`).Scan(&revision, &rows, &maximum, &audits); e != nil {
		return out, state, ErrConflict
	}
	if rows != maximum || revision != maximum || audits != rows {
		return out, state, ErrConflict
	}
	if revision > 0 {
		var raw, proof []byte
		if e := q.QueryRow(ctx, `SELECT facts,proof_hash FROM site_brand_revisions193 WHERE revision=$1`, revision).Scan(&raw, &proof); e != nil {
			return out, state, ErrConflict
		}
		if len(raw) > 4096 {
			return out, state, ErrConflict
		}
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if e := decoder.Decode(&state); e != nil {
			return out, state, ErrConflict
		}
		want, e := s.digest("revision", state)
		if e != nil || !hmac.Equal(want, proof) || state.Version != 1 || state.Revision != revision || state.ActorID <= 0 || state.ActorName == "" || state.ChangedAt.IsZero() {
			return out, state, ErrConflict
		}
		var matched int
		if e = q.QueryRow(ctx, `SELECT count(*)FROM backoffice_audit WHERE resource='site-branding193' AND resource_id=$1 AND actor_id=$2 AND changes->>'commitment'=$3`, revision, state.ActorID, hex.EncodeToString(proof)).Scan(&matched); e != nil || matched != 1 {
			return out, state, ErrConflict
		}
	}
	var e error
	out.Logo, e = readAsset(ctx, q, state.Logo)
	if e != nil {
		return out, state, e
	}
	out.Icon, e = readAsset(ctx, q, state.Icon)
	if e != nil {
		return out, state, e
	}
	// An unconfigured old/local renderer may have no console signing key. It
	// may show the original text brand, but cannot obtain a write fingerprint.
	if revision == 0 && len(s.key) < 32 {
		out.Revision = "0"
		return out, state, nil
	}
	fingerprint, e := s.digest("cas", state)
	if e != nil {
		return out, state, e
	}
	out.Fingerprint = hex.EncodeToString(fingerprint)
	out.Revision = strconv.FormatInt(revision, 10)
	return out, state, nil
}
func (s *Store) Read(ctx context.Context, a Actor) (View, error) {
	var out View
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, settingsLock); e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	out, _, e = s.load(ctx, tx)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) Publish(ctx context.Context, a Actor, slot, expected string, raw []byte) (View, error) {
	var out View
	if slot != "logo" && slot != "icon" || len(expected) != 64 {
		return out, ErrInvalid
	}
	image, e := Normalize(raw)
	if e != nil {
		return out, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, settingsLock); e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	before, state, e := s.load(ctx, tx)
	if e != nil {
		return out, e
	}
	if !hmac.Equal([]byte(expected), []byte(before.Fingerprint)) {
		return out, ErrConflict
	}
	if _, e = tx.Exec(ctx, `INSERT INTO site_brand_assets193(id,mime,width,height,content)VALUES($1,$2,$3,$4,$5)ON CONFLICT(id)DO NOTHING`, image.ID, image.MIME, image.Width, image.Height, image.Bytes); e != nil {
		return out, e
	}
	if _, e = readAsset(ctx, tx, image.ID); e != nil {
		return out, e
	}
	state.Version = 1
	state.Revision++
	state.ActorID = a.User.ID
	state.ActorName = a.User.Username
	if slot == "logo" {
		state.Logo = image.ID
	} else {
		state.Icon = image.ID
	}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&state.ChangedAt); e != nil {
		return out, e
	}
	state.ChangedAt = state.ChangedAt.UTC()
	proof, e := s.digest("revision", state)
	if e != nil {
		return out, e
	}
	body, e := json.Marshal(state)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO site_brand_revisions193(revision,facts,proof_hash)VALUES($1,$2::jsonb,$3)`, state.Revision, string(body), proof); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `UPDATE site_brand_head193 SET revision=$1 WHERE singleton`, state.Revision); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',$1,$2,'site-branding193',$3,'brand-publish',jsonb_build_object('slot',$4::text,'commitment',$5::text))`, a.User.ID, a.User.Username, state.Revision, slot, hex.EncodeToString(proof)); e != nil {
		return out, e
	}
	out, _, e = s.load(ctx, tx)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) Public(ctx context.Context, slot string) (*Asset, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	view, _, e := s.load(ctx, tx)
	if e != nil {
		return nil, e
	}
	var out *Asset
	if slot == "logo" {
		out = view.Logo
	} else if slot == "icon" {
		out = view.Icon
	} else {
		return nil, ErrInvalid
	}
	if out == nil {
		return nil, ErrNotFound
	}
	return out, tx.Commit(ctx)
}
