package backoffice

import (
	"context"
	"errors"
	"goepay/internal/payment"
	"testing"
)

func TestMerchantPaymentReview82OriginalFieldIsAtomicAndNotProfilePermission(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	created, err := s.Create(ctx, admin, "merchants", input(`{"id":"8282","name":"Review82","api_key":"synthetic-merchant-review82-key","signature_algorithm":"MD5","pay":"2","login_username":"review82","login_password":"review82-correct-password"}`))
	if err != nil {
		t.Fatal("original pay field is not supported", err)
	}
	if created.Fields["pay"] != "2" || created.Fields["enabled"] != true {
		t.Fatal(created.Fields)
	}
	var version int64
	if err = s.pool.QueryRow(ctx, "SELECT auth_version FROM console_users WHERE username='review82'").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if _, err = s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 8282, OutTradeNo: "pending82", Amount: 125}); !errors.Is(err, payment.ErrState) {
		t.Fatal("pending review accepted payment", err)
	}
	updated, err := s.Update(ctx, admin, "merchants", 8282, input(`{"pay":"1"}`))
	if err != nil || updated.Fields["pay"] != "1" {
		t.Fatal(updated, err)
	}
	var next int64
	var profileHasPay bool
	err = s.pool.QueryRow(ctx, "SELECT u.auth_version,m.profile ? 'pay' FROM merchants m JOIN console_users u ON u.subject_id=m.id AND u.role='merchant' WHERE m.id=8282").Scan(&next, &profileHasPay)
	if err != nil || next != version || profileHasPay {
		t.Fatal("pay approval changed login credentials or persisted mutable profile authority", next, version, profileHasPay, err)
	}
	if _, err = s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 8282, OutTradeNo: "approved82", Amount: 125}); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"pay":"3"}`, `{"pay":1}`, `{"pay":null}`} {
		if _, err = s.Update(ctx, admin, "merchants", 8282, input(raw)); err == nil {
			t.Fatal("malformed pay accepted", raw)
		}
	}
	auditor := admin
	auditor.AdminClass = "auditor"
	if _, err = s.Update(ctx, auditor, "merchants", 8282, input(`{"pay":"0"}`)); !errors.Is(err, ErrForbidden) {
		t.Fatal("auditor changed payment status", err)
	}
	if _, err = s.Update(ctx, Principal{Role: "merchant", SubjectID: 8282}, "merchants", 8282, input(`{"pay":"0"}`)); !errors.Is(err, ErrForbidden) {
		t.Fatal("merchant changed own approval", err)
	}
	balance, err := s.store.Balance(ctx, 8282)
	if err != nil || balance.Available != 0 || balance.Reserved != 0 {
		t.Fatal("review moved money", balance, err)
	}
}
