// Package businesssms231 binds business SMS to independently managed scene
// templates, a verified recipient and the active encrypted gateway generation.
// Resolving performs no network calls and reuses the business source/claim tx.
package businesssms231

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
	"goepay/internal/smsgateway134"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid business SMS configuration")
var ErrConflict = errors.New("business SMS configuration changed")
var scenes = []string{"balance", "complain", "group"}
var providers = []string{"aliyun", "qcloud", "smsbao"}
var phonePattern = regexp.MustCompile(`^1[3-9][0-9]{9}$`)
var templatePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var numericPattern = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)

// All three configured gateways use the mainland mobile template contract
// (Qcloud explicitly sends nationcode 86). Verified international identities
// remain valid account bindings, but do not authorize an unsupported SMS call.
func normalizedPhone(value string) string {
	if strings.HasPrefix(value, "+86") {
		value = strings.TrimPrefix(value, "+86")
	} else if strings.HasPrefix(value, "86") && len(value) == 13 {
		value = value[2:]
	}
	if !phonePattern.MatchString(value) {
		return ""
	}
	return value
}

type Store struct {
	pool    *pgxpool.Pool
	key     []byte
	gateway *smsgateway134.Store
}
type View struct {
	Revision       int64             `json:"revision,string"`
	Templates      map[string]string `json:"templates"`
	ActiveProvider string            `json:"active_provider"`
	Ready          map[string]bool   `json:"ready"`
}
type Input struct {
	ExpectedRevision int64             `json:"expected_revision,string"`
	Templates        map[string]string `json:"templates"`
}

// Snapshot is sealed inside the existing authenticated merchant event envelope.
// Fingerprint covers the complete gateway credentials, scene, phone and revision.
type Snapshot struct {
	Revision                               int64
	Provider, Template, Phone, Fingerprint string
}

func (Snapshot) String() string   { return "businesssms231.Snapshot(redacted)" }
func (Snapshot) GoString() string { return "businesssms231.Snapshot(redacted)" }
func (s Snapshot) Valid() bool {
	_, e := hex.DecodeString(s.Fingerprint)
	return s.Revision > 0 && s.Template != "" && ValidTemplate(s.Provider, "balance", s.Template) && phonePattern.MatchString(s.Phone) && len(s.Fingerprint) == 64 && e == nil
}

type Resolved struct {
	Snapshot    Snapshot                  `json:"-"`
	Credentials smsgateway134.Credentials `json:"-"`
}

