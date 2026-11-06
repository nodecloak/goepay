package server

import (
	"net/http"
	"strings"

	"goepay/internal/backoffice"
	"goepay/internal/identity"
	"goepay/internal/ledger"
	"goepay/internal/smsgateway"
)

// merchantDashboardTip is one actionable health notice shown as an alert banner
// on the merchant dashboard. It mirrors the epay_pro `tips[]` shape (type,
// title, link, link_text) consumed by the Console index page: each banner
// carries a button that routes the merchant to the console page that resolves
// the condition. Only conditions GOEPAY can compute from its own data are
// emitted; identity (real-name) verification is deliberately out of scope.
type merchantDashboardTip struct {
	Type     string `json:"type"`
	Title    string `json:"title"`
	Link     string `json:"link"`
	LinkText string `json:"link_text"`
}

// merchantDashboardTips is an owner-scoped, read-only endpoint:
// GET /console/v1/merchant/dashboard/tips. It never writes, calls a provider or
// touches the pinned dashboard snapshot query; a failure here returns an error
// the dashboard ignores rather than breaking the overview. Merchant sessions
// only — an admin or anonymous caller is refused.
func (s *Server) merchantDashboardTips(w http.ResponseWriter, r *http.Request, role string, session identity.Session, parts []string) bool {
	if len(parts) != 3 || parts[1] != "dashboard" || parts[2] != "tips" {
		return false
	}
	if r.Method != http.MethodGet {
		response(w, 405, -1, "GET required", nil)
		return true
	}
	if role != "merchant" {
		response(w, 403, -1, "merchant session required", nil)
		return true
	}
	if r.URL.RawQuery != "" {
		response(w, 400, -1, "query parameters are not accepted", nil)
		return true
	}
	if s.Store == nil || s.Pool == nil {
		response(w, 503, -1, "dashboard tips unavailable", nil)
		return true
	}
	mid := session.User.ID
	tips, e := s.collectMerchantDashboardTips(r, mid)
	if e != nil {
		s.fail(w, e)
		return true
	}
	response(w, 200, 0, "", map[string]any{"tips": tips})
	return true
}

// merchantTipState is everything the tips depend on, read from the merchant's
// own account state; merchantTipsFor turns it into banners without touching
// the database, so each rule is unit-tested on its own.
type merchantTipState struct {
	Pay               int
	SettlementMissing bool
	PhoneBound        bool
	SMSReady          bool
	EmailBound        bool
	DepositEnabled    bool
	DepositMinCents   int64
	DepositHeldCents  int64
}

// merchantTipsFor lists the banners in a fixed order: payment status, deposit
// shortfall, settlement account, phone, email. The phone tip only appears when
// an SMS sender is active, because without one the merchant cannot bind a
// phone and the banner could never be cleared.
func merchantTipsFor(st merchantTipState) []merchantDashboardTip {
	tips := []merchantDashboardTip{}
	switch st.Pay {
	case 2:
		tips = append(tips, merchantDashboardTip{Type: "warning", Title: "您的支付功能正在审核中，审核通过后方可正常收款", Link: "/account/settings", LinkText: "查看账户"})
	case 0:
		tips = append(tips, merchantDashboardTip{Type: "error", Title: "您的支付功能已被限制，请联系平台客服处理", Link: "/account/settings", LinkText: "查看账户"})
	}
	// 当前平台要求最低缴纳保证金 N 元，否则无法调用接口发起支付 (enforced by
	// payment.ErrDepositBelowMinimum on every payment order).
	if st.DepositEnabled && st.DepositMinCents > 0 && st.DepositHeldCents < st.DepositMinCents {
		tips = append(tips, merchantDashboardTip{Type: "error", Title: "您的保证金不足，当前平台要求最低缴纳保证金 " + ledger.Format(st.DepositMinCents) + " 元，否则无法调用接口发起支付", Link: "/account/deposit", LinkText: "缴纳保证金"})
	}
	if st.SettlementMissing {
		tips = append(tips, merchantDashboardTip{Type: "warning", Title: "您还未设置结算账户，请尽快设置以便正常结算", Link: "/account/settings", LinkText: "前往设置"})
	}
	if st.SMSReady && !st.PhoneBound {
		tips = append(tips, merchantDashboardTip{Type: "warning", Title: "您还未绑定手机号码，请尽快绑定以保障账户安全", Link: "/account/settings", LinkText: "前往绑定"})
	}
	if !st.EmailBound {
		tips = append(tips, merchantDashboardTip{Type: "warning", Title: "您还未绑定邮箱，请尽快绑定以保障账户安全", Link: "/account/settings", LinkText: "前往绑定"})
	}
	return tips
}

// collectMerchantDashboardTips reads the merchant's state for merchantTipsFor.
// A phone counts as bound when either the registration phone or the verified
// 账号绑定 mobile (bind_mobile) is set.
func (s *Server) collectMerchantDashboardTips(r *http.Request, mid int64) ([]merchantDashboardTip, error) {
	ctx := r.Context()
	var st merchantTipState
	var phone, bindMobile, email string
	var hasEmail bool
	if e := s.Pool.QueryRow(ctx, `SELECT COALESCE((to_jsonb(m)->>'pay')::int,1),COALESCE(m.profile->>'phone',''),COALESCE(m.profile->>'bind_mobile',''),COALESCE((SELECT email FROM enrollment_emails WHERE merchant_id=m.id),m.profile->>'email',''),EXISTS(SELECT 1 FROM enrollment_emails WHERE merchant_id=m.id) OR COALESCE(m.profile->>'email','')<>'' FROM merchants m WHERE m.id=$1`, mid).Scan(&st.Pay, &phone, &bindMobile, &email, &hasEmail); e != nil {
		return nil, e
	}
	st.PhoneBound = strings.TrimSpace(phone) != "" || strings.TrimSpace(bindMobile) != ""
	st.EmailBound = hasEmail && strings.TrimSpace(email) != ""
	recipient, e := s.Store.SettlementRecipient(ctx, mid)
	if e != nil {
		return nil, e
	}
	st.SettlementMissing = recipient.State == "missing"
	if len(s.CheckoutKey) == 32 {
		if _, e := s.smsActiveSender(ctx, smsgateway.New(s.Pool, s.CheckoutKey)); e == nil {
			st.SMSReady = true
		}
	}
	deposit, e := backoffice.New(s.Pool, s.Store).DepositSettingsForMerchant(ctx, mid)
	if e != nil {
		return nil, e
	}
	st.DepositEnabled, st.DepositMinCents = deposit.Enabled, deposit.MinCents
	if st.DepositEnabled && st.DepositMinCents > 0 {
		if st.DepositHeldCents, _, e = s.Store.DepositBalance(ctx, mid); e != nil {
			return nil, e
		}
	}
	return merchantTipsFor(st), nil
}
