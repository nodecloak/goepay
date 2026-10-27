package server

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"goepay/internal/enrollment"
	"strings"
	"testing"
)

type closedEmail197 struct{}

func (closedEmail197) PrepareVerification(context.Context, pgx.Tx) (enrollment.Sender, error) {
	return nil, errors.New("synthetic managed account closed")
}
func (closedEmail197) SendVerification(context.Context, string, string, string) error {
	panic("disabled prepared account fallback")
}
func TestEmail197MetadataReflectsActualManagedDeliveryAvailability(t *testing.T) {
	f := email197Local(t)
	service, e := enrollment.New(f.p, f.s.Store, make([]byte, 32), closedEmail197{})
	if e != nil {
		t.Fatal(e)
	}
	f.s.Enrollment = enrollment.NewHandler(service, "http://127.0.0.1:8187")
	v := email197Data(t, f.request("GET", "", nil))
	if v["email_enabled"] != false {
		t.Fatal("disabled managed SMTP advertised as usable", v)
	}
}
func TestEmail197ExplicitResendHonorsSharedCooldownAndInvalidatesOnlyPriorProof(t *testing.T) {
	f := email197Local(t)
	f.legacy(t)
	id := strings.Repeat("r", 43)
	f.start(t, id)
	sendID := strings.Repeat("s", 43)
	v := email197Data(t, f.request("POST", "/"+id+"/old-code", map[string]any{"send_id": sendID}))
	if v["send_id"] != sendID {
		t.Fatal("send identity missing", v)
	}
	f.mu.Lock()
	oldCode := f.messages[len(f.messages)-1].code
	count := len(f.messages)
	f.mu.Unlock()
	email197Data(t, f.request("POST", "/"+id+"/old-code", map[string]any{"send_id": sendID}))
	f.mu.Lock()
	if len(f.messages) != count {
		t.Fatal("lost response retried SMTP")
	}
	f.mu.Unlock()
	nextID := strings.Repeat("t", 43)
	if w := f.request("POST", "/"+id+"/old-code", map[string]any{"send_id": nextID}); w.Code != 429 {
		t.Fatal("manual resend bypassed shared cooldown", w.Code)
	}
	if _, e := f.p.Exec(t.Context(), `UPDATE enrollment_challenges SET created_at=created_at-interval '2 minutes'`); e != nil {
		t.Fatal(e)
	}
	email197Data(t, f.request("POST", "/"+id+"/old-code", map[string]any{"send_id": nextID}))
	f.mu.Lock()
	newCode := f.messages[len(f.messages)-1].code
	newCount := len(f.messages)
	f.mu.Unlock()
	if newCount != count+1 {
		t.Fatal("explicit new attempt did not send exactly once")
	}
	if newCode != oldCode {
		if w := f.request("POST", "/"+id+"/old-verify", map[string]any{"code": oldCode}); w.Code != 422 {
			t.Fatal("superseded OTP remained usable", w.Code)
		}
	}
	email197Data(t, f.request("POST", "/"+id+"/old-verify", map[string]any{"code": newCode}))
}
