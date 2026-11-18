package contract_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/cryptochain"
	"goepay/internal/cryptopay"
	"goepay/internal/cryptosigner"
	"golang.org/x/crypto/sha3"
)

// This test uses real node protocol adapters, real secp256k1 signatures and a
// real isolated PostgreSQL schema. Only the external blockchain is a local HTTP
// fixture; no domain port, balance mutation, scanner or executor is mocked.
func TestUSDTCustodyLifecycleWithRealAdaptersAndSigner(t *testing.T) {
	for _, chain := range []cryptochain.Chain{cryptochain.Tron, cryptochain.Ethereum} {
		t.Run(string(chain), func(t *testing.T) {
			ctx := t.Context()
			network := string(chain)
			deposit, hot, payer, merchant := custodyAddress(t, chain, 1), custodyAddress(t, chain, 2), custodyAddress(t, chain, 3), custodyAddress(t, chain, 4)
			service := custodyService(t, network, hot)
			if err := service.AddAddress(ctx, network, deposit, "deposit"); err != nil {
				t.Fatal(err)
			}
			invoice, err := service.CreateInvoice(ctx, cryptopay.CreateInvoiceInput{MerchantID: 1, OutTradeNo: "full-lifecycle-" + network, Network: network, Amount: "10", CNYAmount: 9900, ExpiresIn: time.Hour})
			if err != nil {
				t.Fatal(err)
			}
			if invoice.Address != deposit || invoice.State != "pending" {
				t.Fatalf("unexpected new invoice: %+v", invoice)
			}
			chainState := newCustodyNode(t, chain)
			chainState.deposit(payer, deposit, "10000000")
			primaryNode := httptest.NewServer(http.HandlerFunc(chainState.serve))
			defer primaryNode.Close()
			witnessNode := httptest.NewServer(http.HandlerFunc(chainState.serve))
			defer witnessNode.Close()
			primary := custodyAdapter(t, chain, primaryNode.URL, true)
			witness := custodyAdapter(t, chain, witnessNode.URL, false)
			scanner := cryptopay.Scanner{Service: service, Primary: map[string]cryptochain.Adapter{network: primary}, Witness: map[string]cryptochain.Adapter{network: witness}, StartHeight: map[string]uint64{network: 1}, MaxBlocks: 20}
			if err = scanner.Scan(ctx, network); err != nil {
				t.Fatal(err)
			}
			if err = scanner.Scan(ctx, network); err != nil {
				t.Fatal(err)
			}
			paid, err := service.GetInvoice(ctx, 1, invoice.ID)
			if err != nil || paid.State != "paid" || paid.Received != "10" || paid.Fee != "0.1" || paid.CNYAmount != 9900 {
				t.Fatalf("invoice not credited from final nodes: %+v %v", paid, err)
			}
			custodyBalances(t, service, network, "9.9", "0")
			var depositCount int
			if err = service.Pool.QueryRow(ctx, "SELECT count(*) FROM crypto_deposits").Scan(&depositCount); err != nil || depositCount != 1 {
				t.Fatalf("duplicate scanner credit: %d %v", depositCount, err)
			}
			custodyReconcile(t, service)
			signer := custodySigner(t, chain, primaryNode.URL, deposit, hot, payer, merchant)
			executor := cryptopay.Executor{Service: service, Adapters: map[string]cryptochain.Adapter{network: primary}, Witness: map[string]cryptochain.Adapter{network: witness}, Signer: signer, FeeLimits: map[string]uint64{network: 100000000}}
			if chain == cryptochain.Ethereum {
				executor.FeeLimits[network] = 2000000000000000
			}
			sweep, err := executor.RequestSweep(ctx, network, deposit, "sweep-paid-invoice")
			if err != nil {
				t.Fatal(err)
			}
			custodyConfirm(t, service, &executor, chainState, sweep.ID, deposit, hot, "10000000")
			custodyBalances(t, service, network, "9.9", "0")
			for i, address := range []string{payer, merchant} {
				recipient, err := service.RequestRecipient(ctx, 1, network, address)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = service.ApproveRecipient(ctx, recipient.ID, 9); err != nil {
					t.Fatal(err)
				}
				// Fixture-only elapsed cooling period; production approval is unchanged.
				if _, err = service.Pool.Exec(ctx, "UPDATE crypto_recipients SET usable_at=clock_timestamp()-interval '1 second' WHERE id=$1", recipient.ID); err != nil {
					t.Fatalf("recipient %d: %v", i, err)
				}
			}
			refund, err := service.Refund(ctx, cryptopay.RefundInput{MerchantID: 1, RequestID: "merchant-refund", InvoiceID: invoice.ID, Address: payer, Amount: "1"})
			if err != nil {
				t.Fatal(err)
			}
			custodyBalances(t, service, network, "8.9", "1")
			if _, err = service.ApproveWithdrawal(ctx, refund.ID, 9); err != nil {
				t.Fatal(err)
			}
			custodyConfirm(t, service, &executor, chainState, refund.ID, hot, payer, "1000000")
			custodyBalances(t, service, network, "8.9", "0")
			settlement, err := service.RequestWithdrawal(ctx, cryptopay.WithdrawalInput{MerchantID: 1, RequestID: "merchant-settlement", Network: network, Address: merchant, Amount: "8.9"})
			if err != nil {
				t.Fatal(err)
			}
			custodyBalances(t, service, network, "0", "8.9")
			if _, err = service.ApproveWithdrawal(ctx, settlement.ID, 9); err != nil {
				t.Fatal(err)
			}
			custodyConfirm(t, service, &executor, chainState, settlement.ID, hot, merchant, "8900000")
			custodyBalances(t, service, network, "0", "0")
			if err = scanner.Scan(ctx, network); err != nil {
				t.Fatal(err)
			}
			custodyReconcile(t, service)
			var reserved, available, fees, cny int64
			if err = service.Pool.QueryRow(ctx, "SELECT COALESCE(sum(balance) FILTER(WHERE kind='reserved'),0)::bigint,COALESCE(sum(balance) FILTER(WHERE kind='available'),0)::bigint,COALESCE(sum(balance) FILTER(WHERE kind='fees'),0)::bigint FROM crypto_accounts WHERE network=$1", network).Scan(&reserved, &available, &fees); err != nil {
				t.Fatal(err)
			}
			if err = service.Pool.QueryRow(ctx, "SELECT balance FROM accounts WHERE owner_id=1 AND kind='available'").Scan(&cny); err != nil {
				t.Fatal(err)
			}
			if reserved != 0 || available != 0 || fees != 100000 || cny != 12345 {
				t.Fatalf("final independent ledgers: reserved=%d available=%d fees=%d CNY=%d", reserved, available, fees, cny)
			}
			var nativeFeeRows int
			var nativeAsset, nativeTotal string
			if err = service.Pool.QueryRow(ctx, "SELECT count(*),min(asset),sum(atomic_amount)::text FROM crypto_network_fees WHERE network=$1", network).Scan(&nativeFeeRows, &nativeAsset, &nativeTotal); err != nil {
				t.Fatal(err)
			}
			wantAsset, wantNative := "TRX", "1500000"
			if chain == cryptochain.Ethereum {
				wantAsset, wantNative = "ETH", "3600000000000000"
			}
			if nativeFeeRows != 3 || nativeAsset != wantAsset || nativeTotal != wantNative {
				t.Fatalf("native fee receipts must be recorded exactly once: count=%d asset=%s total=%s", nativeFeeRows, nativeAsset, nativeTotal)
			}
			chainState.mu.Lock()
			defer chainState.mu.Unlock()
			if chainState.balance(deposit) != "0" || chainState.balance(hot) != "100000" || chainState.balance(payer) != "1000000" || chainState.balance(merchant) != "8900000" || chainState.broadcasts != 3 {
				t.Fatalf("wrong chain asset movements: balances=%v broadcasts=%d", chainState.balances, chainState.broadcasts)
			}
		})
	}
}

