package payment

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"

	"goepay/internal/identity"
)

func transferRoutingActor228(t *testing.T, s *Store) ParentChannelActor {
	t.Helper()
	if _, err := s.pool.Exec(t.Context(), `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('routing228','admin',228,'Synthetic Routing','unused','super')`); err != nil {
		t.Fatal(err)
	}
	u := identity.User{Username: "routing228", ID: 228, Role: "admin", AdminClass: "super", AuthVersion: 1}
	token, csrf, err := identity.NewPersistentSessions(s.pool, false).Create(u)
	if err != nil {
		t.Fatal(err)
	}
	return ParentChannelActor{User: u, Token: token, CSRF: csrf}
}

func TestTransferRouting228RuntimeViewUsesCallerSnapshotWithoutSending(t *testing.T) {
	s := testStore(t)
	p := &outgoingProvider{}
	g := ProviderFlow{Store: s, Providers: map[int64]adapter.Provider{2: p}, AllowLive: true}
	tx, err := s.pool.BeginTx(t.Context(), pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(t.Context())
	view, allowed, err := g.RuntimeParentChannelsSnapshot(t.Context(), tx, 1, map[int64][]int64{1: {2}})
	if err != nil || len(allowed) != 1 || allowed[0] != 2 || view.Providers[2] != p || p.transfers.Load() != 0 || p.calls.Load() != 0 {
		t.Fatal("runtime snapshot changed authorization or sent a request", allowed, err)
	}
	// The caller still owns and can use its transaction.
	var one int
	if err := tx.QueryRow(t.Context(), "SELECT 1").Scan(&one); err != nil || one != 1 {
		t.Fatal(err)
	}
}

func TestTransferRouting228PolicyPersistsWithoutReinterpretingLegacyFlags(t *testing.T) {
	s := testStore(t)
	a := transferRoutingActor228(t, s)
	if _, err := s.pool.Exec(t.Context(), `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"transfer_wxpay","value":"1"}')`); err != nil {
		t.Fatal(err)
	}
	before, err := s.TransferRouting228(t.Context())
	if err != nil || before.Configured || before.Routes["wxpay"] != "inherit" {
		t.Fatal("legacy 1 became channel selection", before, err)
	}
	routes := map[string]string{"alipay": "inherit", "wxpay": "channel:2", "qqpay": "disabled", "bank": "manual"}
	after, err := s.SaveTransferRouting228(t.Context(), a, before.Fingerprint, routes)
	if err != nil || !after.Configured || after.Revision != "1" || after.Routes["wxpay"] != "channel:2" {
		t.Fatal(after, err)
	}
	read, err := s.TransferRouting228(t.Context())
	if err != nil || read.Fingerprint != after.Fingerprint || read.Routes["bank"] != "manual" {
		t.Fatal("configuration did not persist", read, err)
	}
	var legacy string
	if err := s.pool.QueryRow(t.Context(), `SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='transfer_wxpay'`).Scan(&legacy); err != nil || legacy != "1" {
		t.Fatal("legacy flag mutated", legacy, err)
	}
	if _, err := s.SaveTransferRouting228(t.Context(), a, before.Fingerprint, routes); !errors.Is(err, ErrConflict) {
		t.Fatal("stale writer was accepted", err)
	}
	a.Delegated = true
	if _, err := s.SaveTransferRouting228(t.Context(), a, after.Fingerprint, routes); err == nil {
		t.Fatal("delegated session wrote global payment routing")
	}
}
