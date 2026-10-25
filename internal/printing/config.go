package printing

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/payment"
	"time"
)

type reader interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Query(context.Context, string, ...any) (pgx.Rows, error)
}
type Store struct {
	pool   *pgxpool.Pool
	source *payment.Store
	key    []byte
	aead   cipher.AEAD
}

func New(pool *pgxpool.Pool, source *payment.Store) (*Store, error) {
	if pool == nil || source == nil {
		return nil, ErrInvalid
	}
	key := source.PrintingProtectionKey()
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(block)
	if e != nil {
		return nil, e
	}
	return &Store{pool: pool, source: source, key: key, aead: a}, nil
}

type Actor struct {
	Role, Class, Name string
	ID                int64
	Delegated         bool
}

func super(a Actor) bool {
	return a.Role == "admin" && a.Class == "super" && a.ID > 0 && !a.Delegated && a.Name != "" && safeText(a.Name, 128)
}
func owner(mid int64, a Actor) bool {
	return a.Role == "merchant" && a.ID == mid && mid > 0 && !a.Delegated && a.Name != "" && safeText(a.Name, 128)
}
func (s *Store) digest(domain string, fact any) ([]byte, error) {
	raw, e := json.Marshal(fact)
	if e != nil {
		return nil, e
	}
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("goepay/printing/" + domain + "\x00"))
	m.Write(raw)
	return m.Sum(nil), nil
}
func (s *Store) seal(kind string, id int64, v any) ([]byte, error) {
	raw, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	n := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(n); e != nil {
		return nil, e
	}
	return s.aead.Seal(n, n, raw, []byte(fmt.Sprintf("printing/%s/%d", kind, id))), nil
}
func (s *Store) open(kind string, id int64, b []byte, v any) error {
	n := s.aead.NonceSize()
	if len(b) <= n {
		return ErrConflict
	}
	raw, e := s.aead.Open(nil, b[:n], b[n:], []byte(fmt.Sprintf("printing/%s/%d", kind, id)))
	if e != nil {
		return ErrConflict
	}
	if json.Unmarshal(raw, v) != nil {
		return ErrConflict
	}
	return nil
}
func mask(v string) string {
	r := []rune(v)
	if len(r) < 5 {
		return "***"
	}
	return "***" + string(r[len(r)-4:])
}
func audit(ctx context.Context, tx pgx.Tx, a Actor, resource string, id int64, op string, changes any) error {
	raw, _ := json.Marshal(changes)
	_, e := tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,$4,$5,$6,$7)", a.Role, a.ID, a.Name, resource, id, op, raw)
	return e
}

type Credential struct {
	AppID  string `json:"app_id"`
	Secret string `json:"secret"`
}
type PlatformInput struct {
	ExpectedRevision int64  `json:"expected_revision,string"`
	Enabled          bool   `json:"enabled"`
	Provider         string `json:"provider"`
	CredentialAction string `json:"credential_action"`
	AppID            string `json:"app_id"`
	Secret           string `json:"secret"`
}
type PlatformView struct {
	ID                         int64  `json:"id,string"`
	Revision                   int64  `json:"revision,string"`
	Enabled                    bool   `json:"enabled"`
	Provider                   string `json:"provider"`
	AppIDMasked                string `json:"app_id_masked"`
	SecretPresent              bool   `json:"secret_present"`
	ExternalExecutionAvailable bool   `json:"external_execution_available"`
}
type platformRecord struct {
	V          PlatformView
	Cipher     []byte
	ActorID    int64
	ActorName  string
	At         time.Time
	Proof      []byte
	Credential Credential
}

