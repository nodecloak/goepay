package backoffice

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

// B07 #9: the admin 授权域名 page gains 批量通过/批量拒绝 (delete is already covered by
// the generic batch-delete). Each selected domain goes through the same admin
// review path the single 通过/拒绝 edit uses, including the domain-review audit.
func TestBatchDomainStatusApproveReject(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	admins := Principal{Role: "admin", AdminClass: "super", SubjectID: 1, Username: "a"}

	// Schema exposes batch_status for admin domains, not for a merchant.
	sc, err := s.Schema(admins, "domains")
	if err != nil || !sc.BatchStatus {
		t.Fatalf("admin domains must expose batch_status: %+v err=%v", sc, err)
	}
	if ms, err := s.Schema(Principal{Role: "merchant", SubjectID: 101}, "domains"); err != nil || ms.BatchStatus {
		t.Fatalf("merchant domains must not expose batch_status: %+v err=%v", ms, err)
	}

	var ids []int64
	for i, host := range []string{"a.example.invalid", "b.example.invalid", "c.example.invalid"} {
		d, e := s.Create(ctx, admins, "domains", input(fmt.Sprintf(`{"merchant_id":"101","domain":"%s","approved":false,"enabled":true}`, host)))
		if e != nil {
			t.Fatal(e)
		}
		if d.Fields["approved"] != false {
			t.Fatalf("seed %d should be unapproved", i)
		}
		ids = append(ids, d.ID)
	}

	// Approve the first two via the service.
	done, e := s.BatchDomainStatus(ctx, admins, ids[:2], true)
	if e != nil || done != 2 {
		t.Fatalf("batch approve: done=%d err=%v", done, e)
	}
	var approved int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_records WHERE resource='domains' AND body->>'approved'='true'").Scan(&approved); e != nil || approved != 2 {
		t.Fatalf("expected 2 approved, got %d err=%v", approved, e)
	}
	// A domain review audit row is written per decision.
	var audits int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM backoffice_audit WHERE resource='domains'").Scan(&audits); e != nil {
		t.Fatal(e)
	}
	if audits < 2 {
		t.Fatalf("expected per-decision audit rows, got %d", audits)
	}

	// Reject the first one back via the HTTP endpoint.
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/resources/domains/batch-status", strings.NewReader(fmt.Sprintf(`{"ids":["%d"],"approved":false}`, ids[0])))
	s.serveBatchDomainStatus(w, r, admins, "domains")
	if w.Code != 200 {
		t.Fatalf("http batch-status: %d %s", w.Code, w.Body.String())
	}
	var stillApproved bool
	if e = s.pool.QueryRow(ctx, "SELECT (body->>'approved')='true' FROM backoffice_records WHERE resource='domains' AND id=$1", ids[0]).Scan(&stillApproved); e != nil {
		t.Fatal(e)
	}
	if stillApproved {
		t.Fatal("first domain should be rejected after batch reject")
	}

	// Guards: merchant forbidden, empty/oversized ids and missing approved refused.
	if _, e = s.BatchDomainStatus(ctx, Principal{Role: "merchant", SubjectID: 101}, ids, true); e != ErrForbidden {
		t.Fatalf("merchant must be forbidden: %v", e)
	}
	if _, e = s.BatchDomainStatus(ctx, admins, nil, true); e != ErrInvalid {
		t.Fatalf("empty ids must be invalid: %v", e)
	}
	for _, body := range []string{`{"ids":["1"]}`, `{"ids":["0"],"approved":true}`, `{"ids":["x"],"approved":true}`, `{"approved":true}`, `{"ids":["1"],"approved":true,"extra":1}`} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("POST", "/resources/domains/batch-status", strings.NewReader(body))
		s.serveBatchDomainStatus(w, r, admins, "domains")
		if w.Code < 400 {
			t.Fatalf("bad batch-status body accepted: %s code=%d", body, w.Code)
		}
	}
	// Wrong resource refused.
	w = httptest.NewRecorder()
	r = httptest.NewRequest("POST", "/resources/blacklist/batch-status", strings.NewReader(`{"ids":["1"],"approved":true}`))
	s.serveBatchDomainStatus(w, r, admins, "blacklist")
	if w.Code != 404 {
		t.Fatalf("non-domains resource must 404: %d", w.Code)
	}
}
