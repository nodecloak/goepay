package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	adapter "goepay/internal/adapters/payment"
)

var ErrChannelStopped = errors.New("channel is stopped for new writes; reconcile original requests")

type ChannelWriteStatus struct {
	ChannelID       int64      `json:"channel_id,string"`
	EventID         int64      `json:"event_id,string"`
	Revision        int64      `json:"revision,string"`
	Blocked         bool       `json:"blocked"`
	Code            string     `json:"code,omitempty"`
	ChangedAt       *time.Time `json:"changed_at,omitempty"`
	NoticeState     string     `json:"notice_state,omitempty"`
	NoticeTransport string     `json:"notice_transport,omitempty"`
}
type channelStopEvent struct {
	ID, ChannelID, Revision              int64
	PreviousID                           *int64
	PreviousHash                         []byte
	Action                               string
	RequestID, TransferID, ClaimSequence int64
	Receipt                              adapter.TransferFailureReceipt
	AccountHash                          []byte
	ActorRole                            string
	ActorID                              int64
	ActorName, ActorClass                string
	CreatedAt                            time.Time
	TransactionID                        string
	Proof                                []byte
}

func (s *Store) channelStopProof(v channelStopEvent) ([]byte, error) {
	return s.referralDigest("channel-stop76/event", []any{v.ID, v.ChannelID, v.Revision, v.PreviousID, v.PreviousHash, v.Action, v.RequestID, v.TransferID, v.ClaimSequence, v.Receipt, v.AccountHash, v.ActorRole, v.ActorID, v.ActorName, v.ActorClass, v.CreatedAt.UTC().Format(time.RFC3339Nano), v.TransactionID})
}
func channelStopChanges(v channelStopEvent) []byte {
	raw, _ := json.Marshal(map[string]string{"channel_id": fmt.Sprint(v.ChannelID), "request_id": fmt.Sprint(v.RequestID), "proof": hex.EncodeToString(v.Proof)})
	return raw
}
func channelStopAudit(ctx context.Context, tx pgx.Tx, v channelStopEvent) error {
	_, e := tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES($1,$2,$3,'channel-stops',$4,$5,$6)`, v.ActorRole, v.ActorID, v.ActorName, v.ID, v.Action, channelStopChanges(v))
	return e
}
func (s *Store) channelStopOrigin(ctx context.Context, q referralReader, v channelStopEvent) error {
	if v.ID <= 0 || v.ChannelID <= 0 || v.Revision <= 0 || v.Revision > 4096 || v.RequestID <= 0 || v.TransferID <= 0 || v.ClaimSequence <= 0 || v.CreatedAt.IsZero() || v.TransactionID == "" || len(v.Proof) != 32 || len(v.AccountHash) != 32 || classifyTransferFailure76(v.Receipt.Code) != transferFailureChannel76 {
		return ErrConflict
	}
	if v.Action == "pause" {
		if v.ActorRole != "system" || v.ActorID != 0 || v.ActorName != "settlement-auto" || v.ActorClass != "" {
			return ErrConflict
		}
	} else if v.Action != "resume" || v.ActorRole != "admin" || v.ActorID <= 0 || v.ActorName == "" || v.ActorClass != "super" {
		return ErrConflict
	}
	proof, e := s.channelStopProof(v)
	if e != nil || !hmac.Equal(proof, v.Proof) {
		return ErrConflict
	}
	p, e := s.settlementPayout(ctx, q, v.RequestID)
	if e != nil {
		return e
	}
	var sequence, authority int64
	var at time.Time
	if e = q.QueryRow(ctx, `SELECT claim_seq,authorization_id,claimed_at FROM settlement_auto_claims WHERE request_id=$1`, v.RequestID).Scan(&sequence, &authority, &at); e != nil {
		return ErrConflict
	}
	if p.SendState != "claimed" || p.TransferID != v.TransferID || p.ChannelID != v.ChannelID || sequence != v.ClaimSequence || v.CreatedAt.Before(at) || v.Receipt.TransferNo != fmt.Sprintf("TF%d", v.TransferID) || v.Receipt.Amount != p.Transfer.Amount || v.Receipt.Type != p.Transfer.Type || v.Receipt.Provider != p.ProviderKind || !observationID68(v.Receipt.ResponseSHA256) {
		return ErrConflict
	}
	a, e := s.readSettlementAuto(ctx, q, authority)
	if e != nil {
		return e
	}
	matched := false
	for _, ch := range a.Scope.Channels {
		if ch.MerchantID == p.MerchantID && ch.ChannelID == p.ChannelID && ch.Provider == v.Receipt.Provider && ch.Product == v.Receipt.Product && ch.AccountScope == v.Receipt.AccountScope {
			matched = true
		}
	}
	if !matched || s.checkTransferAccountScope(ctx, q, v.TransferID, v.Receipt.AccountScope, true) != nil {
		return ErrConflict
	}
	expected, e := s.transferAccountHash(v.Receipt.AccountScope)
	if e != nil || !hmac.Equal(expected, v.AccountHash) {
		return ErrConflict
	}
	switch v.Receipt.Provider {
	case "alipay":
		if v.Receipt.Authentication != "alipay_rsa2" || v.Receipt.Operation != v.Receipt.Product {
			return ErrConflict
		}
	case "wxpay":
		if v.Receipt.Authentication != "wechat_v2_mtls" && v.Receipt.Authentication != "wechat_v2_md5" || v.Receipt.Operation != "/mmpaymkttransfers/promotion/transfers" {
			return ErrConflict
		}
	default:
		return ErrConflict
	}
	var audited bool
	e = q.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='channel-stops' AND resource_id=$1 AND operation=$2 AND (actor_role,actor_id,actor_name)=($3,$4,$5) AND changes=$6::jsonb`, v.ID, v.Action, v.ActorRole, v.ActorID, v.ActorName, channelStopChanges(v)).Scan(&audited)
	if e != nil {
		return e
	}
	if !audited {
		return ErrConflict
	}
	if v.Action == "pause" {
		if e = q.QueryRow(ctx, `SELECT count(*)=1 FROM jobs WHERE event_key=$1 AND kind='admin.channel-stopped' AND payload=jsonb_build_object('event_id',$2::bigint::text)`, fmt.Sprintf("channel-stop:%d", v.ID), v.ID).Scan(&audited); e != nil {
			return e
		}
		if !audited {
			return ErrConflict
		}
	}
	return nil
}

