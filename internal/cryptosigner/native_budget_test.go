package cryptosigner

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goepay/internal/cryptochain"
)

func TestTinyTransfersCannotExhaustNativeFeesWithinUSDTDailyLimit(t *testing.T) {
	for _, limit := range []string{"native-fee", "transaction-count"} {
		t.Run(limit, func(t *testing.T) {
			cfg := testConfig(filepath.Join(t.TempDir(), "journal.jsonl"))
			if limit == "native-fee" {
				cfg.Rules[0].MaxDailyFeeAtomic = "2000000000000000"
			} else {
				cfg.Rules[0].MaxDailyTransactions = 1
			}
			keys := testKeys(t)
			s, err := NewService(cfg, keys)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = s.Cancel(t.Context(), ethereumPrepared("never-signed", 0, "1")); err != nil {
				t.Fatal(err)
			}
			p := ethereumPrepared("one-micro-usdt", 0, "1")
			first, err := s.Sign(t.Context(), p)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Close(); err != nil {
				t.Fatal(err)
			}
			s, err = NewService(cfg, keys)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			for i := 0; i < 3; i++ {
				replay, err := s.Sign(t.Context(), p)
				if err != nil || replay != first {
					t.Fatalf("replay consumed another budget reservation: %v", err)
				}
			}
			if _, err = s.Sign(t.Context(), ethereumPrepared("another-micro-usdt", 1, "1")); !errors.Is(err, ErrPolicy) {
				t.Fatalf("tiny payout bypassed daily %s budget: %v", limit, err)
			}
		})
	}
}

func TestUnknownHSMKeepsNativeFeeBudgetReservedAfterRestart(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "journal.jsonl"))
	cfg.Rules[0].MaxDailyFeeAtomic = "1300000000000000"
	keys := testKeys(t)
	s, err := NewService(cfg, unavailableHSM{keys})
	if err != nil {
		t.Fatal(err)
	}
	p := ethereumPrepared("unknown-native-fee", 0, "1")
	if _, err = s.Sign(t.Context(), p); !errors.Is(err, ErrIndeterminate) {
		t.Fatal(err)
	}
	if _, err = s.Cancel(t.Context(), p); !errors.Is(err, ErrIndeterminate) {
		t.Fatal("unknown HSM fee reservation was cancelled")
	}
	s.Close()
	s, err = NewService(cfg, keys)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Sign(t.Context(), ethereumPrepared("second-native-fee", 1, "1")); !errors.Is(err, ErrPolicy) {
		t.Fatalf("HSM timeout released native fee budget: %v", err)
	}
}

type reservationCheckingKeys struct {
	KeyProvider
	path string
}

func (k reservationCheckingKeys) PublicKey(ctx context.Context, id string) ([]byte, error) {
	raw, err := os.ReadFile(k.path)
	if err != nil {
		return nil, err
	}
	var e entry
	if err = json.Unmarshal(raw, &e); err != nil {
		return nil, err
	}
	if e.State != "pending" || e.FeeBudget != "1300000000000000" {
		return nil, errors.New("key accessed before durable native budget reservation")
	}
	return k.KeyProvider.PublicKey(ctx, id)
}

func TestNativeFeeAndTransactionReservationIsDurableBeforeKeyAccess(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "journal.jsonl"))
	keys := reservationCheckingKeys{KeyProvider: testKeys(t), path: cfg.StatePath}
	s, err := NewService(cfg, keys)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Sign(t.Context(), ethereumPrepared("budget-before-key", 0, "1")); err != nil {
		t.Fatal(err)
	}
}

func TestSignerRequiresExplicitNativeFeeAndTransactionLimits(t *testing.T) {
	for _, field := range []string{"transactions", "fee"} {
		cfg := testConfig(filepath.Join(t.TempDir(), "journal.jsonl"))
		if field == "transactions" {
			cfg.Rules[0].MaxDailyTransactions = 0
		} else {
			cfg.Rules[0].MaxDailyFeeAtomic = ""
		}
		if s, err := NewService(cfg, testKeys(t)); err == nil {
			s.Close()
			t.Fatalf("missing daily %s budget accepted", field)
		}
	}
}

func TestTRONNativeFeeBudgetUsesFeeLimitAcrossRestart(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		height := uint64(100)
		if r.URL.Path != "/wallet/getnowblock" {
			var p struct {
				Num uint64 `json:"num"`
			}
			json.NewDecoder(r.Body).Decode(&p)
			height = p.Num
		}
		id := "0000000000000064112233445566778800000000000000000000000000000000"
		if height == 0 {
			id = cryptochain.TronMainnetGenesis
		}
		json.NewEncoder(w).Encode(map[string]any{"blockID": id, "block_header": map[string]any{"raw_data": map[string]uint64{"number": height}}})
	}))
	defer rpc.Close()
	p := tronPrepared("fee-limit-one", now)
	cfg := Config{StatePath: filepath.Join(t.TempDir(), "journal.jsonl"), Now: func() time.Time { return now }, Rules: []Rule{{Chain: cryptochain.Tron, Network: "mainnet", ChainID: "728126428", Contract: cryptochain.TronUSDTContract, From: p.From, KeyID: "treasury", Destinations: []string{p.To}, MaxAmount: "1000000", DailyLimit: "3000000", MaxDailyTransactions: 10, MaxDailyFeeAtomic: "100000000", MaxFeeLimit: 100000000, MaxTTLSeconds: 300, TRONRPCURL: rpc.URL}}}
	keys := testKeys(t)
	s, err := NewService(cfg, keys)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Sign(t.Context(), p); err != nil {
		t.Fatal(err)
	}
	s.Close()
	cfg.Rules[0].From, _ = cryptochain.NormalizeAddress(cryptochain.Tron, p.From)
	s, err = NewService(cfg, keys)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	next := tronPrepared("fee-limit-two", now.Add(time.Millisecond))
	next.From = cfg.Rules[0].From
	if _, err = s.Sign(t.Context(), next); !errors.Is(err, ErrPolicy) {
		t.Fatalf("TRON fee_limit budget reset or ignored: %v", err)
	}
}
