package server

import (
	"context"
	"encoding/json"
	"fmt"
	"goepay/internal/merchantnotice"
	"goepay/internal/platform/jobs"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRisk177OriginalCheckNoticeSwitchCreatesConsumedJobIntent(t *testing.T) {
	for _, kind := range []string{"merchant", "channel"} {
		t.Run(kind, func(t *testing.T) {
			f, _, _ := risk177Flow(t)
			if _, e := f.s.Pool.Exec(context.Background(), `UPDATE merchants SET profile=jsonb_set(profile,'{email}','"risk177-recipient@example.invalid"')WHERE id=1`); e != nil {
				t.Fatal(e)
			}
			v := risk177Data(t, f.request("GET", "", f.csrf))
			v = risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
				if kind == "merchant" {
					s["auto_check_sucrate"] = "1"
					s["check_sucrate_second"] = "3600"
					s["check_sucrate_count"] = "1"
					s["check_sucrate_value"] = "50"
					s["check_sucrate_notice"] = "1"
				} else {
					s["auto_check_channel"] = "1"
					s["check_channel_second"] = "3600"
					s["check_channel_failcount"] = "1"
					s["check_channel_notice"] = "1"
				}
			}), f.csrf))
			p := f.pay(t, "risk177-check-notice", "192.0.2.177")
			if p["code"] != float64(1) {
				t.Fatal(p)
			}
			f.check(t, v, "risk177-check-notice")
			var count int
			if e := f.s.Pool.QueryRow(context.Background(), `SELECT count(*) FROM jobs WHERE kind='core-risk.notice'`).Scan(&count); e != nil || count != 1 {
				t.Fatal("notice switch was saved metadata without durable consumer intent", kind, count, e)
			}
			var calls atomic.Int32
			sink := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]string
				if json.NewDecoder(r.Body).Decode(&body) != nil || !strings.Contains(body["title"], "风控") || body["to"] == "" {
					t.Error("invalid original risk notice")
				}
				calls.Add(1)
				w.WriteHeader(204)
			}))
			defer sink.Close()
			resolver := risk177NoticeResolver{&risk177NoticeSender{client: sink.Client(), target: sink.URL}, "risk177-admin@example.invalid"}
			q := jobs.Queue{Pool: f.s.Pool}
			j, e := q.Claim(context.Background(), []string{"core-risk.notice"}, time.Minute)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = f.s.Pool.Exec(context.Background(), `UPDATE merchants SET profile=jsonb_set(profile,'{email}','"later@example.invalid"')WHERE id=1`); e != nil {
				t.Fatal(e)
			}
			if e = f.s.Store.DeliverCoreRiskNotice(context.Background(), j.ID, j.Token, resolver); e != nil {
				t.Fatal("actual local notice consumer", e)
			}
			if e = f.s.Store.DeliverCoreRiskNotice(context.Background(), j.ID, j.Token, resolver); e != nil || calls.Load() != 1 {
				t.Fatal("successful original notice replay sent again", calls.Load(), e)
			}
		})
	}
}

type risk177NoticeSender struct {
	client *http.Client
	target string
}

func (*risk177NoticeSender) NoticeTransport() merchantnotice.Transport { return merchantnotice.SMTP }
func (*risk177NoticeSender) NoticeScope() string                       { return strings.Repeat("a", 64) }
func (*risk177NoticeSender) ValidateNoticeRecipient(to string) error {
	if !strings.HasSuffix(to, "@example.invalid") {
		return fmt.Errorf("synthetic recipient only")
	}
	return nil
}
func (s *risk177NoticeSender) SendMerchantNotice(ctx context.Context, to string, m merchantnotice.Message) error {
	if !m.Valid() {
		return fmt.Errorf("unsealed message")
	}
	raw, _ := json.Marshal(map[string]string{"to": to, "title": m.Title, "html": m.HTML})
	r, e := http.NewRequestWithContext(ctx, "POST", s.target, strings.NewReader(string(raw)))
	if e != nil {
		return e
	}
	resp, e := s.client.Do(r)
	if e != nil {
		return e
	}
	resp.Body.Close()
	if resp.StatusCode != 204 {
		return fmt.Errorf("synthetic unknown")
	}
	return nil
}

