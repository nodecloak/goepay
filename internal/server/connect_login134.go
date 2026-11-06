package server

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	connect "goepay/internal/adapters/connectlogin"
	"goepay/internal/connectconfig"
	"goepay/internal/connectstate"
	"goepay/internal/identity"
)

// connectLoginBrowserCookie carries the single-use browser nonce that binds a
// quick-login state to this browser. Short-lived, HttpOnly, SameSite=Lax (the
// provider redirect is a top-level GET back to us).
const connectLoginBrowserCookie = "goepay_connect_nonce"

// providerSettingKeys maps a provider to its (enable, appid, appkey) settings
// keys, matching epay_release admin/set.php.
var providerSettingKeys = map[string][3]string{
	"qq":     {"login_qq", "login_qq_appid", "login_qq_appkey"},
	"wx":     {"login_wx", "login_appid", "login_appkey"},
	"alipay": {"login_alipay", "login_appid", "login_appkey"},
	// telegram uses a bot token, not appid/appkey; enable + token keys.
	"telegram": {"login_tg", "login_tg_bot_token", "login_tg_bot_token"},
}

// connectConfigField maps a provider to its connectconfig secret-store field.
var connectConfigField = map[string]string{"qq": "qq", "wx": "wx", "alipay": "alipay", "telegram": "telegram"}

// connectConfigStore builds the write-only OAuth secret store from the
// deployment checkout key. Nil (no key / no pool) means the store is
// unavailable and callers fall back to the legacy plaintext setting.
func (s *Server) connectConfigStore() *connectconfig.Store {
	if s.Pool == nil || len(s.CheckoutKey) < 32 {
		return nil
	}
	store, e := connectconfig.NewStore(s.Pool, s.CheckoutKey)
	if e != nil {
		return nil
	}
	return store
}

// connectSecret returns a provider's secret (appkey, or the Telegram bot
// token) from the write-only secret store, falling back to the legacy
// plaintext setting when the store has no value for it. The secret store is
// the primary source; the plaintext key stays readable for values set the old
// way and for the test seams.
func (s *Server) connectSecret(r *http.Request, provider, legacyKey string) string {
	if field, ok := connectConfigField[provider]; ok {
		if store := s.connectConfigStore(); store != nil {
			if v, e := store.Resolve(r.Context(), field); e == nil && v != "" {
				return v
			}
		}
	}
	return s.connectSetting(r, legacyKey)
}

// connectLoginConfig reads a provider's admin keys and whether quick login is
// enabled for it. The enable switch and appid are plaintext settings; the
// appkey is the write-only secret (falling back to the legacy plaintext key).
// Off (missing/"0") by default.
func (s *Server) connectLoginConfig(r *http.Request, provider string) (enabled bool, appid, appkey string) {
	keys, ok := providerSettingKeys[provider]
	if !ok || s.Pool == nil {
		return false, "", ""
	}
	rows, e := s.Pool.Query(r.Context(), `SELECT body->>'key',COALESCE(body->>'value','') FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1::text[]) ORDER BY id`, []string{keys[0], keys[1], keys[2]})
	if e != nil {
		return false, "", ""
	}
	defer rows.Close()
	m := map[string]string{}
	for rows.Next() {
		var k, v string
		if rows.Scan(&k, &v) == nil {
			m[k] = v
		}
	}
	on := m[keys[0]]
	enabled = on != "" && on != "0"
	appkey = s.connectSecret(r, provider, keys[2])
	return enabled, m[keys[1]], appkey
}

