package server

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"strconv"
	"testing"
)

func TestParentChannel180ActualTLSBankTransferHasIndependentProductAndOriginalAccount(t *testing.T) {
	setup := parent180Outgoing(t)
	f := setup.f
	ctx := t.Context()
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	bankKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "account": map[string]any{"config": map[string]any{"enable_bank_transfer": map[string]string{"operation": "replace", "value": "true"}, "bank_public_key": map[string]string{"operation": "replace", "value": parent180Public(bankKey)}}}})
	w := parent180HTTP(f, "PATCH", "/"+setup.id, string(body))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var state struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &state)
	setup.state = state.Data
	request := adapter.TransferRequest{Type: "bank", Account: "6222020000000000000", Name: "Synthetic Receiver", BankCode: "1002", Amount: 100, Remark: "synthetic bank180"}
	transfer, e := f.s.ProviderFlow.Transfer(ctx, 1, id, "parent180-bank-A", request)
	if e != nil || transfer.Status != "pending" || setup.transfers.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("actual bank transfer missing", setup.transfers.Load(), setup.bad.Load(), e)
	}
	var product string
	if e = f.p.QueryRow(ctx, `SELECT facts->>'Product' FROM runtime_parent_transfer_sources WHERE transfer_id=$1`, transfer.ID).Scan(&product); e != nil || product != adapter.BankProductWechat {
		t.Fatal("bank impersonated payment product", product, e)
	}
	parent180RotateOutgoing(t, setup)
	result, e := f.s.ProviderFlow.QueryTransfer(ctx, 1, "parent180-bank-A")
	if e != nil || result.Status != "succeeded" || setup.transferQueries.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("bank query changed original key/certificate", setup.transferQueries.Load(), setup.bad.Load(), e)
	}
}

func TestParentChannel180ActualTLSTransferUnknownKeepsOriginalAccountAfterRotation(t *testing.T) {
	setup := parent180Outgoing(t)
	f := setup.f
	ctx := t.Context()
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	request := adapter.TransferRequest{Type: "wxpay", Account: "synthetic180-recipient", Amount: 100, Remark: "synthetic parent180"}
	first, e := f.s.ProviderFlow.Transfer(ctx, 1, id, "parent180-transfer-A", request)
	if e == nil || first.ID <= 0 || setup.transfers.Load() != 1 {
		t.Fatalf("new actual parent transfer did not reach original adapter once: id=%d sends=%d err=%v", first.ID, setup.transfers.Load(), e)
	}
	var sources int
	if e = f.p.QueryRow(ctx, `SELECT count(*) FROM runtime_parent_transfer_sources WHERE transfer_id=$1`, first.ID).Scan(&sources); e != nil || sources != 1 {
		t.Fatal("transfer source not atomic", sources, e)
	}
	parent180RotateOutgoing(t, setup)
	setup.expectedCreate.Store("B")
	if _, e = f.s.ProviderFlow.Transfer(ctx, 1, id, "parent180-transfer-B", request); e == nil || setup.transfers.Load() != 2 {
		t.Fatal("new transfer did not use current version", setup.transfers.Load(), e)
	}
	current, e := f.s.ProviderFlow.QueryTransfer(ctx, 1, "parent180-transfer-A")
	if e != nil || current.Status != "succeeded" || setup.transferQueries.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("original transfer query changed account", setup.transferQueries.Load(), setup.bad.Load(), e)
	}
	if _, e = f.s.ProviderFlow.Transfer(ctx, 1, id, "parent180-transfer-A", request); e != nil || setup.transfers.Load() != 2 {
		t.Fatal("unknown original was sent again", setup.transfers.Load(), e)
	}
}

