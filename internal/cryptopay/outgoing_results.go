package cryptopay

import (
	"context"
	"goepay/internal/cryptochain"
	"strconv"
)

func sameStatus(a, b cryptochain.TransactionStatus) bool {
	return a.Chain == b.Chain && a.TxID == b.TxID && a.Found == b.Found && a.Finalized == b.Finalized && a.Success == b.Success && a.BlockHash == b.BlockHash && a.BlockHeight == b.BlockHeight && a.FeeAtomic == b.FeeAtomic && sameTransfers(a.Transfers, b.Transfers)
}
func validatePrepared(v Withdrawal, p cryptochain.PreparedTransaction, fee uint64) error {
	n, e := ParseAmount(v.Amount)
	contract := cryptochain.TronUSDTContract
	if v.Network == "ethereum" {
		contract = cryptochain.EthereumUSDTContract
	}
	if e != nil || string(p.Chain) != v.Network || p.Network != cryptochain.Mainnet || p.RequestID != v.ID || p.From != v.Source || p.To != v.Address || p.Contract != contract || p.AtomicAmount != strconv.FormatInt(n, 10) || p.UnsignedPayload == "" || p.FeeLimit > fee {
		return ErrConflict
	}
	return nil
}
func (x *Executor) broadcast(ctx context.Context, v Withdrawal, t cryptochain.SignedTransaction, a cryptochain.Adapter) error {
	if _, e := x.Service.Pool.Exec(ctx, "UPDATE crypto_withdrawals SET state='broadcasting' WHERE id=$1", v.ID); e != nil {
		return e
	}
	result, e := a.Broadcast(ctx, t)
	state := "unknown"
	if e == nil && result.Accepted && result.TxID == t.TxID {
		state = "broadcasting"
	} else if e == nil {
		e = ErrConflict
	}
	if _, dbErr := x.Service.Pool.Exec(context.WithoutCancel(ctx), "UPDATE crypto_withdrawals SET state=$2 WHERE id=$1", v.ID, state); dbErr != nil {
		return dbErr
	}
	return e
}
func (x *Executor) finish(ctx context.Context, v Withdrawal, p cryptochain.PreparedTransaction, signed cryptochain.SignedTransaction, status cryptochain.TransactionStatus) error {
	if string(status.Chain) != v.Network || status.TxID != signed.TxID {
		return ErrConflict
	}
	if status.Success {
		matched := false
		for _, t := range status.Transfers {
			if t.Chain == p.Chain && t.TxID == signed.TxID && t.Contract == p.Contract && t.From == p.From && t.To == p.To && t.AtomicAmount == p.AtomicAmount {
				if matched {
					return ErrConflict
				}
				matched = true
			}
		}
		if !matched {
			return ErrConflict
		}
	}
	tx, e := x.Service.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	current, e := scanWithdrawal(tx.QueryRow(ctx, "SELECT "+withdrawalColumns+" FROM crypto_withdrawals WHERE id=$1 FOR UPDATE", v.ID))
	if e != nil {
		return e
	}
	if current.State == "confirmed" || current.State == "failed" {
		return nil
	}
	if current.TxID != signed.TxID {
		return ErrConflict
	}
	state := "failed"
	n, _ := ParseAmount(v.Amount)
	if status.Success {
		state = "confirmed"
		if v.Kind != "sweep" {
			e = post(ctx, tx, v.Network, "outgoing:"+v.ID, posting{v.MerchantID, "reserved", -n}, posting{0, "clearing", n})
		}
	} else if v.Kind != "sweep" {
		credit := posting{v.MerchantID, "available", n}
		if v.Kind == "exception_refund" {
			credit = posting{0, "suspense", n}
		}
		e = post(ctx, tx, v.Network, "failed:"+v.ID, posting{v.MerchantID, "reserved", -n}, credit)
	}
	if e != nil {
		return e
	}
	if v.Kind == "sweep" {
		if status.Success {
			_, e = tx.Exec(ctx, "UPDATE crypto_sweep_queue SET failures=0 WHERE network=$1 AND address=$2", v.Network, v.Source)
		} else {
			e = retrySweep(ctx, tx, v)
		}
		if e != nil {
			return e
		}
	}
	if status.FeeAtomic != "" {
		asset := "TRX"
		if v.Network == "ethereum" {
			asset = "ETH"
		}
		if _, e = tx.Exec(ctx, "INSERT INTO crypto_network_fees(network,tx_id,asset,atomic_amount) VALUES($1,$2,$3,$4::numeric)", v.Network, signed.TxID, asset, status.FeeAtomic); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE crypto_withdrawals SET state=$2 WHERE id=$1", v.ID, state); e != nil {
		return e
	}
	if e = audit(ctx, tx, 0, "chain-"+state, v.ID); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
