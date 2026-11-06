package publictestpay

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/captcha"
	"goepay/internal/payment"
	"goepay/internal/siteinfo"
)

var ErrClosed = errors.New("public test payment is closed")
var ErrInvalid = errors.New("invalid public test payment")
var ErrForbidden = errors.New("public browser authorization required")
var ErrConflict = errors.New("original public test payment requires read-only verification")
var ErrUnavailable = errors.New("public test payment unavailable")
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var moneyPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,6})(\.[0-9]{1,2})?$`)

type PayType struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	ShowName string `json:"showname"`
}
type Intent struct {
	ID                                   string
	Browser, CSRF                        []byte
	MerchantID                           int64
	OutTradeNo, ConfigHash               string
	Amount                               int64
	TypeID, PayType, State, OrderTradeNo string
	ExpiresAt                            time.Time
}
type Engine interface {
	Types(context.Context, int64) ([]PayType, error)
	Create(context.Context, Intent, string) (string, string, error)
	Checkout(context.Context, payment.Order) (string, error)
}
type Service struct {
	pool         *pgxpool.Pool
	store        *payment.Store
	key          []byte
	origin       string
	publicOrigin bool
	credentials  *captcha.Store
	engine       Engine
	verifier     captcha.Verifier
	limiter      func(string, string) bool
}

func New(pool *pgxpool.Pool, store *payment.Store, key []byte, origin string, engine Engine, verifier captcha.Verifier, limiter func(string, string) bool) (*Service, error) {
	publicOrigin := origin != ""
	if !publicOrigin {
		origin = "https://public-disabled.invalid"
	}
	u, e := url.Parse(origin)
	if pool == nil || store == nil || engine == nil || len(key) < 32 || e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" || u.Scheme != "https" && u.Scheme != "http" {
		return nil, ErrUnavailable
	}
	credentials, e := captcha.NewStore(pool, key)
	if e != nil {
		return nil, e
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte("goepay/public-testpay203/integrity"))
	return &Service{pool: pool, store: store, key: mac.Sum(nil), origin: u.Scheme + "://" + u.Host, publicOrigin: publicOrigin, credentials: credentials, engine: engine, verifier: verifier, limiter: limiter}, nil
}
func (s *Service) Credentials() *captcha.Store { return s.credentials }
func (s *Service) sum(purpose string, raw []byte) []byte {
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(purpose + "\n"))
	mac.Write(raw)
	return mac.Sum(nil)
}
func (s *Service) intentProof(in Intent) []byte {
	in.ExpiresAt = in.ExpiresAt.UTC()
	raw, _ := json.Marshal(in)
	return s.sum("intent", raw)
}
func randomToken() (string, error) {
	raw := make([]byte, 32)
	if _, e := rand.Read(raw); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func (s *Service) browser(r *http.Request) (string, error) {
	c, e := r.Cookie("goepay_testpay203")
	if e != nil || !tokenPattern.MatchString(c.Value) {
		return "", ErrForbidden
	}
	return c.Value, nil
}
func (s *Service) readIntent(ctx context.Context, tx pgx.Tx, browser, csrf string) (Intent, error) {
	var in Intent
	var proof []byte
	e := tx.QueryRow(ctx, `SELECT id,browser_hash,csrf_hash,merchant_id,out_trade_no,config_hash,amount,type_id,pay_type,state,order_trade_no,expires_at,proof_hash FROM public_testpay203_intents WHERE browser_hash=$1 AND csrf_hash=$2 FOR UPDATE`, s.sum("browser", []byte(browser)), s.sum("csrf", []byte(csrf))).Scan(&in.ID, &in.Browser, &in.CSRF, &in.MerchantID, &in.OutTradeNo, &in.ConfigHash, &in.Amount, &in.TypeID, &in.PayType, &in.State, &in.OrderTradeNo, &in.ExpiresAt, &proof)
	if errors.Is(e, pgx.ErrNoRows) {
		return in, ErrForbidden
	}
	if e != nil {
		return in, e
	}
	if !hmac.Equal(proof, s.intentProof(in)) {
		return in, ErrConflict
	}
	return in, nil
}
func (s *Service) saveIntent(ctx context.Context, tx pgx.Tx, in Intent) error {
	_, e := tx.Exec(ctx, `UPDATE public_testpay203_intents SET amount=$2,type_id=$3,pay_type=$4,state=$5,order_trade_no=$6,proof_hash=$7 WHERE id=$1`, in.ID, in.Amount, in.TypeID, in.PayType, in.State, in.OrderTradeNo, s.intentProof(in))
	return e
}
func alive(ctx context.Context, tx pgx.Tx, expiry time.Time) error {
	var now time.Time
	if e := tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); e != nil {
		return e
	}
	if !now.Before(expiry) {
		return ErrForbidden
	}
	return nil
}

type configuration struct {
	Open, Captcha bool
	MerchantID    int64
	SiteName      string
	CaptchaConfig captcha.Config
	Fingerprint   string
}

func (s *Service) config(ctx context.Context, tx pgx.Tx) (configuration, error) {
	var c configuration
	keys := []string{"test_open", "test_pay_uid", "captcha_open_test", "captcha_id", "captcha_version"}
	rows, e := tx.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1) FOR SHARE`, keys)
	if e != nil {
		return c, e
	}
	values := map[string]string{}
	for rows.Next() {
		var k string
		var raw []byte
		if e = rows.Scan(&k, &raw); e != nil {
			rows.Close()
			return c, e
		}
		if _, ok := values[k]; ok {
			rows.Close()
			return c, ErrUnavailable
		}
		var value string
		if json.Unmarshal(raw, &value) != nil || strings.TrimSpace(value) != value {
			rows.Close()
			return c, ErrUnavailable
		}
		values[k] = value
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return c, e
	}
	for _, k := range []string{"test_open", "captcha_open_test", "captcha_version"} {
		if values[k] == "" {
			values[k] = "0"
		}
		if values[k] != "0" && values[k] != "1" {
			return c, ErrUnavailable
		}
	}
	c.Open = values["test_open"] == "1"
	c.Captcha = values["captcha_open_test"] == "1"
	c.MerchantID, _ = strconv.ParseInt(values["test_pay_uid"], 10, 64)
	if c.Open && (c.MerchantID <= 0 || strconv.FormatInt(c.MerchantID, 10) != values["test_pay_uid"]) {
		return c, ErrUnavailable
	}
	site, e := siteinfo.Load(ctx, tx)
	if e != nil {
		return c, e
	}
	c.SiteName = site.Name
	view, key, e := s.credentials.ReadTx(ctx, tx)
	if e != nil {
		return c, e
	}
	version, _ := strconv.Atoi(values["captcha_version"])
	c.CaptchaConfig = captcha.Config{Version: version, ID: values["captcha_id"], Key: key}
	raw, _ := json.Marshal(struct {
		Values     map[string]string
		Credential string
	}{values, view.Fingerprint})
	c.Fingerprint = hex.EncodeToString(s.sum("configuration", raw))
	return c, nil
}
