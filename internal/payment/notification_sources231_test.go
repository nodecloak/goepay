package payment

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/businessnotice"
	"goepay/internal/merchantnotice"
	"goepay/internal/noticesource231"
	"goepay/internal/platform/jobs"
	"net/http"
	"strings"
	"testing"
	"time"
)

type noticePayment231 struct {
	flowProvider
	account string
}

func TestChannelLimit231CrossingIncludesVerifiedHistoricalBaselineAndDeduplicates(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := &noticePayment231{account: "limit-mch"}
	if e := s.CreateMerchant(ctx, Merchant{ID: 23103, Name: "limit231", APIKey: "synthetic-limit-key"}); e != nil {
		t.Fatal(e)
	}
	_ = complaintVerifiedPaid231(t, s, 23103, "baseline-before-limits", p)
	g := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	// A previously accepted payment can settle after a limit is configured.
	// Its verified gross joins the old ledger baseline at callback time.
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 23103, OutTradeNo: "crossing-limits", Name: "synthetic pending", Type: "wxpay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.231", ""); e != nil {
		t.Fatal(e)
	}
	b, e := g.RuntimeChannel(7)
	if e != nil {
		t.Fatal(e)
	}
	policy := runtimeGlobalLimitPolicy{Channel: 7, Kind: b.kind, Identity: b.identity, Limits: parentGlobalLimits{DayTop: 200, DayMaxOrder: 2}, Revision: 1}
	policy.Proof, e = s.globalLimitProof(policy)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO runtime_channel_limits(channel_id,pay_type,identity_hash,daytop,daymaxorder,paymin,paymax,rate_limit_seconds,rate_limit_count,revision,proof_hash) VALUES($1,$2,$3,200,2,0,0,0,0,1,$4)`, policy.Channel, policy.Kind, policy.Identity, policy.Proof); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',1,'synthetic','runtime-channel-limits',7,'runtime-channel-limits-update','{}')`); e != nil {
		t.Fatal(e)
	}
	if e = g.HandleNotification(ctx, 7, o.TradeNo, nil, []byte("valid-signed-test-event")); e != nil {
		t.Fatal(e)
	}
	var n, events int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM notification_sources231 WHERE kind='channellimit'),(SELECT count(*) FROM admin_business_notice_events WHERE scene='channellimit')`).Scan(&n, &events); e != nil || n != 2 || events != 2 {
		t.Fatal("daily crossing missing", n, events, e)
	}
	if e = g.HandleNotification(ctx, 7, o.TradeNo, nil, []byte("valid-signed-test-event")); e != nil {
		t.Fatal(e)
	}
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM notification_sources231 WHERE kind='channellimit'`).Scan(&n); e != nil || n != 2 {
		t.Fatal("callback replay duplicated limit notice", n, e)
	}
	if check, e := s.business.Check(ctx); e != nil || check.InvalidEvents != 0 {
		t.Fatal("limit source integrity", check, e)
	}
}

type adminSender231 struct{ calls int }

