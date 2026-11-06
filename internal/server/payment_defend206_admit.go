package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"regexp"
	"sort"

	"github.com/jackc/pgx/v5"
	"goepay/internal/captcha"
	"goepay/internal/compat"
	"goepay/internal/paymentdefend"
)

// Platform verification fields are added by the platform's own verification
// page AFTER the merchant signed. They are removed before signature
// verification and only the browser submit may carry them.
type defend206Platform struct {
	present  bool
	token    string
	hasProof bool
	proof    captcha.Proof
}

var defend206PlatformFields = map[string]func(*defend206Platform, string){
	"__defend_continue":       func(d *defend206Platform, v string) { d.token = v },
	"__defend_captcha_id":     func(d *defend206Platform, v string) { d.hasProof = true; d.proof.CaptchaID = v },
	"__defend_lot_number":     func(d *defend206Platform, v string) { d.hasProof = true; d.proof.LotNumber = v },
	"__defend_pass_token":     func(d *defend206Platform, v string) { d.hasProof = true; d.proof.PassToken = v },
	"__defend_gen_time":       func(d *defend206Platform, v string) { d.hasProof = true; d.proof.GenTime = v },
	"__defend_captcha_output": func(d *defend206Platform, v string) { d.hasProof = true; d.proof.CaptchaOutput = v },
}

func takeDefend206Platform(p compat.Params) defend206Platform {
	var d defend206Platform
	for name, set := range defend206PlatformFields {
		if v, ok := p[name]; ok {
			d.present = true
			set(&d, v)
			delete(p, name)
		}
	}
	return d
}

var captchaID206 = regexp.MustCompile(`^[0-9a-f]{32}$`)

func (s *Server) paymentDefendCaptchaStore206() (*captcha.Store, error) {
	if s.PaymentDefendCaptcha206 != nil {
		return s.PaymentDefendCaptcha206, nil
	}
	return captcha.NewStore(s.Pool, s.CheckoutKey)
}

// captchaConfig206 reads the site Geetest credentials. Create-order human
// verification uses Geetest v4 only (as the original verify_invisible/slide
// pages do); anything else is unavailable, never a demo id or a pass-through.
func (s *Server) captchaConfig206(ctx context.Context, tx pgx.Tx) (captcha.Config, error) {
	values := map[string]string{"captcha_version": "", "captcha_id": ""}
	rows, err := tx.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1)`, []string{"captcha_version", "captcha_id"})
	if err != nil {
		return captcha.Config{}, err
	}
	seen := map[string]bool{}
	for rows.Next() {
		var k string
		var raw []byte
		var v string
		if err = rows.Scan(&k, &raw); err != nil || seen[k] || json.Unmarshal(raw, &v) != nil {
			rows.Close()
			return captcha.Config{}, captcha.ErrUnavailable
		}
		seen[k] = true
		values[k] = v
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return captcha.Config{}, err
	}
	if values["captcha_version"] != "1" || !captchaID206.MatchString(values["captcha_id"]) || s.PaymentDefendVerifier206 == nil {
		return captcha.Config{}, captcha.ErrUnavailable
	}
	store, err := s.paymentDefendCaptchaStore206()
	if err != nil {
		return captcha.Config{}, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared(hashtextextended('captcha-credential203',203))`); err != nil {
		return captcha.Config{}, err
	}
	_, key, err := store.ReadTx(ctx, tx)
	if err != nil || len(key) < 16 {
		return captcha.Config{}, captcha.ErrUnavailable
	}
	return captcha.Config{Version: 1, ID: values["captcha_id"], Key: key}, nil
}

type defend206Decision struct {
	policy  paymentdefend.Policy
	kind    string // pay_verify_type when triggered
	captcha captcha.Config
}

