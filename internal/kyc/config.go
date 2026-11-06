package kyc

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/channelconfig"
	"goepay/internal/identity"
	"goepay/internal/ledger"
)

type ChannelResolver func(context.Context, pgx.Tx, int64) (channelconfig.Channel, error)
type Service struct {
	pool     *pgxpool.Pool
	aead     cipher.AEAD
	key      []byte
	provider Provider
	resolve  ChannelResolver
	base     string
}

func New(pool *pgxpool.Pool, key []byte, provider Provider, resolve ChannelResolver, base string) (*Service, error) {
	if pool == nil || len(key) != 32 {
		return nil, ErrInvalid
	}
	derive := func(label string) []byte {
		m := hmac.New(sha256.New, key)
		m.Write([]byte("goepay/kyc233/" + label))
		return m.Sum(nil)
	}
	b, e := aes.NewCipher(derive("encryption"))
	if e != nil {
		return nil, e
	}
	a, e := cipher.NewGCM(b)
	if e != nil {
		return nil, e
	}
	if base != "" {
		u, e := url.Parse(base)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1"))) {
			return nil, ErrInvalid
		}
	}
	return &Service{pool: pool, aead: a, key: derive("integrity"), provider: provider, resolve: resolve, base: strings.TrimRight(base, "/")}, nil
}

func (s *Service) digest(parts ...any) []byte {
	raw, _ := json.Marshal(parts)
	m := hmac.New(sha256.New, s.key)
	m.Write(raw)
	return m.Sum(nil)
}
func (s *Service) seal(aad string, v any) ([]byte, error) {
	raw, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return nil, e
	}
	return s.aead.Seal(nonce, nonce, raw, []byte(aad)), nil
}
func (s *Service) open(aad string, raw []byte, v any) error {
	n := s.aead.NonceSize()
	if len(raw) <= n {
		return ErrConflict
	}
	plain, e := s.aead.Open(nil, raw[:n], raw[n:], []byte(aad))
	if e != nil || json.Unmarshal(plain, v) != nil {
		return ErrConflict
	}
	return nil
}
func randomID() (string, error) {
	b := make([]byte, 16)
	_, e := rand.Read(b)
	return hex.EncodeToString(b), e
}
func mask(v string) string {
	r := []rune(v)
	if len(r) == 0 {
		return ""
	}
	if len(r) <= 4 {
		return "***"
	}
	return string(r[:1]) + "***" + string(r[len(r)-2:])
}
func credentialValues(c *Credentials) map[string]*string {
	v := reflect.ValueOf(c).Elem()
	t := v.Type()
	out := make(map[string]*string, v.NumField())
	for i := 0; i < v.NumField(); i++ {
		out[t.Field(i).Tag.Get("json")] = v.Field(i).Addr().Interface().(*string)
	}
	return out
}
func CredentialKeys() []string {
	var c Credentials
	out := []string{}
	for k := range credentialValues(&c) {
		out = append(out, k)
	}
	return out
}

