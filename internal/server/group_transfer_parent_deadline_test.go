package server

import (
	"errors"
	"strconv"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
)

func TestGroupTransferFeeParentSourceAuditWaitCannotCommitExpiredMembership(t *testing.T) {
	setup := parent180Outgoing(t)
	f, ctx := setup.f, t.Context()
	channel, err := strconv.ParseInt(setup.id, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.Exec(ctx, `UPDATE backoffice_records SET body=body||'{"transfer_rate":"10.00"}'::jsonb WHERE resource='groups' AND id=0`); err != nil {
		t.Fatal(err)
	}
	var vip int64
	if err = f.p.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('groups','{"name":"parent source expiring fee","enabled":true,"transfer_rate":"0.00"}')RETURNING id`).Scan(&vip); err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.Exec(ctx, `CREATE SEQUENCE parent_fee_audit_waits;
CREATE FUNCTION wait_parent_fee_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.resource='runtime-parent-transfer-intents' THEN
  PERFORM nextval('parent_fee_audit_waits');
  PERFORM pg_sleep(GREATEST(0,(SELECT extract(epoch FROM expires_at-clock_timestamp()) FROM memberships WHERE merchant_id=1))::double precision+0.05);
 END IF;
 RETURN NEW; END $$;
CREATE TRIGGER wait_parent_fee_audit BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION wait_parent_fee_audit()`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at)VALUES(1,$1,clock_timestamp()+interval '2 seconds')`, vip); err != nil {
		t.Fatal(err)
	}
	transfer, err := f.s.ProviderFlow.Transfer(ctx, 1, channel, "expired-parent-source-fee", adapter.TransferRequest{Type: "wxpay", Account: "synthetic-parent-fee-recipient", Amount: 100, Remark: "Synthetic group deadline"})
	var reached bool
	if e := f.p.QueryRow(ctx, `SELECT is_called FROM parent_fee_audit_waits`).Scan(&reached); e != nil || !reached {
		t.Fatal("test must reach the parent source audit after the inner fee check", reached, e)
	}
	if !errors.Is(err, payment.ErrConflict) || setup.transfers.Load() != 0 {
		t.Errorf("expired fee crossed outer parent source persistence: transfer=%d rate=%d cost=%d sends=%d err=%v", transfer.ID, transfer.FeeRateBPS, transfer.CostAmount, setup.transfers.Load(), err)
	}
	balance, e := f.s.Store.Balance(ctx, 1)
	if e != nil || balance.Available != 10000 || balance.Reserved != 0 {
		t.Error("expired parent fee left funds reserved", balance, e)
	}
	var intents int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM transfers WHERE merchant_id=1 AND transfer_no='expired-parent-source-fee'`).Scan(&intents); e != nil || intents != 0 {
		t.Error("expired parent fee left a durable transfer intent", intents, e)
	}
}
