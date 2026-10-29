package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"goepay/internal/identity"
	"goepay/internal/payment"
)

// B07 #5/#6/#7/#8: the admin 站内转账 page gains 发起方/收款方/状态/日期 filters,
// 来源 (管理员/商户) / 备注(拒绝原因) / 完成时间 columns, and a 拒绝原因 captured on
// rejection. Big IDs stress int64 formatting; the reason round-trips through the
// immutable review event.
const adminIT165A int64 = 9007199254742001
const adminIT165B int64 = 9007199254742003
const adminIT165C int64 = 9007199254742005

func adminInternalTransfer165Fixture(t *testing.T) (*Server, func(string, string, string) *httptest.ResponseRecorder) {
	t.Helper()
	s, p := groupRoutingFixture152(t)
	s.ProviderFlow = nil
	s.Sessions = identity.NewSessions(false)
	ctx := context.Background()
	for _, id := range []int64{adminIT165A, adminIT165B, adminIT165C} {
		if e := s.Store.CreateMerchant(ctx, payment.Merchant{ID: id, Name: "synthetic adminIT165", APIKey: fmt.Sprintf("synthetic-adminit165-%d", id)}); e != nil {
			t.Fatal(e)
		}
	}
	if _, e := p.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class) VALUES('adminit165','admin',165,'Synthetic Admin','unused-synthetic-hash','super')`); e != nil {
		t.Fatal(e)
	}
	token, csrf, e := s.Sessions.Create(identity.User{Role: "admin", ID: 165, Username: "adminit165", AdminClass: "super"})
	if e != nil {
		t.Fatal(e)
	}
	// Seed requests with their origin (first event) and terminal (decision) events.
	rows := []struct {
		id, from, to int64
		status       string
		origin       string
		reason       string
	}{
		{9007199254742101, adminIT165A, adminIT165B, "approved", "admin:9:private-admin", ""},
		{9007199254742103, adminIT165A, adminIT165B, "rejected", "merchant:9007199254742001:private-merchant", "余额不足，拒绝"},
		{9007199254742105, adminIT165B, adminIT165A, "pending", "admin:9:private-admin", ""},
		{9007199254742107, adminIT165A, adminIT165C, "pending", "merchant:9007199254742001:private-merchant", ""},
	}
	for _, v := range rows {
		if _, e := p.Exec(ctx, "INSERT INTO ledger_requests(id,kind,merchant_id,recipient_id,external_no,amount,status,created_at,updated_at) VALUES($1,'internal-transfers',$2,$3,$4,123,$5,'2026-10-20T01:00:00Z','2026-10-21T22:00:00Z')", v.id, v.from, v.to, fmt.Sprintf("private-ref-%d", v.id), v.status); e != nil {
			t.Fatal(e)
		}
		if _, e := p.Exec(ctx, "INSERT INTO ledger_request_events(request_id,actor,status,created_at) VALUES($1,$2,'pending','2026-10-20T01:30:00Z')", v.id, v.origin); e != nil {
			t.Fatal(e)
		}
		if v.status != "pending" {
			var reason any
			if v.reason != "" {
				reason = v.reason
			}
			if _, e := p.Exec(ctx, "INSERT INTO ledger_request_events(request_id,actor,status,created_at,reason) VALUES($1,'admin:165:adminit165',$2,'2026-10-20T02:00:00Z',$3)", v.id, v.status, reason); e != nil {
				t.Fatal(e)
			}
		}
	}
	return s, func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, "/console/v1/admin/"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: token})
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
}

func adminIT165Page(t *testing.T, w *httptest.ResponseRecorder) (int, []map[string]any) {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("HTTP status=%d body=%s", w.Code, w.Body.String())
	}
	var v struct {
		Data struct {
			Total int              `json:"total"`
			Items []map[string]any `json:"items"`
		} `json:"data"`
	}
	if e := json.Unmarshal(w.Body.Bytes(), &v); e != nil {
		t.Fatal(e)
	}
	return v.Data.Total, v.Data.Items
}

func TestAdminInternalTransfer165FiltersColumnsAndOrigin(t *testing.T) {
	_, request := adminInternalTransfer165Fixture(t)
	// Unfiltered: admin sees all four.
	total, items := adminIT165Page(t, request("GET", "workflows/internal-transfers", ""))
	if total != 4 || len(items) != 4 {
		t.Fatalf("admin must see all internal transfers: total=%d items=%d", total, len(items))
	}
	// Enriched columns on the terminal rejected row.
	var rejected map[string]any
	for _, row := range items {
		if row["id"] == "9007199254742103" {
			rejected = row
		}
	}
	if rejected == nil {
		t.Fatal("rejected row missing")
	}
	if rejected["merchant_id"] != "9007199254742001" || rejected["recipient_id"] != "9007199254742003" {
		t.Fatalf("发起方/收款方 wrong: %#v", rejected)
	}
	if rejected["origin"] != "merchant" || rejected["reason"] != "余额不足，拒绝" || rejected["completed_at"] == nil {
		t.Fatalf("来源/备注/完成时间 wrong: %#v", rejected)
	}
	// Filters.
	for _, v := range []struct {
		query string
		total int
	}{
		{"merchant_id=9007199254742001", 3},
		{"recipient_id=9007199254742001", 1},
		{"status=pending", 2},
		{"status=rejected", 1},
		{"merchant_id=9007199254742001&status=approved", 1},
		{"from=2026-10-20&to=2026-10-20", 4},
		{"from=2026-10-21", 0},
	} {
		total, rows := adminIT165Page(t, request("GET", "workflows/internal-transfers?"+v.query, ""))
		if total != v.total || len(rows) != v.total {
			t.Fatalf("filter %q got total=%d items=%d want %d", v.query, total, len(rows), v.total)
		}
	}
	// Invalid filters refused.
	for _, q := range []string{"merchant_id=0", "merchant_id=abc", "recipient_id=-1", "status=succeeded", "from=2026-13-01", "from=2026-10-22&to=2026-10-20", "unknown=1"} {
		if w := request("GET", "workflows/internal-transfers?"+q, ""); w.Code < 400 {
			t.Fatalf("invalid filter accepted: %q code=%d", q, w.Code)
		}
	}
	// No private material leaks.
	for _, needle := range []string{"private-admin", "private-merchant", "private-ref", "adminit165"} {
		if strings.Contains(fmt.Sprint(items), needle) {
			t.Fatalf("sensitive data leaked: %s", needle)
		}
	}
}

func TestAdminInternalTransfer165RejectReasonRoundTrips(t *testing.T) {
	s, request := adminInternalTransfer165Fixture(t)
	ctx := context.Background()
	// Real reservable requests: fund the initiator, then create pending transfers
	// the admin review can actually release. Each carries a merchant: origin event.
	o, e := s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: adminIT165A, OutTradeNo: "adminit165-fund", Amount: 100000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Store.MarkPaid(ctx, o.TradeNo, "adminit165-fund-request"); e != nil {
		t.Fatal(e)
	}
	withReason, e := s.Store.RequestFunds(ctx, "internal-transfers", adminIT165A, adminIT165B, "adminit165-reject-reason", 300, fmt.Sprintf("merchant:%d", adminIT165A))
	if e != nil {
		t.Fatal(e)
	}
	noReason, e := s.Store.RequestFunds(ctx, "internal-transfers", adminIT165A, adminIT165B, "adminit165-reject-empty", 200, fmt.Sprintf("merchant:%d", adminIT165A))
	if e != nil {
		t.Fatal(e)
	}
	withID := strconv.FormatInt(withReason.ID, 10)
	noID := strconv.FormatInt(noReason.ID, 10)
	// Reject with a 拒绝原因; it must land on the review event.
	w := request("POST", "workflows/internal-transfers/"+withID+"/reject", `{"reason":"重复申请，驳回"}`)
	if w.Code != 200 {
		t.Fatalf("reject with reason failed: %d %s", w.Code, w.Body.String())
	}
	var stored string
	if e := s.Pool.QueryRow(ctx, "SELECT reason FROM ledger_request_events WHERE request_id=$1 AND status='rejected' ORDER BY id DESC LIMIT 1", withReason.ID).Scan(&stored); e != nil {
		t.Fatal(e)
	}
	if stored != "重复申请，驳回" {
		t.Fatalf("reason not stored: %q", stored)
	}
	// It is readable back as the row's 备注.
	_, items := adminIT165Page(t, request("GET", "workflows/internal-transfers?status=rejected", ""))
	var found bool
	for _, row := range items {
		if row["id"] == withID {
			found = true
			if row["reason"] != "重复申请，驳回" {
				t.Fatalf("reason not surfaced: %#v", row)
			}
		}
	}
	if !found {
		t.Fatal("rejected row not returned after reject")
	}
	// A rejection with no body still works and stores no reason.
	w = request("POST", "workflows/internal-transfers/"+noID+"/reject", "{}")
	if w.Code != 200 {
		t.Fatalf("reject without reason failed: %d %s", w.Code, w.Body.String())
	}
	var nullReason *string
	if e := s.Pool.QueryRow(ctx, "SELECT reason FROM ledger_request_events WHERE request_id=$1 AND status='rejected' ORDER BY id DESC LIMIT 1", noReason.ID).Scan(&nullReason); e != nil {
		t.Fatal(e)
	}
	if nullReason != nil {
		t.Fatalf("empty reason should stay NULL, got %q", *nullReason)
	}
	// An over-long, unknown-field, or control-char reason is refused before review.
	for _, bad := range []string{`{"reason":"` + strings.Repeat("x", 501) + `"}`, `{"note":"x"}`, "{\"reason\":\"a\nb\"}"} {
		if w := request("POST", "workflows/internal-transfers/9007199254742101/reject", bad); w.Code < 400 {
			t.Fatalf("bad reject body accepted: %s code=%d", bad, w.Code)
		}
	}
}
