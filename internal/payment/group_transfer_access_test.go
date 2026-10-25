package payment

import (
	"context"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"testing"
	"time"
)

func TestOriginalGroupTransferAccessDisabledGroupCannotGrantNewTransfers(t *testing.T) {
	s, g, p := transferFixture(t)
	ctx := context.Background()
	var gid int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"禁用组保护145","enabled":false,"user_transfer":"1"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb($1::bigint::text)) WHERE id=501`, gid); e != nil {
		t.Fatal(e)
	}
	req := adapter.TransferRequest{Type: "wxpay", Account: "disabled-group145", Amount: 100, Remark: "本地测试"}
	if _, e := g.Transfer(ctx, 501, 7, "group145-disabled", req); e == nil {
		t.Fatal("disabled group granted a new transfer")
	}
	balance, e := s.Balance(ctx, 501)
	if e != nil || balance.Available != 1000 || balance.Reserved != 0 || p.transfers.Load() != 0 {
		t.Fatalf("disabled group touched funds/provider %+v %v calls%d", balance, e, p.transfers.Load())
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','true') WHERE id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	first, e := g.Transfer(ctx, 501, 7, "group145-original", req)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false') WHERE id=$1`, gid); e != nil {
		t.Fatal(e)
	}
	replay, e := g.Transfer(ctx, 501, 7, "group145-original", req)
	if e != nil || replay.ID != first.ID || p.transfers.Load() != 1 {
		t.Fatalf("disabling a group changed or resent an original intent %+v %v", replay, e)
	}
	if _, e = g.QueryTransfer(ctx, 501, "group145-original"); e != nil {
		t.Fatalf("group disable blocked original reconciliation %v", e)
	}
}

func TestOriginalGroupTransferAccessExpiryDuringGroupLockCannotReserve(t *testing.T) {
	s, g, p := transferFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	transferSetting(t, s, "transfer_enabled", "false")
	var gid int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"本地到期锁144","user_transfer":"1"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(501,$1,clock_timestamp()+interval '3 seconds')`, gid); e != nil {
		t.Fatal(e)
	}
	blocker, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer blocker.Rollback(context.Background())
	var blockerPID int
	if e = blocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); e != nil {
		t.Fatal(e)
	}
	if _, e = blocker.Exec(ctx, `SELECT id FROM backoffice_records WHERE id=$1 FOR UPDATE`, gid); e != nil {
		t.Fatal(e)
	}
	result := make(chan error, 1)
	go func() {
		_, e := g.Transfer(ctx, 501, 7, "group144-expiring-lock", adapter.TransferRequest{Type: "wxpay", Account: "local-expiry144", Amount: 100, Remark: "本地测试"})
		result <- e
	}()
	for {
		var blocked bool
		if e = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&blocked); e != nil {
			t.Fatal(e)
		}
		if blocked {
			break
		}
		select {
		case e := <-result:
			t.Fatalf("transfer did not reach the original group row wait: %v", e)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if _, e = s.pool.Exec(ctx, `SELECT pg_sleep(GREATEST(0,extract(epoch FROM expires_at-clock_timestamp()))::double precision+0.05) FROM memberships WHERE merchant_id=501`); e != nil {
		t.Fatal(e)
	}
	if e = blocker.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-result:
		if !errors.Is(e, ErrState) {
			t.Fatalf("permission was sampled before expiry lock wait: %v", e)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	balance, e := s.Balance(ctx, 501)
	if e != nil || balance.Available != 1000 || balance.Reserved != 0 || p.transfers.Load() != 0 {
		t.Fatalf("expired waiting request reserved/called %+v %v calls%d", balance, e, p.transfers.Load())
	}
}

func TestOriginalGroupTransferAccessUnsentBatchRechecksExpiryAndLiveGate(t *testing.T) {
	s, g, p := batchFixture(t)
	ctx := context.Background()
	transferSetting(t, s, "transfer_enabled", "false")
	var gid int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"本地批量代付144","user_transfer":"1"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(501,$1,now()+interval '1 day')`, gid); e != nil {
		t.Fatal(e)
	}
	b, e := g.PrepareTransferBatch(ctx, 501, "group144-batch", batchInputs(100), batchMerchantActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE memberships SET expires_at=now()-interval '1 second' WHERE merchant_id=501`); e != nil {
		t.Fatal(e)
	}
	if _, e = g.ExecuteTransferBatch(ctx, 501, b.ID, 1, batchMerchantActor, nil); !errors.Is(e, ErrState) {
		t.Fatalf("expired batch permission not rechecked %v", e)
	}
	current, e := s.TransferBatch(ctx, 501, b.ID)
	if e != nil || current.Summary.Unsent != 1 || p.transfers.Load() != 0 {
		t.Fatalf("rejected batch consumed send %+v %v", current, e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE memberships SET expires_at=now()+interval '1 day' WHERE merchant_id=501`); e != nil {
		t.Fatal(e)
	}
	g.AllowLive = false
	if _, e = g.ExecuteTransferBatch(ctx, 501, b.ID, 1, batchMerchantActor, nil); !errors.Is(e, ErrLiveDisabled) {
		t.Fatalf("group bypassed execution kill switch %v", e)
	}
	if p.transfers.Load() != 0 {
		t.Fatal("kill switch called provider")
	}
	g.AllowLive = true
	if _, e = g.ExecuteTransferBatch(ctx, 501, b.ID, 1, batchMerchantActor, nil); e != nil || p.transfers.Load() != 1 {
		t.Fatalf("renewed original group cannot send unsent item %v calls%d", e, p.transfers.Load())
	}
}

