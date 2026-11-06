package server

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/identity"
	"goepay/internal/payment"
	"goepay/internal/paymentdefend"
)

type defendAdmission206Fixture struct {
	channel168Fixture
	observer *pgxpool.Pool
	key      *rsa.PrivateKey
	app      string
}

// All business readers/writers use the replacement pool. The original fixture
// pool is retained only for test observations and test-owned lock barriers.
func defendAdmission206Local(t *testing.T, rsaMode, singleConnection bool) defendAdmission206Fixture {
	t.Helper()
	f, key := paymentDefend206WireFixture(t, rsaMode)
	observer := f.p
	var schema string
	if err := observer.QueryRow(t.Context(), `SELECT current_schema()`).Scan(&schema); err != nil {
		t.Fatal(err)
	}
	config := observer.Config()
	app := "defend206-" + schema
	config.ConnConfig.RuntimeParams["application_name"] = app
	config.MinConns = 0
	if singleConnection {
		config.MaxConns = 1
	}
	pool, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	store, err := payment.NewWithKey(pool, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := paymentdefend.NewStore(pool, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	f.p, f.s.Pool, f.s.Store = pool, pool, store
	f.s.ProviderFlow.Store = store
	f.s.Sessions = identity.NewPersistentSessions(pool, false)
	f.s.PaymentDefend206 = policy
	return defendAdmission206Fixture{f, observer, key, app}
}

func (f defendAdmission206Fixture) wire(t *testing.T, ctx context.Context, path, original string) *httptest.ResponseRecorder {
	t.Helper()
	p := compat.Params{"pid": "1", "out_trade_no": original, "type": "alipay", "name": "Synthetic admission206", "money": "1.23", "channel_id": "7", "method": "native", "clientip": "192.0.2.206", "notify_url": "https://merchant.example.invalid/notify", "return_url": "https://merchant.example.invalid/return", "sign_type": "MD5"}
	if f.key == nil {
		p["sign"] = compat.SignMD5WithProfile(p, "test-secret", compat.ProfileProServer)
	} else {
		p["sign_type"], p["timestamp"] = "RSA", strconv.FormatInt(time.Now().Unix(), 10)
		var err error
		p["sign"], err = compat.SignRSAWithProfile(p, f.key, compat.ProfileProServer)
		if err != nil {
			t.Fatal(err)
		}
	}
	form := url.Values{}
	for key, value := range p {
		form.Set(key, value)
	}
	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode())).WithContext(ctx)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.RemoteAddr = "192.0.2.206:5206"
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func (f defendAdmission206Fixture) configure(ctx context.Context, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPatch, paymentDefend206Path, strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", f.csrf)
	r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	return w
}

func defendAdmission206Configured(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var out struct {
		Code int `json:"code"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Code != 0 {
		t.Fatalf("original policy did not commit through HTTP: HTTP=%d body=%s", w.Code, w.Body.String())
	}
}

func defendAdmission206Blocked(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out["code"] != float64(-1) {
		t.Errorf("new payment was not fail-closed: HTTP=%d body=%s err=%v", w.Code, w.Body.String(), err)
	}
	if w.Header().Get("Location") != "" {
		t.Error("blocked payment returned an executable redirect")
	}
	for _, key := range []string{"payurl", "pay_info", "material", "qrcode", "prepay_id", "url"} {
		if _, ok := out[key]; ok {
			t.Errorf("blocked payment returned payment material %q", key)
		}
	}
}

func (f defendAdmission206Fixture) effects(t *testing.T) string {
	t.Helper()
	readOnlyFixture := f.channel168Fixture
	readOnlyFixture.p = f.observer
	return paymentDefend206Effects(t, readOnlyFixture)
}

func (f defendAdmission206Fixture) counts(t *testing.T) (routes, orders, attempts, jobs int) {
	t.Helper()
	if err := f.observer.QueryRow(t.Context(), `SELECT (SELECT count(*) FROM provider_route_assignments),(SELECT count(*) FROM orders),(SELECT count(*) FROM provider_attempts),(SELECT count(*) FROM jobs)`).Scan(&routes, &orders, &attempts, &jobs); err != nil {
		t.Fatal(err)
	}
	return
}

func defendAdmission206Await(t *testing.T, ctx context.Context, done <-chan *httptest.ResponseRecorder) *httptest.ResponseRecorder {
	t.Helper()
	select {
	case out := <-done:
		return out
	case <-ctx.Done():
		t.Fatal("bounded original HTTP request did not finish", ctx.Err())
		return nil
	}
}

func TestPaymentDefend206AdmissionEnabledBeforeAllFourWireRoutesHasNoPaymentEffects(t *testing.T) {
	for _, path := range []string{"/mapi.php", "/submit.php", "/api/pay/create", "/api/pay/submit"} {
		t.Run(path, func(t *testing.T) {
			f := defendAdmission206Local(t, strings.HasPrefix(path, "/api/"), false)
			paymentDefend206Enable(t, f.channel168Fixture)
			before := f.effects(t)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			defendAdmission206Blocked(t, f.wire(t, ctx, path, "defend206-before-route"))
			if f.effects(t) != before || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 0 {
				t.Fatal("enabled policy was checked after route/order/attempt/queue work or provider I/O")
			}
		})
	}
}

func TestPaymentDefend206AdmissionPolicyChangeAtCommittedRouteAndOrderBoundariesStopsFirstSend(t *testing.T) {
	for _, phase := range []string{"route", "order"} {
		t.Run(phase, func(t *testing.T) {
			f := defendAdmission206Local(t, false, false)
			before := paymentDefend206Read(t, f.channel168Fixture)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			var schema string
			if err := f.observer.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil {
				t.Fatal(err)
			}
			key := schema + "/defend206/" + phase
			held, err := f.observer.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer held.Rollback(context.Background())
			if _, err = held.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,206))`, key); err != nil {
				t.Fatal(err)
			}
			table := "provider_route_assignments"
			if phase == "order" {
				table = "orders"
			}
			statement := fmt.Sprintf(`CREATE FUNCTION defend206_stage_barrier() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(hashtextextended(current_schema()||'/defend206/%s',206)); RETURN NEW; END $$; CREATE TRIGGER defend206_stage_barrier AFTER INSERT ON %s FOR EACH ROW EXECUTE FUNCTION defend206_stage_barrier()`, phase, table)
			if _, err = f.observer.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
			paymentDone := make(chan *httptest.ResponseRecorder, 1)
			go func() { paymentDone <- f.wire(t, ctx, "/mapi.php", "defend206-stage-"+phase) }()
			var paymentPID int
			for paymentPID == 0 {
				if err = f.observer.QueryRow(ctx, `SELECT COALESCE((SELECT pid FROM pg_stat_activity WHERE application_name=$1 AND $2::int=ANY(pg_blocking_pids(pid)) LIMIT 1),0)`, f.app, int(held.Conn().PgConn().PID())).Scan(&paymentPID); err != nil {
					t.Fatal(err)
				}
				if paymentPID != 0 {
					break
				}
				select {
				case early := <-paymentDone:
					t.Fatal("valid off-policy payment did not reach the real PG stage barrier", early.Code, early.Body.String())
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			changed := make(chan *httptest.ResponseRecorder, 1)
			body := paymentDefend206Body(t, before, true)
			go func() { changed <- f.configure(ctx, body) }()
			var configured *httptest.ResponseRecorder
			// Old code may let the writer commit immediately. Correct staged
			// locks queue the writer behind this stage and ahead of the next.
			for configured == nil {
				select {
				case configured = <-changed:
				default:
				}
				if configured != nil {
					break
				}
				var queued bool
				if err = f.observer.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=$1 AND pid<>$2 AND $2::int=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock' AND query LIKE '%payment-defend206/policy%')`, f.app, paymentPID).Scan(&queued); err != nil {
					t.Fatal(err)
				}
				if queued {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("policy writer neither committed nor queued at the controlled stage", ctx.Err())
				case <-time.After(10 * time.Millisecond):
				}
			}
			if err = held.Rollback(ctx); err != nil {
				t.Fatal(err)
			}
			if configured == nil {
				configured = defendAdmission206Await(t, ctx, changed)
			}
			defendAdmission206Configured(t, configured)
			defendAdmission206Blocked(t, defendAdmission206Await(t, ctx, paymentDone))
			routes, orders, attempts, jobs := f.counts(t)
			wantOrders := 0
			if phase == "order" {
				wantOrders = 1
			}
			// Earlier successfully committed route/order facts are retained.
			// This test intentionally does not demand their rollback when the
			// policy changes between the existing separate transactions.
			if routes != 1 || orders != wantOrders || attempts != 0 || jobs != 0 || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 0 {
				t.Fatalf("later policy did not stop the first send after prior committed %s: routes=%d orders=%d attempts=%d jobs=%d", phase, routes, orders, attempts, jobs)
			}
		})
	}
}

