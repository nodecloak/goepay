package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestOriginalMerchantSettlementPermissionBlocksNewRequestButPreservesOriginalReplay(t *testing.T) {
	s := testStore(t)
	settlementMerchant(t, s)
	ctx := context.Background()
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 681, OutTradeNo: "settle160-income", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "settle160-paid"); e != nil {
		t.Fatal(e)
	}
	first, e := s.RequestFunds(ctx, "settlements", 681, 0, "settle160-original", 100, "merchant")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"settle":0,"settle_status":1}'::jsonb WHERE id=681;UPDATE backoffice_records SET body=body||'{"settle_open":"3","user_transfer":"1"}'::jsonb WHERE id=0 AND resource='groups'`); e != nil {
		t.Fatal(e)
	}
	quote, e := s.SettlementQuote(ctx, 681, 100)
	if e != nil || quote.CanApply || quote.Access.ManualAllowed || quote.Access.AutomaticAllowed {
		t.Fatal("group/global overrode merchant settle=0", quote, e)
	}
	before, e := s.Balance(ctx, 681)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.RequestFunds(ctx, "settlements", 681, 0, "settle160-denied", 100, "merchant"); e == nil {
		t.Fatal("disabled merchant reserved a new settlement")
	}
	replay, e := s.RequestFunds(ctx, "settlements", 681, 0, "settle160-original", 100, "merchant")
	if e != nil || replay.ID != first.ID {
		t.Fatal("original settlement replay changed", replay, e)
	}
	after, e := s.Balance(ctx, 681)
	if e != nil || after != before {
		t.Fatal("denied/replay changed funds", after, e)
	}
	var requests int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM ledger_requests WHERE merchant_id=681 AND kind='settlements'").Scan(&requests); e != nil || requests != 1 {
		t.Fatal(requests, e)
	}
}

func TestOriginalMerchantSettlementPermissionBlocksNewTransferButAllowsOriginalQuery(t *testing.T) {
	s, g, p := transferFixture(t)
	ctx := context.Background()
	req := adapter.TransferRequest{Type: "wxpay", Account: "merchant-settle160-local", Amount: 100, Remark: "local"}
	first, e := g.Transfer(ctx, 501, 7, "settle160-original", req)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"settle":0}'::jsonb WHERE id=501;UPDATE backoffice_records SET body=body||'{"user_transfer":"1"}'::jsonb WHERE id=0 AND resource='groups'`); e != nil {
		t.Fatal(e)
	}
	before, e := s.Balance(ctx, 501)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.Transfer(ctx, 501, 7, "settle160-denied", req); e == nil {
		t.Fatal("disabled merchant reached new provider transfer")
	}
	if p.transfers.Load() != 1 {
		t.Fatal("denied transfer reached provider")
	}
	replay, e := g.Transfer(ctx, 501, 7, "settle160-original", req)
	if e != nil || replay.ID != first.ID || p.transfers.Load() != 1 {
		t.Fatal("disabled merchant original replay changed", replay, e)
	}
	after, e := s.Balance(ctx, 501)
	if e != nil || before != after {
		t.Fatal("denied/replay changed original funds", before, after, e)
	}
	if _, e = g.QueryTransfer(ctx, 501, "settle160-original"); e != nil {
		t.Fatal("original reconciliation blocked by current permission", e)
	}
}

func TestOriginalMerchantSettlementPermissionRevisionCannotRearmFutureAuthority(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	p := settlementPlanReady(t, s, 16001)
	guard := settlementPlanGuard(t, s)
	a, e := s.AuthorizeSettlementPlan(guard, p.ID, p.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	var original, after int64
	if e = s.pool.QueryRow(ctx, "SELECT settlement_permission_revision FROM merchants WHERE id=16001").Scan(&original); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"phone":"13000016001","settle":1}'::jsonb WHERE id=16001`); e != nil {
		t.Fatal(e)
	}
	if e = s.pool.QueryRow(ctx, "SELECT settlement_permission_revision FROM merchants WHERE id=16001").Scan(&after); e != nil || after != original {
		t.Fatal("non-permission edit invalidated authority", original, after, e)
	}
	run, e := s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin)
	if e != nil {
		t.Fatal("non-permission edit broke original authority", e)
	}
	for _, value := range []string{"0", "1"} {
		if _, e = s.pool.Exec(ctx, "UPDATE merchants SET profile=jsonb_set(profile,'{settle}',$1::jsonb) WHERE id=16001", value); e != nil {
			t.Fatal(e)
		}
	}
	if e = s.pool.QueryRow(ctx, "SELECT settlement_permission_revision FROM merchants WHERE id=16001").Scan(&after); e != nil || after != original+2 {
		t.Fatal("permission toggle revision not monotonic", original, after, e)
	}
	if _, e = s.CreateSettlementPlanRun(ctx, p.ID, p.Revision, settlementPlanAdmin); !errors.Is(e, ErrConflict) {
		t.Fatal("off/on rearmed old future authorization", e)
	}
	if e = s.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
		t.Fatal(e)
	}
	view, e := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if e != nil || len(view.Items) != 1 || view.Items[0].Reason != "scope_changed" || view.CreatedCount != 0 {
		t.Fatal("queued authority silently expanded", view, e)
	}
	old, e := s.readSettlementPlanAuthorization(ctx, s.pool, a.ID)
	if e != nil || old.View.ID != a.ID {
		t.Fatal("original authenticated proof was rewritten", old, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE merchants SET settlement_permission_revision=100 WHERE id=16001"); e == nil {
		t.Fatal("direct permission revision override accepted")
	}
}

