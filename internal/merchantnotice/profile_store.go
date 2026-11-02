package merchantnotice

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/businesssms231"
	"goepay/internal/connectconfig"
	"goepay/internal/voiceconfig230"
	"io"
	"time"
)

type Store struct {
	connect231 *connectconfig.Store
	pool       *pgxpool.Pool
	aead       cipher.AEAD
	macKey     []byte
	voice      *voiceconfig230.Store
	sms        *businesssms231.Store
}

// Each bounded context derives its own AES and integrity keys. No credentials
// are loaded and no database or network work occurs in this constructor.
func NewStore(pool *pgxpool.Pool, key []byte) (*Store, error) {
	if pool == nil || len(key) != 32 {
		return nil, ErrInvalid
	}
	derive := func(purpose string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte("goepay/merchant-notice80/key/v1/" + purpose))
		return m.Sum(nil)
	}
	block, e := aes.NewCipher(derive("encryption"))
	if e != nil {
		return nil, ErrInvalid
	}
	aead, e := cipher.NewGCM(block)
	if e != nil {
		return nil, ErrInvalid
	}
	voice, e := voiceconfig230.FromMaster(pool, key)
	if e != nil {
		return nil, e
	}
	sms, e := businesssms231.FromMaster(pool, key)
	if e != nil {
		return nil, e
	}
	checkout := sha256.Sum256(append([]byte("checkout-signing:"), key...))
	connect, e := connectconfig.NewStore(pool, checkout[:])
	if e != nil {
		return nil, e
	}
	return &Store{pool: pool, aead: aead, macKey: derive("integrity"), voice: voice, sms: sms, connect231: connect}, nil
}
func (s *Store) available() bool {
	return s != nil && s.pool != nil && s.aead != nil && len(s.macKey) == 32
}
func (s *Store) digest(domain string, raw []byte) []byte {
	m := hmac.New(sha256.New, s.macKey)
	m.Write([]byte("goepay/merchant-notice80/" + domain + "/v1\x00"))
	m.Write(raw)
	return m.Sum(nil)
}

type profileRecord struct {
	ID, MerchantID, Revision, PreviousID        int64
	PreviousHash, Cipher, BodyHash, ProfileHash []byte
	Actor                                       Actor
	CreatedAt                                   time.Time
}

func profileAAD(r profileRecord) []byte {
	raw, _ := json.Marshal([]any{"notice-profile80/v1", r.ID, r.MerchantID, r.Revision, r.PreviousID, hex.EncodeToString(r.PreviousHash), hex.EncodeToString(r.BodyHash)})
	return raw
}
func (s *Store) profileProof(r profileRecord) []byte {
	checksum := sha256.Sum256(r.Cipher)
	raw, _ := json.Marshal([]any{r.ID, r.MerchantID, r.Revision, r.PreviousID, hex.EncodeToString(r.PreviousHash), hex.EncodeToString(r.BodyHash), hex.EncodeToString(checksum[:]), r.Actor.Role, r.Actor.ID, r.Actor.Name, r.Actor.AdminClass, r.CreatedAt.UTC().Format(time.RFC3339Nano)})
	return s.digest("profile-header", raw)
}
func profileAudit(r profileRecord) []byte {
	raw, _ := json.Marshal(map[string]any{"profile_hash": hex.EncodeToString(r.ProfileHash), "revision": r.Revision})
	return raw
}

func noticeMerchant(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, mid int64, lock bool) (bool, string, error) {
	query := "SELECT enabled,COALESCE(profile->>'email',''),COALESCE(jsonb_typeof(profile->'email'),'missing') FROM merchants WHERE id=$1"
	if lock {
		query += " FOR UPDATE"
	}
	var active bool
	var email, kind string
	e := q.QueryRow(ctx, query, mid).Scan(&active, &email, &kind)
	if errors.Is(e, pgx.ErrNoRows) {
		return false, "", ErrNotFound
	}
	if e != nil {
		return false, "", e
	}
	if kind != "missing" && kind != "string" || !validNoticeEmail(email) {
		return false, "", ErrConflict
	}
	return active, email, nil
}

