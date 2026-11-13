// Package cryptopay owns USDT liabilities independently of the CNY payment ledger.
package cryptopay

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"time"
)

var (
	ErrInvalid      = errors.New("invalid crypto request")
	ErrNotFound     = errors.New("crypto record not found")
	ErrConflict     = errors.New("crypto request conflict")
	ErrInsufficient = errors.New("insufficient crypto balance")
	ErrUnavailable  = errors.New("crypto service unavailable")
)

type NetworkConfig struct {
	Enabled         bool   `json:"enabled"`
	FeeBPS          int64  `json:"fee_bps"`
	TreasuryAddress string `json:"treasury_address"`
	HotAddress      string `json:"hot_address"`
}
type Service struct {
	Pool     *pgxpool.Pool
	Networks map[string]NetworkConfig
	TokenKey []byte
}
type CreateInvoiceInput struct {
	MerchantID                                        int64
	OutTradeNo, Network, Amount, NotifyURL, ReturnURL string
	CNYAmount                                         int64
	ExpiresIn                                         time.Duration
	Name, Param, Version                              string
}
type Invoice struct {
	ID            string    `json:"id"`
	MerchantID    int64     `json:"merchant_id,string"`
	OutTradeNo    string    `json:"out_trade_no"`
	Network       string    `json:"network"`
	Address       string    `json:"address"`
	Amount        string    `json:"amount"`
	Received      string    `json:"received"`
	Fee           string    `json:"fee"`
	State         string    `json:"state"`
	ExpiresAt     time.Time `json:"expires_at"`
	CheckoutToken string    `json:"-"`
	NotifyURL     string    `json:"notify_url"`
	ReturnURL     string    `json:"return_url"`
	CNYAmount     int64     `json:"cny_amount,string"`
	Name          string    `json:"name"`
	Param         string    `json:"param"`
	Version       string    `json:"version"`
}
type Balance struct {
	Network   string `json:"network"`
	Available string `json:"available"`
	Reserved  string `json:"reserved"`
}
type WithdrawalInput struct {
	MerchantID                          int64
	RequestID, Network, Address, Amount string
}
type RefundInput struct {
	MerchantID                            int64
	RequestID, InvoiceID, Address, Amount string
}
type Withdrawal struct {
	CancellationRequested bool   `json:"cancellation_requested"`
	ID                    string `json:"id"`
	MerchantID            int64  `json:"merchant_id,string"`
	Kind                  string `json:"kind"`
	RequestID             string `json:"request_id"`
	Network               string `json:"network"`
	Address               string `json:"address"`
	Amount                string `json:"amount"`
	State                 string `json:"state"`
	TxID                  string `json:"tx_id"`
	InvoiceID             string `json:"invoice_id"`
	ApprovedBy            int64  `json:"approved_by,string"`
	Source                string `json:"-"`
}

// Deposit is an internal trusted observation. Only a configured finalized-chain
// scanner may call RecordDeposit; no HTTP endpoint accepts deposit evidence.
type Deposit struct {
	Network, TxID, BlockHash, Address, Amount string
	EventIndex                                int
	BlockHeight                               int64
	OccurredAt                                time.Time
}
type ReconcileResult struct {
	ImbalancedJournals int64 `json:"imbalanced_journals"`
	BalanceMismatches  int64 `json:"balance_mismatches"`
	NegativeBalances   int64 `json:"negative_balances"`
}
type authorityKey struct{}

func WithAuthority(ctx context.Context, guard func(context.Context, pgx.Tx) error) context.Context {
	return context.WithValue(ctx, authorityKey{}, guard)
}
func authorize(ctx context.Context, tx pgx.Tx) error {
	if f, ok := ctx.Value(authorityKey{}).(func(context.Context, pgx.Tx) error); ok {
		return f(ctx, tx)
	}
	return nil
}
func (s *Service) begin(ctx context.Context) (pgx.Tx, error) {
	if s == nil || s.Pool == nil {
		return nil, ErrUnavailable
	}
	tx, e := s.Pool.Begin(ctx)
	if e == nil {
		e = authorize(ctx, tx)
		if e != nil {
			tx.Rollback(ctx)
		}
	}
	return tx, e
}
func commit(ctx context.Context, tx pgx.Tx) error {
	if e := authorize(ctx, tx); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