func TestPaymentDefend206AdmissionPolicyCommittedBeforeClaimStopsFirstSend(t *testing.T) {
	f := defendAdmission206Local(t, false, false)
	before := paymentDefend206Read(t, f.channel168Fixture)
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var once, releaseOnce sync.Once
	releaseGate := func() { releaseOnce.Do(func() { close(release) }) }
	f.s.ProviderFlow.SubjectResolver = func(ctx context.Context, o payment.Order, _ int64) (string, error) {
		once.Do(func() { close(entered) })
		select {
		case <-release:
			return o.Name, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	defer releaseGate()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- f.wire(t, ctx, "/mapi.php", "defend206-before-claim") }()
	select {
	case <-entered:
	case early := <-done:
		t.Fatal("off-policy payment never reached the existing pre-claim subject seam", early.Code, early.Body.String())
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	defendAdmission206Configured(t, f.configure(ctx, paymentDefend206Body(t, before, true)))
	releaseGate()
	defendAdmission206Blocked(t, defendAdmission206Await(t, ctx, done))
	routes, orders, attempts, jobs := f.counts(t)
	if routes != 1 || orders != 1 || attempts != 0 || jobs != 0 || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 0 {
		t.Fatal("policy committed after prepared order but before claim did not prevent first provider I/O", routes, orders, attempts, jobs)
	}
}

type defendAdmission206Unknown struct {
	*channel168Provider
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *defendAdmission206Unknown) Create(ctx context.Context, _ adapter.CreateRequest) (adapter.CreateResult, error) {
	p.sends.Add(1)
	p.once.Do(func() { close(p.entered) })
	select {
	case <-p.release:
		return adapter.CreateResult{}, &adapter.UnknownOutcome{Operation: "controlled206", Cause: errors.New("synthetic lost response")}
	case <-ctx.Done():
		return adapter.CreateResult{}, &adapter.UnknownOutcome{Operation: "controlled206", Cause: ctx.Err()}
	}
}

func TestPaymentDefend206ClaimedUnknownRemainsRecoverableWithoutHoldingDatabaseAcrossProviderIO(t *testing.T) {
	for _, single := range []bool{false, true} {
		t.Run(fmt.Sprintf("single-connection-%v", single), func(t *testing.T) {
			f := defendAdmission206Local(t, false, single)
			before := paymentDefend206Read(t, f.channel168Fixture)
			base := f.s.ProviderFlow.Providers[7].(*channel168Provider)
			var queries atomic.Int64
			base.subchannelTestQuery = func(_ context.Context, r adapter.QueryRequest) (adapter.QueryResult, error) {
				queries.Add(1)
				return adapter.QueryResult{TradeNo: r.TradeNo, ProviderTradeNo: "synthetic206-original", State: "SUCCESS", Currency: "CNY", BuyerID: "2088000000000206", Amount: 123}, nil
			}
			provider := &defendAdmission206Unknown{channel168Provider: base, entered: make(chan struct{}), release: make(chan struct{})}
			f.s.ProviderFlow.Providers[7] = provider
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(provider.release) }) }
			defer release()
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- f.wire(t, ctx, "/mapi.php", "defend206-claimed-original") }()
			select {
			case <-provider.entered:
			case early := <-done:
				t.Fatal("valid off-policy first send did not reach the in-process provider", early.Code, early.Body.String())
			case <-ctx.Done():
				t.Fatal("first send deadlocked before controlled provider entry", ctx.Err())
			}
			if acquired := f.p.Stat().AcquiredConns(); acquired != 0 {
				t.Errorf("provider I/O retained %d business database connections/transactions", acquired)
			}
			var orderState, attemptState string
			if err := f.observer.QueryRow(ctx, `SELECT o.status,a.state FROM orders o JOIN provider_attempts a ON a.order_id=o.id WHERE o.out_trade_no='defend206-claimed-original'`).Scan(&orderState, &attemptState); err != nil || orderState != "submitting" || attemptState != "processing" {
				t.Fatal("provider call started without its committed original claim", orderState, attemptState, err)
			}
			changeCtx, changeCancel := context.WithTimeout(ctx, 4*time.Second)
			configured := f.configure(changeCtx, paymentDefend206Body(t, before, true))
			changeCancel()
			defendAdmission206Configured(t, configured)
			release()
			defendAdmission206Blocked(t, defendAdmission206Await(t, ctx, done))
			if base.sends.Load() != 1 {
				t.Fatal("already committed original first-send right was lost or sent twice", base.sends.Load())
			}
			// Read-only legacy query remains available after the new policy.
			form := url.Values{"act": {"order"}, "pid": {"1"}, "key": {"test-secret"}, "out_trade_no": {"defend206-claimed-original"}}
			r := httptest.NewRequest(http.MethodPost, "/api.php", strings.NewReader(form.Encode())).WithContext(ctx)
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.RemoteAddr = "192.0.2.206:5206"
			w := httptest.NewRecorder()
			f.s.Handler().ServeHTTP(w, r)
			var queried map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &queried); err != nil || queried["code"] != float64(1) || queries.Load() != 1 {
				t.Fatal("new-payment policy blocked original unknown query recovery", w.Code, w.Body.String(), queries.Load(), err)
			}
			o, err := f.s.Store.Order(ctx, 1, "defend206-claimed-original")
			if err != nil || o.Status != "paid" {
				t.Fatal("controlled authenticated original query was not applied", o.Status, err)
			}
			originalEffects := f.effects(t)
			f.wire(t, ctx, "/mapi.php", "defend206-claimed-original")
			defendAdmission206Blocked(t, f.wire(t, ctx, "/mapi.php", "defend206-after-claim-new"))
			if base.sends.Load() != 1 || f.effects(t) != originalEffects {
				t.Fatal("replay/new payment after policy re-owned the old send or changed payment work")
			}
		})
	}
}

