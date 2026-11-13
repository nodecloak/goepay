package cryptosigner

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"goepay/internal/cryptochain"
)

func TestTRONSignsValidatedTransferAndRejectsWrongReferenceOrExtraContract(t *testing.T) {
	now := time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)
	rpc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := uint64(100)
		if r.URL.Path == "/wallet/getblockbynum" {
			var p struct {
				Num uint64 `json:"num"`
			}
			json.NewDecoder(r.Body).Decode(&p)
			n = p.Num
		}
		id := "0000000000000064112233445566778800000000000000000000000000000000"
		if n == 0 {
			id = cryptochain.TronMainnetGenesis
		}
		json.NewEncoder(w).Encode(map[string]any{"blockID": id, "block_header": map[string]any{"raw_data": map[string]uint64{"number": n}}})
	}))
	defer rpc.Close()
	cfg := Config{StatePath: filepath.Join(t.TempDir(), "journal.jsonl"), Now: func() time.Time { return now }, Rules: []Rule{{Chain: cryptochain.Tron, Network: "mainnet", ChainID: "728126428", Contract: cryptochain.TronUSDTContract, From: "417e5f4552091a69125d5dfcb7b8c2659029395bdf", KeyID: "treasury", Destinations: []string{"412222222222222222222222222222222222222222"}, MaxAmount: "1000000", DailyLimit: "3000000", MaxDailyTransactions: 100, MaxDailyFeeAtomic: "1000000000000", MaxFeeLimit: 100000000, MaxTTLSeconds: 300, TRONRPCURL: rpc.URL}}}
	s, err := NewService(cfg, testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := tronPrepared("tron-payment", now)
	signed, err := s.Sign(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err = VerifySigned(p, signed); err != nil {
		t.Fatalf("invalid TRON signed result: %v", err)
	}
	var envelope struct {
		Signature []string `json:"signature"`
		RawData   struct {
			Contract []any `json:"contract"`
		} `json:"raw_data"`
	}
	if err = json.Unmarshal([]byte(signed.RawTransaction), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Signature) != 1 || len(envelope.Signature[0]) != 130 || len(envelope.RawData.Contract) != 1 {
		t.Fatalf("invalid signed envelope: %s", signed.RawTransaction)
	}
	for _, mutation := range []struct {
		name   string
		change func([]byte) []byte
	}{
		{"wrong-reference", func(raw []byte) []byte {
			for i := 0; i+8 <= len(raw); i++ {
				if hex.EncodeToString(raw[i:i+8]) == "1122334455667788" {
					raw[i] = 0xff
					break
				}
			}
			return raw
		}},
		{"extra-contract", func(raw []byte) []byte { return append(raw, protoBytes(11, []byte{8, 31})...) }},
		{"permission-change", func(raw []byte) []byte { return append(raw, protoBytes(9, []byte{1})...) }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			bad := tronPrepared(mutation.name, now)
			raw, _ := hex.DecodeString(bad.SigningPayload)
			setTRONRaw(&bad, mutation.change(raw))
			if _, err := s.Sign(context.Background(), bad); !errors.Is(err, ErrPolicy) {
				t.Fatalf("unsafe TRON accepted: %v", err)
			}
		})
	}
	// Reformatting the same source in configuration cannot reset its daily limit.
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.Rules[0].From, _ = cryptochain.NormalizeAddress(cryptochain.Tron, cfg.Rules[0].From)
	cfg.Rules[0].DailyLimit = "100000"
	s2, err := NewService(cfg, testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	next := tronPrepared("next-day-budget-bypass", now.Add(time.Millisecond))
	next.From = cfg.Rules[0].From
	if _, err = s2.Sign(context.Background(), next); !errors.Is(err, ErrPolicy) {
		t.Fatalf("source reformat reset daily limit: %v", err)
	}
}

func TestVerifySignedRejectsPayloadAndSourceSubstitution(t *testing.T) {
	s, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p := ethereumPrepared("verify", 0, "100000")
	signed, err := s.Sign(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err = VerifySigned(p, signed); err != nil {
		t.Fatal(err)
	}
	changed := p
	changed.From = "0x3333333333333333333333333333333333333333"
	if err = VerifySigned(changed, signed); err == nil {
		t.Fatal("foreign source accepted")
	}
	changed = ethereumPrepared("verify", 0, "200000")
	if err = VerifySigned(changed, signed); err == nil {
		t.Fatal("substituted payment accepted")
	}
	signed.TxID = "0x" + hex.EncodeToString(make([]byte, 32))
	if err = VerifySigned(p, signed); err == nil {
		t.Fatal("incorrect transaction ID accepted")
	}
}

func tronPrepared(id string, now time.Time) cryptochain.PreparedTransaction {
	from, _ := hex.DecodeString("417e5f4552091a69125d5dfcb7b8c2659029395bdf")
	contract, _ := addressBytes(cryptochain.Tron, cryptochain.TronUSDTContract)
	data, _ := hex.DecodeString("a9059cbb000000000000000000000000222222222222222222222222222222222222222200000000000000000000000000000000000000000000000000000000000186a0")
	trigger := append(protoBytes(1, from), protoBytes(2, contract)...)
	trigger = append(trigger, protoBytes(4, data)...)
	anyValue := append(protoBytes(1, []byte("type.googleapis.com/protocol.TriggerSmartContract")), protoBytes(2, trigger)...)
	call := append(protoInt(1, 31), protoBytes(2, anyValue)...)
	raw := append(protoBytes(1, []byte{0, 100}), protoBytes(4, []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88})...)
	expiry := uint64(now.Add(time.Minute).UnixMilli())
	raw = append(raw, protoInt(8, expiry)...)
	raw = append(raw, protoBytes(11, call)...)
	raw = append(raw, protoInt(14, uint64(now.UnixMilli()))...)
	raw = append(raw, protoInt(18, 100000000)...)
	p := cryptochain.PreparedTransaction{Chain: cryptochain.Tron, Network: "mainnet", ChainID: "728126428", RequestID: id, From: hex.EncodeToString(from), To: "412222222222222222222222222222222222222222", Contract: cryptochain.TronUSDTContract, AtomicAmount: "100000", FeeLimit: 100000000, Expiration: expiry}
	setTRONRaw(&p, raw)
	return p
}
func setTRONRaw(p *cryptochain.PreparedTransaction, raw []byte) {
	p.SigningPayload = hex.EncodeToString(raw)
	id := sha256.Sum256(raw)
	p.TxID = hex.EncodeToString(id[:])
	b, _ := json.Marshal(map[string]any{"txID": p.TxID, "raw_data_hex": p.SigningPayload})
	p.UnsignedPayload = string(b)
}
func protoBytes(field uint64, b []byte) []byte {
	out := binary.AppendUvarint(nil, field<<3|2)
	out = binary.AppendUvarint(out, uint64(len(b)))
	return append(out, b...)
}
func protoInt(field, n uint64) []byte {
	return binary.AppendUvarint(binary.AppendUvarint(nil, field<<3), n)
}
