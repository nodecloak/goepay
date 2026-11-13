// Package cryptosigner is an isolated, policy-constrained transaction signer.
// Only this process receives key handles. Its public protocol accepts a typed
// USDT transfer, never a caller-selected digest or arbitrary bytes to sign.
package cryptosigner

import (
	"bufio"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"goepay/internal/cryptochain"
)

var (
	ErrPolicy          = errors.New("cryptosigner: policy denied")
	ErrRequestConflict = errors.New("cryptosigner: request or nonce already bound")
	ErrIndeterminate   = errors.New("cryptosigner: original signing result is indeterminate; manual reconciliation required")
	ErrCancelled       = errors.New("cryptosigner: prepared request was durably cancelled before signing")
)

// KeyProvider is implemented by a local key store or an HSM adapter. It is not
// exposed by the HTTP service. SignDigest returns canonical r || s || recoveryID.
type KeyProvider interface {
	PublicKey(context.Context, string) ([]byte, error)
	SignDigest(context.Context, string, []byte) ([]byte, error)
}

type Rule struct {
	Chain                cryptochain.Chain `json:"chain"`
	Network              string            `json:"network"`
	ChainID              string            `json:"chain_id"`
	Contract             string            `json:"contract"`
	From                 string            `json:"from"`
	KeyID                string            `json:"key_id"`
	Destinations         []string          `json:"destinations"`
	MaxAmount            string            `json:"max_amount"`
	DailyLimit           string            `json:"daily_limit"`
	MaxDailyTransactions uint64            `json:"max_daily_transactions"`
	MaxDailyFeeAtomic    string            `json:"max_daily_fee_atomic"`
	MaxGasLimit          uint64            `json:"max_gas_limit"`
	MaxGasPrice          string            `json:"max_gas_price"`
	MinNonce             uint64            `json:"min_nonce"`
	MaxNonce             uint64            `json:"max_nonce"`
	MaxFeeLimit          uint64            `json:"max_fee_limit"`
	MaxTTLSeconds        uint64            `json:"max_ttl_seconds"`
	TRONRPCURL           string            `json:"tron_rpc_url"`
}

type Config struct {
	StatePath string           `json:"state_path"`
	Rules     []Rule           `json:"rules"`
	Now       func() time.Time `json:"-"`
}

type entry struct {
	RequestID   string                        `json:"request_id"`
	Binding     string                        `json:"binding"`
	Day         string                        `json:"day"`
	RuleID      string                        `json:"rule_id"`
	Amount      string                        `json:"amount"`
	FeeBudget   string                        `json:"fee_budget"`
	NonceKey    string                        `json:"nonce_key"`
	State       string                        `json:"state"`
	Transaction cryptochain.SignedTransaction `json:"transaction"`
}

type Service struct {
	mu       sync.Mutex
	cfg      Config
	keys     KeyProvider
	file     *os.File
	lockPath string
	entries  map[string]entry
	closed   bool
	poisoned error
}

func NewService(cfg Config, keys KeyProvider) (*Service, error) {
	if keys == nil || cfg.StatePath == "" || len(cfg.Rules) == 0 {
		return nil, fmt.Errorf("%w: keys, state path and rules required", ErrPolicy)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	seen := map[string]bool{}
	for _, r := range cfg.Rules {
		if err := validateRule(r); err != nil {
			return nil, err
		}
		id := ruleID(r)
		if seen[id] {
			return nil, fmt.Errorf("%w: duplicate rule", ErrPolicy)
		}
		seen[id] = true
	}
	if err := os.MkdirAll(filepath.Dir(cfg.StatePath), 0700); err != nil {
		return nil, err
	}
	lockPath := cfg.StatePath + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("signer state lock (a crashed owner requires operator reconciliation): %w", err)
	}
	fmt.Fprintf(lock, "pid=%d\n", os.Getpid())
	lock.Close()
	ok := false
	defer func() {
		if !ok {
			os.Remove(lockPath)
		}
	}()
	f, err := os.OpenFile(cfg.StatePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}
	s := &Service{cfg: cfg, keys: keys, file: f, lockPath: lockPath, entries: map[string]entry{}}
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 4096), 2<<20)
	for scan.Scan() {
		var e entry
		if err = json.Unmarshal(scan.Bytes(), &e); err != nil {
			f.Close()
			return nil, fmt.Errorf("invalid signer journal: %w", err)
		}
		cancelled := e.State == "cancelled"
		if e.RequestID == "" || len(e.Binding) != 64 || (!cancelled && (e.Day == "" || e.RuleID == "" || !validPositive(e.Amount) || !validPositive(e.FeeBudget) || e.NonceKey == "" || (e.State != "pending" && e.State != "signed"))) || (cancelled && (e.Day != "" || e.RuleID != "" || e.Amount != "" || e.FeeBudget != "" || e.NonceKey != "" || e.Transaction != (cryptochain.SignedTransaction{}))) {
			f.Close()
			return nil, fmt.Errorf("invalid signer journal record")
		}
		if prior, exists := s.entries[e.RequestID]; exists && (prior.Binding != e.Binding || prior.Day != e.Day || prior.RuleID != e.RuleID || prior.Amount != e.Amount || prior.FeeBudget != e.FeeBudget || prior.NonceKey != e.NonceKey || prior.State != "pending" || e.State != "signed") {
			f.Close()
			return nil, fmt.Errorf("conflicting signer journal")
		}
		if e.State == "signed" && (e.Transaction.TxID == "" || e.Transaction.RawTransaction == "") {
			f.Close()
			return nil, fmt.Errorf("empty signer result in journal")
		}
		s.entries[e.RequestID] = e
	}
	if err = scan.Err(); err != nil {
		f.Close()
		return nil, err
	}
	// A complete record always ends in a newline; a torn write is fail-closed.
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.Size() > 0 {
		b := make([]byte, 1)
		if _, err = f.ReadAt(b, info.Size()-1); err != nil || b[0] != '\n' {
			f.Close()
			return nil, fmt.Errorf("truncated signer journal")
		}
	}
	ok = true
	return s, nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	err := s.file.Close()
	if e := os.Remove(s.lockPath); err == nil {
		err = e
	}
	return err
}

