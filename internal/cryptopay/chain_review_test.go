package cryptopay

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"goepay/internal/cryptochain"
	"goepay/internal/cryptosigner"
)

// This protocol fixture has a fixed future timestamp for repeatability.
// VerifySigned checks contents/signature, independently of live RPC/time policy.
// Production Sign applies current expiration, TAPOS, key, and spending policy.
func reviewPreparedTransfer(r cryptochain.TransferRequest) (cryptochain.PreparedTransaction, error) {
	call, err := cryptochain.TransferCallData(cryptochain.Tron, r.To, r.AtomicAmount)
	if err != nil {
		return cryptochain.PreparedTransaction{}, err
	}
	data, _ := hex.DecodeString(call)
	owner, _ := hex.DecodeString("417e5f4552091a69125d5dfcb7b8c2659029395bdf")
	token, _ := hex.DecodeString("41a614f803b6fd780986a42c78ec9c7f77e6ded13c")
	fee := r.FeeLimit
	if fee == 0 {
		fee = 100000000
	}
	const timestamp = 4102444800000
	const expiration = 4102444860000
	trigger := reviewProtoBytes(nil, 1, owner)
	trigger = reviewProtoBytes(trigger, 2, token)
	trigger = reviewProtoBytes(trigger, 4, data)
	parameter := reviewProtoBytes(nil, 1, []byte("type.googleapis.com/protocol.TriggerSmartContract"))
	parameter = reviewProtoBytes(parameter, 2, trigger)
	contract := reviewProtoUint(nil, 1, 31)
	contract = reviewProtoBytes(contract, 2, parameter)
	raw := reviewProtoBytes(nil, 1, []byte{0, 1})
	raw = reviewProtoBytes(raw, 4, []byte{0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa, 0xaa})
	raw = reviewProtoUint(raw, 8, expiration)
	raw = reviewProtoBytes(raw, 11, contract)
	raw = reviewProtoUint(raw, 14, timestamp)
	raw = reviewProtoUint(raw, 18, fee)
	digest := sha256.Sum256(raw)
	id := hex.EncodeToString(digest[:])
	transaction := map[string]any{"visible": false, "txID": id, "raw_data_hex": hex.EncodeToString(raw), "raw_data": map[string]any{"ref_block_bytes": "0001", "ref_block_hash": "aaaaaaaaaaaaaaaa", "timestamp": timestamp, "expiration": expiration, "fee_limit": fee, "contract": []any{map[string]any{"type": "TriggerSmartContract", "parameter": map[string]any{"type_url": "type.googleapis.com/protocol.TriggerSmartContract", "value": map[string]any{"owner_address": hex.EncodeToString(owner), "contract_address": hex.EncodeToString(token), "data": call}}}}}}
	payload, err := json.Marshal(transaction)
	if err != nil {
		return cryptochain.PreparedTransaction{}, err
	}
	return cryptochain.PreparedTransaction{Chain: cryptochain.Tron, Network: cryptochain.Mainnet, ChainID: "728126428", RequestID: r.RequestID, From: r.From, To: r.To, Contract: cryptochain.TronUSDTContract, AtomicAmount: r.AtomicAmount, UnsignedPayload: string(payload), SigningPayload: hex.EncodeToString(raw), TxID: id, FeeLimit: fee, Expiration: expiration}, nil
}

func reviewProtoBytes(out []byte, field uint64, value []byte) []byte {
	out = binary.AppendUvarint(out, field<<3|2)
	out = binary.AppendUvarint(out, uint64(len(value)))
	return append(out, value...)
}
func reviewProtoUint(out []byte, field, value uint64) []byte {
	out = binary.AppendUvarint(out, field<<3)
	return binary.AppendUvarint(out, value)
}

