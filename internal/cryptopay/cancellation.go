package cryptopay

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"goepay/internal/cryptochain"
	"goepay/internal/cryptosigner"
)

type cancellationSigner interface {
	Cancel(context.Context, cryptochain.PreparedTransaction) (cryptosigner.CancellationReceipt, error)
}

// cancelUnsigned is called only with the wallet dispatch lock. Its signer
// tombstone proves this exact request can never acquire a signature later.
func (x *Executor) cancelUnsigned(ctx context.Context, v Withdrawal, pb []byte) error {
	var p cryptochain.PreparedTransaction
	if len(pb) > 0 {
		if e := json.Unmarshal(pb, &p); e != nil {
			return e
		}
		if e := validatePrepared(v, p, p.FeeLimit); e != nil {
			return e
		}
		signer, ok := x.Signer.(cancellationSigner)
		if !ok {
			return ErrUnavailable
		}
		proof, e := signer.Cancel(ctx, p)
		if e != nil {
			return e
		}
		if !cryptosigner.VerifyCancellation(p, proof) {
			return ErrConflict
		}
	}
	tx, e := x.Service.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var state string
	var signed []byte
	if e = tx.QueryRow(ctx, "SELECT state,signed FROM crypto_withdrawals WHERE id=$1 FOR UPDATE", v.ID).Scan(&state, &signed); e != nil {
		return e
	}
	if state == "cancelled" {
		return nil
	}
	if len(signed) > 0 || state == "confirmed" || state == "failed" {
		return ErrConflict
	}
	raw, _ := json.Marshal(p)
	hash := sha256.Sum256(raw)
	if _, e = tx.Exec(ctx, "INSERT INTO crypto_signer_cancellations(withdrawal_id,binding_sha256) VALUES($1,$2)", v.ID, hex.EncodeToString(hash[:])); e != nil {
		return e
	}
	if len(pb) > 0 && v.Network == "ethereum" {
		if _, e = tx.Exec(ctx, "INSERT INTO crypto_released_nonces(network,address,nonce,cancellation_id) VALUES($1,$2,$3,$4)", v.Network, v.Source, int64(p.Nonce), v.ID); e != nil {
			return e
		}
	}
	if v.Kind != "sweep" {
		n, _ := ParseAmount(v.Amount)
		credit := posting{v.MerchantID, "available", n}
		if v.Kind == "exception_refund" {
			credit = posting{0, "suspense", n}
		}
		if e = post(ctx, tx, v.Network, "cancel:"+v.ID, posting{v.MerchantID, "reserved", -n}, credit); e != nil {
			return e
		}
	} else {
		if e = retrySweep(ctx, tx, v); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(ctx, "UPDATE crypto_withdrawals SET state='cancelled',cancel_requested=true WHERE id=$1", v.ID); e != nil {
		return e
	}
	if e = audit(ctx, tx, 0, "cancel-proven-unsigned", v.ID); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
