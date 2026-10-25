package businessnotice_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/backoffice"
	"goepay/internal/businessnotice"
	"goepay/internal/merchantnotice"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
)

type businessSender83 struct {
	pool    *pgxpool.Pool
	calls   atomic.Int32
	err     error
	cancel  context.CancelFunc
	message merchantnotice.Message
	kind    merchantnotice.Transport
	account string
}

func (s *businessSender83) NoticeTransport() merchantnotice.Transport {
	if s.kind != "" {
		return s.kind
	}
	return merchantnotice.SMTP
}
func (s *businessSender83) NoticeScope() string     { return "synthetic-account83" }
func (s *businessSender83) NoticeAccountID() string { return s.account }
func (s *businessSender83) ValidateNoticeRecipient(to string) error {
	if to == "" {
		return errors.New("empty")
	}
	return nil
}
func (s *businessSender83) SendMerchantNotice(ctx context.Context, to string, m merchantnotice.Message) error {
	var count int
	if err := s.pool.QueryRow(context.Background(), `SELECT count(*) FROM admin_business_notice_claims`).Scan(&count); err != nil || count != 1 {
		return errors.New("claim not committed before I/O")
	}
	s.calls.Add(1)
	s.message = m
	if s.cancel != nil {
		s.cancel()
	}
	return s.err
}

type businessResolver83 struct {
	sender        *businessSender83
	calls         atomic.Int32
	mutate        bool
	expected      string
	mutateAccount bool
}

func (r *businessResolver83) BusinessNoticeAccountID(context.Context, merchantnotice.Transport) (string, error) {
	return r.expected, nil
}

func (r *businessResolver83) ResolveBusinessNotice(_ context.Context, kind merchantnotice.Transport, a businessnotice.Account) (businessnotice.Route, error) {
	r.calls.Add(1)
	if r.mutateAccount {
		r.expected = r.sender.account
	}
	if r.mutate {
		a.Input.Params["domain"] = "factory-injected83"
	}
	return businessnotice.Route{Sender: r.sender, Recipient: "admin83@example.test"}, nil
}

func delivery83(t *testing.T, selected ...string) (*businessnotice.Store, *pgxpool.Pool, jobs.Job) {
	t.Helper()
	p := businessPool(t)
	ctx := context.Background()
	s, err := payment.NewWithKey(p, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.CreateMerchant(ctx, payment.Merchant{ID: 8301, Name: "delivery83", APIKey: "synthetic-delivery83"}); err != nil {
		t.Fatal(err)
	}
	mode := "1"
	if len(selected) > 0 {
		mode = selected[0]
	}
	if _, err = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key','msgconfig_domain','value',$1::text)),('settings','{"key":"mail_recv","value":"admin83@example.test"}')`, mode); err != nil {
		t.Fatal(err)
	}
	service := backoffice.New(p, s)
	if _, err = service.Create(ctx, backoffice.Principal{Role: "merchant", SubjectID: 8301, Username: "owner8301"}, "domains", rawFields(`{"domain":"original83.example.test","enabled":true}`)); err != nil {
		t.Fatal(err)
	}
	j, err := (jobs.Queue{Pool: p}).ClaimProviderChannels(ctx, []string{businessnotice.JobKind}, time.Minute, []int64{999999})
	if err != nil {
		t.Fatal(err)
	}
	return s.AdminBusinessNotices(), p, j
}

func TestAdminBusiness83QQFactoryCannotReplaceIndependentExpectedApp(t *testing.T) {
	s, p, j := delivery83(t, "5")
	sender := &businessSender83{pool: p, kind: merchantnotice.QQBot, account: "wrong-app83"}
	r := &businessResolver83{sender: sender, expected: "original-app83", mutateAccount: true}
	if err := s.HandleJob(context.Background(), j.ID, j.Token, r); !errors.Is(err, businessnotice.ErrConflict) || sender.calls.Load() != 0 {
		t.Fatal("factory substituted independently selected QQ app", err, sender.calls.Load())
	}
}

