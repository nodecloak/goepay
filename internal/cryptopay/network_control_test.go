package cryptopay

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
)

const switchDeposit = "0x1111111111111111111111111111111111111111"

func managedNetworkTestService(t *testing.T) *Service {
	t.Helper()
	service := channelTestService(t)
	migration, err := os.ReadFile(filepath.Join("..", "..", "db", "migrations", "000201_crypto_network_control.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Pool.Exec(t.Context(), string(migration)); err != nil {
		t.Fatal(err)
	}
	service.ManageNetworks = true
	service.Networks["ethereum"] = NetworkConfig{
		Enabled:         true,
		HotAddress:      "0x2222222222222222222222222222222222222222",
		TreasuryAddress: "0x3333333333333333333333333333333333333333",
	}
	return service
}

func TestCryptoManagedNetworkChecksWalletPoolAndListenerAndPreservesReplay(t *testing.T) {
	s := managedNetworkTestService(t)
	channel := saveTestChannel(t, s, "ethereum managed", "ethereum")
	ctx := channelAuthority(t.Context())
	setListener := func(fingerprint string, offsetSeconds int) {
		t.Helper()
		if _, err := s.Pool.Exec(t.Context(), `
			INSERT INTO crypto_listener_health(network,fingerprint,checked_at)
			VALUES('ethereum',$1,clock_timestamp()+$2::integer*interval '1 second')
			ON CONFLICT(network) DO UPDATE SET fingerprint=EXCLUDED.fingerprint,checked_at=EXCLUDED.checked_at
		`, fingerprint, offsetSeconds); err != nil {
			t.Fatal(err)
		}
	}
	expectNotReady := func() {
		t.Helper()
		if _, err := s.SetNetworkEnabled(ctx, "ethereum", true, 1, 1); !errors.Is(err, ErrNetworkNotReady) {
			t.Fatalf("unprepared network enabled: %v", err)
		}
	}
	expectNotReady()
	if err := s.AddAddress(ctx, "ethereum", s.Networks["ethereum"].HotAddress, "deposit"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("hot wallet must not be used as a deposit slot: %v", err)
	}
	if err := s.AddAddress(ctx, "ethereum", switchDeposit, "deposit"); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := ListenerFingerprint("ethereum", s.Networks["ethereum"])
	if err != nil {
		t.Fatal(err)
	}
	expectNotReady()
	setListener(fingerprint, -61)
	expectNotReady()
	setListener(fingerprint, 30)
	expectNotReady()
	setListener(fingerprint, 0)
	status, err := s.SetNetworkEnabled(ctx, "ethereum", true, 1, 1)
	if err != nil || status.Revision != 2 || !status.EffectiveEnabled || !status.ReadyToEnable {
		t.Fatalf("ready network failed activation: %+v %v", status, err)
	}
	if _, err = s.SetNetworkEnabled(ctx, "ethereum", false, 1, 1); !errors.Is(err, ErrConflict) {
		t.Fatalf("stale tab changed current state: %v", err)
	}
	in := CreateInvoiceInput{MerchantID: 1, ChannelID: channel.ID, OutTradeNo: "managed-original", Amount: "1"}
	original, err := s.CreateInvoice(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	setListener(fingerprint, -61)
	if ready, err := s.RuntimeNetworkEnabled(ctx, "ethereum"); err != nil || ready {
		t.Fatalf("stale listener left the network active: %t %v", ready, err)
	}
	if fresh, err := s.CreateInvoice(ctx, CreateInvoiceInput{MerchantID: 2, ChannelID: channel.ID, OutTradeNo: "closed-new", Amount: "1"}); !errors.Is(err, ErrUnavailable) || fresh.ID != "" {
		t.Fatalf("unhealthy network admitted a new invoice: %+v %v", fresh, err)
	}
	if _, err := s.RequestWithdrawal(ctx, WithdrawalInput{MerchantID: 1, RequestID: "closed-withdrawal", Network: "ethereum", Address: switchDeposit, Amount: "1"}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unhealthy network admitted a new withdrawal: %v", err)
	}
	replay, err := s.CreateInvoice(ctx, in)
	if err != nil || replay.ID != original.ID || replay.Address != original.Address {
		t.Fatalf("network pause lost original invoice: %+v %v", replay, err)
	}
	cfg := s.Networks["ethereum"]
	cfg.HotAddress = "0x4444444444444444444444444444444444444444"
	s.Networks["ethereum"] = cfg
	changedFingerprint, err := ListenerFingerprint("ethereum", cfg)
	if err != nil {
		t.Fatal(err)
	}
	setListener(changedFingerprint, 0)
	if active, err := s.RuntimeNetworkEnabled(ctx, "ethereum"); err != nil || active {
		t.Fatalf("replacement wallet inherited prior approval: %t %v", active, err)
	}
	status, err = s.SetNetworkEnabled(ctx, "ethereum", false, 2, 1)
	if err != nil || status.Enabled || status.Revision != 3 {
		t.Fatalf("unhealthy network must remain closable: %+v %v", status, err)
	}
}

func TestCryptoManagedNetworkRechecksListenerAfterAuthority(t *testing.T) {
	s := managedNetworkTestService(t)
	if err := s.AddAddress(t.Context(), "ethereum", switchDeposit, "deposit"); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := ListenerFingerprint("ethereum", s.Networks["ethereum"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Pool.Exec(t.Context(), "INSERT INTO crypto_listener_health VALUES('ethereum',$1,clock_timestamp())", fingerprint); err != nil {
		t.Fatal(err)
	}
	checks := 0
	ctx := WithAuthority(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		checks++
		if checks != 2 {
			return nil
		}
		_, err := tx.Exec(ctx, "UPDATE crypto_listener_health SET checked_at=clock_timestamp()-interval '61 seconds' WHERE network='ethereum'")
		return err
	})
	if _, err := s.SetNetworkEnabled(ctx, "ethereum", true, 1, 1); !errors.Is(err, ErrNetworkNotReady) || checks != 2 {
		t.Fatalf("expired proof during final authority was accepted: %d %v", checks, err)
	}
	var enabled bool
	var revision, audits int
	if err := s.Pool.QueryRow(t.Context(), `SELECT enabled,revision,(SELECT count(*) FROM crypto_audit WHERE operation='enable-network') FROM crypto_network_controls WHERE network='ethereum'`).Scan(&enabled, &revision, &audits); err != nil || enabled || revision != 1 || audits != 0 {
		t.Fatalf("late proof left changes: %t %d %d %v", enabled, revision, audits, err)
	}
}

func TestCryptoManagedInvoiceRollsBackIfListenerExpiresDuringFinalAuthority(t *testing.T) {
	s := managedNetworkTestService(t)
	channel := saveTestChannel(t, s, "managed final proof", "ethereum")
	if err := s.AddAddress(t.Context(), "ethereum", switchDeposit, "deposit"); err != nil {
		t.Fatal(err)
	}
	fingerprint, err := ListenerFingerprint("ethereum", s.Networks["ethereum"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Pool.Exec(t.Context(), "INSERT INTO crypto_listener_health VALUES('ethereum',$1,clock_timestamp())", fingerprint); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SetNetworkEnabled(channelAuthority(t.Context()), "ethereum", true, 1, 1); err != nil {
		t.Fatal(err)
	}
	checks := 0
	ctx := WithAuthority(t.Context(), func(ctx context.Context, tx pgx.Tx) error {
		checks++
		if checks == 3 {
			_, err := tx.Exec(ctx, "UPDATE crypto_listener_health SET checked_at=clock_timestamp()-interval '61 seconds' WHERE network='ethereum'")
			return err
		}
		return nil
	})
	if _, err = s.CreateInvoice(ctx, CreateInvoiceInput{MerchantID: 1, ChannelID: channel.ID, OutTradeNo: "listener-expires", Amount: "1"}); !errors.Is(err, ErrUnavailable) || checks != 3 {
		t.Fatalf("invoice crossed the final listener check: %d %v", checks, err)
	}
	var invoices, used int
	if err = s.Pool.QueryRow(t.Context(), "SELECT (SELECT count(*) FROM crypto_invoices),(SELECT count(*) FROM crypto_addresses WHERE used)").Scan(&invoices, &used); err != nil || invoices != 0 || used != 0 {
		t.Fatalf("listener lapse left an invoice or reserved address: %d %d %v", invoices, used, err)
	}
}

func TestCryptoProvisionedNetworkStartsClosedForNewInvoices(t *testing.T) {
	s := managedNetworkTestService(t)
	channel := saveTestChannel(t, s, "ethereum collection", "ethereum")
	if err := s.AddAddress(t.Context(), "ethereum", switchDeposit, "deposit"); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateInvoice(t.Context(), CreateInvoiceInput{MerchantID: 1, ChannelID: channel.ID, OutTradeNo: "provisioned-but-closed", Amount: "1"})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("provisioning alone must not admit a new invoice: %v", err)
	}
}