func (s *Server) connectProvider(r *http.Request, provider string) (connect.Provider, bool) {
	enabled, appid, appkey := s.connectLoginConfig(r, provider)
	if !enabled || appid == "" || appkey == "" {
		return nil, false
	}
	// callback base is the configured public origin; derived, never proxy input.
	base := s.connectCallbackBase()
	if base == "" {
		return nil, false
	}
	callback := base + "/console/v1/public/connect/" + provider + "/callback"
	switch provider {
	case "qq":
		qq := connect.QQ{AppID: appid, AppKey: appkey, CallbackURL: callback}
		if s.ConnectTokenBase != nil {
			if b := s.ConnectTokenBase[provider]; b != "" {
				qq.TokenBase = b
				qq.AuthBase = b + "/authorize"
			}
		}
		return qq, true
	case "wx", "alipay":
		apiurl := s.connectSetting(r, "login_apiurl")
		if s.ConnectTokenBase != nil {
			if b := s.ConnectTokenBase[provider]; b != "" {
				apiurl = b
			}
		}
		if apiurl == "" {
			return nil, false
		}
		return connect.OauthRelay{APIBase: apiurl, AppID: appid, AppKey: appkey, Type: provider, CallbackURL: callback}, true
	case "telegram":
		// Telegram has no server-side authorize/exchange: the login widget signs
		// the identity client-side and posts it to the callback, verified by
		// telegramIdentity. ok=true (nil Provider) means "configured & enabled".
		return nil, true
	}
	return nil, false
}

// connectSetting reads one settings-record value by key (empty when absent).
func (s *Server) connectSetting(r *http.Request, key string) string {
	if s.Pool == nil {
		return ""
	}
	var v string
	if e := s.Pool.QueryRow(r.Context(), `SELECT COALESCE(body->>'value','')FROM backoffice_records WHERE resource='settings' AND body->>'key'=$1 ORDER BY id LIMIT 1`, key).Scan(&v); e != nil {
		return ""
	}
	return v
}

// telegramIdentity verifies the Telegram login-widget query params and returns
// the Telegram user id. The bot token comes from settings (login_tg_bot_token),
// or the ConnectTokenBase test seam. Everything except `hash` and our own
// `state` is part of the signed data-check-string.
func (s *Server) telegramIdentity(r *http.Request, q url.Values) (string, error) {
	token := s.connectSecret(r, "telegram", "login_tg_bot_token")
	if s.ConnectTokenBase != nil {
		if b := s.ConnectTokenBase["telegram"]; b != "" {
			token = b
		}
	}
	if token == "" {
		return "", connect.ErrTelegram
	}
	fields := map[string]string{}
	for k := range q {
		if k == "hash" || k == "state" {
			continue
		}
		fields[k] = q.Get(k)
	}
	tg := connect.Telegram{BotToken: token}
	return tg.Verify(fields, q.Get("hash"))
}

// connectCallbackBase returns the configured public origin (scheme://host) for
// building provider callback URLs, from the server's CallbackBaseURL; empty when
// unknown (feature unavailable).
func (s *Server) connectCallbackBase() string {
	if s.ProviderFlow == nil {
		return ""
	}
	return strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/")
}

func connectBrowserCookie(r *http.Request) string {
	if c, e := r.Cookie(connectLoginBrowserCookie); e == nil {
		return c.Value
	}
	return ""
}

// accountBindingsConnectStart handles the authenticated 3rd-party BIND start:
// POST account/bindings/connect/{provider}/start -> issue a bind state + browser
// cookie, return the provider authorize URL. CSRF + merchant session required.
func (s *Server) accountBindingsConnectStart(w http.ResponseWriter, r *http.Request, session identity.Session, provider string) bool {
	identityHeaders(w)
	if r.Method != "POST" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if session.Issuer != nil || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
		response(w, 403, -1, "original merchant session required", nil)
		return true
	}
	qq, ok := s.connectProvider(r, provider)
	if !ok {
		response(w, 503, -1, "该第三方快捷登录未开启", nil)
		return true
	}
	states := connectstate.New(s.Pool)
	iss, e := states.Issue(r.Context(), provider, "bind", session.User.ID)
	if e != nil {
		response(w, 503, -1, "无法发起绑定", nil)
		return true
	}
	if provider == "telegram" {
		// The Telegram login widget signs the identity client-side; return the
		// state + callback so the UI renders the widget pointing back here.
		s.setConnectBrowserCookie(w, iss.Browser)
		response(w, 200, 0, "", map[string]any{"telegram": true, "state": iss.State, "callback": s.connectCallbackBase() + "/console/v1/public/connect/telegram/callback"})
		return true
	}
	authCtx, cancelAuth := context.WithTimeout(r.Context(), 12*time.Second)
	authURL, e := qq.AuthorizeURL(authCtx, iss.State)
	cancelAuth()
	if e != nil {
		response(w, 503, -1, "无法构造授权地址", nil)
		return true
	}
	s.setConnectBrowserCookie(w, iss.Browser)
	response(w, 200, 0, "", map[string]any{"authorize_url": authURL})
	return true
}

