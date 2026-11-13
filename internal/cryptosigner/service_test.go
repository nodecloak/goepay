package cryptosigner

import (
	"context"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goepay/internal/cryptochain"
)

type unavailableHSM struct{ KeyProvider }

func (unavailableHSM) SignDigest(context.Context, string, []byte) ([]byte, error) {
	return nil, errors.New("HSM timed out")
}

func TestIndeterminateSigningReservesRequestNonceAndBudgetAcrossRestart(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "journal.jsonl"))
	keys := testKeys(t)
	s, err := NewService(cfg, unavailableHSM{keys})
	if err != nil {
		t.Fatal(err)
	}
	p := ethereumPrepared("unknown", 0, "700000")
	if _, err = s.Sign(context.Background(), p); !errors.Is(err, ErrIndeterminate) {
		t.Fatalf("expected indeterminate, got %v", err)
	}
	s.Close()
	s, err = NewService(cfg, keys)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Sign(context.Background(), p); !errors.Is(err, ErrIndeterminate) {
		t.Fatalf("pending was signed after restart: %v", err)
	}
	if _, err = s.Sign(context.Background(), ethereumPrepared("replacement", 0, "700000")); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("replacement signed: %v", err)
	}
	if _, err = s.Sign(context.Background(), ethereumPrepared("other", 1, "400000")); !errors.Is(err, ErrPolicy) {
		t.Fatalf("pending budget not reserved: %v", err)
	}
}

func TestSignerRejectsApprovalAndUnboundedGas(t *testing.T) {
	s, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := ethereumPrepared("approval", 0, "100000")
	raw, _ := hex.DecodeString(p.UnsignedPayload)
	fields, _ := decodeList(raw)
	copy(fields[5][:4], []byte{0x09, 0x5e, 0xa7, 0xb3})
	raw = encodeList(fields)
	p.UnsignedPayload = hex.EncodeToString(raw)
	p.SigningPayload = hex.EncodeToString(keccak(raw))
	if _, err = s.Sign(context.Background(), p); !errors.Is(err, ErrPolicy) {
		t.Fatalf("approval authorized: %v", err)
	}
	p = ethereumPrepared("gas", 0, "100000")
	raw, _ = hex.DecodeString(p.UnsignedPayload)
	fields, _ = decodeList(raw)
	fields[2] = uintBytes(100001)
	raw = encodeList(fields)
	p.UnsignedPayload = hex.EncodeToString(raw)
	p.SigningPayload = hex.EncodeToString(keccak(raw))
	p.GasLimit = 100001
	if _, err = s.Sign(context.Background(), p); !errors.Is(err, ErrPolicy) {
		t.Fatalf("unbounded gas authorized: %v", err)
	}
}

func TestJournalFailureMakesSignerFailClosed(t *testing.T) {
	s, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.file.Close()
	if _, err = s.Sign(context.Background(), ethereumPrepared("storage-failed", 0, "100000")); !errors.Is(err, ErrIndeterminate) {
		t.Fatalf("storage failure not fail-closed: %v", err)
	}
	if _, err = s.Sign(context.Background(), ethereumPrepared("another", 1, "100000")); !errors.Is(err, ErrIndeterminate) {
		t.Fatalf("signer continued after journal failure: %v", err)
	}
}

