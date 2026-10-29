package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

func tgWidgetSign(botToken string, fields map[string]string) string {
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(k + "=" + fields[k])
	}
	secret := sha256.Sum256([]byte(botToken))
	mac := hmac.New(sha256.New, secret[:])
	mac.Write([]byte(b.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

func tgCallback(f *email197Fixture, state, browser string, fields map[string]string, hash string) *httptest.ResponseRecorder {
	v := url.Values{}
	v.Set("state", state)
	for k, val := range fields {
		v.Set(k, val)
	}
	v.Set("hash", hash)
	r := httptest.NewRequest("GET", "/console/v1/public/connect/telegram/callback?"+v.Encode(), nil)
	if browser != "" {
		r.AddCookie(&http.Cookie{Name: connectLoginBrowserCookie, Value: browser})
	}
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

// TestConnectLoginTelegramEndToEnd (c4): Telegram login-widget bind + login, with
// the bot token verifying the signed identity. Unknown id refused, never logged in.
func TestConnectLoginTelegramEndToEnd(t *testing.T) {
	f := email197Local(t)
	const mid int64 = 1
	const bot = "999:telegram-bot-token"
	f.s.ConnectTokenBase = map[string]string{"telegram": bot}

	// off by default.
	if w := connectReq(f, "POST", "/console/v1/merchant/account/bindings/connect/telegram/start", true); w.Code != 503 {
		t.Fatal("telegram bind-start must be 503 while off", w.Code)
	}
	seedConnectSetting(t, f, "login_tg", "1")
	seedConnectSetting(t, f, "login_tg_bot_token", bot)

	// bind start -> telegram widget config + state + nonce.
	w := connectReq(f, "POST", "/console/v1/merchant/account/bindings/connect/telegram/start", true)
	if w.Code != 200 {
		t.Fatal("telegram bind-start", w.Code, w.Body.String())
	}
	var start struct {
		Data struct {
			Telegram bool   `json:"telegram"`
			State    string `json:"state"`
		}
	}
	json.Unmarshal(w.Body.Bytes(), &start)
	if !start.Data.Telegram || start.Data.State == "" {
		t.Fatal("telegram start should return widget config + state", w.Body.String())
	}
	nonce := connectCookie(w, connectLoginBrowserCookie)

	fields := map[string]string{"id": "555000", "first_name": "Zed", "auth_date": strconv.FormatInt(time.Now().Unix(), 10)}
	hash := tgWidgetSign(bot, fields)

	// tampered hash refused.
	if cb := tgCallback(f, start.Data.State, nonce, fields, "deadbeef"); cb.Code != 403 {
		t.Fatal("tampered telegram hash must be 403", cb.Code)
	}
	// valid bind links the telegram id. (Re-issue state since the previous
	// consume on the failed attempt did not occur — the failed one happened
	// after state consume, so issue a fresh bind.)
	w2 := connectReq(f, "POST", "/console/v1/merchant/account/bindings/connect/telegram/start", true)
	var start2 struct {
		Data struct {
			State string `json:"state"`
		}
	}
	json.Unmarshal(w2.Body.Bytes(), &start2)
	nonce2 := connectCookie(w2, connectLoginBrowserCookie)
	cb := tgCallback(f, start2.Data.State, nonce2, fields, hash)
	if cb.Code != 302 {
		t.Fatal("telegram bind callback", cb.Code, cb.Body.String())
	}
	var bound string
	if e := f.p.QueryRow(t.Context(), `SELECT profile->>'telegram_uid' FROM merchants WHERE id=$1`, mid).Scan(&bound); e != nil || bound != "555000" {
		t.Fatal("telegram id not linked", bound, e)
	}

	// login with the bound id -> session.
	ls := connectLoginStart(f, "telegram")
	var lstart struct {
		Data struct {
			State string `json:"state"`
		}
	}
	json.Unmarshal(ls.Body.Bytes(), &lstart)
	lnonce := connectCookie(ls, connectLoginBrowserCookie)
	lc := tgCallback(f, lstart.Data.State, lnonce, fields, hash)
	if lc.Code != 302 || connectCookie(lc, "goepay_merchant") == "" {
		t.Fatal("bound telegram login should set a session", lc.Code, lc.Body.String())
	}

	// unknown id -> refused, no cookie.
	us := connectLoginStart(f, "telegram")
	var ustart struct {
		Data struct {
			State string `json:"state"`
		}
	}
	json.Unmarshal(us.Body.Bytes(), &ustart)
	unonce := connectCookie(us, connectLoginBrowserCookie)
	unknown := map[string]string{"id": "111222", "auth_date": strconv.FormatInt(time.Now().Unix(), 10)}
	uc := tgCallback(f, ustart.Data.State, unonce, unknown, tgWidgetSign(bot, unknown))
	// Unknown identity is NOT logged in: the connectstate135 pending-connect flow
	// records a single-use pending state and redirects to the login page carrying
	// ?connect=<pending>, where 商户ID + 密钥 completes the bind (see
	// connect_login_complete_test.go). Never a session cookie.
	if uc.Code != 302 {
		t.Fatal("unknown telegram id must land on the login page, never logged in", uc.Code, uc.Body.String())
	}
	uloc, _ := url.Parse(uc.Header().Get("Location"))
	if !strings.HasSuffix(uloc.Path, "/merchant/login") || uloc.Query().Get("connect") == "" {
		t.Fatal("unknown telegram id must carry a pending connect state", uloc.String())
	}
	if connectCookie(uc, "goepay_merchant") != "" {
		t.Fatal("unknown telegram id must not set a session cookie")
	}
}