func (s *Server) setConnectBrowserCookie(w http.ResponseWriter, nonce string) {
	secure := s.Sessions == nil || s.Sessions.Secure
	http.SetCookie(w, &http.Cookie{Name: connectLoginBrowserCookie, Value: nonce, Path: "/console/v1/public/connect", HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode, MaxAge: 600})
}

// publicConnectLogin handles the unauthenticated quick-login endpoints:
//
//	GET /console/v1/public/connect/{provider}/start    -> issue login state, redirect to provider
//	GET /console/v1/public/connect/{provider}/callback -> consume + exchange + login/bind
func (s *Server) publicConnectLogin(w http.ResponseWriter, r *http.Request) bool {
	const prefix = "/console/v1/public/connect/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		return false
	}
	rest := strings.TrimPrefix(r.URL.Path, prefix)
	segs := strings.Split(rest, "/")
	// connect/options -> which providers are enabled (for the login page).
	if len(segs) == 1 && segs[0] == "options" {
		return s.connectOptions(w, r)
	}
	// connect/complete -> finish an unknown-identity login with 商户ID + 密钥.
	if len(segs) == 1 && segs[0] == "complete" {
		return s.connectComplete(w, r)
	}
	if len(segs) != 2 {
		response(w, 404, -1, "not found", nil)
		return true
	}
	provider, action := segs[0], segs[1]
	w.Header().Set("Cache-Control", "no-store")
	qq, ok := s.connectProvider(r, provider)
	if !ok {
		response(w, 503, -1, "该第三方快捷登录未开启", nil)
		return true
	}
	states := connectstate.New(s.Pool)
	switch action {
	case "start":
		if r.Method != "GET" {
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		iss, e := states.Issue(r.Context(), provider, "login", 0)
		if e != nil {
			response(w, 503, -1, "无法发起登录", nil)
			return true
		}
		if provider == "telegram" {
			s.setConnectBrowserCookie(w, iss.Browser)
			response(w, 200, 0, "", map[string]any{"telegram": true, "state": iss.State, "callback": s.connectCallbackBase() + "/console/v1/public/connect/telegram/callback"})
			return true
		}
		authCtx, cancelAuth := context.WithTimeout(r.Context(), 12*time.Second)
		authURL, e := qq.AuthorizeURL(authCtx, iss.State)
		cancelAuth()
		if e != nil {
			response(w, 503, -1, "无法构造授权地址", nil)
			return true
		}
		s.setConnectBrowserCookie(w, iss.Browser)
		http.Redirect(w, r, authURL, http.StatusFound)
		return true
	case "callback":
		if r.Method != "GET" {
			response(w, 405, -1, "method not allowed", nil)
			return true
		}
		q := r.URL.Query()
		state := q.Get("state")
		browser := connectBrowserCookie(r)
		if state == "" || browser == "" {
			response(w, 400, -1, "登录回调参数不完整", nil)
			return true
		}
		claim, e := states.Consume(r.Context(), provider, state, browser)
		if e != nil {
			// Mismatch / expired / reused state — possible CSRF; never proceed.
			response(w, 403, -1, "登录状态校验失败，请重新发起", nil)
			return true
		}
		var openid string
		if provider == "telegram" {
			openid, e = s.telegramIdentity(r, q)
			if e != nil || openid == "" {
				response(w, 403, -1, "Telegram 登录校验失败", nil)
				return true
			}
		} else {
			code := q.Get("code")
			if code == "" {
				response(w, 400, -1, "登录回调参数不完整", nil)
				return true
			}
			exchangeCtx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
			openid, e = qq.Exchange(exchangeCtx, code)
			cancel()
			if e != nil || openid == "" {
				response(w, 502, -1, "获取登录数据失败", nil)
				return true
			}
		}
		if claim.Purpose == "bind" {
			return s.connectBindOpenID(w, r, provider, claim.Merchant, openid)
		}
		return s.connectLoginOpenID(w, r, provider, openid, browser)
	}
	response(w, 404, -1, "not found", nil)
	return true
}

