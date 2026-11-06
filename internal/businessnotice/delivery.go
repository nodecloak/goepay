package businessnotice

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"goepay/internal/businesssms"
	"goepay/internal/merchantnotice"
	"goepay/internal/noticeconfig"
	"time"
)

type Account struct {
	SMS231        *businesssms.Resolved   `json:"-"`
	Managed231    bool                    `json:"-"`
	Credential231 noticeconfig.Credential `json:"-"`
	Revision231   int64                   `json:"-"`
	Settings      map[string]string       `json:"-"`
	Input         merchantnotice.Input    `json:"-"`
}

func (Account) String() string   { return "businessnotice.Account(redacted)" }
func (Account) GoString() string { return "businessnotice.Account(redacted)" }

type Route struct {
	Sender    merchantnotice.Sender `json:"-"`
	Recipient string                `json:"-"`
}

func (Route) String() string   { return "businessnotice.Route(redacted)" }
func (Route) GoString() string { return "businessnotice.Route(redacted)" }

type Resolver interface {
	ResolveBusinessNotice(context.Context, merchantnotice.Transport, Account) (Route, error)
}
type AccountResolver interface {
	BusinessNoticeAccountID(context.Context, merchantnotice.Transport) (string, error)
}

func (s *Store) HandleJob(ctx context.Context, job int64, lease string, resolver Resolver) error {
	if !s.available() || ctx == nil || job <= 0 || len(lease) != 32 || resolver == nil {
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
	var id int64
	if err = tx.QueryRow(ctx, `SELECT e.id FROM jobs j JOIN admin_business_notice_events e ON e.job_id=j.id AND j.event_key='admin-business-notice:'||e.id::text AND j.payload=jsonb_build_object('event_id',e.id::text) WHERE j.id=$1 AND j.kind='admin.business-notice' AND j.state='running' AND j.lease_token=$2 AND j.lease_until>clock_timestamp() FOR UPDATE OF j`, job, lease).Scan(&id); err != nil {
		return ErrState
	}
	event, data, err := s.readEvent(ctx, tx, id)
	if err != nil {
		return err
	}
	if _, _, err = s.readClaim(ctx, tx, event); err == nil {
		return tx.Commit(ctx)
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	var orphan bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM backoffice_audit WHERE resource='admin-business-notice-deliveries' AND resource_id=$1 AND operation='attempt')`, id).Scan(&orphan); err != nil {
		return err
	}
	if orphan {
		return ErrConflict
	}
	values, err := ReadSettings(ctx, tx)
	if err != nil {
		return err
	}
	key, _ := SettingKey(event.Scene)
	mode := "0"
	if value, ok := values[key]; ok {
		mode = value
	}
	kind, err := ModeFor(event.Scene, event.Mode)
	if err != nil || kind == "" {
		return ErrState
	}
	blocked := mode != event.Mode
	// Render before a pure constructor receives any mutable map reference.
	message, err := merchantnotice.Render(data.Input)
	if err != nil {
		return ErrConflict
	}
	var route Route
	if !blocked {
		expected := ""
		account := Account{Settings: values, Input: data.Input}
		if kind == merchantnotice.SMS {
			_, c, managed, stamp, e := s.accounts231.EffectiveTx(ctx, tx, "robot")
			if e != nil {
				return e
			}
			if !managed || data.Version < 2 || data.SMS231 == nil || data.NoticeFingerprint231["robot"] != stamp {
				return ErrState
			}
			resolved, e := s.sms231.ResolveAdminTx(ctx, tx, c.Phone, "complain")
			if e != nil {
				return e
			}
			if resolved == nil || resolved.Snapshot != *data.SMS231 {
				return ErrState
			}
			account.SMS231 = resolved
		}
		if kind == merchantnotice.Robot || kind == merchantnotice.Telegram || kind == merchantnotice.QQBot {
			v, c, managed, stamp, e := s.accounts231.EffectiveTx(ctx, tx, string(kind))
			if e != nil {
				return e
			}
			if managed && (!v.Enabled || data.Version < 2 || data.NoticeRevision231[string(kind)] != v.Revision || data.NoticeFingerprint231[string(kind)] != stamp) {
				return ErrState
			}
			account.Managed231 = managed
			account.Credential231 = c
			account.Revision231 = v.Revision
			if managed && kind == merchantnotice.QQBot {
				expected = c.AppID
			}
		}
		if kind == merchantnotice.QQBot && !account.Managed231 {
			identity, ok := resolver.(AccountResolver)
			if !ok {
				return ErrState
			}
			expected, err = identity.BusinessNoticeAccountID(ctx, kind)
			if err != nil || expected == "" {
				return ErrState
			}
		}
		if resolved, ok := resolver.(interface {
			ResolveBusinessNoticeTx231(context.Context, pgx.Tx, merchantnotice.Transport, Account) (Route, error)
		}); ok {
			route, err = resolved.ResolveBusinessNoticeTx231(ctx, tx, kind, account)
		} else {
			route, err = resolver.ResolveBusinessNotice(ctx, kind, account)
		}
		if err != nil || route.Sender == nil || route.Sender.NoticeTransport() != kind || route.Sender.NoticeScope() == "" || len(route.Sender.NoticeScope()) > 4096 || route.Sender.ValidateNoticeRecipient(route.Recipient) != nil {
			return ErrState
		}
		if kind == merchantnotice.QQBot {
			bound, ok := route.Sender.(merchantnotice.AccountBoundSender)
			if !ok || bound.NoticeAccountID() != expected {
				return ErrConflict
			}
		}
	}
	scope := "blocked-mode-change"
	if !blocked {
		scope = route.Sender.NoticeScope()
	}
	leaseHash := sha256.Sum256([]byte(lease))
	c := claimRecord{EventID: id, JobID: job, MerchantID: event.MerchantID, Transport: kind, Blocked: blocked, RecipientHash: s.digest("recipient", route.Recipient), ScopeHash: s.digest("scope", scope), LeaseHash: leaseHash[:], EventProof: event.Proof}
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp(),pg_current_xact_id()::text`).Scan(&c.At, &c.Transaction); err != nil {
		return err
	}
	c.At = c.At.UTC()
	c.Proof = s.claimProof(c)
	var merchant any
	if c.MerchantID > 0 {
		merchant = c.MerchantID
	}
	if _, err = tx.Exec(ctx, `INSERT INTO admin_business_notice_claims(event_id,job_id,merchant_id,transport,blocked,recipient_hash,scope_hash,lease_hash,created_at,transaction_id,proof_hash) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::xid8,$11)`, c.EventID, c.JobID, merchant, c.Transport, c.Blocked, c.RecipientHash, c.ScopeHash, c.LeaseHash, c.At, c.Transaction, c.Proof); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'business-notice','admin-business-notice-deliveries',$1,'attempt',$2::jsonb)`, id, claimAudit(c)); err != nil {
		return err
	}
	var alive bool
	if err = tx.QueryRow(ctx, `SELECT state='running' AND lease_token=$2 AND lease_until>clock_timestamp() FROM jobs WHERE id=$1`, job, lease).Scan(&alive); err != nil {
		return err
	}
	if !alive {
		return ErrState
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	state := "not_sent"
	if !blocked && ctx.Err() == nil {
		// A stopped/replaced lease after claiming cannot regain sending authority.
		err = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM jobs WHERE id=$1 AND state='running' AND lease_token=$2 AND lease_until>clock_timestamp())`, job, lease).Scan(&alive)
		if err == nil && alive {
			state = "accepted"
			if route.Sender.SendMerchantNotice(ctx, route.Recipient, message) != nil {
				state = "unknown"
			}
		}
	}
	ack, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.recordResult(ack, c, state)
}

