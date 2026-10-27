package server

import (
	"encoding/json"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"strconv"
	"testing"
)

func TestParentChannel180ActualTLSSharingReceiverRestoresItsOriginalAccount(t *testing.T) {
	setup, receiver := parent180Sharing(t)
	f := setup.f
	ctx := t.Context()
	actor := payment.TransferBatchActor{Role: "admin", Class: "super", Name: "admin168", ID: 168}
	parent180RotateOutgoing(t, setup)
	if _, e := f.s.ProviderFlow.BindChannelSharingReceiver(ctx, 1, receiver.ID, "parent180-receiver-bind", actor); e != nil || setup.sharingReceiverCalls.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("receiver changed its original key/cert", setup.sharingReceiverCalls.Load(), setup.bad.Load(), e)
	}
}

func TestParentChannel180ActualTLSReceiverSourceTamperOrLossNeverReachesProvider(t *testing.T) {
	for _, loss := range []string{"row", "table", "table-and-registration", "audit", "case-alias"} {
		t.Run(loss, func(t *testing.T) {
			setup, receiver := parent180Sharing(t)
			f := setup.f
			ctx := t.Context()
			var sql string
			switch loss {
			case "row":
				sql = `ALTER TABLE runtime_parent_receiver_sources DISABLE TRIGGER runtime_parent_receiver_source_immutable;DELETE FROM runtime_parent_receiver_sources;ALTER TABLE runtime_parent_receiver_sources ENABLE TRIGGER runtime_parent_receiver_source_immutable`
			case "table":
				sql = `DROP TABLE runtime_parent_receiver_sources`
			case "table-and-registration":
				sql = `DROP TABLE runtime_parent_receiver_sources;DELETE FROM schema_migrations WHERE name='000103_runtime_parent_channel_accounts.up.sql'`
			case "audit":
				sql = `ALTER TABLE backoffice_audit DISABLE TRIGGER backoffice_audit_immutable;DELETE FROM backoffice_audit WHERE resource='runtime-parent-receiver-intents';ALTER TABLE backoffice_audit ENABLE TRIGGER backoffice_audit_immutable`
			case "case-alias":
				sql = `ALTER TABLE runtime_parent_receiver_sources DISABLE TRIGGER runtime_parent_receiver_source_immutable;UPDATE runtime_parent_receiver_sources SET facts=facts||jsonb_build_object('merchant',facts->'Merchant');ALTER TABLE runtime_parent_receiver_sources ENABLE TRIGGER runtime_parent_receiver_source_immutable`
			}
			if _, e := f.p.Exec(ctx, sql); e != nil {
				t.Fatal(e)
			}
			actor := payment.TransferBatchActor{Role: "admin", Class: "super", Name: "admin168", ID: 168}
			if _, e := f.s.ProviderFlow.BindChannelSharingReceiver(ctx, 1, receiver.ID, "parent180-missing-receiver", actor); !errors.Is(e, payment.ErrConflict) || setup.sharingReceiverCalls.Load() != 0 {
				t.Fatal("lost receiver source borrowed a current key", setup.sharingReceiverCalls.Load(), e)
			}
		})
	}
}

func parent180Sharing(t *testing.T) (parent180Runtime, payment.ChannelSharingReceiverView) {
	t.Helper()
	setup := parent180RuntimeReady(t)
	f := setup.f
	ctx := t.Context()
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	f.s.ProviderFlow.MerchantChannels = f.s.MerchantChannels
	if e := f.s.ProviderFlow.InstallParentAccountFactories(f.s.ProviderFlow.ParentFactories); e != nil {
		t.Fatal(e)
	}
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "account": map[string]any{"config": map[string]any{"enable_channel_sharing": map[string]string{"operation": "replace", "value": "true"}}}})
	w := parent180HTTP(f, "PATCH", "/"+setup.id, string(body))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var state struct{ Data map[string]any }
	json.Unmarshal(w.Body.Bytes(), &state)
	setup.state = state.Data
	current, _, e := f.s.ProviderFlow.RuntimeParentChannels(ctx, 1, f.s.MerchantChannels)
	if e != nil {
		t.Fatal(e)
	}
	actor := payment.TransferBatchActor{Role: "admin", Class: "super", Name: "admin168", ID: 168}
	receiver, e := current.CreateChannelSharingReceiver(ctx, 1, id, "parent180-receiver", adapter.SharingReceiver{Type: "MERCHANT_ID", Account: "receiver180", Name: "Synthetic Receiver", RelationType: "SUPPLIER"}, actor)
	if e != nil {
		t.Fatal(e)
	}
	return setup, receiver
}

func TestParentChannel180ActualTLSSharingOrderQueriesOriginalPaymentVersion(t *testing.T) {
	setup, _, order := parent180PaidSharing(t)
	f := setup.f
	ctx := t.Context()
	actor := payment.TransferBatchActor{Role: "admin", Class: "super", Name: "admin168", ID: 168}
	parent180RotateOutgoing(t, setup)
	if _, e := f.s.ProviderFlow.SubmitChannelSharing(ctx, 1, order.TradeNo, "parent180-original-sharing", actor); e != nil || setup.sharingSends.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("sharing changed original payment account", setup.sharingSends.Load(), setup.bad.Load(), e)
	}
	result, e := f.s.ProviderFlow.QueryChannelSharing(ctx, 1, "parent180-original-sharing", actor)
	if e != nil || !result.Completed || !result.FundsReleased || setup.sharingQueries.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("sharing query changed original account", setup.sharingQueries.Load(), setup.bad.Load(), e)
	}
}

