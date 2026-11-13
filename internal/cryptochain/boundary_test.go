package cryptochain

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLookupRequiresFinalizedSuccessfulReceiptAndCanonicalInclusion(t *testing.T) {
	for _, chain := range []Chain{Tron, Ethereum} {
		for _, failed := range []bool{false, true} {
			a := fixtureAdapter(t, rpcFixture{chain: chain, failed: failed})
			id := testTx
			if chain == Ethereum {
				id = "0x" + id
			}
			s, err := a.Lookup(context.Background(), id)
			if err != nil {
				t.Fatal(err)
			}
			if !s.Found || !s.Finalized || s.Success == failed || s.BlockHeight != 10 {
				t.Fatalf("%s failed=%v: %+v", chain, failed, s)
			}
			fee := "0"
			if chain == Ethereum {
				fee = "1200000000000000"
			}
			if s.FeeAtomic != fee {
				t.Fatalf("%s fee=%q want %q", chain, s.FeeAtomic, fee)
			}
			if failed && len(s.Transfers) != 0 {
				t.Fatal("failed transfer credited")
			}
			if !failed && (len(s.Transfers) != 1 || s.Transfers[0].OccurredAt.Unix() != 1700000010) {
				t.Fatalf("missing or untimed transfers: %+v", s)
			}
		}
	}
}

func TestUnknownLookupDoesNotClassifyMissingReceiptAsFailed(t *testing.T) {
	for _, chain := range []Chain{Tron, Ethereum} {
		t.Run(string(chain), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if chain == Tron && r.URL.Path == "/walletsolidity/gettransactionbyid" {
					_ = json.NewEncoder(w).Encode(map[string]any{})
					return
				}
				if chain == Ethereum {
					body, _ := io.ReadAll(r.Body)
					var req struct {
						Method string `json:"method"`
						ID     uint64 `json:"id"`
					}
					_ = json.Unmarshal(body, &req)
					if req.Method == "eth_getTransactionReceipt" {
						_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": nil})
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
				a, err = NewTron(Config{Endpoint: s.URL})
			} else {
				a, err = NewEthereum(Config{Endpoint: s.URL})
			}
			if err != nil {
				t.Fatal(err)
			}
			id := testTx
			if chain == Ethereum {
				id = "0x" + id
			}
			status, err := a.Lookup(context.Background(), id)
			if err != nil || status.Found || status.Finalized || len(status.Transfers) != 0 {
				t.Fatalf("%s unknown=%+v err=%v", chain, status, err)
			}
		})
	}
}

func TestLookupNeverInventsNetworkFeeWhenRequiredReceiptFieldIsMissing(t *testing.T) {
	for _, chain := range []Chain{Tron, Ethereum} {
		a := fixtureAdapter(t, rpcFixture{chain: chain, missingFee: true})
		id := testTx
		if chain == Ethereum {
			id = "0x" + id
		}
		s, err := a.Lookup(context.Background(), id)
		if !errors.Is(err, ErrInvalidResponse) || s.Found || s.FeeAtomic != "" {
			t.Fatalf("%s missing fee status=%+v err=%v", chain, s, err)
		}
	}
}

func TestCanonicalBlockHashIncludesEmptyBlocksAndRejectsUnfinalizedHeight(t *testing.T) {
	for _, chain := range []Chain{Tron, Ethereum} {
		a := fixtureAdapter(t, rpcFixture{chain: chain, empty: true})
		hash, err := a.CanonicalBlockHash(context.Background(), 10)
		want := tronBlockHash
		if chain == Ethereum {
			want = "0x" + testHash
		}
		if err != nil || hash != want {
			t.Fatalf("%s hash=%s err=%v", chain, hash, err)
		}
		if _, err := a.CanonicalBlockHash(context.Background(), 13); !errors.Is(err, ErrUnfinalized) {
			t.Fatalf("%s unfinalized err=%v", chain, err)
		}
		if _, err := a.ScanTransfers(context.Background(), 0, 100); !errors.Is(err, ErrInvalidResponse) {
			t.Fatalf("%s over-limit err=%v", chain, err)
		}
	}
}

func TestBroadcastTimeoutRetainsOriginalTransactionAndDoesNotRetry(t *testing.T) {
	payload := tronPreparedFixture(time.Now())
	payload["signature"] = []string{strings.Repeat("1", 130)}
	raw, _ := json.Marshal(payload)
	id := payload["txID"].(string)
	var attempts atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/wallet/broadcasttransaction" {
			attempts.Add(1)
			select {
			case <-r.Context().Done():
			case <-time.After(100 * time.Millisecond):
			}
			return
		}
		rpcFixture{chain: Tron}.serve(w, r)
	}))
	defer s.Close()
	a, err := NewTron(Config{Endpoint: s.URL, AllowBroadcast: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	result, err := a.Broadcast(ctx, SignedTransaction{Chain: Tron, TxID: id, RawTransaction: string(raw)})
	if !errors.Is(err, context.DeadlineExceeded) || result.TxID != id || result.Accepted || attempts.Load() != 1 {
		t.Fatalf("broadcast timeout result=%+v err=%v attempts=%d", result, err, attempts.Load())
	}
}

func TestPreparationRejectsAmbiguousHexGasPrice(t *testing.T) {
	for _, s := range []string{"0x+1", "0x-1", "0x01", "0x", "+1"} {
		if _, err := hexBigQuantity(s); err == nil {
			t.Fatalf("accepted gas price %q", s)
		}
	}
}

func TestEthereumRejectsWrongChainAndNonCanonicalSignedRLP(t *testing.T) {
	data := mustHex("a9059cbb" + strings.Repeat("0", 24) + strings.Repeat("2", 40) + tokenAmount)
	for _, v := range []byte{27, 28, 39, 40} {
		raw := encodeRLPList([][]byte{{1}, {1}, {0x52, 0x08}, mustHex(EthereumUSDTContract[2:]), {}, data, {v}, {1}, {1}})
		if err := validateEthereumSigned(raw); err == nil {
			t.Fatalf("accepted v=%d", v)
		}
	}
	if _, err := decodeRLPList(mustHex("f80180")); err == nil {
		t.Fatal("accepted noncanonical RLP list length")
	}
	if _, err := decodeRLPList(mustHex("c28101")); err == nil {
		t.Fatal("accepted noncanonical string")
	}
}

func TestTronJSONCannotSubstituteSignedProtobufFields(t *testing.T) {
	payload := tronPreparedFixture(time.Now())
	var tx tronUnsigned
	b, _ := json.Marshal(payload)
	_ = json.Unmarshal(b, &tx)
	if _, _, _, err := validateTronPayload(tx, false); err != nil {
		t.Fatal(err)
	}
	tx.RawData.Contract[0].Parameter.Value.Data = "a9059cbb" + topicTo + strings.Repeat("0", 63) + "1"
	if _, _, _, err := validateTronPayload(tx, false); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("JSON/raw mismatch accepted: %v", err)
	}
}