func TestAdminBusiness83ConcurrentDeliveryClaimsBeforeIOAndUnknownNeverResends(t *testing.T) {
	s, p, j := delivery83(t)
	sender := &businessSender83{pool: p, err: errors.New("synthetic lost ACK")}
	resolver := &businessResolver83{sender: sender, mutate: true}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- s.HandleJob(context.Background(), j.ID, j.Token, resolver) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if sender.calls.Load() != 1 || resolver.calls.Load() != 1 {
		t.Fatal("not one permanent sending attempt", sender.calls.Load(), resolver.calls.Load())
	}
	check, checkErr := s.Check(context.Background())
	if checkErr != nil || check.InvalidEvents != 0 || check.InvalidDeliveries != 0 || check.UnknownDeliveries != 1 {
		t.Fatal("unknown delivery integrity mismatch", check, checkErr)
	}
	if !strings.Contains(sender.message.HTML, "original83.example.test") || strings.Contains(sender.message.HTML, "factory-injected83") {
		t.Fatal("factory changed rendered original message")
	}
	views, err := s.Events(context.Background(), 0, 20)
	if err != nil || len(views) != 1 || views[0].State != "unknown" {
		t.Fatal(views, err)
	}
	if _, err = p.Exec(context.Background(), `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"5"') WHERE resource='settings' AND body->>'key'='msgconfig_domain'`); err != nil {
		t.Fatal(err)
	}
	if err = s.HandleJob(context.Background(), j.ID, j.Token, resolver); err != nil || sender.calls.Load() != 1 || resolver.calls.Load() != 1 {
		t.Fatal("changed configuration resurrected send", err)
	}
}

func TestAdminBusiness83ChangedModePermanentlyStopsWithoutResolvingCredentials(t *testing.T) {
	s, p, j := delivery83(t)
	resolver := &businessResolver83{sender: &businessSender83{pool: p}}
	if _, err := p.Exec(context.Background(), `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"2"') WHERE resource='settings' AND body->>'key'='msgconfig_domain'`); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleJob(context.Background(), j.ID, j.Token, resolver); err != nil {
		t.Fatal(err)
	}
	if resolver.calls.Load() != 0 {
		t.Fatal("changed original mode read credentials")
	}
	if _, err := p.Exec(context.Background(), `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"1"') WHERE resource='settings' AND body->>'key'='msgconfig_domain'`); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleJob(context.Background(), j.ID, j.Token, resolver); err != nil {
		t.Fatal(err)
	}
	views, err := s.Events(context.Background(), 0, 20)
	if err != nil || len(views) != 1 || views[0].State != "not_sent" || resolver.calls.Load() != 0 {
		t.Fatal(views, err)
	}
}

func TestAdminBusiness83LateACKSurvivesCanceledJobContext(t *testing.T) {
	s, p, j := delivery83(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sender := &businessSender83{pool: p, cancel: cancel}
	r := &businessResolver83{sender: sender}
	if err := s.HandleJob(ctx, j.ID, j.Token, r); err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(context.Background(), 0, 20)
	if err != nil || len(events) != 1 || events[0].State != "accepted" {
		t.Fatal(events, err)
	}
}

func TestAdminBusiness83MissingACKAndReplacementJobNeverAcquireSecondSend(t *testing.T) {
	s, p, j := delivery83(t)
	ctx := context.Background()
	sender := &businessSender83{pool: p}
	r := &businessResolver83{sender: sender}
	if err := s.HandleJob(ctx, j.ID, j.Token, r); err != nil {
		t.Fatal(err)
	}
	// Simulate loss of both response evidence records in this disposable schema.
	if _, err := p.Exec(ctx, `ALTER TABLE admin_business_notice_results DISABLE TRIGGER admin_business_result_immutable; ALTER TABLE backoffice_audit DISABLE TRIGGER backoffice_audit_immutable; DELETE FROM admin_business_notice_results; DELETE FROM backoffice_audit WHERE resource='admin-business-notice-deliveries' AND operation='result'`); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleJob(ctx, j.ID, j.Token, r); err != nil || sender.calls.Load() != 1 || r.calls.Load() != 1 {
		t.Fatal("lost ACK released permanent claim", err)
	}
	var replacement int64
	if err := p.QueryRow(ctx, `INSERT INTO jobs(event_key,kind,payload,state,lease_token,lease_until) VALUES('synthetic-replacement83','admin.business-notice','{"event_id":"1"}','running',$1,now()+interval '1 minute') RETURNING id`, j.Token).Scan(&replacement); err != nil {
		t.Fatal(err)
	}
	if err := s.HandleJob(ctx, replacement, j.Token, r); !errors.Is(err, businessnotice.ErrState) || sender.calls.Load() != 1 {
		t.Fatal("replacement job acquired original authority", err)
	}
	check, err := s.Check(ctx)
	if err != nil || check.UnknownDeliveries != 1 || check.InvalidEvents != 1 || check.InvalidDeliveries != 0 {
		t.Fatal("unknown/orphan job diagnostic", check, err)
	}
}
