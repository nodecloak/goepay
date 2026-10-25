package payment

import (
	"context"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"sync"
	"sync/atomic"
	"testing"
)

type settlementWxBankProvider struct {
	bankTestProvider
	writes atomic.Int32
}

func (p *settlementWxBankProvider) Transfer(ctx context.Context, r adapter.TransferRequest) (adapter.TransferResult, error) {
	p.writes.Add(1)
	return p.bankTestProvider.Transfer(ctx, r)
}

func (*settlementWxBankProvider) ApplicationID() string           { return "bank-local-app" }
func (*settlementWxBankProvider) TransferAccountIdentity() string { return "bank-local-mch" }
func (*settlementWxBankProvider) BankTransferProduct() string     { return adapter.BankProductWechat }

type settlementAliBankProvider struct{ alipayBankProvider }

func (*settlementAliBankProvider) ID() string                      { return "alipay" }
func (*settlementAliBankProvider) ApplicationID() string           { return "ali-bank-local-app" }
func (*settlementAliBankProvider) TransferAccountIdentity() string { return "ali-bank-local-app" }

func settlementBankFixture(t *testing.T, product string, feeBPS ...string) (*Store, ProviderFlow, LedgerRequest, func(string)) {
	t.Helper()
	s, g, _ := batchFixture(t)
	var state func(string)
	if product == adapter.BankProductWechat {
		p := &settlementWxBankProvider{}
		p.queryState.Store("SUCCESS")
		p.providerNo.Store("local-bank-proof")
		p.feeKnown.Store(true)
		p.fee.Store(2)
		g.Providers[7] = p
		state = func(mode string) {
			p.feeKnown.Store(mode != "unverified")
			if mode == "returned" || mode == "unverified" {
				p.queryState.Store("BANK_FAIL")
			} else if mode == "failed" {
				p.queryState.Store("FAILED")
			} else {
				p.queryState.Store("SUCCESS")
			}
		}
	} else {
		p := &settlementAliBankProvider{}
		p.mode.Store("success")
		g.Providers[7] = p
		state = func(mode string) {
			if mode == "returned" {
				p.mode.Store("refund")
			} else if mode == "unverified" {
				p.mode.Store("refund-missing")
			} else if mode == "failed" {
				p.mode.Store("fail")
			} else {
				p.mode.Store("success")
			}
		}
	}
	rate := "300"
	if len(feeBPS) > 0 {
		rate = feeBPS[0]
	}
	transferSetting(t, s, "settlement_fee_rate_bps", rate)
	ctx := context.Background()
	if _, e := s.SetSettlementRecipient(ctx, 501, SettlementRecipientInput{Type: "bank", Account: "6222020000000000", Name: "Synthetic Bank Recipient", BankCode: "1002"}, nil, SettlementRecipientActor{Role: "merchant", ID: 501, Name: "merchant"}); e != nil {
		t.Fatal(e)
	}
	v, e := s.RequestFunds(ctx, "settlements", 501, 0, "bank-settlement-request", 1000, "merchant:501")
	if e != nil {
		t.Fatal(e)
	}
	v, e = s.ReviewFundsRequest(ctx, v.ID, true, "admin:1")
	if e != nil {
		t.Fatal(e)
	}
	return s, g, v, state
}

