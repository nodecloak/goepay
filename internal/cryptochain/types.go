// Package cryptochain implements pinned-mainnet USDT node protocols. It neither
// holds keys nor signs transactions; callers persist prepared/signed payloads
// before any broadcast and reconcile the original transaction after a timeout.
package cryptochain

import (
	"context"
	"errors"
	"net/http"
	"time"
)

type Chain string

const (
	Tron                   Chain = "tron"
	Ethereum               Chain = "ethereum"
	Mainnet                      = "mainnet"
	USDTDecimals                 = 6
	TronUSDTContract             = "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"
	EthereumUSDTContract         = "0xdac17f958d2ee523a2206206994597c13d831ec7"
	TronMainnetGenesis           = "00000000000000001ebf88508a03865c71d452e25f4d51194196a1d22b6653dc"
	EthereumMainnetGenesis       = "0xd4e56740f876aef8c010b86a40d5f56745a118d0906a34e69aec8c0db1cb8fa3"
)

var (
	ErrWrongNetwork      = errors.New("cryptochain: wrong network")
	ErrInvalidAddress    = errors.New("cryptochain: invalid address")
	ErrInvalidAmount     = errors.New("cryptochain: invalid atomic amount")
	ErrInvalidResponse   = errors.New("cryptochain: invalid node response")
	ErrUnfinalized       = errors.New("cryptochain: requested range is not finalized")
	ErrBroadcastDisabled = errors.New("cryptochain: broadcast disabled")
)

type Transfer struct {
	Chain        Chain     `json:"chain"`
	TxID         string    `json:"tx_id"`
	EventIndex   uint64    `json:"event_index"`
	BlockHeight  uint64    `json:"block_height"`
	BlockHash    string    `json:"block_hash"`
	From         string    `json:"from"`
	To           string    `json:"to"`
	Contract     string    `json:"contract"`
	AtomicAmount string    `json:"atomic_amount"`
	OccurredAt   time.Time `json:"occurred_at"`
}

// Nonce, when supplied, must be reserved durably by the caller. An absent nonce
// is queried from the pending node view, and still must be reserved before signing.
type TransferRequest struct {
	RequestID    string  `json:"request_id"`
	From         string  `json:"from"`
	To           string  `json:"to"`
	AtomicAmount string  `json:"atomic_amount"`
	FeeLimit     uint64  `json:"fee_limit"`
	Nonce        *uint64 `json:"nonce,omitempty"`
}

type PreparedTransaction struct {
	Chain           Chain  `json:"chain"`
	Network         string `json:"network"`
	ChainID         string `json:"chain_id"`
	RequestID       string `json:"request_id"`
	From            string `json:"from"`
	To              string `json:"to"`
	Contract        string `json:"contract"`
	AtomicAmount    string `json:"atomic_amount"`
	UnsignedPayload string `json:"unsigned_payload"`
	SigningPayload  string `json:"signing_payload"`
	TxID            string `json:"tx_id"`
	Nonce           uint64 `json:"nonce"`
	GasLimit        uint64 `json:"gas_limit"`
	GasPrice        string `json:"gas_price"`
	FeeLimit        uint64 `json:"fee_limit"`
	Expiration      uint64 `json:"expiration"`
}

type SignedTransaction struct {
	Chain          Chain  `json:"chain"`
	TxID           string `json:"tx_id"`
	RawTransaction string `json:"raw_transaction"`
}

type BroadcastResult struct {
	TxID     string `json:"tx_id"`
	Accepted bool   `json:"accepted"`
}

// Found=false or Finalized=false is an unresolved state, never proof that an
// original transaction can safely be replaced with another payment.
type TransactionStatus struct {
	Chain       Chain      `json:"chain"`
	TxID        string     `json:"tx_id"`
	BlockHash   string     `json:"block_hash"`
	BlockHeight uint64     `json:"block_height"`
	Found       bool       `json:"found"`
	Finalized   bool       `json:"finalized"`
	Success     bool       `json:"success"`
	FeeAtomic   string     `json:"fee_atomic"`
	Transfers   []Transfer `json:"transfers"`
}

type Adapter interface {
	Chain() Chain
	VerifyNetwork(context.Context) error
	FinalizedHeight(context.Context) (uint64, error)
	CanonicalBlockHash(context.Context, uint64) (string, error)
	Balance(context.Context, string) (string, error)
	ScanTransfers(context.Context, uint64, uint64) ([]Transfer, error)
	Prepare(context.Context, TransferRequest) (PreparedTransaction, error)
	Broadcast(context.Context, SignedTransaction) (BroadcastResult, error)
	Lookup(context.Context, string) (TransactionStatus, error)
}

// Config requires an explicit endpoint. Construction never makes a request.
// Broadcast is opt-in so read-only workers cannot accidentally submit a payment.
type Config struct {
	Endpoint       string
	Client         *http.Client
	Headers        http.Header
	AllowBroadcast bool
	MaxScanBlocks  uint64
}
