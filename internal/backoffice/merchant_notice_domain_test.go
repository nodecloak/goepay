package backoffice

import (
	"context"
	"errors"
	"testing"

	notice "goepay/internal/merchantnotice"
)

func TestMerchantNotice80DomainReviewHasFreshAdminAuditAndDoesNotReplaySameDecision(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"owner80@example.test"}'::jsonb WHERE id=101`); err != nil {
		t.Fatal(err)
	}
	ns, err := notice.NewStore(s.pool, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	owner := notice.Actor{Role: "merchant", ID: 101, Name: "merchant"}
	if _, err = ns.SaveSettings(ctx, 101, 0, notice.SettingsWrite{Preferences: notice.Preferences{DomainAudit: 2}}, owner); err != nil {
		t.Fatal(err)
	}
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "merchant"}
	item, err := s.Create(ctx, merchant, "domains", input(`{"domain":"original80.example.com"}`))
	if err != nil {
		t.Fatal(err)
	}
	reviewer := Principal{Role: "admin", AdminClass: "operator", SubjectID: 1, Username: "admin"}
	if _, err = s.Update(ctx, Principal{Role: "admin", AdminClass: "auditor", SubjectID: 2, Username: "audit-only"}, "domains", item.ID, input(`{"approved":true}`)); !errors.Is(err, ErrForbidden) {
		t.Fatal("read-only administrator approved a domain", err)
	}
	if _, err = s.Update(ctx, merchant, "domains", item.ID, input(`{"approved":true}`)); !errors.Is(err, ErrForbidden) {
		t.Fatal("merchant approved own domain", err)
	}
	if _, err = s.Update(ctx, reviewer, "domains", item.ID, input(`{"approved":true}`)); err != nil {
		t.Fatal(err)
	}
	first, err := ns.Events(ctx, 101, 0, 20, owner)
	if err != nil || len(first) != 1 || first[0].Scene != "domain_audit" || len(first[0].Deliveries) != 1 {
		t.Fatal("missing or duplicate review notification", first, err)
	}
	if _, err = s.Update(ctx, reviewer, "domains", item.ID, input(`{"approved":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, merchant, "domains", item.ID, input(`{"domain":"renamed80.example.com"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Update(ctx, reviewer, "domains", item.ID, input(`{"approved":false}`)); err != nil {
		t.Fatal(err)
	}
	events, err := ns.Events(ctx, 101, 0, 20, owner)
	if err != nil || len(events) != 2 {
		t.Fatal("same decision repeated or explicit rejection lost", events, err)
	}
	var audit int64
	if err = s.pool.QueryRow(ctx, `SELECT min(id) FROM backoffice_audit WHERE resource='domains' AND notice_source_transaction IS NOT NULL`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if err = ns.CaptureDomainAudit(ctx, tx, audit); !errors.Is(err, notice.ErrConflict) {
		t.Fatal("historical domain review backfilled", err)
	}
	tx.Rollback(ctx)
	if err = s.Delete(ctx, reviewer, "domains", item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = ns.Events(ctx, 101, 0, 20, owner); err != nil {
		t.Fatal("mutable hostname/deletion invalidated immutable review history", err)
	}
}
