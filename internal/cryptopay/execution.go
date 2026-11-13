package cryptopay

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/cryptochain"
	"goepay/internal/cryptosigner"
	"strconv"
	"time"
)

type Signer interface {
	Sign(context.Context, cryptochain.PreparedTransaction) (cryptochain.SignedTransaction, error)
}
type Executor struct {
	Service           *Service
	Adapters, Witness map[string]cryptochain.Adapter
	Signer            Signer
	FeeLimits         map[string]uint64
}

// Execute serializes a wallet's outgoing nonce and persists each boundary before
// invoking external code. Recovery can only sign the SAME prepared transaction
// or broadcast the SAME signed transaction; it never replaces an unknown send.
func (x *Executor) Execute(ctx context.Context, id string) error {
	s := x.Service
	if s == nil || s.Pool == nil || x.Signer == nil {
		return ErrUnavailable
	}
	v, e := s.GetWithdrawal(ctx, id)
	if e != nil {
		return e
	}
	a := x.Adapters[v.Network]
	if a == nil || string(a.Chain()) != v.Network || x.FeeLimits[v.Network] == 0 {
		return ErrUnavailable
	}
	conn, e := s.Pool.Acquire(ctx)
	if e != nil {
		return e
	}
	defer conn.Release()
	lock := "crypto-send:" + v.Network + ":" + v.Source
	var locked bool
	if e = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,199))", lock).Scan(&locked); e != nil {
		return e
	}
	if !locked {
		return ErrConflict
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock(hashtextextended($1,199))", lock)
	v, e = s.GetWithdrawal(ctx, id)
	if e != nil {
		return e
	}
	if v.State == "confirmed" || v.State == "failed" {
		return nil
	}
	if v.State == "requested" || v.State == "cancelled" {
		return ErrConflict
	}
	if _, e = normalize(v.Network, v.Source); e != nil {
		return ErrUnavailable
	}
	if e = a.VerifyNetwork(ctx); e != nil {
		return e
	}
	var prepared cryptochain.PreparedTransaction
	var signed cryptochain.SignedTransaction
	var pb, sb []byte
	if e = s.Pool.QueryRow(ctx, "SELECT prepared,signed FROM crypto_withdrawals WHERE id=$1", id).Scan(&pb, &sb); e != nil {
		return e
	}
	if len(sb) == 0 && v.CancellationRequested {
		return x.cancelUnsigned(ctx, v, pb)
	}
	if len(sb) > 0 {
		if e = json.Unmarshal(sb, &signed); e != nil {
			return e
		}
		if e = json.Unmarshal(pb, &prepared); e != nil {
			return e
		}
		if e = validatePrepared(v, prepared, prepared.FeeLimit); e != nil {
			return e
		}
		if e = cryptosigner.VerifySigned(prepared, signed); e != nil {
			return e
		}
		status, err := a.Lookup(ctx, signed.TxID)
		if err != nil {
			return err
		}
		if status.Found && status.Finalized {
			witness := x.Witness[v.Network]
			if witness == nil || witness == a {
				return ErrUnavailable
			}
			if e = witness.VerifyNetwork(ctx); e != nil {
				return e
			}
			other, err := witness.Lookup(ctx, signed.TxID)
			if err != nil {
				return err
			}
			if !sameStatus(status, other) {
				return ErrConflict
			}
			return x.finish(ctx, v, prepared, signed, status)
		}
		return x.broadcast(ctx, v, signed, a)
	}
	if len(pb) == 0 {
		if v.Kind != "sweep" {
			var allowed bool
			if e = s.Pool.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM crypto_recipients WHERE merchant_id=$1 AND network=$2 AND address=$3 AND state='approved' AND usable_at<=clock_timestamp())", v.MerchantID, v.Network, v.Address).Scan(&allowed); e != nil {
				return e
			}
			if !allowed {
				return ErrConflict
			}
		}
		if _, e = s.Pool.Exec(ctx, "UPDATE crypto_withdrawals SET state='preparing' WHERE id=$1 AND state IN ('approved','preparing')", id); e != nil {
			return e
		}
		n, _ := ParseAmount(v.Amount)
		req := cryptochain.TransferRequest{RequestID: id, From: v.Source, To: v.Address, AtomicAmount: strconv.FormatInt(n, 10), FeeLimit: x.FeeLimits[v.Network]}
		prepared, e = a.Prepare(ctx, req)
		if e != nil {
			return e
		}
		if v.Network == "ethereum" {
			// Wallet lock spans the node nonce read and durable reservation. Source is
			// exclusively managed here; the signer separately rejects nonce reuse.
			tx, err := s.Pool.Begin(ctx)
			if err != nil {
				return err
			}
			defer tx.Rollback(ctx)
			var next int64
			err = tx.QueryRow(ctx, "SELECT next_nonce FROM crypto_nonces WHERE network=$1 AND address=$2 FOR UPDATE", v.Network, v.Source).Scan(&next)
			if err != nil && err != pgx.ErrNoRows {
				return err
			}
			if prepared.Nonce >= uint64(^uint64(0)>>1) {
				return ErrInvalid
			}
			// Only a durable signer cancellation may release an unsigned nonce.
			// Reusing the lowest hole lets already-signed higher nonces progress.
			if _, e = tx.Exec(ctx, "DELETE FROM crypto_released_nonces WHERE network=$1 AND address=$2 AND nonce<$3", v.Network, v.Source, int64(prepared.Nonce)); e != nil {
				return e
			}
			chosen := max(uint64(next), prepared.Nonce)
			var released int64
			err = tx.QueryRow(ctx, "SELECT nonce FROM crypto_released_nonces WHERE network=$1 AND address=$2 ORDER BY nonce FOR UPDATE LIMIT 1", v.Network, v.Source).Scan(&released)
			if err == nil {
				chosen = uint64(released)
				if _, e = tx.Exec(ctx, "DELETE FROM crypto_released_nonces WHERE network=$1 AND address=$2 AND nonce=$3", v.Network, v.Source, released); e != nil {
					return e
				}
			} else if err != pgx.ErrNoRows {
				return err
			}
			if chosen != prepared.Nonce {
				nonce := chosen
				req.Nonce = &nonce
				prepared, e = a.Prepare(ctx, req)
				if e != nil {
					return e
				}
			}
			if _, e = tx.Exec(ctx, "INSERT INTO crypto_nonces(network,address,next_nonce) VALUES($1,$2,$3) ON CONFLICT(network,address) DO UPDATE SET next_nonce=GREATEST(crypto_nonces.next_nonce,EXCLUDED.next_nonce)", v.Network, v.Source, int64(prepared.Nonce)+1); e != nil {
				return e
			}
			if e = validatePrepared(v, prepared, x.FeeLimits[v.Network]); e != nil {
				return e
			}
			pb, e = json.Marshal(prepared)
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, "UPDATE crypto_withdrawals SET prepared=$2,state='prepared',nonce=$3 WHERE id=$1", id, pb, int64(prepared.Nonce)); e != nil {
				return e
			}
			if e = tx.Commit(ctx); e != nil {
				return e
			}
		} else {
			if e = validatePrepared(v, prepared, x.FeeLimits[v.Network]); e != nil {
				return e
			}
			pb, e = json.Marshal(prepared)
			if e != nil {
				return e
			}
			if _, e = s.Pool.Exec(ctx, "UPDATE crypto_withdrawals SET prepared=$2,state='prepared' WHERE id=$1", id, pb); e != nil {
				return e
			}
		}
	} else if e = json.Unmarshal(pb, &prepared); e != nil {
		return e
	}
	if v.State != "signing" && v.Network == "tron" && prepared.Expiration > 0 && prepared.Expiration <= uint64(time.Now().UnixMilli()) {
		return x.cancelUnsigned(ctx, v, pb)
	}
	if e = validatePrepared(v, prepared, x.FeeLimits[v.Network]); e != nil {
		return e
	}
	if _, e = s.Pool.Exec(ctx, "UPDATE crypto_withdrawals SET state='signing' WHERE id=$1", id); e != nil {
		return e
	}
	signTx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer signTx.Rollback(ctx)
	var cancelRequested bool
	if e = signTx.QueryRow(ctx, "SELECT cancel_requested FROM crypto_withdrawals WHERE id=$1 FOR UPDATE", id).Scan(&cancelRequested); e != nil {
		return e
	}
	if cancelRequested {
		return ErrConflict
	}
	if v.Kind != "sweep" {
		var recipient string
		e = signTx.QueryRow(ctx, "SELECT id FROM crypto_recipients WHERE merchant_id=$1 AND network=$2 AND address=$3 AND state='approved' AND usable_at<=clock_timestamp() FOR SHARE", v.MerchantID, v.Network, v.Address).Scan(&recipient)
		if e != nil {
			return ErrConflict
		}
	}
	signed, e = x.Signer.Sign(ctx, prepared)
	if e != nil {
		if v.Network == "tron" && prepared.Expiration <= uint64(time.Now().UnixMilli()) && errors.Is(e, cryptosigner.ErrPolicy) {
			signTx.Rollback(ctx)
			return x.cancelUnsigned(ctx, v, pb)
		}
		return e
	}
	if e = cryptosigner.VerifySigned(prepared, signed); e != nil {
		return e
	}
	if signed.Chain != prepared.Chain || signed.TxID == "" || signed.RawTransaction == "" {
		return ErrConflict
	}
	sb, e = json.Marshal(signed)
	if e != nil {
		return e
	}
	if _, e = signTx.Exec(ctx, "UPDATE crypto_withdrawals SET signed=$2,tx_id=$3,state='signed' WHERE id=$1", id, sb, signed.TxID); e != nil {
		return e
	}
	if e = signTx.Commit(ctx); e != nil {
		return e
	}
	return x.broadcast(ctx, v, signed, a)
}
