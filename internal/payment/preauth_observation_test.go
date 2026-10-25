package payment

import (
	"context"
	"encoding/json"
	adapter "goepay/internal/adapters/payment"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

type observationProvider68 struct {
	preauthFlowProvider
	reads     atomic.Int32
	remaining int64
	known     bool
	fail      error
	mutate    func(*adapter.AuthorizationEvent)
	before    func()
}

func (*observationProvider68) ApplicationID() string           { return "2021000000000068" }
func (*observationProvider68) TransferAccountIdentity() string { return "seller:2088000000000068" }
func (p *observationProvider68) QueryAuthorization(_ context.Context, r adapter.AuthorizationRequest) (adapter.AuthorizationEvent, error) {
	p.reads.Add(1)
	if p.before != nil {
		p.before()
	}
	ev := adapter.AuthorizationEvent{TradeNo: r.TradeNo, RequestNo: r.RequestNo, AuthNo: r.AuthNo, BuyerID: "2088000000001068", Amount: r.Amount, RestAmount: p.remaining, RestAmountKnown: p.known, OperationType: "FREEZE", State: "SUCCESS", OrderStatus: "AUTHORIZED"}
	if p.mutate != nil {
		p.mutate(&ev)
	}
	return ev, p.fail
}
func observationFixture68(t *testing.T) (*Store, ProviderFlow, Order, string, *observationProvider68) {
	t.Helper()
	ctx := context.Background()
	s := testStore(t)
	token := strings.Repeat("p", 43)
	lookupTestSession(t, s, token, "observation-owner68", 1068)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 1068, OutTradeNo: "observation-original68", Name: "synthetic authorization", Type: "alipay", Amount: 1000})
	if e != nil {
		t.Fatal(e)
	}
	p := &observationProvider68{remaining: 450, known: true}
	scope, e := adapter.TransferAccountScope(p)
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = s.BeginProviderAttemptWithAccount(ctx, o.TradeNo, 7, "preauth", p.ApplicationID(), "", scope); e != nil {
		t.Fatal(e)
	}
	if e = s.RecordPreauthorization(ctx, 7, adapter.AuthorizationEvent{TradeNo: o.TradeNo, RequestNo: o.TradeNo, AuthNo: "original-auth68", BuyerID: "2088000000001068", Amount: 1000, State: "SUCCESS"}); e != nil {
		t.Fatal(e)
	}
	return s, ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{7: p}}, o, token, p
}
func observationCheck68(context.Context) error { return nil }
func observationFinancial68(t *testing.T, s *Store, trade string) string {
	t.Helper()
	var raw []byte
	if e := s.pool.QueryRow(context.Background(), `SELECT jsonb_build_array((SELECT count(*) FROM ledger_entries),(SELECT count(*) FROM jobs),(SELECT count(*) FROM verified_payment_receipts),(SELECT count(*) FROM provider_operations),(SELECT status FROM orders WHERE trade_no=$1),(SELECT jsonb_build_array(capture_state,release_state) FROM payment_authorizations a JOIN orders o ON o.id=a.order_id WHERE o.trade_no=$1))`, trade).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func TestPreauthObservation68SingleReadAndNoFinancialMutation(t *testing.T) {
	s, g, o, token, p := observationFixture68(t)
	ctx := context.Background()
	before := observationFinancial68(t, s, o.TradeNo)
	in := PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("a", 64)}
	var wg sync.WaitGroup
	var last PreauthorizationObservation
	var mu sync.Mutex
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q, e := g.ObservePreauthorization(ctx, in, token, "merchant", observationCheck68)
			if e == nil {
				mu.Lock()
				last = q
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if p.reads.Load() != 1 {
		t.Fatal("duplicate original query", p.reads.Load())
	}
	q, e := g.ReadPreauthorizationObservation(ctx, last.ID, token, "merchant", observationCheck68)
	if e != nil || q.Status != "verified" || !q.RestKnown || q.RestAmount == nil || *q.RestAmount != 450 || !q.SourceVerified {
		t.Fatal(q, e)
	}
	raw, _ := json.Marshal(q)
	if !strings.Contains(string(raw), `"amount":"1000"`) || !strings.Contains(string(raw), `"rest_amount":"450"`) {
		t.Fatal("money not string", string(raw))
	}
	if after := observationFinancial68(t, s, o.TradeNo); after != before {
		t.Fatal("query changed financial state", before, after)
	}
	if _, e = g.ObservePreauthorization(ctx, in, token, "merchant", observationCheck68); e != nil || p.reads.Load() != 1 {
		t.Fatal("replay queried again", e, p.reads.Load())
	}
}

func TestPreauthObservation68UnknownAndBadEvidenceNeverInventRemainder(t *testing.T) {
	for _, kind := range []string{"missing-rest", "wrong-trade", "wrong-request", "wrong-auth", "wrong-buyer", "wrong-amount", "too-large-rest", "negative-rest", "wrong-operation", "wrong-state", "wrong-order-status", "transport"} {
		t.Run(kind, func(t *testing.T) {
			s, g, o, token, p := observationFixture68(t)
			ctx := context.Background()
			p.mutate = func(e *adapter.AuthorizationEvent) {
				switch kind {
				case "missing-rest":
					e.RestAmountKnown = false
				case "wrong-trade":
					e.TradeNo = "other"
				case "wrong-request":
					e.RequestNo = "UA-other"
				case "wrong-auth":
					e.AuthNo = "other"
				case "wrong-buyer":
					e.BuyerID = "2088000000009999"
				case "wrong-amount":
					e.Amount++
				case "too-large-rest":
					e.RestAmount = e.Amount + 1
				case "negative-rest":
					e.RestAmount = -1
				case "wrong-operation":
					e.OperationType = "UNFREEZE"
				case "wrong-state":
					e.State = "PROCESSING"
				case "wrong-order-status":
					e.OrderStatus = "UNEXPECTED"
				}
			}
			if kind == "transport" {
				p.fail = context.DeadlineExceeded
			}
			in := PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("c", 64)}
			before := observationFinancial68(t, s, o.TradeNo)
			q, e := g.ObservePreauthorization(ctx, in, token, "merchant", observationCheck68)
			if e != nil || q.Status != "unknown" || q.RestKnown || q.RestAmount != nil || q.SourceVerified {
				t.Fatal(q, e)
			}
			raw, _ := json.Marshal(q)
			if strings.Contains(string(raw), `"rest_amount"`) {
				t.Fatal("unknown became zero", string(raw))
			}
			if _, e = g.ObservePreauthorization(ctx, in, token, "merchant", observationCheck68); e != nil || p.reads.Load() != 1 {
				t.Fatal("unknown original query repeated", e, p.reads.Load())
			}
			if observationFinancial68(t, s, o.TradeNo) != before {
				t.Fatal("unknown changed financial tables")
			}
		})
	}
}
func TestPreauthObservation68OriginalSessionAndMerchantRevisionDuringIO(t *testing.T) {
	for _, kind := range []string{"delete-session", "revoke-user", "merchant-off-on", "policy-lost"} {
		t.Run(kind, func(t *testing.T) {
			s, g, o, token, p := observationFixture68(t)
			ctx := context.Background()
			var lost atomic.Bool
			p.before = func() {
				switch kind {
				case "delete-session":
					_, _ = s.pool.Exec(ctx, `DELETE FROM console_sessions WHERE token_hash=$1`, lookupSessionHash(token))
				case "revoke-user":
					_, _ = s.pool.Exec(ctx, `UPDATE console_users SET enabled=false WHERE username='observation-owner68'`)
				case "merchant-off-on":
					for _, enabled := range []bool{false, true} {
						m, e := s.Merchant(ctx, o.MerchantID)
						if e != nil {
							t.Fatal(e)
						}
						m.Enabled = enabled
						if e = s.ReplaceMerchantAtomic(ctx, m, []byte(`{}`), nil); e != nil {
							t.Fatal(e)
						}
					}
				case "policy-lost":
					lost.Store(true)
				}
			}
			check := func(context.Context) error {
				if lost.Load() {
					return ErrState
				}
				return nil
			}
			q, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("d", 64)}, token, "merchant", check)
			if e == nil || q.SourceVerified || q.RestAmount != nil || p.reads.Load() != 1 {
				t.Fatal("revoked query returned evidence", q, e, p.reads.Load())
			}
			var count int
			if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM preauth_observations WHERE status='verified'`).Scan(&count); e != nil || count != 0 {
				t.Fatal("revoked query persisted verified evidence", count, e)
			}
		})
	}
}
func TestPreauthObservation68AuditRollbackAndNoRetryAfterResultLoss(t *testing.T) {
	s, g, o, token, p := observationFixture68(t)
	ctx := context.Background()
	in := PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("e", 64)}
	if _, e := s.pool.Exec(ctx, `CREATE FUNCTION fault_observation68() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='claimed' THEN RAISE EXCEPTION 'synthetic claim audit fault';END IF;RETURN NEW;END$$;CREATE TRIGGER fault_observation68 BEFORE INSERT ON preauth_observation_events FOR EACH ROW EXECUTE FUNCTION fault_observation68()`); e != nil {
		t.Fatal(e)
	}
	if _, e := g.ObservePreauthorization(ctx, in, token, "merchant", observationCheck68); e == nil || p.reads.Load() != 0 {
		t.Fatal("uncommitted query audit allowed network", e, p.reads.Load())
	}
	var rows int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM preauth_observations`).Scan(&rows); e != nil || rows != 0 {
		t.Fatal("claim did not roll back", rows, e)
	}
	if _, e := s.pool.Exec(ctx, `CREATE OR REPLACE FUNCTION fault_observation68() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='verified' THEN RAISE EXCEPTION 'synthetic result audit fault';END IF;RETURN NEW;END$$`); e != nil {
		t.Fatal(e)
	}
	if _, e := g.ObservePreauthorization(ctx, in, token, "merchant", observationCheck68); e == nil || p.reads.Load() != 1 {
		t.Fatal("result fault fixture", e, p.reads.Load())
	}
	if _, e := s.pool.Exec(ctx, `DROP TRIGGER fault_observation68 ON preauth_observation_events`); e != nil {
		t.Fatal(e)
	}
	q, e := g.ObservePreauthorization(ctx, in, token, "merchant", observationCheck68)
	if e != nil || q.Status != "unknown" || p.reads.Load() != 1 {
		t.Fatal("lost result repeated original query", q, e, p.reads.Load())
	}
	in.ClientRequestNo = strings.Repeat("f", 64)
	q, e = g.ObservePreauthorization(ctx, in, token, "merchant", observationCheck68)
	if e != nil || q.Status != "verified" || p.reads.Load() != 2 {
		t.Fatal("new explicit id could not read", q, e, p.reads.Load())
	}
}
func TestPreauthObservation68CurrentOwnerHistoricalReadAndDelegation(t *testing.T) {
	s, g, o, token, p := observationFixture68(t)
	ctx := context.Background()
	p.remaining = 0
	q, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("1", 64)}, token, "merchant", observationCheck68)
	if e != nil || q.RestAmount == nil || *q.RestAmount != 0 || !q.RestKnown {
		t.Fatal(q, e)
	}
	next := strings.Repeat("n", 43)
	if _, e = s.pool.Exec(ctx, `DELETE FROM console_sessions WHERE token_hash=$1`, lookupSessionHash(token)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO console_sessions(token_hash,username,role,subject_id,csrf,expires_at,auth_version)SELECT $1,username,role,subject_id,'synthetic',clock_timestamp()+interval '1 hour',auth_version FROM console_users WHERE username='observation-owner68'`, lookupSessionHash(next)); e != nil {
		t.Fatal(e)
	}
	if _, e = g.ReadPreauthorizationObservation(ctx, q.ID, token, "merchant", observationCheck68); e == nil {
		t.Fatal("deleted original session read")
	}
	historical, e := g.ReadPreauthorizationObservation(ctx, q.ID, next, "merchant", observationCheck68)
	if e != nil || !historical.SourceVerified || p.reads.Load() != 1 {
		t.Fatal("fresh own session could not read history without network", e, p.reads.Load())
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO console_session_issuers(token_hash,issuer_session_hash,issuer_username,issuer_id,issuer_auth_version)VALUES($1,$2,'synthetic-issuer',1,1)`, lookupSessionHash(next), lookupSessionHash(strings.Repeat("i", 43))); e != nil {
		t.Fatal(e)
	}
	if _, e = g.ReadPreauthorizationObservation(ctx, q.ID, next, "merchant", observationCheck68); e == nil {
		t.Fatal("delegated session read history")
	}
}
func TestPreauthObservation68HMACAndMissingAuditRejectOfflineDamage(t *testing.T) {
	for _, kind := range []string{"result", "source", "missing-audit"} {
		t.Run(kind, func(t *testing.T) {
			s, g, o, token, _ := observationFixture68(t)
			ctx := context.Background()
			q, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("2", 64)}, token, "merchant", observationCheck68)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = s.pool.Exec(ctx, `UPDATE preauth_observations SET rest_amount=rest_amount+1 WHERE id=$1`, q.ID); e == nil {
				t.Fatal("immutable result changed")
			}
			switch kind {
			case "result":
				_, e = s.pool.Exec(ctx, `ALTER TABLE preauth_observations DISABLE TRIGGER USER;UPDATE preauth_observations SET rest_amount=rest_amount+1;ALTER TABLE preauth_observations ENABLE TRIGGER USER`)
			case "source":
				_, e = s.pool.Exec(ctx, `ALTER TABLE preauth_observations DISABLE TRIGGER USER;UPDATE preauth_observations SET app_id='2021000000009999';ALTER TABLE preauth_observations ENABLE TRIGGER USER`)
			case "missing-audit":
				_, e = s.pool.Exec(ctx, `ALTER TABLE preauth_observation_events DISABLE TRIGGER USER;DELETE FROM preauth_observation_events WHERE operation='verified';ALTER TABLE preauth_observation_events ENABLE TRIGGER USER`)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = g.ReadPreauthorizationObservation(ctx, q.ID, token, "merchant", observationCheck68); e == nil {
				t.Fatal("damaged observation accepted", kind)
			}
		})
	}
}
func TestPreauthObservation68MandatoryPolicyAndOwnerBeforeQuery(t *testing.T) {
	s, g, o, token, p := observationFixture68(t)
	ctx := context.Background()
	in := PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("3", 64)}
	if _, e := g.ObservePreauthorization(ctx, in, token, "merchant", nil); e == nil {
		t.Fatal("nil policy accepted")
	}
	if _, e := g.ObservePreauthorization(ctx, in, strings.Repeat("x", 43), "merchant", observationCheck68); e == nil {
		t.Fatal("fake session")
	}
	if _, e := g.ObservePreauthorization(ctx, in, token, "admin", observationCheck68); e == nil {
		t.Fatal("caller relabelled admin")
	}
	lookupTestSession(t, s, strings.Repeat("y", 43), "observation-other68", 1069)
	if _, e := g.ObservePreauthorization(ctx, in, strings.Repeat("y", 43), "merchant", observationCheck68); e == nil {
		t.Fatal("foreign owner")
	}
	if p.reads.Load() != 0 {
		t.Fatal("unauthorized vendor query", p.reads.Load())
	}
}

