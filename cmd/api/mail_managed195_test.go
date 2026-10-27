package main

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"goepay/internal/enrollment"
	"testing"
)

func TestManagedMail195StartupKeepsExplicitGateAndAllowsConsoleOnlyCredentialsWithoutIO(t *testing.T) {
	pool, e := pgxpool.New(context.Background(), "postgres://synthetic195@127.0.0.1:9/unused195?sslmode=disable")
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	env := map[string]string{"GOEPAY_ENABLE_EMAIL_VERIFICATION": "true"}
	getenv := func(k string) string { return env[k] }
	sender, e := configuredManagedVerification195(pool, make([]byte, 32), getenv)
	if e != nil || sender == nil {
		t.Fatal("console-only SMTP account cannot start behind explicit gate", e)
	}
	if _, ok := sender.(enrollment.SenderPreparer); !ok {
		t.Fatal("runtime did not bind sealed account in admission transaction")
	}
	if e = sender.SendVerification(context.Background(), "recipient@example.test", "recover", "1234567"); e == nil {
		t.Fatal("mutable account resolver allowed direct unadmitted send")
	}
	for _, gate := range []string{"", "false"} {
		env["GOEPAY_ENABLE_EMAIL_VERIFICATION"] = gate
		s, e := configuredManagedVerification195(nil, nil, getenv)
		if e != nil || s != nil {
			t.Fatal("disabled gate constructed a sender", gate, e)
		}
	}
	env["GOEPAY_ENABLE_EMAIL_VERIFICATION"] = "true"
	env["GOEPAY_SMTP_PASSWORD"] = "synthetic195-incomplete"
	if _, e = configuredManagedVerification195(pool, make([]byte, 32), getenv); e == nil {
		t.Fatal("partial deployment configuration silently ignored")
	}
}