func (s *Service) Sign(ctx context.Context, p cryptochain.PreparedTransaction) (cryptochain.SignedTransaction, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var zero cryptochain.SignedTransaction
	if s.closed {
		return zero, errors.New("signer closed")
	}
	if s.poisoned != nil {
		return zero, fmt.Errorf("%w: signer journal unavailable", ErrIndeterminate)
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	binding, err := preparedBinding(p)
	if err != nil {
		return zero, err
	}
	if e, ok := s.entries[p.RequestID]; ok {
		if e.Binding != binding {
			return zero, ErrRequestConflict
		}
		if e.State == "cancelled" {
			return zero, ErrCancelled
		}
		if e.State != "signed" {
			return zero, ErrIndeterminate
		}
		return e.Transaction, nil
	}
	r, err := s.rule(p)
	if err != nil {
		return zero, err
	}
	validated, err := validateTransaction(ctx, p, r, s.cfg.Now())
	if err != nil {
		return zero, fmt.Errorf("%w: %v", ErrPolicy, err)
	}
	wallet := walletID(p.Chain, p.Network, p.From)
	nonceKey := wallet + "/" + validated.reservation
	day := s.cfg.Now().UTC().Format("2006-01-02")
	total := new(big.Int)
	nativeTotal := new(big.Int)
	var transactions uint64
	feeBudget := new(big.Int).SetUint64(p.FeeLimit)
	if p.Chain == cryptochain.Ethereum {
		feeBudget.Mul(mustBig(p.GasPrice), new(big.Int).SetUint64(p.GasLimit))
	}
	id := ruleID(r)
	for _, e := range s.entries {
		if e.State == "cancelled" {
			continue
		}
		if e.NonceKey == nonceKey {
			return zero, ErrRequestConflict
		}
		if e.Day == day && e.RuleID == id {
			total.Add(total, mustBig(e.Amount))
		}
		if e.Day == day && strings.HasPrefix(e.NonceKey, wallet+"/") {
			transactions++
			nativeTotal.Add(nativeTotal, mustBig(e.FeeBudget))
		}
	}
	total.Add(total, mustBig(p.AtomicAmount))
	if total.Cmp(mustBig(r.DailyLimit)) > 0 {
		return zero, fmt.Errorf("%w: daily limit exceeded", ErrPolicy)
	}
	nativeTotal.Add(nativeTotal, feeBudget)
	if transactions >= r.MaxDailyTransactions || nativeTotal.Cmp(mustBig(r.MaxDailyFeeAtomic)) > 0 {
		return zero, fmt.Errorf("%w: daily native fee or transaction budget exceeded", ErrPolicy)
	}
	e := entry{RequestID: p.RequestID, Binding: binding, Day: day, RuleID: id, Amount: p.AtomicAmount, FeeBudget: feeBudget.String(), NonceKey: nonceKey, State: "pending"}
	// Persist before invoking an HSM. Any failure after this point reserves the
	// original payment forever until an operator reconciles it.
	if err = s.append(e); err != nil {
		return zero, fmt.Errorf("%w: persist reservation: %v", ErrIndeterminate, err)
	}
	s.entries[e.RequestID] = e
	pub, err := s.keys.PublicKey(ctx, r.KeyID)
	if err != nil {
		return zero, fmt.Errorf("%w: public key lookup: %v", ErrIndeterminate, err)
	}
	if !publicKeyMatches(pub, p.Chain, p.From) {
		return zero, fmt.Errorf("%w: source does not match key", ErrIndeterminate)
	}
	sig, err := s.keys.SignDigest(ctx, r.KeyID, validated.digest)
	if err != nil {
		return zero, fmt.Errorf("%w: %v", ErrIndeterminate, err)
	}
	if err = verifySignature(pub, validated.digest, sig); err != nil {
		return zero, fmt.Errorf("%w: %v", ErrIndeterminate, err)
	}
	result, err := validated.finish(sig)
	if err != nil {
		return zero, fmt.Errorf("%w: %v", ErrIndeterminate, err)
	}
	e.State = "signed"
	e.Transaction = result
	if err = s.append(e); err != nil {
		return zero, fmt.Errorf("%w: persist signature: %v", ErrIndeterminate, err)
	}
	s.entries[e.RequestID] = e
	return result, nil
}

func (s *Service) append(e entry) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	n, err := s.file.Write(b)
	if err == nil && n != len(b) {
		err = errors.New("short signer journal write")
	}
	if err == nil {
		err = s.file.Sync()
	}
	if err != nil {
		s.poisoned = err
	}
	return err
}

