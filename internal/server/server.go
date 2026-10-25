package server

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	provider "goepay/internal/adapters/payment"
	"goepay/internal/backoffice"
	"goepay/internal/businessnotice"
	"goepay/internal/channelconfig"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/merchantnotice"
	"goepay/internal/migration"
	"goepay/internal/payment"
	"goepay/internal/platform/httpclient"
	"goepay/internal/printing"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Server struct {
	FundsActionResolver                     func(context.Context, identity.FundsQuerier, string, json.RawMessage) (identity.FundsAction, map[string]string, error)
	MFA                                     *identity.MFA
	ProviderFlow                            *payment.ProviderFlow
	OAuthProviders                          map[int64]provider.WebOAuthProvider
	MiniOAuthProviders                      map[int64]provider.MiniOAuthProvider
	IdentityProfiles                        map[int64]channelconfig.IdentityProfile
	IdentityProfileEnabled                  bool
	IdentityProfileCallbackBaseURL          string
	ApplicationAuthorizationProfiles        map[int64]channelconfig.ApplicationAuthorizationProfile
	ApplicationAuthorizationEnabled         bool
	ApplicationAuthorizationCallbackBaseURL string
	AuthorizationObservationEnabled         bool
	CheckoutKey                             []byte
	MerchantChannels                        map[int64][]int64
	Simulation                              http.Handler
	Enrollment                              http.Handler
	Pool                                    *pgxpool.Pool
	Store                                   *payment.Store
	Printing                                *printing.Store
	MerchantNotice                          *merchantnotice.Store
	BusinessNotice                          *businessnotice.Store
	LegacyRefundHistory                     *migration.RefundHistoryReader
	Sessions                                *identity.Sessions
	PlatformKey                             *rsa.PrivateKey
	Sandbox                                 bool
	mu                                      sync.Mutex
	attempts                                map[string]attempt
	lastAttemptCleanup                      time.Time
	collectionAttempts                      map[string]attempt
	lastCollectionCleanup                   time.Time
}
type attempt struct {
	count int
	since time.Time
}

