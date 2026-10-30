package server

import (
	"net/http"
	"strings"

	"goepay/internal/identity"
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

// collectMerchantDashboardTips derives the banner list from the merchant's own
// account state. Every tip is a condition epay_pro warns the merchant about on
// the dashboard and that GOEPAY can evaluate locally, worded the epay_pro way
// and linked to the merchant route that fixes it.
func (s *Server) collectMerchantDashboardTips(r *http.Request, mid int64) ([]merchantDashboardTip, error) {
	ctx := r.Context()
	tips := []merchantDashboardTip{}

	// Payment permission: pay=2 pending audit, pay=0 restricted. pay=1 normal.
	var pay int
	var phone, email string
	var hasEmail bool
	if e := s.Pool.QueryRow(ctx, `SELECT COALESCE((to_jsonb(m)->>'pay')::int,1),COALESCE(m.profile->>'phone',''),COALESCE((SELECT email FROM enrollment_emails WHERE merchant_id=m.id),m.profile->>'email',''),EXISTS(SELECT 1 FROM enrollment_emails WHERE merchant_id=m.id) OR COALESCE(m.profile->>'email','')<>'' FROM merchants m WHERE m.id=$1`, mid).Scan(&pay, &phone, &email, &hasEmail); e != nil {
		return nil, e
	}
	switch pay {
	case 2:
		tips = append(tips, merchantDashboardTip{Type: "warning", Title: "您的支付功能正在审核中，审核通过后方可正常收款", Link: "/account/settings", LinkText: "查看账户"})
	case 0:
		tips = append(tips, merchantDashboardTip{Type: "error", Title: "您的支付功能已被限制，请联系平台客服处理", Link: "/account/settings", LinkText: "查看账户"})
	}

	// Settlement account: epay_pro warns when the merchant has not set a
	// settlement (收款) account. SettlementRecipient reports "missing" until one
	// is configured.
	recipient, e := s.Store.SettlementRecipient(ctx, mid)
	if e != nil {
		return nil, e
	}
	if recipient.State == "missing" {
		tips = append(tips, merchantDashboardTip{Type: "warning", Title: "您还未设置结算账户，请尽快设置以便正常结算", Link: "/account/settings", LinkText: "前往设置"})
	}

	// Contact bindings: epay_pro requires 绑定手机号码 / 绑定邮箱 before several
	// actions ("请先绑定手机号码！" / "请先绑定邮箱！").
	if strings.TrimSpace(phone) == "" {
		tips = append(tips, merchantDashboardTip{Type: "warning", Title: "您还未绑定手机号码，请尽快绑定以保障账户安全", Link: "/account/settings", LinkText: "前往绑定"})
	}
	if !hasEmail || strings.TrimSpace(email) == "" {
		tips = append(tips, merchantDashboardTip{Type: "warning", Title: "您还未绑定邮箱，请尽快绑定以保障账户安全", Link: "/account/settings", LinkText: "前往绑定"})
	}

	return tips, nil
}
