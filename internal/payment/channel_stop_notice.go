package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"time"
)

type ChannelNoticeSender interface {
	SendChannelStopped(context.Context, string, int64, int64, string) error
}

type channelNoticeClaim struct {
	EventID, JobID            int64
	RecipientHash, LeaseHash  []byte
	CreatedAt                 time.Time
	TransactionID             string
	Proof                     []byte
	State                     string
	RouteVersion              int
	RouteMode                 string
	RouteScopeHash, RouteHash []byte
}

func (s *Store) channelNoticeProof(v channelNoticeClaim) ([]byte, error) {
	if !s.validChannelNoticeRoute(v) {
		return nil, ErrConflict
	}
	if v.RouteVersion == 1 {
		return s.referralDigest("channel-stop77/notice-claim", []any{v.EventID, v.JobID, v.RecipientHash, v.LeaseHash, v.CreatedAt.UTC().Format(time.RFC3339Nano), v.TransactionID, v.RouteVersion, v.RouteMode, v.RouteScopeHash, v.RouteHash})
	}
	return s.referralDigest("channel-stop76/notice-claim", []any{v.EventID, v.JobID, v.RecipientHash, v.LeaseHash, v.CreatedAt.UTC().Format(time.RFC3339Nano), v.TransactionID})
}
func channelNoticeChanges(v channelNoticeClaim) []byte {
	raw, _ := json.Marshal(map[string]string{"proof": hex.EncodeToString(v.Proof), "job_id": fmt.Sprint(v.JobID)})
	return raw
}
func (s *Store) readChannelNoticeClaim(ctx context.Context, q referralReader, event int64) (channelNoticeClaim, error) {
	var v channelNoticeClaim
	// JSON projection keeps actual schema76 legacy fixtures readable. A new
	// versioned proof with missing columns still fails its HMAC; it never falls
	// back to a legacy proof. New route inserts require migration77 columns.
	e := q.QueryRow(ctx, `SELECT event_id,job_id,recipient_hash,lease_hash,created_at,transaction_id::text,proof_hash,COALESCE((to_jsonb(c)->>'route_version')::int,0),COALESCE(to_jsonb(c)->>'route_mode',''),(to_jsonb(c)->>'route_scope_hash')::bytea,(to_jsonb(c)->>'route_hash')::bytea FROM channel_stop_notice_claims c WHERE event_id=$1`, event).Scan(&v.EventID, &v.JobID, &v.RecipientHash, &v.LeaseHash, &v.CreatedAt, &v.TransactionID, &v.Proof, &v.RouteVersion, &v.RouteMode, &v.RouteScopeHash, &v.RouteHash)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, ErrNotFound
	}
	if e != nil {
		return v, e
	}
	proof, e := s.channelNoticeProof(v)
	if e != nil || !hmac.Equal(proof, v.Proof) || v.JobID <= 0 || v.CreatedAt.IsZero() || v.TransactionID == "" || len(v.RecipientHash) != 32 || len(v.LeaseHash) != 32 {
		return v, ErrConflict
	}
	var valid bool
	e = q.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='channel-stop-notices' AND resource_id=$1 AND operation='attempt' AND (actor_role,actor_id,actor_name)=('system',0,'channel-notice') AND changes=$2::jsonb`, event, channelNoticeChanges(v)).Scan(&valid)
	if e != nil {
		return v, e
	}
	if !valid {
		return v, ErrConflict
	}
	if e = q.QueryRow(ctx, `SELECT count(*)=1 FROM jobs j JOIN channel_stop_events e ON e.id=$1 AND e.action='pause' WHERE j.id=$2 AND j.kind='admin.channel-stopped' AND j.event_key='channel-stop:'||e.id::text AND j.payload=jsonb_build_object('event_id',e.id::text) AND e.created_at<=$3`, event, v.JobID, v.CreatedAt).Scan(&valid); e != nil {
		return v, e
	}
	if !valid {
		return v, ErrConflict
	}
	v.State = "attempted"
	rows, e := q.Query(ctx, `SELECT operation,changes,actor_role,actor_id,actor_name FROM backoffice_audit WHERE resource='channel-stop-notices' AND resource_id=$1 AND operation<>'attempt' ORDER BY id LIMIT 2`, event)
	if e != nil {
		return v, e
	}
	count := 0
	for rows.Next() {
		count++
		var state, role, name string
		var actor int64
		var raw []byte
		if e = rows.Scan(&state, &raw, &role, &actor, &name); e != nil {
			rows.Close()
			return v, e
		}
		var fields map[string]string
		if count > 1 || (state != "accepted" && state != "unknown") || role != "system" || actor != 0 || name != "channel-notice" || json.Unmarshal(raw, &fields) != nil || len(fields) != 4 || fields["claim_proof"] != hex.EncodeToString(v.Proof) || fields["transaction_id"] == "" {
			rows.Close()
			return v, ErrConflict
		}
		at, e := time.Parse(time.RFC3339Nano, fields["created_at"])
		if e != nil || at.Before(v.CreatedAt) {
			rows.Close()
			return v, ErrConflict
		}
		proof, e := s.referralDigest("channel-stop76/notice-result", []any{v.Proof, state, at.UTC().Format(time.RFC3339Nano), fields["transaction_id"]})
		if e != nil || fields["proof"] != hex.EncodeToString(proof) {
			rows.Close()
			return v, ErrConflict
		}
		v.State = state
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return v, e
	}
	var audits int
	if e = q.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='channel-stop-notices' AND resource_id=$1`, event).Scan(&audits); e != nil {
		return v, e
	}
	if audits != count+1 {
		return v, ErrConflict
	}
	return v, nil
}

