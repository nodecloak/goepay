package cryptochain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func tronPreparedFixture(now time.Time) map[string]any {
	data := mustHex("a9059cbb" + topicTo + tokenAmount)
	trigger := testProtoBytes(nil, 1, mustHex("411111111111111111111111111111111111111111"))
	trigger = testProtoBytes(trigger, 2, mustHex("41a614f803b6fd780986a42c78ec9c7f77e6ded13c"))
	trigger = testProtoBytes(trigger, 4, data)
	anyValue := testProtoBytes(nil, 1, []byte("type.googleapis.com/protocol.TriggerSmartContract"))
	anyValue = testProtoBytes(anyValue, 2, trigger)
	contract := testProtoUint(nil, 1, 31)
	contract = testProtoBytes(contract, 2, anyValue)
	b := testProtoBytes(nil, 1, []byte{0, 1})
	b = testProtoBytes(b, 4, mustHex("aaaaaaaaaaaaaaaa"))
	b = testProtoUint(b, 8, uint64(now.Add(time.Minute).UnixMilli()))
	b = testProtoBytes(b, 11, contract)
	b = testProtoUint(b, 14, uint64(now.UnixMilli()))
	b = testProtoUint(b, 18, 100000000)
	raw := hex.EncodeToString(b)
	digest := sha256.Sum256(b)
	return map[string]any{"txID": hex.EncodeToString(digest[:]), "raw_data_hex": raw, "raw_data": map[string]any{"ref_block_bytes": "0001", "ref_block_hash": "aaaaaaaaaaaaaaaa", "timestamp": now.UnixMilli(), "expiration": now.Add(time.Minute).UnixMilli(), "fee_limit": 100000000, "contract": []any{map[string]any{"type": "TriggerSmartContract", "parameter": map[string]any{"type_url": "type.googleapis.com/protocol.TriggerSmartContract", "value": map[string]any{"owner_address": "411111111111111111111111111111111111111111", "contract_address": "41a614f803b6fd780986a42c78ec9c7f77e6ded13c", "data": "a9059cbb" + topicTo + tokenAmount}}}}}}
}

func testProtoBytes(out []byte, field uint64, data []byte) []byte {
	out = binary.AppendUvarint(out, field<<3|2)
	out = binary.AppendUvarint(out, uint64(len(data)))
	return append(out, data...)
}
func testProtoUint(out []byte, field, value uint64) []byte {
	out = binary.AppendUvarint(out, field<<3)
	return binary.AppendUvarint(out, value)
}

func TestTronPrepareBuildsOnlyRequestedUSDTTransferAndNeverBroadcasts(t *testing.T) {
	for _, tamper := range []string{"", "owner", "token", "amount", "digest", "signed", "expiration"} {
		t.Run(tamper, func(t *testing.T) {
			payload := tronPreparedFixture(time.Now())
			broadcasts := 0
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/wallet/triggersmartcontract" {
					var input map[string]any
					if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
						t.Error(err)
					}
					if input["function_selector"] != "transfer(address,uint256)" || input["contract_address"] != "41a614f803b6fd780986a42c78ec9c7f77e6ded13c" || input["parameter"] != topicTo+tokenAmount {
						t.Errorf("wrong node construction %+v", input)
					}
					raw := payload["raw_data"].(map[string]any)
					value := raw["contract"].([]any)[0].(map[string]any)["parameter"].(map[string]any)["value"].(map[string]any)
					switch tamper {
					case "owner":
						value["owner_address"] = "412222222222222222222222222222222222222222"
					case "token":
						value["contract_address"] = "413333333333333333333333333333333333333333"
					case "amount":
						value["data"] = "a9059cbb" + topicTo + strings.Repeat("0", 63) + "2"
					case "digest":
						payload["txID"] = testTx
					case "signed":
						payload["signature"] = []string{strings.Repeat("1", 130)}
					case "expiration":
						raw["expiration"] = time.Now().Add(-time.Hour).UnixMilli()
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"result": true}, "transaction": payload})
					return
				}
				if r.URL.Path == "/wallet/broadcasttransaction" {
					broadcasts++
				}
				rpcFixture{chain: Tron}.serve(w, r)
			}))
			defer s.Close()
			a, err := NewTron(Config{Endpoint: s.URL})
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := a.Prepare(context.Background(), TransferRequest{RequestID: "withdrawal-one", From: "411111111111111111111111111111111111111111", To: TronUSDTContract, AtomicAmount: "1000001", FeeLimit: 100000000})
			if tamper != "" {
				if err == nil {
					t.Fatalf("accepted node tampering %s: %+v", tamper, prepared)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if prepared.Chain != Tron || prepared.ChainID != "728126428" || prepared.Contract != TronUSDTContract || prepared.AtomicAmount != "1000001" || prepared.SigningPayload == "" || prepared.RequestID != "withdrawal-one" || prepared.TxID == "" {
				t.Fatalf("bad prepare %+v", prepared)
			}
			if broadcasts != 0 {
				t.Fatal("preparation broadcast a transaction")
			}
		})
	}
}

