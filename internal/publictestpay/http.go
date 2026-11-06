package publictestpay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/captcha"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const prefix = "/console/v1/public/testpay/"

func (s *Service) Matches(path string) bool {
	return strings.HasPrefix(path, prefix) || path == "/console/v1/public/captcha"
}
func reply(w http.ResponseWriter, status int, message string, data any) {
	code := 0
	if status >= 400 {
		code = -1
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"code": code, "message": message, "data": data})
}
func fail(w http.ResponseWriter, e error) {
	status, message := 503, "服务暂不可用，请稍后重新核对"
	switch {
	case errors.Is(e, ErrClosed):
		status, message = 403, "测试支付暂未开放"
	case errors.Is(e, ErrForbidden):
		status, message = 403, "测试支付会话已失效，请重新核对"
	case errors.Is(e, ErrInvalid), errors.Is(e, captcha.ErrInvalid):
		status, message = 422, "金额、支付方式或验证码无效"
	case errors.Is(e, ErrConflict), errors.Is(e, captcha.ErrConflict):
		status, message = 409, "原测试订单结果未知，请只读核对，不要重复支付"
	case errors.Is(e, captcha.ErrUnavailable):
		message = "验证码服务不可用，请稍后重试"
	}
	reply(w, status, message, nil)
}
func exactBody(w http.ResponseWriter, r *http.Request, out any) bool {
	media, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		reply(w, 415, "JSON required", nil)
		return false
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, 16385))
	if e != nil || len(raw) > 16384 {
		reply(w, 400, "invalid JSON input", nil)
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		reply(w, 400, "JSON object required", nil)
		return false
	}
	seen := map[string]bool{}
	for d.More() {
		token, e = d.Token()
		k, ok := token.(string)
		if e != nil || !ok || seen[k] {
			reply(w, 400, "duplicate or invalid JSON field", nil)
			return false
		}
		seen[k] = true
		var field json.RawMessage
		if d.Decode(&field) != nil {
			reply(w, 400, "invalid JSON field", nil)
			return false
		}
	}
	if _, e = d.Token(); e != nil || d.Decode(&struct{}{}) != io.EOF {
		reply(w, 400, "invalid JSON input", nil)
		return false
	}
	d = json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		reply(w, 400, "unsupported JSON fields", nil)
		return false
	}
	if input, ok := out.(*submitInput); ok {
		input.proofFields = map[string]bool{}
		for key := range seen {
			if key != "money" && key != "typeid" && key != "csrf_token" {
				input.proofFields[key] = true
			}
		}
	}
	return true
}
func (s *Service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.origin {
		reply(w, 403, "origin rejected", nil)
		return
	}
	action := strings.TrimPrefix(r.URL.Path, prefix)
	if r.URL.Path == "/console/v1/public/captcha" {
		action = "captcha"
	}
	method := "GET"
	if action == "submit" {
		method = "POST"
	}
	if action != "init" && action != "submit" && action != "success" && action != "captcha" {
		reply(w, 404, "not found", nil)
		return
	}
	if r.Method != method {
		w.Header().Set("Allow", method)
		reply(w, 405, "method not allowed", nil)
		return
	}
	q := r.URL.Query()
	if action == "init" || action == "submit" {
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			reply(w, 400, "query parameters not accepted", nil)
			return
		}
	} else {
		key := "trade_no"
		if action == "captcha" {
			key = "purpose"
		}
		if len(q) != 1 || len(q[key]) != 1 || q.Get(key) == "" {
			reply(w, 400, "exact public query required", nil)
			return
		}
	}
	if !s.publicOrigin {
		if action == "captcha" {
			fail(w, captcha.ErrUnavailable)
		} else {
			fail(w, ErrClosed)
		}
		return
	}
	source, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		source = r.RemoteAddr
	}
	if net.ParseIP(source) == nil {
		reply(w, 400, "invalid connection source", nil)
		return
	}
	if s.limiter != nil && !s.limiter(r.RemoteAddr, action) {
		w.Header().Set("Retry-After", "60")
		reply(w, 429, "too many public payment attempts", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	switch action {
	case "init":
		s.initialize(w, r)
	case "submit":
		var in submitInput
		if !exactBody(w, r, &in) {
			return
		}
		s.submit(w, r, in, source)
	case "success":
		s.success(w, r, q.Get("trade_no"))
	case "captcha":
		purpose := q.Get("purpose")
		if purpose != "testpay" && purpose != "login" && purpose != "payment-defend" {
			reply(w, 400, "invalid CAPTCHA purpose", nil)
			return
		}
		if purpose != "testpay" {
			reply(w, 503, "CAPTCHA consumer is not enabled for this purpose", nil)
			return
		}
		s.challenge(w, r, source)
	}
}
func (s *Service) initialize(w http.ResponseWriter, r *http.Request) {
	browser, e := s.browser(r)
	if e != nil {
		browser, e = randomToken()
	}
	if e != nil {
		fail(w, e)
		return
	}
	tx, e := s.pool.Begin(r.Context())
	if e != nil {
		fail(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	if e = s.lockBrowserAdmission203(r.Context(), tx, browser); e != nil {
		fail(w, e)
		return
	}
	c, e := s.config(r.Context(), tx)
	if e != nil {
		fail(w, e)
		return
	}
	if !c.Open {
		fail(w, ErrClosed)
		return
	}
	var enabled bool
	if e = tx.QueryRow(r.Context(), "SELECT enabled AND COALESCE((to_jsonb(merchants)->>'pay')::int,1)=1 FROM merchants WHERE id=$1 FOR SHARE", c.MerchantID).Scan(&enabled); e != nil || !enabled {
		fail(w, ErrUnavailable)
		return
	}
	types, e := s.engine.Types(r.Context(), c.MerchantID)
	if e != nil {
		fail(w, e)
		return
	}
	if types == nil {
		types = []PayType{}
	}
	outstanding, previousError := s.outstandingIntent203(r.Context(), tx, browser)
	if previousError == nil {
		reply(w, 409, "原测试订单结果尚未核实，请只读核对", map[string]string{"trade_no": outstanding.OutTradeNo})
		return
	}
	if !errors.Is(previousError, pgx.ErrNoRows) {
		fail(w, previousError)
		return
	}
	csrf, e := randomToken()
	if e != nil {
		fail(w, e)
		return
	}
	id, e := randomToken()
	if e != nil {
		fail(w, e)
		return
	}
	in := Intent{ID: id, Browser: s.sum("browser", []byte(browser)), CSRF: s.sum("csrf", []byte(csrf)), MerchantID: c.MerchantID, OutTradeNo: "TP" + strings.ReplaceAll(id, "-", "A"), ConfigHash: c.Fingerprint, State: "fresh", ExpiresAt: time.Now().UTC().Truncate(time.Microsecond).Add(10 * time.Minute)}
	_, e = tx.Exec(r.Context(), `INSERT INTO public_testpay203_intents(id,browser_hash,csrf_hash,merchant_id,out_trade_no,config_hash,state,expires_at,proof_hash) VALUES($1,$2,$3,$4,$5,$6,'fresh',$7,$8)`, in.ID, in.Browser, in.CSRF, in.MerchantID, in.OutTradeNo, in.ConfigHash, in.ExpiresAt, s.intentProof(in))
	if e != nil {
		fail(w, e)
		return
	}
	if e = alive(r.Context(), tx, in.ExpiresAt); e != nil {
		fail(w, e)
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		fail(w, e)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "goepay_testpay203", Value: browser, Path: "/console/v1/public", HttpOnly: true, Secure: strings.HasPrefix(s.origin, "https://"), SameSite: http.SameSiteStrictMode, MaxAge: 3600})
	open := 0
	if c.Captcha {
		open = 1
	}
	reply(w, 200, "", struct {
		CSRF    string    `json:"csrf_token"`
		Trade   string    `json:"trade_no"`
		Money   string    `json:"money"`
		Types   []PayType `json:"paytype"`
		Name    string    `json:"sitename"`
		Captcha int       `json:"captcha_open"`
	}{csrf, in.OutTradeNo, "1.00", types, c.SiteName, open})
}
func (s *Service) success(w http.ResponseWriter, r *http.Request, trade string) {
	browser, e := s.browser(r)
	if e != nil {
		fail(w, e)
		return
	}
	tx, e := s.pool.Begin(r.Context())
	if e != nil {
		fail(w, e)
		return
	}
	defer tx.Rollback(r.Context())
	var csrf []byte
	e = tx.QueryRow(r.Context(), `SELECT i.csrf_hash FROM public_testpay203_intents i LEFT JOIN orders o ON o.merchant_id=i.merchant_id AND o.out_trade_no=i.out_trade_no WHERE i.browser_hash=$1 AND (i.out_trade_no=$2 OR o.trade_no=$2)`, s.sum("browser", []byte(browser)), trade).Scan(&csrf)
	if e != nil {
		fail(w, ErrForbidden)
		return
	}
	var in Intent
	var proof []byte
	e = tx.QueryRow(r.Context(), `SELECT id,browser_hash,csrf_hash,merchant_id,out_trade_no,config_hash,amount,type_id,pay_type,state,order_trade_no,expires_at,proof_hash FROM public_testpay203_intents WHERE browser_hash=$1 AND csrf_hash=$2 FOR SHARE`, s.sum("browser", []byte(browser)), csrf).Scan(&in.ID, &in.Browser, &in.CSRF, &in.MerchantID, &in.OutTradeNo, &in.ConfigHash, &in.Amount, &in.TypeID, &in.PayType, &in.State, &in.OrderTradeNo, &in.ExpiresAt, &proof)
	if e != nil || !bytes.Equal(proof, s.intentProof(in)) || in.Amount <= 0 {
		fail(w, ErrConflict)
		return
	}
	order, e := s.store.Order(r.Context(), in.MerchantID, in.OutTradeNo)
	if e != nil || order.Status != "paid" || order.Amount != in.Amount || order.Name != "支付测试" || order.Type != in.PayType || order.Param != "public-testpay203:"+in.ID {
		fail(w, ErrConflict)
		return
	}
	c, e := s.config(r.Context(), tx)
	if e != nil {
		fail(w, e)
		return
	}
	reply(w, 200, "", map[string]string{"money": strconv.FormatInt(order.Amount/100, 10) + "." + fmtTwo(order.Amount%100), "sitename": c.SiteName})
}
func fmtTwo(v int64) string {
	if v < 10 {
		return "0" + strconv.FormatInt(v, 10)
	}
	return strconv.FormatInt(v, 10)
}
