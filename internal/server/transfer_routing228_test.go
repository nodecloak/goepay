package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	adapter "goepay/internal/adapters/payment"
	"goepay/internal/compat"
	"goepay/internal/payment"
)

func routingActor228(t *testing.T, f channel168Fixture) payment.ParentChannelActor {
	t.Helper()
	session, ok := f.s.Sessions.Get(f.token, "admin")
	if !ok {
		t.Fatal("session")
	}
	return payment.ParentChannelActor{User: session.User, Token: f.token, CSRF: f.csrf}
}

func setRouting228(t *testing.T, f channel168Fixture, kind, route string) payment.TransferRoutingState228 {
	t.Helper()
	state, err := f.s.Store.TransferRouting228(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	state.Routes[kind] = route
	state, err = f.s.Store.SaveTransferRouting228(t.Context(), routingActor228(t, f), state.Fingerprint, state.Routes)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestTransferRouting228FirstIntentGateAndOriginalUnknownAfterManualSwitch(t *testing.T) {
	setup := parent180Outgoing(t)
	f := setup.f
	ctx := t.Context()
	setRouting228(t, f, "wxpay", "channel:"+setup.id)
	view, id, state, err := f.s.resolveTransferRouting228(ctx, f.p, 1, "routing228-original", compat.Params{"type": "wxpay"})
	if err != nil {
		t.Fatal(err)
	}
	req := adapter.TransferRequest{Type: "wxpay", Account: "synthetic228-recipient", Amount: 100, Remark: "synthetic routing228"}
	first, err := view.ProviderFlow.Transfer(transferRoutingContext228(ctx, state), 1, id, "routing228-original", req)
	if err == nil || first.ID == 0 || setup.transfers.Load() != 1 {
		t.Fatal("fake unknown send missing", first.ID, err)
	}
	setRouting228(t, f, "wxpay", "manual")
	// A request selected before the edit must not reserve or consume consent.
	if _, err = view.ProviderFlow.Transfer(transferRoutingContext228(ctx, state), 1, id, "routing228-stale", req); !errors.Is(err, payment.ErrConflict) {
		t.Fatal("stale admission", err)
	}
	if _, err = view.ProviderFlow.Transfer(ctx, 1, id, "routing228-direct", req); !errors.Is(err, payment.ErrState) {
		t.Fatal("direct disabled/manual bypass", err)
	}
	var count int
	if err = f.p.QueryRow(ctx, `SELECT count(*) FROM transfers WHERE merchant_id=1 AND transfer_no IN ('routing228-stale','routing228-direct')`).Scan(&count); err != nil || count != 0 {
		t.Fatal("rejected request reserved funds", count, err)
	}
	// Existing intent ignores the new default, retains request hash checking,
	// and performs no second send, even after original account rotation.
	parent180RotateOutgoing(t, setup)
	original, oldID, oldState, err := f.s.resolveTransferRouting228(ctx, f.p, 1, "routing228-original", compat.Params{"type": "wxpay"})
	if err != nil || oldID != id || oldState.Fingerprint != "" {
		t.Fatal("lost original route", err)
	}
	if _, err = original.ProviderFlow.Transfer(ctx, 1, oldID, "routing228-original", req); err != nil {
		t.Fatal("existing unknown retry", err)
	}
	if setup.transfers.Load() != 1 {
		t.Fatal("original resent")
	}
	req.Amount++
	if _, err = original.ProviderFlow.Transfer(ctx, 1, oldID, "routing228-original", req); !errors.Is(err, payment.ErrConflict) {
		t.Fatal("changed retry accepted", err)
	}
}

func TestTransferRouting228ActualHTTPVersionedSettingsAreReadOnlyUntilExplicitSave(t *testing.T) {
	setup := parent180Outgoing(t)
	f := setup.f
	call := func(method, body, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/console/v1/admin/settings/transfer-routes", strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	w := call("GET", "", "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal(w.Code, w.Body.String())
	}
	var out struct {
		Data struct {
			State    payment.TransferRoutingState228
			Channels []payment.TransferRoutingChoice228
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, choice := range out.Data.Channels {
		found = found || choice.ID == setup.id
	}
	if !found {
		t.Fatal("managed catalogue missing")
	}
	out.Data.State.Routes["wxpay"] = "channel:" + setup.id
	body, _ := json.Marshal(map[string]any{"expected_fingerprint": out.Data.State.Fingerprint, "routes": out.Data.State.Routes})
	if w = call("PATCH", string(body), ""); w.Code != 403 {
		t.Fatal("CSRF bypass", w.Code)
	}
	if w = call("PATCH", string(body), f.csrf); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = call("PATCH", string(body), f.csrf); w.Code != 409 {
		t.Fatal("stale overwrite", w.Code)
	}
	if setup.transfers.Load() != 0 || setup.transferQueries.Load() != 0 {
		t.Fatal("settings sent upstream request")
	}
}

func TestTransferRouting228FundsPreviewUsesSameAuthorizedManagedDefault(t *testing.T) {
	setup := parent180Outgoing(t)
	f := setup.f
	state := setRouting228(t, f, "wxpay", "channel:"+setup.id)
	raw := json.RawMessage(`{"merchant_id":"1","external_no":"routing228-consent","type":"wxpay","account":"synthetic228-recipient","amount":"1.00","remark":"synthetic consent"}`)
	_, summary, err := f.s.ResolveFundsAction(t.Context(), f.p, "/console/v1/admin/transfers", raw)
	if err != nil || summary["channel_id"] != setup.id {
		t.Fatal("preview diverged from execution", summary, err)
	}
	setRouting228(t, f, "wxpay", "disabled")
	if _, _, err = f.s.ResolveFundsAction(payment.WithTransferRoutingSelection228(t.Context(), state.Fingerprint), f.p, "/console/v1/admin/transfers", raw); !errors.Is(err, payment.ErrConflict) {
		t.Fatal("consent switched selected route", err)
	}
	if setup.transfers.Load() != 0 {
		t.Fatal("preview sent payment")
	}
}

func TestTransferRouting228LegacyOffBlocksDirectBatchAndRedpackButKeepsPreparedBatch(t *testing.T) {
	setup := parent180Outgoing(t)
	f := setup.f
	ctx := t.Context()
	id, _ := strconv.ParseInt(setup.id, 10, 64)
	actor := payment.TransferBatchActor{Role: "admin", Class: "super", Name: "admin168", ID: 168}
	req := adapter.TransferRequest{Type: "wxpay", Account: "synthetic228-recipient", Amount: 100, Remark: "synthetic batch228"}
	inputs := []payment.TransferBatchInput{{ChannelID: id, Request: req}}
	prepared, err := f.s.ProviderFlow.PrepareTransferBatch(ctx, 1, "routing228-prepared", inputs, actor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.p.Exec(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('settings','{"key":"transfer_wxpay","value":"0"}')`); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.ProviderFlow.Transfer(ctx, 1, id, "routing228-legacy-off", req); !errors.Is(err, payment.ErrState) {
		t.Fatal("direct rail bypass", err)
	}
	if _, err = f.s.ProviderFlow.PrepareTransferBatch(ctx, 1, "routing228-batch-off", inputs, actor); !errors.Is(err, payment.ErrState) {
		t.Fatal("batch rail bypass", err)
	}
	if _, err = f.s.Store.CreateRedpack(ctx, payment.RedpackInput{MerchantID: 1, ChannelID: id, ActorID: 168, OutBizNo: "2026110200000000228", Type: "wxpay", Remark: "synthetic red228", Role: "admin", Amount: 100}); !errors.Is(err, payment.ErrState) {
		t.Fatal("redpack rail bypass", err)
	}
	if setup.transfers.Load() != 0 {
		t.Fatal("disabled new intent sent")
	}
	setRouting228(t, f, "wxpay", "manual")
	if _, err = f.s.ProviderFlow.ExecuteTransferBatch(ctx, 1, prepared.ID, 1, actor, nil); err != nil || setup.transfers.Load() != 1 {
		t.Fatal("original prepared batch lost", err)
	}
}

func TestTransferRouting228ActualHTTPManagedBatchPrepareExecuteAndCancel(t *testing.T) {
	setup := parent180Outgoing(t)
	f := setup.f
	ctx := t.Context()
	setRouting228(t, f, "wxpay", "channel:"+setup.id)
	call := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/console/v1/admin/transfer-batches"+path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "goepay_admin", Value: f.token})
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", f.csrf)
		w := httptest.NewRecorder()
		f.s.Handler().ServeHTTP(w, r)
		return w
	}
	prepare := func(no string) string {
		w := call("", `{"merchant_id":"1","external_no":"`+no+`","items":[{"type":"wxpay","account":"synthetic228-recipient","amount":"1.00","remark":"synthetic batch HTTP"}]}`)
		var out struct{ Data payment.TransferBatch }
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatal(w.Code, w.Body.String())
		}
		if len(out.Data.Items) != 1 || strconv.FormatInt(out.Data.Items[0].Transfer.ChannelID, 10) != setup.id {
			t.Fatal("batch default lost")
		}
		return strconv.FormatInt(out.Data.ID, 10)
	}
	a, b := prepare("routing228-http-batch-a"), prepare("routing228-http-batch-b")
	setRouting228(t, f, "wxpay", "manual")
	for _, target := range []struct{ path, body string }{{"/" + a + "/execute", `{"limit":1}`}, {"/" + b + "/cancel-unsent", `{}`}} {
		if _, _, err := f.s.ResolveFundsAction(ctx, f.p, "/console/v1/admin/transfer-batches"+target.path, json.RawMessage(target.body)); err != nil {
			t.Fatal("managed consent blocked", err)
		}
		if w := call(target.path, target.body); w.Code != 200 {
			t.Fatal("managed batch HTTP blocked", w.Code, w.Body.String())
		}
	}
	if setup.transfers.Load() != 1 || setup.bad.Load() != 0 {
		t.Fatal("batch sends", setup.transfers.Load(), setup.bad.Load())
	}
}

func TestTransferRouting228RestoresAuthorizedManagedDefaultWithoutSending(t *testing.T) {
	setup := parent180Outgoing(t)
	f := setup.f
	session, ok := f.s.Sessions.Get(f.token, "admin")
	if !ok {
		t.Fatal("session")
	}
	actor := payment.ParentChannelActor{User: session.User, Token: f.token, CSRF: f.csrf}
	state, err := f.s.Store.TransferRouting228(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	state.Routes["wxpay"] = "channel:" + setup.id
	saved, err := f.s.Store.SaveTransferRouting228(t.Context(), actor, state.Fingerprint, state.Routes)
	if err != nil {
		t.Fatal(err)
	}
	view, id, captured, err := f.s.resolveTransferRouting228(t.Context(), f.p, 1, "route228-first", compat.Params{"type": "wxpay"})
	want, _ := strconv.ParseInt(setup.id, 10, 64)
	if err != nil || id != want || view.ProviderFlow.Providers[id] == nil || captured.Fingerprint != saved.Fingerprint || setup.transfers.Load() != 0 {
		t.Fatal("managed default", id, err)
	}
	// Configuration never creates permission for another merchant.
	if _, _, _, err = f.s.resolveTransferRouting228(t.Context(), f.p, 2, "route228-other", compat.Params{"type": "wxpay"}); err == nil {
		t.Fatal("default expanded merchant permission")
	}
	// A changed route invalidates a captured request before consent/reservation.
	state = saved
	state.Routes["wxpay"] = "manual"
	if _, err = f.s.Store.SaveTransferRouting228(t.Context(), actor, saved.Fingerprint, state.Routes); err != nil {
		t.Fatal(err)
	}
	ctx := payment.WithTransferRoutingSelection228(t.Context(), saved.Fingerprint)
	if _, _, _, err = f.s.resolveTransferRouting228(ctx, f.p, 1, "route228-stale", compat.Params{"type": "wxpay"}); !errors.Is(err, payment.ErrConflict) {
		t.Fatal("stale route accepted", err)
	}
	if setup.transfers.Load() != 0 || setup.transferQueries.Load() != 0 {
		t.Fatal("configuration sent upstream traffic")
	}
}
