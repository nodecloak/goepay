package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantpolicy"
)

type settlementScopeMerchant struct {
	MerchantID                   int64                              `json:"merchant_id,string"`
	PreferenceID                 int64                              `json:"preference_id,string"`
	PreferenceRevision           int64                              `json:"preference_revision,string"`
	PreferenceHash               []byte                             `json:"preference_hash"`
	RecipientConfigID            int64                              `json:"recipient_config_id,string"`
	RecipientHash                []byte                             `json:"recipient_hash"`
	Recipient                    SettlementRecipientView            `json:"recipient"`
	Enabled                      bool                               `json:"enabled"`
	BaseGroupID                  int64                              `json:"base_group_id,string"`
	Unresolved                   bool                               `json:"group_unresolved"`
	Threshold                    *settlementThresholdMerchantPolicy `json:"threshold,omitempty"`
	SettlementPolicy             *SettlementPolicy                  `json:"settlement_policy,omitempty"`
	SettlementPermissionRevision int64                              `json:"settlement_permission_revision,string,omitempty"`
}

type settlementPlanScope struct {
	DefinitionID   int64                      `json:"definition_id,string"`
	DefinitionHash []byte                     `json:"definition_hash"`
	Policy         SettlementPolicy           `json:"policy"`
	SettleType     string                     `json:"settle_type"`
	Merchants      []settlementScopeMerchant  `json:"merchants"`
	Threshold      *settlementThresholdConfig `json:"threshold,omitempty"`
}

type settlementPlanAuthorizationRecord struct {
	View      SettlementPlanAuthorizationView
	Scope     settlementPlanScope
	ActorID   int64
	ActorName string
	Proof     []byte
}

func validateSettlementPlanAuthorization(in SettlementPlanAuthorizationInput) error {
	if in.ExpectedAuthorizationID < 0 || !validSettlementScheduleTime(in.ValidFrom) || !validSettlementScheduleTime(in.ExpiresAt) || in.ValidFrom.Nanosecond()%1000 != 0 || in.ExpiresAt.Nanosecond()%1000 != 0 || !in.ExpiresAt.After(in.ValidFrom) || in.ExpiresAt.Sub(in.ValidFrom) > 31*24*time.Hour {
		return ErrInvalid
	}
	if in.MaxItemGross < 1 || in.MaxItemGross > 100000000000 || in.MaxRunGross < in.MaxItemGross || in.MaxRunGross > 100000000000 || in.MaxDayGross < in.MaxRunGross || in.MaxDayGross > 100000000000 || in.MaxDayItems < 1 || in.MaxDayItems > 100 {
		return ErrInvalid
	}
	return nil
}

