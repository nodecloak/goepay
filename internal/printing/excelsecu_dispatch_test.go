package printing

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestExcelsecuDispatchCommitsOriginalClaimBeforeTLSAndNeverResends(t *testing.T) {
	for _, mode := range []string{"accepted", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, p := printFixture(t)
			configurePrinting(t, s, 2)
			ctx := context.Background()
			if _, e := s.SetPlatform(ctx, PlatformInput{ExpectedRevision: 1, Enabled: true, Provider: "excelsecu", CredentialAction: "keep"}, printAdmin); e != nil {
				t.Fatal(e)
			}
			o, _, _, _ := printPaidOrder(t, p, "external-local-"+mode)
			job, e := s.PrepareAutomatic(ctx, o.ID)
			if e != nil || job == nil {
				t.Fatal(job, e)
			}
			var before int64
			if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries`).Scan(&before); e != nil {
				t.Fatal(e)
			}
			connector, calls := printerTLSConnector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var claims, audits int
				if e := s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM printing_claims WHERE intent_id=$1),(SELECT count(*) FROM backoffice_audit WHERE resource='printing-intents' AND resource_id=$1 AND operation='claim')`, job.ID).Scan(&claims, &audits); e != nil || claims != 1 || audits != 1 {
					t.Error("external send crossed uncommitted claim/audit", claims, audits, e)
				}
				if mode == "unknown" {
					conn, _, e := w.(http.Hijacker).Hijack()
					if e == nil {
						conn.Close()
					}
					return
				}
				io.WriteString(w, `{"code":0}`)
			}))
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					if _, e := s.Dispatch(ctx, job.ID, connector); e != nil {
						t.Error(e)
					}
				}()
			}
			wg.Wait()
			if calls.Load() != 1 {
				t.Fatal("original external intent was not sent exactly once", calls.Load())
			}
			current, e := s.Job(ctx, 901, job.ID, printOwner)
			if e != nil || current.State != mode {
				t.Fatal(current, e)
			}
			for i := 0; i < 3; i++ {
				if _, e = s.Dispatch(ctx, job.ID, connector); e != nil {
					t.Fatal(e)
				}
			}
			if calls.Load() != 1 {
				t.Fatal("claimed/unknown print was sent again", calls.Load())
			}
			var after int64
			if e = s.pool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries`).Scan(&after); e != nil || after != before {
				t.Fatal("external printing changed funds", before, after, e)
			}
			if bad, e := s.Check(ctx); e != nil || bad != 0 {
				t.Fatal("printing integrity failed", bad, e)
			}
		})
	}
}

func TestExternalPrinterWrongWorkerLeavesOriginalQueueUnclaimed(t *testing.T) {
	s, p := printFixture(t)
	configurePrinting(t, s, 2)
	ctx := context.Background()
	if _, e := s.SetPlatform(ctx, PlatformInput{ExpectedRevision: 1, Enabled: true, Provider: "excelsecu", CredentialAction: "keep"}, printAdmin); e != nil {
		t.Fatal(e)
	}
	o, _, _, _ := printPaidOrder(t, p, "wrong-worker-mode")
	job, e := s.PrepareAutomatic(ctx, o.ID)
	if e != nil {
		t.Fatal(e)
	}
	worked, e := s.runOne(ctx, true, true, LocalFake{}, 20*time.Second)
	if worked || !errors.Is(e, ErrUnsupported) {
		t.Fatal("wrong worker claimed another provider's queue", worked, e)
	}
	var claims, leased int
	if e = s.pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM printing_claims WHERE intent_id=$1),(SELECT count(*) FROM jobs WHERE kind LIKE 'receipt-print.%' AND (state<>'pending' OR attempts<>0 OR lease_token IS NOT NULL))`, job.ID).Scan(&claims, &leased); e != nil || claims != 0 || leased != 0 {
		t.Fatal("wrong worker altered original intent or task", claims, leased, e)
	}
}

func TestExcelsecuConnectorCannotSendLocalFakeIntent(t *testing.T) {
	s, p := printFixture(t)
	configurePrinting(t, s, 2)
	ctx := context.Background()
	o, _, _, _ := printPaidOrder(t, p, "external-wrong-source")
	job, e := s.PrepareAutomatic(ctx, o.ID)
	if e != nil {
		t.Fatal(e)
	}
	connector, calls := printerTLSConnector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("local fake reached external TLS")
		io.WriteString(w, `{"code":0}`)
	}))
	out, e := s.Dispatch(ctx, job.ID, connector)
	if !errors.Is(e, ErrUnsupported) || out.State != "queued" || calls.Load() != 0 {
		t.Fatal(out, e, calls.Load())
	}
}

func TestExcelsecuTLSAuditFaultsNeverRearmACommittedSend(t *testing.T) {
	for _, phase := range []string{"claim", "result"} {
		t.Run(phase, func(t *testing.T) {
			s, p := printFixture(t)
			configurePrinting(t, s, 2)
			ctx := context.Background()
			if _, e := s.SetPlatform(ctx, PlatformInput{ExpectedRevision: 1, Enabled: true, Provider: "excelsecu", CredentialAction: "keep"}, printAdmin); e != nil {
				t.Fatal(e)
			}
			o, _, _, _ := printPaidOrder(t, p, "tls-audit-"+phase)
			job, e := s.PrepareAutomatic(ctx, o.ID)
			if e != nil {
				t.Fatal(e)
			}
			connector, calls := printerTLSConnector(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, `{"code":0}`) }))
			if _, e = s.pool.Exec(ctx, `CREATE FUNCTION external_print_audit_fault() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN IF NEW.resource='printing-intents' AND NEW.operation='`+phase+`' THEN RAISE EXCEPTION 'synthetic external print audit failure';END IF;RETURN NEW;END$$;CREATE TRIGGER external_print_audit_fault BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION external_print_audit_fault()`); e != nil {
				t.Fatal(e)
			}
			if _, e = s.Dispatch(ctx, job.ID, connector); e == nil {
				t.Fatal("audit failure ignored")
			}
			want := int32(0)
			if phase == "result" {
				want = 1
			}
			if calls.Load() != want {
				t.Fatal("HTTP crossed missing durable claim or was resent", calls.Load(), want)
			}
			if _, e = s.pool.Exec(ctx, `DROP TRIGGER external_print_audit_fault ON backoffice_audit`); e != nil {
				t.Fatal(e)
			}
			if _, e = s.Dispatch(ctx, job.ID, connector); e != nil {
				t.Fatal(e)
			}
			if calls.Load() != 1 {
				t.Fatal("original committed HTTP request was repeated", calls.Load())
			}
			if bad, e := s.Check(ctx); e != nil || bad != 0 {
				t.Fatal(bad, e)
			}
		})
	}
}
