package businessnotice

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

type IntegrityCheck struct {
	InvalidEvents     int64 `json:"invalid_admin_business_notice_events"`
	InvalidDeliveries int64 `json:"invalid_admin_business_notice_deliveries"`
	UnknownDeliveries int64 `json:"unknown_admin_business_notice_deliveries"`
}

func (s *Store) Check(ctx context.Context) (IntegrityCheck, error) {
	var out IntegrityCheck
	if !s.available() || ctx == nil {
		return out, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	for cursor := int64(0); ; {
		rows, err := tx.Query(ctx, `SELECT id FROM admin_business_notice_events WHERE id>$1 ORDER BY id LIMIT 100`, cursor)
		if err != nil {
			return out, err
		}
		ids := []int64{}
		for rows.Next() {
			var id int64
			if err = rows.Scan(&id); err != nil {
				rows.Close()
				return out, err
			}
			ids = append(ids, id)
		}
		rows.Close()
		if err = rows.Err(); err != nil {
			return out, err
		}
		for _, id := range ids {
			r, _, e := s.readEvent(ctx, tx, id)
			if e != nil {
				if !evidenceError(e) {
					return out, e
				}
				out.InvalidEvents++
				cursor = id
				continue
			}
			_, state, e := s.readClaim(ctx, tx, r)
			if e != nil && !errors.Is(e, ErrNotFound) {
				if !evidenceError(e) {
					return out, e
				}
				out.InvalidDeliveries++
			} else if e == nil && state == "unknown" {
				out.UnknownDeliveries++
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	var missingEvents, missingDeliveries int64
	err = tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM backoffice_audit a WHERE a.business_notice_scene IS NOT NULL AND NOT EXISTS(SELECT 1 FROM admin_business_notice_events e WHERE e.audit_id=a.id))
 +(SELECT count(*) FROM notification_sources231 n WHERE n.kind IN('complain','channellimit') AND NOT EXISTS(SELECT 1 FROM backoffice_audit a JOIN admin_business_notice_events e ON e.audit_id=a.id WHERE a.resource='notification-sources231' AND a.resource_id=n.id AND a.actor_name='notification-source231'))
 +(SELECT count(*) FROM ledger_request_events a WHERE a.business_notice_transaction IS NOT NULL AND NOT EXISTS(SELECT 1 FROM admin_business_notice_events e WHERE e.request_event_id=a.id))
 +(SELECT count(*) FROM backoffice_audit a WHERE a.resource='admin-business-notice-events' AND NOT EXISTS(SELECT 1 FROM admin_business_notice_events e WHERE e.id=a.resource_id))
 +(SELECT count(*) FROM jobs j WHERE j.kind='admin.business-notice' AND NOT EXISTS(SELECT 1 FROM admin_business_notice_events e WHERE e.job_id=j.id)),
 (SELECT count(*) FROM backoffice_audit a WHERE a.resource='admin-business-notice-deliveries' AND NOT EXISTS(SELECT 1 FROM admin_business_notice_claims c WHERE c.event_id=a.resource_id))
 +(SELECT count(*) FROM admin_business_notice_claims c WHERE NOT EXISTS(SELECT 1 FROM admin_business_notice_events e WHERE e.id=c.event_id))
 +(SELECT count(*) FROM admin_business_notice_results r WHERE NOT EXISTS(SELECT 1 FROM admin_business_notice_claims c WHERE c.event_id=r.event_id))`).Scan(&missingEvents, &missingDeliveries)
	if err != nil {
		return out, err
	}
	out.InvalidEvents += missingEvents
	out.InvalidDeliveries += missingDeliveries
	return out, tx.Commit(ctx)
}
func evidenceError(err error) bool {
	return errors.Is(err, ErrInvalid) || errors.Is(err, ErrConflict) || errors.Is(err, ErrState) || errors.Is(err, ErrNotFound) || errors.Is(err, pgx.ErrNoRows)
}