func (s *Store) settlementPlanStaticScope(ctx context.Context, q referralReader, p settlementPlanRecord) (settlementPlanScope, error) {
	out := settlementPlanScope{DefinitionID: p.DefinitionID, DefinitionHash: p.Proof, Merchants: []settlementScopeMerchant{}}
	var err error
	var thresholdAt time.Time
	if p.Input.Mode == settlementThresholdMode {
		cfg, e := readSettlementThresholdConfig(ctx, q, false)
		if e != nil {
			return out, e
		}
		if !cfg.Enabled {
			return out, ErrState
		}
		out.Threshold = &cfg
		out.Policy = SettlementPolicy{FeeRateBPS: cfg.Policy.FeeRateBPS, FeeMaximumCents: cfg.Policy.FeeMaximumCents}
		out.SettleType = cfg.SettleType
		if err = q.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&thresholdAt); err != nil {
			return out, err
		}
	} else {
		out.Policy, err = readSettlementPolicy(ctx, q, false)
		if err != nil {
			return out, err
		}
		if err = validateScheduledSettlementPolicy(out.Policy); err != nil {
			return out, err
		}
		err = q.QueryRow(ctx, `SELECT COALESCE((SELECT body->>'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'='settle_type'),'0')`).Scan(&out.SettleType)
		if err != nil {
			return out, err
		}
	}
	if out.SettleType != "0" && out.SettleType != "1" {
		return out, ErrInvalid
	}
	for _, mid := range p.Input.MerchantIDs {
		v := settlementScopeMerchant{MerchantID: mid}
		if err = q.QueryRow(ctx, "SELECT enabled,base_group_id,group_baseline_unresolved FROM merchants WHERE id=$1", mid).Scan(&v.Enabled, &v.BaseGroupID, &v.Unresolved); err != nil {
			return out, err
		}
		if !v.Enabled || v.Unresolved {
			return out, ErrState
		}
		permission, e := merchantpolicy.ReadSettlementPermission(ctx, q, mid, false)
		if e != nil {
			return out, e
		}
		if !permission.Allowed {
			return out, ErrState
		}
		v.SettlementPermissionRevision = permission.Revision
		pref, e := s.readSettlementPreference(ctx, q, mid)
		if e != nil {
			return out, e
		}
		if !pref.View.Enabled || pref.View.RecipientConfigID <= 0 {
			return out, ErrState
		}
		config, in, e := s.readSettlementConfig(ctx, q, mid)
		if e != nil {
			return out, e
		}
		if config.ID != pref.View.RecipientConfigID {
			return out, ErrConflict
		}
		v.PreferenceID, v.PreferenceRevision, v.PreferenceHash = pref.ID, pref.View.Revision, pref.Proof
		v.RecipientConfigID, v.RecipientHash, v.Recipient = config.ID, config.Hash, recipientView("configured", config.ID, in)
		if out.Threshold != nil {
			if in.Type != "alipay" {
				return out, ErrState
			}
			policy, e := readSettlementThresholdMerchantPolicy(ctx, q, mid, out.Threshold.Policy, thresholdAt)
			if e != nil {
				return out, e
			}
			v.Threshold = &policy
		} else {
			policy, e := readMerchantSettlementPolicy(ctx, q, mid, false)
			if e != nil {
				return out, e
			}
			if e = validateScheduledSettlementPolicy(policy); e != nil {
				return out, e
			}
			v.SettlementPolicy = &policy
		}
		out.Merchants = append(out.Merchants, v)
	}
	return out, nil
}

func (s *Store) SettlementPlanAuthorizationSnapshot(ctx context.Context, q referralReader, id, expected int64, in SettlementPlanAuthorizationInput) (json.RawMessage, error) {
	if q == nil || id <= 0 || expected <= 0 || validateSettlementPlanAuthorization(in) != nil {
		return nil, ErrInvalid
	}
	p, err := s.readSettlementPlan(ctx, q, id)
	if err != nil {
		return nil, err
	}
	if p.View.Revision != expected || p.View.LastAuthorizationID != in.ExpectedAuthorizationID {
		return nil, ErrConflict
	}
	scope, err := s.settlementPlanStaticScope(ctx, q, p)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Plan  SettlementPlanView               `json:"plan"`
		Input SettlementPlanAuthorizationInput `json:"authorization_input"`
		Scope settlementPlanScope              `json:"scope"`
	}{p.View, in, scope})
}

func (s *Store) settlementPlanAuthorizationProof(r settlementPlanAuthorizationRecord) ([]byte, error) {
	in := r.View.SettlementPlanAuthorizationInput
	return s.referralDigest("settlement-plan-authorization", []any{r.View.ID, r.View.PlanID, r.View.PlanRevision, in.ExpectedAuthorizationID, in.ValidFrom.UTC().Format(time.RFC3339Nano), in.ExpiresAt.UTC().Format(time.RFC3339Nano), in.MaxItemGross, in.MaxRunGross, in.MaxDayGross, in.MaxDayItems, r.Scope, r.ActorID, r.ActorName, r.View.CreatedAt.UTC().Format(time.RFC3339Nano)})
}

