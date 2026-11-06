// Package smsgateway owns the SMS gateway credentials (sms_api + provider
// keys) without exposing a secret or performing network I/O while reading or
// saving configuration. Credentials are sealed with the same checkout key path
// as internal/mailaccount (AES-GCM body + HMAC fingerprint); plaintext is
// never persisted and never returned to the browser.
//
// Original: epay_release/includes/lib/sms/{Aliyun,Qcloud,SmsBao}.php selected by
// the admin sms_api* settings. GOEPAY keeps three providers; one is the active
// gateway via a managed head, mirroring sms_api on/off.
package smsgateway

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
	"io"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/identity"
)

var ErrSource = errors.New("sms gateway source cannot be confirmed")
var ErrInvalid = errors.New("invalid sms gateway configuration")
var ErrConflict = errors.New("sms gateway changed")

const platformLock = int64(7170134001)

// Providers are exactly the original's three; order is stable for views.
var Providers = []string{"aliyun", "qcloud", "smsbao"}

func known(p string) bool {
	for _, v := range Providers {
		if v == p {
			return true
		}
	}
	return false
}

// Credentials carries one provider's secret fields. Only the subset each
// provider uses is populated; the rest stay empty. The template id and sign are
// NOT secret in the original but are kept sealed with the keys for one atomic
// record. send() reads these; the browser never does.
type Credentials struct {
	// aliyun: AccessKeyId/Secret + TemplateCode + SignName
	AccessKeyID     string `json:"access_key_id,omitempty"`
	AccessKeySecret string `json:"access_key_secret,omitempty"`
	// qcloud: appid/appkey + tpl_id + sign
	AppID  string `json:"appid,omitempty"`
	AppKey string `json:"appkey,omitempty"`
	// smsbao: user/pass + template + sign
	User string `json:"user,omitempty"`
	Pass string `json:"pass,omitempty"`
	// shared across providers
	Template string `json:"template,omitempty"`
	Sign     string `json:"sign,omitempty"`
}

