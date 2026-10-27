package payment

import (
	"context"
	"crypto/hmac"
	"fmt"
	"github.com/jackc/pgx/v5"
)

type coreRiskChildCheckState struct {
	active map[int64]bool
	multi  bool
}

// Original 98 route/account/birth proofs authenticate the child namespace.
// Current authenticated heads decide whether the original automatic check is
// applicable; no account restore or provider request occurs here.
func (s *Store) coreRiskChildCheckSourceTx(ctx context.Context, tx pgx.Tx, id, parent int64, cache map[int64]coreRiskChildCheckState) (int64, bool, error) {
	o, e := scanOrder(tx.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE id=$1`, id))
	if e != nil {
		return 0, false, e
	}
	present, e := s.childSchemaPresent(ctx, tx, o.MerchantID, o.OutTradeNo)
	if e != nil || !present {
		return 0, false, e
	}
	state, known := cache[parent]
	if !known {
		state.active = map[int64]bool{}
		rows, e := tx.Query(ctx, `SELECT id FROM payment_subchannels WHERE parent_id=$1 ORDER BY id`, parent)
		if e != nil {
			return 0, false, e
		}
		ids := []int64{}
		for rows.Next() {
			var id int64
			if e = rows.Scan(&id); e != nil {
				rows.Close()
				return 0, false, e
			}
			ids = append(ids, id)
			if len(ids) > 10000 {
				rows.Close()
				return 0, false, ErrState
			}
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return 0, false, e
		}
		for _, id := range ids {
			c, e := s.readSubchannel(ctx, tx, tx.QueryRow(ctx, `SELECT `+childColumns+` FROM payment_subchannels WHERE id=$1 FOR SHARE`, id))
			if e != nil {
				return 0, false, e
			}
			if c.Enabled && !c.Archived {
				state.active[id] = true
				state.multi = true
			}
		}
		cache[parent] = state
	}
	b, e := s.readChildRoute(ctx, tx, o.MerchantID, o.OutTradeNo)
	if e != nil {
		return 0, false, e
	}
	quote, found, e := s.readRouteFeeQuote(ctx, tx, o.MerchantID, o.OutTradeNo)
	if e != nil {
		return 0, false, e
	}
	if b == nil {
		if found && quote.facts.Child != nil {
			return 0, false, ErrConflict
		}
		return 0, state.multi, nil
	}
	if !found || b.parent != parent || !childCommitmentEqual(quote.facts.Child, b.commitment()) {
		return 0, false, ErrConflict
	}
	if _, e = s.validateOrderRouteFeeTerms(ctx, tx, o); e != nil {
		return 0, false, e
	}
	account, e := s.childAccount(ctx, tx, b.child, b.version)
	if e != nil || account.Merchant != o.MerchantID || account.Parent != parent || !hmac.Equal(account.Proof, b.accountProof) {
		return 0, false, ErrConflict
	}
	if !state.active[b.child] {
		return 0, state.multi, nil
	}
	return b.child, state.multi, nil
}

func (s *Store) checkCoreRiskChildAdmissionTx(ctx context.Context, tx pgx.Tx, id int64) error {
	if e := lockCoreRiskActionKeys(ctx, tx, [][2]string{{"subchannel", fmt.Sprint(id)}}); e != nil {
		return e
	}
	action, _, e := s.readCoreRiskAction(ctx, tx, "subchannel", fmt.Sprint(id))
	if e != nil {
		return e
	}
	now, e := coreRiskDBNow(ctx, tx)
	if e != nil {
		return e
	}
	if coreRiskActive(action, now) {
		return ErrState
	}
	return nil
}
