package server

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/payment"
)

const redpackBrowserCookie = "goepay_red"

func (s *Server) redpackKey() []byte {
	mac := hmac.New(sha256.New, s.CheckoutKey)
	mac.Write([]byte("goepay/redpack/claim-link/v1"))
	return mac.Sum(nil)
}

func (s *Server) redpackSign(parts ...string) string {
	mac := hmac.New(sha256.New, s.redpackKey())
	mac.Write([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(mac.Sum(nil))
}

// redpackClaimURL is the signed QR link (original paypage/red.php?n=&t=&s=,
// whose md5(SYS_KEY..) signature is replaced by HMAC-SHA256).
func (s *Server) redpackClaimURL(r payment.Redpack) string {
	n, t := strconv.FormatInt(r.ID, 10), strconv.FormatInt(r.CreatedAt.Unix(), 10)
	base := ""
	if s.ProviderFlow != nil {
		base = strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/")
	}
	return base + "/pay/red?" + url.Values{"n": {n}, "t": {t}, "s": {s.redpackSign("link", n, t)}}.Encode()
}

func (s *Server) redpackFromLink(ctx context.Context, q url.Values) (payment.Redpack, bool) {
	n, t, sig := q.Get("n"), q.Get("t"), q.Get("s")
	id, err := strconv.ParseInt(n, 10, 64)
	if err != nil || id <= 0 || len(sig) != 64 || !hmac.Equal([]byte(sig), []byte(s.redpackSign("link", n, t))) {
		return payment.Redpack{}, false
	}
	r, err := s.Store.Redpack(ctx, id)
	if err != nil || strconv.FormatInt(r.CreatedAt.Unix(), 10) != t {
		return payment.Redpack{}, false
	}
	return r, true
}

type redpackView struct {
	payment.Redpack
	Money    string `json:"money"`
	ClaimURL string `json:"claim_url"`
}

func (s *Server) redpackView(r payment.Redpack) redpackView {
	return redpackView{Redpack: r, Money: ledger.Format(r.Amount), ClaimURL: s.redpackClaimURL(r)}
}

func (s *Server) redpackSettingOpen(ctx context.Context, mid int64) bool {
	on, err := s.Store.MerchantRedpackFeature(ctx, mid)
	return err == nil && on
}

// redpackConsole: /console/v1/{admin|merchant}/redpacks[/{id}[/query]].
func (s *Server) redpackConsole(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) < 2 || parts[1] != "redpacks" {
		return false
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "database unavailable", nil)
		return true
	}
	w.Header().Set("Cache-Control", "no-store")
	self := session.User.ID
	switch {
	case len(parts) == 2 && r.Method == http.MethodGet:
		mid := self
		if role == "admin" {
			mid = 0
			if raw := r.URL.Query().Get("merchant_id"); raw != "" {
				v, err := strconv.ParseInt(raw, 10, 64)
				if err != nil || v <= 0 {
					response(w, 422, -1, "invalid merchant_id", nil)
					return true
				}
				mid = v
			}
		}
		list, err := s.Store.Redpacks(r.Context(), mid, 100)
		if err != nil {
			s.fail(w, err)
			return true
		}
		out := make([]redpackView, 0, len(list))
		for _, p := range list {
			out = append(out, s.redpackView(p))
		}
		response(w, 200, 0, "", map[string]any{"items": out, "open": role == "admin" || s.redpackSettingOpen(r.Context(), mid)})
		return true
	case len(parts) == 2 && r.Method == http.MethodPost:
		var in struct {
			MerchantID string `json:"merchant_id"`
			ChannelID  string `json:"channel_id"`
			Type       string `json:"type"`
			OutBizNo   string `json:"out_biz_no"`
			Amount     string `json:"amount"`
			Remark     string `json:"remark"`
		}
		if !decode(w, r, &in) {
			return true
		}
		mid := self
		if role == "admin" {
			v, err := strconv.ParseInt(in.MerchantID, 10, 64)
			if err != nil || v <= 0 {
				response(w, 422, -1, "merchant_id required", nil)
				return true
			}
			mid = v
		} else {
			if in.MerchantID != "" && in.MerchantID != fmt.Sprint(self) {
				response(w, 403, -1, "merchant ownership required", nil)
				return true
			}
			if !s.redpackSettingOpen(r.Context(), mid) {
				response(w, 403, -1, "未开启创建红包功能", nil)
				return true
			}
			_, access, err := s.Store.ManualTransferPolicy(r.Context(), mid)
			if err != nil || !access.Enabled {
				response(w, 403, -1, "未开启转账付款功能", nil)
				return true
			}
		}
		amount, err := ledger.Parse(in.Amount)
		if err != nil || amount <= 0 {
			response(w, 422, -1, "红包金额无效", nil)
			return true
		}
		channel, err := s.transferChannel(mid, compat.Params{"type": in.Type, "channel_id": in.ChannelID})
		if err != nil {
			response(w, 422, -1, "转账通道不可用", nil)
			return true
		}
		p, err := s.Store.CreateRedpack(r.Context(), payment.RedpackInput{MerchantID: mid, ChannelID: channel, ActorID: self, OutBizNo: in.OutBizNo, Type: in.Type, Remark: in.Remark, Role: role, Amount: amount})
		switch {
		case err == nil:
			response(w, 200, 0, "", s.redpackView(p))
		case errors.Is(err, payment.ErrConflict):
			response(w, 409, -1, "该交易号已存在", nil)
		case errors.Is(err, payment.ErrInsufficient):
			response(w, 422, -1, "可转账余额不足", nil)
		case errors.Is(err, payment.ErrInvalid):
			response(w, 422, -1, "交易号须为19位数字，金额须在转账限额内，备注不超过32字", nil)
		default:
			s.fail(w, err)
		}
		return true
	case len(parts) == 3 || len(parts) == 4 && parts[3] == "query" && r.Method == http.MethodPost:
		id, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || id <= 0 || len(parts) == 3 && r.Method != http.MethodGet {
			response(w, 404, -1, "red packet unavailable", nil)
			return true
		}
		p, err := s.Store.Redpack(r.Context(), id)
		if err != nil || role != "admin" && p.MerchantID != self {
			response(w, 404, -1, "red packet unavailable", nil)
			return true
		}
		if len(parts) == 4 && s.ProviderFlow != nil {
			if p, _, err = s.ProviderFlow.QueryRedpack(r.Context(), id); err != nil {
				response(w, 202, 0, "领取结果仍在确认中", s.redpackView(p))
				return true
			}
		}
		response(w, 200, 0, "", s.redpackView(p))
		return true
	}
	response(w, 405, -1, "red packet method unavailable", nil)
	return true
}

