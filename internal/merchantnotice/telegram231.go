package merchantnotice

import (
	"context"
	"github.com/jackc/pgx/v5"
)

func (s *Store) prepareTelegram231(ctx context.Context, tx pgx.Tx, a *Account) error {
	token, e := s.connect231.ResolveTx(ctx, tx, "telegram")
	if e != nil {
		return e
	}
	a.TelegramResolved = true
	a.TelegramToken = token
	return nil
}
