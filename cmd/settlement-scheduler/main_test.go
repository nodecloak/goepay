package main

import (
	"context"
	"testing"
)

func TestDisabledSchedulerDoesNotConnectOrLoadCredentials(t *testing.T) {
	t.Setenv("GOEPAY_SETTLEMENT_SCHEDULER_ENABLED", "")
	t.Setenv("DATABASE_URL", "invalid-not-a-database")
	t.Setenv("GOEPAY_MASTER_KEY", "invalid-not-a-key")
	if err := run(context.Background()); err != nil {
		t.Fatal("default disabled attempted startup", err)
	}
	t.Setenv("GOEPAY_SETTLEMENT_SCHEDULER_ENABLED", "false")
	if err := run(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestEnabledValueMustBeExplicitCanonicalBoolean(t *testing.T) {
	for _, v := range []string{"1", "TRUE", " true", "yes"} {
		t.Setenv("GOEPAY_SETTLEMENT_SCHEDULER_ENABLED", v)
		if err := run(context.Background()); err == nil {
			t.Fatal("ambiguous enabled setting accepted", v)
		}
	}
}