type risk177NoticeResolver struct {
	sender    merchantnotice.Sender
	recipient string
}

func (r risk177NoticeResolver) ResolveCoreRiskNotice(context.Context, string) (merchantnotice.Sender, string, error) {
	return r.sender, r.recipient, nil
}
func TestRisk177NoticeUnknownNeverRetriesAndFinalClaimLeaseRollsBack(t *testing.T) {
	for _, scenario := range []string{"unknown", "expired-last-write", "claim-loss"} {
		t.Run(scenario, func(t *testing.T) {
			f, _, _ := risk177Flow(t)
			ctx := context.Background()
			if _, e := f.s.Pool.Exec(ctx, `UPDATE merchants SET profile=jsonb_set(profile,'{email}','"risk177-recipient@example.invalid"')WHERE id=1`); e != nil {
				t.Fatal(e)
			}
			v := risk177Data(t, f.request("GET", "", f.csrf))
			v = risk177Data(t, f.request("PATCH", risk177Body(t, v, func(s map[string]any) {
				s["auto_check_sucrate"] = "1"
				s["check_sucrate_second"] = "3600"
				s["check_sucrate_count"] = "1"
				s["check_sucrate_value"] = "50"
				s["check_sucrate_notice"] = "1"
			}), f.csrf))
			if p := f.pay(t, "risk177-notice-negative", "192.0.2.177"); p["code"] != float64(1) {
				t.Fatal(p)
			}
			f.check(t, v, "risk177-notice-negative-check")
			var calls atomic.Int32
			sink := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(503) }))
			defer sink.Close()
			resolver := risk177NoticeResolver{&risk177NoticeSender{client: sink.Client(), target: sink.URL}, ""}
			q := jobs.Queue{Pool: f.s.Pool}
			lease := time.Minute
			if scenario == "expired-last-write" {
				lease = time.Second
				if _, e := f.s.Pool.Exec(ctx, `CREATE FUNCTION risk177_slow_notice_claim()RETURNS TRIGGER LANGUAGE plpgsql AS $$BEGIN PERFORM pg_sleep(1.2);RETURN NEW;END$$;CREATE TRIGGER risk177_slow_notice_claim BEFORE INSERT ON core_risk_notice_claims FOR EACH ROW EXECUTE FUNCTION risk177_slow_notice_claim()`); e != nil {
					t.Fatal(e)
				}
			}
			j, e := q.Claim(ctx, []string{"core-risk.notice"}, lease)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.s.Store.DeliverCoreRiskNotice(ctx, j.ID, j.Token, resolver); e == nil {
				t.Fatal("unconfirmed delivery was reported complete")
			}
			if scenario == "expired-last-write" {
				var claims, audits int
				if e = f.s.Pool.QueryRow(ctx, `SELECT(SELECT count(*)FROM core_risk_notice_claims),(SELECT count(*)FROM backoffice_audit WHERE resource='core-risk-notices'AND operation='attempt')`).Scan(&claims, &audits); e != nil || claims != 0 || audits != 0 || calls.Load() != 0 {
					t.Fatal("expired final claim committed or sent", claims, audits, calls.Load(), e)
				}
				return
			}
			if calls.Load() != 1 {
				t.Fatal("actual original local attempt missing", calls.Load())
			}
			if scenario == "claim-loss" {
				if _, e = f.s.Pool.Exec(ctx, `ALTER TABLE core_risk_notice_claims DISABLE TRIGGER USER;ALTER TABLE core_risk_notice_results DISABLE TRIGGER USER;DELETE FROM core_risk_notice_results;DELETE FROM core_risk_notice_claims;ALTER TABLE core_risk_notice_claims ENABLE TRIGGER USER;ALTER TABLE core_risk_notice_results ENABLE TRIGGER USER`); e != nil {
					t.Fatal(e)
				}
			}
			if e = q.Retry(ctx, j, "synthetic local unresolved", 0); e != nil {
				t.Fatal(e)
			}
			j, e = q.Claim(ctx, []string{"core-risk.notice"}, time.Minute)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.s.Store.DeliverCoreRiskNotice(ctx, j.ID, j.Token, resolver); e == nil || calls.Load() != 1 {
				t.Fatal("unknown/lost original claim was resent", scenario, calls.Load(), e)
			}
		})
	}
}
