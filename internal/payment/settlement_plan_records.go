package payment

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type settlementPlanRecord struct {
	View                  SettlementPlanView
	Input                 SettlementPlanInput
	DefinitionID, ActorID int64
	ActorName             string
	Proof                 []byte
}
type settlementPreferenceRecord struct {
	View      SettlementSchedulePreferenceView
	ID        int64
	ActorName string
	CreatedAt time.Time
	Proof     []byte
}

func settlementPlanAdminValid(a SettlementPlanActor) bool {
	return a.Role == "admin" && a.Class == "super" && a.ID > 0 && sharingSafeField(a.Name, 128, true)
}
func settlementPreferenceOwner(mid int64, a SettlementPlanActor) bool {
	return mid > 0 && a.Role == "merchant" && a.ID == mid && sharingSafeField(a.Name, 128, true)
}
func normalizeSettlementPlan(in SettlementPlanInput) (SettlementPlanInput, error) {
	if !sharingSafeField(in.Name, 100, true) || validateSettlementPlanSchedule(in.Schedule, in.Mode) != nil || len(in.MerchantIDs) < 1 || len(in.MerchantIDs) > 100 {
		return in, ErrInvalid
	}
	in.Schedule.AnchorAt = in.Schedule.AnchorAt.UTC()
	in.MerchantIDs = append([]int64(nil), in.MerchantIDs...)
	sort.Slice(in.MerchantIDs, func(i, j int) bool { return in.MerchantIDs[i] < in.MerchantIDs[j] })
	for i, id := range in.MerchantIDs {
		if id <= 0 || i > 0 && id == in.MerchantIDs[i-1] {
			return in, ErrInvalid
		}
	}
	return in, nil
}
func (s *Store) settlementPlanProof(r settlementPlanRecord) ([]byte, error) {
	return s.referralDigest("settlement-plan-definition", []any{r.DefinitionID, r.View.ID, r.View.Revision, r.Input, r.ActorID, r.ActorName, r.View.CreatedAt.UTC().Format(time.RFC3339Nano)})
}
func (s *Store) settlementPreferenceProof(r settlementPreferenceRecord) ([]byte, error) {
	return s.referralDigest("settlement-schedule-preference", []any{r.ID, r.View.MerchantID, r.View.Revision, r.View.Enabled, r.View.RecipientConfigID, r.ActorName, r.CreatedAt.UTC().Format(time.RFC3339Nano)})
}
func settlementPlanAudit(ctx context.Context, tx pgx.Tx, role string, actorID int64, actorName, resource string, id int64, op string, changes any) error {
	raw, e := json.Marshal(changes)
	if e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES($1,$2,$3,$4,$5,$6,$7)", role, actorID, actorName, resource, id, op, raw)
	return e
}
func (s *Store) readSettlementPlan(ctx context.Context, q referralReader, id int64) (settlementPlanRecord, error) {
	var r settlementPlanRecord
	var raw []byte
	var audited bool
	e := q.QueryRow(ctx, `SELECT p.id,p.revision,p.enabled,COALESCE(p.last_authorization_id,0),v.id,v.name,v.mode,v.schedule,v.merchant_ids,v.actor_id,v.actor_name,v.proof_hash,v.created_at,
 EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='settlement-plan-versions' AND a.resource_id=v.id AND a.operation='define' AND (a.actor_role,a.actor_id,a.actor_name)=('admin',v.actor_id,v.actor_name))
 FROM settlement_plans p JOIN settlement_plan_versions v ON (v.plan_id,v.revision)=(p.id,p.revision) WHERE p.id=$1`, id).Scan(&r.View.ID, &r.View.Revision, &r.View.Enabled, &r.View.LastAuthorizationID, &r.DefinitionID, &r.Input.Name, &r.Input.Mode, &raw, &r.Input.MerchantIDs, &r.ActorID, &r.ActorName, &r.Proof, &r.View.CreatedAt, &audited)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if e != nil {
		return r, e
	}
	var latestRevision, latestAuthority int64
	if e = q.QueryRow(ctx, "SELECT COALESCE(max(revision),0),COALESCE((SELECT max(id) FROM settlement_plan_authorizations WHERE plan_id=$1),0) FROM settlement_plan_versions WHERE plan_id=$1", id).Scan(&latestRevision, &latestAuthority); e != nil {
		return r, e
	}
	if latestRevision != r.View.Revision || latestAuthority != r.View.LastAuthorizationID {
		return r, ErrConflict
	}
	if json.Unmarshal(raw, &r.Input.Schedule) != nil {
		return r, ErrConflict
	}
	normal, e := normalizeSettlementPlan(r.Input)
	if e != nil {
		return r, ErrConflict
	}
	r.Input = normal
	proof, e := s.settlementPlanProof(r)
	if e != nil || !hmac.Equal(proof, r.Proof) || !audited {
		return r, ErrConflict
	}
	r.View.Name, r.View.Mode, r.View.Schedule = r.Input.Name, r.Input.Mode, r.Input.Schedule
	r.View.MerchantIDs = make([]string, 0, len(r.Input.MerchantIDs))
	for _, mid := range r.Input.MerchantIDs {
		r.View.MerchantIDs = append(r.View.MerchantIDs, strconv.FormatInt(mid, 10))
	}
	if r.View.LastAuthorizationID > 0 {
		a, err := s.readSettlementPlanAuthorization(ctx, q, r.View.LastAuthorizationID)
		if err != nil {
			return r, err
		}
		if a.View.PlanID != r.View.ID || r.View.Enabled && (a.View.PlanRevision != r.View.Revision || a.View.Revoked) {
			return r, ErrConflict
		}
		if r.View.Enabled {
			var now time.Time
			if err = q.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&now); err != nil {
				return r, err
			}
			r.View.Enabled = !now.Before(a.View.ValidFrom) && now.Before(a.View.ExpiresAt)
		}
	} else if r.View.Enabled {
		return r, ErrConflict
	}
	return r, nil
}
func (s *Store) writeSettlementPlan(ctx context.Context, id, expected int64, in SettlementPlanInput, a SettlementPlanActor) (SettlementPlanView, error) {
	if !settlementPlanAdminValid(a) || id < 0 || expected < 0 {
		return SettlementPlanView{}, ErrInvalid
	}
	in, e := normalizeSettlementPlan(in)
	if e != nil {
		return SettlementPlanView{}, e
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return SettlementPlanView{}, e
	}
	defer tx.Rollback(ctx)
	if id == 0 {
		if expected != 0 {
			return SettlementPlanView{}, ErrInvalid
		}
		if e = tx.QueryRow(ctx, "INSERT INTO settlement_plans DEFAULT VALUES RETURNING id").Scan(&id); e != nil {
			return SettlementPlanView{}, e
		}
	} else {
		if _, e = tx.Exec(ctx, "SELECT id FROM settlement_plans WHERE id=$1 FOR UPDATE", id); e != nil {
			return SettlementPlanView{}, e
		}
		old, e := s.readSettlementPlan(ctx, tx, id)
		if e != nil {
			return SettlementPlanView{}, e
		}
		if old.View.Revision != expected {
			return SettlementPlanView{}, ErrConflict
		}
		if old.Input.Mode != in.Mode && old.View.LastAuthorizationID > 0 {
			return SettlementPlanView{}, ErrState
		}
		if _, e = tx.Exec(ctx, "UPDATE settlement_plans SET revision=revision+1,enabled=false WHERE id=$1", id); e != nil {
			return SettlementPlanView{}, e
		}
	}
	var count int
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM merchants WHERE id=ANY($1::bigint[])", in.MerchantIDs).Scan(&count); e != nil {
		return SettlementPlanView{}, e
	}
	if count != len(in.MerchantIDs) {
		return SettlementPlanView{}, ErrNotFound
	}
	r := settlementPlanRecord{View: SettlementPlanView{ID: id, Revision: expected + 1}, Input: in, ActorID: a.ID, ActorName: a.Name}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('settlement_plan_versions','id')),transaction_timestamp()").Scan(&r.DefinitionID, &r.View.CreatedAt); e != nil {
		return SettlementPlanView{}, e
	}
	r.Proof, e = s.settlementPlanProof(r)
	if e != nil {
		return SettlementPlanView{}, e
	}
	raw, _ := json.Marshal(in.Schedule)
	if _, e = tx.Exec(ctx, "INSERT INTO settlement_plan_versions(id,plan_id,revision,name,mode,schedule,merchant_ids,actor_id,actor_name,proof_hash,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", r.DefinitionID, id, r.View.Revision, in.Name, in.Mode, raw, in.MerchantIDs, a.ID, a.Name, r.Proof, r.View.CreatedAt); e != nil {
		return SettlementPlanView{}, e
	}
	if e = settlementPlanAudit(ctx, tx, "admin", a.ID, a.Name, "settlement-plan-versions", r.DefinitionID, "define", map[string]string{"plan_id": fmt.Sprint(id), "revision": fmt.Sprint(r.View.Revision)}); e != nil {
		return SettlementPlanView{}, e
	}
	r, e = s.readSettlementPlan(ctx, tx, id)
	if e != nil {
		return r.View, e
	}
	return r.View, tx.Commit(ctx)
}
func (s *Store) readSettlementPreference(ctx context.Context, q referralReader, mid int64) (settlementPreferenceRecord, error) {
	r := settlementPreferenceRecord{View: SettlementSchedulePreferenceView{MerchantID: mid, Recipient: SettlementRecipientView{State: "missing"}}}
	var audited bool
	e := q.QueryRow(ctx, `SELECT p.id,p.revision,p.enabled,COALESCE(p.recipient_config_id,0),p.actor_name,p.created_at,p.proof_hash,
 EXISTS(SELECT 1 FROM backoffice_audit a WHERE a.resource='settlement-schedule-preferences' AND a.resource_id=p.id AND a.operation='configure' AND (a.actor_role,a.actor_id,a.actor_name)=('merchant',p.merchant_id,p.actor_name))
 FROM settlement_schedule_preference_heads h JOIN settlement_schedule_preferences p ON (p.id,p.merchant_id,p.revision)=(h.preference_id,h.merchant_id,h.revision) WHERE h.merchant_id=$1`, mid).Scan(&r.ID, &r.View.Revision, &r.View.Enabled, &r.View.RecipientConfigID, &r.ActorName, &r.CreatedAt, &r.Proof, &audited)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, nil
	}
	if e != nil {
		return r, e
	}
	proof, e := s.settlementPreferenceProof(r)
	if e != nil || !hmac.Equal(proof, r.Proof) || !audited {
		return r, ErrConflict
	}
	if r.View.RecipientConfigID > 0 {
		var c recipientRecord
		var in SettlementRecipientInput
		e = q.QueryRow(ctx, "SELECT id,merchant_id,recipient_type,app_id,identity_hash,recipient_cipher FROM settlement_recipient_configs WHERE id=$1 AND merchant_id=$2", r.View.RecipientConfigID, mid).Scan(&c.ID, &c.MerchantID, &c.Type, &c.AppID, &c.Hash, &c.Cipher)
		if e != nil {
			return r, e
		}
		in, e = s.decryptSettlementRecipient("config", c.ID, c.Cipher)
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
func (s *Store) setSettlementPreference(ctx context.Context, mid, expected int64, enabled bool, cfgID int64, a SettlementPlanActor) (SettlementSchedulePreferenceView, error) {
	if !settlementPreferenceOwner(mid, a) || expected < 0 || cfgID < 0 || enabled && cfgID == 0 {
		return SettlementSchedulePreferenceView{}, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return SettlementSchedulePreferenceView{}, e
	}
	defer tx.Rollback(ctx)
	var active bool
	if e = tx.QueryRow(ctx, "SELECT enabled FROM merchants WHERE id=$1 FOR NO KEY UPDATE", mid).Scan(&active); e != nil {
		return SettlementSchedulePreferenceView{}, e
	}
	if !active {
		return SettlementSchedulePreferenceView{}, ErrState
	}
	old, e := s.readSettlementPreference(ctx, tx, mid)
	if e != nil {
		return old.View, e
	}
	if old.View.Revision != expected {
		return old.View, ErrConflict
	}
	if cfgID > 0 {
		c, _, e := s.readSettlementConfig(ctx, tx, mid)
		if e != nil {
			return old.View, e
		}
		if c.ID != cfgID {
			return old.View, ErrConflict
		}
	}
	r := settlementPreferenceRecord{View: SettlementSchedulePreferenceView{MerchantID: mid, Revision: expected + 1, Enabled: enabled, RecipientConfigID: cfgID}, ActorName: a.Name}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('settlement_schedule_preferences','id')),transaction_timestamp()").Scan(&r.ID, &r.CreatedAt); e != nil {
		return r.View, e
	}
	r.Proof, e = s.settlementPreferenceProof(r)
	if e != nil {
		return r.View, e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO settlement_schedule_preferences(id,merchant_id,revision,enabled,recipient_config_id,actor_name,proof_hash,created_at) VALUES($1,$2,$3,$4,NULLIF($5::bigint,0),$6,$7,$8)", r.ID, mid, r.View.Revision, enabled, cfgID, a.Name, r.Proof, r.CreatedAt); e != nil {
		return r.View, e
	}
	if expected == 0 {
		if _, e = tx.Exec(ctx, "INSERT INTO settlement_schedule_preference_heads(merchant_id,preference_id,revision) VALUES($1,$2,$3)", mid, r.ID, r.View.Revision); e != nil {
			return r.View, e
		}
	} else {
		tag, err := tx.Exec(ctx, "UPDATE settlement_schedule_preference_heads SET preference_id=$2,revision=$3 WHERE merchant_id=$1 AND revision=$4", mid, r.ID, r.View.Revision, expected)
		if err != nil {
			return r.View, err
		}
		if tag.RowsAffected() != 1 {
			return r.View, ErrConflict
		}
	}
	if e = settlementPlanAudit(ctx, tx, "merchant", mid, a.Name, "settlement-schedule-preferences", r.ID, "configure", map[string]any{"revision": fmt.Sprint(r.View.Revision), "enabled": enabled}); e != nil {
		return r.View, e
	}
	r, e = s.readSettlementPreference(ctx, tx, mid)
	if e != nil {
		return r.View, e
	}
	return r.View, tx.Commit(ctx)
}

func settlementPlanIDs(ctx context.Context, q referralReader, sql string, args ...any) ([]int64, error) {
	rows, e := q.Query(ctx, sql, args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func settlementPlanCanonicalJSON(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }
func settlementPlanNameValid(name string) bool          { return len(strings.TrimSpace(name)) > 0 }