func (s *Store) platformProof(r platformRecord) ([]byte, error) {
	return s.digest("platform", []any{r.V.ID, r.V.Revision, r.V.Enabled, r.V.Provider, r.Cipher, r.ActorID, r.ActorName, r.At.UTC().Format(time.RFC3339Nano)})
}
func (s *Store) readPlatform(ctx context.Context, q reader, id int64) (platformRecord, error) {
	var r platformRecord
	sql := `SELECT c.id,c.revision,c.enabled,c.provider,c.credential_cipher,c.actor_id,c.actor_name,c.created_at,c.proof_hash,EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='printing-platform' AND a.resource_id=c.id AND a.operation='configure' AND (a.actor_role,a.actor_id,a.actor_name)=('admin',c.actor_id,c.actor_name)) FROM printing_platform_configs c WHERE c.id=$1`
	if id == 0 {
		sql = `SELECT c.id,c.revision,c.enabled,c.provider,c.credential_cipher,c.actor_id,c.actor_name,c.created_at,c.proof_hash,EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='printing-platform' AND a.resource_id=c.id AND a.operation='configure' AND (a.actor_role,a.actor_id,a.actor_name)=('admin',c.actor_id,c.actor_name)) FROM printing_platform_head h JOIN printing_platform_configs c ON c.id=h.config_id WHERE $1::bigint=0`
	}
	var audited bool
	e := q.QueryRow(ctx, sql, id).Scan(&r.V.ID, &r.V.Revision, &r.V.Enabled, &r.V.Provider, &r.Cipher, &r.ActorID, &r.ActorName, &r.At, &r.Proof, &audited)
	if errors.Is(e, pgx.ErrNoRows) {
		if id == 0 {
			var history bool
			if x := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM printing_platform_configs)").Scan(&history); x != nil {
				return r, x
			}
			if history {
				return r, ErrConflict
			}
		}
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	want, e := s.platformProof(r)
	if e != nil || !hmac.Equal(want, r.Proof) || !audited {
		return r, ErrConflict
	}
	if id == 0 {
		var current bool
		if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM printing_platform_head h WHERE h.config_id=$1 AND h.revision=$2 AND h.revision=(SELECT max(revision) FROM printing_platform_configs))`, r.V.ID, r.V.Revision).Scan(&current); e != nil {
			return r, e
		}
		if !current {
			return r, ErrConflict
		}
	}
	if len(r.Cipher) > 0 {
		if e = s.open("platform", r.V.ID, r.Cipher, &r.Credential); e != nil {
			return r, e
		}
		r.V.AppIDMasked = mask(r.Credential.AppID)
		r.V.SecretPresent = r.Credential.Secret != ""
	}
	return r, nil
}
func (s *Store) Platform(ctx context.Context, a Actor) (PlatformView, error) {
	if !super(a) {
		return PlatformView{}, ErrInvalid
	}
	r, e := s.readPlatform(ctx, s.pool, 0)
	if errors.Is(e, ErrNotFound) {
		return PlatformView{Provider: "local_fake", ExternalExecutionAvailable: true}, nil
	}
	// Capability of this binary, not a worker heartbeat or an enable switch.
	r.V.ExternalExecutionAvailable = true
	return r.V, e
}
func (s *Store) SetPlatform(ctx context.Context, in PlatformInput, a Actor) (PlatformView, error) {
	var out PlatformView
	if !super(a) || in.ExpectedRevision < 0 || (in.Provider != "local_fake" && in.Provider != "excelsecu") {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('printing-platform',0))"); e != nil {
		return out, e
	}
	old, e := s.readPlatform(ctx, tx, 0)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return out, e
	}
	if old.V.Revision != in.ExpectedRevision {
		return out, ErrConflict
	}
	cred := old.Credential
	switch in.CredentialAction {
	case "keep":
		if in.AppID != "" || in.Secret != "" {
			return out, ErrInvalid
		}
	case "clear":
		if in.Enabled || in.AppID != "" || in.Secret != "" {
			return out, ErrInvalid
		}
		cred = Credential{}
	case "replace":
		if in.AppID == "" || in.Secret == "" || !safeText(in.AppID, 128) || !safeText(in.Secret, 512) {
			return out, ErrInvalid
		}
		cred = Credential{in.AppID, in.Secret}
	default:
		return out, ErrInvalid
	}
	if in.Enabled && (cred.AppID == "" || cred.Secret == "") {
		return out, ErrInvalid
	}
	r := platformRecord{V: PlatformView{Revision: in.ExpectedRevision + 1, Enabled: in.Enabled, Provider: in.Provider}, ActorID: a.ID, ActorName: a.Name, Credential: cred}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('printing_platform_configs','id')),transaction_timestamp()").Scan(&r.V.ID, &r.At); e != nil {
		return out, e
	}
	if cred.Secret != "" {
		r.Cipher, e = s.seal("platform", r.V.ID, cred)
		if e != nil {
			return out, e
		}
	}
	r.Proof, e = s.platformProof(r)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO printing_platform_configs(id,revision,enabled,provider,credential_cipher,actor_id,actor_name,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)", r.V.ID, r.V.Revision, in.Enabled, in.Provider, r.Cipher, a.ID, a.Name, r.Proof, r.At); e != nil {
		return out, e
	}
	if old.V.ID == 0 {
		_, e = tx.Exec(ctx, "INSERT INTO printing_platform_head(config_id,revision) VALUES($1,$2)", r.V.ID, r.V.Revision)
	} else {
		_, e = tx.Exec(ctx, "UPDATE printing_platform_head SET config_id=$1,revision=$2 WHERE singleton", r.V.ID, r.V.Revision)
	}
	if e != nil {
		return out, e
	}
	if e = audit(ctx, tx, a, "printing-platform", r.V.ID, "configure", map[string]any{"enabled": in.Enabled, "provider": in.Provider, "credential_operation": in.CredentialAction}); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return s.Platform(ctx, a)
}

type MerchantInput struct {
	ExpectedRevision int64  `json:"expected_revision,string"`
	Mode             int    `json:"mode"`
	Copies           int    `json:"copies"`
	Voice            bool   `json:"voice"`
	DeviceAction     string `json:"device_action"`
	DeviceID         string `json:"device_id"`
}
type MerchantView struct {
	ID            int64  `json:"id,string"`
	MerchantID    int64  `json:"merchant_id,string"`
	Revision      int64  `json:"revision,string"`
	Mode          int    `json:"mode"`
	Copies        int    `json:"copies"`
	Voice         bool   `json:"voice"`
	DeviceMasked  string `json:"device_masked"`
	DevicePresent bool   `json:"device_present"`
}
type merchantRecord struct {
	V      MerchantView
	Cipher []byte
	Name   string
	At     time.Time
	Proof  []byte
	Device string
}

func (s *Store) merchantProof(r merchantRecord) ([]byte, error) {
	return s.digest("merchant", []any{r.V.ID, r.V.MerchantID, r.V.Revision, r.V.Mode, r.V.Copies, r.V.Voice, r.Cipher, r.Name, r.At.UTC().Format(time.RFC3339Nano)})
}
func (s *Store) readMerchant(ctx context.Context, q reader, mid, id int64) (merchantRecord, error) {
	var r merchantRecord
	var audited bool
	sql := `SELECT c.id,c.merchant_id,c.revision,c.mode,c.copies,c.voice,c.device_cipher,c.actor_name,c.created_at,c.proof_hash,EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='printing-merchant' AND a.resource_id=c.id AND a.operation='configure' AND (a.actor_role,a.actor_id,a.actor_name)=('merchant',c.merchant_id,c.actor_name)) FROM printing_merchant_configs c WHERE c.id=$2 AND c.merchant_id=$1`
	if id == 0 {
		sql = `SELECT c.id,c.merchant_id,c.revision,c.mode,c.copies,c.voice,c.device_cipher,c.actor_name,c.created_at,c.proof_hash,EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='printing-merchant' AND a.resource_id=c.id AND a.operation='configure' AND (a.actor_role,a.actor_id,a.actor_name)=('merchant',c.merchant_id,c.actor_name)) FROM printing_merchant_heads h JOIN printing_merchant_configs c ON c.id=h.config_id WHERE h.merchant_id=$1 AND $2::bigint=0`
	}
	e := q.QueryRow(ctx, sql, mid, id).Scan(&r.V.ID, &r.V.MerchantID, &r.V.Revision, &r.V.Mode, &r.V.Copies, &r.V.Voice, &r.Cipher, &r.Name, &r.At, &r.Proof, &audited)
	if errors.Is(e, pgx.ErrNoRows) {
		if id == 0 {
			var history bool
			if x := q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM printing_merchant_configs WHERE merchant_id=$1)", mid).Scan(&history); x != nil {
				return r, x
			}
			if history {
				return r, ErrConflict
			}
		}
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	want, e := s.merchantProof(r)
	if e != nil || !hmac.Equal(want, r.Proof) || !audited {
		return r, ErrConflict
	}
	if id == 0 {
		var current bool
		if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM printing_merchant_heads h WHERE h.merchant_id=$1 AND h.config_id=$2 AND h.revision=$3 AND h.revision=(SELECT max(revision) FROM printing_merchant_configs WHERE merchant_id=$1))`, mid, r.V.ID, r.V.Revision).Scan(&current); e != nil {
			return r, e
		}
		if r.V.MerchantID != mid || !current {
			return r, ErrConflict
		}
	}
	if len(r.Cipher) > 0 {
		if e = s.open("device", r.V.ID, r.Cipher, &r.Device); e != nil {
			return r, e
		}
		r.V.DeviceMasked = mask(r.Device)
		r.V.DevicePresent = r.Device != ""
	}
	return r, nil
}
func (s *Store) Merchant(ctx context.Context, mid int64, a Actor) (MerchantView, error) {
	if !owner(mid, a) {
		return MerchantView{}, ErrInvalid
	}
	r, e := s.readMerchant(ctx, s.pool, mid, 0)
	if errors.Is(e, ErrNotFound) {
		return MerchantView{MerchantID: mid, Copies: 1}, nil
	}
	return r.V, e
}
func (s *Store) SetMerchant(ctx context.Context, mid int64, in MerchantInput, a Actor) (MerchantView, error) {
	var out MerchantView
	if !owner(mid, a) || in.ExpectedRevision < 0 || in.Mode < 0 || in.Mode > 2 || in.Copies < 1 || in.Copies > 5 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	var active bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR NO KEY UPDATE", mid).Scan(&active); e != nil {
		return out, e
	}
	if !active {
		return out, ErrState
	}
	old, e := s.readMerchant(ctx, tx, mid, 0)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return out, e
	}
	if old.V.Revision != in.ExpectedRevision {
		return out, ErrConflict
	}
	device := old.Device
	switch in.DeviceAction {
	case "keep":
		if in.DeviceID != "" {
			return out, ErrInvalid
		}
	case "replace":
		if in.DeviceID == "" || !safeText(in.DeviceID, 128) {
			return out, ErrInvalid
		}
		device = in.DeviceID
	case "clear":
		if in.Mode != 0 || in.DeviceID != "" {
			return out, ErrInvalid
		}
		device = ""
	default:
		return out, ErrInvalid
	}
	if in.Mode > 0 && device == "" {
		return out, ErrInvalid
	}
	r := merchantRecord{V: MerchantView{MerchantID: mid, Revision: in.ExpectedRevision + 1, Mode: in.Mode, Copies: in.Copies, Voice: in.Voice}, Name: a.Name, Device: device}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('printing_merchant_configs','id')),transaction_timestamp()").Scan(&r.V.ID, &r.At); e != nil {
		return out, e
	}
	if device != "" {
		r.Cipher, e = s.seal("device", r.V.ID, device)
		if e != nil {
			return out, e
		}
	}
	r.Proof, e = s.merchantProof(r)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO printing_merchant_configs(id,merchant_id,revision,mode,device_cipher,copies,voice,actor_name,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", r.V.ID, mid, r.V.Revision, in.Mode, r.Cipher, in.Copies, in.Voice, a.Name, r.Proof, r.At); e != nil {
		return out, e
	}
	if old.V.ID == 0 {
		_, e = tx.Exec(ctx, "INSERT INTO printing_merchant_heads(merchant_id,config_id,revision) VALUES($1,$2,$3)", mid, r.V.ID, r.V.Revision)
	} else {
		_, e = tx.Exec(ctx, "UPDATE printing_merchant_heads SET config_id=$2,revision=$3 WHERE merchant_id=$1", mid, r.V.ID, r.V.Revision)
	}
	if e != nil {
		return out, e
	}
	if e = audit(ctx, tx, a, "printing-merchant", r.V.ID, "configure", map[string]any{"mode": in.Mode, "copies": in.Copies, "voice": in.Voice, "device_operation": in.DeviceAction}); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	return s.Merchant(ctx, mid, a)
}
