package payment

import (
	"context"
	"fmt"
	adapter "goepay/internal/adapters/payment"
	"testing"
)

func TestChannelSharingFundingDatabaseRequiresOriginalVerifiedReceipt(t *testing.T) {
	s, g, _, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	g.CallbackBaseURL = "https://local.example.invalid"
	sharingPaymentRule(t, g, 1000)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "sharing-missing-receipt", Type: "wxpay", Amount: 1000, Name: "local item"})
	if e != nil {
		t.Fatal(e)
	}
	// Real TLS submission commits the original declared attempt. It does not
	// prove payment; this transaction deliberately omits its verified receipt.
	if _, e = g.Submit(ctx, o.TradeNo, 7, "native", "127.0.0.1", ""); e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	var locked int64
	if e = tx.QueryRow(ctx, "SELECT id FROM orders WHERE id=$1 FOR UPDATE", o.ID).Scan(&locked); e != nil {
		t.Fatal(e)
	}
	if e = post(ctx, tx, "payment:"+o.TradeNo, "unverified net credit must reject", 0, "clearing", o.MerchantID, "available", o.MerchantAmount); e != nil {
		t.Fatal(e)
	}
	var account, available int64
	if e = tx.QueryRow(ctx, "INSERT INTO accounts(owner_id,kind,scope_id) VALUES($1,'channel_sharing_reserve',$2) RETURNING id", o.MerchantID, o.ID).Scan(&account); e != nil {
		t.Fatal(e)
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM accounts WHERE owner_id=$1 AND kind='available' AND scope_id=0", o.MerchantID).Scan(&available); e != nil {
		t.Fatal(e)
	}
	key := fmt.Sprintf("channel-sharing:%d:reserve", o.ID)
	if e = postByAccountIDs(ctx, tx, key, "unverified sharing reserve must reject", available, account, o.MerchantAmount); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "INSERT INTO channel_sharing_fundings(order_id,account_id,amount,reserve_journal_id) SELECT $1,$2,$3,id FROM journals WHERE event_key=$4", o.ID, account, o.MerchantAmount, key); e != nil {
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE orders SET status='paid',paid_at=statement_timestamp() WHERE id=$1", o.ID); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e == nil {
		t.Fatal("database accepted paid sharing funding with balanced original journals but no verified payment receipt")
	}
	var journals, fundings, receipts int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM journals),(SELECT count(*) FROM channel_sharing_fundings),(SELECT count(*) FROM verified_payment_receipts)").Scan(&journals, &fundings, &receipts); e != nil || journals != 0 || fundings != 0 || receipts != 0 {
		t.Fatal("missing-receipt rejection did not roll back all financial evidence", journals, fundings, receipts, e)
	}
	current, e := s.OrderByTradeNo(ctx, o.TradeNo)
	if e != nil || current.Status != "pending" {
		t.Fatal(current, e)
	}
}

func TestChannelSharingDatabaseCannotAdoptAlreadyPaidHistoricalAttempt(t *testing.T) {
	for _, reset := range []bool{false, true} {
		t.Run(fmt.Sprintf("reset_mutable_status_%t", reset), func(t *testing.T) {
			testRejectHistoricalSharingDeclaration(t, reset)
		})
	}
}

func testRejectHistoricalSharingDeclaration(t *testing.T, resetStatus bool) {
	t.Helper()
	s, g, _, _ := sharingReceiverFlowFixture(t)
	ctx := context.Background()
	rule := sharingPaymentRule(t, g, 1000)
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 751, OutTradeNo: "sharing-historical-paid", Type: "wxpay", Amount: 1000, Name: "historical local item"})
	if e != nil {
		t.Fatal(e)
	}
	// Model an original pre-declaration-era record through the legacy internal
	// entry point. Current ProviderFlow deliberately cannot adopt such attempts.
	a, owner, e := s.BeginProviderAttempt(ctx, o.TradeNo, 7, "native")
	if e != nil || !owner {
		t.Fatal(a, owner, e)
	}
	if e = s.markPaid(ctx, o.TradeNo, "historical-paid", true, PaidReceipt{ChannelID: 7, ProviderTradeNo: "LOCAL_HISTORICAL_PAYMENT"}); e != nil {
		t.Fatal(e)
	}
	if resetStatus {
		// These order fields are mutable in the legacy schema. Original payment
		// journals and verified receipts remain the durable evidence of payment.
		if _, e = s.pool.Exec(ctx, "UPDATE orders SET status='created',paid_at=NULL WHERE id=$1", o.ID); e != nil {
			t.Fatal(e)
		}
	}
	scope, e := adapter.TransferAccountScope(g.Providers[7])
	if e != nil {
		t.Fatal(e)
	}
	account, e := s.providerAccountHash(scope)
	if e != nil {
		t.Fatal(e)
	}
	sharingAccount, e := s.sharingDigest("account", []byte(scope))
	if e != nil {
		t.Fatal(e)
	}
	d := sharingPaymentIntent{ChannelSharingPaymentView: ChannelSharingPaymentView{AttemptID: a.ID, OrderID: o.ID, MerchantID: o.MerchantID, ChannelID: 7, Method: "native", Provider: "wxpay", AppID: "local-app", Product: "wxpay_v3_direct", RuleID: rule.ID, Declared: true, GrossAmount: o.Amount, MerchantNet: o.MerchantAmount, RefundMode: "merchant_funded", AutoFinish: true, Items: []ChannelSharingPaymentItem{{ReceiverID: rule.Items[0].ReceiverID, Amount: 100}}}, AccountHash: sharingAccount}
	d.RequestHash, e = s.sharingPaymentFingerprint(d)
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	// Private SQL must not convert an already paid ordinary order into a
	// declared order after its original proceeds have become spendable.
	adoptErr := func() error {
		if _, e := tx.Exec(ctx, "INSERT INTO provider_attempt_accounts(attempt_id,account_hash) VALUES($1,$2)", a.ID, account); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `INSERT INTO provider_attempt_sharing(attempt_id,order_id,merchant_id,channel_id,method,provider,app_id,product,account_hash,request_hash,rule_id,declared,gross_amount,merchant_net,refund_mode,auto_finish,receiver_count) VALUES($1,$2,$3,7,'native','wxpay','local-app','wxpay_v3_direct',$4,$5,$6,true,$7,$8,'merchant_funded',true,1)`, a.ID, o.ID, o.MerchantID, d.AccountHash, d.RequestHash, rule.ID, o.Amount, o.MerchantAmount); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, "INSERT INTO provider_attempt_sharing_items(attempt_id,position,receiver_id,amount) VALUES($1,1,$2,100)", a.ID, rule.Items[0].ReceiverID); e != nil {
			return e
		}
		return tx.Commit(ctx)
	}()
	if adoptErr == nil {
		t.Fatal("database retrospectively declared a paid historical payment without reserving its original income")
	}
	tx.Rollback(ctx)
	var declarations, accountProofs int
	if e = s.pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM provider_attempt_sharing),(SELECT count(*) FROM provider_attempt_accounts)").Scan(&declarations, &accountProofs); e != nil || declarations != 0 || accountProofs != 0 {
		t.Fatal("historical adoption left partial intent/account proofs", declarations, accountProofs, e)
	}
	b, e := s.Balance(ctx, o.MerchantID)
	if e != nil || b.Available != o.MerchantAmount || b.Reserved != 0 {
		t.Fatal("rejected adoption changed historical ordinary funds", b, e)
	}
}
