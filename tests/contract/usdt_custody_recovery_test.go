package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"goepay/internal/cryptochain"
	"goepay/internal/cryptopay"
	"goepay/internal/cryptosigner"
)

func TestUSDTCustodyRecoveryFromInsufficientFeeAndLostBroadcast(t *testing.T) {
	for _, chain := range []cryptochain.Chain{cryptochain.Tron, cryptochain.Ethereum} {
		t.Run(string(chain), func(t *testing.T) {
			ctx := t.Context()
			network := string(chain)
			deposit := custodyAddress(t, chain, 1)
			hot := custodyAddress(t, chain, 2)
			payer := custodyAddress(t, chain, 3)
			recipient := custodyAddress(t, chain, 4)
			service := custodyService(t, network, hot)
			if err := service.AddAddress(ctx, network, deposit, "deposit"); err != nil {
				t.Fatal(err)
			}
			invoice, err := service.CreateInvoice(ctx, cryptopay.CreateInvoiceInput{
				MerchantID: 1, OutTradeNo: "recovery-drill", Network: network, Amount: "10", ExpiresIn: time.Hour,
			})
			if err != nil {
				t.Fatal(err)
			}
			node := newCustodyNode(t, chain)
			node.deposit(payer, invoice.Address, "10000000")
			faults := &custodyRecoveryNode{node: node}
			primaryNode := httptest.NewServer(faults.handler(false))
			defer primaryNode.Close()
			witnessNode := httptest.NewServer(faults.handler(true))
			defer witnessNode.Close()
			signer := &custodyRecoverySigner{signer: custodySigner(t, chain, primaryNode.URL, deposit, hot, payer, recipient)}
			feeLimit := uint64(100000000)
			if chain == cryptochain.Ethereum {
				feeLimit = 2000000000000000
			}
			worker := func() *cryptopay.Executor {
				return &cryptopay.Executor{
					Service:   service,
					Adapters:  map[string]cryptochain.Adapter{network: custodyAdapter(t, chain, primaryNode.URL, true)},
					Witness:   map[string]cryptochain.Adapter{network: custodyAdapter(t, chain, witnessNode.URL, false)},
					Signer:    signer,
					FeeLimits: map[string]uint64{network: feeLimit},
				}
			}
			executor := worker()
			scanner := cryptopay.Scanner{Service: service, Primary: executor.Adapters, Witness: executor.Witness, StartHeight: map[string]uint64{network: 1}}
			if err = scanner.Scan(ctx, network); err != nil {
				t.Fatal(err)
			}
			sweep, err := executor.RequestSweep(ctx, network, deposit, "recovery-sweep")
			if err != nil {
				t.Fatal(err)
			}
			faults.set(custodyRecoveryFaults{shortPreparationFee: true})
			assertUnsignedFeeFailure(t, executor, sweep.ID)
			custodyBalances(t, service, network, "9.9", "0")
			if _, err = executor.RequestSweep(ctx, network, deposit, "duplicate-active-sweep"); !errors.Is(err, cryptopay.ErrConflict) {
				t.Fatalf("a second sweep replaced the active one: %v", err)
			}
			faults.set(custodyRecoveryFaults{})
			custodyConfirm(t, service, executor, node, sweep.ID, deposit, hot, "10000000")

			address := recipient
			if chain == cryptochain.Tron {
				address = payer
			}
			record, err := service.RequestRecipient(ctx, 1, network, address)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = service.ApproveRecipient(ctx, record.ID, 9); err != nil {
				t.Fatal(err)
			}
			// Advance only this synthetic recipient through its cooling period.
			if _, err = service.Pool.Exec(ctx, "UPDATE crypto_recipients SET usable_at=clock_timestamp()-interval '1 second' WHERE id=$1", record.ID); err != nil {
				t.Fatal(err)
			}
			request := func() (cryptopay.Withdrawal, error) {
				if chain == cryptochain.Tron {
					return service.Refund(ctx, cryptopay.RefundInput{MerchantID: 1, RequestID: "recovery-original", InvoiceID: invoice.ID, Address: address, Amount: "6"})
				}
				return service.RequestWithdrawal(ctx, cryptopay.WithdrawalInput{MerchantID: 1, RequestID: "recovery-original", Network: network, Address: address, Amount: "6"})
			}
			outgoing, err := request()
			if err != nil {
				t.Fatal(err)
			}
			if _, err = service.ApproveWithdrawal(ctx, outgoing.ID, 9); err != nil {
				t.Fatal(err)
			}
			faults.set(custodyRecoveryFaults{shortPreparationFee: true})
			assertUnsignedFeeFailure(t, executor, outgoing.ID)
			custodyBalances(t, service, network, "3.9", "6")
			custodyReconcile(t, service)

			// The node may prepare the transfer but reject the signed
			// submission for insufficient fee resources.
			faults.set(custodyRecoveryFaults{shortBroadcastFee: true})
			if err = executor.RunPendingFor(ctx, network); err == nil {
				t.Fatal("native fee rejection was treated as broadcast success")
			}
			original, err := service.GetWithdrawal(ctx, outgoing.ID)
			if err != nil || original.State != "unknown" || original.TxID == "" {
				t.Fatalf("original signed transaction not retained: %+v %v", original, err)
			}
			if _, err = service.CancelWithdrawal(ctx, 1, original.ID); !errors.Is(err, cryptopay.ErrConflict) {
				t.Fatalf("unknown transaction became cancellable: %v", err)
			}
			if replay, err := request(); err != nil || replay.ID != original.ID {
				t.Fatalf("request replay changed the original identity: %+v %v", replay, err)
			}
			assertRecoverySettlement(t, service, original, "6", 0)

			// After simulated fee funding the node accepts the same bytes,
			// but the response is lost and neither node can show a receipt yet.
			faults.set(custodyRecoveryFaults{loseReply: true, hideMain: true, hideWitness: true})
			if err = executor.RunPendingFor(ctx, network); err == nil {
				t.Fatal("missing broadcast response was treated as success")
			}
			unknown, err := service.GetWithdrawal(ctx, outgoing.ID)
			if err != nil || unknown.State != "unknown" || unknown.TxID != original.TxID {
				t.Fatalf("lost reply changed original intent: %+v %v", unknown, err)
			}
			assertRecoverySettlement(t, service, original, "6", 0)

			// Recreate the dispatcher and node adapters. The persisted signed bytes
			// are rebroadcast while the transaction remains invisible to both nodes.
			restarted := worker()
			if err = restarted.RunPendingFor(ctx, network); err != nil {
				t.Fatalf("rebroadcast of original transaction: %v", err)
			}
			assertRecoverySettlement(t, service, original, "6", 0)

			faults.set(custodyRecoveryFaults{hideWitness: true})
			if err = restarted.RunPendingFor(ctx, network); err == nil {
				t.Fatal("primary-only receipt finalized the payout")
			}
			assertRecoverySettlement(t, service, original, "6", 0)

			faults.set(custodyRecoveryFaults{})
			for i := 0; i < 2; i++ {
				if err = restarted.RunPendingFor(ctx, network); err != nil {
					t.Fatalf("dual-node finalize/replay: %v", err)
				}
			}
			actual, err := service.GetWithdrawal(ctx, outgoing.ID)
			if err != nil || actual.State != "confirmed" || actual.TxID != original.TxID || actual.RequestID != outgoing.RequestID {
				t.Fatalf("original payout did not finalize: %+v %v", actual, err)
			}
			assertRecoverySettlement(t, service, original, "0", 1)
			if signer.calls != 2 {
				t.Fatalf("expected one sweep signature and one payout signature, got %d", signer.calls)
			}
			faults.assertRebroadcast(t)
			node.mu.Lock()
			defer node.mu.Unlock()
			if tx := node.transactions[original.TxID]; tx.from != hot || tx.to != address || tx.amount != "6000000" || node.broadcasts != 2 {
				t.Fatalf("synthetic chain contains unexpected transfers: %+v, accepted=%d", tx, node.broadcasts)
			}
		})
	}
}