func parent180Outgoing(t *testing.T) parent180Runtime {
	t.Helper()
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := t.Context()
	if _, e := f.p.Exec(ctx, `UPDATE merchants SET profile=profile||'{"transfer_api_enabled":true,"transfer_rate":"0"}'::jsonb WHERE id=1;INSERT INTO backoffice_records(resource,body) VALUES('settings','{"key":"transfer_enabled","value":"true"}')`); e != nil {
		t.Fatal(e)
	}
	fund, e := f.s.Store.CreateOrder(ctx, payment.CreateOrderInput{MerchantID: 1, OutTradeNo: "parent180-transfer-fund", Name: "synthetic reserve", Amount: 10000})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Store.MarkPaid(ctx, fund.TradeNo, "parent180-transfer-fixture"); e != nil {
		t.Fatal(e)
	}
	f.s.ProviderFlow.MerchantChannels = f.s.MerchantChannels
	if e = f.s.ProviderFlow.InstallParentAccountFactories(f.s.ProviderFlow.ParentFactories); e != nil {
		t.Fatal(e)
	}
	return setup
}
func parent180RotateOutgoing(t *testing.T, setup parent180Runtime) {
	t.Helper()
	f := setup.f
	change, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "account": map[string]any{"config": map[string]any{"appkey": map[string]string{"operation": "replace", "value": setup.keyB}}, "certificates": map[string]any{"sslcert_path": map[string]string{"operation": "replace", "value": setup.certB}, "sslkey_path": map[string]string{"operation": "replace", "value": setup.pemB}}}})
	if w := parent180HTTP(f, "PATCH", "/"+setup.id, string(change)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestParentChannel180ActualTLSPreparedBatchUsesOriginalVersionAfterRotation(t *testing.T) {
	setup := parent180Outgoing(t)
	f := setup.f
	ctx := t.Context()
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	actor := payment.TransferBatchActor{Role: "admin", Class: "super", Name: "admin168", ID: 168}
	inputs := []payment.TransferBatchInput{{ChannelID: id, Request: adapter.TransferRequest{Type: "wxpay", Account: "synthetic180-recipient", Amount: 100, Remark: "synthetic batch180"}}}
	b, e := f.s.ProviderFlow.PrepareTransferBatch(ctx, 1, "parent180-batch-original", inputs, actor)
	if e != nil || len(b.Items) != 1 {
		t.Fatal("new parent batch unavailable", e)
	}
	parent180RotateOutgoing(t, setup)
	if _, e = f.s.ProviderFlow.ExecuteTransferBatch(ctx, 1, b.ID, 1, actor, nil); e != nil || setup.transfers.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("prepared batch changed its original account", setup.transfers.Load(), setup.bad.Load(), e)
	}
	queried, e := f.s.ProviderFlow.QueryTransferBatch(ctx, 1, b.ID, actor)
	if e != nil || !queried.AllSucceeded || setup.transferQueries.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("batch recovery did not query original account", setup.transferQueries.Load(), e)
	}
}

func TestParentChannel180ActualTLSDisabledParentCannotClaimPreparedBatch(t *testing.T) {
	setup := parent180Outgoing(t)
	f := setup.f
	ctx := t.Context()
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	actor := payment.TransferBatchActor{Role: "admin", Class: "super", Name: "admin168", ID: 168}
	b, e := f.s.ProviderFlow.PrepareTransferBatch(ctx, 1, "parent180-batch-disabled", []payment.TransferBatchInput{{ChannelID: id, Request: adapter.TransferRequest{Type: "wxpay", Account: "synthetic180-recipient", Amount: 100, Remark: "synthetic batch180"}}}, actor)
	if e != nil {
		t.Fatal(e)
	}
	change, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "enabled": false})
	if w := parent180HTTP(f, "PATCH", "/"+setup.id, string(change)); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if _, e = f.s.ProviderFlow.ExecuteTransferBatch(ctx, 1, b.ID, 1, actor, nil); e == nil || setup.transfers.Load() != 0 {
		t.Fatal("disabled prepared parent still sent", setup.transfers.Load(), e)
	}
	current, e := f.s.Store.TransferBatch(ctx, 1, b.ID)
	if e != nil || current.Items[0].SendState != "unsent" {
		t.Fatal("off parent consumed original send opportunity", e)
	}
}

func TestParentChannel180ActualTLSTransferSourceLossNeverFallsBack(t *testing.T) {
	for _, loss := range []string{"row", "table", "table-and-registration"} {
		t.Run(loss, func(t *testing.T) {
			setup := parent180Outgoing(t)
			f := setup.f
			ctx := t.Context()
			id, _ := strconv.ParseInt(setup.id, 10, 64)
			if _, e := f.s.ProviderFlow.Transfer(ctx, 1, id, "parent180-source-loss", adapter.TransferRequest{Type: "wxpay", Account: "synthetic180-recipient", Amount: 100, Remark: "synthetic source180"}); e == nil || setup.transfers.Load() != 1 {
				t.Fatal("fixture did not send one unknown", e)
			}
			var e error
			switch loss {
			case "row":
				_, e = f.p.Exec(ctx, `ALTER TABLE runtime_parent_transfer_sources DISABLE TRIGGER runtime_parent_transfer_source_immutable;DELETE FROM runtime_parent_transfer_sources;ALTER TABLE runtime_parent_transfer_sources ENABLE TRIGGER runtime_parent_transfer_source_immutable`)
			case "table":
				_, e = f.p.Exec(ctx, `DROP TABLE runtime_parent_transfer_sources`)
			case "table-and-registration":
				_, e = f.p.Exec(ctx, `DROP TABLE runtime_parent_transfer_sources;DELETE FROM schema_migrations WHERE name='000103_runtime_parent_channel_accounts.up.sql'`)
			}
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.s.ProviderFlow.QueryTransfer(ctx, 1, "parent180-source-loss"); !errors.Is(e, payment.ErrConflict) || setup.transferQueries.Load() != 0 {
				t.Fatal("lost transfer source reached fallback account", setup.transferQueries.Load(), e)
			}
		})
	}
}