func TestOriginalMerchantSettlementPermissionBlocksUnsentTransferBatch(t *testing.T) {
	s, g, p := batchFixture(t)
	ctx := context.Background()
	batch, e := g.PrepareTransferBatch(ctx, 501, "settle160-batch", batchInputs(100), batchMerchantActor)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"settle":0}'::jsonb WHERE id=501`); e != nil {
		t.Fatal(e)
	}
	before, e := s.Balance(ctx, 501)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = g.ExecuteTransferBatch(ctx, 501, batch.ID, 1, batchMerchantActor, nil); e == nil {
		t.Fatal("closed merchant sent an unsent batch")
	}
	replay, e := g.PrepareTransferBatch(ctx, 501, "settle160-batch", batchInputs(100), batchMerchantActor)
	if e != nil || replay.ID != batch.ID {
		t.Fatal("original prepared batch replay lost", replay, e)
	}
	view, e := s.TransferBatch(ctx, 501, batch.ID)
	if e != nil || view.Summary.Unsent != 1 || p.transfers.Load() != 0 {
		t.Fatal("permission failure consumed original unsent item", view, e)
	}
	after, e := s.Balance(ctx, 501)
	if e != nil || after != before {
		t.Fatal("blocked batch changed reservation", before, after, e)
	}
	if _, e = g.PrepareTransferBatch(ctx, 501, "settle160-new-batch", batchInputs(100), batchMerchantActor); e == nil {
		t.Fatal("closed merchant prepared a new batch")
	}
	if _, e = g.QueryTransfer(ctx, 501, batch.Items[0].Transfer.TransferNo); e != nil || p.query.Load() != 0 {
		t.Fatal("unsent original local query blocked", e)
	}
}

func TestOriginalMerchantSettlementPermissionBlocksUnsentPayoutButAllowsUnknownQuery(t *testing.T) {
	for _, sent := range []bool{false, true} {
		t.Run(map[bool]string{false: "unsent", true: "unknown"}[sent], func(t *testing.T) {
			s, g, p, request := payoutFixture(t)
			ctx := context.Background()
			p.unknown = true
			original, e := g.PrepareSettlementPayout(ctx, request.ID, 7, settlementAdmin, nil)
			if e != nil {
				t.Fatal(e)
			}
			if sent {
				_, e = g.ExecuteSettlementPayout(ctx, request.ID, settlementAdmin, nil)
				var unknown *adapter.UnknownOutcome
				if !errors.As(e, &unknown) {
					t.Fatal("test provider did not create an actual unknown outcome", e)
				}
			}
			if _, e = s.pool.Exec(ctx, `UPDATE merchants SET profile=profile||'{"settle":0}'::jsonb WHERE id=501`); e != nil {
				t.Fatal(e)
			}
			before, e := s.Balance(ctx, 501)
			if e != nil {
				t.Fatal(e)
			}
			replay, e := g.PrepareSettlementPayout(ctx, request.ID, 7, settlementAdmin, nil)
			if e != nil || replay.TransferID != original.TransferID {
				t.Fatal("original payout replay changed", replay, e)
			}
			_, e = g.ExecuteSettlementPayout(ctx, request.ID, settlementAdmin, nil)
			if !sent && e == nil {
				t.Fatal("closed merchant sent unsent payout")
			}
			if sent && e != nil {
				t.Fatal("unknown original replay blocked", e)
			}
			wantCalls := int32(0)
			if sent {
				wantCalls = 1
			}
			if p.transfers.Load() != wantCalls {
				t.Fatal("unexpected payout provider send", p.transfers.Load())
			}
			after, e := s.Balance(ctx, 501)
			if e != nil || after != before {
				t.Fatal("blocked or replayed payout changed funds", before, after, e)
			}
			queried, e := g.QuerySettlementPayout(ctx, request.ID, settlementAdmin)
			if e != nil {
				t.Fatal("original payout query blocked", e)
			}
			if sent {
				if queried.State != "succeeded" || p.query.Load() != 1 {
					t.Fatal("original unknown outcome not reconciled", queried, p.query.Load())
				}
				balance, e := s.Balance(ctx, 501)
				if e != nil || balance.Available != 0 || balance.Reserved != 0 {
					t.Fatal("original outcome not posted exactly", balance, e)
				}
			} else if queried.State != "unsent" || p.query.Load() != 0 {
				t.Fatal("unsent query touched provider", queried, p.query.Load())
			}
		})
	}
}