func TestSettlementBankFirstReturnFailureAndZeroFeePreserveOriginalSource(t *testing.T) {
	for _, product := range []string{adapter.BankProductWechat, adapter.BankProductAlipay} {
		for _, scenario := range []string{"first-return", "failed", "zero-fee"} {
			t.Run(product+"/"+scenario, func(t *testing.T) {
				rate := "300"
				if scenario == "zero-fee" {
					rate = "0"
				}
				s, g, v, state := settlementBankFixture(t, product, rate)
				ctx := context.Background()
				if _, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil); e != nil {
					t.Fatal(e)
				}
				if _, e := g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil); e != nil {
					t.Fatal(e)
				}
				if scenario == "zero-fee" {
					p, e := g.QuerySettlementPayout(ctx, v.ID, settlementAdmin)
					if e != nil || p.State != "succeeded" || p.Funding.State != "paid" || p.Funding.ReservedAmount != 0 {
						t.Fatal(p, e)
					}
				}
				wantState, wantFunding := "reversed", "returned"
				if scenario == "failed" {
					state("failed")
					wantState = "failed"
					wantFunding = "released"
				} else {
					state("returned")
				}
				p, e := g.QuerySettlementPayout(ctx, v.ID, settlementAdmin)
				if e != nil || p.State != wantState || p.Funding.State != wantFunding {
					t.Fatal(p, e)
				}
				bal, e := s.Balance(ctx, 501)
				if e != nil || bal.Available != 1000 || bal.Reserved != 0 {
					t.Fatal(bal, e)
				}
				report, e := s.CheckLedger(ctx)
				if e != nil || report.InvalidSettlementFundings != 0 || report.InvalidAccounts != 0 || report.InvalidJournals != 0 {
					t.Fatal(report, e)
				}
			})
		}
	}
}

func TestSettlementBankEvidenceGuardRejectsEarlyFeeIncomeAndForgedReturn(t *testing.T) {
	s, g, v, _ := settlementBankFixture(t, adapter.BankProductWechat)
	ctx := context.Background()
	if _, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil); e != nil {
		t.Fatal(e)
	}
	if _, e := g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil); e != nil {
		t.Fatal(e)
	}
	p, e := g.QuerySettlementPayout(ctx, v.ID, settlementAdmin)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, "UPDATE bank_transfer_lifecycle SET state='reversed',return_observed=true,returned_principal=970 WHERE transfer_id=$1", p.TransferID); e == nil {
		t.Fatal("forged return committed without return journals")
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var fees int64
	if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=0 AND kind='fees' AND scope_id=0").Scan(&fees); e != nil {
		t.Fatal(e)
	}
	if e = postByAccountIDs(ctx, tx, "test:forged-bank-settlement-fee", "must not recognize held settlement fee", p.Funding.AccountID, fees, 30); e != nil {
		t.Fatal("test did not reach deferred guard", e)
	}
	if e = tx.Commit(ctx); e == nil {
		t.Fatal("held fee escaped original bank account")
	}
	bal, e := s.Balance(ctx, 501)
	if e != nil || bal.Available != 0 || bal.Reserved != 30 {
		t.Fatal(bal, e)
	}
}

func TestSettlementBankLostAcknowledgmentKeepsGrossAndNeverResends(t *testing.T) {
	for _, product := range []string{adapter.BankProductWechat, adapter.BankProductAlipay} {
		t.Run(product, func(t *testing.T) {
			s, g, v, _ := settlementBankFixture(t, product)
			ctx := context.Background()
			calls := func() int32 {
				if p, ok := g.Providers[7].(*settlementWxBankProvider); ok {
					return p.writes.Load()
				}
				return g.Providers[7].(*settlementAliBankProvider).writes.Load()
			}
			if p, ok := g.Providers[7].(*settlementWxBankProvider); ok {
				p.submitError = &adapter.UnknownOutcome{Operation: "local-bank-lost"}
			} else {
				g.Providers[7].(*settlementAliBankProvider).lost = true
			}
			if _, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil); e != nil {
				t.Fatal(e)
			}
			if calls() != 0 {
				t.Fatal("prepare touched provider")
			}
			p, e := g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil)
			var unknown *adapter.UnknownOutcome
			if !errors.As(e, &unknown) || p.SendState != "claimed" || p.State != "unknown" {
				t.Fatal(p, e)
			}
			for range 3 {
				if _, e = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil); e != nil {
					t.Fatal(e)
				}
			}
			bal, e := s.Balance(ctx, 501)
			if e != nil || bal.Available != 0 || bal.Reserved != 1000 || calls() != 1 {
				t.Fatal("unknown released or resent", bal, e, calls())
			}
			if product == adapter.BankProductAlipay {
				p, e = g.QuerySettlementPayout(ctx, v.ID, settlementAdmin)
				if !errors.Is(e, ErrBankReversalPending) || p.Funding.ReservedAmount != 1000 {
					t.Fatal("lost bank submission binding guessed from current config", p, e)
				}
			}
		})
	}
}