func (s *Service) rule(p cryptochain.PreparedTransaction) (Rule, error) {
	for _, r := range s.cfg.Rules {
		if r.Chain != p.Chain || r.Network != p.Network || r.ChainID != p.ChainID || !sameAddress(p.Chain, r.Contract, p.Contract) || !sameAddress(p.Chain, r.From, p.From) {
			continue
		}
		allowed := false
		for _, d := range r.Destinations {
			if sameAddress(p.Chain, d, p.To) {
				allowed = true
			}
		}
		if !allowed || !validPositive(p.AtomicAmount) || mustBig(p.AtomicAmount).Cmp(mustBig(r.MaxAmount)) > 0 {
			return Rule{}, fmt.Errorf("%w: destination or amount denied", ErrPolicy)
		}
		return r, nil
	}
	return Rule{}, fmt.Errorf("%w: network, token or source denied", ErrPolicy)
}

func validateRule(r Rule) error {
	if r.Network != cryptochain.Mainnet || r.KeyID == "" || len(r.Destinations) == 0 || !validPositive(r.MaxAmount) || !validPositive(r.DailyLimit) || r.MaxDailyTransactions == 0 || !validPositive(r.MaxDailyFeeAtomic) {
		return fmt.Errorf("%w: invalid rule", ErrPolicy)
	}
	if r.Chain == cryptochain.Ethereum {
		if r.ChainID != "1" || !sameAddress(r.Chain, r.Contract, cryptochain.EthereumUSDTContract) || r.MaxGasLimit == 0 || !validPositive(r.MaxGasPrice) || r.MaxNonce < r.MinNonce {
			return fmt.Errorf("%w: invalid Ethereum rule", ErrPolicy)
		}
	} else if r.Chain == cryptochain.Tron {
		if r.ChainID != "728126428" {
			return fmt.Errorf("%w: invalid TRON chain ID", ErrPolicy)
		}
		if r.Contract != cryptochain.TronUSDTContract || r.MaxFeeLimit == 0 || r.MaxTTLSeconds == 0 || r.MaxTTLSeconds > 86400 || r.TRONRPCURL == "" {
			return fmt.Errorf("%w: invalid TRON rule", ErrPolicy)
		}
	} else {
		return fmt.Errorf("%w: unsupported chain", ErrPolicy)
	}
	if _, err := addressBytes(r.Chain, r.From); err != nil {
		return err
	}
	for _, d := range r.Destinations {
		if _, err := addressBytes(r.Chain, d); err != nil {
			return err
		}
	}
	return nil
}

func ruleID(r Rule) string {
	contract, _ := addressBytes(r.Chain, r.Contract)
	return walletID(r.Chain, r.Network, r.From) + "/" + hex.EncodeToString(contract)
}
func walletID(chain cryptochain.Chain, network, from string) string {
	source, _ := addressBytes(chain, from)
	return string(chain) + "/" + network + "/" + hex.EncodeToString(source)
}
func validPositive(s string) bool {
	n, ok := new(big.Int).SetString(s, 10)
	return ok && n.Sign() > 0 && n.BitLen() <= 256 && n.String() == s
}
func mustBig(s string) *big.Int { n, _ := new(big.Int).SetString(s, 10); return n }
func sameAddress(chain cryptochain.Chain, a, b string) bool {
	x, e := addressBytes(chain, a)
	y, f := addressBytes(chain, b)
	return e == nil && f == nil && string(x) == string(y)
}
