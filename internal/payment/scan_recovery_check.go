package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"strconv"
)

func (s *Store) checkScanRecoveries(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	var tables int
	var accountsPresent bool
	e := tx.QueryRow(ctx, `SELECT num_nonnulls(to_regclass('scan_recovery_origins'),to_regclass('scan_recovery_eligibility'),to_regclass('scan_recovery_query_events'),to_regclass('scan_recovery_query_heads'),to_regclass('scan_cancellation_claims'),to_regclass('scan_cancellation_results')),to_regclass('provider_attempt_accounts') IS NOT NULL`).Scan(&tables, &accountsPresent)
	if e != nil {
		return e
	}
	if tables == 0 {
		// Actual pre78 schemas remain readable. New version anchors must never be
		// silently accepted if their entire source schema has disappeared.
		if accountsPresent {
			var count int64
			e = tx.QueryRow(ctx, `SELECT count(*) FROM provider_attempt_accounts a WHERE COALESCE((to_jsonb(a)->>'recovery_version')::int,0)<>0 OR to_jsonb(a)->>'recovery_source_hash' IS NOT NULL`).Scan(&count)
			if e != nil {
				return e
			}
			report.InvalidScanRecoverySources += count
		}
		return nil
	}
	if tables != 6 || !accountsPresent {
		report.InvalidScanRecoverySources++
		return nil
	}
	// One server cursor performs one complete candidate enumeration while Go
	// retains at most100 keys. FETCH closes its result before proof reads, so the
	// same repeatable-read snapshot can validate every source and independent
	// orphan anchor without first-page limits or an unbounded Go result slice.
	_, e = tx.Exec(ctx, `DECLARE goepay_scan_recovery_evidence NO SCROLL CURSOR FOR
 SELECT source_key,bool_or(financial_anchor) FROM (
  SELECT attempt_id::text source_key,false financial_anchor FROM scan_recovery_origins
  UNION ALL SELECT attempt_id::text,false FROM scan_recovery_eligibility
  UNION ALL SELECT attempt_id::text,false FROM scan_recovery_query_heads
  UNION ALL SELECT attempt_id::text,false FROM scan_recovery_query_events
  UNION ALL SELECT attempt_id::text,true FROM scan_cancellation_claims
  UNION ALL SELECT attempt_id::text,true FROM scan_cancellation_results
  UNION ALL SELECT attempt_id::text,false FROM provider_attempt_accounts a WHERE COALESCE((to_jsonb(a)->>'recovery_version')::int,0)<>0 OR to_jsonb(a)->>'recovery_source_hash' IS NOT NULL
  UNION ALL SELECT resource_id::text,resource IN('scan-cancellations','scan-cancellation-results') FROM backoffice_audit WHERE resource IN('scan-recovery-origins','scan-recovery-eligibility','scan-cancellations','scan-cancellation-results')
  UNION ALL SELECT COALESCE(changes->>'attempt_id',''),false FROM backoffice_audit WHERE resource='scan-recovery-queries'
  UNION ALL SELECT COALESCE(payload->>'attempt_id',''),false FROM jobs WHERE kind='payment.scan-recover'
 ) candidates GROUP BY source_key ORDER BY source_key COLLATE "C"`)
	if e != nil {
		return e
	}
	for {
		rows, e := tx.Query(ctx, "FETCH FORWARD 100 FROM goepay_scan_recovery_evidence")
		if e != nil {
			return e
		}
		type candidate struct {
			key       string
			financial bool
		}
		page := make([]candidate, 0, 100)
		for rows.Next() {
			var item candidate
			if e = rows.Scan(&item.key, &item.financial); e != nil {
				break
			}
			page = append(page, item)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return e
		}
		for _, item := range page {
			id, parseErr := strconv.ParseInt(item.key, 10, 64)
			if parseErr != nil || id <= 0 || strconv.FormatInt(id, 10) != item.key {
				report.InvalidScanRecoverySources++
				if item.financial {
					report.UnknownScanCancellations++
				}
				continue
			}
			source, checkErr := s.readScanRecoveryOrigin(ctx, tx, id)
			if checkErr == nil {
				checkErr = s.checkScanRecoveryAnchor(ctx, tx, source)
			}
			if checkErr == nil {
				checkErr = s.scanCancellationBarrierForSource(ctx, tx, source)
			}
			if checkErr == nil || errors.Is(checkErr, ErrScanCancelled) {
				continue
			}
			if errors.Is(checkErr, ErrScanCancellationPending) {
				report.UnknownScanCancellations++
				continue
			}
			if invalidSharingEvidence(checkErr) || errors.Is(checkErr, ErrInvalid) || errors.Is(checkErr, pgx.ErrNoRows) {
				report.InvalidScanRecoverySources++
				if item.financial {
					report.UnknownScanCancellations++
				}
			} else {
				return checkErr
			}
		}
		if len(page) < 100 {
			break
		}
	}
	_, e = tx.Exec(ctx, "CLOSE goepay_scan_recovery_evidence")
	return e
}
