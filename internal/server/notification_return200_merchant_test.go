package server

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/payment"
)

func TestNotificationReturn200MerchantTxUsesCallerConnectionAndLocksCurrentSigningIdentity(t *testing.T) {
	f := notificationReturn200Local(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	config := f.p.Config()
	config.MaxConns = 1
	one, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer one.Close()
	store, err := payment.NewWithKey(one, make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := f.s.Store.Merchant(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := one.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	actual, err := store.MerchantTx(ctx, tx, 1)
	if err != nil || !reflect.DeepEqual(actual, legacy) {
		t.Fatal("transactional merchant changed existing fields/decryption or acquired a second connection")
	}
	missing, err := store.MerchantTx(ctx, tx, 999200)
	if !errors.Is(err, payment.ErrNotFound) || missing.ID != 0 {
		t.Fatal("transactional missing merchant semantics changed")
	}
	writer, err := f.p.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback(context.Background())
	writerPID := writer.Conn().PgConn().PID()
	readerPID := tx.Conn().PgConn().PID()
	done := make(chan error, 1)
	go func() {
		_, e := writer.Exec(ctx, "UPDATE merchants SET name='changed-after-return200-lock' WHERE id=1")
		done <- e
	}()
	for {
		var waiting bool
		if err = f.p.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=$1 AND $2::integer=ANY(pg_blocking_pids(pid)))", writerPID, readerPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-done:
			t.Fatal("merchant signing identity was not protected by the caller transaction")
		case <-ctx.Done():
			t.Fatal("transaction lock was not observed before deadline")
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err = <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("writer stayed blocked after the caller released its lock")
	}
	if err = writer.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	legacy, err = store.Merchant(ctx, 1)
	if err != nil || legacy.Name != "changed-after-return200-lock" {
		t.Fatal("legacy merchant no longer observes committed merchant changes")
	}
	tx, err = one.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	actual, err = store.MerchantTx(ctx, tx, 1)
	if err != nil || !reflect.DeepEqual(actual, legacy) {
		t.Fatal("transactional merchant did not observe the same committed identity as the legacy reader")
	}
}

func TestNotificationReturn200MerchantTxPreservesMissingAndCipherAssociatedDataErrors(t *testing.T) {
	f := notificationReturn200Local(t)
	if _, err := f.s.Store.Merchant(t.Context(), 999200); !errors.Is(err, payment.ErrNotFound) {
		t.Fatal("legacy missing merchant semantics changed")
	}
	// Copying a valid ciphertext to another merchant must still fail the
	// merchant-ID associated-data check in both public readers.
	if _, err := f.p.Exec(t.Context(), "UPDATE merchants SET api_key=(SELECT api_key FROM merchants WHERE id=1) WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.s.Store.Merchant(t.Context(), 2); err == nil {
		t.Fatal("legacy reader accepted another merchant's encrypted signing identity")
	}
	tx, err := f.p.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	if _, err = f.s.Store.MerchantTx(t.Context(), tx, 2); err == nil {
		t.Fatal("transactional reader accepted another merchant's encrypted signing identity")
	}
	if _, err = f.s.Store.MerchantTx(t.Context(), nil, 1); !errors.Is(err, payment.ErrInvalid) {
		t.Fatal("nil caller transaction did not fail closed")
	}
}