func (s *Store) readProfile(ctx context.Context, tx pgx.Tx, mid int64) (profileRecord, profileData, error) {
	var r profileRecord
	var headHash []byte
	var headRevision, highest int64
	e := tx.QueryRow(ctx, `SELECT p.id,p.merchant_id,p.revision,COALESCE(p.previous_id,0),p.previous_hash,p.payload_cipher,p.body_hash,p.profile_hash,p.actor_role,p.actor_id,p.actor_name,p.actor_class,p.created_at,c.profile_hash,c.revision,
 (SELECT max(q.revision) FROM merchant_notice_profiles q WHERE q.merchant_id=c.merchant_id)
 FROM merchant_notice_current c JOIN merchant_notice_profiles p ON p.id=c.profile_id WHERE c.merchant_id=$1`, mid).Scan(&r.ID, &r.MerchantID, &r.Revision, &r.PreviousID, &r.PreviousHash, &r.Cipher, &r.BodyHash, &r.ProfileHash, &r.Actor.Role, &r.Actor.ID, &r.Actor.Name, &r.Actor.AdminClass, &r.CreatedAt, &headHash, &headRevision, &highest)
	if errors.Is(e, pgx.ErrNoRows) {
		var partial bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM merchant_notice_profiles WHERE merchant_id=$1) OR EXISTS(SELECT 1 FROM merchant_notice_current WHERE merchant_id=$1)", mid).Scan(&partial); e != nil {
			return r, profileData{}, e
		}
		if partial {
			return r, profileData{}, ErrConflict
		}
		return r, profileData{}, ErrNotFound
	}
	if e != nil {
		return r, profileData{}, e
	}
	if r.MerchantID != mid || r.Revision <= 0 || r.Revision != headRevision || r.Revision != highest || !r.Actor.valid(mid, true) || !hmac.Equal(r.ProfileHash, headHash) || !hmac.Equal(r.ProfileHash, s.profileProof(r)) || r.PreviousID == 0 != (r.Revision == 1) {
		return r, profileData{}, ErrConflict
	}
	return s.validateProfileRecord(ctx, tx, r)
}

func (s *Store) readProfileVersion(ctx context.Context, tx pgx.Tx, id int64) (profileRecord, profileData, error) {
	var r profileRecord
	err := tx.QueryRow(ctx, `SELECT id,merchant_id,revision,COALESCE(previous_id,0),previous_hash,payload_cipher,body_hash,profile_hash,actor_role,actor_id,actor_name,actor_class,created_at FROM merchant_notice_profiles WHERE id=$1`, id).Scan(&r.ID, &r.MerchantID, &r.Revision, &r.PreviousID, &r.PreviousHash, &r.Cipher, &r.BodyHash, &r.ProfileHash, &r.Actor.Role, &r.Actor.ID, &r.Actor.Name, &r.Actor.AdminClass, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, profileData{}, ErrConflict
	}
	if err != nil {
		return r, profileData{}, err
	}
	return s.validateProfileRecord(ctx, tx, r)
}