func TestEthereumPrepareReservesRequestedNonceAndBuildsEIP155Transfer(t *testing.T) {
	a := fixtureAdapter(t, rpcFixture{chain: Ethereum})
	nonce := uint64(42)
	p, err := a.Prepare(context.Background(), TransferRequest{RequestID: "outbound-eth", From: "0x" + strings.Repeat("1", 40), To: "0x" + strings.Repeat("2", 40), AtomicAmount: "1000001", Nonce: &nonce, FeeLimit: 2000000000000000})
	if err != nil {
		t.Fatal(err)
	}
	if p.ChainID != "1" || p.Nonce != 42 || p.Contract != EthereumUSDTContract || p.GasPrice != "20000000000" || p.GasLimit != 72000 || p.TxID != "" || p.SigningPayload == "" || p.UnsignedPayload == "" {
		t.Fatalf("wrong prepared transaction %+v", p)
	}
	_, err = a.Prepare(context.Background(), TransferRequest{RequestID: "too-small-fee", From: "0x" + strings.Repeat("1", 40), To: "0x" + strings.Repeat("2", 40), AtomicAmount: "1000001", FeeLimit: 1})
	if err == nil {
		t.Fatal("ignored fee budget")
	}
}

func TestRLPEncodingMatchesPublishedEIP155Vector(t *testing.T) {
	// EIP-155's public worked example, independent of payment preparation.
	encoded := encodeRLPList([][]byte{{9}, {4, 168, 23, 200, 0}, {0x52, 0x08}, mustHex("3535353535353535353535353535353535353535"), mustHex("0de0b6b3a7640000"), {}, {1}, {}, {}})
	if hex.EncodeToString(encoded) != "ec098504a817c800825208943535353535353535353535353535353535353535880de0b6b3a764000080018080" {
		t.Fatalf("wrong RLP %x", encoded)
	}
	if hex.EncodeToString(keccak(encoded)) != "daf5a779ae972f972197303d7b574746c7ef83eadac0f2791ad23db92e4c8e53" {
		t.Fatal("wrong signing hash")
	}
}

func mustHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

func TestBroadcastVerifiesPayloadIdentityAndPerformsSingleSubmission(t *testing.T) {
	for _, chain := range []Chain{Tron, Ethereum} {
		t.Run(string(chain), func(t *testing.T) {
			calls := 0
			raw := ""
			id := ""
			if chain == Tron {
				payload := tronPreparedFixture(time.Now())
				payload["signature"] = []string{strings.Repeat("1", 130)}
				b, _ := json.Marshal(payload)
				raw = string(b)
				id = payload["txID"].(string)
			} else {
				data := "a9059cbb" + strings.Repeat("0", 24) + strings.Repeat("2", 40) + tokenAmount
				b := encodeRLPList([][]byte{{7}, mustHex("04a817c800"), mustHex("011940"), mustHex(EthereumUSDTContract[2:]), {}, mustHex(data), {37}, {1}, {1}})
				raw = "0x" + hex.EncodeToString(b)
				id = "0x" + hex.EncodeToString(keccak(b))
			}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if chain == Tron && r.URL.Path == "/wallet/broadcasttransaction" {
					calls++
					_ = json.NewEncoder(w).Encode(map[string]any{"result": true, "txid": id})
					return
				}
				if chain == Ethereum {
					var req struct {
						Method string `json:"method"`
						ID     uint64 `json:"id"`
					}
					body, _ := io.ReadAll(r.Body)
					_ = json.Unmarshal(body, &req)
					r.Body = http.NoBody
					if req.Method == "eth_sendRawTransaction" {
						calls++
						_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": id})
						return
					}
					r.Body = io.NopCloser(bytes.NewReader(body))
				}
				rpcFixture{chain: chain}.serve(w, r)
			}))
			defer s.Close()
			var a Adapter
			var err error
			if chain == Tron {
				a, err = NewTron(Config{Endpoint: s.URL, AllowBroadcast: true})
			} else {
				a, err = NewEthereum(Config{Endpoint: s.URL, AllowBroadcast: true})
			}
			if err != nil {
				t.Fatal(err)
			}
			result, err := a.Broadcast(context.Background(), SignedTransaction{Chain: chain, TxID: id, RawTransaction: raw})
			if err != nil || !result.Accepted || result.TxID != id || calls != 1 {
				t.Fatalf("broadcast=%+v err=%v calls=%d", result, err, calls)
			}
			_, err = a.Broadcast(context.Background(), SignedTransaction{Chain: chain, TxID: testTx, RawTransaction: raw})
			if !errors.Is(err, ErrInvalidResponse) || calls != 1 {
				t.Fatalf("identity mismatch err=%v calls=%d", err, calls)
			}
		})
	}
}
