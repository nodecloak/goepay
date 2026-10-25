package payment

import (
	"context"
	"errors"
	"testing"
)

type accountBoundProvider struct {
	batchTestProvider
	account string
}

func (p *accountBoundProvider) TransferAccountIdentity() string { return p.account }

func TestTransferAccountSwitchBatchReplayAndPostClaimSend(t *testing.T) {
	for _, stage := range []string{"replay", "after-claim"} {
		t.Run(stage, func(t *testing.T) {
			s, g, _ := batchFixture(t)
			ctx := context.Background()
			original := &accountBoundProvider{account: "original"}
			changed := &accountBoundProvider{account: "changed"}
			g.Providers[7] = original
			b, e := g.PrepareTransferBatch(ctx, 501, "account-stage", batchInputs(100), batchMerchantActor)
			if e != nil {
				t.Fatal(e)
			}
			if stage == "replay" {
				g.Providers[7] = changed
				_, e = g.PrepareTransferBatch(ctx, 501, "account-stage", batchInputs(100), batchMerchantActor)
			} else {
				checks := 0
				_, e = g.ExecuteTransferBatch(ctx, 501, b.ID, 1, batchMerchantActor, func(context.Context, int64) error {
					checks++
					if checks == 3 {
						g.Providers[7] = changed
					}
					return nil
				})
			}
			if !errors.Is(e, ErrConflict) {
				t.Fatal("changed payer inherited original batch", e)
			}
			current, e := s.TransferBatch(ctx, 501, b.ID)
			if e != nil || original.transfers.Load() != 0 || changed.transfers.Load() != 0 {
				t.Fatal(current, e)
			}
			wantUnsent := 0
			if stage == "replay" {
				wantUnsent = 1
			}
			if current.Summary.Unsent != wantUnsent {
				t.Fatal("claimed send was rearmed", current.Summary)
			}
			bal, e := s.Balance(ctx, 501)
			if e != nil || bal.Available != 890 || bal.Reserved != 110 {
				t.Fatal("original reserve changed", bal, e)
			}
		})
	}
}

func TestTransferAccountSwitchSameApplicationCannotQueryOrAcquireSend(t *testing.T) {
	s, g, _ := batchFixture(t)
	ctx := context.Background()
	old := &accountBoundProvider{account: "synthetic-mch-original"}
	old.unknown = true
	g.Providers[7] = old
	in := batchInputs(100)[0].Request
	v, e := g.Transfer(ctx, 501, 7, "bound-account", in)
	if e == nil || v.Status != "unknown" {
		t.Fatal(v, e)
	}
	changed := &accountBoundProvider{account: "synthetic-mch-reconfigured"}
	g.Providers[7] = changed
	if _, e = g.QueryTransfer(ctx, 501, "bound-account"); !errors.Is(e, ErrConflict) {
		t.Fatal("same AppID changed payer account still queried", e)
	}
	if changed.query.Load() != 0 {
		t.Fatal("wrong payer account touched upstream")
	}
	g.Providers[7] = old
	if _, e = g.QueryTransfer(ctx, 501, "bound-account"); e != nil {
		t.Fatal("original payer not recoverable", e)
	}
	g.Providers[7] = changed
	if _, e = g.TransferProof(ctx, 501, "bound-account"); !errors.Is(e, ErrConflict) {
		t.Fatal("receipt obtained under reconfigured payer", e)
	}
	if changed.proofApply.Load() != 0 || changed.proofQuery.Load() != 0 {
		t.Fatal("wrong payer proof IO")
	}
	var count int
	if e = s.pool.QueryRow(ctx, "SELECT count(*) FROM provider_transfer_accounts WHERE transfer_id=$1", v.ID).Scan(&count); e != nil || count != 1 {
		t.Fatal("missing immutable account proof", count, e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE provider_transfer_accounts SET account_hash=decode(repeat('00',32),'hex') WHERE transfer_id=$1", v.ID); e == nil {
		t.Fatal("payer binding mutable")
	}
}

func TestTransferAccountSwitchBlocksBatchAndSettlementBeforeClaim(t *testing.T) {
	for _, kind := range []string{"batch", "settlement"} {
		t.Run(kind, func(t *testing.T) {
			s, g, _ := batchFixture(t)
			ctx := context.Background()
			original := &accountBoundProvider{account: "original"}
			g.Providers[7] = original
			if kind == "batch" {
				b, e := g.PrepareTransferBatch(ctx, 501, "bound-batch", batchInputs(100), batchMerchantActor)
				if e != nil {
					t.Fatal(e)
				}
				changed := &accountBoundProvider{account: "changed"}
				g.Providers[7] = changed
				if _, e = g.ExecuteTransferBatch(ctx, 501, b.ID, 1, batchMerchantActor, nil); !errors.Is(e, ErrConflict) {
					t.Fatal("changed batch payer claimed", e)
				}
				current, e := s.TransferBatch(ctx, 501, b.ID)
				if e != nil || current.Summary.Unsent != 1 || changed.transfers.Load() != 0 {
					t.Fatal(current, e)
				}
			} else {
				if _, e := s.SetSettlementRecipient(ctx, 501, SettlementRecipientInput{Type: "wxpay", Account: "synthetic-openid", Name: "Synthetic Name", AppID: original.ApplicationID()}, nil, SettlementRecipientActor{Role: "merchant", ID: 501, Name: "merchant"}); e != nil {
					t.Fatal(e)
				}
				v, e := s.RequestFunds(ctx, "settlements", 501, 0, "bound-settlement", 100, "merchant:501")
				if e != nil {
					t.Fatal(e)
				}
				if _, e = s.ReviewFundsRequest(ctx, v.ID, true, "admin:1"); e != nil {
					t.Fatal(e)
				}
				if _, e = g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil); e != nil {
					t.Fatal(e)
				}
				changed := &accountBoundProvider{account: "changed"}
				g.Providers[7] = changed
				if _, e = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil); !errors.Is(e, ErrConflict) {
					t.Fatal("changed settlement payer claimed", e)
				}
				current, e := s.SettlementPayout(ctx, v.ID)
				if e != nil || current.SendState != "unsent" || changed.transfers.Load() != 0 {
					t.Fatal(current, e)
				}
			}
		})
	}
}
