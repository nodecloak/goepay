package cryptosigner

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"goepay/internal/cryptochain"
)

func TestEthereumAdapterPrepareSignerAndLocalBroadcastAreCompatible(t *testing.T) {
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			ID     uint64            `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		var result any
		switch request.Method {
		case "eth_chainId":
			result = "0x1"
		case "eth_getBlockByNumber":
			result = map[string]any{"number": "0x0", "hash": cryptochain.EthereumMainnetGenesis}
		case "eth_gasPrice":
			result = "0x4a817c800"
		case "eth_estimateGas":
			result = "0xea60"
		case "eth_sendRawTransaction":
			var raw string
			if err := json.Unmarshal(request.Params[0], &raw); err != nil {
				t.Error(err)
			}
			b, err := decodeHex(raw)
			if err != nil {
				t.Error(err)
			}
			result = "0x" + hex.EncodeToString(keccak(b))
		default:
			t.Errorf("unexpected node method: %s", request.Method)
			w.WriteHeader(400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
	}))
	defer node.Close()
	adapter, err := cryptochain.NewEthereum(cryptochain.Config{Endpoint: node.URL, AllowBroadcast: true})
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(testConfig(filepath.Join(t.TempDir(), "journal.jsonl")), testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	nonce := uint64(42)
	p, err := adapter.Prepare(context.Background(), cryptochain.TransferRequest{RequestID: "adapter-transfer", From: "0x7e5f4552091a69125d5dfcb7b8c2659029395bdf", To: "0x2222222222222222222222222222222222222222", AtomicAmount: "100000", Nonce: &nonce, FeeLimit: 2000000000000000})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := service.Sign(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err = VerifySigned(p, signed); err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Broadcast(context.Background(), signed)
	if err != nil || !result.Accepted || result.TxID != signed.TxID {
		t.Fatalf("local broadcast failed: %#v %v", result, err)
	}
}

func TestTRONAdapterPrepareSignerAndLocalBroadcastAreCompatible(t *testing.T) {
	now := time.Now().UTC()
	fixture := tronPrepared("adapter-tron", now)
	data := "a9059cbb000000000000000000000000222222222222222222222222222222222222222200000000000000000000000000000000000000000000000000000000000186a0"
	rawData := map[string]any{"ref_block_bytes": "0064", "ref_block_hash": "1122334455667788", "timestamp": now.UnixMilli(), "expiration": fixture.Expiration, "fee_limit": 100000000, "contract": []any{map[string]any{"type": "TriggerSmartContract", "parameter": map[string]any{"type_url": "type.googleapis.com/protocol.TriggerSmartContract", "value": map[string]any{"owner_address": "417e5f4552091a69125d5dfcb7b8c2659029395bdf", "contract_address": "41a614f803b6fd780986a42c78ec9c7f77e6ded13c", "data": data}}}}}
	payload := map[string]any{"txID": fixture.TxID, "raw_data_hex": fixture.SigningPayload, "raw_data": rawData}
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/wallet/triggersmartcontract":
			json.NewEncoder(w).Encode(map[string]any{"result": map[string]bool{"result": true}, "transaction": payload})
			return
		case "/wallet/broadcasttransaction":
			var signed struct {
				TxID string `json:"txID"`
			}
			if err := json.NewDecoder(r.Body).Decode(&signed); err != nil {
				t.Error(err)
			}
			json.NewEncoder(w).Encode(map[string]any{"result": true, "txid": signed.TxID})
			return
		case "/wallet/getnowblock", "/wallet/getblockbynum", "/walletsolidity/getblockbynum":
			n := uint64(100)
			if r.URL.Path != "/wallet/getnowblock" {
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
			return
		default:
			t.Errorf("unexpected node route: %s", r.URL.Path)
			w.WriteHeader(400)
		}
	}))
	defer node.Close()
	adapter, err := cryptochain.NewTron(cryptochain.Config{Endpoint: node.URL, AllowBroadcast: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{StatePath: filepath.Join(t.TempDir(), "journal.jsonl"), Rules: []Rule{{Chain: cryptochain.Tron, Network: "mainnet", ChainID: "728126428", Contract: cryptochain.TronUSDTContract, From: fixture.From, KeyID: "treasury", Destinations: []string{fixture.To}, MaxAmount: "1000000", DailyLimit: "1000000", MaxDailyTransactions: 100, MaxDailyFeeAtomic: "1000000000000", MaxFeeLimit: 100000000, MaxTTLSeconds: 300, TRONRPCURL: node.URL}}}
	service, err := NewService(cfg, testKeys(t))
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	p, err := adapter.Prepare(context.Background(), cryptochain.TransferRequest{RequestID: fixture.RequestID, From: fixture.From, To: fixture.To, AtomicAmount: fixture.AtomicAmount, FeeLimit: 100000000})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := service.Sign(context.Background(), p)
	if err != nil {
		t.Fatal(err)
	}
	if err = VerifySigned(p, signed); err != nil {
		t.Fatal(err)
	}
	result, err := adapter.Broadcast(context.Background(), signed)
	if err != nil || !result.Accepted || result.TxID != signed.TxID {
		t.Fatalf("local broadcast failed: %#v %v", result, err)
	}
}
