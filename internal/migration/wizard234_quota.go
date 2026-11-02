package migration

import (
	"context"

	"github.com/jackc/pgx/v5"
)

const wizardArchiveLimit234 int64 = 512 << 20

func wizardWithinCapacity234(ownerRuns, allRuns, storedBytes, incomingBytes int64) bool {
	return ownerRuns < 20 && allRuns < 64 && incomingBytes >= 0 && storedBytes >= 0 && incomingBytes <= wizardArchiveLimit234-storedBytes
}

// This separate short lock protects preparations across API processes without
// taking the financial-import lock while parsing an uploaded backup. Existing
// archives are never purged to make a new preparation fit.
func wizardCapacity234(ctx context.Context, tx pgx.Tx, a WizardActor234, incoming int64) error {
	if _, e := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(74973234)"); e != nil {
		return e
	}
	var owner, all, stored int64
	e := tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE owner_username=$1 AND owner_subject=$2 AND created_at>clock_timestamp()-interval '24 hours'),count(*) FILTER(WHERE created_at>clock_timestamp()-interval '24 hours'),COALESCE(sum(octet_length(archive_cipher)::bigint+octet_length(snapshot_cipher)::bigint+octet_length(payload_cipher)::bigint),0) FROM migration_wizard_runs234`, a.User.Username, a.User.ID).Scan(&owner, &all, &stored)
	if e != nil {
		return e
	}
	if !wizardWithinCapacity234(owner, all, stored, incoming) {
		return ErrWizardCapacity234
	}
	return nil
}

func wizardBoundIssues234(in []Issue, code string) []Issue {
	if len(in) <= 512 {
		return in
	}
	out := append([]Issue(nil), in[:511]...)
	return append(out, Issue{"backup", code})
}

func wizardPublicReport234(in Report) Report {
	out := in
	out.Issues = wizardBoundIssues234(append([]Issue{}, in.Issues...), "issues_truncated")
	out.Warnings = wizardBoundIssues234(append([]Issue{}, in.Warnings...), "warnings_truncated")
	return out
}
