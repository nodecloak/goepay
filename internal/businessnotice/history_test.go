package businessnotice_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"goepay/internal/backoffice"
	"goepay/internal/businessnotice"
	"goepay/internal/payment"
)

func TestAdminBusiness83HistoryAuthenticatesOriginalSourceAndSurvivesDomainChange(t *testing.T) {
	p := businessPool(t)
	ctx := context.Background()
	payments, err := payment.NewWithKey(p, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err = payments.CreateMerchant(ctx, payment.Merchant{ID: 8301, Name: "history83", APIKey: "synthetic-history83"}); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"msgconfig_domain","value":"2"}')`); err != nil {
		t.Fatal(err)
	}
	service := backoffice.New(p, payments)
	owner := backoffice.Principal{Role: "merchant", SubjectID: 8301, Username: "owner8301"}
	domain, err := service.Create(ctx, owner, "domains", rawFields(`{"domain":"secret-domain83.example.test","enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	notices := payments.AdminBusinessNotices()
	events, err := notices.Events(ctx, 0, 20)
	if err != nil || len(events) != 1 {
		t.Fatalf("original event: %v %v", events, err)
	}
	first := events[0]
	if first.Scene != "domain" || first.MerchantID != 8301 || first.State != "queued" || first.Mode != "2" {
		t.Fatal("incorrect public event", first)
	}
	raw, _ := json.Marshal(first)
	if strings.Contains(string(raw), "secret-domain83") || strings.Contains(string(raw), "owner8301") {
		t.Fatal("private source exposed")
	}
	if _, err = service.Update(ctx, owner, "domains", domain.ID, rawFields(`{"domain":"later-domain83.example.test"}`)); err != nil {
		t.Fatal(err)
	}
	if err = service.Delete(ctx, owner, "domains", domain.ID); err != nil {
		t.Fatal(err)
	}
	after, err := notices.Events(ctx, 0, 20)
	if err != nil || len(after) != 2 || after[1] != first {
		t.Fatalf("history relied on mutable parent: %v %v", after, err)
	}
	if _, err = notices.Events(ctx, 0, 101); !errors.Is(err, businessnotice.ErrInvalid) {
		t.Fatal("unbounded history", err)
	}
	if _, err = p.Exec(ctx, `ALTER TABLE backoffice_audit DISABLE TRIGGER backoffice_audit_immutable`); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, `UPDATE backoffice_audit SET changes=jsonb_set(changes,'{domain}','"tampered.example.test"') WHERE id=(SELECT audit_id FROM admin_business_notice_events WHERE id=$1)`, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = notices.Events(ctx, 0, 20); !errors.Is(err, businessnotice.ErrConflict) {
		t.Fatal("tampered original audit accepted", err)
	}
	check, err := notices.Check(ctx)
	if err != nil || check.InvalidEvents != 1 {
		t.Fatal("tampered source not reported", check, err)
	}
}

func TestAdminBusiness83ProjectionFailurePreservesOriginalDomainAndReportsMissingEvent(t *testing.T) {
	p := businessPool(t)
	ctx := context.Background()
	payments, err := payment.NewWithKey(p, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err = payments.CreateMerchant(ctx, payment.Merchant{ID: 8301, Name: "optional83", APIKey: "synthetic-optional83"}); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, `CREATE FUNCTION reject_notice83() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'synthetic notice storage failure'; END$$; CREATE TRIGGER reject_notice83 BEFORE INSERT ON admin_business_notice_events FOR EACH ROW EXECUTE FUNCTION reject_notice83()`); err != nil {
		t.Fatal(err)
	}
	service := backoffice.New(p, payments)
	domain, err := service.Create(ctx, backoffice.Principal{Role: "merchant", SubjectID: 8301, Username: "owner8301"}, "domains", rawFields(`{"domain":"survives83.example.test","enabled":true}`))
	if err != nil || domain.ID <= 0 {
		t.Fatal("optional projection rolled back original business", domain, err)
	}
	check, err := payments.AdminBusinessNotices().Check(ctx)
	if err != nil || check.InvalidEvents != 1 {
		t.Fatal("missing projection silently lost", check, err)
	}
	var count int
	if err = p.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind='admin.business-notice'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed optional projection leaked job", count, err)
	}
	var audit int64
	if err = p.QueryRow(ctx, `SELECT id FROM backoffice_audit WHERE resource='domains' AND resource_id=$1`, domain.ID).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = payments.AdminBusinessNotices().CaptureDomain(ctx, tx, audit); !errors.Is(err, businessnotice.ErrConflict) {
		t.Fatal("historical source was adopted", err)
	}
}

func TestAdminBusiness83EventHeaderAndJobTamperingCannotSend(t *testing.T) {
	s, p, j := delivery83(t)
	ctx := context.Background()
	sender := &businessSender83{pool: p}
	r := &businessResolver83{sender: sender}
	if _, err := p.Exec(ctx, `UPDATE jobs SET payload='{"event_id":"9999"}' WHERE id=$1`, j.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleJob(ctx, j.ID, j.Token, r); !errors.Is(err, businessnotice.ErrState) {
		t.Fatal("forged job payload accepted", err)
	}
	if _, err := s.Events(ctx, 0, 20); !errors.Is(err, businessnotice.ErrConflict) {
		t.Fatal("job mutation lost original binding", err)
	}
	if _, err := p.Exec(ctx, `UPDATE jobs SET payload=jsonb_build_object('event_id',(SELECT id::text FROM admin_business_notice_events WHERE job_id=$1)) WHERE id=$1`, j.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `ALTER TABLE admin_business_notice_events DISABLE TRIGGER admin_business_event_immutable`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Exec(ctx, `UPDATE admin_business_notice_events SET mode='2' WHERE job_id=$1`, j.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleJob(ctx, j.ID, j.Token, r); !errors.Is(err, businessnotice.ErrConflict) || sender.calls.Load() != 0 || r.calls.Load() != 0 {
		t.Fatal("modified event header reached sender", err)
	}
	check, err := s.Check(ctx)
	if err != nil || check.InvalidEvents != 1 {
		t.Fatal("tampered header not diagnosed", check, err)
	}
}

func TestAdminBusiness83DisabledSourceRemainsUnscheduledAfterLaterEnable(t *testing.T) {
	p := businessPool(t)
	ctx := context.Background()
	payments, err := payment.NewWithKey(p, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err = payments.CreateMerchant(ctx, payment.Merchant{ID: 8301, Name: "disabled83", APIKey: "synthetic-disabled83"}); err != nil {
		t.Fatal(err)
	}
	service := backoffice.New(p, payments)
	if _, err = service.Create(ctx, backoffice.Principal{Role: "merchant", SubjectID: 8301, Username: "owner8301"}, "domains", rawFields(`{"domain":"disabled83.example.test","enabled":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"msgconfig_domain","value":"1"}')`); err != nil {
		t.Fatal(err)
	}
	items, err := payments.AdminBusinessNotices().Events(ctx, 0, 20)
	if err != nil || len(items) != 1 || items[0].Mode != "0" || items[0].JobID != 0 || items[0].State != "not_scheduled" {
		t.Fatal("disabled historical source was scheduled", items, err)
	}
	var count int
	if err = p.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind='admin.business-notice'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("enable backfilled old job", count, err)
	}
}