// connectOptions reports which quick-login providers are enabled, for the login
// page to render only the configured buttons. Reads the admin keys; no secret.
func (s *Server) connectOptions(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "GET" {
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	out := map[string]bool{}
	for _, p := range []string{"qq", "wx", "alipay", "telegram"} {
		_, ok := s.connectProvider(r, p)
		out[p] = ok
	}
	response(w, 200, 0, "", map[string]any{"providers": out})
	return true
}

// connectComplete finishes an unknown-identity quick login: the merchant proves
// ownership with 商户ID + 密钥 (the existing key-login checks + MFA still apply),
// the single-use pending-connect is consumed, the identity is bound to that
// merchant, and the merchant is logged in. Without a valid pending state it does
// nothing.
func (s *Server) connectComplete(w http.ResponseWriter, r *http.Request) bool {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" {
		w.Header().Set("Allow", "POST")
		response(w, 405, -1, "method not allowed", nil)
		return true
	}
	if s.Pool == nil || s.Store == nil || s.Sessions == nil || !s.Sessions.SupportsDelegation() {
		response(w, 503, -1, "登录服务不可用", nil)
		return true
	}
	in, ok := identityJSON(w, r, "state", "merchant_id", "api_key", "code")
	if !ok {
		return true
	}
	mid, okID := positiveFundsID(in["merchant_id"])
	if in["state"] == "" || !okID || in["api_key"] == "" || len(in["api_key"]) > 512 || len(in["code"]) > 16 {
		response(w, 400, -1, "请输入商户ID和密钥完成绑定和登录", nil)
		return true
	}
	browser := connectBrowserCookie(r)
	if browser == "" {
		response(w, 403, -1, "绑定状态校验失败，请重新发起", nil)
		return true
	}
	// Consume the single-use pending-connect FIRST: a wrong/expired/replayed
	// state does nothing (and even a correct one is spent before we try the key,
	// so a failed key attempt cannot be retried against the same state).
	pending, e := connectstate.New(s.Pool).ConsumePending(r.Context(), in["state"], browser)
	if e != nil {
		response(w, 403, -1, "绑定状态无效或已失效，请重新发起", nil)
		return true
	}
	if !claimLoginVerification() {
		w.Header().Set("Retry-After", "1")
		response(w, 429, -1, "login verification busy", nil)
		return true
	}
	defer releaseLoginVerification()
	grant, prepareError := s.Store.PrepareMerchantKeyLogin(r.Context(), mid, in["api_key"])
	username := grant.User().Username
	if prepareError != nil {
		response(w, 401, -1, "invalid credentials", nil)
		return true
	}
	if !s.checkMFA(r.Context(), username, "merchant", in["code"]) {
		response(w, 401, -1, "invalid credentials or authenticator code", nil)
		return true
	}
	// Bind the resolved identity to this merchant (refuse if already bound
	// elsewhere), then complete the key login to mint the session.
	uidKey := connectUIDKey(pending.Provider)
	tx, e := s.Pool.Begin(r.Context())
	if e != nil {
		s.fail(w, e)
		return true
	}
	defer tx.Rollback(r.Context())
	var other int64
	if e = tx.QueryRow(r.Context(), `SELECT id FROM merchants WHERE profile->>$1=$2 AND id<>$3 LIMIT 1`, uidKey, pending.SocialUID, mid).Scan(&other); e == nil {
		response(w, 409, -1, "该第三方账号已被其他商户绑定", nil)
		return true
	} else if !errors.Is(e, pgx.ErrNoRows) {
		s.fail(w, e)
		return true
	}
	var enabled bool
	if e = tx.QueryRow(r.Context(), `SELECT enabled FROM merchants WHERE id=$1 FOR UPDATE`, mid).Scan(&enabled); e != nil || !enabled {
		response(w, 403, -1, "merchant unavailable", nil)
		return true
	}
	if _, e = tx.Exec(r.Context(), `UPDATE merchants SET profile=COALESCE(profile,'{}'::jsonb)||jsonb_build_object($2::text,$3::text)WHERE id=$1 AND enabled`, mid, uidKey, pending.SocialUID); e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			response(w, 409, -1, "该第三方账号已被其他商户绑定", nil)
			return true
		}
		s.fail(w, e)
		return true
	}
	if _, e = tx.Exec(r.Context(), `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('merchant',$1,$2,'account-bindings',$1,'bind-connect-login',jsonb_build_object('provider',$3::text))`, mid, username, pending.Provider); e != nil {
		s.fail(w, e)
		return true
	}
	if e = tx.Commit(r.Context()); e != nil {
		s.fail(w, e)
		return true
	}
	user, token, csrf, e := s.Store.CompleteMerchantKeyLogin(r.Context(), grant, s.Sessions)
	if e != nil {
		response(w, 401, -1, "invalid credentials", nil)
		return true
	}
	success := true
	s.recordLogin(r, "merchant", username, &success, user)
	http.SetCookie(w, &http.Cookie{Name: "goepay_merchant", Value: token, Path: "/console/v1/merchant", HttpOnly: true, Secure: s.Sessions.Secure, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	response(w, 200, 0, "", map[string]any{"user": consoleSessionUser(user), "csrf_token": csrf})
	return true
}

// connectBindOpenID links openid -> the signed-in merchant (from the bind
// state), refusing if that identity is already bound to someone.
func (s *Server) connectBindOpenID(w http.ResponseWriter, r *http.Request, provider string, merchant int64, openid string) bool {
	uidKey := connectUIDKey(provider)
	tx, e := s.Pool.Begin(r.Context())
	if e != nil {
		s.fail(w, e)
		return true
	}
	defer tx.Rollback(r.Context())
	// This openid must not already belong to another merchant.
	var other int64
	e = tx.QueryRow(r.Context(), `SELECT id FROM merchants WHERE profile->>$1=$2 AND id<>$3 LIMIT 1`, uidKey, openid, merchant).Scan(&other)
	if e == nil {
		response(w, 409, -1, "该第三方账号已被其他商户绑定", nil)
		return true
	} else if !errors.Is(e, pgx.ErrNoRows) {
		s.fail(w, e)
		return true
	}
	var enabled bool
	if e = tx.QueryRow(r.Context(), `SELECT enabled FROM merchants WHERE id=$1 FOR UPDATE`, merchant).Scan(&enabled); e != nil || !enabled {
		response(w, 403, -1, "merchant unavailable", nil)
		return true
	}
	if _, e = tx.Exec(r.Context(), `UPDATE merchants SET profile=COALESCE(profile,'{}'::jsonb)||jsonb_build_object($2::text,$3::text)WHERE id=$1 AND enabled`, merchant, uidKey, openid); e != nil {
		var pg *pgconn.PgError
		if errors.As(e, &pg) && pg.Code == "23505" {
			response(w, 409, -1, "该第三方账号已被其他商户绑定", nil)
			return true
		}
		s.fail(w, e)
		return true
	}
	if _, e = tx.Exec(r.Context(), `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('merchant',$1,'','account-bindings',$1,'bind-connect',jsonb_build_object('provider',$2::text))`, merchant, provider); e != nil {
		s.fail(w, e)
		return true
	}
	if e = tx.Commit(r.Context()); e != nil {
		s.fail(w, e)
		return true
	}
	// Redirect back to account settings (top-level GET).
	http.Redirect(w, r, s.connectAccountReturn(), http.StatusFound)
	return true
}

// connectLoginOpenID logs in the merchant BOUND to this openid. An unknown
// openid is NOT logged in: it is recorded as a single-use pending-connect and
// the browser is sent to the login page carrying the original's prompt, where
// the merchant confirms with 商户ID + 密钥 to bind and log in.
func (s *Server) connectLoginOpenID(w http.ResponseWriter, r *http.Request, provider, openid, browser string) bool {
	if s.Sessions == nil {
		response(w, 503, -1, "登录服务不可用", nil)
		return true
	}
	uidKey := connectUIDKey(provider)
	var mid int64
	var username string
	var version int64
	var enabled bool
	e := s.Pool.QueryRow(r.Context(), `SELECT m.id,m.enabled,COALESCE(u.username,''),COALESCE(u.auth_version,0)
 FROM merchants m LEFT JOIN LATERAL (SELECT username,auth_version FROM console_users WHERE role='merchant' AND subject_id=m.id AND enabled ORDER BY username LIMIT 1) u ON true
 WHERE m.profile->>$1=$2 LIMIT 1`, uidKey, openid).Scan(&mid, &enabled, &username, &version)
	if e != nil || !enabled || username == "" {
		// Unknown identity — never auto-login/register. Record a single-use
		// pending-connect and land on the login page with the original prompt.
		pending, pe := connectstate.New(s.Pool).IssuePending(r.Context(), provider, openid, browser)
		if pe != nil {
			response(w, 403, -1, "请输入商户ID和密钥完成绑定和登录", nil)
			return true
		}
		base := s.connectCallbackBase()
		http.Redirect(w, r, base+"/merchant/login?connect="+url.QueryEscape(pending), http.StatusFound)
		return true
	}
	// A provider callback does not include an authenticator code. Refuse the
	// shortcut for accounts with MFA rather than issuing an incomplete session.
	// Enrolling MFA increments auth_version, so a concurrent change also makes
	// persistent session creation fail against the version read above.
	if !s.checkMFA(r.Context(), username, "merchant", "") {
		response(w, 403, -1, "此账号已启用双重验证，请使用账号登录", nil)
		return true
	}
	user := identity.User{ID: mid, Role: "merchant", Username: username, AuthVersion: version}
	token, _, e := s.Sessions.Create(user)
	if e != nil {
		response(w, 503, -1, "登录会话创建失败", nil)
		return true
	}
	success := true
	s.recordLogin(r, "merchant", username, &success, user)
	secure := s.Sessions.Secure
	http.SetCookie(w, &http.Cookie{Name: "goepay_merchant", Value: token, Path: "/console/v1/merchant", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	// The SPA re-establishes CSRF via /session after the redirect.
	http.Redirect(w, r, s.connectConsoleReturn(), http.StatusFound)
	return true
}

func (s *Server) connectAccountReturn() string {
	base := s.connectCallbackBase()
	if base == "" {
		return "/"
	}
	return base + "/merchant/account/settings"
}
func (s *Server) connectConsoleReturn() string {
	base := s.connectCallbackBase()
	if base == "" {
		return "/"
	}
	return base + "/merchant"
}