// paymentDefendAdmit206 handles the browser-submit verification step before
// any route, order, attempt or send. It returns true when it wrote the
// response. Without platform fields in a triggered verification mode it mints
// a one-time continue token and renders that mode's page; with them it
// verifies (remote Geetest first, outside any transaction), then consumes the
// token exactly once and lets the guard admit this request.
func (s *Server) paymentDefendAdmit206(w http.ResponseWriter, r *http.Request, legacy bool, route string, merchantID int64, p compat.Params, plat defend206Platform, adm *defend206Admission, peerIP string) bool {
	reject := func() bool {
		s.wire(w, legacy, nil, "payment verification failed")
		return true
	}
	browser := route == "/api/pay/submit"
	if s.PaymentDefend206 == nil || s.Pool == nil || !browser {
		if plat.present {
			return reject()
		}
		return false
	}
	ctx := r.Context()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		if plat.present {
			return reject()
		}
		return false // the in-transaction guard still fails closed
	}
	defer tx.Rollback(ctx)
	d, verifyNeeded, err := s.defend206Decide(ctx, tx, adm)
	if err != nil || !verifyNeeded {
		if plat.present {
			return reject()
		}
		return false // the in-transaction guard admits or fails closed
	}
	c := paymentdefend.Continue{
		MerchantID:        merchantID,
		OutTradeNo:        p["out_trade_no"],
		Request:           defend206RequestIdentity(r.URL.Path, p),
		Browser:           defend206Browser(r, peerIP),
		PolicyFingerprint: d.policy.Fingerprint,
		Mode:              d.kind,
	}
	if !plat.present {
		minted, err := s.PaymentDefend206.ContinueMintTx(ctx, tx, c, defend206ContinueTTLSeconds)
		if err != nil || tx.Commit(ctx) != nil {
			s.wire(w, legacy, nil, "payment verification unavailable")
			return true
		}
		s.renderDefend206(w, r.URL.Path, p, minted, d)
		return true
	}
	if plat.token == "" || (d.kind == "0") == plat.hasProof {
		return reject()
	}
	_ = tx.Rollback(ctx)
	if d.kind != "0" {
		// Authoritative remote verification, never inside a transaction. An
		// unknown result (timeout, network, malformed) is a rejection.
		issued := captcha.Challenge{Version: 1, GT: d.captcha.ID, Success: 1}
		if s.PaymentDefendVerifier206.Verify(ctx, d.captcha, issued, plat.proof, peerIP) != nil {
			return reject()
		}
	}
	tx2, err := s.Pool.Begin(ctx)
	if err != nil {
		return reject()
	}
	defer tx2.Rollback(ctx)
	adm.decided = false
	again, verifyNeeded, err := s.defend206Decide(ctx, tx2, adm)
	if err != nil || !verifyNeeded || again.policy.Fingerprint != d.policy.Fingerprint || again.kind != d.kind || again.captcha != d.captcha {
		return reject()
	}
	if s.PaymentDefend206.ContinueConsumeTx(ctx, tx2, plat.token, c) != nil {
		return reject()
	}
	if d.kind != "0" {
		store, err := s.paymentDefendCaptchaStore206()
		challenge := sha256.Sum256([]byte("payment-defend206\n" + plat.token))
		if err != nil || store.Claim(ctx, tx2, d.captcha, plat.proof, "payment-defend", challenge[:]) != nil {
			return reject()
		}
	}
	if tx2.Commit(ctx) != nil {
		return reject()
	}
	adm.consumed, adm.fingerprint, adm.kind = true, d.policy.Fingerprint, d.kind
	return false
}

// defend206Decide reports whether this browser submit must verify and how.
// The forbid form (type 3) and an undecidable trigger are left to the guard,
// which rejects them.
func (s *Server) defend206Decide(ctx context.Context, tx pgx.Tx, adm *defend206Admission) (defend206Decision, bool, error) {
	var d defend206Decision
	policy, err := s.PaymentDefend206.CurrentTx(ctx, tx)
	if err != nil {
		return d, false, err
	}
	triggered, known, err := s.defend206Triggered(ctx, tx, policy, adm)
	if err != nil {
		return d, false, err
	}
	if !known || !triggered {
		return d, false, nil
	}
	d.policy, d.kind = policy, policy.Settings["pay_verify_type"]
	switch d.kind {
	case "0":
		return d, true, nil
	case "1", "2":
		if d.captcha, err = s.captchaConfig206(ctx, tx); err != nil {
			return d, false, err
		}
		return d, true, nil
	}
	return d, false, nil
}

