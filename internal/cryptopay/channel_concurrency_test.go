package cryptopay

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestCryptoFirstChannelRetainsOnlySelectedLock(t *testing.T) {
	for _, quotaFull := range []bool{false, true} {
		name := "later channel"
		if quotaFull {
			name = "earlier channel over quota"
		}
		t.Run(name, func(t *testing.T) {
			s := channelTestService(t)
			first := saveTestChannel(t, s, "first", "tron")
			second := saveTestChannel(t, s, "second", "ethereum")
			for _, address := range []string{depositOne, depositTwo} {
				if err := s.AddAddress(t.Context(), "tron", address, "deposit"); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.AddAddress(t.Context(), "ethereum", "0x0000000000000000000000000000000000000001", "deposit"); err != nil {
				t.Fatal(err)
			}
			selected, unused := first, second
			if quotaFull {
				if _, err := s.Pool.Exec(t.Context(), "UPDATE crypto_channels SET day_limit=2000000 WHERE id=$1", first.ID); err != nil {
					t.Fatal(err)
				}
				if _, err := s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 2, ChannelID: first.ID, OutTradeNo: "existing-reservation", Amount: "1"}); err != nil {
					t.Fatal(err)
				}
				selected, unused = second, first
			}
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			checked := make(chan struct{})
			release := make(chan struct{}, 1)
			defer close(release)
			type result struct {
				invoice Invoice
				err     error
			}
			done := make(chan result, 1)
			checks := 0
			go func() {
				ctx := WithAuthority(ctx, func(ctx context.Context, _ pgx.Tx) error {
					checks++
					if checks == 3 {
						close(checked)
						select {
						case <-release:
						case <-ctx.Done():
							return ctx.Err()
						}
					}
					return nil
				})
				invoice, err := s.CreateInvoice(ctx, CreateInvoiceInput{MerchantID: 1, OutTradeNo: "first-available", Amount: "2"})
				done <- result{invoice, err}
			}()
			select {
			case <-checked:
			case r := <-done:
				t.Fatalf("first order ended before commit gate: %+v", r)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			// Another merchant must be able to use the unselected channel
			// while the first invoice still holds its transaction open.
			otherCtx, cancelOther := context.WithTimeout(ctx, 2*time.Second)
			defer cancelOther()
			other, err := s.CreateInvoice(otherCtx, CreateInvoiceInput{MerchantID: 2, ChannelID: unused.ID, OutTradeNo: "independent-channel", Amount: "1"})
			if err != nil || other.ChannelID != unused.ID {
				t.Errorf("unselected channel %d blocked: %+v %v", unused.ID, other, err)
			}
			release <- struct{}{}
			r := <-done
			if r.err != nil || r.invoice.ChannelID != selected.ID {
				t.Fatalf("wrong first-available decision: %+v", r)
			}
		})
	}
}

func TestCryptoDailyLimitCountsExpiredOrdersAndResetsAtUTCDate(t *testing.T) {
	s := channelTestService(t)
	c := saveTestChannel(t, s, "capped", "tron")
	if _, err := s.Pool.Exec(t.Context(), "UPDATE crypto_channels SET day_limit=10000000 WHERE id=$1", c.ID); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{depositOne, depositTwo} {
		if err := s.AddAddress(t.Context(), "tron", address, "deposit"); err != nil {
			t.Fatal(err)
		}
	}
	first := CreateInvoiceInput{MerchantID: 1, ChannelID: c.ID, OutTradeNo: "expired-reservation", Amount: "10"}
	invoice, err := s.CreateInvoice(t.Context(), first)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(t.Context(), "UPDATE crypto_invoices SET state='expired' WHERE id=$1", invoice.ID); err != nil {
		t.Fatal(err)
	}
	replayed, err := s.CreateInvoice(t.Context(), first)
	if err != nil || replayed.ID != invoice.ID || replayed.State != "expired" {
		t.Fatalf("original reservation did not replay: %+v %v", replayed, err)
	}
	fresh := CreateInvoiceInput{MerchantID: 2, ChannelID: c.ID, OutTradeNo: "another-day", Amount: "10"}
	if _, err = s.CreateInvoice(t.Context(), fresh); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("same-date expired invoice stopped counting toward quota: %v", err)
	}
	if _, err = s.Pool.Exec(t.Context(), "UPDATE crypto_invoices SET created_at=(date_trunc('day',clock_timestamp() AT TIME ZONE 'UTC') AT TIME ZONE 'UTC') - interval '1 microsecond' WHERE id=$1", invoice.ID); err != nil {
		t.Fatal(err)
	}
	if next, err := s.CreateInvoice(t.Context(), fresh); err != nil || next.Address == invoice.Address {
		t.Fatalf("previous UTC date consumed today's quota or address was reused: %+v %v", next, err)
	}
}
