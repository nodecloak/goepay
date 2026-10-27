package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"goepay/internal/platform/jobs"
	"goepay/internal/refundevent201"
)

const recoveryEndpoint201 = "https://events.example.invalid/recovery201"
const recoveryVerifiedEmail201 = "verified201@example.test"

// Seed an existing verified registration claim with the enrollment fixture's
// real HMAC domain. An ordinary profile email remains a separate, unverified
// value; these tests never send external email or use a production account.
func recoveryVerifiedContact201(t *testing.T, f *eventFixture201) {
	t.Helper()
	master := hmac.New(sha256.New, make([]byte, 32))
	master.Write([]byte("goepay/public-enrollment/v1"))
	digest := hmac.New(sha256.New, master.Sum(nil))
	digest.Write([]byte("email\x00" + recoveryVerifiedEmail201 + "\x00"))
	if _, err := f.p.Exec(t.Context(), `INSERT INTO enrollment_emails(email,email_hash,merchant_id,username)VALUES($1,$2,1,'events201')`, recoveryVerifiedEmail201, digest.Sum(nil)); err != nil {
		t.Fatal(err)
	}
}

func recoveryResultFailureOnce201(t *testing.T, f *eventFixture201) {
	t.Helper()
	// Sequence advances survive the failed result transaction, so the first
	// result INSERT fails while the recovery transaction can persist unknown.
	if _, err := f.p.Exec(t.Context(), `CREATE SEQUENCE refund201_result_failure_once;
CREATE FUNCTION reject_refund201_result_once() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF nextval('refund201_result_failure_once')=1 THEN RAISE EXCEPTION 'synthetic-sensitive-result-write201'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER reject_refund201_result_once BEFORE INSERT ON merchant_refund_event_results201 FOR EACH ROW EXECUTE FUNCTION reject_refund201_result_once()`); err != nil {
		t.Fatal(err)
	}
}

func TestRefundEvent201ResultWriteFailureRecoversWithoutResendingAndCountsOnce(t *testing.T) {
	for _, priorFailures := range []int{0, 9} {
		t.Run(fmt.Sprintf("prior-failures-%d", priorFailures), func(t *testing.T) {
			f := eventLocal201(t)
			if priorFailures == 9 {
				recoveryVerifiedContact201(t, f)
			}
			f.save(t, recoveryEndpoint201, []string{"refund"})
			o := f.paidOrder(t, fmt.Sprintf("persist-result-%d", priorFailures))
			for i := 0; i < priorFailures; i++ {
				f.manual(t, o, fmt.Sprintf("persist-prior-%d", i), 1)
				f.deliver(t, f.claim(t), func(*http.Request) (*http.Response, error) { return eventResponse201(200, "receiver rejected"), nil })
			}
			if got := f.view(t).ConsecutiveFailures; got != priorFailures {
				t.Fatal("independent failure fixture not established", got)
			}
			f.manual(t, o, "result-persistence-loss", 1)
			j := f.claim(t)
			recoveryResultFailureOnce201(t, f)
			var calls atomic.Int32
			client := &http.Client{Transport: eventTransport201(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				return eventResponse201(200, "receiver rejected"), nil
			})}
			err := f.s.Store.DeliverMerchantRefundEvent201(t.Context(), j, f.key, client)
			if err == nil {
				t.Fatal("sent event whose result write failed returned success; worker would permanently complete the unresolved job")
			}
			var pgerr *pgconn.PgError
			if errors.As(err, &pgerr) || strings.Contains(err.Error(), "synthetic-sensitive") || strings.Contains(err.Error(), "SQLSTATE") {
				t.Fatal("recoverable event result failure leaked private SQL details", err)
			}
			var claimCount, resultCount int
			var queueState string
			if err = f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM merchant_refund_event_claims201),(SELECT count(*)FROM merchant_refund_event_results201),(SELECT state FROM jobs WHERE id=$1)`, j.ID).Scan(&claimCount, &resultCount, &queueState); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 || claimCount != priorFailures+1 || resultCount != priorFailures || queueState != "running" {
				t.Fatal("failed result write lost original durable send claim", calls.Load(), claimCount, resultCount, queueState)
			}
			queue := jobs.Queue{Pool: f.p}
			if err = queue.Retry(t.Context(), j, "fixed recoverable event result failure", 0); err != nil {
				t.Fatal(err)
			}
			recovered, err := queue.Claim(t.Context(), []string{refundevent201.JobKind}, time.Minute)
			if err != nil || recovered.ID != j.ID || recovered.Attempts != j.Attempts+1 || recovered.Token == j.Token {
				t.Fatal("recovery did not claim original event with a fresh lease", recovered, err)
			}
			if err = f.s.Store.DeliverMerchantRefundEvent201(t.Context(), recovered, f.key, client); err != nil {
				t.Fatal("durable original claim could not settle unknown without more HTTP", err)
			}
			if err = queue.Complete(t.Context(), recovered); err != nil {
				t.Fatal(err)
			}
			state := f.view(t)
			wantFailures := priorFailures + 1
			if priorFailures == 9 {
				wantFailures = 0
			}
			if calls.Load() != 1 || state.LastDeliveryState != "unknown" || state.ConsecutiveFailures != wantFailures || state.EventURL != recoveryEndpoint201 {
				t.Fatal("recovered unknown resent HTTP, missed independent failure, or changed URL", calls.Load(), state)
			}
			var notices, noticeJobs int
			if err = f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM merchant_refund_event_closed_notices201),(SELECT count(*)FROM jobs WHERE kind=$1),(SELECT count(*)FROM merchant_refund_event_results201)`, refundevent201.NoticeJobKind).Scan(&notices, &noticeJobs, &resultCount); err != nil {
				t.Fatal(err)
			}
			if resultCount != priorFailures+1 {
				t.Fatal("recovered original event must have exactly one persisted result", resultCount)
			}
			if priorFailures == 9 {
				if len(state.EventTypes) != 0 || state.SubscriptionNotice == "" || notices != 1 || noticeJobs != 1 {
					t.Fatal("tenth independent failure did not close only types and capture one verified closure notice", state, notices, noticeJobs)
				}
			} else if len(state.EventTypes) != 1 || notices != 0 || noticeJobs != 0 {
				t.Fatal("first failure incorrectly closed subscription", state, notices, noticeJobs)
			}
			_ = f.s.Store.DeliverMerchantRefundEvent201(t.Context(), recovered, f.key, client)
			if calls.Load() != 1 || f.view(t).ConsecutiveFailures != wantFailures {
				t.Fatal("completed recovery replay sent again or counted the same event twice", calls.Load())
			}
			if _, err = queue.Claim(t.Context(), []string{refundevent201.JobKind}, time.Minute); !jobs.IsEmpty(err) {
				t.Fatal("recovered unknown scheduled another automatic event send", err)
			}
		})
	}
}

