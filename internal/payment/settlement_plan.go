package payment

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

type SettlementPlanActor struct {
	Role, Class, Name string
	ID                int64
}

type SettlementPlanInput struct {
	Name        string                 `json:"name"`
	Mode        string                 `json:"mode"`
	Schedule    SettlementScheduleSpec `json:"schedule"`
	MerchantIDs []int64                `json:"merchant_ids"`
}

type SettlementPlanView struct {
	ID                  int64                  `json:"id,string"`
	Revision            int64                  `json:"revision,string"`
	Name                string                 `json:"name"`
	Mode                string                 `json:"mode"`
	Schedule            SettlementScheduleSpec `json:"schedule"`
	MerchantIDs         []string               `json:"merchant_ids"`
	Enabled             bool                   `json:"enabled"`
	LastAuthorizationID int64                  `json:"last_authorization_id,string"`
	CreatedAt           time.Time              `json:"created_at"`
}

type SettlementPlanAuthorizationInput struct {
	ExpectedAuthorizationID int64     `json:"expected_authorization_id,string"`
	ValidFrom               time.Time `json:"valid_from"`
	ExpiresAt               time.Time `json:"expires_at"`
	MaxItemGross            int64     `json:"max_item_gross,string"`
	MaxRunGross             int64     `json:"max_run_gross,string"`
	MaxDayGross             int64     `json:"max_day_gross,string"`
	MaxDayItems             int64     `json:"max_day_items,string"`
}

type SettlementPlanAuthorizationView struct {
	ID           int64 `json:"id,string"`
	PlanID       int64 `json:"plan_id,string"`
	PlanRevision int64 `json:"plan_revision,string"`
	SettlementPlanAuthorizationInput
	Revoked   bool      `json:"revoked"`
	CreatedAt time.Time `json:"created_at"`
}

type SettlementSchedulePreferenceView struct {
	MerchantID        int64                   `json:"merchant_id,string"`
	Revision          int64                   `json:"revision,string"`
	Enabled           bool                    `json:"enabled"`
	RecipientConfigID int64                   `json:"recipient_config_id,string"`
	Recipient         SettlementRecipientView `json:"recipient"`
}

type SettlementPlanItemView struct {
	ID                int64  `json:"id,string"`
	MerchantID        int64  `json:"merchant_id,string"`
	RequestID         int64  `json:"request_id,string"`
	State             string `json:"state"`
	Reason            string `json:"reason"`
	LedgerAvailable   int64  `json:"ledger_available,string"`
	Eligible          int64  `json:"eligible,string"`
	Retained          int64  `json:"retained,string"`
	Gross             int64  `json:"gross,string"`
	Fee               int64  `json:"fee,string"`
	Net               int64  `json:"net,string"`
	RecipientConfigID int64  `json:"recipient_config_id,string"`
}

type SettlementPlanRunView struct {
	ID              int64                    `json:"id,string"`
	PlanID          int64                    `json:"plan_id,string"`
	PlanRevision    int64                    `json:"plan_revision,string"`
	AuthorizationID int64                    `json:"authorization_id,string"`
	SlotNo          int64                    `json:"slot_no,string"`
	BusinessDate    string                   `json:"business_date"`
	ScheduledAt     time.Time                `json:"scheduled_at"`
	State           string                   `json:"state"`
	CreatedCount    int64                    `json:"created_count,string"`
	SkippedCount    int64                    `json:"skipped_count,string"`
	BlockedCount    int64                    `json:"blocked_count,string"`
	ErrorCount      int64                    `json:"error_count,string"`
	CreatedAt       time.Time                `json:"created_at"`
	Items           []SettlementPlanItemView `json:"items,omitempty"`
}

func (s *Store) CreateSettlementPlan(ctx context.Context, in SettlementPlanInput, a SettlementPlanActor) (SettlementPlanView, error) {
	return s.writeSettlementPlan(ctx, 0, 0, in, a)
}
func (s *Store) UpdateSettlementPlan(ctx context.Context, id, expected int64, in SettlementPlanInput, a SettlementPlanActor) (SettlementPlanView, error) {
	if id <= 0 || expected <= 0 {
		return SettlementPlanView{}, ErrInvalid
	}
	return s.writeSettlementPlan(ctx, id, expected, in, a)
}
func (s *Store) SettlementPlan(ctx context.Context, id int64, a SettlementPlanActor) (SettlementPlanView, error) {
	if !settlementPlanAdminValid(a) || id <= 0 {
		return SettlementPlanView{}, ErrInvalid
	}
	r, e := s.readSettlementPlan(ctx, s.pool, id)
	return r.View, e
}
func (s *Store) SettlementPlans(ctx context.Context, after int64, limit int, a SettlementPlanActor) ([]SettlementPlanView, error) {
	if !settlementPlanAdminValid(a) || after < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	ids, e := settlementPlanIDs(ctx, tx, "SELECT id FROM settlement_plans WHERE id>$1 ORDER BY id LIMIT $2", after, limit)
	if e != nil {
		return nil, e
	}
	out := make([]SettlementPlanView, 0, len(ids))
	for _, id := range ids {
		r, e := s.readSettlementPlan(ctx, tx, id)
		if e != nil {
			return nil, e
		}
		out = append(out, r.View)
	}
	return out, tx.Commit(ctx)
}
func (s *Store) SettlementSchedulePreference(ctx context.Context, mid int64, a SettlementPlanActor) (SettlementSchedulePreferenceView, error) {
	if !settlementPreferenceOwner(mid, a) {
		return SettlementSchedulePreferenceView{}, ErrInvalid
	}
	r, e := s.readSettlementPreference(ctx, s.pool, mid)
	if e != nil {
		return r.View, e
	}
	c, in, e := s.readSettlementConfig(ctx, s.pool, mid)
	if errors.Is(e, ErrNotFound) {
		return r.View, nil
	}
	if e != nil {
		return r.View, e
	}
	r.View.Recipient = recipientView("configured", c.ID, in)
	return r.View, nil
}
func (s *Store) SetSettlementSchedulePreference(ctx context.Context, mid, expected int64, enabled bool, cfgID int64, a SettlementPlanActor) (SettlementSchedulePreferenceView, error) {
	return s.setSettlementPreference(ctx, mid, expected, enabled, cfgID, a)
}
