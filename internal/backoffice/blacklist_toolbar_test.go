package backoffice

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// Blacklist toolbar: a per-field filter (黑名单类型 on kind), a bounded non-secret
// CSV export, and 批量删除 that runs the existing reversible delete per row.
// Domains gets the same generic field filter (状态 on approved, 启用 on enabled),
// mapped to GOEPAY's booleans with no invented 拒绝 state.
func TestBlacklistToolbarFilterExportBatchDelete(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	mk := func(kind, value string) int64 {
		item, e := s.Create(ctx, admin, "blacklist", input(`{"kind":"`+kind+`","value":"`+value+`","reason":"r","enabled":true}`))
		if e != nil {
			t.Fatalf("create %s %s: %v", kind, value, e)
		}
		return item.ID
	}
	ip1 := mk("ip", "10.0.0.1")
	ip2 := mk("ip", "10.0.0.2")
	mk("buyer", "buyer-x")

	// Schema advertises the toolbar capabilities.
	sc, e := s.Schema(admin, "blacklist")
	if e != nil {
		t.Fatal(e)
	}
	if !sc.Exportable || !sc.BatchDelete || len(sc.Filters) != 1 || sc.Filters[0].Param != "kind" {
		t.Fatalf("blacklist schema toolbar flags wrong: %+v", sc)
	}

	// (1) Filter on kind=ip returns only the two IP rows.
	page, e := s.List(ctx, admin, "blacklist", Query{Filter: map[string]string{"kind": "ip"}})
	if e != nil {
		t.Fatal(e)
	}
	if page.Total != 2 {
		t.Fatalf("kind=ip total=%d want 2", page.Total)
	}
	for _, it := range page.Items {
		if it.Fields["kind"] != "ip" {
			t.Fatalf("filtered row not ip: %+v", it.Fields)
		}
	}
	// An undeclared filter value is rejected.
	if _, e = s.List(ctx, admin, "blacklist", Query{Filter: map[string]string{"kind": "nope"}}); e != ErrInvalid {
		t.Fatalf("unknown filter value err=%v want ErrInvalid", e)
	}

	// (2) CSV export: bounded, non-secret headers, honours the kind filter.
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/resources/blacklist?format=csv&kind=ip", nil)
	s.Serve(w, req, admin, []string{"blacklist"})
	if w.Code != 200 {
		t.Fatalf("export status=%d body=%s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Fatalf("content-type=%q", ct)
	}
	bodyText := strings.TrimPrefix(w.Body.String(), "\xef\xbb\xbf")
	records, e := csv.NewReader(strings.NewReader(bodyText)).ReadAll()
	if e != nil {
		t.Fatal(e)
	}
	if len(records) != 3 { // header + 2 IP rows (buyer excluded by filter)
		t.Fatalf("export rows=%d want header+2", len(records))
	}
	header := strings.Join(records[0], ",")
	for _, secret := range []string{"password", "api_key", "secret", "token", "rsa"} {
		if strings.Contains(strings.ToLower(header), secret) {
			t.Fatalf("secret header %q in %s", secret, header)
		}
	}
	for _, want := range []string{"黑名单类型", "黑名单内容", "添加时间"} {
		if !strings.Contains(header, want) {
			t.Fatalf("header missing %q: %s", want, header)
		}
	}

	// (3) Batch delete runs the reversible delete for each selected row.
	raw, _ := json.Marshal(map[string][]string{"ids": {strconv.FormatInt(ip1, 10), strconv.FormatInt(ip2, 10)}})
	bw := httptest.NewRecorder()
	br := httptest.NewRequest("POST", "/resources/blacklist/batch-delete", strings.NewReader(string(raw)))
	s.Serve(bw, br, admin, []string{"blacklist", "batch-delete"})
	if bw.Code != 200 {
		t.Fatalf("batch-delete status=%d body=%s", bw.Code, bw.Body.String())
	}
	var resp struct {
		Data struct {
			Deleted int `json:"deleted"`
		} `json:"data"`
	}
	json.Unmarshal(bw.Body.Bytes(), &resp)
	if resp.Data.Deleted != 2 {
		t.Fatalf("deleted=%d want 2", resp.Data.Deleted)
	}
	after, _ := s.List(ctx, admin, "blacklist", Query{Filter: map[string]string{"kind": "ip"}})
	if after.Total != 0 {
		t.Fatalf("after batch delete kind=ip total=%d want 0", after.Total)
	}
}

func TestDomainsStatusAndEnabledFilter(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	admins := Principal{Role: "admin", SubjectID: 1, Username: "a"}
	// Two domains for merchant 101: one approved+enabled, one pending+disabled.
	if _, e := s.Create(ctx, admins, "domains", input(`{"merchant_id":"101","domain":"approved.example.invalid","approved":true,"enabled":true}`)); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Create(ctx, admins, "domains", input(`{"merchant_id":"101","domain":"pending.example.invalid","approved":false,"enabled":false}`)); e != nil {
		t.Fatal(e)
	}
	sc, e := s.Schema(admins, "domains")
	if e != nil {
		t.Fatal(e)
	}
	if len(sc.Filters) != 3 || sc.Filters[0].Param != "merchant_id" || sc.Filters[1].Param != "approved" || sc.Filters[2].Param != "enabled" {
		t.Fatalf("domains filters wrong: %+v", sc.Filters)
	}
	// No invented 拒绝 state: 状态 options are only 全部/待审核/已通过.
	labels := []string{}
	for _, o := range sc.Filters[1].Options {
		labels = append(labels, o.Label)
	}
	if strings.Join(labels, ",") != "全部状态,待审核,已通过" {
		t.Fatalf("状态 options = %v", labels)
	}
	// approved=true returns only the approved domain.
	page, e := s.List(ctx, admins, "domains", Query{Filter: map[string]string{"approved": "true"}})
	if e != nil || page.Total != 1 || page.Items[0].Fields["domain"] != "approved.example.invalid" {
		t.Fatalf("approved=true page=%+v err=%v", page, e)
	}
	// 待审核 (approved=false).
	pending, e := s.List(ctx, admins, "domains", Query{Filter: map[string]string{"approved": "false"}})
	if e != nil || pending.Total != 1 || pending.Items[0].Fields["domain"] != "pending.example.invalid" {
		t.Fatalf("approved=false page=%+v err=%v", pending, e)
	}
	// 启用=否 (enabled=false) returns the disabled one.
	disabled, e := s.List(ctx, admins, "domains", Query{Filter: map[string]string{"enabled": "false"}})
	if e != nil || disabled.Total != 1 || disabled.Items[0].Fields["domain"] != "pending.example.invalid" {
		t.Fatalf("enabled=false page=%+v err=%v", disabled, e)
	}
	// Merchant principal filters within its own domains only.
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m"}
	mp, e := s.List(ctx, merchant, "domains", Query{Filter: map[string]string{"approved": "true"}})
	if e != nil || mp.Total != 1 {
		t.Fatalf("merchant approved=true total=%d err=%v", mp.Total, e)
	}
}
