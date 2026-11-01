package payment

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestGroupPaymentDomainNewIntentAdmissionAndOriginalReplay(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	const mid int64 = 123060
	if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "Synthetic domain admission", APIKey: "synthetic-domain-admission"}); e != nil {
		t.Fatal(e)
	}
	set := func(v string) { t.Helper(); setGroupTransferFields(t, s, map[string]any{"pay_domain_forbid": v}) }
	in := CreateOrderInput{MerchantID: mid, OutTradeNo: "domain-new", Name: "Synthetic goods", Type: "wxpay", Amount: 100, NotifyURL: "https://shop.example.com/hook", ReturnURL: "https://return.example.com/done"}
	set("1")
	if _, e := s.CreateOrder(ctx, in); e == nil {
		t.Error("new order bypassed group domain approval")
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,owner_id,body) VALUES('domains',$1,'{"domain":"*.example.com","approved":true,"enabled":true}')`, mid); e != nil {
		t.Fatal(e)
	}
	original, e := s.CreateOrder(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"approved":false}'::jsonb WHERE resource='domains'`); e != nil {
		t.Fatal(e)
	}
	replay, e := s.CreateOrder(ctx, in)
	if e != nil || replay.ID != original.ID {
		t.Fatal("new domain policy invalidated exact original intent", replay, e)
	}
	changed := in
	changed.NotifyURL = "https://other.example.com/hook"
	if _, e = s.CreateOrder(ctx, changed); !errors.Is(e, ErrConflict) {
		t.Error("original external number accepted changed callbacks", e)
	}
	in.OutTradeNo = "domain-after-revoke"
	if _, e = s.CreateOrder(ctx, in); e == nil {
		t.Error("revoked domain admitted a new order")
	}
	set("0")
	if _, e = s.CreateOrder(ctx, in); e != nil {
		t.Error("explicit group zero did not disable domain gate", e)
	}
	set("bad")
	in.OutTradeNo = "domain-corrupt"
	if _, e = s.CreateOrder(ctx, in); e == nil {
		t.Error("corrupt group domain field failed open")
	}
}

func TestGroupPaymentDomainRejectsFirstGlobalInsertBeforeNewOrderCommit(t *testing.T) {
	s := testStore(t)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	const mid int64 = 123061
	if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "Synthetic domain recheck", APIKey: "synthetic-domain-recheck"}); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION domain_order_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM set_config('application_name','group-domain-first-global',true); PERFORM pg_advisory_xact_lock(123061::bigint); RETURN NEW; END $$; CREATE TRIGGER domain_order_barrier BEFORE INSERT ON orders FOR EACH ROW WHEN (NEW.out_trade_no='domain-first-global') EXECUTE FUNCTION domain_order_barrier()`); e != nil {
		t.Fatal(e)
	}
	blocker, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(ctx)
	if _, e = blocker.Exec(ctx, `SELECT pg_advisory_xact_lock(123061::bigint)`); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() {
		_, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: mid, OutTradeNo: "domain-first-global", Amount: 100, NotifyURL: "https://unapproved.example.com/hook"})
		result <- e
	}()
	depositPolicyWaitForLock(t, ctx, s, "group-domain-first-global")
	if _, e = s.pool.Exec(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"enforce_domains","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	if e = blocker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-result:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if e == nil {
		t.Error("first global domain policy inserted after capture was ignored")
	}
	if _, e = s.Order(ctx, mid, "domain-first-global"); !errors.Is(e, ErrNotFound) {
		t.Error("late domain policy left a durable new order", e)
	}
}

func TestGroupPaymentDomainMembershipExpiryAndLateSnapshotRollback(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()
	const mid int64 = 123062
	if e := s.CreateMerchant(ctx, Merchant{ID: mid, Name: "Synthetic domain member", APIKey: "synthetic-domain-member"}); e != nil {
		t.Fatal(e)
	}
	setGroupTransferFields(t, s, map[string]any{"pay_domain_forbid": "1"})
	var gid int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"Synthetic domain VIP","enabled":true,"pay_domain_forbid":"0"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES($1,$2,clock_timestamp()+interval '1 day')`, mid, gid); e != nil {
		t.Fatal(e)
	}
	in := CreateOrderInput{MerchantID: mid, OutTradeNo: "domain-vip-original", Amount: 100, NotifyURL: "https://original.example.com/hook"}
	original, e := s.CreateOrder(ctx, in)
	if e != nil {
		t.Fatal("active VIP override ignored", e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE memberships SET expires_at=clock_timestamp()-interval '1 second' WHERE merchant_id=$1`, mid); e != nil {
		t.Fatal(e)
	}
	if replay, e := s.CreateOrder(ctx, in); e != nil || replay.ID != original.ID {
		t.Fatal("expiry invalidated original domain intent", replay, e)
	}
	in.OutTradeNo = "domain-expired-new"
	if _, e = s.CreateOrder(ctx, in); e == nil {
		t.Error("expired VIP's off override admitted a new payment")
	}
	if _, e = s.pool.Exec(ctx, `UPDATE memberships SET expires_at=clock_timestamp()+interval '1 second' WHERE merchant_id=$1`, mid); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `CREATE FUNCTION domain_vip_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM (SELECT expires_at FROM memberships WHERE merchant_id=NEW.merchant_id)-clock_timestamp()))+0.05); RETURN NEW; END $$; CREATE TRIGGER domain_vip_barrier BEFORE INSERT ON orders FOR EACH ROW WHEN (NEW.out_trade_no='domain-vip-late') EXECUTE FUNCTION domain_vip_barrier()`); e != nil {
		t.Fatal(e)
	}
	in.OutTradeNo = "domain-vip-late"
	if _, e = s.CreateOrder(ctx, in); e == nil {
		t.Error("expired VIP domain exemption committed after the insert wait")
	}
	if _, e = s.Order(ctx, mid, in.OutTradeNo); !errors.Is(e, ErrNotFound) {
		t.Error("expired new domain intent survived rollback", e)
	}
}
