package cryptopay

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestCryptoQuoteExpiresWhileFinalAuthorityIsWaiting(t *testing.T) {
	s := channelTestService(t)
	channel := saveTestChannel(t, s, "quote deadline", "tron")
	if err := s.AddAddress(t.Context(), "tron", depositOne, "deposit"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var deadline time.Time
	if err := s.Pool.QueryRow(ctx, "SELECT clock_timestamp()+interval '2 seconds'").Scan(&deadline); err != nil {
		t.Fatal(err)
	}
	checks := 0
	ctx = WithAuthority(ctx, func(ctx context.Context, tx pgx.Tx) error {
		checks++
		// begin, merchant lock, final proof after the invoice write.
		if checks != 3 {
			return nil
		}
		_, err := tx.Exec(ctx, "SELECT pg_sleep(GREATEST(0,EXTRACT(EPOCH FROM($1::timestamptz-clock_timestamp())))+0.01)", deadline)
		return err
	})
	in := CreateInvoiceInput{MerchantID: 1, ChannelID: channel.ID, OutTradeNo: "expires-at-commit", Amount: "1", CNYAmount: 720, QuoteRate: "7.2", QuoteValidUntil: deadline}
	_, err := s.CreateInvoice(ctx, in)
	if !errors.Is(err, ErrQuoteExpired) || checks != 3 {
		t.Fatalf("late invoice crossed the final quote cutoff: checks=%d error=%v", checks, err)
	}
	var invoices, used int
	if err := s.Pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM crypto_invoices), (SELECT count(*) FROM crypto_addresses WHERE used)").Scan(&invoices, &used); err != nil || invoices != 0 || used != 0 {
		t.Fatalf("expired quote left a reservation: invoices=%d addresses=%d error=%v", invoices, used, err)
	}
}