func (*adminSender231) NoticeTransport() merchantnotice.Transport { return merchantnotice.SMTP }
func (*adminSender231) NoticeScope() string                       { return "synthetic-admin231" }
func (*adminSender231) ValidateNoticeRecipient(to string) error {
	if to != "admin@example.invalid" {
		return merchantnotice.ErrInvalid
	}
	return nil
}
func (s *adminSender231) SendMerchantNotice(_ context.Context, to string, m merchantnotice.Message) error {
	if !m.Valid() || to != "admin@example.invalid" {
		return merchantnotice.ErrInvalid
	}
	s.calls++
	return nil
}
func (s *adminSender231) ResolveBusinessNotice(_ context.Context, kind merchantnotice.Transport, _ businessnotice.Account) (businessnotice.Route, error) {
	return businessnotice.Route{Sender: s, Recipient: "admin@example.invalid"}, nil
}
func TestUnknownComplaint231AdministratorDeliveryHasNoInventedMerchantAndIsOneShot(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"msgconfig_complain_all","value":"1"}')`); e != nil {
		t.Fatal(e)
	}
	if e := s.upsertComplaint133(ctx, 7, adapter.ComplaintRecord{PayType: "wxpay", ThirdID: "unknown-admin231", Status: "PENDING", Times: 1, TradeNos: []string{"unmapped-platform-number"}}); e != nil {
		t.Fatal(e)
	}
	var mid *int64
	if e := s.pool.QueryRow(ctx, `SELECT merchant_id FROM admin_business_notice_events WHERE scene='complain_all'`).Scan(&mid); e != nil || mid != nil {
		t.Fatal("invented merchant", mid, e)
	}
	job, e := (jobs.Queue{Pool: s.pool}).Claim(ctx, []string{businessnotice.JobKind}, time.Minute)
	if e != nil {
		t.Fatal(e)
	}
	sender := &adminSender231{}
	if e = s.business.HandleJob(ctx, job.ID, job.Token, sender); e != nil {
		t.Fatal(e)
	}
	if e = s.business.HandleJob(ctx, job.ID, job.Token, sender); e != nil {
		t.Fatal(e)
	}
	if sender.calls != 1 {
		t.Fatal("administrator complaint resent", sender.calls)
	}
	if check, e := s.business.Check(ctx); e != nil || check.InvalidEvents != 0 || check.InvalidDeliveries != 0 || check.UnknownDeliveries != 0 {
		t.Fatal("administrator receipt integrity", check, e)
	}
	var modeGuard bool
	if e = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_constraint WHERE conrelid='admin_business_notice_events'::regclass AND conname='admin_business_mode231')`).Scan(&modeGuard); e != nil || !modeGuard {
		t.Fatal("final mode guard missing", e)
	}
	_, e = s.pool.Exec(ctx, `INSERT INTO admin_business_notice_events(merchant_id,scene,mode,audit_id,job_id,source_transaction,created_at,payload_cipher,body_hash,proof_hash) SELECT merchant_id,'channellimit','3',audit_id,job_id,source_transaction,created_at,payload_cipher,body_hash,proof_hash FROM admin_business_notice_events LIMIT 1`)
	var constraint *pgconn.PgError
	if !errors.As(e, &constraint) || constraint.Code != "23514" || constraint.ConstraintName != "admin_business_mode231" {
		t.Fatal("database accepted unsupported channel-limit SMS mode", e)
	}
}

func (p *noticePayment231) ApplicationID() string           { return "wx-source231" }
func (p *noticePayment231) TransferAccountIdentity() string { return p.account }
func (p *noticePayment231) VerifyNotification(ctx context.Context, h http.Header, raw []byte, expected adapter.ExpectedPayment) (adapter.PaymentEvent, error) {
	event, e := p.flowProvider.VerifyNotification(ctx, h, raw, expected)
	event.EventID = "source231-" + expected.TradeNo
	return event, e
}

func complaintVerifiedPaid231(t *testing.T, s *Store, mid int64, no string, p adapter.Provider) Order {
	t.Helper()
	ctx := context.Background()
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: no, Name: "synthetic complaint product", Type: "wxpay", Amount: 100})
	if e != nil {
		t.Fatal(e)
	}
	g := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}, AllowLive: true, CallbackBaseURL: "https://pay.example.invalid"}
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "192.0.2.231", ""); e != nil {
		t.Fatal("submit synthetic", no, e)
	}
	if e = g.HandleNotification(ctx, 7, o.TradeNo, nil, []byte("valid-signed-test-event")); e != nil {
		t.Fatal("callback synthetic", no, e)
	}
	return o
}
func TestComplaint231AllOrdersAndAccountMustMatchBeforeMerchantNotice(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := &noticePayment231{account: "original-mch"}
	for _, mid := range []int64{23101, 23102} {
		if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "source231", APIKey: "synthetic-source231-key"}); e != nil {
			t.Fatal(e)
		}
	}
	a := complaintVerifiedPaid231(t, s, 23101, "same-merchant-number", p)
	b := complaintVerifiedPaid231(t, s, 23102, "same-merchant-number", p)
	cases := []struct {
		name     string
		nos      []string
		channel  int64
		provider adapter.Provider
		owner    int64
	}{
		{"exact", []string{a.TradeNo, a.TradeNo}, 7, p, 23101},
		{"merchant number collision", []string{a.OutTradeNo}, 7, p, 0},
		{"mixed merchants", []string{a.TradeNo, b.TradeNo}, 7, p, 0},
		{"partly unknown", []string{a.TradeNo, "unknown-platform-order"}, 7, p, 0},
		{"other channel", []string{a.TradeNo}, 8, p, 0},
		{"rotated payer account", []string{a.TradeNo}, 7, &noticePayment231{account: "new-mch"}, 0},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := adapter.ComplaintRecord{ThirdID: fmt.Sprintf("notice231-%d", i), PayType: "wxpay", Status: "PENDING", TradeNos: tc.nos, Title: "synthetic reason", Content: "synthetic body", Times: 1}
			if e := s.upsertComplaint133(ctx, tc.channel, rec, tc.provider); e != nil {
				t.Fatal(e)
			}
			var id, owner int64
			if e := s.pool.QueryRow(ctx, `SELECT id,COALESCE(owner_id,0) FROM complaints133 WHERE third_id=$1`, rec.ThirdID).Scan(&id, &owner); e != nil || owner != tc.owner {
				t.Fatalf("wrong attribution %d %v", owner, e)
			}
			var merchant, admin int
			if e := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM merchant_notice_events e JOIN notification_sources231 n ON n.id=e.supplement_id WHERE n.kind='complain' AND n.reference_id=$1),(SELECT count(*) FROM admin_business_notice_events e JOIN backoffice_audit a ON a.id=e.audit_id JOIN notification_sources231 n ON n.id=a.resource_id WHERE a.resource='notification-sources231' AND n.kind='complain' AND n.reference_id=$1)`, id).Scan(&merchant, &admin); e != nil {
				t.Fatal(e)
			}
			wantMerchant := 0
			if tc.owner > 0 {
				wantMerchant = 1
			}
			if merchant != wantMerchant || admin != 1 {
				t.Fatalf("notices merchant=%d admin=%d want merchant=%d", merchant, admin, wantMerchant)
			}
			// A legacy best-effort owner cannot survive a current authenticated
			// sync that cannot prove that merchant's complete original account.
			if tc.name == "merchant number collision" {
				if _, e := s.pool.Exec(ctx, `UPDATE complaints133 SET owner_id=23102 WHERE id=$1`, id); e != nil {
					t.Fatal(e)
				}
			}
			rec.Status = "PROCESSING"
			if e := s.upsertComplaint133(ctx, tc.channel, rec, tc.provider); e != nil {
				t.Fatal(e)
			}
			if e := s.pool.QueryRow(ctx, `SELECT COALESCE(owner_id,0) FROM complaints133 WHERE id=$1`, id).Scan(&owner); e != nil || owner != tc.owner {
				t.Fatal("legacy guessed ownership survived verification", owner, e)
			}
			var total int
			if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM notification_sources231 WHERE kind='complain' AND reference_id=$1`, id).Scan(&total); e != nil || total != 1 {
				t.Fatal("resync duplicated original notice", total, e)
			}
			tx, e := s.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			if _, _, e = noticesource231.Insert(ctx, tx, fmt.Sprintf("forged:%d", id), noticesource231.Facts{Kind: "complain", ReferenceID: id, ChannelID: tc.channel}); e == nil {
				t.Fatal("historical complaint invented a new source")
			}
		})
	}
	if check, e := s.notice.Check(ctx); e != nil || check.InvalidEvents != 0 {
		t.Fatal("merchant source verification", check, e)
	}
	if check, e := s.business.Check(ctx); e != nil || check.InvalidEvents != 0 {
		t.Fatal("admin source verification", check, e)
	}
}

type confirmProvider231 struct{ mchTransferDouble }

func (*confirmProvider231) ApplicationID() string           { return "wx-confirm231" }
func (*confirmProvider231) TransferAccountIdentity() string { return "payer-confirm231" }

type confirmationSender231 struct{ calls int }

func (*confirmationSender231) NoticeTransport() merchantnotice.Transport {
	return merchantnotice.Wechat
}
func (*confirmationSender231) NoticeScope() string     { return "synthetic-confirmation231" }
func (*confirmationSender231) NoticeAccountID() string { return "wx-confirm231" }
func (*confirmationSender231) ValidateNoticeRecipient(to string) error {
	if to != "original-payee231" {
		return merchantnotice.ErrInvalid
	}
	return nil
}
func (s *confirmationSender231) ResolveMerchantNotice(_ context.Context, kind merchantnotice.Transport, a merchantnotice.Account) (merchantnotice.Sender, error) {
	if kind != merchantnotice.Wechat || a.WXAppID != "wx-confirm231" || a.Input.Scene != "wxtransfer" || !strings.HasPrefix(a.Input.Params["confirm_path"], "/pay/wxtrans?") {
		return nil, merchantnotice.ErrInvalid
	}
	return s, nil
}
func (s *confirmationSender231) SendMerchantNotice(_ context.Context, to string, m merchantnotice.Message) error {
	if s.ValidateNoticeRecipient(to) != nil || !m.Valid() || !strings.Contains(m.Title, "待确认收款") {
		return merchantnotice.ErrInvalid
	}
	s.calls++
	return nil
}
func TestWechatConfirmation231OnlyFirstPackageBirthCreatesNotice(t *testing.T) {
	s, flow, _ := transferFixture(t)
	p := &confirmProvider231{}
	flow.Providers[7] = p
	for k, v := range map[string]string{"transfer_wxpay_type": "1", "transfer_wxpay_scene_id": "1005", "transfer_wxpay_info_type": "岗位类型", "transfer_wxpay_info_content": "外卖员"} {
		mchTransferSetting(t, s, k, v)
	}
	mchTransferSetting(t, s, "wxnotice_tpl_wxtransfer", "synthetic-template231")
	mchTransferSetting(t, s, "wxnotice_tpl_wxtransfer_money", "amount1")
	req := adapter.TransferRequest{Type: "wxpay", Account: "original-payee231", Amount: 100, Remark: "synthetic"}
	r, e := flow.Transfer(t.Context(), 501, 7, "confirm231", req)
	if e != nil {
		t.Fatal(e)
	}
	var n int
	count := func() {
		t.Helper()
		if e = s.pool.QueryRow(t.Context(), `SELECT count(*) FROM merchant_notice_events e JOIN notification_sources231 n ON n.id=e.supplement_id WHERE n.kind='wxtransfer' AND n.reference_id=$1`, r.ID).Scan(&n); e != nil || n != 1 {
			t.Fatal("confirmation source count", n, e)
		}
	}
	count()
	var snapshot string
	var profile *int64
	var currentProfiles int
	if e = s.pool.QueryRow(t.Context(), `SELECT e.snapshot_state,e.profile_id,(SELECT count(*) FROM merchant_notice_current WHERE merchant_id=501) FROM merchant_notice_events e JOIN notification_sources231 n ON n.id=e.supplement_id WHERE n.kind='wxtransfer' AND n.reference_id=$1`, r.ID).Scan(&snapshot, &profile, &currentProfiles); e != nil || snapshot != "direct" || profile != nil || currentProfiles != 0 {
		t.Fatal("confirmation wrongly requires personal notice profile", snapshot, profile, currentProfiles, e)
	}
	job, e := (jobs.Queue{Pool: s.pool}).Claim(t.Context(), []string{"merchant.notice"}, time.Minute)
	if e != nil {
		t.Fatal("confirmation failed to enqueue without a personal profile", e)
	}
	sender := &confirmationSender231{}
	mchTransferSetting(t, s, "wxnotice_tpl_wxtransfer_money", "amount2")
	if e = s.notice.HandleJob(t.Context(), job.ID, job.Token, sender); !errors.Is(e, merchantnotice.ErrState) || sender.calls != 0 {
		t.Fatal("historical confirmation adopted another template", sender.calls, e)
	}
	mchTransferSetting(t, s, "wxnotice_tpl_wxtransfer_money", "amount1")
	if e = s.notice.HandleJob(t.Context(), job.ID, job.Token, sender); e != nil {
		t.Fatal("source-bound confirmation delivery", e)
	}
	if e = s.notice.HandleJob(t.Context(), job.ID, job.Token, sender); e != nil {
		t.Fatal("confirmation replay", e)
	}
	if sender.calls != 1 {
		t.Fatal("confirmation did not send exactly once to original payee", sender.calls)
	}
	if e = s.resolveProviderTransferPackage(t.Context(), r.ID, "pending", r.ProviderTransferNo, "", "different-later-package", p); e != nil {
		t.Fatal(e)
	}
	count()
	if check, e := s.notice.Check(t.Context()); e != nil || check.InvalidEvents != 0 || check.InvalidDeliveries != 0 || check.UnknownDeliveries != 0 {
		t.Fatal("confirmation source integrity", check, e)
	}
}