func recoveryClosedNotice201(t *testing.T, verified bool) (*eventFixture201, jobs.Job) {
	t.Helper()
	f := eventLocal201(t)
	if _, err := f.p.Exec(t.Context(), `UPDATE merchants SET profile=profile||'{"email":"profile-only201@example.test"}'::jsonb WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if verified {
		recoveryVerifiedContact201(t, f)
	}
	f.save(t, recoveryEndpoint201, []string{"refund"})
	o := f.paidOrder(t, "closure-recovery201")
	for i := 1; i <= 10; i++ {
		f.manual(t, o, fmt.Sprintf("closure-independent-%d", i), 1)
		f.deliver(t, f.claim(t), func(*http.Request) (*http.Response, error) { return eventResponse201(200, "receiver rejected"), nil })
	}
	state := f.view(t)
	if len(state.EventTypes) != 0 || state.EventURL != recoveryEndpoint201 || state.SubscriptionNotice == "" {
		t.Fatal("ten actual independent failures did not close subscription", state)
	}
	job, err := (jobs.Queue{Pool: f.p}).Claim(t.Context(), []string{refundevent201.NoticeJobKind}, time.Minute)
	if !verified {
		if !jobs.IsEmpty(err) {
			t.Fatal("unverified profile email created a claimable closure mail job", job, err)
		}
		return f, jobs.Job{}
	}
	if err != nil {
		t.Fatal("verified closure notice lacks its original durable job", err)
	}
	return f, job
}

type recoveryNoticeSender201 struct {
	entered  chan struct{}
	release  chan struct{}
	once     sync.Once
	mu       sync.Mutex
	messages []struct {
		to      string
		message refundevent201.NoticeMessage
	}
	result error
}

func (*recoveryNoticeSender201) NoticeScope() string { return "synthetic-managed195-scope201" }
func (*recoveryNoticeSender201) ValidateNoticeRecipient(to string) error {
	if to != recoveryVerifiedEmail201 {
		return errors.New("unexpected closure recipient")
	}
	return nil
}
func (s *recoveryNoticeSender201) SendRefundEventSubscriptionClosed(ctx context.Context, to string, message refundevent201.NoticeMessage) error {
	s.mu.Lock()
	s.messages = append(s.messages, struct {
		to      string
		message refundevent201.NoticeMessage
	}{to, message})
	s.mu.Unlock()
	if s.entered != nil {
		s.once.Do(func() { close(s.entered) })
	}
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.result
}
func (s *recoveryNoticeSender201) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.messages)
}

func TestRefundEvent201ClosedNoticeConcurrentAndUnknownSMTPNeverSendTwice(t *testing.T) {
	for _, outcome := range []string{"accepted", "unknown"} {
		t.Run(outcome, func(t *testing.T) {
			f, j := recoveryClosedNotice201(t, true)
			sender := &recoveryNoticeSender201{entered: make(chan struct{}), release: make(chan struct{})}
			if outcome == "unknown" {
				sender.result = errors.New("synthetic SMTP acceptance unknown201")
			}
			var release sync.Once
			unblock := func() { release.Do(func() { close(sender.release) }) }
			defer unblock()
			first := make(chan error, 1)
			ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
			defer cancel()
			go func() { first <- f.s.Store.DeliverMerchantRefundEventClosedNotice201(ctx, j, sender) }()
			select {
			case <-sender.entered:
			case err := <-first:
				t.Fatal("first closure delivery never reached SMTP boundary", err)
			case <-ctx.Done():
				t.Fatal("closure delivery did not reach SMTP before deadline")
			}
			secondCtx, secondCancel := context.WithTimeout(t.Context(), 2*time.Second)
			err := f.s.Store.DeliverMerchantRefundEventClosedNotice201(secondCtx, j, sender)
			secondCancel()
			unblock()
			if err != nil {
				t.Fatal("concurrent original lease failed to recognize durable once claim", err)
			}
			if err = <-first; err != nil {
				t.Fatal(err)
			}
			if err = (jobs.Queue{Pool: f.p}).Complete(t.Context(), j); err != nil {
				t.Fatal(err)
			}
			_ = f.s.Store.DeliverMerchantRefundEventClosedNotice201(t.Context(), j, sender)
			var claims, results int
			if err = f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM merchant_refund_event_notice_claims201),(SELECT count(*)FROM merchant_refund_event_notice_results201)`).Scan(&claims, &results); err != nil {
				t.Fatal(err)
			}
			if sender.count() != 1 || claims != 1 || results != 1 {
				t.Fatal("concurrent or unknown closure delivery replayed SMTP", sender.count(), claims, results)
			}
			sender.mu.Lock()
			message := sender.messages[0]
			sender.mu.Unlock()
			if message.to != recoveryVerifiedEmail201 || !message.message.Valid() || !strings.Contains(message.message.Text, recoveryEndpoint201) || !strings.Contains(message.message.Text, "连续请求失败10次") {
				t.Fatal("closure delivery lost verified recipient or sealed message")
			}
		})
	}
}

