// Package cryptopay owns USDT liabilities independently of the CNY payment ledger.
package cryptopay

import (
	"context"
	"encoding/json"
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
	ErrQuoteExpired = errors.New("USDT quote expired")
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
	// Website collection must use channel admission. Low-level chain fixtures
	// and historical custody tools do not create a channel implicitly.
	RequireChannels bool
}
type CreateInvoiceInput struct {
	MerchantID                                        int64
	ChannelID                                         int64
	OutTradeNo, Network, Amount, NotifyURL, ReturnURL string
	CNYAmount                                         int64
	ExpiresIn                                         time.Duration
	Name, Param, Version                              string
	QuoteRate                                         string
	QuoteValidUntil                                   time.Time
}
type Invoice struct {
	ID              string          `json:"id"`
	MerchantID      int64           `json:"merchant_id,string"`
	OutTradeNo      string          `json:"out_trade_no"`
	Network         string          `json:"network"`
	Address         string          `json:"address"`
	Amount          string          `json:"amount"`
	Received        string          `json:"received"`
	Fee             string          `json:"fee"`
	State           string          `json:"state"`
	ExpiresAt       time.Time       `json:"expires_at"`
	CheckoutToken   string          `json:"-"`
	NotifyURL       string          `json:"notify_url"`
	ReturnURL       string          `json:"return_url"`
	CNYAmount       int64           `json:"cny_amount,string"`
	Name            string          `json:"name"`
	Param           string          `json:"param"`
	Version         string          `json:"version"`
	ChannelID       int64           `json:"channel_id,string"`
	ChannelRevision int64           `json:"channel_revision,string"`
	FeeBPS          int64           `json:"fee_bps"`
	FeeSource       string          `json:"fee_source"`
	ChannelSnapshot json.RawMessage `json:"channel_snapshot"`
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
	if prior, ok := ctx.Value(authorityKey{}).(func(context.Context, pgx.Tx) error); ok && prior != nil {
		return context.WithValue(ctx, authorityKey{}, func(inner context.Context, tx pgx.Tx) error {
			if e := prior(inner, tx); e != nil {
				return e
			}
			if guard != nil {
				return guard(inner, tx)
			}
			return nil
		})
	}
	if guard == nil {
		return ctx
	}
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
