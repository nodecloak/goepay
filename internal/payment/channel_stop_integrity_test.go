package payment

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestChannelStop76DamagedHistoryAuditOrNoticeCannotBecomeWritable(t *testing.T) {
	for _, mode := range []string{"head-proof", "source-proof", "missing-audit", "notice-orphan", "notice-job"} {
		t.Run(mode, func(t *testing.T) {
			s, g, _, _, _, id := autoReady73(t)
			ctx := context.Background()
			platform, e := rsa.GenerateKey(rand.Reader, 2048)
			if e != nil {
				t.Fatal(e)
			}
			merchant, e := rsa.GenerateKey(rand.Reader, 2048)
			if e != nil {
				t.Fatal(e)
			}
			up := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
			ali, e := adapter.NewAlipay(adapter.AlipayConfig{AppID: "batch-test-app", SellerID: "2088000000000073", PrivateKey: merchant, PublicKey: &platform.PublicKey, GatewayURL: up.URL, HTTPClient: up.Client()})
			if e != nil {
				t.Fatal(e)
			}
			g.Providers[7] = ali
			g.ChannelAutoStopEnabled = true
			if _, e = g.ExecuteSettlementAuto(ctx, id, nil); e == nil {
				t.Fatal("fixture did not generate signed error")
			}
			before, e := s.ChannelWriteStatus(ctx, 7)
			if e != nil || !before.Blocked {
				t.Fatal(before, e)
			}
			tx, e := s.pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer tx.Rollback(ctx)
			if _, e = tx.Exec(ctx, `SET LOCAL session_replication_role=replica`); e != nil {
				t.Fatal(e)
			}
			sql := map[string]string{
				"head-proof":    `UPDATE channel_stop_heads SET proof_hash=decode(repeat('00',32),'hex') WHERE channel_id=7`,
				"source-proof":  `UPDATE channel_stop_events SET proof_hash=decode(repeat('00',32),'hex') WHERE id=$1`,
				"missing-audit": `DELETE FROM backoffice_audit WHERE resource='channel-stops' AND resource_id=$1`,
				"notice-orphan": `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'channel-notice','channel-stop-notices',$1,'unknown','{}')`,
				"notice-job":    `UPDATE jobs SET payload='{"event_id":"999999"}' WHERE kind='admin.channel-stopped' AND event_key='channel-stop:'||$1::bigint::text`,
			}[mode]
			if mode == "head-proof" {
				_, e = tx.Exec(ctx, sql)
			} else {
				_, e = tx.Exec(ctx, sql, before.EventID)
			}
			if e != nil {
				t.Fatal(e)
			}
			if e = tx.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			if _, e = s.ChannelWriteStatus(ctx, 7); !errors.Is(e, ErrConflict) {
				t.Fatal("damaged original source became an enabled channel", e)
			}
			if _, e = g.Transfer(ctx, 7395, 7, "must-not-write76", adapter.TransferRequest{Type: "alipay", Account: "local-recipient@example.test", Name: "Local Recipient", Amount: 100}); !errors.Is(e, ErrConflict) {
				t.Fatal("damaged source did not block new writes", e)
			}
			report, e := s.CheckLedger(ctx)
			if e != nil || report.InvalidChannelStops == 0 {
				t.Fatal("damaged independent source escaped checker", report, e)
			}
			balance, e := s.Balance(ctx, 7395)
			if e != nil || balance.Available != 1000 || balance.Reserved != 1000 {
				t.Fatal("damaged metadata released unknown gross", balance, e)
			}
		})
	}
}