func reviewSignPrepared(p cryptochain.PreparedTransaction) (cryptochain.SignedTransaction, error) {
	if p.Chain != cryptochain.Tron {
		return cryptochain.SignedTransaction{}, fmt.Errorf("test signer supports TRON only")
	}
	raw, err := hex.DecodeString(p.SigningPayload)
	if err != nil {
		return cryptochain.SignedTransaction{}, err
	}
	digest := sha256.Sum256(raw)
	key := make([]byte, 32)
	key[31] = 1
	compact := ecdsa.SignCompact(secp256k1.PrivKeyFromBytes(key), digest[:], false)
	sig := append(append([]byte{}, compact[1:]...), compact[0]-27)
	var transaction map[string]json.RawMessage
	if err := json.Unmarshal([]byte(p.UnsignedPayload), &transaction); err != nil {
		return cryptochain.SignedTransaction{}, err
	}
	transaction["signature"], _ = json.Marshal([]string{hex.EncodeToString(sig)})
	payload, err := json.Marshal(transaction)
	return cryptochain.SignedTransaction{Chain: p.Chain, TxID: hex.EncodeToString(digest[:]), RawTransaction: string(payload)}, err
}

var reviewFixtureTxID = func() string {
	p, err := reviewPreparedTransfer(cryptochain.TransferRequest{RequestID: "fixture", From: depositOne, To: depositTwo, AtomicAmount: "6000000", FeeLimit: 100000000})
	if err != nil {
		panic(err)
	}
	return p.TxID
}()

func TestChainReviewFixturePassesIndependentSignedTransactionVerification(t *testing.T) {
	p, err := reviewPreparedTransfer(cryptochain.TransferRequest{RequestID: "fixture", From: depositOne, To: depositTwo, AtomicAmount: "6000000", FeeLimit: 100000000})
	if err != nil {
		t.Fatal(err)
	}
	signed, err := reviewSignPrepared(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = cryptosigner.VerifySigned(p, signed); err != nil {
		t.Fatal(err)
	}
}

func TestChainReviewCommittedJournalRejectsLaterUnbalancedEntry(t *testing.T) {
	s, w := fundedWithdrawal(t)
	var journal, account int64
	if err := s.Pool.QueryRow(t.Context(), "SELECT id FROM crypto_journals WHERE reference=$1", "reserve:"+w.ID).Scan(&journal); err != nil {
		t.Fatal(err)
	}
	if err := s.Pool.QueryRow(t.Context(), "SELECT id FROM crypto_accounts WHERE network='tron' AND owner_id=1 AND kind='available'").Scan(&account); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(t.Context(), "INSERT INTO crypto_entries(journal_id,account_id,amount) VALUES($1,$2,1000000)", journal, account); err == nil {
		balances, _ := s.Balances(t.Context(), 1)
		t.Fatalf("committed balanced journal accepted an unbalanced later entry and minted spendable USDT: %+v", balances)
	}
	report, err := s.Reconcile(t.Context())
	if err != nil || report.ImbalancedJournals != 0 || report.BalanceMismatches != 0 {
		t.Fatalf("rejected append damaged ledger: %+v %v", report, err)
	}
}

func TestChainReviewAccountBalanceCannotBeUpdatedOutsideLedger(t *testing.T) {
	s, _ := fundedWithdrawal(t)
	if _, err := s.Pool.Exec(t.Context(), "UPDATE crypto_accounts SET balance=balance+1000000 WHERE network='tron' AND owner_id=1 AND kind='available'"); err == nil {
		balances, _ := s.Balances(t.Context(), 1)
		t.Fatalf("direct account update minted spendable USDT outside any journal: %+v", balances)
	}
	report, err := s.Reconcile(t.Context())
	if err != nil || report.ImbalancedJournals != 0 || report.BalanceMismatches != 0 {
		t.Fatalf("rejected update damaged ledger: %+v %v", report, err)
	}
}

func TestChainReviewHotWalletConfigurationStoresCanonicalSource(t *testing.T) {
	s, _ := fundedWithdrawal(t)
	cfg := s.Networks["tron"]
	cfg.HotAddress = "417e5f4552091a69125d5dfcb7b8c2659029395bdf"
	s.Networks["tron"] = cfg
	w, err := s.RequestWithdrawal(t.Context(), WithdrawalInput{MerchantID: 1, RequestID: "hex-hot-wallet", Network: "tron", Address: depositTwo, Amount: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if w.Source != depositOne {
		t.Fatalf("valid alternate hot address encoding persisted as %q; adapter preparation returns %q and validatePrepared rejects the outgoing", w.Source, depositOne)
	}
}