func response(w http.ResponseWriter, status int, code int, msg string, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"code": code, "message": msg, "data": data})
}
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		bodyLimit := int64(1 << 20)
		if r.Method == "POST" && r.URL.Path == "/console/v1/admin/collection-style-images" {
			bodyLimit += 8192
		}
		r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
		if s.collectionStyleAsset(w, r) {
			return
		}
		if s.templatePublic(w, r) {
			return
		}
		if s.referralLegacyAlias(w, r) {
			return
		}
		if s.applicationAuthorizationPublic(w, r) {
			return
		}
		if s.IdentityProfilePublic(w, r) {
			return
		}
		if s.IdentityLookupPublic(w, r) {
			return
		}
		if strings.HasPrefix(r.URL.Path, "/console/v1/public/") {
			if s.Enrollment == nil {
				response(w, 503, -1, "verification service unavailable", nil)
				return
			}
			s.Enrollment.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/collect/") {
			s.collect(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/oauth/callback/") {
			s.oauthCallback(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/oauth/mini/complete/") {
			s.handleMiniOAuthComplete(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/pay/preauthnotify/") {
			s.preauthorizationCallback(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/pay/preauth/") {
			s.preauthorizationCheckout(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/simulation/") && s.Simulation != nil {
			s.Simulation.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/callbacks/providers/") {
			s.providerCallback(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/checkout/") {
			s.checkout(w, r)
			return
		}
		if r.URL.Path == "/healthz" {
			response(w, 200, 0, "alive", nil)
			return
		}
		if r.URL.Path == "/readyz" {
			if s.Pool == nil || s.schemaReady(r) != nil {
				response(w, 503, -1, "database unavailable", nil)
			} else {
				response(w, 200, 0, "ready", nil)
			}
			return
		}
		if strings.HasPrefix(r.URL.Path, "/console/v1/") {
			s.console(w, r)
			return
		}
		s.paymentAPI(w, r)
	})
}
func (s *Server) console(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/console/v1/"), "/")
	if len(parts) < 2 || (parts[0] != "admin" && parts[0] != "merchant") {
		response(w, 404, -1, "not found", nil)
		return
	}
	role, action := parts[0], parts[1]
	cookieName := "goepay_" + role
	if action == "login" && r.Method == "POST" {
		s.login(w, r, role, cookieName)
		return
	}
	c, e := r.Cookie(cookieName)
	if e != nil {
		response(w, 401, -1, "authentication required", nil)
		return
	}
	session, ok := s.Sessions.Get(c.Value, role)
	if !ok {
		response(w, 401, -1, "session expired", nil)
		return
	}
	if s.Pool == nil {
		response(w, 503, -1, "database unavailable", nil)
		return
	}
	var enabled bool
	e = s.Pool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM console_users WHERE username=$1 AND role=$2 AND subject_id=$3 AND enabled AND ($4::bigint=0 OR auth_version=$4))", session.User.Username, role, session.User.ID, session.User.AuthVersion).Scan(&enabled)
	if e != nil {
		response(w, 503, -1, "account verification unavailable", nil)
		return
	}
	if enabled && role == "merchant" {
		e = s.Pool.QueryRow(r.Context(), "SELECT enabled FROM merchants WHERE id=$1", session.User.ID).Scan(&enabled)
		if e != nil {
			enabled = false
		}
	}
	if !enabled {
		s.Sessions.Delete(c.Value)
		response(w, 401, -1, "account disabled", nil)
		return
	}
	if role == "admin" {
		if err := s.Pool.QueryRow(r.Context(), "SELECT admin_class FROM console_users WHERE username=$1 AND role='admin' AND subject_id=$2 AND enabled", session.User.Username, session.User.ID).Scan(&session.User.AdminClass); err != nil {
			response(w, 401, -1, "account unavailable", nil)
			return
		}
		if !adminOperationAllowed(session.User.AdminClass, parts) {
			response(w, 403, -1, "super administrator required", nil)
			return
		}
	}
	if r.Method != "GET" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(session.CSRF)) != 1 {
		response(w, 403, -1, "invalid CSRF token", nil)
		return
	}
	if !s.auditDelegatedRequest(w, r, session) {
		return
	}
	if s.ssoConsole(w, r, role, session, parts, c.Value) {
		return
	}
	if s.fundsSecurity(w, r, role, session, parts, c.Value) {
		return
	}
	var fundsOK bool
	r, fundsOK = s.protectAdminFunds(w, r, role, session, c.Value)
	if !fundsOK {
		return
	}
	if s.applicationAuthorizationConsole(w, r, role, session, parts, c.Value) {
		return
	}
	if s.IdentityProfileConsole(w, r, role, session, parts, c.Value) {
		return
	}
	if s.IdentityLookupConsole(w, r, role, session, parts, c.Value) {
		return
	}
	if s.channelSharingConsole(w, r, role, session, parts) {
		return
	}
	if s.orderIncomeConsole(w, r, role, session, parts) {
		return
	}
	if s.refundFundingConsole(w, r, role, session, parts) {
		return
	}
	if s.legacyRefundHistoryConsole(w, r, role, session, parts) {
		return
	}
	if s.preauthorizationObservationConsole(w, r, role, session, parts, c.Value) {
		return
	}
	if s.preauthorizationCaptureConsole(w, r, role, session, parts) {
		return
	}
	if s.notificationResendConsole(w, r, role, session, parts) {
		return
	}
	if s.preauthorizationRemainingConsole(w, r, role, session, parts, c.Value) {
		return
	}
	if s.authorizationReleaseConsole(w, r, role, session, parts) {
		return
	}
	if s.orderBlacklistConsole(w, r, role, session, parts) {
		return
	}
	if s.orderBatchControlsConsole(w, r, role, session, parts) {
		return
	}
	if s.orderAdminActions(w, r, role, session, parts) {
		return
	}
	if s.membershipSalesConsole(w, r, role, session, parts) {
		return
	}
	if s.invitationCodesConsole(w, r, role, session, parts) {
		return
	}
	if s.collectionStylesConsole(w, r, role, session, parts) {
		return
	}
	if s.collectionGeneratorConsole(w, r, role, session, parts) {
		return
	}
	if s.collectionStyleImagesConsole(w, r, role, session, parts) {
		return
	}
	if s.orderOperations(w, r, role, session, parts) {
		return
	}
	if s.transferConsole(w, r, role, session, parts) {
		return
	}
	if s.transferBatchConsole(w, r, role, session, parts) {
		return
	}
	if s.transferBatchImport(w, r, role, session, parts) {
		return
	}
	if s.referralConsole(w, r, role, session, parts) {
		return
	}
	if s.membershipReferralConsole(w, r, role, session, parts) {
		return
	}
	if s.channelStopConsole(w, r, role, session, parts, c.Value) {
		return
	}
	if s.settlementAutoConsole(w, r, role, session, parts) {
		return
	}
	if s.settlementPlanConsole(w, r, role, session, parts) {
		return
	}
	if s.printingConsole(w, r, role, session, parts) {
		return
	}
	if s.merchantNoticeConsole(w, r, role, session, parts) {
		return
	}
	if s.businessNoticeConsole(w, r, role, session, parts) {
		return
	}
	if s.templateConsole(w, r, role, session, parts) {
		return
	}
	if s.settlementPayoutConsole(w, r, role, session, parts) {
		return
	}
	if s.settlementBatchConsole(w, r, role, session, parts) {
		return
	}
	if s.collectionLink(w, r, role, session, parts) {
		return
	}
	if s.security(w, r, role, session, parts) {
		return
	}
	if s.operations(w, r, role, session, parts) {
		return
	}
	if s.account(w, r, role, session, parts) {
		return
	}
	if s.adminMerchantLoginLog(w, r, role, session, parts) {
		return
	}
	if s.refundSummary(w, r, role, session, parts) {
		return
	}
	if s.reports(w, r, role, session, parts) {
		return
	}
	if s.backoffice(w, r, role, session, parts) {
		return
	}
	if action == "session" && r.Method == "GET" {
		response(w, 200, 0, "", map[string]any{"user": session.User, "csrf_token": session.CSRF, "delegated_by": session.Issuer})
		return
	}
	if action == "logout" && r.Method == "POST" {
		s.Sessions.Delete(c.Value)
		http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/console/v1/" + role, MaxAge: -1, HttpOnly: true, Secure: s.Sessions.Secure, SameSite: http.SameSiteStrictMode})
		response(w, 200, 0, "", nil)
		return
	}
	mid := session.User.ID
	if role == "admin" {
		mid = 0
	}
	if s.Store == nil {
		response(w, 503, -1, "database unavailable", nil)
		return
	}
	if r.Method == "GET" {
		switch action {
		case "dashboard":
			s.dashboard(w, r, mid)
			return
		case "orders", "refunds", "transfers", "merchants", "ledger":
			s.list(w, r, role, mid, action, session)
			return
		}
	}
	if r.Method == "POST" && action == "orders" && len(parts) == 4 {
		order, e := s.Store.OrderByTradeNo(r.Context(), parts[2])
		if e != nil || (role != "admin" && order.MerchantID != mid) {
			response(w, 404, -1, "order not found", nil)
			return
		}
		switch parts[3] {
		case "sandbox-paid":
			if !s.Sandbox || role != "admin" {
				response(w, 403, -1, "sandbox administrator required", nil)
				return
			}
			e = s.Store.MarkPaid(r.Context(), order.TradeNo, "sandbox:"+order.TradeNo)
			if e != nil {
				s.fail(w, e)
				return
			}
			response(w, 200, 0, "sandbox payment recorded", nil)
			return
		case "refund":
			var in struct {
				Amount     string `json:"amount"`
				ExternalNo string `json:"external_no"`
			}
			if !decode(w, r, &in) {
				return
			}
			amount, e := ledger.Parse(in.Amount)
			if e != nil {
				s.fail(w, e)
				return
			}
			if !s.Sandbox {
				response(w, 503, -1, "no verified refund provider configured", nil)
				return
			}
			v, e := s.Store.ReserveRefund(r.Context(), order.MerchantID, order.OutTradeNo, in.ExternalNo, amount)
			if e != nil {
				s.fail(w, e)
				return
			}
			response(w, 202, 0, "refund reserved; not sent to upstream", v)
			return
		}
	}
	if r.Method == "POST" && action == "transfers" {
		response(w, 503, -1, "no verified transfer provider configured", nil)
		return
	}
	response(w, 501, -1, "capability not implemented", nil)
}
func decode(w http.ResponseWriter, r *http.Request, out any) bool {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(out); e != nil {
		response(w, 400, -1, "invalid JSON", nil)
		return false
	}
	var extra any
	if e := d.Decode(&extra); e != io.EOF {
		response(w, 400, -1, "invalid trailing JSON", nil)
		return false
	}
	return true
}
func (s *Server) login(w http.ResponseWriter, r *http.Request, role, cookieName string) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	success := false
	var verifiedLogin identity.User
	defer func() { s.recordLogin(r, role, in.Username, &success, verifiedLogin) }()
	allowed, throttleError := s.allowLoginRequest(r, role, in.Username)
	if throttleError != nil {
		response(w, 503, -1, "login verification unavailable", nil)
		return
	}
	if !allowed {
		w.Header().Set("Retry-After", "900")
		response(w, 429, -1, "too many attempts", nil)
		return
	}
	if !claimLoginVerification() {
		w.Header().Set("Retry-After", "1")
		response(w, 429, -1, "login verification busy", nil)
		return
	}
	defer releaseLoginVerification()
	if s.Pool == nil {
		response(w, 503, -1, "database unavailable", nil)
		return
	}
	var hash, name, adminClass string
	var id, authVersion int64
	e := s.Pool.QueryRow(r.Context(), "SELECT subject_id,display_name,password_hash,auth_version,admin_class FROM console_users WHERE username=$1 AND role=$2 AND enabled", in.Username, role).Scan(&id, &name, &hash, &authVersion, &adminClass)
	if e != nil || !identity.CheckPassword(hash, in.Password) {
		response(w, 401, -1, "invalid credentials", nil)
		return
	}
	if role == "merchant" {
		var active bool
		if err := s.Pool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM merchants WHERE id=$1 AND enabled)", id).Scan(&active); err != nil || !active {
			response(w, 401, -1, "invalid credentials", nil)
			return
		}
	}
	u := identity.User{Username: in.Username, ID: id, Name: name, Role: role, AuthVersion: authVersion, AdminClass: adminClass}
	if !s.checkMFA(r.Context(), in.Username, role, in.Code) {
		response(w, 401, -1, "invalid credentials or authenticator code", nil)
		return
	}
	token, csrf, e := s.Sessions.Create(u)
	if e != nil {
		s.fail(w, e)
		return
	}
	verifiedLogin = u
	success = true
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/console/v1/" + role, HttpOnly: true, Secure: s.Sessions.Secure, SameSite: http.SameSiteStrictMode, MaxAge: 43200})
	response(w, 200, 0, "", map[string]any{"user": u, "csrf_token": csrf})
}
func (s *Server) fail(w http.ResponseWriter, e error) {
	if errors.Is(e, identity.ErrFundsAuthorization) || errors.Is(e, identity.ErrFundsNotConfigured) || errors.Is(e, identity.ErrFundsAuthentication) || errors.Is(e, identity.ErrFundsRateLimited) || errors.Is(e, identity.ErrFundsBusy) || errors.Is(e, identity.ErrFundsInvalid) {
		fundsHTTPError(w, e)
		return
	}
	status := 500
	msg := "internal error"
	switch {
	case errors.Is(e, payment.ErrNotFound):
		status = 404
		msg = "not found"
	case errors.Is(e, payment.ErrScanCancellationPending):
		status = 409
		msg = "original scan cancellation requires reconciliation"
	case errors.Is(e, payment.ErrScanCancelled):
		status = 409
		msg = "original scan payment was cancelled"
	case errors.Is(e, payment.ErrConflict):
		status = 409
		msg = "idempotency conflict"
	case errors.Is(e, payment.ErrInsufficient):
		status = 409
		msg = "insufficient balance"
	case errors.Is(e, payment.ErrInvalid), errors.Is(e, payment.ErrState):
		status = 422
		msg = "invalid input or state"
	}
	response(w, status, -1, msg, nil)
}
func (s *Server) paymentAPI(w http.ResponseWriter, r *http.Request) {
	route := r.URL.Path
	legacy := route == "/submit.php" || route == "/mapi.php" || route == "/api.php"
	if route == "/submit.php" {
		route = "/api/pay/submit"
	}
	if route == "/mapi.php" {
		route = "/api/pay/create"
	}
	if r.Method != "GET" && r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if r.Method == "GET" && !legacy && strings.HasPrefix(route, "/api/") && route != "/api/pay/submit" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if e := r.ParseForm(); e != nil {
		http.Error(w, "invalid form", 400)
		return
	}
	p, e := compat.Parse(r.Form)
	if e != nil {
		s.wire(w, legacy, nil, "invalid parameters")
		return
	}
	id, e := strconv.ParseInt(p["pid"], 10, 64)
	if e != nil || id <= 0 || s.Store == nil {
		s.wire(w, legacy, nil, "merchant unavailable")
		return
	}
	m, e := s.Store.Merchant(r.Context(), id)
	if e != nil || !m.Enabled {
		s.wire(w, legacy, nil, "merchant unavailable")
		return
	}
	if r.URL.Path == "/api.php" {
		if m.SignatureAlgorithm != "MD5" || subtle.ConstantTimeCompare([]byte(p["key"]), []byte(m.APIKey)) != 1 {
			s.wire(w, true, nil, "authentication failed")
			return
		}
		switch p["act"] {
		case "order":
			route = "/api/pay/query"
		case "query":
			route = "/api/merchant/info"
		case "orders":
			route = "/api/merchant/orders"
		case "refund":
			route = "/api/pay/refund"
		case "close":
			route = "/api/pay/close"
		default:
			s.wire(w, true, nil, "capability unavailable")
			return
		}
	} else {
		cred := compat.Credentials{MD5Key: m.APIKey, Algorithm: m.SignatureAlgorithm}
		if cred.Algorithm == "" {
			cred.Algorithm = "MD5"
		}
		if !legacy && cred.Algorithm != "RSA" {
			s.wire(w, legacy, nil, "RSA merchant credentials required")
			return
		}
		if cred.Algorithm == "RSA" {
			cred.PublicKey, e = compat.ParsePublicKey(m.RSAPublicKey)
			if e != nil {
				s.wire(w, legacy, nil, "RSA credentials unavailable")
				return
			}
		}
		if e = compat.Verify(p, cred, time.Now()); e != nil {
			s.wire(w, legacy, nil, "signature verification failed")
			return
		}
	}
	if !legacy && s.PlatformKey == nil {
		s.wire(w, false, nil, "platform signing key unavailable")
		return
	}
	switch route {
	case "/api/pay/create", "/api/pay/submit":
		if p["method"] == "scan" {
			if r.Method != "POST" {
				s.wire(w, legacy, nil, "scan requires POST")
				return
			}
			p, e = normalizeScanParams(p)
			if e != nil {
				s.wire(w, legacy, nil, "invalid scan code or payment type")
				return
			}
		}
		clientIP, _, splitError := net.SplitHostPort(r.RemoteAddr)
		if splitError != nil {
			clientIP = r.RemoteAddr
		}
		if e := backoffice.New(s.Pool, s.Store).CheckPayment(r.Context(), backoffice.PaymentContext{MerchantID: id, ClientIP: clientIP, Buyer: p["sub_openid"], NotifyURL: p["notify_url"], ReturnURL: p["return_url"]}); e != nil {
			s.wire(w, legacy, nil, "payment rejected by configured policy")
			return
		}
		// A merchant backend's TCP address and its signed buyer address are
		// distinct identities. A claimed buyer must never override peer policy.
		buyerIP := clientIP
		if p["clientip"] != "" {
			parsed := net.ParseIP(p["clientip"])
			if parsed == nil {
				s.wire(w, legacy, nil, "invalid client IP")
				return
			}
			buyerIP = parsed.String()
			if e := backoffice.New(s.Pool, s.Store).CheckPayment(r.Context(), backoffice.PaymentContext{MerchantID: id, ClientIP: buyerIP, Buyer: p["sub_openid"], NotifyURL: p["notify_url"], ReturnURL: p["return_url"]}); e != nil {
				s.wire(w, legacy, nil, "payment rejected by configured policy")
				return
			}
		}
		if p["type"] != "sandbox" {
			s.providerCreate(w, r, legacy, id, p)
			return
		}
		if !s.Sandbox {
			s.wire(w, legacy, nil, "no verified payment provider configured")
			return
		}
		amount, e := ledger.Parse(p["money"])
		if e != nil {
			s.wire(w, legacy, nil, "invalid money")
			return
		}
		if p["type"] != "sandbox" {
			s.wire(w, legacy, nil, "only sandbox payment is configured")
			return
		}
		if !validURL(p["notify_url"]) || !validURL(p["return_url"]) {
			s.wire(w, legacy, nil, "invalid callback URL")
			return
		}
		o, e := s.Store.CreateOrder(r.Context(), payment.CreateOrderInput{Version: wireVersion(legacy), MerchantID: id, OutTradeNo: p["out_trade_no"], Name: p["name"], Type: p["type"], Amount: amount, NotifyURL: p["notify_url"], ReturnURL: p["return_url"], Param: p["param"]})
		if e != nil {
			s.wire(w, legacy, nil, "order rejected")
			return
		}
		if net.ParseIP(buyerIP) != nil {
			if e = s.Store.SetOrderClientIP(r.Context(), o.ID, buyerIP); e != nil {
				s.wire(w, legacy, nil, "order metadata unavailable")
				return
			}
		}
		materials := map[string]any{"trade_no": o.TradeNo, "sandbox": true}
		if legacy {
			materials["payurl"] = "/merchant/trade/order"
		} else {
			materials["pay_type"] = "jump"
			materials["pay_info"] = "/merchant/trade/order"
		}
		s.wire(w, legacy, materials, "")
	case "/api/pay/refund", "/api/pay/refundquery", "/api/pay/close":
		s.providerOperation(w, r, legacy, id, p, route)
		return
	case "/api/pay/query":
		o, e := s.lookup(r, id, p)
		if e != nil {
			s.wire(w, legacy, nil, "order not found")
			return
		}
		if s.ProviderFlow != nil && (o.Type == "wxpay" || o.Type == "alipay") && o.Status != "paid" {
			if _, e := s.ProviderFlow.Query(r.Context(), id, o.TradeNo); e != nil {
				s.wire(w, legacy, nil, "provider query unavailable")
				return
			}
			o, e = s.lookup(r, id, p)
			if e != nil {
				s.wire(w, legacy, nil, "order not found")
				return
			}
		}
		s.paymentQuery(w, r, legacy, o)
	case "/api/merchant/info":
		s.merchantInfo(w, r, legacy, id)
	case "/api/merchant/orders":
		s.merchantOrders(w, r, legacy, id, p)
	case "/api/transfer/query", "/api/transfer/balance", "/api/transfer/submit", "/api/transfer/proof":
		s.transferAPI(w, r, id, p, route)
	default:
		s.wire(w, legacy, nil, "capability not implemented")
	}
}
func (s *Server) lookup(r *http.Request, id int64, p compat.Params) (payment.Order, error) {
	if p["trade_no"] != "" {
		o, e := s.Store.OrderByTradeNo(r.Context(), p["trade_no"])
		if e == nil && o.MerchantID != id {
			return payment.Order{}, payment.ErrNotFound
		}
		return o, e
	}
	return s.Store.Order(r.Context(), id, p["out_trade_no"])
}
func validURL(raw string) bool { return raw == "" || httpclient.ValidateURL(raw) == nil }
func wireVersion(legacy bool) string {
	if legacy {
		return "v1"
	}
	return "v2"
}
func (s *Server) wire(w http.ResponseWriter, legacy bool, data map[string]any, msg string) {
	code := 0
	if legacy {
		code = 1
	}
	s.wireWithCode(w, legacy, data, msg, code)
}
func (s *Server) wireWithCode(w http.ResponseWriter, legacy bool, data map[string]any, msg string, successCode int) {
	if data == nil {
		data = map[string]any{}
	}
	if msg != "" {
		data["code"] = -1
		data["msg"] = msg
	} else {
		data["code"] = successCode
	}
	if !legacy && s.PlatformKey != nil {
		p := compat.Params{}
		data["timestamp"] = fmt.Sprint(time.Now().Unix())
		data["sign_type"] = "RSA"
		for k, v := range data {
			switch scalar := v.(type) {
			case []map[string]any, []any, map[string]any:
				continue
			case bool:
				if scalar {
					p[k] = "1"
				} else {
					p[k] = ""
				}
			case nil:
				p[k] = ""
			default:
				p[k] = fmt.Sprint(v)
			}
		}
		sig, e := compat.SignRSAWithProfile(p, s.PlatformKey, compat.ProfileSDKV2)
		if e == nil {
			data["sign"] = sig
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(data)
}

func (s *Server) schemaReady(r *http.Request) error {
	if err := db.VerifySchema(r.Context(), s.Pool); err != nil {
		return err
	}
	var orders, users bool
	return s.Pool.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM orders LIMIT 1),EXISTS(SELECT 1 FROM console_users LIMIT 1)").Scan(&orders, &users)
}

func (s *Server) allowLogin(remote, role, username string, now time.Time) bool {
	ip, _, err := net.SplitHostPort(remote)
	if err != nil {
		ip = remote
	}
	key := fmt.Sprintf("%x", sha256.Sum256([]byte(ip+"\x00"+role+"\x00"+strings.ToLower(username))))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.attempts == nil {
		s.attempts = map[string]attempt{}
	}
	if s.lastAttemptCleanup.IsZero() || now.Sub(s.lastAttemptCleanup) >= time.Minute || len(s.attempts) >= 10000 {
		for k, a := range s.attempts {
			if now.Sub(a.since) >= 15*time.Minute {
				delete(s.attempts, k)
			}
		}
		s.lastAttemptCleanup = now
	}
	a, exists := s.attempts[key]
	if !exists && len(s.attempts) >= 10000 {
		return false
	}
	if now.Sub(a.since) >= 15*time.Minute {
		a = attempt{since: now}
	}
	a.count++
	s.attempts[key] = a
	return a.count <= 10
}