func assertUnsignedFeeFailure(t *testing.T, worker *cryptopay.Executor, id string) {
	t.Helper()
	if err := worker.RunPendingFor(t.Context(), ""); err == nil {
		t.Fatal("node admitted a preparation with insufficient native fee")
	}
	var state, txID string
	var prepared, signed bool
	err := worker.Service.Pool.QueryRow(t.Context(), `SELECT state,tx_id,prepared IS NOT NULL,signed IS NOT NULL FROM crypto_withdrawals WHERE id=$1`, id).Scan(&state, &txID, &prepared, &signed)
	if err != nil || state != "preparing" || txID != "" || prepared || signed {
		t.Fatalf("fee rejection crossed the signing boundary: state=%s txid=%s prepared=%t signed=%t err=%v", state, txID, prepared, signed, err)
	}
}

func assertRecoverySettlement(t *testing.T, service *cryptopay.Service, original cryptopay.Withdrawal, reserved string, finalized int) {
	t.Helper()
	custodyBalances(t, service, original.Network, "3.9", reserved)
	custodyReconcile(t, service)
	var outgoing, fees int
	var cny int64
	err := service.Pool.QueryRow(t.Context(), `
		SELECT (SELECT count(*) FROM crypto_journals WHERE reference=$1),
		       (SELECT count(*) FROM crypto_network_fees WHERE network=$2 AND tx_id=$3),
		       (SELECT balance FROM accounts WHERE owner_id=1 AND kind='available')
	`, "outgoing:"+original.ID, original.Network, original.TxID).Scan(&outgoing, &fees, &cny)
	if err != nil || outgoing != finalized || fees != finalized || cny != 12345 {
		t.Fatalf("unexpected recovery ledger counts: outgoing=%d fees=%d CNY=%d err=%v", outgoing, fees, cny, err)
	}
}

