package backoffice

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func rollEditorRequest(s *Service, p Principal, method, path, raw string) (int, map[string]any) {
	w := httptest.NewRecorder()
	s.Serve(w, httptest.NewRequest(method, path, strings.NewReader(raw)), p, strings.Split(strings.TrimPrefix(path, "/"), "/"))
	var response struct {
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	return w.Code, response.Data
}

func TestPayRollEditorDraftIsInertAndRejectsHiddenActivation(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	status, data := rollEditorRequest(s, admin, "POST", "/pay-rolls/drafts", `{"creation_mode":"draft","name":"待配置轮询","type":"alipay","strategy":"weighted"}`)
	if status != 201 {
		t.Fatalf("three-field draft rejected: %d %+v", status, data)
	}
	id := data["id"].(string)
	var fields map[string]any
	var raw []byte
	if e := s.pool.QueryRow(ctx, `SELECT body FROM backoffice_records WHERE resource='pay-rolls' AND id=$1`, id).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	if json.Unmarshal(raw, &fields) != nil {
		t.Fatal("invalid stored body")
	}
	if fields["enabled"] != false || fields["default_route"] != false || fields["merchant_id"] != "0" || fields["channel_ids"] != "" || fields["limits"] != "{}" || fields["rules"] != "{}" {
		t.Fatalf("draft activated routing or invented policy: %+v", fields)
	}
	roll, e := ParsePayRoll(fields)
	if e != nil || roll.Enabled || roll.DefaultRoute || len(roll.Channels) != 0 {
		t.Fatalf("inert weighted draft invalid: %+v %v", roll, e)
	}
	for _, extra := range []string{`"enabled":true`, `"default_route":true`, `"merchant_id":"101"`, `"channel_ids":"7:1"`, `"rules":"{}"`} {
		status, _ = rollEditorRequest(s, admin, "POST", "/pay-rolls/drafts", `{"creation_mode":"draft","name":"bad","type":"alipay","strategy":"first",`+extra+`}`)
		if status != 422 {
			t.Fatalf("hidden activation accepted: %s status=%d", extra, status)
		}
	}
	status, _ = rollEditorRequest(s, Principal{Role: "merchant", SubjectID: 101}, "POST", "/pay-rolls/drafts", `{"creation_mode":"draft","name":"bad","type":"alipay","strategy":"first"}`)
	if status != 403 {
		t.Fatalf("merchant draft access=%d", status)
	}
	var records, audits, eligible int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM backoffice_records WHERE resource='pay-rolls'),(SELECT count(*) FROM backoffice_audit WHERE resource='pay-rolls'),(SELECT count(*) FROM backoffice_records WHERE resource='pay-rolls' AND body->'enabled'='true'::jsonb)`).Scan(&records, &audits, &eligible); e != nil || records != 1 || audits != 1 || eligible != 0 {
		t.Fatalf("draft side effects records=%d audits=%d eligible=%d: %v", records, audits, eligible, e)
	}
	for key, value := range map[string]any{"enabled": true, "default_route": true, "limits": `{"daily_count":1}`, "rules": `{"7":{}}`} {
		bad := map[string]any{}
		for k, v := range fields {
			bad[k] = v
		}
		bad[key] = value
		if _, e = ParsePayRoll(bad); e == nil {
			t.Fatalf("empty executable/nonempty-policy draft accepted: %s", key)
		}
	}
	if _, e = s.Create(ctx, admin, "groups", input(fmt.Sprintf(`{"name":"draft-reference","info":{"alipay":{"channel":%q,"type":"roll","rate":"99"}}}`, id))); e == nil {
		t.Fatal("disabled draft became explicit group route")
	}
}

func TestPayRollEditorChannelsCASPreservesPolicyAndRequiresActiveConfirmation(t *testing.T) {
	s := setup(t)
	s.GroupChannelAllowed = func(id int64, kind string) bool { return kind == "alipay" && (id == 7 || id == 8 || id == 9) }
	ctx := context.Background()
	raw, _ := json.Marshal(payRollValues())
	item, e := s.Create(ctx, admin, "pay-rolls", input(string(raw)))
	if e != nil {
		t.Fatal(e)
	}
	path := fmt.Sprintf("/pay-rolls/%d/channels", item.ID)
	status, snapshot := rollEditorRequest(s, admin, "GET", path, "")
	if status != 200 {
		t.Fatalf("channel snapshot unavailable: %d %+v", status, snapshot)
	}
	revision, _ := snapshot["revision"].(string)
	post := func(rev, ids string, confirmed bool) int {
		body, _ := json.Marshal(map[string]any{"expected_revision": rev, "channel_ids": ids, "confirm_enabled": confirmed})
		code, _ := rollEditorRequest(s, admin, "POST", path, string(body))
		return code
	}
	if code := post(revision, "8:3,9:2", false); code != 422 {
		t.Fatalf("active route changed without confirmation: %d", code)
	}
	if code := post(revision, "8:3,99:2", true); code != 422 {
		t.Fatalf("unknown/cross-type runtime channel accepted: %d", code)
	}
	if code := post(revision, "8:3,9:2", true); code != 200 {
		t.Fatalf("confirmed channel save failed: %d", code)
	}
	got, e := s.Get(ctx, admin, "pay-rolls", item.ID)
	if e != nil {
		t.Fatal(e)
	}
	if got.Fields["merchant_id"] != "101" || got.Fields["default_route"] != true || got.Fields["enabled"] != true || got.Fields["limits"] != payRollValues()["limits"] || got.Fields["rules"] != "{}" || got.Fields["channel_ids"] != "8:3,9:2" {
		t.Fatalf("channel save rewrote advanced policy: %+v", got.Fields)
	}
	if code := post(revision, "7:1", true); code != 409 {
		t.Fatalf("stale full-body revision overwrote newer config: %d", code)
	}
	_, snapshot = rollEditorRequest(s, admin, "GET", path, "")
	revision = snapshot["revision"].(string)
	if _, e = s.Update(ctx, admin, "pay-rolls", item.ID, input(`{"limits":"{\"daily_count\":7}"}`)); e != nil {
		t.Fatal(e)
	}
	if code := post(revision, "7:1", true); code != 409 {
		t.Fatalf("advanced policy edit not protected by channel CAS: %d", code)
	}
	if code, _ := rollEditorRequest(s, Principal{Role: "merchant", SubjectID: 101}, "GET", path, ""); code != 403 {
		t.Fatalf("merchant channel snapshot access=%d", code)
	}
}

func TestPayRollEditorRuntimeCatalogUsesItsOwnResourcePermission(t *testing.T) {
	s := setup(t)
	s.GroupRoutingChannels = func() ([]GroupPaymentChannel, error) {
		return []GroupPaymentChannel{{ID: "7", Type: "alipay", Methods: []string{"page"}}}, nil
	}
	status, data := rollEditorRequest(s, admin, "GET", "/pay-rolls/routing-options", "")
	if status != 200 || data["available"] != true {
		t.Fatalf("pay-roll resource cannot read its runtime catalog: %d %+v", status, data)
	}
	if code, _ := rollEditorRequest(s, Principal{Role: "merchant", SubjectID: 101}, "GET", "/pay-rolls/routing-options", ""); code != 403 {
		t.Fatalf("merchant catalog access=%d", code)
	}
}

func TestPayRollEditorInertDraftCanRenameButCannotActivateWithoutChannels(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	draft, e := s.CreatePayRollDraft(ctx, admin, input(`{"creation_mode":"draft","name":"待配置原名","type":"alipay","strategy":"weighted"}`))
	if e != nil {
		t.Fatal(e)
	}
	renamed, e := s.Update(ctx, admin, "pay-rolls", draft.ID, input(`{"name":"待配置新名"}`))
	if e != nil {
		t.Fatalf("safe draft rename rejected before channels were configured: %v", e)
	}
	if renamed.Fields["name"] != "待配置新名" || renamed.Fields["channel_ids"] != "" || renamed.Fields["enabled"] != false || renamed.Fields["default_route"] != false {
		t.Fatal("rename changed inert routing state", renamed.Fields)
	}
	for _, change := range []string{`{"enabled":true}`, `{"default_route":true}`, `{"limits":"{\"daily_count\":1}"}`, `{"rules":"{\"7\":{}}"}`} {
		if _, e = s.Update(ctx, admin, "pay-rolls", draft.ID, input(change)); e == nil {
			t.Fatal("empty draft accepted executable or nonempty hidden policy", change)
		}
	}
	var records, audits int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM backoffice_records WHERE resource='pay-rolls'),(SELECT count(*) FROM backoffice_audit WHERE resource='pay-rolls')`).Scan(&records, &audits); e != nil || records != 1 || audits != 2 {
		t.Fatalf("rejected activation leaked state/audit: records=%d audits=%d err=%v", records, audits, e)
	}
	if _, e = s.Create(ctx, admin, "pay-rolls", input(`{"name":"generic must remain strict","merchant_id":"0","type":"alipay","strategy":"first","channel_ids":"","enabled":false,"default_route":false}`)); e == nil {
		t.Fatal("generic creation silently relaxed channel requirement")
	}
}