func TestOriginalGroupTransferAccessOverrideExpiryAndOriginalReplay(t *testing.T) {
	s, g, p := transferFixture(t)
	ctx := context.Background()
	transferSetting(t, s, "transfer_enabled", "false")
	var gid int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"本地普通代付144","enabled":true,"user_transfer":"1"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `INSERT INTO memberships(merchant_id,group_id,expires_at) VALUES(501,$1,now()+interval '1 day')`, gid); e != nil {
		t.Fatal(e)
	}
	req := adapter.TransferRequest{Type: "wxpay", Account: "local144-recipient", Amount: 100, Remark: "本地测试"}
	first, e := g.Transfer(ctx, 501, 7, "group144-first", req)
	if e != nil {
		t.Fatalf("original group enable did not override ordinary business switch: %v", e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE memberships SET expires_at=now()-interval '1 second' WHERE merchant_id=501`); e != nil {
		t.Fatal(e)
	}
	if _, e = g.Transfer(ctx, 501, 7, "group144-after-expiry", req); e == nil {
		t.Fatal("expired group still allowed a new transfer")
	}
	replay, e := g.Transfer(ctx, 501, 7, "group144-first", req)
	if e != nil || replay.ID != first.ID || p.transfers.Load() != 1 {
		t.Fatalf("historical replay repriced or resent %+v %v calls%d", replay, e, p.transfers.Load())
	}
	if _, e = g.QueryTransfer(ctx, 501, "group144-first"); e != nil || p.query.Load() != 1 {
		t.Fatalf("old intent lost trusted reconciliation after permission expired %v", e)
	}
	policy, e := s.TransferPolicy(ctx)
	if e != nil || policy.Enabled {
		t.Fatalf("ordinary group altered global payout policy %+v %v", policy, e)
	}
}

func TestOriginalGroupTransferAccessLegacyGlobalFallbackAndIndependentMerchantGate(t *testing.T) {
	s, g, p := transferFixture(t)
	ctx := context.Background()
	if _, e := s.pool.Exec(ctx, `DELETE FROM backoffice_records WHERE resource='settings' AND body->>'key'='transfer_enabled'`); e != nil {
		t.Fatal(e)
	}
	transferSetting(t, s, "user_transfer", "1")
	req := adapter.TransferRequest{Type: "wxpay", Account: "local144-legacy", Amount: 100, Remark: "本地测试"}
	if _, e := g.Transfer(ctx, 501, 7, "group144-legacy", req); e != nil {
		t.Fatalf("original user_transfer not consumed for ordinary new transfers: %v", e)
	}
	transferSetting(t, s, "transfer_enabled", "false")
	if _, e := g.Transfer(ctx, 501, 7, "group144-canonical-off", req); e == nil {
		t.Fatal("legacy flag bypassed explicit canonical off")
	}
	var gid int64
	if e := s.pool.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('groups','{"name":"普通代付开启144","enabled":true,"user_transfer":"1"}') RETURNING id`).Scan(&gid); e != nil {
		t.Fatal(e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{group_id}',to_jsonb($1::bigint::text))||'{"transfer_api_enabled":false}'::jsonb WHERE id=501`, gid); e != nil {
		t.Fatal(e)
	}
	if _, e := g.Transfer(ctx, 501, 7, "group144-no-merchant-grant", req); e == nil {
		t.Fatal("group enable bypassed merchant grant")
	}
	if p.transfers.Load() != 1 {
		t.Fatal("rejected operation reached provider", p.transfers.Load())
	}
	balance, e := s.Balance(ctx, 501)
	if e != nil || balance.Reserved != 110 {
		t.Fatalf("rejected requests reserved funds %+v %v", balance, e)
	}
}