var redpackPage = template.Must(template.New("red").Parse(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="robots" content="noindex,nofollow"><title>{{.Title}}</title>
<style>body{margin:0;font-family:-apple-system,"Microsoft YaHei",sans-serif;background:#c0392b;color:#fff;text-align:center}.box{max-width:420px;margin:12vh auto;padding:32px 20px}.money{font-size:44px;font-weight:600;margin:16px 0}.btn{display:inline-block;margin-top:24px;padding:12px 40px;border-radius:24px;background:#f1c40f;color:#7b241c;text-decoration:none;font-size:18px}.msg{font-size:16px;opacity:.9}</style>
</head><body><div class="box"><p class="msg">{{.Heading}}</p>{{if .Money}}<p class="money">￥{{.Money}}</p>{{end}}{{if .Remark}}<p class="msg">{{.Remark}}</p>{{end}}<p class="msg">{{.Message}}</p>{{if .Action}}<a class="btn" href="{{.Action}}">{{.ActionLabel}}</a>{{end}}</div></body></html>`))

type redpackPageData struct {
	Title, Heading, Money, Remark, Message, Action, ActionLabel string
}

func redpackRender(w http.ResponseWriter, status int, d redpackPageData) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; style-src 'unsafe-inline'; form-action 'none'")
	w.WriteHeader(status)
	_ = redpackPage.Execute(w, d)
}

func redpackWallet(t string) string {
	if t == "alipay" {
		return "支付宝余额"
	}
	return "微信零钱"
}

// redpackPublic serves /pay/red (landing), /pay/red/authorize (start the
// claimer's OAuth) and /pay/red/callback/{id}/ (claim with the platform's own
// OAuth identity; a claimer never types an account).
func (s *Server) redpackPublic(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/pay/red" && r.URL.Path != "/pay/red/authorize" && !strings.HasPrefix(r.URL.Path, "/pay/red/callback/") {
		return false
	}
	fail := func(status int, msg string) {
		redpackRender(w, status, redpackPageData{Title: "红包", Heading: "红包", Message: msg})
	}
	if r.Method != http.MethodGet {
		fail(405, "请求方式无效")
		return true
	}
	if s.Store == nil || s.Pool == nil || s.ProviderFlow == nil {
		fail(503, "红包服务暂不可用")
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/pay/red/callback/") {
		s.redpackCallback(w, r, fail)
		return true
	}
	p, ok := s.redpackFromLink(r.Context(), r.URL.Query())
	if !ok {
		fail(400, "红包链接无效")
		return true
	}
	switch p.Status {
	case "succeeded", "claiming", "failed":
		fail(409, "该红包已被领取")
		return true
	case "expired":
		fail(410, "红包已过期")
		return true
	}
	if !time.Now().Before(p.ExpiresAt) {
		fail(410, "红包已过期")
		return true
	}
	if r.URL.Path == "/pay/red" {
		redpackRender(w, 200, redpackPageData{Title: "领取红包", Heading: "恭喜发财，大吉大利", Money: ledger.Format(p.Amount), Remark: p.Remark, Message: "领取后将存入" + redpackWallet(p.Type), Action: "/pay/red/authorize?" + r.URL.RawQuery, ActionLabel: "立即领取"})
		return true
	}
	oauth := s.OAuthProviders[p.ChannelID]
	if oauth == nil || oauth.ID() != p.Type {
		fail(503, "红包领取通道暂不可用")
		return true
	}
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		fail(503, "红包服务暂不可用")
		return true
	}
	nonce := hex.EncodeToString(nonceBytes)
	n := strconv.FormatInt(p.ID, 10)
	exp := strconv.FormatInt(time.Now().Add(10*time.Minute).Unix(), 10)
	payload := base64.RawURLEncoding.EncodeToString([]byte(n + "." + exp))
	state := payload + "." + s.redpackSign("state", n, exp, nonce)[:40]
	callback := strings.TrimRight(s.ProviderFlow.CallbackBaseURL, "/") + "/pay/red/callback/" + n + "/"
	target, err := oauth.OAuthAuthorizationURL(state, callback)
	if err != nil {
		fail(503, "红包领取通道暂不可用")
		return true
	}
	http.SetCookie(w, &http.Cookie{Name: redpackBrowserCookie, Value: nonce, Path: "/pay/red/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	http.Redirect(w, r, target, http.StatusFound)
	return true
}

func (s *Server) redpackCallback(w http.ResponseWriter, r *http.Request, fail func(int, string)) {
	n := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/pay/red/callback/"), "/")
	id, err := strconv.ParseInt(n, 10, 64)
	if err != nil || id <= 0 || !strings.HasSuffix(r.URL.Path, "/") {
		fail(400, "红包链接无效")
		return
	}
	cookie, err := r.Cookie(redpackBrowserCookie)
	q := r.URL.Query()
	state := q.Get("state")
	dot := strings.LastIndexByte(state, '.')
	if err != nil || dot < 0 {
		fail(400, "领取会话已失效，请重新扫码")
		return
	}
	raw, err := base64.RawURLEncoding.DecodeString(state[:dot])
	fields := strings.SplitN(string(raw), ".", 2)
	if err != nil || len(fields) != 2 || fields[0] != n || !hmac.Equal([]byte(state[dot+1:]), []byte(s.redpackSign("state", n, fields[1], cookie.Value)[:40])) {
		fail(400, "领取会话已失效，请重新扫码")
		return
	}
	if exp, err := strconv.ParseInt(fields[1], 10, 64); err != nil || time.Now().Unix() > exp {
		fail(400, "领取会话已失效，请重新扫码")
		return
	}
	p, err := s.Store.Redpack(r.Context(), id)
	if err != nil {
		fail(404, "红包不存在")
		return
	}
	oauth := s.OAuthProviders[p.ChannelID]
	code := q.Get("code")
	if p.Type == "alipay" {
		code = q.Get("auth_code")
	}
	if oauth == nil || oauth.ID() != p.Type || code == "" {
		fail(400, "授权失败，请重新扫码")
		return
	}
	who, err := oauth.ExchangeOAuth(r.Context(), code)
	if err != nil || who.SubjectID == "" || who.Provider != p.Type {
		fail(400, "授权失败，请重新扫码")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: redpackBrowserCookie, Value: "", Path: "/pay/red/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	got, t, err := s.ProviderFlow.ClaimRedpack(r.Context(), id, who.SubjectID)
	if err == nil && got.Status == "claiming" && t.ID > 0 && t.Type == "wxpay" {
		// 商家转账（新） (original red_confirmwx.php): the claimer, already in
		// WeChat, confirms the bill with requestMerchantTransfer.
		if links, lerr := s.transferConfirmURLs(r.Context(), []int64{t.ID}); lerr == nil && links[t.ID] != "" {
			http.Redirect(w, r, links[t.ID], http.StatusFound)
			return
		}
	}
	switch {
	case errors.Is(err, payment.ErrRedpackClaimed):
		fail(409, "该红包已被领取")
	case errors.Is(err, payment.ErrRedpackExpired):
		fail(410, "红包已过期")
	case got.Status == "succeeded":
		redpackRender(w, 200, redpackPageData{Title: "领取成功", Heading: "领取成功", Money: ledger.Format(got.Amount), Message: "已存入" + redpackWallet(got.Type)})
	case got.Status == "claiming" || t.ID > 0 && got.Status != "failed":
		redpackRender(w, 200, redpackPageData{Title: "领取成功", Heading: "领取成功", Money: ledger.Format(got.Amount), Message: "将稍后存入" + redpackWallet(got.Type)})
	default:
		fail(502, "红包领取失败，请联系发放方")
	}
}
