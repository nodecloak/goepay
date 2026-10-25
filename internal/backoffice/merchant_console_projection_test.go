package backoffice

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"goepay/internal/payment"
)

func TestMerchantConsoleProjectionReadOnlyAndNoProfilePollution(t *testing.T) {
	s := setup(t)
	ctx := context.Background()
	for _, row := range []struct {
		no     string
		amount int64
	}{{"available", 1000}, {"held", 250}} {
		o, err := s.store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 101, OutTradeNo: row.no, Name: "Synthetic funds", Amount: row.amount})
		if err != nil {
			t.Fatal(err)
		}
		if err = s.store.MarkPaid(ctx, o.TradeNo, "paid-"+row.no); err != nil {
			t.Fatal(err)
		}
		if row.no == "held" {
			if _, err = s.store.FreezeOrderIncome(ctx, 101, o.TradeNo, "projection-held", payment.IncomeHoldActor{ID: 1, Name: "operator", Class: "operator"}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.store.ReserveRefund(ctx, 101, "available", "projection-reserved", 100); err != nil {
		t.Fatal(err)
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash) VALUES('projection-login','merchant',101,'Synthetic','unused')`); err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{"available_minor": "900", "frozen_minor": "100", "income_held_minor": "250", "login_username": "projection-login"}
	assertFields := func(it Item) {
		t.Helper()
		for key, want := range expected {
			if it.Fields[key] != want {
				t.Errorf("%s: got %#v want %q", key, it.Fields[key], want)
			}
		}
		if _, err := time.Parse(time.RFC3339Nano, fmt.Sprint(it.Fields["created_at"])); err != nil {
			t.Errorf("original registration time missing: %v", it.Fields["created_at"])
		}
	}
	it, err := s.Get(ctx, admin, "merchants", 101)
	if err != nil {
		t.Fatal(err)
	}
	assertFields(it)
	page, err := s.List(ctx, admin, "merchants", Query{Page: 1, Limit: 20, Search: "projection-login"})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].ID != 101 {
		t.Errorf("login search/list mismatch: %+v", page)
	} else {
		assertFields(page.Items[0])
	}
	other, err := s.Get(ctx, admin, "merchants", 102)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"available_minor", "frozen_minor", "income_held_minor"} {
		if other.Fields[key] != "0" {
			t.Errorf("other merchant %s: %v", key, other.Fields[key])
		}
	}
	for _, key := range []string{"available_minor", "frozen_minor", "income_held_minor", "created_at"} {
		if _, err = s.Update(ctx, admin, "merchants", 101, input(fmt.Sprintf(`{"%s":"999"}`, key))); !errors.Is(err, ErrForbidden) {
			t.Errorf("computed field update %s accepted: %v", key, err)
		}
		if _, err = s.Create(ctx, admin, "merchants", input(fmt.Sprintf(`{"id":"9001","name":"forged","api_key":"synthetic-long-key-at-least-24","signature_algorithm":"MD5","%s":"999"}`, key))); err == nil {
			t.Errorf("computed field create %s accepted", key)
		}
	}
	updated, err := s.Update(ctx, admin, "merchants", 101, input(`{"name":"ordinary edit"}`))
	if err != nil {
		t.Fatal(err)
	}
	assertFields(updated)
	var polluted bool
	if err = s.pool.QueryRow(ctx, `SELECT profile ?| ARRAY['available_minor','frozen_minor','income_held_minor','created_at','login_username'] FROM merchants WHERE id=101`).Scan(&polluted); err != nil || polluted {
		t.Fatal("derived data copied to merchant profile", polluted, err)
	}
	merchant := Principal{Role: "merchant", SubjectID: 101, Username: "projection-login"}
	if _, err = s.List(ctx, merchant, "merchants", Query{Page: 1, Limit: 20}); !errors.Is(err, ErrForbidden) {
		t.Fatal("merchant gained admin list", err)
	}
	if balance, err := s.store.Balance(ctx, 101); err != nil || balance.Available != 900 || balance.Reserved != 100 {
		t.Fatal("projection changed or confused balances", balance, err)
	}
}
