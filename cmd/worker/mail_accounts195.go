package main

import (
	"context"
	"github.com/jackc/pgx/v5"
	"goepay/internal/mailaccount"
)

type mailAccountResolver195 interface {
	ResolveSMTP(context.Context) (*mailaccount.BoundSMTP, bool, error)
}

type txMailResolver231 interface {
	ResolveSMTPTx231(context.Context, pgx.Tx) (*mailaccount.BoundSMTP, bool, error)
}
type txMailBinding231 struct {
	source txMailResolver231
	tx     pgx.Tx
}

func (t txMailBinding231) ResolveSMTP(ctx context.Context) (*mailaccount.BoundSMTP, bool, error) {
	return t.source.ResolveSMTPTx231(ctx, t.tx)
}
func bindMailTx231(source mailAccountResolver195, tx pgx.Tx) mailAccountResolver195 {
	if resolved, ok := source.(txMailResolver231); ok {
		return txMailBinding231{resolved, tx}
	}
	return source
}