func (s *Store) validateProfileRecord(ctx context.Context, tx pgx.Tx, r profileRecord) (profileRecord, profileData, error) {
	if r.ID <= 0 || r.MerchantID <= 0 || r.Revision <= 0 || !r.Actor.valid(r.MerchantID, true) || !hmac.Equal(r.ProfileHash, s.profileProof(r)) || (r.PreviousID == 0) != (r.Revision == 1) {
		return r, profileData{}, ErrConflict
	}
	var e error
	if r.PreviousID > 0 {
		var linked bool
		if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM merchant_notice_profiles WHERE id=$1 AND merchant_id=$2 AND revision=$3 AND profile_hash=$4)", r.PreviousID, r.MerchantID, r.Revision-1, r.PreviousHash).Scan(&linked); e != nil {
			return r, profileData{}, e
		}
		if !linked {
			return r, profileData{}, ErrConflict
		}
	} else if len(r.PreviousHash) != 0 {
		return r, profileData{}, ErrConflict
	}
	var audits int
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='merchant-notice-profiles' AND resource_id=$1 AND operation='save' AND (actor_role,actor_id,actor_name)=($2,$3,$4) AND changes=$5::jsonb`, r.ID, r.Actor.Role, r.Actor.ID, r.Actor.Name, profileAudit(r)).Scan(&audits); e != nil {
		return r, profileData{}, e
	}
	if audits != 1 {
		return r, profileData{}, ErrConflict
	}
	n := s.aead.NonceSize()
	if len(r.Cipher) < n+s.aead.Overhead() {
		return r, profileData{}, ErrConflict
	}
	raw, e := s.aead.Open(nil, r.Cipher[:n], r.Cipher[n:], profileAAD(r))
	if e != nil || !hmac.Equal(r.BodyHash, s.digest("profile-body", raw)) {
		return r, profileData{}, ErrConflict
	}
	var data profileData
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&data) != nil || decoder.Decode(new(any)) != io.EOF || !data.valid() {
		return r, profileData{}, ErrConflict
	}
	return r, data, nil
}

func (s *Store) Settings(ctx context.Context, mid int64, actor Actor) (SettingsView, error) {
	if !actor.valid(mid, false) {
		return SettingsView{}, ErrForbidden
	}
	if !s.available() {
		return SettingsView{}, ErrState
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return SettingsView{}, e
	}
	defer tx.Rollback(ctx)
	_, email, e := noticeMerchant(ctx, tx, mid, false)
	if e != nil {
		return SettingsView{}, e
	}
	r, data, e := s.readProfile(ctx, tx, mid)
	if errors.Is(e, ErrNotFound) {
		return profileView(0, mid, 0, profileData{Version: 1, Targets: storedTargets{Email: email}}), tx.Commit(ctx)
	}
	if e != nil {
		return SettingsView{}, e
	}
	return profileView(r.ID, mid, r.Revision, data), tx.Commit(ctx)
}

func (s *Store) SaveSettings(ctx context.Context, mid, expected int64, in SettingsWrite, actor Actor) (SettingsView, error) {
	if !actor.valid(mid, true) {
		return SettingsView{}, ErrForbidden
	}
	if !s.available() {
		return SettingsView{}, ErrState
	}
	if expected < 0 || !in.Preferences.valid() {
		return SettingsView{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return SettingsView{}, e
	}
	defer tx.Rollback(ctx)
	active, email, e := noticeMerchant(ctx, tx, mid, true)
	if e != nil {
		return SettingsView{}, e
	}
	if !active {
		return SettingsView{}, ErrState
	}
	old, data, e := s.readProfile(ctx, tx, mid)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return SettingsView{}, e
	}
	if old.ID != expected {
		return SettingsView{}, ErrConflict
	}
	prepared, e := applySettings(data, in, email)
	if e != nil {
		return SettingsView{}, e
	}
	raw, e := json.Marshal(prepared)
	if e != nil || len(raw) > 60000 {
		return SettingsView{}, ErrInvalid
	}
	bodyHash := s.digest("profile-body", raw)
	if old.ID > 0 && hmac.Equal(old.BodyHash, bodyHash) {
		return profileView(old.ID, mid, old.Revision, data), tx.Commit(ctx)
	}
	r := profileRecord{MerchantID: mid, Revision: old.Revision + 1, PreviousID: old.ID, PreviousHash: old.ProfileHash, BodyHash: bodyHash, Actor: actor}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('merchant_notice_profiles','id')),clock_timestamp()").Scan(&r.ID, &r.CreatedAt); e != nil {
		return SettingsView{}, e
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return SettingsView{}, ErrState
	}
	r.Cipher = s.aead.Seal(nonce, nonce, raw, profileAAD(r))
	r.ProfileHash = s.profileProof(r)
	var previous any
	if r.PreviousID > 0 {
		previous = r.PreviousID
	}
	if _, e = tx.Exec(ctx, `INSERT INTO merchant_notice_profiles(id,merchant_id,revision,previous_id,previous_hash,payload_cipher,body_hash,profile_hash,actor_role,actor_id,actor_name,actor_class,created_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`, r.ID, mid, r.Revision, previous, r.PreviousHash, r.Cipher, r.BodyHash, r.ProfileHash, actor.Role, actor.ID, actor.Name, actor.AdminClass, r.CreatedAt); e != nil {
		return SettingsView{}, e
	}
	if old.ID == 0 {
		if _, e = tx.Exec(ctx, `INSERT INTO merchant_notice_current(merchant_id,profile_id,revision,profile_hash) VALUES($1,$2,$3,$4)`, mid, r.ID, r.Revision, r.ProfileHash); e != nil {
			return SettingsView{}, e
		}
	} else {
		tag, updateError := tx.Exec(ctx, `UPDATE merchant_notice_current SET profile_id=$2,revision=$3,profile_hash=$4 WHERE merchant_id=$1 AND profile_id=$5`, mid, r.ID, r.Revision, r.ProfileHash, old.ID)
		if updateError != nil {
			return SettingsView{}, updateError
		}
		if tag.RowsAffected() != 1 {
			return SettingsView{}, ErrConflict
		}
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,'merchant-notice-profiles',$4,'save',$5)`, actor.Role, actor.ID, actor.Name, r.ID, profileAudit(r)); e != nil {
		return SettingsView{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return SettingsView{}, e
	}
	return profileView(r.ID, mid, r.Revision, prepared), nil
}