func (s *Service) readConfig(ctx context.Context, tx pgx.Tx) (Snapshot, error) {
	var v Snapshot
	var raw, proof []byte
	e := tx.QueryRow(ctx, "SELECT revision,config_cipher,proof_hash FROM kyc_settings233 WHERE id=1 FOR SHARE").Scan(&v.Revision, &raw, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		var exists bool
		e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='kyc-settings233')").Scan(&exists)
		if e == nil && exists {
			return v, ErrConflict
		}
		return v, e
	}
	if e != nil {
		return v, e
	}
	rev := v.Revision
	if !hmac.Equal(proof, s.digest("config", rev, raw)) {
		return v, ErrConflict
	}
	var ok bool
	e = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE changes->>'revision'=$1 AND changes->>'proof_hash'=$2)=1 AND count(*) FILTER(WHERE (changes->>'revision')::bigint>$3)=0 FROM backoffice_audit WHERE resource='kyc-settings233'`, strconv.FormatInt(rev, 10), hex.EncodeToString(proof), rev).Scan(&ok)
	if e != nil {
		return v, e
	}
	if !ok {
		return v, ErrConflict
	}
	if e = s.open(fmt.Sprint("config/", rev), raw, &v); e != nil || v.Revision != rev {
		return Snapshot{}, ErrConflict
	}
	return v, nil
}
func (s *Service) admin(ctx context.Context, tx pgx.Tx, a Actor) error {
	if e := identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.Token, a.Delegated); e != nil {
		return e
	}
	h := sha256.Sum256([]byte(a.Token))
	var csrf string
	if e := tx.QueryRow(ctx, "SELECT csrf FROM console_sessions WHERE token_hash=$1 AND role='admin'", h[:]).Scan(&csrf); e != nil {
		return e
	}
	if len(a.CSRF) != 43 || subtle.ConstantTimeCompare([]byte(csrf), []byte(a.CSRF)) != 1 {
		return identity.ErrAdminSession
	}
	return nil
}
func (s *Service) view(ctx context.Context, tx pgx.Tx, v Snapshot) (ConfigView, error) {
	out := ConfigView{Revision: v.Revision, Mode: v.Mode, ChannelID: strconv.FormatInt(v.ChannelID, 10), Corporate: v.Corporate, Force: v.Force, Money: ledger.Format(v.Fee), OCRType: v.OCRType, Credentials: map[string]CredentialView{}, Channels: []ChannelView{}}
	for k, p := range credentialValues(&v.Credentials) {
		out.Credentials[k] = CredentialView{Present: *p != "", Masked: mask(*p)}
	}
	rows, e := tx.Query(ctx, "SELECT id::text,name FROM runtime_parent_channels WHERE pay_type='alipay' AND NOT archived ORDER BY id")
	if e != nil {
		return out, e
	}
	defer rows.Close()
	candidates := []ChannelView{}
	for rows.Next() {
		var c ChannelView
		if e = rows.Scan(&c.ID, &c.Name); e != nil {
			return out, e
		}
		candidates = append(candidates, c)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if s.resolve != nil {
		for _, c := range candidates {
			id, err := strconv.ParseInt(c.ID, 10, 64)
			if err != nil {
				continue
			}
			channel, err := s.resolve(ctx, tx, id)
			if err == nil && channel.Plugin == "alipay" {
				out.Channels = append(out.Channels, c)
			}
		}
	}
	return out, nil
}
func (s *Service) Config(ctx context.Context, a Actor) (ConfigView, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ConfigView{}, e
	}
	defer tx.Rollback(ctx)
	if e = s.admin(ctx, tx, a); e != nil {
		return ConfigView{}, e
	}
	v, e := s.readConfig(ctx, tx)
	if e != nil {
		return ConfigView{}, e
	}
	out, e := s.view(ctx, tx, v)
	if e != nil {
		return out, e
	}
	if e = s.admin(ctx, tx, a); e != nil {
		return ConfigView{}, e
	}
	return out, nil
}
func validateCredentials(v Snapshot) bool {
	for _, p := range credentialValues(&v.Credentials) {
		if len(*p) > 4096 {
			return false
		}
		for _, c := range *p {
			if c < 33 || c > 126 {
				return false
			}
		}
	}
	c := v.Credentials
	if v.Mode < 0 || v.Mode > 5 || v.Fee < 0 || v.Fee > 10000000 || v.ChannelID < 0 || v.OCRType != "" && v.OCRType != "aliyun" && v.OCRType != "baidu" {
		return false
	}
	if v.Mode == 0 && (v.Force || v.Corporate) {
		return false
	}
	if (v.Mode == 1 || v.Mode == 3) && v.ChannelID == 0 {
		return false
	}
	if v.Mode == 2 && c.AppCode == "" || v.Mode == 4 && (c.TencentID == "" || c.TencentKey == "" || c.TencentRuleID == "") || v.Mode == 5 && (c.AliyunID == "" || c.AliyunKey == "" || c.AliyunScene == "") {
		return false
	}
	if v.Corporate && c.CompanyAppCode == "" {
		return false
	}
	if v.OCRType == "aliyun" && (c.OCRAliyunID == "" || c.OCRAliyunKey == "") || v.OCRType == "baidu" && (c.OCRBaiduID == "" || c.OCRBaiduKey == "") {
		return false
	}
	return true
}
func (s *Service) UpdateConfig(ctx context.Context, a Actor, in ConfigInput) (ConfigView, error) {
	fee, e := ledger.Parse(in.Money)
	if e != nil {
		return ConfigView{}, ErrInvalid
	}
	id, e := strconv.ParseInt(in.ChannelID, 10, 64)
	if e != nil || strconv.FormatInt(id, 10) != in.ChannelID {
		return ConfigView{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ConfigView{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(749732332)"); e != nil {
		return ConfigView{}, e
	}
	if e = s.admin(ctx, tx, a); e != nil {
		return ConfigView{}, e
	}
	v, e := s.readConfig(ctx, tx)
	if e != nil {
		return ConfigView{}, e
	}
	if v.Revision != in.ExpectedRevision {
		return ConfigView{}, ErrConflict
	}
	v.Revision++
	v.Mode = in.Mode
	v.ChannelID = id
	v.Corporate = in.Corporate
	v.Force = in.Force
	v.Fee = fee
	v.OCRType = in.OCRType
	v.Alipay = nil
	values := credentialValues(&v.Credentials)
	if len(in.Credentials) != len(values) {
		return ConfigView{}, ErrInvalid
	}
	for k, p := range values {
		edit, ok := in.Credentials[k]
		if !ok {
			return ConfigView{}, ErrInvalid
		}
		switch edit.Action {
		case "keep":
			if edit.Value != "" {
				return ConfigView{}, ErrInvalid
			}
		case "replace":
			if edit.Value == "" {
				return ConfigView{}, ErrInvalid
			}
			*p = edit.Value
		case "clear":
			if edit.Value != "" {
				return ConfigView{}, ErrInvalid
			}
			*p = ""
		default:
			return ConfigView{}, ErrInvalid
		}
	}
	if !validateCredentials(v) {
		return ConfigView{}, ErrInvalid
	}
	if v.Mode == 1 || v.Mode == 3 {
		if s.resolve == nil {
			return ConfigView{}, ErrUnavailable
		}
		channel, e := s.resolve(ctx, tx, id)
		if e != nil || channel.Plugin != "alipay" {
			return ConfigView{}, ErrInvalid
		}
	}
	raw, e := s.seal(fmt.Sprint("config/", v.Revision), v)
	if e != nil {
		return ConfigView{}, e
	}
	proof := s.digest("config", v.Revision, raw)
	if _, e = tx.Exec(ctx, `INSERT INTO kyc_settings233(id,revision,config_cipher,proof_hash) VALUES(1,$1,$2,$3) ON CONFLICT(id) DO UPDATE SET revision=EXCLUDED.revision,config_cipher=EXCLUDED.config_cipher,proof_hash=EXCLUDED.proof_hash`, v.Revision, raw, proof); e != nil {
		return ConfigView{}, e
	}
	changes, _ := json.Marshal(map[string]string{"revision": strconv.FormatInt(v.Revision, 10), "proof_hash": hex.EncodeToString(proof)})
	if _, e = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,'kyc-settings233',1,'update',$3)", a.User.ID, a.User.Username, changes); e != nil {
		return ConfigView{}, e
	}
	if e = s.admin(ctx, tx, a); e != nil {
		return ConfigView{}, e
	}
	out, e := s.view(ctx, tx, v)
	if e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

// RequiredTx is used at payment acceptance. It authenticates both managed
// configuration and the certification record; raw profile fields never grant it.
func (s *Service) RequiredTx(ctx context.Context, tx pgx.Tx, merchant int64) error {
	var tables int
	var registry, audit bool
	if e := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname IN ('kyc_settings233','kyc_attempts233','kyc_attempt_events233','merchant_certifications233','kyc_certification_events233','ocr_operations233')),to_regclass('schema_migrations') IS NOT NULL,to_regclass('backoffice_audit') IS NOT NULL`).Scan(&tables, &registry, &audit); e != nil {
		return e
	}
	if tables != 6 {
		if tables != 0 {
			return ErrConflict
		}
		var changed bool
		if registry {
			if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE name LIKE '000192%')").Scan(&changed); e != nil {
				return e
			}
			if changed {
				return ErrConflict
			}
		}
		if audit {
			if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='kyc-settings233')").Scan(&changed); e != nil {
				return e
			}
			if changed {
				return ErrConflict
			}
		}
		return nil
	}
	v, e := s.readConfig(ctx, tx)
	if e != nil {
		return e
	}
	if !v.Force {
		return nil
	}
	_, e = s.record(ctx, tx, merchant)
	return e
}

func utc(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