func TestPaymentDefend206KeylessUnconfiguredSingleConnectionRetainsOriginalOffBaseline(t *testing.T) {
	f := defendAdmission206Local(t, false, true)
	f.s.PaymentDefend206 = nil
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	w := f.wire(t, ctx, "/mapi.php", "defend206-keyless-empty")
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out["code"] != float64(1) || f.s.ProviderFlow.Providers[7].(*channel168Provider).sends.Load() != 1 {
		t.Fatal("genuinely empty keyless policy did not preserve off baseline with one connection", w.Code, w.Body.String(), err)
	}
}

func TestPaymentDefend206MissingKeyOrDamagedConfiguredOffPolicyCannotAuthorizeNewSend(t *testing.T) {
	for _, damage := range []string{"nil-service", "zero-service", "head-mac", "orphan-witness", "audit-only", "missing-table", "database-error"} {
		t.Run(damage, func(t *testing.T) {
			f := defendAdmission206Local(t, false, false)
			before := paymentDefend206Read(t, f.channel168Fixture)
			ctx, cancel := context.WithTimeout(t.Context(), 12*time.Second)
			defer cancel()
			// Explicitly save OFF. Blocking must be caused by unavailable or
			// damaged authority, rather than by a configured 3/3 deny rule.
			defendAdmission206Configured(t, f.configure(ctx, paymentDefend206Body(t, before, false)))
			baseline := f.wire(t, ctx, "/mapi.php", "defend206-off-valid-source")
			var ok map[string]any
			base := f.s.ProviderFlow.Providers[7].(*channel168Provider)
			if json.Unmarshal(baseline.Body.Bytes(), &ok) != nil || ok["code"] != float64(1) || base.sends.Load() != 1 {
				t.Fatal("configured OFF positive control did not make exactly one controlled send", baseline.Code, baseline.Body.String())
			}
			switch damage {
			case "nil-service":
				f.s.PaymentDefend206 = nil
			case "zero-service":
				f.s.PaymentDefend206 = &paymentdefend.Store{}
			case "head-mac":
				if _, err := f.observer.Exec(ctx, `ALTER TABLE payment_defend206_policy DISABLE TRIGGER USER; UPDATE payment_defend206_policy SET proof_hash=decode(repeat('00',32),'hex'); ALTER TABLE payment_defend206_policy ENABLE TRIGGER USER`); err != nil {
					t.Fatal(err)
				}
			case "orphan-witness", "audit-only":
				if _, err := f.observer.Exec(ctx, `ALTER TABLE payment_defend206_policy DISABLE TRIGGER USER; DELETE FROM payment_defend206_policy; ALTER TABLE payment_defend206_policy ENABLE TRIGGER USER`); err != nil {
					t.Fatal(err)
				}
				if damage == "audit-only" {
					if _, err := f.observer.Exec(ctx, `ALTER TABLE payment_defend206_policy_commits DISABLE TRIGGER USER; DELETE FROM payment_defend206_policy_commits; ALTER TABLE payment_defend206_policy_commits ENABLE TRIGGER USER`); err != nil {
						t.Fatal(err)
					}
				}
			case "missing-table":
				if _, err := f.observer.Exec(ctx, `ALTER TABLE payment_defend206_policy RENAME TO payment_defend206_policy_unavailable`); err != nil {
					t.Fatal(err)
				}
			case "database-error":
				f.p.Close()
			}
			effects := f.effects(t)
			defendAdmission206Blocked(t, f.wire(t, ctx, "/mapi.php", "defend206-bad-authority-new"))
			if base.sends.Load() != 1 || f.effects(t) != effects {
				t.Fatal("missing/damaged policy authority failed open into a new order or first send", damage, base.sends.Load())
			}
		})
	}
}
