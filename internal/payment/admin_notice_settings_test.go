package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
)

func TestAdminNotice77ReadsOnlyOriginalNonsecretUniqueSettingsAndRejectsImportedAmbiguity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for key, value := range map[string]string{"msgconfig_risk": "2", "msgtelegram_chatid": "-12345", "qqbot_openid": "local77", "mail_recv": "admin@example.test", "mail_name": "fallback@example.test"} {
		transferSetting(t, s, key, value)
	}
	// Protected profiles must never be taken from legacy plaintext settings.
	transferSetting(t, s, "login_telegram_token", "must-never-consume77")
	transferSetting(t, s, "msgrobot_url", "https://evil.invalid/?secret=must-never-consume77")
	v, e := s.AdminChannelNoticeSettings(ctx)
	if e != nil || len(v) != 5 || v["msgconfig_risk"] != "2" || v["msgtelegram_chatid"] != "-12345" {
		t.Fatal("original nonsecret selector unavailable", v, e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,owner_id,body)SELECT resource,owner_id,body FROM backoffice_records WHERE resource='settings' AND body->>'key'='msgconfig_risk'`); e == nil {
		t.Fatal("normal database accepted a duplicate selector")
	} else {
		var constraint *pgconn.PgError
		if !errors.As(e, &constraint) || constraint.Code != "23505" {
			t.Fatal("fixture did not reach original unique index", e)
		}
		// The normal unique index prevents ambiguous writes already. Exercise
		// the reader against a deliberately drifted imported test schema too.
		if _, e = s.pool.Exec(ctx, `DROP INDEX backoffice_setting_unique; INSERT INTO backoffice_records(resource,owner_id,body)SELECT resource,owner_id,body FROM backoffice_records WHERE resource='settings' AND body->>'key'='msgconfig_risk'`); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.AdminChannelNoticeSettings(ctx); !errors.Is(e, ErrInvalid) {
		t.Fatal("ambiguous setting selected an arbitrary transport", e)
	}
}