func (s *Store) readSettlementPlanAuthorization(ctx context.Context, q fundsReader, id int64) (settlementPlanAuthorizationRecord, error) {
	var r settlementPlanAuthorizationRecord
	var raw []byte
	var audited bool
	err := q.QueryRow(ctx, `SELECT a.id,a.plan_id,a.plan_revision,COALESCE(a.expected_authorization_id,0),a.valid_from,a.expires_at,a.max_item_gross,a.max_run_gross,a.max_day_gross,a.max_day_items,a.scope_snapshot,a.actor_id,a.actor_name,a.proof_hash,a.created_at,
 EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-plan-authorizations' AND b.resource_id=a.id AND b.operation='authorize' AND (b.actor_role,b.actor_id,b.actor_name)=('admin',a.actor_id,a.actor_name))
 FROM settlement_plan_authorizations a WHERE a.id=$1`, id).Scan(&r.View.ID, &r.View.PlanID, &r.View.PlanRevision, &r.View.ExpectedAuthorizationID, &r.View.ValidFrom, &r.View.ExpiresAt, &r.View.MaxItemGross, &r.View.MaxRunGross, &r.View.MaxDayGross, &r.View.MaxDayItems, &raw, &r.ActorID, &r.ActorName, &r.Proof, &r.View.CreatedAt, &audited)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	if json.Unmarshal(raw, &r.Scope) != nil || validateSettlementPlanAuthorization(r.View.SettlementPlanAuthorizationInput) != nil {
		return r, ErrConflict
	}
	proof, err := s.settlementPlanAuthorizationProof(r)
	if err != nil || !hmac.Equal(proof, r.Proof) || !audited {
		return r, ErrConflict
	}
	definition, err := s.readSettlementPlanDefinition(ctx, q, r.Scope.DefinitionID)
	if err != nil {
		return r, err
	}
	if definition.View.ID != r.View.PlanID || definition.View.Revision != r.View.PlanRevision || !hmac.Equal(definition.Proof, r.Scope.DefinitionHash) || len(r.Scope.Merchants) != len(definition.Input.MerchantIDs) {
		return r, ErrConflict
	}
	if (definition.Input.Mode == settlementThresholdMode) != (r.Scope.Threshold != nil) {
		return r, ErrConflict
	}
	if r.Scope.Threshold != nil {
		v := r.Scope.Threshold
		if !v.Enabled || v.ChannelID <= 0 || v.Policy.validate() != nil || v.SettleType != r.Scope.SettleType || r.Scope.Policy != (SettlementPolicy{FeeRateBPS: v.Policy.FeeRateBPS, FeeMaximumCents: v.Policy.FeeMaximumCents}) {
			return r, ErrConflict
		}
	}
	for i, m := range r.Scope.Merchants {
		if m.MerchantID != definition.Input.MerchantIDs[i] || m.PreferenceID <= 0 || m.PreferenceRevision <= 0 || m.RecipientConfigID <= 0 || len(m.PreferenceHash) != 32 || len(m.RecipientHash) != 32 || !m.Enabled || m.Unresolved {
			return r, ErrConflict
		}
		if (r.Scope.Threshold != nil) != (m.Threshold != nil) {
			return r, ErrConflict
		}
		if m.SettlementPolicy != nil {
			if r.Scope.Threshold != nil || validateScheduledSettlementPolicy(*m.SettlementPolicy) != nil {
				return r, ErrConflict
			}
			globalFields := *m.SettlementPolicy
			globalFields.FeeRateBPS = r.Scope.Policy.FeeRateBPS
			if globalFields != r.Scope.Policy {
				return r, ErrConflict
			}
		}
		if m.Threshold != nil {
			v := m.Threshold
			if m.Recipient.Type != "alipay" || v.MerchantID != m.MerchantID || v.MerchantRevision <= 0 || v.GroupID < 0 || v.Policy.validate() != nil || v.Policy.ThresholdCents != r.Scope.Threshold.Policy.ThresholdCents || v.Policy.FeeMaximumCents != r.Scope.Threshold.Policy.FeeMaximumCents || v.FeeOverrideBPS == nil && v.Policy.FeeRateBPS != r.Scope.Threshold.Policy.FeeRateBPS || v.FeeOverrideBPS != nil && (*v.FeeOverrideBPS != v.Policy.FeeRateBPS || *v.FeeOverrideBPS < 0 || *v.FeeOverrideBPS > 10000) {
				return r, ErrConflict
			}
		}
	}
	var actorID int64
	var actorName string
	var at time.Time
	var revokedProof []byte
	var revokedAudit bool
	err = q.QueryRow(ctx, `SELECT r.actor_id,r.actor_name,r.created_at,r.proof_hash,EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-plan-authorizations' AND b.resource_id=r.authorization_id AND b.operation='revoke' AND (b.actor_role,b.actor_id,b.actor_name)=('admin',r.actor_id,r.actor_name)) FROM settlement_plan_revocations r WHERE r.authorization_id=$1 AND r.plan_id=$2`, id, r.View.PlanID).Scan(&actorID, &actorName, &at, &revokedProof, &revokedAudit)
	if err == nil {
		expected, e := s.referralDigest("settlement-plan-revocation", []any{id, r.View.PlanID, actorID, actorName, at.UTC().Format(time.RFC3339Nano)})
		if e != nil || !hmac.Equal(expected, revokedProof) || !revokedAudit {
			return r, ErrConflict
		}
		r.View.Revoked = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return r, err
	}
	return r, nil
}

