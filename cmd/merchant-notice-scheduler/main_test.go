package main

import (
	"context"
	"strings"
	"testing"
)

func TestMerchantNoticeSchedulerIsOffBeforeDatabaseAndSecrets(t *testing.T) {
	t.Setenv("DATABASE_URL", "invalid:must-not-connect")
	t.Setenv("GOEPAY_MASTER_KEY", "invalid-must-not-read")
	t.Setenv("GOEPAY_MERCHANT_NOTICE_SEND_HOUR", "invalid-must-not-read")
	for _, flag := range []string{"", "false"} {
		t.Setenv("GOEPAY_MERCHANT_NOTICE_SCHEDULER_ENABLED", flag)
		if err := run(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}
func TestMerchantNoticeSchedulerRejectsAmbiguousFlagAndHourBeforeDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "invalid:must-not-connect")
	for _, flag := range []string{"TRUE", "1", " true", "true "} {
		t.Setenv("GOEPAY_MERCHANT_NOTICE_SCHEDULER_ENABLED", flag)
		if err := run(context.Background()); err == nil || !strings.Contains(err.Error(), "enable flag") {
			t.Fatal(flag, err)
		}
	}
	t.Setenv("GOEPAY_MERCHANT_NOTICE_SCHEDULER_ENABLED", "true")
	for _, hour := range []string{"24", "-1", "09", " 9", "true"} {
		t.Setenv("GOEPAY_MERCHANT_NOTICE_SEND_HOUR", hour)
		if err := run(context.Background()); err == nil || !strings.Contains(err.Error(), "send hour") {
			t.Fatal(hour, err)
		}
	}
}
