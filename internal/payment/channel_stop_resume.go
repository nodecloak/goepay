package payment

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// ChannelResumeSnapshot is private funds-confirmation material. The proof
// binds the exact authenticated history, not an editable channel record.
func (s *Store) ChannelResumeSnapshot(ctx context.Context, q referralReader, channel, event, revision int64) (json.RawMessage, error) {
	history, e := s.readChannelStops(ctx, q, channel)
	if e != nil {
		return nil, e
	}
	if len(history) == 0 {
		return nil, ErrConflict
	}
	v := history[len(history)-1]
	if v.Action != "pause" || v.ID != event || v.Revision != revision {
		return nil, ErrConflict
	}
	return json.Marshal(struct {
		ChannelID int64  `json:"channel_id,string"`
		EventID   int64  `json:"event_id,string"`
		Revision  int64  `json:"revision,string"`
		Code      string `json:"code"`
		Proof     []byte `json:"proof"`
	}{channel, event, revision, v.Receipt.Code, v.Proof})
}

func channelResumeSession(ctx context.Context, tx pgx.Tx, token string, actor SettlementPlanActor) error {
	var alive bool
	e := tx.QueryRow(ctx, `SELECT true FROM console_sessions ss JOIN console_users u ON (u.username,u.role,u.subject_id,u.auth_version)=(ss.username,ss.role,ss.subject_id,ss.auth_version) WHERE ss.token_hash=$1 AND u.username=$2 AND u.role='admin' AND u.subject_id=$3 AND u.admin_class='super' AND u.enabled AND ss.expires_at>clock_timestamp() AND NOT EXISTS(SELECT 1 FROM console_session_issuers i WHERE i.token_hash=ss.token_hash) FOR SHARE OF ss,u`, lookupSessionHash(token), actor.Name, actor.ID).Scan(&alive)
	if e != nil || !alive {
		return ErrInvalid
	}
	return nil
}

func (s *Store) ResumeChannel(ctx context.Context, channel, event, revision int64, token string, actor SettlementPlanActor) (ChannelWriteStatus, error) {
	out := ChannelWriteStatus{ChannelID: channel}
	guard, _ := ctx.Value(fundsActionGuardKey{}).(FundsActionGuard)
	if channel <= 0 || event <= 0 || revision <= 0 || revision >= 4096 || len(token) != 43 || !settlementPlanAdminValid(actor) || guard == nil {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('channel-stop:'||$1::bigint::text,76))`, channel); e != nil {
		return out, e
	}
	if e = channelResumeSession(ctx, tx, token, actor); e != nil {
		return out, e
	}
	history, e := s.readChannelStops(ctx, tx, channel)
	if e != nil {
		return out, e
	}
	if len(history) == 0 {
		return out, ErrConflict
	}
	previous := history[len(history)-1]
	if previous.ID != event || previous.Revision != revision || previous.Action != "pause" {
		return out, ErrConflict
	}
	if e = guard(ctx, tx, "channel-resume", fmt.Sprintf("%d:%d:%d", channel, event, revision)); e != nil {
		return out, e
	}
	v := previous
	v.Revision++
	v.Action = "resume"
	v.PreviousID = &previous.ID
	v.PreviousHash = previous.Proof
	v.ActorRole, v.ActorID, v.ActorName, v.ActorClass = actor.Role, actor.ID, actor.Name, actor.Class
	if e = tx.QueryRow(ctx, `SELECT nextval('channel_stop_events_id_seq'),clock_timestamp(),pg_current_xact_id()::text`).Scan(&v.ID, &v.CreatedAt, &v.TransactionID); e != nil {
		return out, e
	}
	v.Proof, e = s.channelStopProof(v)
	if e != nil {
		return out, e
	}
	raw, e := json.Marshal(v.Receipt)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO channel_stop_events(id,channel_id,revision,previous_id,previous_hash,action,request_id,transfer_id,claim_sequence,receipt,account_hash,actor_role,actor_id,actor_name,actor_class,created_at,transaction_id,proof_hash)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17::xid8,$18)`, v.ID, v.ChannelID, v.Revision, v.PreviousID, v.PreviousHash, v.Action, v.RequestID, v.TransferID, v.ClaimSequence, raw, v.AccountHash, v.ActorRole, v.ActorID, v.ActorName, v.ActorClass, v.CreatedAt, v.TransactionID, v.Proof); e != nil {
		return out, e
	}
	if e = channelStopAudit(ctx, tx, v); e != nil {
		return out, e
	}
	tag, e := tx.Exec(ctx, `UPDATE channel_stop_heads SET event_id=$2,revision=$3,proof_hash=$4 WHERE channel_id=$1 AND event_id=$5 AND revision=$6`, channel, v.ID, v.Revision, v.Proof, event, revision)
	if e != nil {
		return out, e
	}
	if tag.RowsAffected() != 1 {
		return out, ErrConflict
	}
	if _, e = s.readChannelStops(ctx, tx, channel); e != nil {
		return out, e
	}
	out.NoticeState, e = s.channelNoticeState(ctx, tx, previous.ID)
	if e != nil {
		return out, e
	}
	if e = channelResumeSession(ctx, tx, token, actor); e != nil {
		return out, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, e
	}
	out.EventID, out.Revision, out.Code, out.ChangedAt = v.ID, v.Revision, v.Receipt.Code, &v.CreatedAt
	return out, nil
}
