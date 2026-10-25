package payment

import (
	"context"
	"github.com/jackc/pgx/v5"
)

// FundsSettlementRecipientView reuses authenticated recipient decryption and
// masking without escaping the caller's transaction to a separate pool read.
func (s *Store) FundsSettlementRecipientView(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id int64) (SettlementRecipientView, error) {
	v, e := s.attachSettlementRecipient(ctx, q, LedgerRequest{ID: id, Kind: "settlements"})
	if e != nil {
		return SettlementRecipientView{}, e
	}
	if v.SettlementRecipient == nil {
		return SettlementRecipientView{}, ErrNotFound
	}
	return *v.SettlementRecipient, nil
}