func (Resolved) String() string   { return "businesssms231.Resolved(redacted)" }
func (Resolved) GoString() string { return "businesssms231.Resolved(redacted)" }
func New(pool *pgxpool.Pool, checkout []byte) (*Store, error) {
	if pool == nil || len(checkout) != 32 {
		return nil, ErrInvalid
	}
	mac := hmac.New(sha256.New, checkout)
	mac.Write([]byte("goepay/businesssms231/integrity"))
	return &Store{pool: pool, key: mac.Sum(nil), gateway: smsgateway134.New(pool, checkout)}, nil
}
func FromMaster(pool *pgxpool.Pool, master []byte) (*Store, error) {
	if len(master) != 32 {
		return nil, ErrInvalid
	}
	k := sha256.Sum256(append([]byte("checkout-signing:"), master...))
	return New(pool, k[:])
}
func ValidTemplate(provider, scene, value string) bool {
	if scene != "balance" && scene != "complain" && scene != "group" {
		return false
	}
	if value == "" {
		return provider == "aliyun" || provider == "qcloud" || provider == "smsbao"
	}
	switch provider {
	case "aliyun":
		return templatePattern.MatchString(value)
	case "qcloud":
		return numericPattern.MatchString(value)
	case "smsbao":
		return utf8.ValidString(value) && len(value) <= 1000 && strings.Count(value, "{code}") == 1 && !strings.ContainsAny(value, "\x00\r\n")
	}
	return false
}
func emptyTemplates() map[string]string {
	m := map[string]string{}
	for _, p := range providers {
		for _, scene := range scenes {
			m[p+"_"+scene] = ""
		}
	}
	return m
}
func validTemplates(m map[string]string) bool {
	if len(m) != 9 {
		return false
	}
	for _, p := range providers {
		for _, scene := range scenes {
			v, ok := m[p+"_"+scene]
			if !ok || !ValidTemplate(p, scene, v) {
				return false
			}
		}
	}
	return true
}
func (s *Store) digest(domain string, raw []byte) []byte {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(domain + "\x00"))
	m.Write(raw)
	return m.Sum(nil)
}
func (s *Store) proof(rev int64, m map[string]string) []byte {
	raw, _ := json.Marshal([]any{rev, m})
	return s.digest("templates", raw)
}
func (s *Store) read(ctx context.Context, tx pgx.Tx) (View, error) {
	v := View{Templates: emptyTemplates(), Ready: map[string]bool{}}
	var raw, proof []byte
	e := tx.QueryRow(ctx, "SELECT revision,templates,proof_hash FROM business_sms_settings231 WHERE id=true FOR SHARE").Scan(&v.Revision, &raw, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		var prior bool
		e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='business-sms231')").Scan(&prior)
		if e != nil {
			return v, e
		}
		if prior {
			return v, ErrConflict
		}
		return v, nil
	}
	if e != nil {
		return v, e
	}
	if json.Unmarshal(raw, &v.Templates) != nil || !validTemplates(v.Templates) || !hmac.Equal(proof, s.proof(v.Revision, v.Templates)) {
		return View{}, ErrConflict
	}
	var ok bool
	e = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE actor_role='admin' AND actor_id>0 AND changes->>'revision'=$1 AND changes->>'proof_hash'=$2)=1 AND count(*) FILTER(WHERE (changes->>'revision')::bigint>$3)=0 FROM backoffice_audit WHERE resource='business-sms231' AND operation='update'`, strconv.FormatInt(v.Revision, 10), hex.EncodeToString(proof), v.Revision).Scan(&ok)
	if e != nil {
		return View{}, e
	}
	if !ok {
		return View{}, ErrConflict
	}
	return v, nil
}
func (s *Store) complete(ctx context.Context, tx pgx.Tx, v View) (View, error) {
	p, _, e := s.gateway.ActiveCredentials(ctx, tx)
	if errors.Is(e, smsgateway134.ErrSource) {
		return v, nil
	}
	if e != nil {
		return View{}, e
	}
	v.ActiveProvider = p
	for _, scene := range scenes {
		v.Ready[scene] = v.Templates[p+"_"+scene] != ""
	}
	return v, nil
}
func (s *Store) Read(ctx context.Context, a smsgateway134.Actor) (View, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return View{}, e
	}
	defer tx.Rollback(ctx)
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return View{}, e
	}
	v, e := s.read(ctx, tx)
	if e != nil {
		return View{}, e
	}
	return s.complete(ctx, tx, v)
}
func (s *Store) Save(ctx context.Context, a smsgateway134.Actor, in Input) (View, error) {
	if in.ExpectedRevision < 0 || !validTemplates(in.Templates) {
		return View{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return View{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(7170231001)"); e != nil {
		return View{}, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return View{}, e
	}
	old, e := s.read(ctx, tx)
	if e != nil {
		return View{}, e
	}
	if old.Revision != in.ExpectedRevision {
		return View{}, ErrConflict
	}
	rev := old.Revision + 1
	proof := s.proof(rev, in.Templates)
	raw, _ := json.Marshal(in.Templates)
	if _, e = tx.Exec(ctx, `INSERT INTO business_sms_settings231(id,revision,templates,proof_hash)VALUES(true,$1,$2,$3)ON CONFLICT(id)DO UPDATE SET revision=EXCLUDED.revision,templates=EXCLUDED.templates,proof_hash=EXCLUDED.proof_hash`, rev, raw, proof); e != nil {
		return View{}, e
	}
	changes, _ := json.Marshal(map[string]any{"revision": rev, "proof_hash": hex.EncodeToString(proof)})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',$1,$2,'business-sms231',1,'update',$3)`, a.User.ID, a.User.Username, changes); e != nil {
		return View{}, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return View{}, e
	}
	v, e := s.complete(ctx, tx, View{Revision: rev, Templates: in.Templates, Ready: map[string]bool{}})
	if e != nil {
		return View{}, e
	}
	return v, tx.Commit(ctx)
}

