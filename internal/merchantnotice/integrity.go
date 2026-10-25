package merchantnotice

import (
	"context"
	"crypto/hmac"
	"errors"

	"github.com/jackc/pgx/v5"
)

type IntegrityCheck struct {
	InvalidProfiles   int64 `json:"invalid_merchant_notice_profiles"`
	InvalidEvents     int64 `json:"invalid_merchant_notice_events"`
	InvalidDeliveries int64 `json:"invalid_merchant_notice_deliveries"`
	UnknownDeliveries int64 `json:"unknown_merchant_notice_deliveries"`
}

func noticeEvidenceError(err error) bool {
	return errors.Is(err, ErrConflict) || errors.Is(err, ErrState) || errors.Is(err, ErrInvalid) || errors.Is(err, ErrNotFound)
}

func noticeIDs(ctx context.Context, tx pgx.Tx, sql string, cursor int64) ([]int64, error) {
	rows, err := tx.Query(ctx, sql, cursor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, 100)
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) verifyEventProfile(ctx context.Context, tx pgx.Tx, e eventRecord) (profileData, error) {
	if e.ProfileID == 0 {
		return profileData{}, nil
	}
	p, data, err := s.readProfileVersion(ctx, tx, e.ProfileID)
	if err != nil {
		return profileData{}, err
	}
	if p.MerchantID != e.MerchantID || !hmac.Equal(p.ProfileHash, e.ProfileHash) {
		return profileData{}, ErrConflict
	}
	return data, nil
}

// Check authenticates historical versions, sources and permanent claims in one
// read-only snapshot. Missing ACKs remain unknown; this never enqueues, repairs,
// resolves credentials, or invokes a transport.
func (s *Store) Check(ctx context.Context) (IntegrityCheck, error) {
	var result IntegrityCheck
	if !s.available() || ctx == nil {
		return result, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	for cursor := int64(0); ; {
		ids, e := noticeIDs(ctx, tx, `SELECT id FROM merchant_notice_profiles WHERE id>$1 ORDER BY id LIMIT 100`, cursor)
		if e != nil {
			return result, e
		}
		for _, id := range ids {
			_, _, e = s.readProfileVersion(ctx, tx, id)
			if e != nil {
				if !noticeEvidenceError(e) {
					return result, e
				}
				result.InvalidProfiles++
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	for cursor := int64(0); ; {
		ids, e := noticeIDs(ctx, tx, `SELECT id FROM merchant_notice_events WHERE id>$1 ORDER BY id LIMIT 100`, cursor)
		if e != nil {
			return result, e
		}
		for _, id := range ids {
			record, _, readErr := s.readEvent(ctx, tx, id)
			if readErr == nil {
				_, readErr = s.verifyEventProfile(ctx, tx, record)
			}
			if readErr != nil {
				if !noticeEvidenceError(readErr) {
					return result, readErr
				}
				result.InvalidEvents++
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	for cursor := int64(0); ; {
		ids, e := noticeIDs(ctx, tx, `SELECT id FROM merchant_notice_claims WHERE id>$1 ORDER BY id LIMIT 100`, cursor)
		if e != nil {
			return result, e
		}
		for _, id := range ids {
			var eventID int64
			var kind Transport
			if e = tx.QueryRow(ctx, `SELECT event_id,transport FROM merchant_notice_claims WHERE id=$1`, id).Scan(&eventID, &kind); e != nil {
				return result, e
			}
			claim, state, readErr := s.readClaim(ctx, tx, eventID, kind)
			if readErr == nil {
				event, data, sourceErr := s.readEvent(ctx, tx, eventID)
				readErr = sourceErr
				if readErr == nil {
					var profile profileData
					profile, readErr = s.verifyEventProfile(ctx, tx, event)
					route := false
					for _, candidate := range data.Routes {
						if candidate == kind {
							route = true
						}
					}
					if readErr == nil && (!route || claim.ID != id || claim.JobID != event.JobID || claim.ProfileID != event.ProfileID || !hmac.Equal(claim.EventHash, event.Hash) || !hmac.Equal(claim.ProfileHash, event.ProfileHash) || !hmac.Equal(claim.RecipientHash, s.digest("recipient", []byte(recipientFor(profile.Targets, kind))))) {
						readErr = ErrConflict
					}
				}
			}
			if readErr != nil {
				if !noticeEvidenceError(readErr) {
					return result, readErr
				}
				result.InvalidDeliveries++
			} else if state == "unknown" {
				result.UnknownDeliveries++
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	var profiles, events, deliveries int64
	err = tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM merchant_notice_current c LEFT JOIN merchant_notice_profiles p ON p.id=c.profile_id WHERE p.id IS NULL OR (p.merchant_id,p.revision,p.profile_hash) IS DISTINCT FROM (c.merchant_id,c.revision,c.profile_hash) OR c.revision<>(SELECT max(q.revision) FROM merchant_notice_profiles q WHERE q.merchant_id=c.merchant_id))
 +(SELECT count(DISTINCT p.merchant_id) FROM merchant_notice_profiles p WHERE NOT EXISTS(SELECT 1 FROM merchant_notice_current c WHERE c.merchant_id=p.merchant_id))
 +(SELECT count(*) FROM backoffice_audit a WHERE a.resource='merchant-notice-profiles' AND NOT EXISTS(SELECT 1 FROM merchant_notice_profiles p WHERE p.id=a.resource_id)),
 (SELECT count(*) FROM backoffice_audit a WHERE a.resource='merchant-notice-events' AND NOT EXISTS(SELECT 1 FROM merchant_notice_events e WHERE e.id=a.resource_id))
 +(SELECT count(*) FROM merchant_notice_timer_sources t WHERE NOT EXISTS(SELECT 1 FROM merchant_notice_events e WHERE e.timer_id=t.id))
 +(SELECT count(*) FROM backoffice_audit a WHERE a.resource='merchant-notice-timers' AND NOT EXISTS(SELECT 1 FROM merchant_notice_timer_sources t WHERE t.id=a.resource_id))
 +(SELECT count(*) FROM jobs j WHERE j.kind='merchant.notice' AND NOT EXISTS(SELECT 1 FROM merchant_notice_events e WHERE e.job_id=j.id)),
 (SELECT count(*) FROM backoffice_audit a WHERE a.resource='merchant-notice-deliveries' AND NOT EXISTS(SELECT 1 FROM merchant_notice_claims c WHERE c.id=a.resource_id))
 +(SELECT count(*) FROM merchant_notice_results r WHERE NOT EXISTS(SELECT 1 FROM merchant_notice_claims c WHERE c.id=r.claim_id))`).Scan(&profiles, &events, &deliveries)
	if err != nil {
		return result, err
	}
	result.InvalidProfiles += profiles
	result.InvalidEvents += events
	result.InvalidDeliveries += deliveries
	return result, tx.Commit(ctx)
}