func (s *Store) channelNoticeState(ctx context.Context, q referralReader, event int64) (string, error) {
	state, _, e := s.channelNoticeStatus(ctx, q, event)
	return state, e
}

func (s *Store) channelNoticeStatus(ctx context.Context, q referralReader, event int64) (string, string, error) {
	v, e := s.readChannelNoticeClaim(ctx, q, event)
	if e == nil {
		mode := v.RouteMode
		if v.RouteVersion == 0 {
			mode = "smtp"
		}
		return v.State, mode, nil
	}
	if !errors.Is(e, ErrNotFound) {
		return "", "", e
	}
	var orphan bool
	if e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='channel-stop-notices' AND resource_id=$1)`, event).Scan(&orphan); e != nil {
		return "", "", e
	}
	if orphan {
		return "", "", ErrConflict
	}
	return "pending", "", nil
}

// DeliverChannelStopNotice commits a permanent, recipient-bound claim before
// any transport I/O. A crash or uncertain response never opens another send chance.
func (s *Store) DeliverChannelStopNotice(ctx context.Context, jobID int64, leaseToken, recipient string, sender ChannelNoticeSender) error {
	if ctx == nil || jobID <= 0 || len(leaseToken) != 32 || sender == nil {
		return ErrInvalid
	}
	if _, e := hex.DecodeString(leaseToken); e != nil {
		return ErrInvalid
	}
	configured, e := s.channelNoticeRoute(recipient, sender)
	if e != nil {
		return e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var event, channel int64
	e = tx.QueryRow(ctx, `SELECT e.id,e.channel_id FROM jobs j JOIN channel_stop_events e ON j.event_key='channel-stop:'||e.id::text AND j.payload=jsonb_build_object('event_id',e.id::text) WHERE j.id=$1 AND j.kind='admin.channel-stopped' AND e.action='pause' AND j.state='running' AND j.lease_token=$2 AND j.lease_until>clock_timestamp() FOR UPDATE OF j`, jobID, leaseToken).Scan(&event, &channel)
	if e != nil {
		return ErrInvalid
	}
	history, e := s.readChannelStops(ctx, tx, channel)
	if e != nil {
		return e
	}
	var source channelStopEvent
	for _, v := range history {
		if v.ID == event {
			source = v
		}
	}
	if source.ID == 0 || source.Action != "pause" {
		return ErrConflict
	}
	if guarded, ok := sender.(interface {
		ValidateNoticeConfig231(context.Context, pgx.Tx, time.Time) error
	}); ok {
		if e = guarded.ValidateNoticeConfig231(ctx, tx, source.CreatedAt); e != nil {
			return e
		}
	}
	old, e := s.readChannelNoticeClaim(ctx, tx, event)
	if e == nil {
		if !hmac.Equal(old.RecipientHash, configured.RecipientHash) || old.JobID != jobID || !sameChannelNoticeRoute(old, configured) {
			return ErrConflict
		}
		return nil
	}
	if !errors.Is(e, ErrNotFound) {
		return e
	}
	var orphan bool
	if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='channel-stop-notices' AND resource_id=$1)`, event).Scan(&orphan); e != nil {
		return e
	}
	if orphan {
		return ErrConflict
	}
	leaseHash := sha256.Sum256([]byte(leaseToken))
	v := configured
	v.EventID, v.JobID, v.LeaseHash = event, jobID, leaseHash[:]
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp(),pg_current_xact_id()::text`).Scan(&v.CreatedAt, &v.TransactionID); e != nil {
		return e
	}
	v.Proof, e = s.channelNoticeProof(v)
	if e != nil {
		return e
	}
	if v.RouteVersion == 1 {
		_, e = tx.Exec(ctx, `INSERT INTO channel_stop_notice_claims(event_id,job_id,recipient_hash,lease_hash,created_at,transaction_id,proof_hash,route_version,route_mode,route_scope_hash,route_hash)VALUES($1,$2,$3,$4,$5,$6::xid8,$7,$8,$9,$10,$11)`, v.EventID, v.JobID, v.RecipientHash, v.LeaseHash, v.CreatedAt, v.TransactionID, v.Proof, v.RouteVersion, v.RouteMode, v.RouteScopeHash, v.RouteHash)
	} else {
		_, e = tx.Exec(ctx, `INSERT INTO channel_stop_notice_claims(event_id,job_id,recipient_hash,lease_hash,created_at,transaction_id,proof_hash)VALUES($1,$2,$3,$4,$5,$6::xid8,$7)`, v.EventID, v.JobID, v.RecipientHash, v.LeaseHash, v.CreatedAt, v.TransactionID, v.Proof)
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'channel-notice','channel-stop-notices',$1,'attempt',$2)`, event, channelNoticeChanges(v)); e != nil {
		return e
	}
	var alive bool
	if e = tx.QueryRow(ctx, `SELECT state='running' AND lease_token=$2 AND lease_until>clock_timestamp() FROM jobs WHERE id=$1`, jobID, leaseToken).Scan(&alive); e != nil {
		return e
	}
	if !alive {
		return ErrState
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	state := "accepted"
	if sender.SendChannelStopped(ctx, recipient, channel, event, source.Receipt.Code) != nil {
		state = "unknown"
	}
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.recordChannelNoticeResult(work, v, state)
}

func (s *Store) recordChannelNoticeResult(ctx context.Context, v channelNoticeClaim, state string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	stored, e := s.readChannelNoticeClaim(ctx, tx, v.EventID)
	if e != nil || !hmac.Equal(stored.Proof, v.Proof) {
		return ErrConflict
	}
	var at time.Time
	var xid string
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp(),pg_current_xact_id()::text`).Scan(&at, &xid); e != nil {
		return e
	}
	proof, e := s.referralDigest("channel-stop76/notice-result", []any{v.Proof, state, at.UTC().Format(time.RFC3339Nano), xid})
	if e != nil {
		return e
	}
	raw, _ := json.Marshal(map[string]string{"claim_proof": hex.EncodeToString(v.Proof), "proof": hex.EncodeToString(proof), "created_at": at.UTC().Format(time.RFC3339Nano), "transaction_id": xid})
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('system',0,'channel-notice','channel-stop-notices',$1,$2,$3)`, v.EventID, state, raw); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