var defend206GeetestPage = template.Must(template.New("geetest206").Parse(`<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1.0,maximum-scale=1.0,user-scalable=no">
<meta name="robots" content="noindex,nofollow"><title>{{if eq .Kind "2"}}支付安全验证{{else}}正在进行支付安全验证，请稍候...{{end}}</title>
<style>body{margin:0;padding:0;font-family:"Helvetica Neue",Helvetica,"Microsoft YaHei",Arial,sans-serif;background:#f5f5f5;color:#333}
.box{max-width:560px;margin:12vh auto 0;padding:24px;background:#fff;border:1px solid #e5e5e5;border-radius:6px;text-align:center}
.box p{margin:0 0 12px;font-size:18px;line-height:1.7;color:#5d5d5d}.err{color:#c0392b;font-size:15px}</style>
</head><body><div class="box">{{if eq .Kind "2"}}<p>很抱歉，当前支付人数过多，请完成<strong>“滑动验证”</strong>后继续支付</p>{{else}}<p>正在进行支付安全验证，请稍候...</p>{{end}}
<p id="defend206-error" class="err" role="alert" hidden></p></div>
<form id="defend206" method="post" action="{{.Action}}">{{range .Fields}}<input type="hidden" name="{{.K}}" value="{{.V}}">{{end}}<input type="hidden" name="__defend_continue" value="{{.Token}}">
<input type="hidden" name="__defend_captcha_id"><input type="hidden" name="__defend_lot_number"><input type="hidden" name="__defend_pass_token"><input type="hidden" name="__defend_gen_time"><input type="hidden" name="__defend_captcha_output"></form>
<script nonce="{{.Nonce}}" src="https://static.geetest.com/v4/gt4.js"></script>
<script nonce="{{.Nonce}}">(function(){
var form=document.getElementById("defend206"),fail=function(m){var e=document.getElementById("defend206-error");e.textContent=m;e.hidden=false;};
if(typeof initGeetest4!=="function"){fail("验证码加载失败，请刷新页面重试");return;}
initGeetest4({captchaId:{{.CaptchaID}},product:"bind",protocol:"https://",riskType:{{.RiskType}}{{if eq .Kind "2"}},hideSuccess:true{{end}}},function(captcha){
captcha.onReady(function(){captcha.showCaptcha();}).onSuccess(function(){
var r=captcha.getValidate();if(!r){fail("验证未完成，请重试");return;}
form.elements["__defend_captcha_id"].value={{.CaptchaID}};
form.elements["__defend_lot_number"].value=r.lot_number||"";
form.elements["__defend_pass_token"].value=r.pass_token||"";
form.elements["__defend_gen_time"].value=r.gen_time||"";
form.elements["__defend_captcha_output"].value=r.captcha_output||"";
form.submit();}).onError(function(){fail("验证码加载失败，请刷新页面重试");});});})();</script>
</body></html>`))

func (s *Server) renderDefend206(w http.ResponseWriter, action string, p compat.Params, token string, d defend206Decision) {
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		http.Error(w, "verification unavailable", http.StatusInternalServerError)
		return
	}
	nonce := base64.RawStdEncoding.EncodeToString(nonceBytes)
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	fields := make([]defend206Field, 0, len(keys))
	for _, k := range keys {
		fields = append(fields, defend206Field{k, p[k]})
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Content-Type-Options", "nosniff")
	data := map[string]any{"Action": action, "Fields": fields, "Token": token, "Nonce": nonce, "Kind": d.kind}
	page := defend206JumpPage
	if d.kind == "0" {
		h.Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; style-src 'unsafe-inline'; form-action 'self'; script-src 'nonce-"+nonce+"'")
	} else {
		// gt4 loads its own scripts/JSONP, images and styles from Geetest.
		h.Set("Content-Security-Policy", "default-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'nonce-"+nonce+"' 'strict-dynamic' https:; style-src 'unsafe-inline' https://static.geetest.com https://static.geevisit.com; img-src data: https://static.geetest.com https://static.geevisit.com; connect-src https://gcaptcha4.geetest.com https://gcaptcha4.geevisit.com; font-src https://static.geetest.com")
		page = defend206GeetestPage
		data["CaptchaID"] = d.captcha.ID
		data["RiskType"] = map[string]string{"1": "ai", "2": "slide"}[d.kind]
	}
	w.WriteHeader(http.StatusOK)
	_ = page.Execute(w, data)
}