func (s *Store) AuthorizeSettlementPlan(ctx context.Context, id, expected int64, in SettlementPlanAuthorizationInput, actor SettlementPlanActor) (SettlementPlanAuthorizationView, error) {
	var out SettlementPlanAuthorizationView
	if !settlementPlanAdminValid(actor) || id <= 0 || expected <= 0 || validateSettlementPlanAuthorization(in) != nil {
		return out, ErrInvalid
	}
	// PostgreSQL persists microseconds. Seal exactly those instants in UTC,
	// independently of the HTTP timestamp offset or pgx's local timezone.
	in.ValidFrom = in.ValidFrom.UTC().Truncate(time.Microsecond)
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	guard, _ := ctx.Value(fundsActionGuardKey{}).(FundsActionGuard)
	if guard == nil {
		return out, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = tx.QueryRow(ctx, "SELECT id FROM settlement_plans WHERE id=$1 FOR UPDATE", id).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	} else if err != nil {
		return out, err
	}
	p, err := s.readSettlementPlan(ctx, tx, id)
	if err != nil {
		return out, err
	}
	if p.View.Revision != expected || p.View.LastAuthorizationID != in.ExpectedAuthorizationID {
		return out, ErrConflict
	}
	rows, err := tx.Query(ctx, "SELECT id FROM merchants WHERE id=ANY($1::bigint[]) ORDER BY id FOR SHARE", p.Input.MerchantIDs)
	if err != nil {
		return out, err
	}
	for rows.Next() {
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	if p.Input.Mode == settlementThresholdMode {
		if _, err = readSettlementThresholdConfig(ctx, tx, true); err != nil {
			return out, err
		}
	} else {
		if _, err = readSettlementPolicy(ctx, tx, true); err != nil {
			return out, err
		}
	}
	scope, err := s.settlementPlanStaticScope(ctx, tx, p)
	if err != nil {
		return out, err
	}
	if err = requireSettlementPlanAutomaticAccess(ctx, tx, p); err != nil {
		return out, err
	}
	var now time.Time
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return out, err
	}
	if !now.Before(in.ExpiresAt) {
		return out, ErrState
	}
	if err = guard(ctx, tx, "settlement-plan-authorize", fmt.Sprintf("%d:%d", id, expected)); err != nil {
		return out, err
	}
	r := settlementPlanAuthorizationRecord{View: SettlementPlanAuthorizationView{PlanID: id, PlanRevision: expected, SettlementPlanAuthorizationInput: in}, Scope: scope, ActorID: actor.ID, ActorName: actor.Name}
	if err = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('settlement_plan_authorizations','id')),transaction_timestamp()").Scan(&r.View.ID, &r.View.CreatedAt); err != nil {
		return out, err
	}
	r.Proof, err = s.settlementPlanAuthorizationProof(r)
	if err != nil {
		return out, err
	}
	raw, err := json.Marshal(scope)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO settlement_plan_authorizations(id,plan_id,plan_revision,expected_authorization_id,valid_from,expires_at,max_item_gross,max_run_gross,max_day_gross,max_day_items,scope_snapshot,actor_id,actor_name,proof_hash,created_at) VALUES($1,$2,$3,NULLIF($4::bigint,0),$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, r.View.ID, id, expected, in.ExpectedAuthorizationID, in.ValidFrom, in.ExpiresAt, in.MaxItemGross, in.MaxRunGross, in.MaxDayGross, in.MaxDayItems, raw, actor.ID, actor.Name, r.Proof, r.View.CreatedAt); err != nil {
		return out, err
	}
	if err = settlementPlanAudit(ctx, tx, "admin", actor.ID, actor.Name, "settlement-plan-authorizations", r.View.ID, "authorize", map[string]string{"plan_id": fmt.Sprint(id), "revision": fmt.Sprint(expected), "mode": p.Input.Mode}); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, "UPDATE settlement_plans SET enabled=true,last_authorization_id=$2 WHERE id=$1", id, r.View.ID); err != nil {
		return out, err
	}
	// Financial confirmation, audit and head triggers may wait. Never make an
	// already-expired or changed original scope durable after those waits.
	current, err := s.settlementPlanStaticScope(ctx, tx, p)
	if err != nil {
		return out, err
	}
	finalScope, err := json.Marshal(current)
	if err != nil {
		return out, err
	}
	if !bytes.Equal(raw, finalScope) {
		return out, ErrConflict
	}
	if err = requireSettlementPlanAutomaticAccess(ctx, tx, p); err != nil {
		return out, err
	}
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
		return out, err
	}
	if !now.Before(in.ExpiresAt) {
		return out, ErrState
	}
	return r.View, tx.Commit(ctx)
}

