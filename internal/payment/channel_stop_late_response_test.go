package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestChannelStop76EarlierInFlightResponseCannotUndoExplicitLaterResume(t *testing.T) {
	s, g, _, _, _, first := autoReady73(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	plan := settlementPlanReady(t, s, 7396)
	if _, e := s.pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true}' WHERE id=7396`); e != nil {
		t.Fatal(e)
	}
	o, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 7396, OutTradeNo: "late-response76-credit", Amount: 2000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, o.TradeNo, "late-response76-local"); e != nil {
		t.Fatal(e)
	}
	guard := WithFundsActionGuard(ctx, func(context.Context, pgx.Tx, string, string) error { return nil })
	gen, e := s.AuthorizeSettlementPlan(guard, plan.ID, plan.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	run, e := s.CreateSettlementPlanRun(ctx, plan.ID, plan.Revision, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.GenerateSettlementPlanRun(ctx, run.ID); e != nil {
		t.Fatal(e)
	}
	view, e := s.SettlementPlanRun(ctx, run.ID, settlementPlanAdmin)
	if e != nil || len(view.Items) != 1 || view.Items[0].RequestID <= 0 {
		t.Fatal(view, e)
	}
	second := view.Items[0].RequestID
	g.MerchantChannels[7396] = []int64{7}
	if _, e = g.AuthorizeSettlementAuto(guard, plan.ID, plan.Revision, SettlementAutoInput{GenerationAuthorizationID: gen.ID, ValidFrom: gen.ValidFrom, ExpiresAt: gen.ExpiresAt, MaxItemGross: 1000, MaxRunGross: 1000, MaxDayGross: 1000, MaxDayItems: 1, CostPolicy: "provider_managed_excluded", Channels: []SettlementAutoChannel{{MerchantID: 7396, ChannelID: 7}}}, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	merchant, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	platform, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			return
		}
		var b map[string]any
		if json.Unmarshal([]byte(r.Form.Get("biz_content")), &b) != nil {
			t.Error("bad synthetic request")
			return
		}
		p, e := s.SettlementPayout(ctx, second)
		if e != nil || p.SendState != "claimed" {
			t.Error("second response was not originally in flight", p, e)
			return
		}
		if b["out_biz_no"] == fmt.Sprintf("TF%d", p.TransferID) {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return
			}
		}
		raw := []byte(`{"code":"40004","sub_code":"PAYER_BALANCE_NOT_ENOUGH"}`)
		digest := sha256.Sum256(raw)
		sig, e := rsa.SignPKCS1v15(rand.Reader, platform, crypto.SHA256, digest[:])
		if e != nil {
			t.Error(e)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"alipay_fund_trans_toaccount_transfer_response": json.RawMessage(raw), "sign": base64.StdEncoding.EncodeToString(sig)})
	}))
	defer up.Close()
	defer unblock()
	ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "batch-test-app", SellerID: "2088000000000073", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client()})
	if e != nil {
		t.Fatal(e)
	}
	g.Providers[7] = ali
	g.ChannelAutoStopEnabled = true
	done := make(chan error, 1)
	finished := make(chan struct{})
	go func() { defer close(finished); _, e := g.ExecuteSettlementAuto(ctx, second, nil); done <- e }()
	defer func() { unblock(); cancel(); <-finished }()
	select {
	case <-entered:
	case e := <-done:
		t.Fatal("second did not reach original submit", e)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, e = g.ExecuteSettlementAuto(ctx, first, nil); e == nil {
		t.Fatal("first fault did not pause")
	}
	paused, e := s.ChannelWriteStatus(ctx, 7)
	if e != nil || !paused.Blocked {
		t.Fatal(paused, e)
	}
	token := strings.Repeat("l", 43)
	if _, e = s.pool.Exec(ctx, `INSERT INTO console_users(username,role,subject_id,display_name,password_hash,admin_class)VALUES('late-resume76','admin',7602,'Local resume','unused','super')`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.pool.Exec(ctx, `INSERT INTO console_sessions(token_hash,username,role,subject_id,csrf,expires_at,auth_version)VALUES($1,'late-resume76','admin',7602,'local',clock_timestamp()+interval '1 hour',1)`, lookupSessionHash(token)); e != nil {
		t.Fatal(e)
	}
	if _, e = s.ResumeChannel(guard, 7, paused.EventID, paused.Revision, token, SettlementPlanActor{Role: "admin", Class: "super", ID: 7602, Name: "late-resume76"}); e != nil {
		t.Fatal(e)
	}
	unblock()
	if e = <-done; e == nil {
		t.Fatal("late authenticated error was hidden")
	}
	current, e := s.ChannelWriteStatus(ctx, 7)
	if e != nil || current.Blocked || current.Revision != 2 {
		t.Fatal("earlier in-flight error undid explicit later restoration", current, e)
	}
	var notices int
	if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE kind='admin.channel-stopped'`).Scan(&notices); e != nil || notices != 1 {
		t.Fatal("old in-flight response generated another notice", notices, e)
	}
	for _, mid := range []int64{7395, 7396} {
		balance, e := s.Balance(ctx, mid)
		if e != nil || balance.Available != 1000 || balance.Reserved != 1000 {
			t.Fatal("late response released unknown gross", mid, balance, e)
		}
	}
	// A newly claimed fault after resume must still be able to stop writes.
	thirdPlan := settlementPlanReady(t, s, 7397)
	if _, e = s.pool.Exec(ctx, `UPDATE merchants SET profile='{"transfer_api_enabled":true}' WHERE id=7397`); e != nil {
		t.Fatal(e)
	}
	thirdOrder, e := s.CreateOrder(ctx, CreateOrderInput{MerchantID: 7397, OutTradeNo: "fresh-after-resume76-credit", Amount: 2000})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.MarkPaid(ctx, thirdOrder.TradeNo, "fresh-after-resume76-local"); e != nil {
		t.Fatal(e)
	}
	thirdGen, e := s.AuthorizeSettlementPlan(guard, thirdPlan.ID, thirdPlan.Revision, settlementPlanAuthorizationInput(t, s), settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	thirdRun, e := s.CreateSettlementPlanRun(ctx, thirdPlan.ID, thirdPlan.Revision, settlementPlanAdmin)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.GenerateSettlementPlanRun(ctx, thirdRun.ID); e != nil {
		t.Fatal(e)
	}
	thirdView, e := s.SettlementPlanRun(ctx, thirdRun.ID, settlementPlanAdmin)
	if e != nil || len(thirdView.Items) != 1 {
		t.Fatal(thirdView, e)
	}
	g.MerchantChannels[7397] = []int64{7}
	if _, e = g.AuthorizeSettlementAuto(guard, thirdPlan.ID, thirdPlan.Revision, SettlementAutoInput{GenerationAuthorizationID: thirdGen.ID, ValidFrom: thirdGen.ValidFrom, ExpiresAt: thirdGen.ExpiresAt, MaxItemGross: 1000, MaxRunGross: 1000, MaxDayGross: 1000, MaxDayItems: 1, CostPolicy: "provider_managed_excluded", Channels: []SettlementAutoChannel{{MerchantID: 7397, ChannelID: 7}}}, settlementPlanAdmin); e != nil {
		t.Fatal(e)
	}
	if _, e = g.ExecuteSettlementAuto(ctx, thirdView.Items[0].RequestID, nil); e == nil {
		t.Fatal("new authenticated fault was hidden")
	}
	current, e = s.ChannelWriteStatus(ctx, 7)
	if e != nil || !current.Blocked || current.Revision != 3 {
		t.Fatal("new post-restoration fault failed to pause", current, e)
	}
	check, e := s.CheckLedger(ctx)
	if e != nil || check.InvalidChannelStops != 0 {
		t.Fatal("new source cutoff history failed verification", check, e)
	}
}