func (s *Store) readChannelStops(ctx context.Context, q referralReader, channel int64) ([]channelStopEvent, error) {
	if channel <= 0 {
		return nil, ErrInvalid
	}
	var head, revision int64
	var headHash []byte
	e := q.QueryRow(ctx, `SELECT event_id,revision,proof_hash FROM channel_stop_heads WHERE channel_id=$1`, channel).Scan(&head, &revision, &headHash)
	if errors.Is(e, pgx.ErrNoRows) {
		var orphan bool
		e = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM channel_stop_events WHERE channel_id=$1) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='channel-stops' AND changes->>'channel_id'=$1::bigint::text)`, channel).Scan(&orphan)
		if e != nil {
			return nil, e
		}
		if orphan {
			return nil, ErrConflict
		}
		return []channelStopEvent{}, nil
	}
	if e != nil {
		return nil, e
	}
	if revision <= 0 || revision > 4096 || len(headHash) != 32 {
		return nil, ErrConflict
	}
	rows, e := q.Query(ctx, `SELECT id,channel_id,revision,previous_id,previous_hash,action,request_id,transfer_id,claim_sequence,receipt,account_hash,actor_role,actor_id,actor_name,actor_class,created_at,transaction_id::text,proof_hash FROM channel_stop_events WHERE channel_id=$1 ORDER BY revision LIMIT 4097`, channel)
	if e != nil {
		return nil, e
	}
	events := []channelStopEvent{}
	for rows.Next() {
		var v channelStopEvent
		var raw []byte
		if e = rows.Scan(&v.ID, &v.ChannelID, &v.Revision, &v.PreviousID, &v.PreviousHash, &v.Action, &v.RequestID, &v.TransferID, &v.ClaimSequence, &raw, &v.AccountHash, &v.ActorRole, &v.ActorID, &v.ActorName, &v.ActorClass, &v.CreatedAt, &v.TransactionID, &v.Proof); e != nil {
			rows.Close()
			return nil, e
		}
		if json.Unmarshal(raw, &v.Receipt) != nil {
			rows.Close()
			return nil, ErrConflict
		}
		events = append(events, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if int64(len(events)) != revision {
		return nil, ErrConflict
	}
	for i, v := range events {
		if v.Revision != int64(i+1) || v.ChannelID != channel {
			return nil, ErrConflict
		}
		if i == 0 {
			if v.PreviousID != nil || len(v.PreviousHash) != 0 || v.Action != "pause" {
				return nil, ErrConflict
			}
		} else {
			previous := events[i-1]
			if v.PreviousID == nil || *v.PreviousID != previous.ID || !hmac.Equal(v.PreviousHash, previous.Proof) || v.Action == previous.Action || v.CreatedAt.Before(previous.CreatedAt) {
				return nil, ErrConflict
			}
			if v.Action == "resume" && (v.RequestID != previous.RequestID || v.TransferID != previous.TransferID || v.ClaimSequence != previous.ClaimSequence || v.Receipt != previous.Receipt || !hmac.Equal(v.AccountHash, previous.AccountHash)) {
				return nil, ErrConflict
			}
		}
		if e = s.channelStopOrigin(ctx, q, v); e != nil {
			return nil, e
		}
		if i > 0 && v.Action == "pause" {
			var fresh bool
			if e = q.QueryRow(ctx, `SELECT claimed_at>$2 FROM settlement_auto_claims WHERE claim_seq=$1`, v.ClaimSequence, events[i-1].CreatedAt).Scan(&fresh); e != nil {
				return nil, e
			}
			if !fresh {
				return nil, ErrConflict
			}
		}
	}
	last := events[len(events)-1]
	if last.ID != head || !hmac.Equal(last.Proof, headHash) {
		return nil, ErrConflict
	}
	var audits int64
	if e = q.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='channel-stops' AND changes->>'channel_id'=$1::bigint::text`, channel).Scan(&audits); e != nil {
		return nil, e
	}
	if audits != revision {
		return nil, ErrConflict
	}
	return events, nil
}
func (s *Store) ChannelWriteStatus(ctx context.Context, channel int64) (ChannelWriteStatus, error) {
	out := ChannelWriteStatus{ChannelID: channel}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	all, e := s.readChannelStops(ctx, tx, channel)
	if e != nil {
		return out, e
	}
	if len(all) > 0 {
		v := all[len(all)-1]
		out.EventID, out.Revision, out.Blocked, out.Code, out.ChangedAt = v.ID, v.Revision, v.Action == "pause", v.Receipt.Code, &v.CreatedAt
		pause := v.ID
		if v.Action == "resume" {
			pause = all[len(all)-2].ID
		}
		out.NoticeState, out.NoticeTransport, e = s.channelNoticeStatus(ctx, tx, pause)
		if e != nil {
			return out, e
		}
	}
	e = tx.Commit(ctx)
	return out, e
}
func (s *Store) checkChannelWrite(ctx context.Context, channel int64) error {
	out, e := s.ChannelWriteStatus(ctx, channel)
	if e != nil {
		return e
	}
	if out.Blocked {
		return ErrChannelStopped
	}
	return nil
}