type observationChangedAccount68 struct{ *observationProvider68 }

func (*observationChangedAccount68) TransferAccountIdentity() string {
	return "seller:2088000000009999"
}

type observationWeakAccount68 struct{ *observationProvider68 }

func (p *observationWeakAccount68) TransferAccountIdentity() string { return p.ApplicationID() }
func TestPreauthObservation68PhysicalAccountChangeAndOriginalProof(t *testing.T) {
	s, g, o, token, p := observationFixture68(t)
	ctx := context.Background()
	in := PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("5", 64)}
	if _, _, _, e := observationProvider(&observationWeakAccount68{p}); e == nil {
		t.Fatal("AppID fallback treated as physical seller")
	}
	p.before = func() { g.Providers[7] = &observationChangedAccount68{p} }
	if q, e := g.ObservePreauthorization(ctx, in, token, "merchant", observationCheck68); e == nil || q.SourceVerified || q.RestAmount != nil {
		t.Fatal("changed physical seller returned evidence", q, e)
	}
	g.Providers[7] = p
	p.before = nil
	q, e := g.ObservePreauthorization(ctx, in, token, "merchant", observationCheck68)
	if e != nil || q.Status != "unknown" || p.reads.Load() != 1 {
		t.Fatal("restoring seller revived old observation", q, e, p.reads.Load())
	}
	if _, e = s.pool.Exec(ctx, `ALTER TABLE provider_attempt_accounts DISABLE TRIGGER USER;DELETE FROM provider_attempt_accounts;ALTER TABLE provider_attempt_accounts ENABLE TRIGGER USER`); e != nil {
		t.Fatal(e)
	}
	in.ClientRequestNo = strings.Repeat("6", 64)
	if _, e = g.ObservePreauthorization(ctx, in, token, "merchant", observationCheck68); e == nil || p.reads.Load() != 1 {
		t.Fatal("historical original account proof inferred", e, p.reads.Load())
	}
}
func TestPreauthObservation68PermissionLostAfterClaimBeforeRead(t *testing.T) {
	s, g, o, token, p := observationFixture68(t)
	ctx := context.Background()
	var checks atomic.Int32
	check := func(context.Context) error {
		if checks.Add(1) > 1 {
			return ErrState
		}
		return nil
	}
	if _, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("7", 64)}, token, "merchant", check); e == nil || p.reads.Load() != 0 {
		t.Fatal("policy lost before read still queried", e, p.reads.Load())
	}
	var count int
	if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM preauth_observations WHERE status='unknown'`).Scan(&count); e != nil || count != 1 {
		t.Fatal("claimed query did not become unknown", count, e)
	}
}

func TestPreauthObservation68DeadlineRecheckedAfterAudit(t *testing.T) {
	for _, phase := range []string{"claimed", "verified"} {
		t.Run(phase, func(t *testing.T) {
			s, g, o, token, p := observationFixture68(t)
			ctx := context.Background()
			if _, e := s.pool.Exec(ctx, `UPDATE console_sessions SET expires_at=clock_timestamp()+interval '2 seconds' WHERE token_hash=$1`, lookupSessionHash(token)); e != nil {
				t.Fatal(e)
			}
			sql := `CREATE FUNCTION delay_observation68() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.operation='` + phase + `' THEN PERFORM pg_sleep(2.2);END IF;RETURN NEW;END$$;CREATE TRIGGER delay_observation68 BEFORE INSERT ON preauth_observation_events FOR EACH ROW EXECUTE FUNCTION delay_observation68()`
			if _, e := s.pool.Exec(ctx, sql); e != nil {
				t.Fatal(e)
			}
			if q, e := g.ObservePreauthorization(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("8", 64)}, token, "merchant", observationCheck68); e == nil || q.SourceVerified || q.RestAmount != nil {
				t.Fatal("expired observation published", q, e)
			}
			expected := int32(0)
			if phase == "verified" {
				expected = 1
			}
			if p.reads.Load() != expected {
				t.Fatal("network claim crossed deadline", p.reads.Load(), expected)
			}
			var verified int
			if e := s.pool.QueryRow(ctx, `SELECT count(*) FROM preauth_observations WHERE status='verified'`).Scan(&verified); e != nil || verified != 0 {
				t.Fatal("expired observation result committed", verified, e)
			}
		})
	}
}

func TestPreauthObservation68SQLRequiresExplicitVerifiedRemainder(t *testing.T) {
	s, _, o, token, p := observationFixture68(t)
	ctx := context.Background()
	scope, e := adapter.TransferAccountScope(p)
	if e != nil {
		t.Fatal(e)
	}
	q, owner, e := s.beginObservation(ctx, PreauthObservationInput{MerchantID: o.MerchantID, TradeNo: o.TradeNo, ClientRequestNo: strings.Repeat("9", 64)}, token, "merchant", p.ApplicationID(), scope)
	if e != nil || !owner {
		t.Fatal(e)
	}
	for _, item := range []struct {
		name    string
		amount  any
		allowed bool
	}{{"nil", nil, false}, {"zero", int64(0), true}, {"negative", int64(-1), false}, {"above-original", int64(1001), false}, {"exact-original", int64(1000), true}} {
		t.Run(item.name, func(t *testing.T) {
			tx, e := s.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			_, e = tx.Exec(ctx, `UPDATE preauth_observations SET status='verified',rest_amount=$2,provider_status='SUCCESS',order_status='AUTHORIZED',operation_type='FREEZE',observed_at=clock_timestamp(),result_proof=decode(repeat('00',32),'hex') WHERE id=$1`, q.ID, item.amount)
			if item.allowed && e != nil {
				t.Fatal("valid explicit remainder rejected", e)
			}
			if !item.allowed && e == nil {
				t.Fatal("invalid verified remainder accepted by SQL", item.name)
			}
		})
	}
	if p.reads.Load() != 0 {
		t.Fatal("SQL constraint test called provider")
	}
}
