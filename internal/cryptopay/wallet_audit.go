package cryptopay

import (
	"context"
	"math/big"
	"sort"
)

type WalletAudit struct {
	Network          string `json:"network"`
	OnChainAtomic    string `json:"on_chain_atomic"`
	LedgerAtomic     string `json:"ledger_atomic"`
	DifferenceAtomic string `json:"difference_atomic"`
	Stable           bool   `json:"stable"`
	PendingOutgoing  int64  `json:"pending_outgoing"`
}

// AuditWallets compares custody assets and the USDT control account. A changing
// finality head or an in-flight outgoing makes this an observation, not a
// consistent balance snapshot. It never manufactures a balancing entry.
func (x *Executor) AuditWallets(ctx context.Context, network string) (WalletAudit, error) {
	out := WalletAudit{Network: network}
	a, b := x.Adapters[network], x.Witness[network]
	if a == nil || b == nil || a == b {
		return out, ErrUnavailable
	}
	h, e := a.FinalizedHeight(ctx)
	if e != nil {
		return out, e
	}
	wh, e := b.FinalizedHeight(ctx)
	if e != nil {
		return out, e
	}
	if h != wh {
		return out, ErrConflict
	}
	cfg := x.Service.Networks[network]
	set := map[string]bool{}
	for _, s := range []string{cfg.HotAddress, cfg.TreasuryAddress} {
		if s != "" {
			canonical, e := normalize(network, s)
			if e != nil {
				return out, e
			}
			set[canonical] = true
		}
	}
	rows, e := x.Service.Pool.Query(ctx, "SELECT address FROM crypto_addresses WHERE network=$1", network)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var address string
		if e = rows.Scan(&address); e != nil {
			rows.Close()
			return out, e
		}
		set[address] = true
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	addresses := []string{}
	for address := range set {
		addresses = append(addresses, address)
	}
	sort.Strings(addresses)
	sum := new(big.Int)
	for _, address := range addresses {
		value, e := a.Balance(ctx, address)
		if e != nil {
			return out, e
		}
		w, e := b.Balance(ctx, address)
		if e != nil {
			return out, e
		}
		if value != w {
			return out, ErrConflict
		}
		n, ok := new(big.Int).SetString(value, 10)
		if !ok || n.Sign() < 0 {
			return out, ErrConflict
		}
		sum.Add(sum, n)
	}
	var clearing int64
	e = x.Service.Pool.QueryRow(ctx, "SELECT COALESCE(sum(balance),0)::bigint FROM crypto_accounts WHERE network=$1 AND owner_id=0 AND kind='clearing'", network).Scan(&clearing)
	if e != nil {
		return out, e
	}
	ledger := new(big.Int).Neg(big.NewInt(clearing))
	out.OnChainAtomic = sum.String()
	out.LedgerAtomic = ledger.String()
	out.DifferenceAtomic = new(big.Int).Sub(sum, ledger).String()
	if e = x.Service.Pool.QueryRow(ctx, "SELECT count(*) FROM crypto_withdrawals WHERE network=$1 AND state IN ('preparing','prepared','signing','signed','broadcasting','unknown')", network).Scan(&out.PendingOutgoing); e != nil {
		return out, e
	}
	end, e := a.FinalizedHeight(ctx)
	if e != nil {
		return out, e
	}
	wend, e := b.FinalizedHeight(ctx)
	if e != nil {
		return out, e
	}
	var cursor uint64
	e = x.Service.Pool.QueryRow(ctx, "SELECT height FROM crypto_scan_cursors WHERE network=$1", network).Scan(&cursor)
	if e != nil {
		return out, e
	}
	out.Stable = h == end && end == wend && cursor == end && out.PendingOutgoing == 0
	return out, nil
}
