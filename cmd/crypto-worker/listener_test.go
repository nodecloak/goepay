package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/db"
	"goepay/internal/cryptochain"
	"goepay/internal/cryptopay"
)

func TestWorkerOnlyPublishesReadinessFromPersistentHealthyListener(t *testing.T) {
	pool := listenerTestDatabase(t)
	primary, witness := listenerTestNode(t), listenerTestNode(t)
	wallet := cryptopay.NetworkConfig{
		Enabled:         true,
		TreasuryAddress: "0x1111111111111111111111111111111111111111",
		HotAddress:      "0x2222222222222222222222222222222222222222",
	}
	cfg := config{Networks: map[string]networkConfig{"ethereum": {
		NetworkConfig: wallet, PrimaryRPC: primary.URL, WitnessRPC: witness.URL, StartHeight: 1,
	}}}
	if err := run(t.Context(), cfg, true, false); err != nil {
		t.Fatal("one-shot scan could not catch up", err)
	}
	var checkedAt time.Time
	err := pool.QueryRow(t.Context(), "SELECT checked_at FROM crypto_listener_health WHERE network='ethereum'").Scan(&checkedAt)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("one-shot published a listener proof: %s %v", checkedAt, err)
	}

	runCtx, stop := context.WithCancel(t.Context())
	finished := make(chan error, 1)
	go func() { finished <- run(runCtx, cfg, false, false) }()
	t.Cleanup(func() {
		stop()
		if err := <-finished; err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("persistent worker stopped with error: %v", err)
		}
	})

	wait, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	var fingerprint string
	var opened bool
	for {
		err = pool.QueryRow(wait, `SELECT h.fingerprint,h.checked_at,c.enabled
			FROM crypto_listener_health h JOIN crypto_network_controls c USING (network)
			WHERE network='ethereum'`).Scan(&fingerprint, &checkedAt, &opened)
		if err == nil {
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) || wait.Err() != nil {
			t.Fatalf("persistent healthy worker did not record listener readiness: %v", err)
		}
		select {
		case <-wait.Done():
			t.Fatal("persistent healthy worker did not record listener readiness")
		case <-tick.C:
		}
	}
	if len(fingerprint) != 64 || checkedAt.IsZero() || opened {
		t.Fatalf("invalid proof or control was opened automatically: fingerprint length %d, time %s, enabled %v", len(fingerprint), checkedAt, opened)
	}
	if err := publishListenerProof(t.Context(), pool, "ethereum", strings.Repeat("f", 64), checkedAt.Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	var actual string
	var lastChecked time.Time
	if err := pool.QueryRow(t.Context(), "SELECT fingerprint,checked_at FROM crypto_listener_health WHERE network='ethereum'").Scan(&actual, &lastChecked); err != nil {
		t.Fatal(err)
	}
	if actual != fingerprint || lastChecked.Before(checkedAt) {
		t.Fatal("delayed observation replaced a newer listener proof")
	}
}

func listenerTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required")
	}
	uri, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "crypto_listener_" + strings.ToLower(rand.Text())
	name := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE SCHEMA "+name); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		defer admin.Close()
		var active int
		if err := admin.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE application_name=$1", schema).Scan(&active); err != nil || active != 0 {
			t.Errorf("retain worker schema with unverified cleanup: active=%d error=%v", active, err)
			return
		}
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+name+" CASCADE"); err != nil {
			t.Error(err)
		}
	})
	query := uri.Query()
	query.Set("search_path", schema)
	query.Set("application_name", schema)
	uri.RawQuery = query.Encode()
	t.Setenv("DATABASE_URL", uri.String())
	t.Setenv("GOEPAY_MASTER_KEY", strings.Repeat("01", 32))
	t.Setenv("GOEPAY_PLATFORM_PRIVATE_KEY_FILE", "")
	pool, err := pgxpool.New(t.Context(), uri.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(t.Context(), pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func listenerTestNode(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params []any           `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid rpc", http.StatusBadRequest)
			return
		}
		var result any
		switch req.Method {
		case "eth_chainId":
			result = "0x1"
		case "eth_getBlockByNumber":
			if len(req.Params) < 1 {
				http.Error(w, "missing block", http.StatusBadRequest)
				return
			}
			if req.Params[0] == "0x0" {
				result = map[string]string{"number": "0x0", "hash": cryptochain.EthereumMainnetGenesis, "timestamp": "0x1"}
			} else {
				result = map[string]string{"number": "0x1", "hash": "0x" + strings.Repeat("1", 64), "timestamp": "0x2"}
			}
		case "eth_getLogs":
			result = []any{}
		default:
			t.Errorf("listener attempted unexpected node method %s", req.Method)
			http.Error(w, "unexpected rpc", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	}))
	t.Cleanup(s.Close)
	return s
}