func TestParentChannel180ActualTLSHistoricalTransferCannotBorrowAdoptedAccount(t *testing.T) {
	setup := parent180Outgoing(t)
	f := setup.f
	ctx := t.Context()
	legacy := *f.s.ProviderFlow
	legacy.ParentFactories = nil
	if _, e := legacy.Transfer(ctx, 1, 8, "parent180-preversion-transfer", adapter.TransferRequest{Type: "wxpay", Account: "synthetic180-recipient", Amount: 100, Remark: "synthetic legacy180"}); e == nil || setup.transfers.Load() != 1 {
		t.Fatal("legacy unknown fixture", e)
	}
	template, e := f.s.ProviderFlow.ParentChannelTemplate(8, f.s.MerchantChannels)
	if e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"expected_source_fingerprint": template.Public().Fingerprint})
	if w := parent180HTTP(f, "POST", "/8/adopt", string(body)); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, e = f.s.ProviderFlow.QueryTransfer(ctx, 1, "parent180-preversion-transfer"); !errors.Is(e, payment.ErrConflict) || setup.transferQueries.Load() != 0 {
		t.Fatal("new snapshot invented old transfer credentials", setup.transferQueries.Load(), e)
	}
}

func TestParentChannel180ActualTLSPreparedSettlementKeepsOriginalAccount(t *testing.T) {
	setup := parent180Outgoing(t)
	f := setup.f
	ctx := t.Context()
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	flow, _, e := f.s.ProviderFlow.RuntimeParentChannels(ctx, 1, f.s.MerchantChannels)
	if e != nil {
		t.Fatal(e)
	}
	app := flow.Providers[id].(adapter.ApplicationIdentity).ApplicationID()
	cfg, e := f.s.Store.SetSettlementRecipient(ctx, 1, payment.SettlementRecipientInput{Type: "wxpay", AppID: app, Account: "synthetic180-recipient", Name: "Synthetic Recipient"}, nil, payment.SettlementRecipientActor{Role: "merchant", ID: 1, Name: "merchant180"})
	if e != nil {
		t.Fatal("recipient fixture", e)
	}
	request, e := f.s.Store.RequestFundsWithSettlementRecipient(ctx, "settlements", 1, 0, "parent180-settlement", 100, "merchant:1", nil, &cfg.ConfigID)
	if e != nil {
		t.Fatal("settlement fixture", e)
	}
	request, e = f.s.Store.ReviewFundsRequest(ctx, request.ID, true, "admin:168")
	if e != nil {
		t.Fatal(e)
	}
	actor := payment.TransferBatchActor{Role: "admin", Class: "super", Name: "admin168", ID: 168}
	p, e := f.s.ProviderFlow.PrepareSettlementPayout(ctx, request.ID, id, actor, nil)
	if e != nil {
		t.Fatal("new parent settlement unavailable", e)
	}
	if _, e = f.s.ProviderFlow.ParentTransferIntent(ctx, p.Transfer); e != nil {
		t.Fatal("prepared source unreadable before rotation", e)
	}
	parent180RotateOutgoing(t, setup)
	if _, e = f.s.ProviderFlow.ExecuteSettlementPayout(ctx, request.ID, actor, nil); e == nil || setup.transfers.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("prepared settlement changed original version", setup.transfers.Load(), setup.bad.Load(), e)
	}
	got, e := f.s.ProviderFlow.QuerySettlementPayout(ctx, request.ID, actor)
	if e != nil || got.Transfer.Status != "succeeded" || got.TransferID != p.TransferID || setup.transferQueries.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("settlement query changed account", setup.transferQueries.Load(), e)
	}
}