type custodyRecoverySigner struct {
	signer *cryptosigner.Service
	calls  int
}

func (s *custodyRecoverySigner) Sign(ctx context.Context, tx cryptochain.PreparedTransaction) (cryptochain.SignedTransaction, error) {
	s.calls++
	return s.signer.Sign(ctx, tx)
}

type custodyRecoveryNode struct {
	mu     sync.Mutex
	node   *custodyNode
	faults custodyRecoveryFaults
	sends  [][]byte
}

type custodyRecoveryFaults struct {
	shortPreparationFee bool
	shortBroadcastFee   bool
	loseReply           bool
	hideMain            bool
	hideWitness         bool
}

func (n *custodyRecoveryNode) set(faults custodyRecoveryFaults) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.faults = faults
}

func (n *custodyRecoveryNode) assertRebroadcast(t *testing.T) {
	t.Helper()
	n.mu.Lock()
	defer n.mu.Unlock()
	if len(n.sends) != 4 || !bytes.Equal(n.sends[1], n.sends[2]) || !bytes.Equal(n.sends[1], n.sends[3]) {
		t.Fatalf("expected one sweep and three byte-identical payout submissions, observed %d total submissions", len(n.sends))
	}
}

func (n *custodyRecoveryNode) handler(witness bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "fixture request read failed", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var rpc struct {
			Method string `json:"method"`
			ID     uint64 `json:"id"`
		}
		if err = json.Unmarshal(body, &rpc); err != nil {
			http.Error(w, "fixture request is invalid JSON", http.StatusBadRequest)
			return
		}
		n.mu.Lock()
		defer n.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if n.faults.shortPreparationFee {
			switch {
			case rpc.Method == "eth_estimateGas":
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "error": map[string]any{"code": -32000, "message": "insufficient funds for gas"}})
				return
			case r.URL.Path == "/wallet/triggersmartcontract":
				io.WriteString(w, `{"result":{"result":false},"Error":"insufficient native fee resources"}`)
				return
			}
		}
		if (!witness && n.faults.hideMain) || (witness && n.faults.hideWitness) {
			switch {
			case rpc.Method == "eth_getTransactionReceipt":
				json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": nil})
				return
			case r.URL.Path == "/walletsolidity/gettransactionbyid":
				io.WriteString(w, `{}`)
				return
			}
		}
		if rpc.Method == "eth_sendRawTransaction" || r.URL.Path == "/wallet/broadcasttransaction" {
			n.sends = append(n.sends, body)
			if n.faults.shortBroadcastFee {
				if rpc.Method == "eth_sendRawTransaction" {
					json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "error": map[string]any{"code": -32000, "message": "insufficient funds for gas"}})
				} else {
					io.WriteString(w, `{"result":false,"code":"BANDWITH_ERROR"}`)
				}
				return
			}
			if n.faults.loseReply {
				n.faults.loseReply = false
				recorder := httptest.NewRecorder()
				n.node.serve(recorder, r)
				if recorder.Code != http.StatusOK {
					n.node.t.Errorf("fixture did not accept the original transaction: %d", recorder.Code)
				}
				http.Error(w, "simulated lost upstream acknowledgement", http.StatusGatewayTimeout)
				return
			}
		}
		n.node.serve(w, r)
	}
}
