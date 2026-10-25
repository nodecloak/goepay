package payment

import (
	"context"
	"errors"
	"testing"
)

func TestAdminNotice77ImportedTargetMustBeAStringAndCannotSilentlyClearASelectedRecipient(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	transferSetting(t, s, "mail_recv", "admin@example.test")
	for _, raw := range []string{`null`, `123`, `{}`, `[]`} {
		if _, e := s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}',$1::jsonb) WHERE resource='settings' AND body->>'key'='mail_recv'`, raw); e != nil {
			t.Fatal(e)
		}
		if _, e := s.AdminChannelNoticeSettings(ctx); !errors.Is(e, ErrInvalid) {
			t.Fatal("malformed imported recipient became a mail fallback", raw, e)
		}
	}
}
