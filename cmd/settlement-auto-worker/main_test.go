package main

import (
	"context"
	"encoding/json"
	"errors"
	adapter "goepay/internal/adapters/payment"
	"goepay/internal/payment"
	"goepay/internal/platform/jobs"
	"testing"
)

func TestSettlementAuto73DisabledExecutorDoesNotConnectOrLoadCredentials(t *testing.T) {
	t.Setenv("DATABASE_URL", "invalid-local-test-database")
	t.Setenv("GOEPAY_MASTER_KEY", "invalid-local-test-key")
	t.Setenv("GOEPAY_CHANNEL_CONFIG_FILE", "must-not-be-opened")
	for _, flag := range []string{"", "false"} {
		t.Setenv("GOEPAY_SETTLEMENT_AUTO_ENABLED", flag)
		if e := run(context.Background()); e != nil {
			t.Fatal("disabled executor touched configuration or DB", e)
		}
	}
	for _, flag := range []string{"TRUE", "1", " true", "yes"} {
		t.Setenv("GOEPAY_SETTLEMENT_AUTO_ENABLED", flag)
		if e := run(context.Background()); e == nil {
			t.Fatal("noncanonical automatic enable accepted", flag)
		}
	}
}

type localExecutor73 struct {
	calls              int
	request, authority int64
	err                error
}

func (x *localExecutor73) ExecuteSettlementAutoAuthorized(_ context.Context, r, a int64, _ payment.TransferBatchCheck) (payment.SettlementPayout, error) {
	x.calls++
	x.request, x.authority = r, a
	return payment.SettlementPayout{SendState: "claimed", State: "unknown"}, x.err
}
func TestSettlementAuto73HandlerDoesNotHideJoinedProofOrAuditErrors(t *testing.T) {
	j := jobs.Job{Kind: payment.SettlementAutoJobKind, Payload: json.RawMessage(`{"request_id":"17","authorization_id":"8"}`)}
	unknown := &adapter.UnknownOutcome{Operation: "local automatic response"}
	x := &localExecutor73{err: unknown}
	if e := handle(context.Background(), x, j); e != nil || x.calls != 1 || x.request != 17 || x.authority != 8 {
		t.Fatal("original strict job handoff", x, e)
	}
	for _, cause := range []error{payment.ErrConflict, errors.New("local result audit fault")} {
		x.err = errors.Join(unknown, cause)
		if e := handle(context.Background(), x, j); e == nil {
			t.Fatal("unknown result hid independent source/audit failure", cause)
		}
	}
}
func TestSettlementAuto73ExecutorRequiresIndependentConfiguredWriteOptIn(t *testing.T) {
	t.Setenv("GOEPAY_SETTLEMENT_AUTO_ENABLED", "true")
	t.Setenv("DATABASE_URL", "invalid-local-test-database")
	for _, mode := range []string{"", "simulation", "configured"} {
		t.Setenv("GOEPAY_PROVIDER_MODE", mode)
		t.Setenv("GOEPAY_SANDBOX", "true")
		t.Setenv("GOEPAY_ENABLE_PROVIDER_WRITES", "false")
		if e := run(context.Background()); e != errConfiguration {
			t.Fatal("executor connected before independent safe configuration gates", mode, e)
		}
	}
}
func TestSettlementAuto73JobPayloadIsCanonicalAndCannotAcquireNewAuthority(t *testing.T) {
	r, a, e := parseJob(json.RawMessage(`{"request_id":"17","authorization_id":"8"}`))
	if e != nil || r != 17 || a != 8 {
		t.Fatal(r, a, e)
	}
	for _, raw := range []string{`null`, `{}`, `{"request_id":17,"authorization_id":"8"}`, `{"request_id":"017","authorization_id":"8"}`, `{"request_id":"17","authorization_id":"0"}`, `{"request_id":"17","authorization_id":"8","authorization_id":"9"}`, `{"request_id":"17","authorization_id":"8","amount":"1"}`, `{"request_id":"17","authorization_id":"8"} {}`, `{"request_id":"9223372036854775808","authorization_id":"8"}`} {
		if _, _, e := parseJob(json.RawMessage(raw)); e == nil {
			t.Fatal("ambiguous or forged automatic job", raw)
		}
	}
}
