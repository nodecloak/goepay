package merchantnotice_test

import (
	"context"
	"encoding/json"
	"errors"
	notice "goepay/internal/merchantnotice"
	"strings"
	"testing"
)

func TestMerchantNotice80EventListIsOwnerScopedReadOnlyAndVerifiesOriginalSource(t *testing.T) {
	s, pool := profileStore80(t)
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('notice-owner80','merchant',1000,'Local','synthetic-only80')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSettings(ctx, 1000, 0, notice.SettingsWrite{Preferences: notice.Preferences{Login: 2}}, owner80); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var audit int64
	if err = tx.QueryRow(ctx, `INSERT INTO console_login_audit(username,role,ip,success,subject_id,auth_version,source_transaction) VALUES('notice-owner80','merchant','192.0.2.80',true,1000,1,pg_current_xact_id()) RETURNING id`).Scan(&audit); err != nil {
		t.Fatal(err)
	}
	if err = s.CaptureLoginAudit(ctx, tx, audit); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(ctx, 1000, 0, 20, owner80)
	if err != nil || len(events) != 1 || events[0].Scene != "login" || events[0].State != "queued" || len(events[0].Deliveries) != 1 || events[0].Deliveries[0].Transport != notice.SMTP {
		t.Fatal("original event view unavailable", events, err)
	}
	raw, _ := json.Marshal(events)
	if strings.Contains(string(raw), "owner80@example.test") || strings.Contains(string(raw), "192.0.2.80") {
		t.Fatal("event list leaked recipient or source input", string(raw))
	}
	if _, err = s.Events(ctx, 1000, 0, 20, notice.Actor{Role: "merchant", ID: 1001, Name: "other80"}); !errors.Is(err, notice.ErrForbidden) {
		t.Fatal("cross-owner list allowed", err)
	}
	var claims int
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM merchant_notice_claims`).Scan(&claims); err != nil || claims != 0 {
		t.Fatal("read caused send claim", claims, err)
	}
	if _, err = pool.Exec(ctx, `ALTER TABLE merchant_notice_events DISABLE TRIGGER USER;UPDATE merchant_notice_events SET payload_cipher=decode(repeat('ab',40),'hex');ALTER TABLE merchant_notice_events ENABLE TRIGGER USER`); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Events(ctx, 1000, 0, 20, owner80); !errors.Is(err, notice.ErrConflict) {
		t.Fatal("damaged source displayed as queued", err)
	}
}