func TestSettlementBankUsesScopedGrossHoldsFeeAndReturnsExactlyOnce(t *testing.T) {
	for _, product := range []string{adapter.BankProductWechat, adapter.BankProductAlipay} {
		t.Run(product, func(t *testing.T) {
			s, g, v, state := settlementBankFixture(t, product)
			ctx := context.Background()
			out, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil)
			if e != nil {
				t.Fatal("bank settlement prepare", e)
			}
			if out.Transfer.Type != "bank" || out.Transfer.BankProduct != product || out.Transfer.Amount != 970 || out.Transfer.FeeAmount != 0 || out.Transfer.CostAmount != 970 {
				t.Fatal(out)
			}
			out, e = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil)
			if e != nil || out.SendState != "claimed" || out.State != "pending" {
				t.Fatal(out, e)
			}
			out, e = g.QuerySettlementPayout(ctx, v.ID, settlementAdmin)
			if e != nil || out.State != "succeeded" || out.Funding.State != "paid_fee_held" || out.Funding.ReservedAmount != 30 {
				t.Fatal("bank fee incorrectly recognized or source lost", out, e)
			}
			bal, e := s.Balance(ctx, 501)
			if e != nil || bal.Available != 0 || bal.Reserved != 30 {
				t.Fatal(bal, e)
			}
			var fees, pooled int64
			if e = s.pool.QueryRow(ctx, "SELECT COALESCE(sum(balance)FILTER(WHERE owner_id=0 AND kind='fees'),0),COALESCE(sum(balance)FILTER(WHERE owner_id=501 AND kind='reserved'),0) FROM accounts").Scan(&fees, &pooled); e != nil || fees != 0 || pooled != 0 {
				t.Fatal("used ordinary transfer fee/pool", fees, pooled, e)
			}
			state("unverified")
			out, e = g.QuerySettlementPayout(ctx, v.ID, settlementAdmin)
			if !errors.Is(e, ErrBankReversalPending) || out.State != "reversal_pending" || out.Funding.ReservedAmount != 30 {
				t.Fatal("unproven return released", out, e)
			}
			state("returned")
			var wg sync.WaitGroup
			for range 12 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					p, err := g.QuerySettlementPayout(ctx, v.ID, settlementAdmin)
					if err != nil || p.State != "reversed" || p.Funding.State != "returned" {
						t.Error(p, err)
					}
				}()
			}
			wg.Wait()
			bal, e = s.Balance(ctx, 501)
			if e != nil || bal.Available != 1000 || bal.Reserved != 0 {
				t.Fatal("return duplicated or fee lost", bal, e)
			}
			if _, e = g.ExecuteSettlementPayout(ctx, v.ID, settlementAdmin, nil); e != nil {
				t.Fatal("original terminal intent unreadable", e)
			}
			report, e := s.CheckLedger(ctx)
			if e != nil || report.InvalidSettlementFundings != 0 || report.InvalidJournals != 0 || report.InvalidAccounts != 0 {
				t.Fatal(report, e)
			}
		})
	}
}

func TestSettlementBankUnsentCancellationReleasesGrossAndNoProviderCall(t *testing.T) {
	for _, product := range []string{adapter.BankProductWechat, adapter.BankProductAlipay} {
		t.Run(product, func(t *testing.T) {
			s, g, v, _ := settlementBankFixture(t, product)
			ctx := context.Background()
			if _, e := g.PrepareSettlementPayout(ctx, v.ID, 7, settlementAdmin, nil); e != nil {
				t.Fatal(e)
			}
			out, e := g.CancelUnsentSettlementPayout(ctx, v.ID, settlementAdmin)
			if e != nil || out.State != "cancelled" || out.Transfer.BankLifecycle != "cancelled" || out.Funding.State != "released" {
				t.Fatal(out, e)
			}
			bal, e := s.Balance(ctx, 501)
			if e != nil || bal.Available != 1000 || bal.Reserved != 0 {
				t.Fatal(bal, e)
			}
		})
	}
}