func (s *Store) RevokeSettlementPlanAuthorization(ctx context.Context, id int64, actor SettlementPlanActor) error {
	if !settlementPlanAdminValid(actor) || id <= 0 {
		return ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var pid int64
	err = tx.QueryRow(ctx, "SELECT plan_id FROM settlement_plan_authorizations WHERE id=$1", id).Scan(&pid)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if err = tx.QueryRow(ctx, "SELECT id FROM settlement_plans WHERE id=$1 FOR UPDATE", pid).Scan(&pid); err != nil {
		return err
	}
	r, err := s.readSettlementPlanAuthorization(ctx, tx, id)
	if err != nil {
		return err
	}
	if r.View.Revoked {
		return tx.Commit(ctx)
	}
	var at time.Time
	// A revoker may wait behind an already valid generation/payout claim.
	// Backdating to transaction start would falsify their causal order.
	if err = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&at); err != nil {
		return err
	}
	proof, err := s.referralDigest("settlement-plan-revocation", []any{id, pid, actor.ID, actor.Name, at.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO settlement_plan_revocations(authorization_id,plan_id,actor_id,actor_name,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,$6)", id, pid, actor.ID, actor.Name, proof, at); err != nil {
		return err
	}
	if err = settlementPlanAudit(ctx, tx, "admin", actor.ID, actor.Name, "settlement-plan-authorizations", id, "revoke", map[string]string{"plan_id": fmt.Sprint(pid)}); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE settlement_plans SET enabled=false WHERE id=$1 AND last_authorization_id=$2", pid, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
