package main

import (
	"context"
	"strings"
	"testing"
)

func TestDisabledSchedulerNeverReadsDatabaseConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "invalid:must-not-connect")
	t.Setenv("GOEPAY_MASTER_KEY", "invalid-must-not-read")
	for _, flag := range []string{"", "false"} {
		t.Setenv("GOEPAY_REFERRAL_SCHEDULER_ENABLED", flag)
		if err := run(context.Background()); err != nil {
			t.Fatal("disabled scheduler attempted startup", err)
		}
	}
}
func TestSchedulerRejectsAmbiguousOptInBeforeDatabaseAccess(t *testing.T) {
	t.Setenv("DATABASE_URL", "invalid:must-not-connect")
	for _, flag := range []string{"TRUE", "1", "yes", " false", "true "} {
		t.Setenv("GOEPAY_REFERRAL_SCHEDULER_ENABLED", flag)
		if err := run(context.Background()); err == nil || !strings.Contains(err.Error(), "enable flag") {
			t.Fatal("ambiguous scheduler opt-in accepted", flag, err)
		}
	}
}
