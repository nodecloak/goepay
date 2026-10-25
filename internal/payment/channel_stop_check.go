package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (s *Store) checkChannelStops(ctx context.Context, tx pgx.Tx, report *LedgerCheck) error {
	for cursor := int64(0); ; {
		ids, e := sharingEvidenceIDsPage(ctx, tx, `SELECT id FROM(SELECT channel_id id FROM channel_stop_heads UNION SELECT channel_id FROM channel_stop_events) anchors WHERE id>$1 ORDER BY id LIMIT 100`, cursor)
		if e != nil {
			return e
		}
		for _, id := range ids {
			history, e := s.readChannelStops(ctx, tx, id)
			if e == nil {
				for _, v := range history {
					if v.Action == "pause" {
						if _, e = s.channelNoticeState(ctx, tx, v.ID); e != nil {
							break
						}
					}
				}
			}
			if e != nil {
				if invalidSharingEvidence(e) || errors.Is(e, pgx.ErrNoRows) || errors.Is(e, ErrInvalid) {
					report.InvalidChannelStops++
				} else {
					return e
				}
			}
			cursor = id
		}
		if len(ids) < 100 {
			break
		}
	}
	var orphan int64
	e := tx.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM backoffice_audit b WHERE b.resource='channel-stops' AND NOT EXISTS(SELECT 1 FROM channel_stop_events e WHERE e.id=b.resource_id))+
 (SELECT count(*) FROM jobs j WHERE j.kind='admin.channel-stopped' AND NOT EXISTS(SELECT 1 FROM channel_stop_events e WHERE e.action='pause' AND j.event_key='channel-stop:'||e.id::text AND j.payload=jsonb_build_object('event_id',e.id::text)))+
 (SELECT count(*) FROM backoffice_audit b WHERE b.resource='channel-stop-notices' AND NOT EXISTS(SELECT 1 FROM channel_stop_notice_claims c WHERE c.event_id=b.resource_id))+
 (SELECT count(*) FROM channel_stop_notice_claims c WHERE NOT EXISTS(SELECT 1 FROM channel_stop_events e WHERE e.id=c.event_id AND e.action='pause'))`).Scan(&orphan)
	if e != nil {
		return e
	}
	report.InvalidChannelStops += orphan
	return nil
}
