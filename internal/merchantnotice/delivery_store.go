package merchantnotice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Account is short-lived input to the internal transport constructor. Resolving
// it must be pure: token acquisition and all network work belong to Send, after
// the permanent delivery claim has committed. None of this data is an API view.
type Account struct {
	Template                                        WechatTemplateConfig `json:"-"`
	WXAppID, WebhookURL, QQBotAppID, QQBotAppSecret string               `json:"-"`
	Input                                           Input                `json:"-"`
}

func (Account) String() string   { return "merchantnotice.Account(redacted)" }
func (Account) GoString() string { return "merchantnotice.Account(redacted)" }

type Resolver interface {
	ResolveMerchantNotice(context.Context, Transport, Account) (Sender, error)
}
type claimRecord struct {
	ID, EventID, JobID, ProfileID                                     int64
	Transport                                                         Transport
	EventHash, ProfileHash, RecipientHash, ScopeHash, LeaseHash, Hash []byte
	Transaction                                                       string
	CreatedAt                                                         time.Time
}
type resultRecord struct {
	ClaimID            int64
	ClaimHash          []byte
	State, Transaction string
	CreatedAt          time.Time
	Hash               []byte
}

func (s *Store) claimProof(c claimRecord) []byte {
	raw, _ := json.Marshal([]any{c.ID, c.EventID, c.JobID, c.ProfileID, c.Transport, hex.EncodeToString(c.EventHash), hex.EncodeToString(c.ProfileHash), hex.EncodeToString(c.RecipientHash), hex.EncodeToString(c.ScopeHash), hex.EncodeToString(c.LeaseHash), c.Transaction, c.CreatedAt.UTC().Format(time.RFC3339Nano)})
	return s.digest("delivery-claim", raw)
}
func (s *Store) resultProof(r resultRecord) []byte {
	raw, _ := json.Marshal([]any{r.ClaimID, hex.EncodeToString(r.ClaimHash), r.State, r.Transaction, r.CreatedAt.UTC().Format(time.RFC3339Nano)})
	return s.digest("delivery-result", raw)
}
func claimAudit(c claimRecord) []byte {
	raw, _ := json.Marshal(map[string]string{"claim_hash": hex.EncodeToString(c.Hash), "event_id": fmt.Sprint(c.EventID), "job_id": fmt.Sprint(c.JobID), "transport": string(c.Transport)})
	return raw
}
func resultAudit(r resultRecord) []byte {
	raw, _ := json.Marshal(map[string]string{"claim_hash": hex.EncodeToString(r.ClaimHash), "state": r.State, "result_hash": hex.EncodeToString(r.Hash)})
	return raw
}
func (s *Store) readClaim(ctx context.Context, tx pgx.Tx, event int64, kind Transport) (claimRecord, string, error) {
	var c claimRecord
	err := tx.QueryRow(ctx, `SELECT id,event_id,job_id,profile_id,transport,event_hash,profile_hash,recipient_hash,scope_hash,lease_hash,claim_hash,transaction_id::text,created_at FROM merchant_notice_claims WHERE event_id=$1 AND transport=$2`, event, kind).Scan(&c.ID, &c.EventID, &c.JobID, &c.ProfileID, &c.Transport, &c.EventHash, &c.ProfileHash, &c.RecipientHash, &c.ScopeHash, &c.LeaseHash, &c.Hash, &c.Transaction, &c.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, "", ErrNotFound
	}
	if err != nil {
		return c, "", err
	}
	if !hmac.Equal(c.Hash, s.claimProof(c)) {
		return c, "", ErrConflict
	}
	var valid bool
	if err = tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='merchant-notice-deliveries' AND resource_id=$1 AND operation='attempt' AND (actor_role,actor_id,actor_name)=('system',0,'merchant-notice') AND changes=$2::jsonb`, c.ID, claimAudit(c)).Scan(&valid); err != nil {
		return c, "", err
	}
	if !valid {
		return c, "", ErrConflict
	}
	var r resultRecord
	var at *time.Time
	var resultAudits int
	// Result and audit absence must come from ONE MVCC snapshot. Otherwise a
	// concurrent ACK can commit between two absence reads and look like damage.
	err = tx.QueryRow(ctx, `SELECT COALESCE(r.claim_id,0),r.claim_hash,COALESCE(r.state,''),COALESCE(r.transaction_id::text,''),r.created_at,r.result_hash,
 (SELECT count(*) FROM backoffice_audit WHERE resource='merchant-notice-deliveries' AND resource_id=c.id AND operation='result')
 FROM merchant_notice_claims c LEFT JOIN merchant_notice_results r ON r.claim_id=c.id WHERE c.id=$1`, c.ID).Scan(&r.ClaimID, &r.ClaimHash, &r.State, &r.Transaction, &at, &r.Hash, &resultAudits)
	if err != nil {
		return c, "", err
	}
	if r.ClaimID == 0 {
		if resultAudits != 0 {
			return c, "", ErrConflict
		}
		return c, "unknown", nil
	}
	if at == nil || resultAudits != 1 {
		return c, "", ErrConflict
	}
	r.CreatedAt = *at
	if !hmac.Equal(c.Hash, r.ClaimHash) || !hmac.Equal(r.Hash, s.resultProof(r)) || r.State != "accepted" && r.State != "unknown" && r.State != "not_sent" {
		return c, "", ErrConflict
	}
	if err = tx.QueryRow(ctx, `SELECT count(*)=1 FROM backoffice_audit WHERE resource='merchant-notice-deliveries' AND resource_id=$1 AND operation='result' AND (actor_role,actor_id,actor_name)=('system',0,'merchant-notice') AND changes=$2::jsonb`, c.ID, resultAudit(r)).Scan(&valid); err != nil {
		return c, "", err
	}
	if !valid {
		return c, "", ErrConflict
	}
	return c, r.State, nil
}
func recipientFor(t storedTargets, kind Transport) string {
	switch kind {
	case SMTP:
		return t.Email
	case Wechat:
		return t.WXUID
	case Robot:
		return t.MsgRobotURL
	case Telegram:
		return t.TelegramUID
	case QQBot:
		return t.QQBotOpenID
	case Voice:
		return t.VoiceDeviceID
	}
	return ""
}
func validTransport(t Transport) bool {
	switch t {
	case Wechat, SMTP, SMS, Robot, Telegram, QQBot, Voice:
		return true
	}
	return false
}

func (s *Store) Deliver(ctx context.Context, jobID int64, lease string, kind Transport, global GlobalSwitches, resolver Resolver) error {
	if !s.available() || ctx == nil || jobID <= 0 || len(lease) != 32 || !validTransport(kind) || resolver == nil {
		return ErrInvalid
	}
	if _, err := hex.DecodeString(lease); err != nil {
		return ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var event int64
	if err = tx.QueryRow(ctx, `SELECT e.id FROM jobs j JOIN merchant_notice_events e ON j.id=e.job_id AND j.event_key='merchant-notice:'||e.id::text AND j.payload=jsonb_build_object('event_id',e.id::text) WHERE j.id=$1 AND j.kind='merchant.notice' AND j.state='running' AND j.lease_token=$2 AND j.lease_until>clock_timestamp() FOR UPDATE OF j`, jobID, lease).Scan(&event); err != nil {
		return ErrState
	}
	source, data, err := s.readEvent(ctx, tx, event)
	if err != nil {
		return err
	}
	old, _, err := s.readClaim(ctx, tx, event, kind)
	if err == nil {
		if old.JobID != jobID || old.ProfileID != source.ProfileID || !hmac.Equal(old.EventHash, source.Hash) || !hmac.Equal(old.ProfileHash, source.ProfileHash) {
			return ErrConflict
		}
		// Even an unknown or lost response consumes this source/transport forever.
		// Do not resolve current credentials, targets, or another sender on replay.
		return tx.Commit(ctx)
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}
	var orphan bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='merchant-notice-deliveries' AND operation='attempt' AND changes->>'event_id'=$1 AND changes->>'transport'=$2)`, fmt.Sprint(event), kind).Scan(&orphan); err != nil {
		return err
	}
	if orphan {
		return ErrConflict
	}
	active, _, err := noticeMerchant(ctx, tx, source.MerchantID, true)
	if err != nil {
		return err
	}
	if !active || source.SnapshotState != "captured" {
		return ErrState
	}
	profile, prefs, err := s.readProfile(ctx, tx, source.MerchantID)
	if err != nil {
		return err
	}
	if profile.ID != source.ProfileID || !hmac.Equal(profile.ProfileHash, source.ProfileHash) {
		return ErrConflict
	}
	recipient := recipientFor(prefs.Targets, kind)
	recipients := map[Transport]bool{kind: recipient != ""}
	configured := map[Transport]bool{kind: true}
	routes, err := SelectMerchant(SelectionInput{MerchantID: source.MerchantID, Scene: data.Input.Scene, Subject: data.Input.Params["name"], Mode: prefs.Preferences.mode(data.Input.Scene), Amount: data.Source.Amount, OrderMinimum: prefs.Preferences.OrderMoney, VoiceOrder: prefs.VoiceOrder, Global: global, Recipients: recipients, Configured: configured})
	if err != nil {
		return err
	}
	ready := false
	for _, route := range routes {
		if route.Transport == kind && route.Ready {
			ready = true
		}
	}
	if !ready {
		return ErrState
	}
	// Freeze the rendered value before handing any reference-valued source
	// input to a transport constructor. Factories cannot rewrite this message.
	message, err := Render(data.Input)
	if err != nil {
		return err
	}
	account := Account{WXAppID: prefs.Targets.WXAppID, WebhookURL: prefs.Targets.MsgRobotURL, QQBotAppID: prefs.Targets.QQBotAppID, QQBotAppSecret: prefs.Targets.QQBotAppSecret, Input: data.Input}
	if kind == Wechat {
		account.Template, err = s.wechatTemplateSettings(ctx, tx, data.Input.Scene)
		if err != nil {
			return err
		}
	}
	sender, err := resolver.ResolveMerchantNotice(ctx, kind, account)
	if err != nil || sender == nil || sender.NoticeTransport() != kind || sender.NoticeScope() == "" || len(sender.NoticeScope()) > 4096 || sender.ValidateNoticeRecipient(recipient) != nil {
		return ErrState
	}
	if kind == Wechat || kind == QQBot {
		expected := prefs.Targets.WXAppID
		if kind == QQBot {
			expected = prefs.Targets.QQBotAppID
		}
		bound, ok := sender.(AccountBoundSender)
		if !ok || expected == "" || bound.NoticeAccountID() != expected {
			return ErrConflict
		}
	}
	leaseHash := sha256.Sum256([]byte(lease))
	c := claimRecord{EventID: event, JobID: jobID, ProfileID: profile.ID, Transport: kind, EventHash: source.Hash, ProfileHash: profile.ProfileHash, RecipientHash: s.digest("recipient", []byte(recipient)), ScopeHash: s.digest("transport-scope", []byte(sender.NoticeScope())), LeaseHash: leaseHash[:]}
	if err = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('merchant_notice_claims','id')),clock_timestamp(),pg_current_xact_id()::text`).Scan(&c.ID, &c.CreatedAt, &c.Transaction); err != nil {
		return err
	}
	c.Hash = s.claimProof(c)
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_notice_claims(id,event_id,job_id,profile_id,transport,event_hash,profile_hash,recipient_hash,scope_hash,lease_hash,claim_hash,transaction_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::xid8,$13)`, c.ID, c.EventID, c.JobID, c.ProfileID, c.Transport, c.EventHash, c.ProfileHash, c.RecipientHash, c.ScopeHash, c.LeaseHash, c.Hash, c.Transaction, c.CreatedAt); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'merchant-notice','merchant-notice-deliveries',$1,'attempt',$2)`, c.ID, claimAudit(c)); err != nil {
		return err
	}
	var alive bool
	if err = tx.QueryRow(ctx, `SELECT state='running' AND lease_token=$2 AND lease_until>clock_timestamp() FROM jobs WHERE id=$1`, jobID, lease).Scan(&alive); err != nil {
		return err
	}
	if !alive {
		return ErrState
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	state := "not_sent"
	// Recheck immediately before I/O. A revoke or deadline after claiming never
	// frees another sending attempt, and a late response is saved independently.
	if ctx.Err() == nil {
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchants m JOIN merchant_notice_current p ON p.merchant_id=m.id JOIN jobs j ON j.id=$3 WHERE m.id=$1 AND m.enabled AND p.profile_id=$2 AND j.state='running' AND j.lease_token=$4 AND j.lease_until>clock_timestamp())`, source.MerchantID, profile.ID, jobID, lease).Scan(&alive)
		if err == nil && alive {
			state = "accepted"
			if sender.SendMerchantNotice(ctx, recipient, message) != nil {
				state = "unknown"
			}
		}
	}
	work, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.recordResult(work, c, state)
}
func (s *Store) recordResult(ctx context.Context, c claimRecord, state string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// Serialize only against this permanent claim, not a replacement job lease
	// or current merchant profile. Changes after sending cannot erase its ACK.
	var id int64
	if err = tx.QueryRow(ctx, `SELECT id FROM merchant_notice_claims WHERE id=$1 FOR UPDATE`, c.ID).Scan(&id); err != nil {
		return err
	}
	stored, _, err := s.readClaim(ctx, tx, c.EventID, c.Transport)
	if err != nil || !hmac.Equal(stored.Hash, c.Hash) {
		return ErrConflict
	}
	var exists bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchant_notice_results WHERE claim_id=$1)`, c.ID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return tx.Commit(ctx)
	}
	r := resultRecord{ClaimID: c.ID, ClaimHash: c.Hash, State: state}
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp(),pg_current_xact_id()::text`).Scan(&r.CreatedAt, &r.Transaction); err != nil {
		return err
	}
	r.Hash = s.resultProof(r)
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_notice_results(claim_id,claim_hash,state,result_hash,transaction_id,created_at) VALUES($1,$2,$3,$4,$5::xid8,$6)`, r.ClaimID, r.ClaimHash, r.State, r.Hash, r.Transaction, r.CreatedAt); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'merchant-notice','merchant-notice-deliveries',$1,'result',$2)`, c.ID, resultAudit(r)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
