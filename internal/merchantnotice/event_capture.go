package merchantnotice

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
)

func (p Preferences) mode(scene string) Mode {
	switch scene {
	case "order":
		return p.Order
	case "settle":
		return p.Settle
	case "login":
		return p.Login
	case "complain":
		return p.Complain
	case "mchrisk":
		return p.MchRisk
	case "balance":
		return p.Balance
	case "group":
		return p.Group
	case "domain_audit":
		return p.DomainAudit
	case "daily_report":
		return p.DailyReport
	}
	return 0
}

// All publishers share encryption, profile capture, deduplication and queue
// construction. Public HTTP input cannot call this publisher or select a source.
func (s *Store) appendEvent(ctx context.Context, tx pgx.Tx, r eventRecord, data eventData, eligible bool) error {
	r.SnapshotState = "excluded"
	var prefs profileData
	if eligible {
		profile, body, err := s.readProfile(ctx, tx, r.MerchantID)
		switch {
		case err == nil:
			r.ProfileID = profile.ID
			r.ProfileHash = profile.ProfileHash
			r.SnapshotState = "captured"
			prefs = body
		case errors.Is(err, ErrNotFound):
			r.SnapshotState = "absent"
		case errors.Is(err, ErrConflict):
			r.SnapshotState = "unavailable"
		default:
			return err
		}
	}
	var err error
	if err = tx.QueryRow(ctx, `SELECT nextval(pg_get_serial_sequence('merchant_notice_events','id')),clock_timestamp(),pg_current_xact_id()::text`).Scan(&r.ID, &r.CreatedAt, &r.Transaction); err != nil {
		return err
	}
	name, err := noticeSiteName(ctx, tx)
	if err != nil {
		return err
	}
	data.Input.SiteName = name
	if r.Scene != data.Input.Scene {
		return ErrInvalid
	}
	if _, err = Render(data.Input); err != nil {
		return err
	}
	if r.SnapshotState == "captured" {
		all := map[Transport]bool{Wechat: true, SMTP: true, SMS: true, Robot: true, Telegram: true, QQBot: true, Voice: true}
		recipients := make(map[Transport]bool, len(all))
		for kind := range all {
			recipients[kind] = recipientFor(prefs.Targets, kind) != ""
		}
		routes, e := SelectMerchant(SelectionInput{MerchantID: r.MerchantID, Scene: r.Scene, Subject: data.Input.Params["name"], Mode: prefs.Preferences.mode(r.Scene), Amount: data.Source.Amount, OrderMinimum: prefs.Preferences.OrderMoney, VoiceOrder: prefs.VoiceOrder, Global: GlobalSwitches{MailRobot: true, Telegram: true, QQBot: true, Voice: true, GroupSMS: true}, Recipients: recipients, Configured: all})
		if e != nil {
			return e
		}
		for _, route := range routes {
			if route.Ready {
				data.Routes = append(data.Routes, route.Transport)
			}
		}
	}
	if len(data.Routes) > 0 {
		if err = tx.QueryRow(ctx, `INSERT INTO jobs(event_key,kind,payload) VALUES('merchant-notice:'||$1::bigint::text,'merchant.notice',jsonb_build_object('event_id',$1::bigint::text)) RETURNING id`, r.ID).Scan(&r.JobID); err != nil {
			return err
		}
	}
	raw, err := json.Marshal(data)
	if err != nil || len(raw) > 60000 {
		return ErrInvalid
	}
	r.BodyHash = s.digest("event-body", raw)
	nonce := make([]byte, s.aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return err
	}
	r.Cipher = s.aead.Seal(nonce, nonce, raw, eventAAD(r))
	r.Hash = s.eventProof(r)
	var profileID, orderID, loginID, jobID, settlementID, timerID, domainID any
	if r.DomainAuditID > 0 {
		domainID = r.DomainAuditID
	}
	if r.TimerID > 0 {
		timerID = r.TimerID
	}
	if r.SettlementID > 0 {
		settlementID = r.SettlementID
	}
	if r.JobID > 0 {
		jobID = r.JobID
	}
	if r.ProfileID > 0 {
		profileID = r.ProfileID
	}
	if r.OrderID > 0 {
		orderID = r.OrderID
	}
	if r.LoginAuditID > 0 {
		loginID = r.LoginAuditID
	}
	if _, err = tx.Exec(ctx, `INSERT INTO merchant_notice_events(id,merchant_id,order_id,login_audit_id,scene,profile_id,profile_hash,snapshot_state,payload_cipher,body_hash,event_hash,source_transaction,created_at,job_id,settlement_id,timer_id,domain_audit_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::xid8,$13,$14,$15,$16,$17)`, r.ID, r.MerchantID, orderID, loginID, r.Scene, profileID, r.ProfileHash, r.SnapshotState, r.Cipher, r.BodyHash, r.Hash, r.Transaction, r.CreatedAt, jobID, settlementID, timerID, domainID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('system',0,'merchant-notice','merchant-notice-events',$1,'create',$2)`, r.ID, eventAudit(r)); err != nil {
		return err
	}
	return err
}
