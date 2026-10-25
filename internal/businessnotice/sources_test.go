package businessnotice_test

import (
	"context"
	"encoding/json"
	"goepay/internal/backoffice"
	"goepay/internal/enrollment"
	"goepay/internal/payment"
	"testing"
)

func rawFields(value string) map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	_ = json.Unmarshal([]byte(value), &out)
	return out
}

func TestAdminBusiness83NewManualRequestsUseExactMerchantSourceAndSharedDomainMode(t *testing.T) {
	p := businessPool(t)
	ctx := context.Background()
	store, err := payment.NewWithKey(p, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{8301, 8302} {
		if err = store.CreateMerchant(ctx, payment.Merchant{ID: id, Name: "business83", APIKey: "synthetic-business83-key"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"msgconfig_apply","value":"1"}'),('settings','{"key":"msgconfig_domain","value":"4"}')`); err != nil {
		t.Fatal(err)
	}
	order, err := store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 8301, OutTradeNo: "seed83", Name: "测试资金", Type: "sandbox", Amount: 20000})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.MarkPaid(ctx, order.TradeNo, "synthetic-seed83"); err != nil {
		t.Fatal(err)
	}
	first, err := store.RequestFunds(ctx, "internal-transfers", 8301, 8302, "internal83", 1000, "merchant:8301")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.RequestFunds(ctx, "settlements", 8301, 0, "manual83", 2000, "merchant:8301"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RequestFunds(ctx, "settlements", 8301, 0, "auto83", 500, "system:auto"); err != nil {
		t.Fatal(err)
	}
	if _, err = store.RequestFunds(ctx, "settlements", 8301, 0, "admin83", 500, "admin:1"); err != nil {
		t.Fatal(err)
	}
	again, err := store.RequestFunds(ctx, "internal-transfers", 8301, 8302, "internal83", 1000, "merchant:8301")
	if err != nil || again.ID != first.ID {
		t.Fatal(again, err)
	}
	var count int
	if err = p.QueryRow(ctx, `SELECT count(*) FROM admin_business_notice_events WHERE scene='internal_transfer' AND mode='4'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("manual internal transfer source missing, duplicated or wrong original switch", count, err)
	}
	if err = p.QueryRow(ctx, `SELECT count(*) FROM admin_business_notice_events WHERE scene='apply' AND mode='1'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("manual withdrawal source missing or adopted automatic/admin request", count, err)
	}
	if err = p.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind='admin.business-notice'`).Scan(&count); err != nil || count != 2 {
		t.Fatal("request messages not bound once", count, err)
	}
	if _, err = store.ReviewFundsRequest(ctx, first.ID, true, "admin:1"); err != nil {
		t.Fatal(err)
	}
	history, err := store.AdminBusinessNotices().Events(ctx, 0, 20)
	if err != nil || len(history) != 2 {
		t.Fatal("normal funds review invalidated original notice", history, err)
	}
	check, err := store.AdminBusinessNotices().Check(ctx)
	if err != nil || check.InvalidEvents != 0 || check.InvalidDeliveries != 0 {
		t.Fatal(check, err)
	}
}

func TestAdminBusiness83NewVerifiedRegistrationOnlyNotifiesPay2AfterConsumedProof(t *testing.T) {
	p := businessPool(t)
	ctx := context.Background()
	store, err := payment.NewWithKey(p, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"registration_open","value":"true"}'),('settings','{"key":"user_review","value":"1"}'),('settings','{"key":"msgconfig_regaudit","value":"1"}')`); err != nil {
		t.Fatal(err)
	}
	var otp string
	sender := enrollment.SenderFunc(func(_ context.Context, _ string, _ string, code string) error { otp = code; return nil })
	service, err := enrollment.New(p, store, make([]byte, 32), sender)
	if err != nil {
		t.Fatal(err)
	}
	register := func(email, username, source string) enrollment.Registered {
		t.Helper()
		challenge, err := service.Issue(ctx, enrollment.VerificationRequest{Purpose: "register", Email: email, Source: source})
		if err != nil {
			t.Fatal(err)
		}
		result, err := service.Register(ctx, enrollment.RegisterInput{ChallengeID: challenge.ID, Code: otp, Email: email, Source: source, Username: username, Name: "通知测试", Password: "synthetic-register83-password"})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := register("pending83@example.test", "pending83", "192.0.2.83")
	var count int
	if err = p.QueryRow(ctx, `SELECT count(*) FROM admin_business_notice_events WHERE scene='regaudit' AND merchant_id=$1 AND mode='1'`, first.MerchantID).Scan(&count); err != nil || count != 1 {
		t.Fatal("new pay2 registration did not publish original verified source", count, err)
	}
	if _, err = p.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}','"0"') WHERE resource='settings' AND body->>'key'='user_review'`); err != nil {
		t.Fatal(err)
	}
	_ = register("normal83@example.test", "normal83", "192.0.2.84")
	if err = p.QueryRow(ctx, `SELECT count(*) FROM admin_business_notice_events WHERE scene='regaudit'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("pay1 registration was mislabeled pending review", count, err)
	}
	if _, err = p.Exec(ctx, `UPDATE merchants SET pay=1,profile=profile||'{"email":"later83@example.test"}'::jsonb WHERE id=$1`, first.MerchantID); err != nil {
		t.Fatal(err)
	}
	history, err := store.AdminBusinessNotices().Events(ctx, 0, 20)
	if err != nil || len(history) != 1 {
		t.Fatal("registration approval/profile edit invalidated original verified notice", history, err)
	}
}

func TestAdminBusiness83NewDomainSubmissionEmitsOnceFromOriginalMerchantTransaction(t *testing.T) {
	p := businessPool(t)
	ctx := context.Background()
	store, err := payment.NewWithKey(p, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CreateMerchant(ctx, payment.Merchant{ID: 8301, Name: "business83", APIKey: "synthetic-business83-key"}); err != nil {
		t.Fatal(err)
	}
	if _, err = p.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"msgconfig_domain","value":"2"}')`); err != nil {
		t.Fatal(err)
	}
	service := backoffice.New(p, store)
	merchant := backoffice.Principal{Role: "merchant", SubjectID: 8301, Username: "merchant8301"}
	domain, err := service.Create(ctx, merchant, "domains", rawFields(`{"domain":"shop.example.test","enabled":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var table *string
	if err = p.QueryRow(ctx, `SELECT to_regclass('admin_business_notice_events')::text`).Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table == nil {
		t.Fatal("original pending domain submission produced zero administrator business events")
	}
	var count int
	if err = p.QueryRow(ctx, `SELECT count(*) FROM admin_business_notice_events WHERE scene='domain' AND merchant_id=8301 AND mode='2'`).Scan(&count); err != nil || count != 1 {
		t.Fatal("fresh domain source missing or duplicated", count, err)
	}
	if _, err = service.Update(ctx, merchant, "domains", domain.ID, rawFields(`{"enabled":true}`)); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Update(ctx, merchant, "domains", domain.ID, rawFields(`{"domain":"renew.example.test"}`)); err != nil {
		t.Fatal(err)
	}
	if err = p.QueryRow(ctx, `SELECT count(*) FROM admin_business_notice_events WHERE scene='domain' AND merchant_id=8301`).Scan(&count); err != nil || count != 2 {
		t.Fatal("normal edit duplicated or changed-host resubmit lost source", count, err)
	}
	var jobs int
	if err = p.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind='admin.business-notice'`).Scan(&jobs); err != nil || jobs != 2 {
		t.Fatal("original jobs not bound to two submissions", jobs, err)
	}
}