func TestSignerRefusesSecondJournalOwnerAndTruncatedRecovery(t *testing.T) {
	cfg := testConfig(filepath.Join(t.TempDir(), "journal.jsonl"))
	keys := testKeys(t)
	s, err := NewService(cfg, keys)
	if err != nil {
		t.Fatal(err)
	}
	if other, err := NewService(cfg, keys); err == nil {
		other.Close()
		t.Fatal("second journal owner accepted")
	}
	if _, err = s.Sign(context.Background(), ethereumPrepared("durable", 0, "100000")); err != nil {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(cfg.StatePath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"request_id":"torn"`)
	f.Close()
	if recovered, err := NewService(cfg, keys); err == nil {
		recovered.Close()
		t.Fatal("truncated journal ignored")
	}
}

// A changed request or a reset daily counter could authorize a second payment.
func TestSignerPersistsRequestBindingAndDailyLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	keys := testKeys(t)
	cfg := testConfig(path)
	s, err := NewService(cfg, keys)
	if err != nil {
		t.Fatal(err)
	}
	p := ethereumPrepared("withdrawal-one", 0, "700000")
	first, err := s.Sign(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if first.TxID == "" || first.RawTransaction == "" {
		t.Fatal("missing signed transaction")
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = NewService(cfg, keys)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	again, err := s.Sign(context.Background(), p)
	if err != nil || again != first {
		t.Fatalf("replay changed result: %v %#v", err, again)
	}
	changed := ethereumPrepared("withdrawal-one", 0, "600000")
	if _, err = s.Sign(context.Background(), changed); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("changed request: %v", err)
	}
	if _, err = s.Sign(context.Background(), ethereumPrepared("withdrawal-two", 1, "400000")); !errors.Is(err, ErrPolicy) {
		t.Fatalf("daily limit after restart: %v", err)
	}
}

func TestSignerRejectsClaimedTransferWhenActualPayloadDiffers(t *testing.T) {
	s, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, mutation := range []struct {
		name  string
		apply func(*cryptochain.PreparedTransaction)
	}{
		{"amount", func(p *cryptochain.PreparedTransaction) { p.AtomicAmount = "1" }},
		{"network", func(p *cryptochain.PreparedTransaction) { p.Network = "other" }},
		{"chain ID", func(p *cryptochain.PreparedTransaction) { p.ChainID = "2" }},
		{"destination", func(p *cryptochain.PreparedTransaction) { p.To = "0x3333333333333333333333333333333333333333" }},
		{"gas", func(p *cryptochain.PreparedTransaction) { p.GasLimit++ }},
		{"nonce", func(p *cryptochain.PreparedTransaction) { p.Nonce++ }},
		{"digest", func(p *cryptochain.PreparedTransaction) { p.SigningPayload = "00" }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			p := ethereumPrepared(mutation.name, 0, "100000")
			mutation.apply(&p)
			if _, err := s.Sign(context.Background(), p); !errors.Is(err, ErrPolicy) {
				t.Fatalf("expected policy refusal, got %v", err)
			}
		})
	}
}

func TestSignerNeverSignsAnotherRequestAtReservedEthereumNonce(t *testing.T) {
	s, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Sign(context.Background(), ethereumPrepared("first", 0, "100000")); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Sign(context.Background(), ethereumPrepared("second", 0, "100000")); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("nonce reused: %v", err)
	}
}

func testKeys(t *testing.T) KeyProvider {
	t.Helper()
	path := filepath.Join(t.TempDir(), "signer.key")
	if err := os.WriteFile(path, []byte("0000000000000000000000000000000000000000000000000000000000000001\n"), 0600); err != nil {
		t.Fatal(err)
	}
	k, err := NewFileKeys(map[string]string{"treasury": path})
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func testConfig(path string) Config {
	return Config{StatePath: path, Now: func() time.Time { return time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC) }, Rules: []Rule{{
		Chain: cryptochain.Ethereum, Network: "mainnet", ChainID: "1", Contract: "0xdac17f958d2ee523a2206206994597c13d831ec7", From: "0x7e5f4552091a69125d5dfcb7b8c2659029395bdf", KeyID: "treasury",
		Destinations: []string{"0x2222222222222222222222222222222222222222"}, MaxAmount: "1000000", DailyLimit: "1000000", MaxDailyTransactions: 100, MaxDailyFeeAtomic: "1000000000000000000", MaxGasLimit: 100000, MaxGasPrice: "50000000000", MaxNonce: 100,
	}}}
}

// Fixed EIP-155 legacy transfer: nonce 0, gas price 20gwei, gas 65000,
// zero ETH value and transfer(0x2222..., amount). Encoding is test fixture code.
func ethereumPrepared(id string, nonce uint64, amount string) cryptochain.PreparedTransaction {
	data := append([]byte{0xa9, 0x05, 0x9c, 0xbb}, make([]byte, 12)...)
	dest, _ := hex.DecodeString("2222222222222222222222222222222222222222")
	data = append(data, dest...)
	a := mustBig(amount)
	data = append(data, a.FillBytes(make([]byte, 32))...)
	contract, _ := hex.DecodeString("dac17f958d2ee523a2206206994597c13d831ec7")
	raw := encodeList([][]byte{uintBytes(nonce), mustBig("20000000000").Bytes(), uintBytes(65000), contract, nil, data, {1}, nil, nil})
	return cryptochain.PreparedTransaction{Chain: cryptochain.Ethereum, Network: "mainnet", ChainID: "1", RequestID: id, From: "0x7e5f4552091a69125d5dfcb7b8c2659029395bdf", To: "0x2222222222222222222222222222222222222222", Contract: "0xdac17f958d2ee523a2206206994597c13d831ec7", AtomicAmount: amount, Nonce: nonce, GasLimit: 65000, GasPrice: "20000000000", UnsignedPayload: hex.EncodeToString(raw), SigningPayload: hex.EncodeToString(keccak(raw))}
}