func parent180PaidSharing(t *testing.T) (parent180Runtime, payment.ChannelSharingReceiverView, payment.Order) {
	t.Helper()
	setup, receiver := parent180Sharing(t)
	f := setup.f
	ctx := t.Context()
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	actor := payment.TransferBatchActor{Role: "admin", Class: "super", Name: "admin168", ID: 168}
	if _, e := f.s.ProviderFlow.BindChannelSharingReceiver(ctx, 1, receiver.ID, "parent180-bind-before-pay", actor); e != nil {
		t.Fatal(e)
	}
	current, _, e := f.s.ProviderFlow.RuntimeParentChannels(ctx, 1, f.s.MerchantChannels)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = current.SetChannelSharingRule(ctx, 1, id, payment.ChannelSharingRuleInput{Enabled: true, Methods: []string{"native"}, Items: []payment.ChannelSharingRuleItem{{ReceiverID: receiver.ID, Mode: "rate", RateBPS: 1000}}}, 0, actor); e != nil {
		t.Fatal(e)
	}
	if out := parent180WxPay(t, f, setup.id, "parent180-sharing-payment"); out["code"] != float64(1) {
		t.Fatal(out)
	}
	order, e := f.s.Store.Order(ctx, 1, "parent180-sharing-payment")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.s.ProviderFlow.Query(ctx, 1, order.TradeNo); e != nil {
		t.Fatal(e)
	}
	return setup, receiver, order
}

func TestParentChannel180ActualTLSSharingDisabledBeforeClaimCannotConsumeConfirmation(t *testing.T) {
	setup, _, order := parent180PaidSharing(t)
	f := setup.f
	ctx := t.Context()
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": setup.state["fingerprint"], "enabled": false})
	w := parent180HTTP(f, "PATCH", "/"+setup.id, string(body))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	actor := payment.TransferBatchActor{Role: "admin", Class: "super", Name: "admin168", ID: 168}
	if _, e := f.s.ProviderFlow.SubmitChannelSharing(ctx, 1, order.TradeNo, "parent180-disabled-sharing", actor); e == nil || setup.sharingSends.Load() != 0 {
		t.Fatal("disabled parent sharing sent", setup.sharingSends.Load(), e)
	}
	var n int
	if e := f.p.QueryRow(ctx, "SELECT count(*) FROM channel_sharing_orders").Scan(&n); e != nil || n != 0 {
		t.Fatal("disabled claim left financial intent", n, e)
	}
}

func TestParentChannel180ActualTLSSharingReturnRestoresOriginalPaymentAccount(t *testing.T) {
	setup, receiver, order := parent180PaidSharing(t)
	f := setup.f
	ctx := t.Context()
	actor := payment.TransferBatchActor{Role: "admin", Class: "super", Name: "admin168", ID: 168}
	if _, e := f.s.ProviderFlow.SubmitChannelSharing(ctx, 1, order.TradeNo, "parent180-return-sharing", actor); e != nil {
		t.Fatal(e)
	}
	if _, e := f.s.ProviderFlow.QueryChannelSharing(ctx, 1, "parent180-return-sharing", actor); e != nil {
		t.Fatal(e)
	}
	parent180RotateOutgoing(t, setup)
	v, e := f.s.ProviderFlow.SubmitChannelSharingReturn(ctx, 1, "parent180-return-sharing", receiver.ID, "parent180-original-return", 100, actor)
	if e != nil || v.State != "pending" || setup.sharingReturns.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("return changed original account", setup.sharingReturns.Load(), setup.bad.Load(), e)
	}
	v, e = f.s.ProviderFlow.QueryChannelSharingReturn(ctx, 1, "parent180-original-return", actor)
	if e != nil || v.State != "succeeded" || setup.sharingReturnQueries.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("return query changed original account", setup.sharingReturnQueries.Load(), setup.bad.Load(), e)
	}
}

func TestParentChannel180ActualTLSSharingFinishRestoresOriginalPaymentAccount(t *testing.T) {
	setup, _, order := parent180PaidSharing(t)
	f := setup.f
	ctx := t.Context()
	actor := payment.TransferBatchActor{Role: "admin", Class: "super", Name: "admin168", ID: 168}
	parent180RotateOutgoing(t, setup)
	v, e := f.s.ProviderFlow.SubmitChannelSharingFinish(ctx, 1, order.TradeNo, "parent180-original-finish", actor)
	if e != nil || v.State != "pending" || setup.sharingFinishes.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("finish changed original account", setup.sharingFinishes.Load(), setup.bad.Load(), e)
	}
	v, e = f.s.ProviderFlow.QueryChannelSharingFinish(ctx, 1, "parent180-original-finish", actor)
	if e != nil || v.State != "succeeded" || !v.FundsReleased || setup.bad.Load() != 0 {
		t.Fatal("finish query changed original account", setup.bad.Load(), e)
	}
}
