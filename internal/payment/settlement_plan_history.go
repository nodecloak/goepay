package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Historical evidence is checked by immutable row ID. Later definition,
// recipient or preference changes must neither erase nor invalidate history.
func (s *Store) readSettlementPlanDefinition(ctx context.Context, q fundsReader, id int64) (settlementPlanRecord, error) {
	var r settlementPlanRecord
	var raw []byte
	var audited bool
	err := q.QueryRow(ctx, `SELECT v.plan_id,v.revision,v.id,v.name,v.mode,v.schedule,v.merchant_ids,v.actor_id,v.actor_name,v.proof_hash,v.created_at,
 EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='settlement-plan-versions' AND a.resource_id=v.id AND a.operation='define' AND (a.actor_role,a.actor_id,a.actor_name)=('admin',v.actor_id,v.actor_name)) FROM settlement_plan_versions v WHERE v.id=$1`, id).Scan(&r.View.ID, &r.View.Revision, &r.DefinitionID, &r.Input.Name, &r.Input.Mode, &raw, &r.Input.MerchantIDs, &r.ActorID, &r.ActorName, &r.Proof, &r.View.CreatedAt, &audited)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if json.Unmarshal(raw, &r.Input.Schedule) != nil {
		return r, ErrConflict
	}
	r.Input, err = normalizeSettlementPlan(r.Input)
	if err != nil {
		return r, ErrConflict
	}
	expected, err := s.settlementPlanProof(r)
	if err != nil || !hmac.Equal(expected, r.Proof) || !audited {
		return r, ErrConflict
	}
	r.View.Name, r.View.Mode, r.View.Schedule = r.Input.Name, r.Input.Mode, r.Input.Schedule
	return r, nil
}

func (s *Store) readSettlementPreferenceVersion(ctx context.Context, q referralReader, id int64) (settlementPreferenceRecord, error) {
	var r settlementPreferenceRecord
	var audited bool
	err := q.QueryRow(ctx, `SELECT p.id,p.merchant_id,p.revision,p.enabled,COALESCE(p.recipient_config_id,0),p.actor_name,p.created_at,p.proof_hash,
 EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='settlement-schedule-preferences' AND a.resource_id=p.id AND a.operation='configure' AND (a.actor_role,a.actor_id,a.actor_name)=('merchant',p.merchant_id,p.actor_name)) FROM settlement_schedule_preferences p WHERE p.id=$1`, id).Scan(&r.ID, &r.View.MerchantID, &r.View.Revision, &r.View.Enabled, &r.View.RecipientConfigID, &r.ActorName, &r.CreatedAt, &r.Proof, &audited)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	expected, err := s.settlementPreferenceProof(r)
	if err != nil || !hmac.Equal(expected, r.Proof) || !audited {
		return r, ErrConflict
	}
	if r.View.RecipientConfigID > 0 {
		var c recipientRecord
		err = q.QueryRow(ctx, "SELECT id,merchant_id,recipient_type,app_id,identity_hash,recipient_cipher FROM settlement_recipient_configs WHERE id=$1 AND merchant_id=$2", r.View.RecipientConfigID, r.View.MerchantID).Scan(&c.ID, &c.MerchantID, &c.Type, &c.AppID, &c.Hash, &c.Cipher)
		if errors.Is(err, pgx.ErrNoRows) {
			return r, ErrConflict
		}
		if err != nil {
			return r, err
		}
		in, e := s.decryptSettlementRecipient("config", c.ID, c.Cipher)
		if e != nil {
			return r, ErrConflict
		}
		hash, e := s.settlementRecipientHash(in)
		if e != nil || !hmac.Equal(hash, c.Hash) || in.Type != c.Type || in.AppID != c.AppID {
			return r, ErrConflict
		}
		r.View.Recipient = recipientView("configured", c.ID, in)
	}
	return r, nil
}