func TestOriginalMerchantSettlementPermissionSQLProjectionRejectsInvalidValues(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	for _, tc := range []struct{ profile, want string }{
		{`{}`, "1"}, {`{"settle":0}`, "0"}, {`{"settle":"1"}`, "1"}, {`{"settle_status":"0"}`, "0"},
		{`{"settle":0,"settle_status":1}`, "0"}, {`{"settle":null,"settle_status":1}`, "invalid"},
		{`{"settle":true}`, "invalid"}, {`{"settle":"01"}`, "invalid"}, {`{"settle":1.0}`, "invalid"}, {`[]`, "invalid"},
	} {
		var got string
		if e := s.pool.QueryRow(ctx, "SELECT merchant_settlement_permission($1::jsonb)", tc.profile).Scan(&got); e != nil || got != tc.want {
			t.Fatalf("profile %s: got %q want %q: %v", tc.profile, got, tc.want, e)
		}
	}
}

func TestOriginalMerchantSettlementPermissionMigrationPreservesAuthenticHistory(t *testing.T) {
	s, facts := actualHistorical73Store(t)
	ctx := context.Background()
	paths, e := filepath.Glob("../../db/migrations/*.up.sql")
	if e != nil {
		t.Fatal(e)
	}
	apply := func(path string) {
		t.Helper()
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.pool.Exec(ctx, string(raw)); e != nil {
			t.Fatal(filepath.Base(path), e)
		}
	}
	for _, path := range paths {
		version, e := strconv.Atoi(filepath.Base(path)[:6])
		if e != nil {
			t.Fatal(e)
		}
		if version > 73 && version < 93 {
			apply(path)
		}
	}
	rows, e := s.pool.Query(ctx, "SELECT tablename FROM pg_tables WHERE schemaname=current_schema() ORDER BY tablename")
	if e != nil {
		t.Fatal(e)
	}
	var tables []string
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			t.Fatal(e)
		}
		tables = append(tables, name)
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	rows.Close()
	hash := func(table string) string {
		t.Helper()
		expr := "to_jsonb(t)"
		if table == "merchants" {
			expr = "(to_jsonb(t)-'settlement_permission_revision')"
		}
		var value string
		if e := s.pool.QueryRow(ctx, "SELECT md5(COALESCE(jsonb_agg("+expr+" ORDER BY "+expr+"::text),'[]'::jsonb)::text) FROM "+pgx.Identifier{table}.Sanitize()+" t").Scan(&value); e != nil {
			t.Fatal(table, e)
		}
		return value
	}
	before := map[string]string{}
	for _, table := range tables {
		before[table] = hash(table)
	}
	apply("../../db/migrations/000093_merchant_settlement_permission.up.sql")
	for _, table := range tables {
		if got := hash(table); got != before[table] {
			t.Fatal("permission migration rewrote original row", table)
		}
	}
	var invalid int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM merchants WHERE settlement_permission_revision<>1").Scan(&invalid); e != nil || invalid != 0 {
		t.Fatal("historical merchant initial permission revision", invalid, e)
	}
	original, e := s.readSettlementPlanAuthorization(ctx, s.pool, facts.AuthorizationID)
	if e != nil || original.View.ID != facts.AuthorizationID {
		t.Fatal("old authenticated authorization no longer readable", e)
	}
	if _, e = s.SettlementPlanRun(ctx, facts.RunID, settlementPlanAdmin); e != nil {
		t.Fatal("old authenticated run no longer readable", e)
	}
	if report, e := s.CheckLedger(ctx); e != nil || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
		t.Fatal("historical financial invariants changed", report, e)
	}
	t.Logf("Preserved every original column in %d tables, including original financial scopes and proofs", len(tables))
}

func TestOriginalMerchantSettlementPermissionCannotReselectThresholdScope(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	run := thresholdRun74(t, s, 16074)
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{settle}','0'::jsonb) WHERE id=16074`); e != nil {
		t.Fatal(e)
	}
	if e := s.GenerateSettlementPlanRun(ctx, run.ID); !errors.Is(e, ErrState) {
		t.Fatal("closed merchant remained in threshold generation", e)
	}
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{settle}','1'::jsonb) WHERE id=16074`); e != nil {
		t.Fatal(e)
	}
	if e := s.GenerateSettlementPlanRun(ctx, run.ID); !errors.Is(e, ErrConflict) {
		t.Fatal("threshold original future scope revived after off/on", e)
	}
	var count int
	if e := s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM settlement_threshold_selections WHERE run_id=$1)+(SELECT count(*) FROM settlement_plan_items WHERE run_id=$1)+(SELECT count(*) FROM ledger_requests WHERE merchant_id=16074)", run.ID).Scan(&count); e != nil || count != 0 {
		t.Fatal("changed threshold scope selected or reserved new funds", count, e)
	}
	if _, e := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin); e != nil {
		t.Fatal("original threshold run lost authenticated read", e)
	}
}
