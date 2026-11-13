package cryptochain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testTx = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const testHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const tronBlockHash = "000000000000000aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const topicFrom = "0000000000000000000000000000000000000000000000000000000000000000"
const topicTo = "000000000000000000000000a614f803b6fd780986a42c78ec9c7f77e6ded13c"
const tokenAmount = "00000000000000000000000000000000000000000000000000000000000f4241"

type rpcFixture struct {
	chain                                                                              Chain
	badNetwork, failed, malicious, duplicate, mismatch, unfinalized, empty, missingFee bool
}

func (f rpcFixture) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var req struct {
		Method           string            `json:"method"`
		Params           []json.RawMessage `json:"params"`
		ID               uint64            `json:"id"`
		Num              uint64            `json:"num"`
		Parameter        string            `json:"parameter"`
		FunctionSelector string            `json:"function_selector"`
		ContractAddress  string            `json:"contract_address"`
		Visible          bool              `json:"visible"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	respond := func(v any) {
		if f.chain == Ethereum {
			_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": v})
		} else {
			_ = json.NewEncoder(w).Encode(v)
		}
	}
	if f.chain == Ethereum {
		log := map[string]any{"address": EthereumUSDTContract, "topics": []string{"0x" + transferTopic, "0x" + strings.Repeat("0", 24) + strings.Repeat("1", 40), "0x" + strings.Repeat("0", 24) + strings.Repeat("2", 40)}, "data": "0x" + tokenAmount, "blockNumber": "0xa", "blockHash": "0x" + testHash, "transactionHash": "0x" + testTx, "logIndex": "0x2", "removed": false}
		if f.malicious {
			log["address"] = "0x" + strings.Repeat("3", 40)
		}
		switch req.Method {
		case "eth_chainId":
			if f.badNetwork {
				respond("0x5")
			} else {
				respond("0x1")
			}
		case "eth_getBlockByNumber":
			var tag string
			_ = json.Unmarshal(req.Params[0], &tag)
			if tag == "0x0" {
				respond(map[string]any{"number": "0x0", "hash": EthereumMainnetGenesis})
			} else if tag == "finalized" {
				height := "0xc"
				if f.unfinalized {
					height = "0x9"
				}
				respond(map[string]any{"number": height, "hash": "0x" + testHash, "timestamp": "0x6553f10a"})
			} else {
				respond(map[string]any{"number": tag, "hash": "0x" + testHash, "timestamp": "0x6553f10a"})
			}
		case "eth_getLogs":
			var filter map[string]any
			_ = json.Unmarshal(req.Params[0], &filter)
			if filter["address"] != EthereumUSDTContract {
				http.Error(w, "missing token filter", 400)
				return
			}
			logs := []any{log}
			if f.duplicate {
				logs = append(logs, log)
			}
			respond(logs)
		case "eth_getTransactionReceipt":
			status := "0x1"
			if f.failed {
				status = "0x0"
			}
			receiptHash := "0x" + testHash
			if f.mismatch {
				receiptHash = "0x" + strings.Repeat("d", 64)
			}
			receipt := map[string]any{"transactionHash": "0x" + testTx, "blockNumber": "0xa", "blockHash": receiptHash, "status": status, "gasUsed": "0xea60", "effectiveGasPrice": "0x4a817c800", "logs": []any{log}}
			if f.missingFee {
				delete(receipt, "effectiveGasPrice")
			}
			respond(receipt)
		case "eth_call":
			var call map[string]string
			var tag string
			_ = json.Unmarshal(req.Params[0], &call)
			_ = json.Unmarshal(req.Params[1], &tag)
			if tag != "finalized" || call["to"] != EthereumUSDTContract || call["data"] != "0x70a08231"+strings.Repeat("0", 24)+"dac17f958d2ee523a2206206994597c13d831ec7" {
				http.Error(w, "incorrect finalized balance ABI call", 400)
				return
			}
			respond("0x" + tokenAmount)
		case "eth_getTransactionCount":
			respond("0x7")
		case "eth_gasPrice":
			respond("0x4a817c800")
		case "eth_estimateGas":
			respond("0xea60")
		case "eth_sendRawTransaction":
			respond("0x" + testTx)
		default:
			http.Error(w, "unexpected method "+req.Method, 400)
		}
		return
	}
	log := map[string]any{"address": "a614f803b6fd780986a42c78ec9c7f77e6ded13c", "topics": []string{transferTopic, topicFrom, topicTo}, "data": tokenAmount}
	if f.malicious {
		log["address"] = strings.Repeat("3", 40)
	}
	switch r.URL.Path {
	case "/walletsolidity/getblockbynum", "/wallet/getblockbynum":
		if req.Num == 0 {
			hash := TronMainnetGenesis
			if f.badNetwork {
				hash = strings.Repeat("d", 64)
			}
			respond(map[string]any{"blockID": hash, "block_header": map[string]any{"raw_data": map[string]any{"number": 0}}})
			return
		}
		txs := []any{map[string]any{"txID": testTx, "raw_data": map[string]any{"contract": []any{map[string]any{"type": "TriggerSmartContract"}}}}}
		if f.duplicate {
			txs = append(txs, txs[0])
		}
		if f.empty {
			txs = nil
		}
		respond(map[string]any{"blockID": tronBlockHash, "block_header": map[string]any{"raw_data": map[string]any{"number": req.Num, "timestamp": 1700000010000}}, "transactions": txs})
	case "/walletsolidity/getnowblock":
		height := 12
		if f.unfinalized {
			height = 9
		}
		respond(map[string]any{"blockID": fmt.Sprintf("%016x%s", height, strings.Repeat("a", 48)), "block_header": map[string]any{"raw_data": map[string]any{"number": height}}})
	case "/walletsolidity/gettransactionbyid":
		respond(map[string]any{"txID": testTx, "ret": []any{map[string]any{"contractRet": "SUCCESS"}}})
	case "/walletsolidity/gettransactioninfobyid":
		status := "SUCCESS"
		if f.failed {
			status = "REVERT"
		}
		height := 10
		if f.mismatch {
			height = 11
		}
		receipt := map[string]any{"id": testTx, "blockNumber": height, "fee": 0, "receipt": map[string]any{"result": status}, "log": []any{log}}
		if f.missingFee {
			delete(receipt, "fee")
		}
		respond(receipt)
	case "/walletsolidity/triggerconstantcontract":
		if len(req.Parameter) != 64 || req.Parameter != "000000000000000000000000a614f803b6fd780986a42c78ec9c7f77e6ded13c" || req.FunctionSelector != "balanceOf(address)" || req.ContractAddress != TronUSDTContract || !req.Visible {
			http.Error(w, "incorrect solidified balance ABI parameter", 400)
			return
		}
		respond(map[string]any{"result": map[string]any{"result": true}, "constant_result": []string{tokenAmount}})
	case "/wallet/broadcasttransaction":
		respond(map[string]any{"result": true, "txid": testTx})
	default:
		http.Error(w, "unexpected path "+r.URL.Path, 400)
	}
}

func fixtureAdapter(t *testing.T, f rpcFixture) Adapter {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(s.Close)
	var a Adapter
	var err error
	if f.chain == Tron {
		a, err = NewTron(Config{Endpoint: s.URL})
	} else {
		a, err = NewEthereum(Config{Endpoint: s.URL})
	}
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestScannersOnlyCreditSuccessfulFinalizedAllowlistedTransfers(t *testing.T) {
	for _, chain := range []Chain{Tron, Ethereum} {
		t.Run(string(chain), func(t *testing.T) {
			for _, tc := range []struct {
				name    string
				f       rpcFixture
				want    int
				wantErr error
			}{
				{"successful", rpcFixture{chain: chain}, 1, nil},
				{"duplicate", rpcFixture{chain: chain, duplicate: true}, 1, nil},
				{"malicious-token", rpcFixture{chain: chain, malicious: true}, 0, nil},
				{"failed-receipt", rpcFixture{chain: chain, failed: true}, 0, nil},
				{"wrong-network", rpcFixture{chain: chain, badNetwork: true}, 0, ErrWrongNetwork},
				{"unfinalized", rpcFixture{chain: chain, unfinalized: true}, 0, ErrUnfinalized},
				{"receipt-block-mismatch", rpcFixture{chain: chain, mismatch: true}, 0, ErrInvalidResponse},
			} {
				t.Run(tc.name, func(t *testing.T) {
					a := fixtureAdapter(t, tc.f)
					got, err := a.ScanTransfers(context.Background(), 10, 10)
					if tc.wantErr != nil {
						if !errors.Is(err, tc.wantErr) {
							t.Fatalf("got %v, want %v", err, tc.wantErr)
						}
						return
					}
					if err != nil {
						t.Fatal(err)
					}
					if len(got) != tc.want {
						t.Fatalf("got %+v, want %d events", got, tc.want)
					}
					if len(got) > 0 {
						if got[0].AtomicAmount != "1000001" || got[0].BlockHeight != 10 || got[0].Chain != chain || got[0].OccurredAt.Unix() != 1700000010 {
							t.Fatalf("wrong parsed event %+v", got[0])
						}
						if chain == Tron && got[0].To != TronUSDTContract {
							t.Fatalf("wrong tron destination %s", got[0].To)
						}
						if chain == Ethereum && got[0].To != "0x"+strings.Repeat("2", 40) {
							t.Fatal(got[0].To)
						}
					}
				})
			}
		})
	}
}

func TestBalanceUsesExactAtomicIntegerAndFinalizedView(t *testing.T) {
	for _, chain := range []Chain{Tron, Ethereum} {
		a := fixtureAdapter(t, rpcFixture{chain: chain})
		address := EthereumUSDTContract
		if chain == Tron {
			address = TronUSDTContract
		}
		got, err := a.Balance(context.Background(), address)
		if err != nil || got != "1000001" {
			t.Fatalf("%s balance=%s %v", chain, got, err)
		}
	}
}

func TestNodeTimeoutPreservesUnknownState(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}))
	defer s.Close()
	for _, chain := range []Chain{Tron, Ethereum} {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		var a Adapter
		var err error
		if chain == Tron {
			a, err = NewTron(Config{Endpoint: s.URL})
		} else {
			a, err = NewEthereum(Config{Endpoint: s.URL})
		}
		if err != nil {
			t.Fatal(err)
		}
		_, err = a.Lookup(ctx, fmt.Sprintf("%s%s", map[bool]string{true: "0x"}[chain == Ethereum], testTx))
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("%s timeout=%v", chain, err)
		}
	}
}

func TestBroadcastIsDisabledUnlessExplicitlyEnabled(t *testing.T) {
	for _, chain := range []Chain{Tron, Ethereum} {
		a := fixtureAdapter(t, rpcFixture{chain: chain})
		_, err := a.Broadcast(context.Background(), SignedTransaction{Chain: chain})
		if !errors.Is(err, ErrBroadcastDisabled) {
			t.Fatalf("%s: %v", chain, err)
		}
	}
}
