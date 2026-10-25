package main

import (
	"context"
	"testing"
)

func TestLocalPrintingWorkerDefaultOffAndSandboxGateBeforeDatabase(t *testing.T) {
	t.Setenv("GOEPAY_PRINTING_EXTERNAL_ENABLED", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("GOEPAY_LOCAL_PRINTING_FAKE_ENABLED", "")
	if e := run(context.Background()); e != nil {
		t.Fatal(e)
	}
	t.Setenv("GOEPAY_LOCAL_PRINTING_FAKE_ENABLED", "true")
	t.Setenv("GOEPAY_SANDBOX", "false")
	if e := run(context.Background()); e == nil {
		t.Fatal("worker opened without explicit sandbox")
	}
	t.Setenv("GOEPAY_LOCAL_PRINTING_FAKE_ENABLED", "1")
	if e := run(context.Background()); e == nil {
		t.Fatal("ambiguous enable flag accepted")
	}
}

func TestPrintingWorkerExternalModeHasIndependentExplicitGates(t *testing.T) {
	for _, c := range []struct {
		local, external, sandbox, want string
		valid                          bool
	}{
		{"", "", "", "", true},
		{"false", "false", "true", "", true},
		{"true", "false", "true", "local", true},
		{"false", "true", "false", "external", true},
		{"true", "true", "true", "", false},
		{"false", "true", "true", "", false},
		{"false", "true", "", "", false},
		{"1", "false", "true", "", false},
		{"false", "TRUE", "false", "", false},
	} {
		mode, e := printingWorkerMode(c.local, c.external, c.sandbox)
		if (e == nil) != c.valid || mode != c.want {
			t.Fatal(c, mode, e)
		}
	}
	t.Setenv("DATABASE_URL", "")
	t.Setenv("GOEPAY_LOCAL_PRINTING_FAKE_ENABLED", "false")
	t.Setenv("GOEPAY_PRINTING_EXTERNAL_ENABLED", "true")
	t.Setenv("GOEPAY_SANDBOX", "true")
	if e := run(context.Background()); e == nil {
		t.Fatal("sandbox external mode reached database")
	}
}
