package cryptopay

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"goepay/internal/cryptochain"
	"math"
	"sort"
	"strconv"
	"time"
)

type Scanner struct {
	Service          *Service
	Primary, Witness map[string]cryptochain.Adapter
	StartHeight      map[string]uint64
	MaxBlocks        uint64
}

func (s *Scanner) Scan(ctx context.Context, network string) error {
	return s.scan(ctx, network, nil)
}

// ScanForReadiness returns the database time at which the listener finished
// proving a caught-up canonical cursor. A healthy partial scan has no proof.
func (s *Scanner) ScanForReadiness(ctx context.Context, network string) (time.Time, error) {
	var checkedAt time.Time
	err := s.scan(ctx, network, &checkedAt)
	return checkedAt, err
}

func (s *Scanner) scan(ctx context.Context, network string, checkedAt *time.Time) error {
	if s.Service == nil || s.Service.Pool == nil {
		return ErrUnavailable
	}
	a, b := s.Primary[network], s.Witness[network]
	start, ok := s.StartHeight[network]
	if a == nil || b == nil || a == b || !ok || start < 1 || start > math.MaxInt64 {
		return ErrUnavailable
	}
	cfg := s.Service.Networks[network]
	if !cfg.Enabled {
		return ErrUnavailable
	}
	if e := a.VerifyNetwork(ctx); e != nil {
		return e
	}
	if e := b.VerifyNetwork(ctx); e != nil {
		return e
	}
	conn, e := s.Service.Pool.Acquire(ctx)
	if e != nil {
		return e
	}
	defer conn.Release()
	var lock bool
	if e = conn.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,200))", network).Scan(&lock); e != nil {
		return e
	}
	if !lock {
		return ErrConflict
	}
	defer conn.Exec(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock(hashtextextended($1,200))", network)
	var height uint64
	var previous string
	e = conn.QueryRow(ctx, "SELECT height,block_hash FROM crypto_scan_cursors WHERE network=$1", network).Scan(&height, &previous)
	if e == pgx.ErrNoRows {
		height = start - 1
	} else if e != nil {
		return e
	} else {
		h, err := a.CanonicalBlockHash(ctx, height)
		if err != nil {
			return err
		}
		w, err := b.CanonicalBlockHash(ctx, height)
		if err != nil {
			return err
		}
		if h != previous || w != previous {
			return ErrConflict
		}
	}
	ah, e := a.FinalizedHeight(ctx)
	if e != nil {
		return e
	}
	bh, e := b.FinalizedHeight(ctx)
	if e != nil {
		return e
	}
	finalized := min(ah, bh)
	if finalized > math.MaxInt64 {
		return ErrInvalid
	}
	limit := s.MaxBlocks
	if limit == 0 {
		limit = 20
	}
	if limit > 1000 {
		return ErrInvalid
	}
	end := min(finalized, height+limit)
	cursorHash := previous
	for next := height + 1; next <= end; next++ {
		h, e := a.CanonicalBlockHash(ctx, next)
		if e != nil {
			return e
		}
		w, e := b.CanonicalBlockHash(ctx, next)
		if e != nil {
			return e
		}
		if h == "" || h != w {
			return ErrConflict
		}
		transfers, e := a.ScanTransfers(ctx, next, next)
		if e != nil {
			return e
		}
		witness, e := b.ScanTransfers(ctx, next, next)
		if e != nil {
			return e
		}
		if !sameTransfers(transfers, witness) {
			return ErrConflict
		}
		for _, t := range transfers {
			contract := cryptochain.TronUSDTContract
			if network == "ethereum" {
				contract = cryptochain.EthereumUSDTContract
			}
			if string(t.Chain) != network || t.Contract != contract || t.BlockHash != h || t.BlockHeight != next || t.EventIndex > math.MaxInt32 || t.OccurredAt.IsZero() {
				return ErrConflict
			}
			var known bool
			e = conn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM crypto_invoices WHERE network=$1 AND address=$2)", network, t.To).Scan(&known)
			if e != nil {
				return e
			}
			if !known {
				continue
			}
			n, e := strconv.ParseInt(t.AtomicAmount, 10, 64)
			if e != nil || n <= 0 || n > maxAmount {
				return ErrInvalid
			}
			if e = s.Service.RecordDeposit(ctx, Deposit{Network: network, TxID: t.TxID, EventIndex: int(t.EventIndex), BlockHeight: int64(next), BlockHash: h, Address: t.To, Amount: FormatAmount(n), OccurredAt: t.OccurredAt}); e != nil {
				return e
			}
		}
		if _, e = conn.Exec(ctx, "INSERT INTO crypto_scan_cursors(network,height,block_hash) VALUES($1,$2,$3) ON CONFLICT(network) DO UPDATE SET height=EXCLUDED.height,block_hash=EXCLUDED.block_hash", network, int64(next), h); e != nil {
			return e
		}
		cursorHash = h
	}
	if checkedAt != nil && end == finalized && end >= height && end >= start && cursorHash != "" {
		// Capture the proof time before releasing the scan lock. Delayed
		// publishers can safely compare this timestamp against newer proofs.
		var observed time.Time
		if err := conn.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&observed); err == nil {
			*checkedAt = observed
		}
	}
	return nil
}
func sameTransfers(a, b []cryptochain.Transfer) bool {
	normalize := func(in []cryptochain.Transfer) string {
		rows := append([]cryptochain.Transfer(nil), in...)
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].TxID == rows[j].TxID {
				return rows[i].EventIndex < rows[j].EventIndex
			}
			return rows[i].TxID < rows[j].TxID
		})
		raw, _ := json.Marshal(rows)
		return string(raw)
	}
	return normalize(a) == normalize(b)
}
