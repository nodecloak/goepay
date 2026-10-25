package printing

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type countingPrintConnector struct{ calls atomic.Int64 }

func (*countingPrintConnector) LocalOnly() bool { return true }
func (c *countingPrintConnector) Send(context.Context, PrintCommand) (Outcome, error) {
	c.calls.Add(1)
	return Outcome{State: "accepted"}, nil
}
func TestPrintingAuditFailuresPreserveClaimBoundary(t *testing.T) {
	for _, phase := range []string{"claim", "result"} {
		t.Run(phase, func(t *testing.T) {
			s, p := printFixture(t)
			configurePrinting(t, s, 2)
			ctx := context.Background()
			o, _, _, _ := printPaidOrder(t, p, "audit-"+phase)
			j, e := s.PrepareAutomatic(ctx, o.ID)
			if e != nil {
				t.Fatal(e)
			}
			c := &countingPrintConnector{}
			if _, e = s.pool.Exec(ctx, `CREATE FUNCTION local_dispatch_audit_fail() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.resource='printing-intents' AND NEW.operation='`+phase+`' THEN RAISE EXCEPTION 'synthetic printing audit failure'; END IF; RETURN NEW; END $$; CREATE TRIGGER local_dispatch_audit_fail BEFORE INSERT ON backoffice_audit FOR EACH ROW EXECUTE FUNCTION local_dispatch_audit_fail()`); e != nil {
				t.Fatal(e)
			}
			if _, e = s.Dispatch(ctx, j.ID, c); e == nil {
				t.Fatal("audit failure swallowed")
			}
			want := int64(0)
			if phase == "result" {
				want = 1
			}
			if c.calls.Load() != want {
				t.Fatal("IO crossed missing claim audit", c.calls.Load())
			}
			if _, e = s.pool.Exec(ctx, "DROP TRIGGER local_dispatch_audit_fail ON backoffice_audit"); e != nil {
				t.Fatal(e)
			}
			v, e := s.Dispatch(ctx, j.ID, c)
			if e != nil {
				t.Fatal(e)
			}
			if c.calls.Load() != 1 {
				t.Fatal("ambiguous result was sent again", c.calls.Load())
			}
			if phase == "claim" && v.State != "accepted" {
				t.Fatal(v)
			}
			if phase == "result" && v.State != "sending" && v.State != "unknown" {
				t.Fatal("claim without trusted result misreported", v)
			}
			if n, e := s.Check(ctx); e != nil || n != 0 {
				t.Fatal("legitimate unresolved claim not integrity corruption", n, e)
			}
		})
	}
}
func TestPrintingExternalProviderAndChangedScopeCannotExecute(t *testing.T) {
	for _, mode := range []string{"excelsecu", "scope_changed"} {
		t.Run(mode, func(t *testing.T) {
			s, p := printFixture(t)
			configurePrinting(t, s, 2)
			ctx := context.Background()
			if mode == "excelsecu" {
				if _, e := s.SetPlatform(ctx, PlatformInput{ExpectedRevision: 1, Enabled: true, Provider: "excelsecu", CredentialAction: "keep"}, printAdmin); e != nil {
					t.Fatal(e)
				}
			}
			o, _, _, _ := printPaidOrder(t, p, "nonexecute-"+mode)
			j, e := s.PrepareAutomatic(ctx, o.ID)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "scope_changed" {
				if _, e = s.SetMerchant(ctx, 901, MerchantInput{ExpectedRevision: 1, Mode: 2, Copies: 3, DeviceAction: "keep"}, printOwner); e != nil {
					t.Fatal(e)
				}
			}
			c := &countingPrintConnector{}
			v, e := s.Dispatch(ctx, j.ID, c)
			if mode == "excelsecu" && (!errors.Is(e, ErrUnsupported) || v.State != "queued") || mode == "scope_changed" && (e != nil || v.State != "rejected") || c.calls.Load() != 0 {
				t.Fatal(v, c.calls.Load(), e)
			}
			view, e := s.Platform(ctx, printAdmin)
			if e != nil || !view.ExternalExecutionAvailable {
				t.Fatal("fixed external connector capability missing; this is not a worker-health claim", view, e)
			}
		})
	}
}