func TestUSDTEthereumCancelledPreparedNonceIsSafelyReused(t *testing.T) {
	ctx := t.Context()
	chain := cryptochain.Ethereum
	network := string(chain)
	deposit, hot, payer, merchant := custodyAddress(t, chain, 1), custodyAddress(t, chain, 2), custodyAddress(t, chain, 3), custodyAddress(t, chain, 4)
	service := custodyService(t, network, hot)
	if err := service.AddAddress(ctx, network, deposit, "deposit"); err != nil {
		t.Fatal(err)
	}
	invoice, err := service.CreateInvoice(ctx, cryptopay.CreateInvoiceInput{MerchantID: 1, OutTradeNo: "unsigned-nonce-recovery", Network: network, Amount: "10", ExpiresIn: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	state := newCustodyNode(t, chain)
	state.deposit(payer, invoice.Address, "10000000")
	primaryNode := httptest.NewServer(http.HandlerFunc(state.serve))
	defer primaryNode.Close()
	witnessNode := httptest.NewServer(http.HandlerFunc(state.serve))
	defer witnessNode.Close()
	primary := custodyAdapter(t, chain, primaryNode.URL, true)
	witness := custodyAdapter(t, chain, witnessNode.URL, false)
	scanner := cryptopay.Scanner{Service: service, Primary: map[string]cryptochain.Adapter{network: primary}, Witness: map[string]cryptochain.Adapter{network: witness}, StartHeight: map[string]uint64{network: 1}}
	if err = scanner.Scan(ctx, network); err != nil {
		t.Fatal(err)
	}
	signer := custodySigner(t, chain, primaryNode.URL, deposit, hot, payer, merchant)
	executor := cryptopay.Executor{Service: service, Adapters: map[string]cryptochain.Adapter{network: primary}, Witness: map[string]cryptochain.Adapter{network: witness}, Signer: signer, FeeLimits: map[string]uint64{network: 2000000000000000}}
	sweep, err := executor.RequestSweep(ctx, network, deposit, "fund-hot-wallet")
	if err != nil {
		t.Fatal(err)
	}
	custodyConfirm(t, service, &executor, state, sweep.ID, deposit, hot, "10000000")
	recipient, err := service.RequestRecipient(ctx, 1, network, merchant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApproveRecipient(ctx, recipient.ID, 9); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Pool.Exec(ctx, "UPDATE crypto_recipients SET usable_at=clock_timestamp()-interval '1 second' WHERE id=$1", recipient.ID); err != nil {
		t.Fatal(err)
	}
	original, err := service.RequestWithdrawal(ctx, cryptopay.WithdrawalInput{MerchantID: 1, RequestID: "unsigned-original", Network: network, Address: merchant, Amount: "6"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApproveWithdrawal(ctx, original.ID, 9); err != nil {
		t.Fatal(err)
	}
	prepared, err := primary.Prepare(ctx, cryptochain.TransferRequest{RequestID: original.ID, From: hot, To: merchant, AtomicAmount: "6000000", FeeLimit: 2000000000000000})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Nonce != 0 {
		t.Fatalf("original nonce=%d, want 0", prepared.Nonce)
	}
	// Persist the exact production boundary immediately before the signer call,
	// as if the worker stopped after preparing but before accepting any key use.
	raw, err := json.Marshal(prepared)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := service.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "INSERT INTO crypto_nonces(network,address,next_nonce) VALUES($1,$2,1)", network, hot); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE crypto_withdrawals SET state='prepared',prepared=$2,nonce=0 WHERE id=$1", original.ID, raw); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	requested, err := service.CancelWithdrawal(ctx, 1, original.ID)
	if err != nil || !requested.CancellationRequested {
		t.Fatalf("cancellation request not recorded: %+v %v", requested, err)
	}
	custodyBalances(t, service, network, "3.9", "6")
	if err = executor.Execute(ctx, original.ID); err != nil {
		t.Fatal(err)
	}
	cancelled, err := service.GetWithdrawal(ctx, original.ID)
	if err != nil || cancelled.State != "cancelled" || cancelled.TxID != "" {
		t.Fatalf("unsigned request not safely cancelled: %+v %v", cancelled, err)
	}
	custodyBalances(t, service, network, "9.9", "0")
	var released int64
	var cancellationID, binding string
	if err = service.Pool.QueryRow(ctx, "SELECT nonce,cancellation_id FROM crypto_released_nonces WHERE network=$1 AND address=$2", network, hot).Scan(&released, &cancellationID); err != nil || released != 0 || cancellationID != original.ID {
		t.Fatalf("nonce not durably released: %d %s %v", released, cancellationID, err)
	}
	if err = service.Pool.QueryRow(ctx, "SELECT binding_sha256 FROM crypto_signer_cancellations WHERE withdrawal_id=$1", original.ID).Scan(&binding); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(raw)
	if binding != hex.EncodeToString(hash[:]) {
		t.Fatal("cancellation did not bind original prepared transaction")
	}
	if _, err = signer.Sign(ctx, prepared); !errors.Is(err, cryptosigner.ErrCancelled) {
		t.Fatalf("cancelled original was signed: %v", err)
	}
	replacement, err := service.RequestWithdrawal(ctx, cryptopay.WithdrawalInput{MerchantID: 1, RequestID: "new-request-after-proof", Network: network, Address: merchant, Amount: "6"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.ApproveWithdrawal(ctx, replacement.ID, 9); err != nil {
		t.Fatal(err)
	}
	custodyConfirm(t, service, &executor, state, replacement.ID, hot, merchant, "6000000")
	var reusedNonce int64
	var holes int
	if err = service.Pool.QueryRow(ctx, "SELECT nonce FROM crypto_withdrawals WHERE id=$1", replacement.ID).Scan(&reusedNonce); err != nil || reusedNonce != 0 {
		t.Fatalf("did not reuse safely released nonce 0: %d %v", reusedNonce, err)
	}
	if err = service.Pool.QueryRow(ctx, "SELECT count(*) FROM crypto_released_nonces WHERE network=$1 AND address=$2", network, hot).Scan(&holes); err != nil || holes != 0 {
		t.Fatalf("released nonce reused more than once: %d %v", holes, err)
	}
	if _, err = signer.Sign(ctx, prepared); !errors.Is(err, cryptosigner.ErrCancelled) {
		t.Fatalf("reusing nonce resurrected old request: %v", err)
	}
	custodyBalances(t, service, network, "3.9", "0")
	custodyReconcile(t, service)
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.broadcasts != 2 || state.nonces[hot] != 1 || state.balance(merchant) != "6000000" {
		t.Fatalf("wrong signed nonce recovery: broadcasts=%d nonce=%d merchant=%s", state.broadcasts, state.nonces[hot], state.balance(merchant))
	}
}

func custodyService(t *testing.T, network, hot string) *cryptopay.Service {
	t.Helper()
	dsn := os.Getenv("GOEPAY_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("GOEPAY_TEST_DATABASE_URL required for isolated custody integration")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	random := make([]byte, 8)
	if _, err = rand.Read(random); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	schema := "custody_" + hex.EncodeToString(random)
	app := "goepay-" + schema
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	var pool *pgxpool.Pool
	t.Cleanup(func() {
		if pool != nil {
			pool.Close()
		}
		var active int
		err := admin.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE application_name=$1", app).Scan(&active)
		if err != nil || active != 0 {
			t.Errorf("retain own schema %s: active=%d err=%v", schema, active, err)
			admin.Close()
			return
		}
		if _, err = admin.Exec(ctx, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	cfg.ConnConfig.RuntimeParams["application_name"] = app
	pool, err = pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `CREATE TABLE merchants(id bigint PRIMARY KEY,enabled boolean NOT NULL); INSERT INTO merchants VALUES(1,true); CREATE TABLE accounts(owner_id bigint,kind text,balance bigint); INSERT INTO accounts VALUES(1,'available',12345)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"000199_crypto_custody.up.sql", "000200_crypto_payment_channels.up.sql"} {
		migration, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err = pool.Exec(ctx, string(migration)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	return &cryptopay.Service{Pool: pool, TokenKey: make([]byte, 32), Networks: map[string]cryptopay.NetworkConfig{network: {Enabled: true, FeeBPS: 100, HotAddress: hot, TreasuryAddress: hot}}}
}

func custodySigner(t *testing.T, chain cryptochain.Chain, rpc, deposit, hot, payer, merchant string) *cryptosigner.Service {
	t.Helper()
	dir := t.TempDir()
	paths := map[string]string{}
	for i, id := range []string{"deposit", "hot"} {
		p := filepath.Join(dir, id+".key")
		if err := os.WriteFile(p, []byte(fmt.Sprintf("%064x", i+1)), 0600); err != nil {
			t.Fatal(err)
		}
		paths[id] = p
	}
	keys, err := cryptosigner.NewFileKeys(paths)
	if err != nil {
		t.Fatal(err)
	}
	contract := cryptochain.EthereumUSDTContract
	chainID := "1"
	if chain == cryptochain.Tron {
		contract = cryptochain.TronUSDTContract
		chainID = "728126428"
	}
	rule := func(from, key string, to []string) cryptosigner.Rule {
		return cryptosigner.Rule{Chain: chain, Network: "mainnet", ChainID: chainID, Contract: contract, From: from, KeyID: key, Destinations: to, MaxAmount: "10000000", DailyLimit: "20000000", MaxDailyTransactions: 100, MaxDailyFeeAtomic: "1000000000000000000", MaxGasLimit: 100000, MaxGasPrice: "50000000000", MaxNonce: 100, MaxFeeLimit: 100000000, MaxTTLSeconds: 300, TRONRPCURL: rpc}
	}
	s, err := cryptosigner.NewService(cryptosigner.Config{StatePath: filepath.Join(dir, "journal.jsonl"), Rules: []cryptosigner.Rule{rule(deposit, "deposit", []string{hot}), rule(hot, "hot", []string{payer, merchant})}}, keys)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func custodyConfirm(t *testing.T, s *cryptopay.Service, x *cryptopay.Executor, node *custodyNode, id, from, to, amount string) {
	t.Helper()
	if err := x.Execute(t.Context(), id); err != nil {
		t.Fatalf("execute %s: %v", id, err)
	}
	v, err := s.GetWithdrawal(t.Context(), id)
	if err != nil || v.TxID == "" || v.State != "broadcasting" {
		t.Fatalf("signed/broadcast boundary: %+v %v", v, err)
	}
	var pb, sb []byte
	if err = s.Pool.QueryRow(t.Context(), "SELECT prepared,signed FROM crypto_withdrawals WHERE id=$1", id).Scan(&pb, &sb); err != nil {
		t.Fatal(err)
	}
	var p cryptochain.PreparedTransaction
	var signed cryptochain.SignedTransaction
	if err = json.Unmarshal(pb, &p); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(sb, &signed); err != nil {
		t.Fatal(err)
	}
	if err = cryptosigner.VerifySigned(p, signed); err != nil {
		t.Fatal(err)
	}
	if p.From != from || p.To != to || p.AtomicAmount != amount || p.RequestID != id {
		t.Fatalf("prepared payment changed: %+v", p)
	}
	node.mu.Lock()
	actual, ok := node.transactions[v.TxID]
	node.mu.Unlock()
	if !ok || actual.from != from || actual.to != to || actual.amount != amount {
		t.Fatalf("actual signed chain transfer changed: %+v", actual)
	}
	for i := 0; i < 2; i++ {
		if err = x.Execute(t.Context(), id); err != nil {
			t.Fatalf("confirm/replay %s: %v", id, err)
		}
	}
	v, err = s.GetWithdrawal(t.Context(), id)
	if err != nil || v.State != "confirmed" {
		t.Fatalf("unconfirmed withdrawal: %+v %v", v, err)
	}
	custodyReconcile(t, s)
}

func custodyBalances(t *testing.T, s *cryptopay.Service, network, available, reserved string) {
	t.Helper()
	balances, err := s.Balances(t.Context(), 1)
	if err != nil || len(balances) != 1 || balances[0].Network != network || balances[0].Available != available || balances[0].Reserved != reserved {
		t.Fatalf("merchant crypto balances: %+v err=%v want=%s/%s", balances, err, available, reserved)
	}
}
func custodyReconcile(t *testing.T, s *cryptopay.Service) {
	t.Helper()
	r, err := s.Reconcile(t.Context())
	if err != nil || r.ImbalancedJournals != 0 || r.BalanceMismatches != 0 || r.NegativeBalances != 0 {
		t.Fatalf("crypto ledger reconciliation: %+v %v", r, err)
	}
}
func custodyAdapter(t *testing.T, c cryptochain.Chain, url string, broadcast bool) cryptochain.Adapter {
	t.Helper()
	cfg := cryptochain.Config{Endpoint: url, AllowBroadcast: broadcast}
	if c == cryptochain.Tron {
		a, err := cryptochain.NewTron(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	a, err := cryptochain.NewEthereum(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func custodyAddress(t *testing.T, c cryptochain.Chain, n byte) string {
	t.Helper()
	raw := make([]byte, 32)
	raw[31] = n
	key := secp256k1.PrivKeyFromBytes(raw)
	digest := custodyKeccak(key.PubKey().SerializeUncompressed()[1:])
	address := "0x" + hex.EncodeToString(digest[12:])
	if c == cryptochain.Tron {
		address = "41" + hex.EncodeToString(digest[12:])
	}
	canonical, err := cryptochain.NormalizeAddress(c, address)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}
func custodyKeccak(b []byte) []byte { h := sha3.NewLegacyKeccak256(); h.Write(b); return h.Sum(nil) }

type custodyTransfer struct {
	id, from, to, amount string
	height               uint64
	timestamp            time.Time
}
type custodyNode struct {
	t            *testing.T
	chain        cryptochain.Chain
	mu           sync.Mutex
	head         uint64
	transactions map[string]custodyTransfer
	balances     map[string]*big.Int
	nonces       map[string]uint64
	broadcasts   int
}

func newCustodyNode(t *testing.T, c cryptochain.Chain) *custodyNode {
	return &custodyNode{t: t, chain: c, transactions: map[string]custodyTransfer{}, balances: map[string]*big.Int{}, nonces: map[string]uint64{}}
}
func (n *custodyNode) deposit(from, to, amount string) {
	digest := sha256.Sum256([]byte("synthetic local deposit/" + string(n.chain)))
	id := hex.EncodeToString(digest[:])
	if n.chain == cryptochain.Ethereum {
		id = "0x" + id
	}
	n.head = 1
	n.transactions[id] = custodyTransfer{id, from, to, amount, 1, time.Now().Add(2 * time.Second).UTC()}
	v, _ := new(big.Int).SetString(amount, 10)
	n.balances[to] = v
}
func (n *custodyNode) balance(address string) string {
	if b := n.balances[address]; b != nil {
		return b.String()
	}
	return "0"
}
func (n *custodyNode) blockID(height uint64) string {
	if height == 0 {
		if n.chain == cryptochain.Tron {
			return cryptochain.TronMainnetGenesis
		}
		return cryptochain.EthereumMainnetGenesis
	}
	digest := sha256.Sum256([]byte("synthetic local block/" + string(n.chain) + "/" + strconv.FormatUint(height, 10)))
	if n.chain == cryptochain.Tron {
		binary.BigEndian.PutUint64(digest[:8], height)
		return hex.EncodeToString(digest[:])
	}
	return "0x" + hex.EncodeToString(digest[:])
}
func (n *custodyNode) addressHex(address string) string {
	if n.chain == cryptochain.Ethereum {
		return address[2:]
	}
	const alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"
	b := new(big.Int)
	for _, c := range address {
		b.Mul(b, big.NewInt(58))
		b.Add(b, big.NewInt(int64(strings.IndexRune(alphabet, c))))
	}
	return hex.EncodeToString(b.Bytes()[1:21])
}
func (n *custodyNode) event(tx custodyTransfer) map[string]any {
	topics := []string{"ddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", strings.Repeat("0", 24) + n.addressHex(tx.from), strings.Repeat("0", 24) + n.addressHex(tx.to)}
	amount, _ := new(big.Int).SetString(tx.amount, 10)
	data := fmt.Sprintf("%064x", amount)
	if n.chain == cryptochain.Tron {
		return map[string]any{"address": "a614f803b6fd780986a42c78ec9c7f77e6ded13c", "topics": topics, "data": data}
	}
	for i := range topics {
		topics[i] = "0x" + topics[i]
	}
	return map[string]any{"address": cryptochain.EthereumUSDTContract, "topics": topics, "data": "0x" + data, "blockNumber": fmt.Sprintf("0x%x", tx.height), "blockHash": n.blockID(tx.height), "transactionHash": tx.id, "logIndex": "0x0", "removed": false}
}

func (n *custodyNode) serve(w http.ResponseWriter, r *http.Request) {
	n.mu.Lock()
	defer n.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if n.chain == cryptochain.Ethereum {
		n.serveEthereum(w, r)
	} else {
		n.serveTRON(w, r)
	}
}
func (n *custodyNode) serveEthereum(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ID     uint64            `json:"id"`
		Method string            `json:"method"`
		Params []json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		n.t.Error(err)
		w.WriteHeader(400)
		return
	}
	var result any
	param := func(i int) string {
		var s string
		if i < len(request.Params) {
			json.Unmarshal(request.Params[i], &s)
		}
		return s
	}
	switch request.Method {
	case "eth_chainId":
		result = "0x1"
	case "eth_getBlockByNumber":
		height := n.head
		if tag := param(0); tag != "finalized" {
			height, _ = strconv.ParseUint(strings.TrimPrefix(tag, "0x"), 16, 64)
		}
		timestamp := time.Now().Add(2 * time.Second).Unix()
		for _, tx := range n.transactions {
			if tx.height == height {
				timestamp = tx.timestamp.Unix()
			}
		}
		result = map[string]any{"number": fmt.Sprintf("0x%x", height), "hash": n.blockID(height), "timestamp": fmt.Sprintf("0x%x", timestamp)}
	case "eth_getLogs":
		var logs []map[string]any
		var filter struct {
			From string `json:"fromBlock"`
			To   string `json:"toBlock"`
		}
		json.Unmarshal(request.Params[0], &filter)
		from, _ := strconv.ParseUint(strings.TrimPrefix(filter.From, "0x"), 16, 64)
		to, _ := strconv.ParseUint(strings.TrimPrefix(filter.To, "0x"), 16, 64)
		for _, tx := range n.transactions {
			if tx.height >= from && tx.height <= to {
				logs = append(logs, n.event(tx))
			}
		}
		result = logs
	case "eth_getTransactionReceipt":
		if tx, ok := n.transactions[param(0)]; ok {
			result = map[string]any{"transactionHash": tx.id, "blockNumber": fmt.Sprintf("0x%x", tx.height), "blockHash": n.blockID(tx.height), "status": "0x1", "gasUsed": "0xea60", "effectiveGasPrice": "0x4a817c800", "logs": []any{n.event(tx)}}
		}
	case "eth_getTransactionCount":
		result = fmt.Sprintf("0x%x", n.nonces[param(0)])
	case "eth_gasPrice":
		result = "0x4a817c800"
	case "eth_estimateGas":
		result = "0xea60"
	case "eth_call":
		var call struct {
			Data string `json:"data"`
		}
		json.Unmarshal(request.Params[0], &call)
		if len(call.Data) != 74 {
			n.t.Errorf("unexpected balance calldata: %s", call.Data)
			return
		}
		address := "0x" + call.Data[len(call.Data)-40:]
		value, _ := new(big.Int).SetString(n.balance(address), 10)
		result = fmt.Sprintf("0x%064x", value)
	case "eth_sendRawTransaction":
		tx, nonce, err := n.decodeEthereum(param(0))
		if err != nil {
			n.t.Error(err)
			w.WriteHeader(400)
			return
		}
		n.accept(tx)
		n.nonces[tx.from] = nonce + 1
		result = tx.id
	default:
		n.t.Errorf("unexpected Ethereum RPC %s", request.Method)
		w.WriteHeader(400)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": result})
}

func (n *custodyNode) serveTRON(w http.ResponseWriter, r *http.Request) {
	var input map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		n.t.Error(err)
		w.WriteHeader(400)
		return
	}
	var result any
	str := func(name string) string { var s string; json.Unmarshal(input[name], &s); return s }
	switch r.URL.Path {
	case "/wallet/getblockbynum", "/walletsolidity/getblockbynum", "/wallet/getnowblock", "/walletsolidity/getnowblock":
		height := n.head
		if strings.HasSuffix(r.URL.Path, "getblockbynum") {
			json.Unmarshal(input["num"], &height)
		}
		var transactions []map[string]any
		timestamp := time.Now().Add(2 * time.Second).UnixMilli()
		for _, tx := range n.transactions {
			if tx.height == height {
				timestamp = tx.timestamp.UnixMilli()
				transactions = append(transactions, map[string]any{"txID": tx.id, "raw_data": map[string]any{"contract": []any{map[string]any{"type": "TriggerSmartContract"}}}})
			}
		}
		result = map[string]any{"blockID": n.blockID(height), "block_header": map[string]any{"raw_data": map[string]any{"number": height, "timestamp": timestamp}}, "transactions": transactions}
	case "/walletsolidity/gettransactionbyid":
		if tx, ok := n.transactions[str("value")]; ok {
			result = map[string]any{"txID": tx.id, "ret": []any{map[string]any{"contractRet": "SUCCESS"}}, "raw_data": map[string]any{"contract": []any{map[string]any{"type": "TriggerSmartContract"}}}}
		} else {
			result = map[string]any{}
		}
	case "/walletsolidity/gettransactioninfobyid":
		if tx, ok := n.transactions[str("value")]; ok {
			result = map[string]any{"id": tx.id, "blockNumber": tx.height, "fee": 500000, "receipt": map[string]any{"result": "SUCCESS"}, "log": []any{n.event(tx)}}
		} else {
			result = map[string]any{}
		}
	case "/walletsolidity/triggerconstantcontract":
		owner, err := cryptochain.NormalizeAddress(cryptochain.Tron, str("owner_address"))
		if err != nil {
			n.t.Error(err)
		}
		value, _ := new(big.Int).SetString(n.balance(owner), 10)
		result = map[string]any{"result": map[string]bool{"result": true}, "constant_result": []string{fmt.Sprintf("%064x", value)}}
	case "/wallet/triggersmartcontract":
		var fee uint64
		json.Unmarshal(input["fee_limit"], &fee)
		result = map[string]any{"result": map[string]bool{"result": true}, "transaction": n.prepareTRON(str("owner_address"), str("parameter"), fee)}
	case "/wallet/broadcasttransaction":
		b, _ := json.Marshal(input)
		tx, err := n.decodeTRON(b)
		if err != nil {
			n.t.Error(err)
			w.WriteHeader(400)
			return
		}
		n.accept(tx)
		result = map[string]any{"result": true, "txid": tx.id}
	default:
		n.t.Errorf("unexpected TRON RPC %s", r.URL.Path)
		w.WriteHeader(400)
		return
	}
	json.NewEncoder(w).Encode(result)
}

func (n *custodyNode) accept(tx custodyTransfer) {
	if _, exists := n.transactions[tx.id]; exists {
		return
	}
	n.broadcasts++
	n.head++
	tx.height = n.head
	tx.timestamp = time.Now().Add(2 * time.Second).UTC()
	n.transactions[tx.id] = tx
	amount, _ := new(big.Int).SetString(tx.amount, 10)
	for _, address := range []string{tx.from, tx.to} {
		if n.balances[address] == nil {
			n.balances[address] = new(big.Int)
		}
	}
	n.balances[tx.from].Sub(n.balances[tx.from], amount)
	n.balances[tx.to].Add(n.balances[tx.to], amount)
	if n.balances[tx.from].Sign() < 0 {
		n.t.Errorf("synthetic token overspend by %s", tx.from)
	}
}

func (n *custodyNode) prepareTRON(owner, parameter string, fee uint64) map[string]any {
	now := uint64(time.Now().UnixMilli())
	expiration := now + 60000
	ref, _ := hex.DecodeString(n.blockID(n.head))
	ownerBytes, _ := hex.DecodeString(owner)
	contract, _ := hex.DecodeString("41a614f803b6fd780986a42c78ec9c7f77e6ded13c")
	data, _ := hex.DecodeString("a9059cbb" + parameter)
	trigger := custodyProtoBytes(1, ownerBytes)
	trigger = append(trigger, custodyProtoBytes(2, contract)...)
	trigger = append(trigger, custodyProtoBytes(4, data)...)
	anyValue := append(custodyProtoBytes(1, []byte("type.googleapis.com/protocol.TriggerSmartContract")), custodyProtoBytes(2, trigger)...)
	call := append(custodyProtoInt(1, 31), custodyProtoBytes(2, anyValue)...)
	raw := append(custodyProtoBytes(1, ref[6:8]), custodyProtoBytes(4, ref[8:16])...)
	raw = append(raw, custodyProtoInt(8, expiration)...)
	raw = append(raw, custodyProtoBytes(11, call)...)
	raw = append(raw, custodyProtoInt(14, now)...)
	raw = append(raw, custodyProtoInt(18, fee)...)
	digest := sha256.Sum256(raw)
	return map[string]any{"txID": hex.EncodeToString(digest[:]), "raw_data_hex": hex.EncodeToString(raw), "raw_data": map[string]any{"ref_block_bytes": hex.EncodeToString(ref[6:8]), "ref_block_hash": hex.EncodeToString(ref[8:16]), "timestamp": now, "expiration": expiration, "fee_limit": fee, "contract": []any{map[string]any{"type": "TriggerSmartContract", "parameter": map[string]any{"type_url": "type.googleapis.com/protocol.TriggerSmartContract", "value": map[string]any{"owner_address": owner, "contract_address": "41a614f803b6fd780986a42c78ec9c7f77e6ded13c", "data": hex.EncodeToString(data)}}}}}}
}
func (n *custodyNode) decodeTRON(b []byte) (custodyTransfer, error) {
	var signed struct {
		ID        string   `json:"txID"`
		Raw       string   `json:"raw_data_hex"`
		Signature []string `json:"signature"`
		Data      struct {
			Contracts []struct {
				Parameter struct {
					Value struct {
						Owner string `json:"owner_address"`
						Data  string `json:"data"`
					} `json:"value"`
				} `json:"parameter"`
			} `json:"contract"`
		} `json:"raw_data"`
	}
	if err := json.Unmarshal(b, &signed); err != nil {
		return custodyTransfer{}, err
	}
	if len(signed.Data.Contracts) != 1 || len(signed.Signature) != 1 {
		return custodyTransfer{}, fmt.Errorf("wrong signed TRON envelope")
	}
	raw, err := hex.DecodeString(signed.Raw)
	if err != nil {
		return custodyTransfer{}, err
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != signed.ID {
		return custodyTransfer{}, fmt.Errorf("TRON txid mismatch")
	}
	value := signed.Data.Contracts[0].Parameter.Value
	from, err := cryptochain.NormalizeAddress(cryptochain.Tron, value.Owner)
	if err != nil {
		return custodyTransfer{}, err
	}
	data, err := hex.DecodeString(value.Data)
	if err != nil || len(data) != 68 {
		return custodyTransfer{}, fmt.Errorf("wrong transfer data")
	}
	to, err := cryptochain.NormalizeAddress(cryptochain.Tron, "41"+hex.EncodeToString(data[16:36]))
	if err != nil {
		return custodyTransfer{}, err
	}
	return custodyTransfer{id: signed.ID, from: from, to: to, amount: new(big.Int).SetBytes(data[36:]).String()}, nil
}
func (n *custodyNode) decodeEthereum(rawHex string) (custodyTransfer, uint64, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(rawHex, "0x"))
	if err != nil {
		return custodyTransfer{}, 0, err
	}
	fields, err := custodyRLPFields(raw)
	if err != nil || len(fields) != 9 || len(fields[5]) != 68 || len(fields[6]) != 1 || len(fields[7]) > 32 || len(fields[8]) > 32 {
		return custodyTransfer{}, 0, fmt.Errorf("invalid signed Ethereum transfer")
	}
	unsigned := custodyRLPEncode(append(append([][]byte(nil), fields[:6]...), []byte{1}, nil, nil))
	digest := custodyKeccak(unsigned)
	sig := make([]byte, 65)
	sig[0] = 27 + fields[6][0] - 37
	copy(sig[33-len(fields[7]):33], fields[7])
	copy(sig[65-len(fields[8]):], fields[8])
	pub, _, err := ecdsa.RecoverCompact(sig, digest)
	if err != nil {
		return custodyTransfer{}, 0, err
	}
	from := "0x" + hex.EncodeToString(custodyKeccak(pub.SerializeUncompressed()[1:])[12:])
	data := fields[5]
	if !bytes.Equal(data[:4], []byte{0xa9, 0x05, 0x9c, 0xbb}) || hex.EncodeToString(fields[3]) != cryptochain.EthereumUSDTContract[2:] {
		return custodyTransfer{}, 0, fmt.Errorf("unexpected signed operation")
	}
	return custodyTransfer{id: "0x" + hex.EncodeToString(custodyKeccak(raw)), from: from, to: "0x" + hex.EncodeToString(data[16:36]), amount: new(big.Int).SetBytes(data[36:]).String()}, new(big.Int).SetBytes(fields[0]).Uint64(), nil
}

func custodyProtoBytes(field uint64, b []byte) []byte {
	return append(binary.AppendUvarint(binary.AppendUvarint(nil, field<<3|2), uint64(len(b))), b...)
}
func custodyProtoInt(field, n uint64) []byte {
	return binary.AppendUvarint(binary.AppendUvarint(nil, field<<3), n)
}
func custodyRLPEncode(fields [][]byte) []byte {
	var body []byte
	prefix := func(n int, short, long byte) []byte {
		if n < 56 {
			return []byte{short + byte(n)}
		}
		b := new(big.Int).SetInt64(int64(n)).Bytes()
		return append([]byte{long + byte(len(b))}, b...)
	}
	for _, field := range fields {
		if len(field) == 1 && field[0] < 128 {
			body = append(body, field...)
		} else {
			body = append(body, prefix(len(field), 128, 183)...)
			body = append(body, field...)
		}
	}
	return append(prefix(len(body), 192, 247), body...)
}
func custodyRLPFields(raw []byte) ([][]byte, error) {
	item := func(raw []byte) ([]byte, []byte, error) {
		if len(raw) == 0 {
			return nil, nil, fmt.Errorf("empty RLP")
		}
		p := raw[0]
		if p < 128 {
			return raw[:1], raw[1:], nil
		}
		base := byte(128)
		if p >= 192 {
			base = 192
		}
		offset, size := 1, int(p-base)
		if size > 55 {
			count := size - 55
			if len(raw) < 1+count {
				return nil, nil, fmt.Errorf("truncated RLP length")
			}
			size = int(new(big.Int).SetBytes(raw[1 : 1+count]).Int64())
			offset += count
		}
		if size < 0 || size > len(raw)-offset {
			return nil, nil, fmt.Errorf("truncated RLP field")
		}
		return raw[offset : offset+size], raw[offset+size:], nil
	}
	body, tail, err := item(raw)
	if err != nil || len(tail) != 0 {
		return nil, fmt.Errorf("bad RLP list")
	}
	var fields [][]byte
	for len(body) > 0 {
		field, rest, err := item(body)
		if err != nil {
			return nil, err
		}
		fields = append(fields, field)
		body = rest
	}
	return fields, nil
}