func (s *Store) recordAutomaticChannelFailure(ctx context.Context, request int64, cause error) error {
	receipt, verified := adapter.VerifiedTransferFailure(cause)
	if !verified || classifyTransferFailure76(receipt.Code) != transferFailureChannel76 {
		return nil
	}
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	tx, e := s.pool.Begin(work)
	if e != nil {
		return e
	}
	defer tx.Rollback(work)
	p, e := s.settlementPayout(work, tx, request)
	if e != nil {
		return e
	}
	if p.SendState != "claimed" || p.TransferID <= 0 || p.Transfer.Amount != receipt.Amount || receipt.TransferNo != fmt.Sprintf("TF%d", p.TransferID) || p.ProviderKind != receipt.Provider {
		return ErrConflict
	}
	if _, e = tx.Exec(work, `SELECT pg_advisory_xact_lock(hashtextextended('channel-stop:'||$1::bigint::text,76))`, p.ChannelID); e != nil {
		return e
	}
	history, e := s.readChannelStops(work, tx, p.ChannelID)
	if e != nil {
		return e
	}
	// A delayed replay of an already recorded response cannot stop a channel
	// again after an explicit administrator resume.
	var recorded bool
	if e = tx.QueryRow(work, `SELECT EXISTS(SELECT 1 FROM channel_stop_events WHERE transfer_id=$1 AND action='pause')`, p.TransferID).Scan(&recorded); e != nil {
		return e
	}
	if recorded {
		return nil
	}
	if len(history) > 0 && history[len(history)-1].Action == "pause" {
		return nil
	}
	var sequence int64
	var claimedAt time.Time
	if e = tx.QueryRow(work, `SELECT claim_seq,claimed_at FROM settlement_auto_claims WHERE request_id=$1`, request).Scan(&sequence, &claimedAt); e != nil {
		return ErrConflict
	}
	// Restoring the channel establishes a new operational boundary. Earlier
	// in-flight responses retain their funds outcome without undoing it.
	if len(history) > 0 && !claimedAt.After(history[len(history)-1].CreatedAt) {
		return nil
	}
	v := channelStopEvent{ChannelID: p.ChannelID, Revision: int64(len(history) + 1), Action: "pause", RequestID: request, TransferID: p.TransferID, Receipt: receipt, ActorRole: "system", ActorName: "settlement-auto"}
	v.ClaimSequence = sequence
	if len(history) > 0 {
		previous := history[len(history)-1]
		v.PreviousID = &previous.ID
		v.PreviousHash = previous.Proof
	}
	v.AccountHash, e = s.transferAccountHash(receipt.AccountScope)
	if e != nil {
		return e
	}
	if e = tx.QueryRow(work, `SELECT nextval('channel_stop_events_id_seq'),clock_timestamp(),pg_current_xact_id()::text`).Scan(&v.ID, &v.CreatedAt, &v.TransactionID); e != nil {
		return e
	}
	v.Proof, e = s.channelStopProof(v)
	if e != nil {
		return e
	}
	raw, e := json.Marshal(v.Receipt)
	if e != nil {
		return e
	}
	if _, e = tx.Exec(work, `INSERT INTO channel_stop_events(id,channel_id,revision,previous_id,previous_hash,action,request_id,transfer_id,claim_sequence,receipt,account_hash,actor_role,actor_id,actor_name,actor_class,created_at,transaction_id,proof_hash)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17::xid8,$18)`, v.ID, v.ChannelID, v.Revision, v.PreviousID, v.PreviousHash, v.Action, v.RequestID, v.TransferID, v.ClaimSequence, raw, v.AccountHash, v.ActorRole, v.ActorID, v.ActorName, v.ActorClass, v.CreatedAt, v.TransactionID, v.Proof); e != nil {
		return e
	}
	if e = channelStopAudit(work, tx, v); e != nil {
		return e
	}
	if len(history) == 0 {
		_, e = tx.Exec(work, `INSERT INTO channel_stop_heads(channel_id,event_id,revision,proof_hash)VALUES($1,$2,$3,$4)`, v.ChannelID, v.ID, v.Revision, v.Proof)
	} else {
		_, e = tx.Exec(work, `UPDATE channel_stop_heads SET event_id=$2,revision=$3,proof_hash=$4 WHERE channel_id=$1`, v.ChannelID, v.ID, v.Revision, v.Proof)
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec(work, `INSERT INTO jobs(event_key,kind,payload,run_at)VALUES($1,'admin.channel-stopped',jsonb_build_object('event_id',$2::bigint::text),clock_timestamp())`, fmt.Sprintf("channel-stop:%d", v.ID), v.ID); e != nil {
		return e
	}
	if _, e = s.readChannelStops(work, tx, v.ChannelID); e != nil {
		return e
	}
	return tx.Commit(work)
}