// Validate checks that the populated fields are coherent for the provider.
// Syntax only; it never dials the upstream.
func (c Credentials) Validate(provider string) error {
	switch provider {
	case "aliyun":
		if c.AccessKeyID == "" || c.AccessKeySecret == "" || c.Template == "" || c.Sign == "" {
			return ErrInvalid
		}
	case "qcloud":
		if c.AppID == "" || c.AppKey == "" || c.Template == "" || c.Sign == "" {
			return ErrInvalid
		}
	case "smsbao":
		if c.User == "" || c.Pass == "" || c.Template == "" || c.Sign == "" {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

type ProviderView struct {
	Provider   string `json:"provider"`
	Configured bool   `json:"configured"`
	// Non-secret echo so the console can show what is set without the keys.
	Template string `json:"template"`
	Sign     string `json:"sign"`
	// A masked hint of the principal id (never the secret).
	Principal string `json:"principal"`
}

type View struct {
	Providers   []ProviderView `json:"providers"`
	Active      string         `json:"active"`
	Status      int            `json:"status"`
	Fingerprint string         `json:"fingerprint"`
}

type Actor struct {
	User      identity.User
	Token     string
	Delegated bool
}

type Input struct {
	// Provider whose credentials are being saved (empty to only change head).
	Provider    string
	Credentials *Credentials
	// Active provider + on/off for the gateway head.
	Active   string
	Status   int
	Expected string
}

type providerRow struct {
	provider   string
	creds      Credentials
	configured bool
}
type headRow struct {
	active string
	status int
}
type state struct {
	schema    bool
	providers map[string]providerRow
	head      headRow
}

type Store struct {
	pool *pgxpool.Pool
	key  []byte
}

func New(pool *pgxpool.Pool, checkoutKey []byte) *Store {
	return &Store{pool: pool, key: append([]byte(nil), checkoutKey...)}
}

func (s *Store) digest(domain string, v any) ([]byte, error) {
	if len(s.key) != 32 {
		return nil, ErrSource
	}
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("goepay/sms-gateway134/" + domain + "\x00"))
	if e := json.NewEncoder(m).Encode(v); e != nil {
		return nil, e
	}
	return m.Sum(nil), nil
}
func (s *Store) aead() (cipher.AEAD, error) {
	key, e := s.digest("encryption-key", 1)
	if e != nil {
		return nil, e
	}
	block, e := aes.NewCipher(key)
	if e != nil {
		return nil, e
	}
	return cipher.NewGCM(block)
}
func (s *Store) seal(provider string, c Credentials) ([]byte, []byte, error) {
	g, e := s.aead()
	if e != nil {
		return nil, nil, e
	}
	raw, e := json.Marshal(c)
	if e != nil {
		return nil, nil, e
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, nil, e
	}
	aad := []byte("goepay/sms-gateway134/provider/" + provider)
	sealed := append(nonce, g.Seal(nil, nonce, raw, aad)...)
	mac, e := s.digest("provider-mac", []any{provider, sealed})
	if e != nil {
		return nil, nil, e
	}
	return sealed, mac, nil
}
func (s *Store) open(provider string, sealed, mac []byte) (Credentials, error) {
	var out Credentials
	g, e := s.aead()
	if e != nil {
		return out, ErrSource
	}
	want, e := s.digest("provider-mac", []any{provider, sealed})
	if e != nil || !hmac.Equal(want, mac) || len(sealed) < g.NonceSize() || len(sealed) > 8192 {
		return out, ErrSource
	}
	nonce := sealed[:g.NonceSize()]
	aad := []byte("goepay/sms-gateway134/provider/" + provider)
	raw, e := g.Open(nil, nonce, sealed[g.NonceSize():], aad)
	if e != nil {
		return out, ErrSource
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out) != nil || d.Decode(new(any)) != io.EOF {
		return Credentials{}, ErrSource
	}
	return out, nil
}

func (s *Store) load(ctx context.Context, tx pgx.Tx, write bool) (state, error) {
	var out state
	out.providers = map[string]providerRow{}
	lock := "pg_advisory_xact_lock_shared"
	if write {
		lock = "pg_advisory_xact_lock"
	}
	if _, e := tx.Exec(ctx, "SELECT "+lock+"($1)", platformLock); e != nil {
		return out, e
	}
	var registered bool
	if e := tx.QueryRow(ctx, `SELECT to_regclass(current_schema()||'.sms_gateway_head134')IS NOT NULL`).Scan(&registered); e != nil {
		return out, e
	}
	if !registered {
		return out, nil
	}
	out.schema = true
	rows, e := tx.Query(ctx, `SELECT provider,sealed,mac,configured FROM sms_providers134`)
	if e != nil {
		return out, ErrSource
	}
	defer rows.Close()
	for rows.Next() {
		var p string
		var sealed, mac []byte
		var configured bool
		if e = rows.Scan(&p, &sealed, &mac, &configured); e != nil {
			return out, ErrSource
		}
		if !known(p) {
			return out, ErrSource
		}
		creds, e := s.open(p, sealed, mac)
		if e != nil {
			return out, e
		}
		out.providers[p] = providerRow{provider: p, creds: creds, configured: configured}
	}
	if e = rows.Err(); e != nil {
		return out, ErrSource
	}
	var active *string
	if e = tx.QueryRow(ctx, `SELECT active,status FROM sms_gateway_head134 WHERE singleton`).Scan(&active, &out.head.status); e != nil {
		return out, ErrSource
	}
	if active != nil {
		out.head.active = *active
	}
	if out.head.status != 0 && out.head.status != 1 {
		return out, ErrSource
	}
	if out.head.status == 1 && (out.head.active == "" || out.providers[out.head.active].provider == "") {
		return out, ErrSource
	}
	return out, nil
}

func maskPrincipal(provider string, c Credentials) string {
	var id string
	switch provider {
	case "aliyun":
		id = c.AccessKeyID
	case "qcloud":
		id = c.AppID
	case "smsbao":
		id = c.User
	}
	if id == "" {
		return ""
	}
	if len(id) <= 4 {
		return "****"
	}
	return id[:2] + "****" + id[len(id)-2:]
}

func (s *Store) view(st state) (View, error) {
	out := View{Active: st.head.active, Status: st.head.status}
	for _, p := range Providers {
		row, ok := st.providers[p]
		pv := ProviderView{Provider: p}
		if ok {
			pv.Configured = row.configured
			pv.Template = row.creds.Template
			pv.Sign = row.creds.Sign
			pv.Principal = maskPrincipal(p, row.creds)
		}
		out.Providers = append(out.Providers, pv)
	}
	fp, e := s.digest("cas", []any{st.head.active, st.head.status, providerFingerprint(st)})
	if e != nil {
		return out, e
	}
	out.Fingerprint = hex.EncodeToString(fp)
	return out, nil
}

func providerFingerprint(st state) []string {
	ids := make([]string, 0, len(st.providers))
	for p, r := range st.providers {
		ids = append(ids, p+":"+boolStr(r.configured)+":"+r.creds.Template+":"+r.creds.Sign+":"+maskPrincipal(p, r.creds))
	}
	sort.Strings(ids)
	return ids
}
func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (s *Store) Read(ctx context.Context, a Actor) (View, error) {
	var out View
	if s.pool == nil {
		return out, ErrSource
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	st, e := s.load(ctx, tx, false)
	if e != nil {
		return out, e
	}
	if !st.schema {
		return out, ErrSource
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	out, e = s.view(st)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

// Save writes one provider's sealed credentials and/or the head selection, under
// a super-admin session and a CAS fingerprint, auditing the change. It never
// returns a secret.
func (s *Store) Save(ctx context.Context, a Actor, in Input) (View, error) {
	var out View
	if s.pool == nil {
		return out, ErrSource
	}
	if len(in.Expected) != 64 || (in.Status != 0 && in.Status != 1) {
		return out, ErrInvalid
	}
	if in.Active != "" && !known(in.Active) {
		return out, ErrInvalid
	}
	if in.Provider != "" {
		if !known(in.Provider) || in.Credentials == nil {
			return out, ErrInvalid
		}
		if e := in.Credentials.Validate(in.Provider); e != nil {
			return out, e
		}
	}
	if in.Status == 1 && in.Active == "" {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	before, e := s.load(ctx, tx, true)
	if e != nil {
		return out, e
	}
	if !before.schema {
		return out, ErrSource
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	current, e := s.view(before)
	if e != nil {
		return out, e
	}
	if !hmac.Equal([]byte(in.Expected), []byte(current.Fingerprint)) {
		return out, ErrConflict
	}
	if in.Provider != "" {
		sealed, mac, e := s.seal(in.Provider, *in.Credentials)
		if e != nil {
			return out, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO sms_providers134(provider,sealed,mac,configured)VALUES($1,$2,$3,true)
			ON CONFLICT(provider)DO UPDATE SET sealed=EXCLUDED.sealed,mac=EXCLUDED.mac,configured=true,updated_at=clock_timestamp()`, in.Provider, sealed, mac); e != nil {
			return out, e
		}
	}
	// Enabling requires the active provider to be configured (either now or in a
	// prior save).
	if in.Status == 1 {
		if in.Active != in.Provider && before.providers[in.Active].provider == "" {
			return out, ErrInvalid
		}
	}
	var active any
	if in.Active != "" {
		active = in.Active
	}
	if _, e = tx.Exec(ctx, `UPDATE sms_gateway_head134 SET active=$1,status=$2,updated_at=clock_timestamp()WHERE singleton`, active, in.Status); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)
		VALUES('admin',$1,$2,'sms-gateway134',0,'sms-gateway-update',jsonb_build_object('provider',$3::text,'active',$4::text,'status',$5::int))`,
		a.User.ID, a.User.Username, in.Provider, in.Active, in.Status); e != nil {
		return out, e
	}
	after, e := s.load(ctx, tx, true)
	if e != nil {
		return out, e
	}
	out, e = s.view(after)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

// ActiveCredentials returns the active provider's name and credentials when the
// gateway is enabled, for the code sender. Returns ("", _, ErrSource) when the
// gateway is off or unmanaged — the caller then does not send.
func (s *Store) ActiveCredentials(ctx context.Context, tx pgx.Tx) (string, Credentials, error) {
	st, e := s.load(ctx, tx, false)
	if e != nil {
		return "", Credentials{}, e
	}
	if !st.schema || st.head.status != 1 || st.head.active == "" {
		return "", Credentials{}, ErrSource
	}
	row, ok := st.providers[st.head.active]
	if !ok {
		return "", Credentials{}, ErrSource
	}
	return st.head.active, row.creds, nil
}
