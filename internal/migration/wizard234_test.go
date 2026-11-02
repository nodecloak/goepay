package migration

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestWizard234ConstructorRequiresProtectedDatabaseAndKey(t *testing.T) {
	if _, e := NewWizard234(nil, bytes.Repeat([]byte{1}, 32)); !errors.Is(e, ErrWizardUnavailable234) {
		t.Fatal("accepted absent database")
	}
	if _, e := NewWizard234(nil, nil); !errors.Is(e, ErrWizardUnavailable234) {
		t.Fatal("accepted absent key")
	}
}

func TestWizard234PublicViewContainsExplicitNullReport(t *testing.T) {
	raw, e := json.Marshal(WizardView234{})
	if e != nil || !bytes.Contains(raw, []byte(`"report":null`)) {
		t.Fatal("missing stable public null report")
	}
}

func TestWizard234CapacityAndIssueBoundsNeverChangeBlockedFacts(t *testing.T) {
	if !wizardWithinCapacity234(19, 63, wizardArchiveLimit234-100, 100) || wizardWithinCapacity234(20, 0, 0, 1) || wizardWithinCapacity234(0, 64, 0, 1) || wizardWithinCapacity234(0, 0, wizardArchiveLimit234, 1) || wizardWithinCapacity234(0, 0, -1, 1) {
		t.Fatal("staging quota boundary")
	}
	issues := make([]Issue, 2000)
	for i := range issues {
		issues[i] = Issue{"source", "unsupported_obligation"}
	}
	bounded := wizardBoundIssues234(issues, "issues_truncated")
	if len(bounded) != 512 || bounded[0].Code != "unsupported_obligation" || bounded[511].Code != "issues_truncated" || len(issues) != 2000 {
		t.Fatal("unsafe summary truncation")
	}
}

func TestWizard234CompletedPublicReportBoundedWithoutTruncatingOriginal(t *testing.T) {
	original := Report{Valid: true, Applied: true, Merchants: 50000, AvailableTotal: "123456.78", Issues: make([]Issue, 700), Warnings: make([]Issue, 50000)}
	for i := range original.Warnings {
		original.Warnings[i] = Issue{"refunds[10000]", "legacy_refund_is_read_only_history"}
	}
	public := wizardPublicReport234(original)
	if !public.Valid || !public.Applied || public.Merchants != 50000 || public.AvailableTotal != "123456.78" || len(public.Issues) != 512 || len(public.Warnings) != 512 || len(original.Issues) != 700 || len(original.Warnings) != 50000 {
		t.Fatal("public report boundary changed source/completion facts")
	}
	if public.Warnings[511].Code != "warnings_truncated" || public.Issues[511].Code != "issues_truncated" {
		t.Fatal("missing summary disclosure")
	}
	public.Warnings[0].Code = "changed-public-view"
	if original.Warnings[0].Code == "changed-public-view" {
		t.Fatal("public report aliases original import record")
	}
	serialized, e := json.Marshal(public)
	if e != nil || len(serialized) > 2<<20 {
		t.Fatal("completed report exceeds browser contract", len(serialized), e)
	}
}