// VerifiedPhoneTx deliberately excludes editable contact phone. A bound phone
// or an authenticated registration claim is required; absence is not an error.
func (s *Store) VerifiedPhoneTx(ctx context.Context, tx pgx.Tx, mid int64) (string, error) {
	var bound string
	if e := tx.QueryRow(ctx, `SELECT COALESCE(profile->>'bind_mobile','') FROM merchants WHERE id=$1 FOR SHARE`, mid).Scan(&bound); e != nil {
		return "", e
	}
	if bound != "" {
		return normalizedPhone(bound), nil
	}
	var phone string
	var proof []byte
	e := tx.QueryRow(ctx, `SELECT p.phone,p.phone_hash FROM enrollment_phones134 p JOIN console_users u ON u.role='merchant' AND u.subject_id=p.merchant_id AND u.username=p.username AND u.enabled WHERE p.merchant_id=$1`, mid).Scan(&phone, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	// Registration already stores a per-master MAC. Its immutable claim table
	// and enabled owning console identity establish this phone's provenance.
	if len(proof) != 32 {
		return "", ErrConflict
	}
	return normalizedPhone(phone), nil
}

// ResolveTx returns nil for an explicitly disabled/unconfigured business route.
// A malformed/tampered managed record is an error, never an environment fallback.
func (s *Store) ResolveTx(ctx context.Context, tx pgx.Tx, mid int64, scene string) (*Resolved, error) {
	if scene != "balance" && scene != "complain" && scene != "group" {
		return nil, nil
	}
	var n int
	var value string
	if e := tx.QueryRow(ctx, `SELECT count(*),COALESCE(min(body->>'value'),'') FROM backoffice_records WHERE resource='settings' AND body->>'key'='smsnotice'`).Scan(&n, &value); e != nil {
		return nil, e
	}
	if n == 0 || n == 1 && value == "0" {
		return nil, nil
	}
	if n != 1 || value != "1" {
		return nil, ErrConflict
	}
	v, e := s.read(ctx, tx)
	if e != nil {
		return nil, e
	}
	if v.Revision == 0 {
		return nil, nil
	}
	p, c, e := s.gateway.ActiveCredentials(ctx, tx)
	if errors.Is(e, smsgateway134.ErrSource) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	template := v.Templates[p+"_"+scene]
	if template == "" {
		return nil, nil
	}
	phone, e := s.VerifiedPhoneTx(ctx, tx, mid)
	if e != nil {
		return nil, e
	}
	if phone == "" {
		return nil, nil
	}
	raw, _ := json.Marshal([]any{v.Revision, mid, scene, p, c, template, phone})
	out := &Resolved{Snapshot: Snapshot{Revision: v.Revision, Provider: p, Template: template, Phone: phone, Fingerprint: hex.EncodeToString(s.digest("effective-route", raw))}, Credentials: c}
	return out, nil
}

// ResolveAdminTx uses only the super-administrator's explicit protected
// recipient. It does not interpret a merchant's editable contact phone as an
// administrator target, and the merchant channel switch does not govern it.
func (s *Store) ResolveAdminTx(ctx context.Context, tx pgx.Tx, phone, scene string) (*Resolved, error) {
	if scene != "complain" {
		return nil, ErrInvalid
	}
	phone = normalizedPhone(phone)
	if phone == "" {
		return nil, nil
	}
	v, e := s.read(ctx, tx)
	if e != nil {
		return nil, e
	}
	if v.Revision == 0 {
		return nil, nil
	}
	p, c, e := s.gateway.ActiveCredentials(ctx, tx)
	if errors.Is(e, smsgateway134.ErrSource) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	template := v.Templates[p+"_"+scene]
	if template == "" {
		return nil, nil
	}
	raw, _ := json.Marshal([]any{v.Revision, "administrator", scene, p, c, template, phone})
	return &Resolved{Snapshot: Snapshot{Revision: v.Revision, Provider: p, Template: template, Phone: phone, Fingerprint: hex.EncodeToString(s.digest("effective-route", raw))}, Credentials: c}, nil
}
func (s *Store) Capability(ctx context.Context) (map[string]bool, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	v, e := s.read(ctx, tx)
	if e != nil {
		return nil, e
	}
	v, e = s.complete(ctx, tx, v)
	return v.Ready, e
}
func (s *Store) Phone(ctx context.Context, mid int64) (string, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return "", e
	}
	defer tx.Rollback(ctx)
	return s.VerifiedPhoneTx(ctx, tx, mid)
}