func TestRefundEvent201ClosedNoticeExpiredLeaseCannotReachSMTP(t *testing.T) {
	for _, when := range []string{"before-claim", "during-claim-insert"} {
		t.Run(when, func(t *testing.T) {
			f, j := recoveryClosedNotice201(t, true)
			if when == "before-claim" {
				if _, err := f.p.Exec(t.Context(), `UPDATE jobs SET lease_until=clock_timestamp()-interval '1 second' WHERE id=$1`, j.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := f.p.Exec(t.Context(), `CREATE FUNCTION expire_refund201_notice_lease()RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN UPDATE jobs SET lease_until=clock_timestamp()-interval '1 second'WHERE id=NEW.job_id;RETURN NEW;END $$;CREATE TRIGGER expire_refund201_notice_lease BEFORE INSERT ON merchant_refund_event_notice_claims201 FOR EACH ROW EXECUTE FUNCTION expire_refund201_notice_lease()`); err != nil {
					t.Fatal(err)
				}
			}
			sender := &recoveryNoticeSender201{}
			err := f.s.Store.DeliverMerchantRefundEventClosedNotice201(t.Context(), j, sender)
			if !errors.Is(err, jobs.ErrLeaseLost) {
				t.Fatal("expired closure job lease admitted SMTP claim", err)
			}
			var claims int
			if err = f.p.QueryRow(t.Context(), `SELECT count(*)FROM merchant_refund_event_notice_claims201`).Scan(&claims); err != nil {
				t.Fatal(err)
			}
			if sender.count() != 0 || claims != 0 {
				t.Fatal("expired closure lease reached SMTP or left a permanent send claim", sender.count(), claims)
			}
		})
	}
}

func TestRefundEvent201UnverifiedProfileEmailNeverBecomesClosureRecipient(t *testing.T) {
	f, _ := recoveryClosedNotice201(t, false)
	var notices, claims, mailJobs int
	if err := f.p.QueryRow(t.Context(), `SELECT(SELECT count(*)FROM merchant_refund_event_closed_notices201),(SELECT count(*)FROM merchant_refund_event_notice_claims201),(SELECT count(*)FROM jobs WHERE kind=$1)`, refundevent201.NoticeJobKind).Scan(&notices, &claims, &mailJobs); err != nil {
		t.Fatal(err)
	}
	if notices != 1 || claims != 0 || mailJobs != 0 {
		t.Fatal("profile-only email created an outbound closure notice instead of retaining the account warning", notices, claims, mailJobs)
	}
}