type claimRecord struct {
	EventID, JobID, MerchantID                             int64
	Transport                                              merchantnotice.Transport
	Blocked                                                bool
	RecipientHash, ScopeHash, LeaseHash, Proof, EventProof []byte
	At                                                     time.Time
	Transaction                                            string
}
type resultRecord struct {
	EventID            int64
	ClaimHash          []byte
	State, Transaction string
	At                 time.Time
	Proof              []byte
}

func (s *Store) claimProof(c claimRecord) []byte {
	return s.digest("claim", []any{c.EventID, c.JobID, c.MerchantID, c.Transport, c.Blocked, c.RecipientHash, c.ScopeHash, c.LeaseHash, c.EventProof, c.At.UTC().Format(time.RFC3339Nano), c.Transaction})
}
func (s *Store) resultProof(r resultRecord) []byte {
	return s.digest("result", []any{r.EventID, r.ClaimHash, r.State, r.Transaction, r.At.UTC().Format(time.RFC3339Nano)})
}
func claimAudit(c claimRecord) []byte {
	raw, _ := json.Marshal(map[string]string{"proof": hex.EncodeToString(c.Proof), "job_id": fmt.Sprint(c.JobID), "transport": string(c.Transport)})
	return raw
}
func resultAudit(r resultRecord) []byte {
	raw, _ := json.Marshal(map[string]string{"claim_hash": hex.EncodeToString(r.ClaimHash), "proof": hex.EncodeToString(r.Proof), "state": r.State})
	return raw
}
