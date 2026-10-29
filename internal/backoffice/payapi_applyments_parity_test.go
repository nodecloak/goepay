package backoffice

import (
	"context"
	"encoding/csv"
	"net/http/httptest"
	"strings"
	"testing"
)

// 支付方式 gains the original 支持设备 (device) field: a catalog row with no
// record reads as the 0 default (PC+Mobile), and a stored record keeps whatever
// device class it was saved with so the console column and dialog match
// epay_pro's display_device (0 PC+Mobile / 1 PC / 2 Mobile).
func TestPayTypesDeviceFieldCatalogDefaultAndStoredRecord(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	// Schema advertises 支持设备 as a 3-option select defaulting to 0.
	sc, e := s.Schema(admin, "pay-types")
	if e != nil {
		t.Fatal(e)
	}
	var device *Field
	for i := range sc.Fields {
		if sc.Fields[i].Name == "device" {
			device = &sc.Fields[i]
		}
	}
	if device == nil {
		t.Fatal("pay-types schema has no 支持设备 (device) field")
	}
	if device.Label != "支持设备" || device.Type != "select" || device.Default != "0" {
		t.Fatalf("device field wrong: %+v", device)
	}
	labels := []string{}
	for _, o := range device.Options {
		labels = append(labels, o.Value+"="+o.Label)
	}
	if strings.Join(labels, ",") != "0=PC+Mobile,1=PC,2=Mobile" {
		t.Fatalf("device options = %v", labels)
	}

	// Catalog rows with no record carry the 0 default.
	page, e := s.List(ctx, admin, "pay-types", Query{})
	if e != nil {
		t.Fatal(e)
	}
	for _, it := range page.Items {
		if it.ID == 0 && it.Fields["device"] != "0" {
			t.Fatalf("catalog default row device=%v want 0 (%+v)", it.Fields["device"], it.Fields)
		}
	}

	// A record saved with device=2 (Mobile) shows that device through the
	// merged listing.
	created, e := s.Create(ctx, admin, "pay-types", input(`{"name":"QQ钱包","type":"qqpay","enabled":true,"device":"2"}`))
	if e != nil {
		t.Fatal("create pay-type with device rejected", e)
	}
	page, e = s.List(ctx, admin, "pay-types", Query{})
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, it := range page.Items {
		if it.ID == created.ID {
			found = true
			if it.Fields["device"] != "2" {
				t.Fatalf("stored record device=%v want 2", it.Fields["device"])
			}
		}
	}
	if !found {
		t.Fatal("stored qqpay record not present in merged listing")
	}
}

// 商户管控记录 and 风控记录 gain a 状态/风控类型 toolbar filter and a bounded
// non-secret CSV export, matching epay_pro's 导出 and typed search, using the
// same generic machinery as blacklist/domains.
func TestRiskRecordToolbarFilterAndExport(t *testing.T) {
	s := setup(t)
	ctx := context.Background()

	// merchant-risk: status filter + exportable.
	sc, e := s.Schema(admin, "merchant-risk")
	if e != nil {
		t.Fatal(e)
	}
	if !sc.Exportable || len(sc.Filters) != 1 || sc.Filters[0].Param != "status" {
		t.Fatalf("merchant-risk toolbar flags wrong: %+v", sc)
	}
	if _, e = s.Create(ctx, admin, "merchant-risk", input(`{"merchant_id":"101","status":"blocked","reason":"管控","reference":"ref-1"}`)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create(ctx, admin, "merchant-risk", input(`{"merchant_id":"102","status":"normal","reason":"正常","reference":"ref-2"}`)); e != nil {
		t.Fatal(e)
	}
	blocked, e := s.List(ctx, admin, "merchant-risk", Query{Filter: map[string]string{"status": "blocked"}})
	if e != nil || blocked.Total != 1 || blocked.Items[0].Fields["merchant_id"] != "101" {
		t.Fatalf("status=blocked page=%+v err=%v", blocked, e)
	}
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/resources/merchant-risk?format=csv&status=blocked", nil)
	s.Serve(w, req, admin, []string{"merchant-risk"})
	if w.Code != 200 {
		t.Fatalf("merchant-risk export status=%d body=%s", w.Code, w.Body.String())
	}
	bodyText := strings.TrimPrefix(w.Body.String(), "\xef\xbb\xbf")
	records, e := csv.NewReader(strings.NewReader(bodyText)).ReadAll()
	if e != nil {
		t.Fatal(e)
	}
	if len(records) != 2 { // header + 1 blocked row
		t.Fatalf("merchant-risk export rows=%d want header+1", len(records))
	}

	// risk-records: decision filter + exportable.
	rsc, e := s.Schema(admin, "risk-records")
	if e != nil {
		t.Fatal(e)
	}
	if !rsc.Exportable || len(rsc.Filters) != 1 || rsc.Filters[0].Param != "decision" {
		t.Fatalf("risk-records toolbar flags wrong: %+v", rsc)
	}
	if _, e = s.Create(ctx, admin, "risk-records", input(`{"merchant_id":"101","decision":"deny","reason":"拦截","source":"rule","trade_no":"T1"}`)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create(ctx, admin, "risk-records", input(`{"merchant_id":"102","decision":"allow","reason":"放行","source":"rule","trade_no":"T2"}`)); e != nil {
		t.Fatal(e)
	}
	deny, e := s.List(ctx, admin, "risk-records", Query{Filter: map[string]string{"decision": "deny"}})
	if e != nil || deny.Total != 1 || deny.Items[0].Fields["merchant_id"] != "101" {
		t.Fatalf("decision=deny page=%+v err=%v", deny, e)
	}
}

// 进件商户资料 gains a 本地审核状态 toolbar filter so the applyments list can be
// narrowed by lifecycle state, as epay_pro's 状态 search does.
func TestOnboardingApplicationsStatusFilter(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	sc, e := s.Schema(admin, "onboarding-applications")
	if e != nil {
		t.Fatal(e)
	}
	if len(sc.Filters) != 1 || sc.Filters[0].Param != "status" {
		t.Fatalf("onboarding-applications filters wrong: %+v", sc.Filters)
	}
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "m1"}
	approved, e := s.Create(ctx, merchant, "onboarding-applications", input(`{"business_name":"A 店","contact_name":"甲","contact_phone":"13000000001","channel_id":"1"}`))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Create(ctx, merchant, "onboarding-applications", input(`{"business_name":"B 店","contact_name":"乙","contact_phone":"13000000002","channel_id":"1"}`)); e != nil {
		t.Fatal(e)
	}
	// Move the first record through the local review workflow to approved.
	if e = s.caseState(ctx, merchant, "onboarding-applications", approved.ID, "under_review"); e != nil {
		t.Fatal(e)
	}
	if e = s.caseStateWithComment(ctx, admin, "onboarding-applications", approved.ID, "approved", "ok"); e != nil {
		t.Fatal(e)
	}
	page, e := s.List(ctx, admin, "onboarding-applications", Query{Filter: map[string]string{"status": "approved"}})
	if e != nil || page.Total != 1 || page.Items[0].Fields["business_name"] != "A 店" {
		t.Fatalf("status=approved page=%+v err=%v", page, e)
	}
	// The untouched record is still a draft, excluded by the approved filter.
	draft, e := s.List(ctx, admin, "onboarding-applications", Query{Filter: map[string]string{"status": "draft"}})
	if e != nil || draft.Total != 1 || draft.Items[0].Fields["business_name"] != "B 店" {
		t.Fatalf("status=draft page=%+v err=%v", draft, e)
	}
}
