package payment

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	notice "goepay/internal/merchantnotice"
	"goepay/internal/platform/jobs"
)

func scheduler171PaymentStopSource(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	a := scheduler171Admin(t, s)
	scheduler171NotifyOrder(t, s, "mail-stop", 1)
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"original171@example.test"}'::jsonb WHERE id=901`); e != nil {
		t.Fatal(e)
	}
	for key, value := range map[string]string{"auto_check_notify": "1", "check_notify_count": "1", "check_notify_notice": "1"} {
		transferSetting(t, s, key, value)
	}
	scheduler171ConfigureManaged(t, s, a, "notify", true)
	scheduler171Pending(t, s, a, "notify")
	if tick, e := s.RunScheduledTasksOnce(ctx, 20); e != nil || tick.Completed != 1 {
		t.Fatal(tick, e)
	}
	q := jobs.Queue{Pool: s.pool, Policy: s}
	j, e := q.Claim(ctx, []string{"payment.notify"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	// Synthetic outcome fixture through the actual original job lease; the
	// preceding notification package and reconciliation tests prove classification
	// from real local HTTP and rejection of non-delivery errors.
	if e = q.RetryNotificationFailure(ctx, j, "ack", 0); e != nil {
		t.Fatal(e)
	}
	var pay int
	if e = s.pool.QueryRow(ctx, `SELECT pay FROM merchants WHERE id=901`).Scan(&pay); e != nil || pay != 0 {
		t.Fatal("original stop source not created", pay, e)
	}
}

type scheduler171PermissionSender struct {
	target, scope string
	client        *http.Client
	calls         atomic.Int32
}

func (*scheduler171PermissionSender) NoticeTransport() notice.Transport { return notice.SMTP }
func (s *scheduler171PermissionSender) NoticeScope() string             { return s.scope }
func (*scheduler171PermissionSender) ValidateNoticeRecipient(to string) error {
	if to != "original171@example.test" {
		return fmt.Errorf("wrong original recipient")
	}
	return nil
}
func (s *scheduler171PermissionSender) SendMerchantNotice(ctx context.Context, to string, message notice.Message) error {
	s.calls.Add(1)
	raw, _ := json.Marshal(message)
	r, e := http.NewRequestWithContext(ctx, "POST", s.target, bytes.NewReader(raw))
	if e != nil {
		return e
	}
	r.Header.Set("X-Synthetic-Recipient", to)
	response, e := s.client.Do(r)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	if response.StatusCode != 204 {
		return fmt.Errorf("synthetic unknown outcome")
	}
	return nil
}
func TestScheduler171PaymentPermissionMailConsumesOriginalPrivateSourceOnce(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scheduler171PaymentStopSource(t, s)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Synthetic-Recipient") != "original171@example.test" {
			t.Error("adopted current or different merchant target")
		}
		var message notice.Message
		if e := json.NewDecoder(r.Body).Decode(&message); e != nil || message.Title == "" || message.Text == "" {
			t.Error("missing actual message", e)
		}
		w.WriteHeader(204)
	}))
	defer sink.Close()
	sender := &scheduler171PermissionSender{target: sink.URL, client: sink.Client(), scope: "synthetic-original-smtp"}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"email":"changed171@example.test"}'::jsonb WHERE id=901`); e != nil {
		t.Fatal(e)
	}
	q := jobs.Queue{Pool: s.pool, Policy: s}
	j, e := q.ClaimProviderChannels(ctx, []string{"merchant.payment-disabled-notice"}, time.Minute, []int64{7})
	if e != nil {
		t.Fatal("original worker channel filter lost permission mail", e)
	}
	for range 2 {
		if e = s.DeliverScheduledPaymentDisabledNotice(ctx, j.ID, j.Token, sender); e != nil {
			t.Fatal("original permission notice not consumed", e)
		}
	}
	if sender.calls.Load() != 1 {
		t.Fatal("original mail source was not consumed exactly once", sender.calls.Load())
	}
	if e = q.Complete(ctx, j); e != nil {
		t.Fatal(e)
	}
	var facts string
	if e = s.pool.QueryRow(ctx, `SELECT facts::text FROM scheduled_notification_stops`).Scan(&facts); e != nil || bytes.Contains([]byte(facts), []byte("original171@example.test")) {
		t.Fatal("mail target stored in plaintext", e)
	}
}
func TestScheduler171PaymentPermissionMailUnknownNeverResendsOrAdoptsAccount(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	scheduler171PaymentStopSource(t, s)
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer sink.Close()
	sender := &scheduler171PermissionSender{target: sink.URL, client: sink.Client(), scope: "synthetic-first-smtp"}
	q := jobs.Queue{Pool: s.pool, Policy: s}
	j, e := q.Claim(ctx, []string{"merchant.payment-disabled-notice"}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.DeliverScheduledPaymentDisabledNotice(ctx, j.ID, j.Token, sender); e == nil || sender.calls.Load() != 1 {
		t.Fatal("uncertain first submission was not retained", sender.calls.Load(), e)
	}
	sender.scope = "synthetic-replacement-smtp"
	if e = s.DeliverScheduledPaymentDisabledNotice(ctx, j.ID, j.Token, sender); e == nil || sender.calls.Load() != 1 {
		t.Fatal("unknown mail resent with replacement account", sender.calls.Load(), e)
	}
}

func TestScheduler171PermissionMailAuditWaitAndLostFactsCannotResubmit(t *testing.T) {
	for _, mode := range []string{"audit-failure", "late-lease", "lost-claim-and-result"} {
		t.Run(mode, func(t *testing.T) {
			s := testStore(t)
			ctx := context.Background()
			scheduler171PaymentStopSource(t, s)
			sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
			defer sink.Close()
			sender := &scheduler171PermissionSender{target: sink.URL, client: sink.Client(), scope: "synthetic-unchanged-smtp"}
			q := jobs.Queue{Pool: s.pool, Policy: s}
			j, e := q.Claim(ctx, []string{"merchant.payment-disabled-notice"}, time.Minute)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "lost-claim-and-result" {
				if e = s.DeliverScheduledPaymentDisabledNotice(ctx, j.ID, j.Token, sender); e != nil {
					t.Fatal(e)
				}
				tx, e := s.pool.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer tx.Rollback(ctx)
				if _, e = tx.Exec(ctx, `ALTER TABLE scheduled_notification_mail_results DISABLE TRIGGER USER;ALTER TABLE scheduled_notification_mail_claims DISABLE TRIGGER USER;ALTER TABLE jobs DISABLE TRIGGER USER`); e != nil {
					t.Fatal(e)
				}
				if _, e = tx.Exec(ctx, `UPDATE jobs SET scheduled_notification_mail_head=NULL WHERE id=$1`, j.ID); e != nil {
					t.Fatal(e)
				}
				if _, e = tx.Exec(ctx, `DELETE FROM scheduled_notification_mail_results;DELETE FROM scheduled_notification_mail_claims;ALTER TABLE jobs ENABLE TRIGGER USER;ALTER TABLE scheduled_notification_mail_claims ENABLE TRIGGER USER;ALTER TABLE scheduled_notification_mail_results ENABLE TRIGGER USER`); e != nil {
					t.Fatal(e)
				}
				if e = tx.Commit(ctx); e != nil {
					t.Fatal(e)
				}
				if e = s.DeliverScheduledPaymentDisabledNotice(ctx, j.ID, j.Token, sender); e == nil || sender.calls.Load() != 1 {
					t.Fatal("retained original start audit lost to deleted private facts", sender.calls.Load(), e)
				}
				return
			}
			action := "RAISE EXCEPTION 'synthetic scheduler171 mail audit fault';"
			if mode == "late-lease" {
				action = "PERFORM pg_sleep(0.5);"
				if _, e = s.pool.Exec(ctx, `UPDATE jobs SET lease_until=clock_timestamp()+interval '250 milliseconds' WHERE id=$1`, j.ID); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = s.pool.Exec(ctx, `CREATE FUNCTION scheduler171_mail_audit_gate()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='scheduled-notification-mail' THEN `+action+`END IF;RETURN NEW;END$$;CREATE TRIGGER scheduler171_mail_audit_gate BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION scheduler171_mail_audit_gate()`); e != nil {
				t.Fatal(e)
			}
			if e = s.DeliverScheduledPaymentDisabledNotice(ctx, j.ID, j.Token, sender); e == nil || sender.calls.Load() != 0 {
				t.Fatal("uncommitted/expired claim reached SMTP", sender.calls.Load(), e)
			}
			var claims, head int
			if e = s.pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM scheduled_notification_mail_claims),COALESCE(scheduled_notification_mail_head,0)FROM jobs WHERE id=$1`, j.ID).Scan(&claims, &head); e != nil || claims != 0 || head != 0 {
				t.Fatal("original claim rollback incomplete", claims, head, e)
			}
		})
	}
}
