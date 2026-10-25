package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

type SettlementAutoAuthorizationView struct {
	ID           int64 `json:"id,string"`
	PlanID       int64 `json:"plan_id,string"`
	PlanRevision int64 `json:"plan_revision,string"`
	SettlementAutoInput
	Revoked   bool      `json:"revoked"`
	CreatedAt time.Time `json:"created_at"`
}

type settlementAutoScope struct {
	GenerationHash []byte                       `json:"generation_hash"`
	Static         settlementPlanScope          `json:"static"`
	Channels       []settlementAutoChannelScope `json:"channels"`
}

type settlementAutoAuthorization struct {
	View      SettlementAutoAuthorizationView
	Scope     settlementAutoScope
	ActorID   int64
	ActorName string
	Proof     []byte
}

func (s *Store) settlementAutoProof(a settlementAutoAuthorization) ([]byte, error) {
	return s.referralDigest("settlement-auto73/authority", []any{a.View.ID, a.View.PlanID, a.View.PlanRevision, a.View.SettlementAutoInput, a.Scope, a.ActorID, a.ActorName, a.View.CreatedAt.UTC().Format(time.RFC3339Nano)})
}

func (g ProviderFlow) settlementAutoScope(ctx context.Context, q referralReader, p settlementPlanRecord, in SettlementAutoInput) (settlementAutoScope, error) {
	var out settlementAutoScope
	a, e := g.Store.readSettlementPlanAuthorization(ctx, q, in.GenerationAuthorizationID)
	if e != nil {
		return out, e
	}
	var at time.Time
	if e = q.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&at); e != nil {
		return out, e
	}
	if !p.View.Enabled || a.View.ID != p.View.LastAuthorizationID || a.View.PlanID != p.View.ID || a.View.PlanRevision != p.View.Revision || settlementPlanAuthorityAt(a.View, at) != "" || in.ValidFrom.Before(a.View.ValidFrom) || in.ExpiresAt.After(a.View.ExpiresAt) || !at.Before(in.ExpiresAt) {
		return out, ErrState
	}
	static, e := g.Store.settlementPlanStaticScope(ctx, q, p)
	if e != nil {
		return out, e
	}
	want, _ := json.Marshal(a.Scope)
	actual, _ := json.Marshal(static)
	if !bytes.Equal(want, actual) {
		return out, ErrConflict
	}
	if len(in.Channels) != len(p.Input.MerchantIDs) {
		return out, ErrInvalid
	}
	for i, ch := range in.Channels {
		if ch.MerchantID != p.Input.MerchantIDs[i] {
			return out, ErrInvalid
		}
		if static.Threshold != nil && ch.ChannelID != static.Threshold.ChannelID {
			return out, ErrConflict
		}
		admitted := false
		for _, id := range g.MerchantChannels[ch.MerchantID] {
			if id == ch.ChannelID {
				admitted = true
			}
		}
		if !admitted {
			return out, ErrState
		}
		provider := g.Providers[ch.ChannelID]
		history, e := g.Store.readChannelStops(ctx, q, ch.ChannelID)
		if e != nil {
			return out, e
		}
		if len(history) > 0 && history[len(history)-1].Action == "pause" {
			return out, ErrChannelStopped
		}
		scope, e := settlementAutoProviderScope(provider)
		if e != nil {
			return out, e
		}
		if static.Merchants[i].Recipient.Type != scope.Provider || static.Merchants[i].Recipient.AppID != "" && static.Merchants[i].Recipient.AppID != scope.AppID {
			return out, ErrConflict
		}
		scope.SettlementAutoChannel = ch
		out.Channels = append(out.Channels, scope)
	}
	out.GenerationHash, out.Static = a.Proof, static
	return out, nil
}

func (s *Store) readSettlementAuto(ctx context.Context, q fundsReader, id int64) (settlementAutoAuthorization, error) {
	var a settlementAutoAuthorization
	var input, scope []byte
	var generation, previous int64
	var audited bool
	e := q.QueryRow(ctx, `SELECT a.id,a.plan_id,a.plan_revision,a.generation_authorization_id,COALESCE(a.expected_authorization_id,0),a.input,a.scope_snapshot,a.actor_id,a.actor_name,a.proof_hash,a.created_at,
 EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-auto-authorizations' AND b.resource_id=a.id AND b.operation='authorize' AND (b.actor_role,b.actor_id,b.actor_name)=('admin',a.actor_id,a.actor_name))
 FROM settlement_auto_authorizations a WHERE a.id=$1`, id).Scan(&a.View.ID, &a.View.PlanID, &a.View.PlanRevision, &generation, &previous, &input, &scope, &a.ActorID, &a.ActorName, &a.Proof, &a.View.CreatedAt, &audited)
	if errors.Is(e, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if e != nil {
		return a, e
	}
	if json.Unmarshal(input, &a.View.SettlementAutoInput) != nil || json.Unmarshal(scope, &a.Scope) != nil || validateSettlementAutoInput(a.View.SettlementAutoInput) != nil || a.View.GenerationAuthorizationID != generation || a.View.ExpectedAuthorizationID != previous || previous >= id || a.ActorID <= 0 || !sharingSafeField(a.ActorName, 128, true) {
		return a, ErrConflict
	}
	proof, e := s.settlementAutoProof(a)
	if e != nil || !hmac.Equal(proof, a.Proof) || !audited {
		return a, ErrConflict
	}
	gen, e := s.readSettlementPlanAuthorization(ctx, q, generation)
	if e != nil {
		return a, e
	}
	if gen.View.PlanID != a.View.PlanID || gen.View.PlanRevision != a.View.PlanRevision || !hmac.Equal(gen.Proof, a.Scope.GenerationHash) || a.View.ValidFrom.Before(gen.View.ValidFrom) || a.View.ExpiresAt.After(gen.View.ExpiresAt) {
		return a, ErrConflict
	}
	raw, _ := json.Marshal(gen.Scope)
	stored, _ := json.Marshal(a.Scope.Static)
	if !bytes.Equal(raw, stored) || len(a.Scope.Channels) != len(a.View.Channels) {
		return a, ErrConflict
	}
	for i, ch := range a.View.Channels {
		if i >= len(gen.Scope.Merchants) || ch.MerchantID != gen.Scope.Merchants[i].MerchantID || a.Scope.Channels[i].SettlementAutoChannel != ch || a.Scope.Channels[i].Product == "" {
			return a, ErrConflict
		}
	}
	var revokedActor int64
	var name string
	var at time.Time
	var hash []byte
	e = q.QueryRow(ctx, `SELECT r.actor_id,r.actor_name,r.created_at,r.proof_hash,EXISTS(SELECT 1 FROM backoffice_audit b WHERE b.resource='settlement-auto-authorizations' AND b.resource_id=r.authorization_id AND b.operation='revoke' AND (b.actor_role,b.actor_id,b.actor_name)=('admin',r.actor_id,r.actor_name)) FROM settlement_auto_revocations r WHERE r.authorization_id=$1`, id).Scan(&revokedActor, &name, &at, &hash, &audited)
	if e == nil {
		proof, e = s.referralDigest("settlement-auto73/revoke", []any{id, revokedActor, name, at.UTC().Format(time.RFC3339Nano)})
		if e != nil || !hmac.Equal(proof, hash) || !audited {
			return a, ErrConflict
		}
		a.View.Revoked = true
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return a, e
	}
	return a, nil
}

func (s *Store) currentSettlementAuto(ctx context.Context, q fundsReader, plan int64) (settlementAutoAuthorization, error) {
	var id int64
	e := q.QueryRow(ctx, "SELECT authorization_id FROM settlement_auto_heads WHERE plan_id=$1", plan).Scan(&id)
	if errors.Is(e, pgx.ErrNoRows) {
		var exists bool
		if e = q.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM settlement_auto_authorizations WHERE plan_id=$1)", plan).Scan(&exists); e != nil {
			return settlementAutoAuthorization{}, e
		}
		if exists {
			return settlementAutoAuthorization{}, ErrConflict
		}
		return settlementAutoAuthorization{}, ErrNotFound
	}
	if e != nil {
		return settlementAutoAuthorization{}, e
	}
	a, e := s.readSettlementAuto(ctx, q, id)
	if e != nil {
		return a, e
	}
	var latest bool
	e = q.QueryRow(ctx, "SELECT NOT EXISTS(SELECT 1 FROM settlement_auto_authorizations WHERE plan_id=$1 AND id>$2)", plan, id).Scan(&latest)
	if e == nil && (!latest || a.View.PlanID != plan) {
		e = ErrConflict
	}
	return a, e
}

func (s *Store) SettlementAutoAuthorization(ctx context.Context, plan int64, actor SettlementPlanActor) (SettlementAutoAuthorizationView, error) {
	if !settlementPlanAdminValid(actor) || plan <= 0 {
		return SettlementAutoAuthorizationView{}, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return SettlementAutoAuthorizationView{}, e
	}
	defer tx.Rollback(ctx)
	if _, e = s.readSettlementPlan(ctx, tx, plan); e != nil {
		return SettlementAutoAuthorizationView{}, e
	}
	a, e := s.currentSettlementAuto(ctx, tx, plan)
	if errors.Is(e, ErrNotFound) {
		return SettlementAutoAuthorizationView{PlanID: plan}, nil
	}
	return a.View, e
}

// Private authorization material for the funds digest, never an HTTP response.
func (g ProviderFlow) SettlementAutoAuthorizationSnapshot(ctx context.Context, q referralReader, plan, expected int64, in SettlementAutoInput) (json.RawMessage, error) {
	if g.Store == nil || plan <= 0 || expected <= 0 || validateSettlementAutoInput(in) != nil {
		return nil, ErrInvalid
	}
	in.Channels = append([]SettlementAutoChannel(nil), in.Channels...)
	sort.Slice(in.Channels, func(i, j int) bool { return in.Channels[i].MerchantID < in.Channels[j].MerchantID })
	in.ValidFrom, in.ExpiresAt = in.ValidFrom.UTC(), in.ExpiresAt.UTC()
	p, e := g.Store.readSettlementPlan(ctx, q, plan)
	if e != nil {
		return nil, e
	}
	if p.View.Revision != expected {
		return nil, ErrConflict
	}
	old, e := g.Store.currentSettlementAuto(ctx, q, plan)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return nil, e
	}
	if old.View.ID != in.ExpectedAuthorizationID {
		return nil, ErrConflict
	}
	scope, e := g.settlementAutoScope(ctx, q, p, in)
	if e != nil {
		return nil, e
	}
	return json.Marshal(struct {
		Plan  SettlementPlanView  `json:"plan"`
		Input SettlementAutoInput `json:"input"`
		Scope settlementAutoScope `json:"scope"`
	}{p.View, in, scope})
}

type SettlementAutoWalletChannel struct {
	ID          int64    `json:"id,string"`
	Provider    string   `json:"provider"`
	Product     string   `json:"product"`
	AppID       string   `json:"app_id"`
	PayerMasked string   `json:"payer_masked"`
	MerchantIDs []string `json:"merchant_ids"`
}

func (g ProviderFlow) SettlementAutoWalletChannels() []SettlementAutoWalletChannel {
	items := []SettlementAutoWalletChannel{}
	for id, p := range g.Providers {
		scope, e := settlementAutoProviderScope(p)
		if e != nil || id <= 0 {
			continue
		}
		admitted := []int64{}
		for mid, ids := range g.MerchantChannels {
			if mid <= 0 {
				continue
			}
			for _, candidate := range ids {
				if candidate == id {
					admitted = append(admitted, mid)
					break
				}
			}
		}
		if len(admitted) == 0 {
			continue
		}
		sort.Slice(admitted, func(i, j int) bool { return admitted[i] < admitted[j] })
		merchants := make([]string, len(admitted))
		for i, mid := range admitted {
			merchants[i] = fmt.Sprint(mid)
		}
		var parts []string
		if json.Unmarshal([]byte(scope.AccountScope), &parts) != nil {
			continue
		}
		account := parts[2]
		if len(account) > 4 {
			account = "***" + account[len(account)-4:]
		} else {
			account = "***"
		}
		items = append(items, SettlementAutoWalletChannel{ID: id, Provider: scope.Provider, Product: scope.Product, AppID: scope.AppID, PayerMasked: account, MerchantIDs: merchants})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items
}

func (g ProviderFlow) AuthorizeSettlementAuto(ctx context.Context, plan, expected int64, in SettlementAutoInput, actor SettlementPlanActor) (SettlementAutoAuthorizationView, error) {
	var out SettlementAutoAuthorizationView
	if g.Store == nil || !settlementPlanAdminValid(actor) || plan <= 0 || expected <= 0 || validateSettlementAutoInput(in) != nil {
		return out, ErrInvalid
	}
	guard, _ := ctx.Value(fundsActionGuardKey{}).(FundsActionGuard)
	if guard == nil {
		return out, ErrInvalid
	}
	in.Channels = append([]SettlementAutoChannel(nil), in.Channels...)
	sort.Slice(in.Channels, func(i, j int) bool { return in.Channels[i].MerchantID < in.Channels[j].MerchantID })
	in.ValidFrom, in.ExpiresAt = in.ValidFrom.UTC(), in.ExpiresAt.UTC()
	tx, e := g.Store.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = tx.QueryRow(ctx, "SELECT id FROM settlement_plans WHERE id=$1 FOR UPDATE", plan).Scan(&plan); e != nil {
		return out, e
	}
	p, e := g.Store.readSettlementPlan(ctx, tx, plan)
	if e != nil {
		return out, e
	}
	if p.View.Revision != expected {
		return out, ErrConflict
	}
	old, e := g.Store.currentSettlementAuto(ctx, tx, plan)
	if e != nil && !errors.Is(e, ErrNotFound) {
		return out, e
	}
	if old.View.ID != in.ExpectedAuthorizationID {
		return out, ErrConflict
	}
	rows, e := tx.Query(ctx, "SELECT id FROM merchants WHERE id=ANY($1::bigint[]) ORDER BY id FOR SHARE", p.Input.MerchantIDs)
	if e != nil {
		return out, e
	}
	for rows.Next() {
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	scope, e := g.settlementAutoScope(ctx, tx, p, in)
	if e != nil {
		return out, e
	}
	if e = guard(ctx, tx, "settlement-auto-authorize", fmt.Sprintf("%d:%d:%d", plan, expected, in.ExpectedAuthorizationID)); e != nil {
		return out, e
	}
	// The guard can wait on another transaction; recheck database time and the
	// complete original scope before creating a future payment authority.
	locked, e := g.settlementAutoScope(ctx, tx, p, in)
	if e != nil {
		return out, e
	}
	before, _ := json.Marshal(scope)
	after, _ := json.Marshal(locked)
	if !bytes.Equal(before, after) {
		return out, ErrConflict
	}
	a := settlementAutoAuthorization{View: SettlementAutoAuthorizationView{PlanID: plan, PlanRevision: expected, SettlementAutoInput: in}, Scope: scope, ActorID: actor.ID, ActorName: actor.Name}
	if e = tx.QueryRow(ctx, "SELECT nextval(pg_get_serial_sequence('settlement_auto_authorizations','id')),transaction_timestamp()").Scan(&a.View.ID, &a.View.CreatedAt); e != nil {
		return out, e
	}
	a.Proof, e = g.Store.settlementAutoProof(a)
	if e != nil {
		return out, e
	}
	input, _ := json.Marshal(in)
	snapshot, _ := json.Marshal(scope)
	if _, e = tx.Exec(ctx, `INSERT INTO settlement_auto_authorizations(id,plan_id,plan_revision,generation_authorization_id,expected_authorization_id,input,scope_snapshot,actor_id,actor_name,proof_hash,created_at)VALUES($1,$2,$3,$4,NULLIF($5::bigint,0),$6,$7,$8,$9,$10,$11)`, a.View.ID, plan, expected, in.GenerationAuthorizationID, in.ExpectedAuthorizationID, input, snapshot, actor.ID, actor.Name, a.Proof, a.View.CreatedAt); e != nil {
		return out, e
	}
	if old.View.ID == 0 {
		_, e = tx.Exec(ctx, "INSERT INTO settlement_auto_heads(plan_id,authorization_id)VALUES($1,$2)", plan, a.View.ID)
	} else {
		_, e = tx.Exec(ctx, "UPDATE settlement_auto_heads SET authorization_id=$2 WHERE plan_id=$1", plan, a.View.ID)
	}
	if e != nil {
		return out, e
	}
	if e = settlementPlanAudit(ctx, tx, actor.Role, actor.ID, actor.Name, "settlement-auto-authorizations", a.View.ID, "authorize", map[string]any{"plan_id": fmt.Sprint(plan), "generation_authorization_id": fmt.Sprint(in.GenerationAuthorizationID), "cost_policy": in.CostPolicy}); e != nil {
		return out, e
	}
	return a.View, tx.Commit(ctx)
}

func (s *Store) RevokeSettlementAuto(ctx context.Context, id int64, actor SettlementPlanActor) error {
	return s.revokeSettlementAuto(ctx, 0, id, actor)
}

func (s *Store) RevokeSettlementAutoForPlan(ctx context.Context, plan, id int64, actor SettlementPlanActor) error {
	if plan <= 0 {
		return ErrInvalid
	}
	return s.revokeSettlementAuto(ctx, plan, id, actor)
}

func (s *Store) revokeSettlementAuto(ctx context.Context, expectedPlan, id int64, actor SettlementPlanActor) error {
	if !settlementPlanAdminValid(actor) || id <= 0 {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var plan int64
	if e = tx.QueryRow(ctx, "SELECT plan_id FROM settlement_auto_authorizations WHERE id=$1", id).Scan(&plan); e != nil {
		return e
	}
	if expectedPlan > 0 && plan != expectedPlan {
		return ErrNotFound
	}
	if e = tx.QueryRow(ctx, "SELECT id FROM settlement_plans WHERE id=$1 FOR UPDATE", plan).Scan(&plan); e != nil {
		return e
	}
	a, e := s.readSettlementAuto(ctx, tx, id)
	if e != nil {
		return e
	}
	if a.View.Revoked {
		return nil
	}
	var at time.Time
	// The transaction may have begun before waiting for an in-flight claim's
	// plan lock. Record the actual serialized revocation, not its start time.
	if e = tx.QueryRow(ctx, "SELECT clock_timestamp()").Scan(&at); e != nil {
		return e
	}
	proof, e := s.referralDigest("settlement-auto73/revoke", []any{id, actor.ID, actor.Name, at.UTC().Format(time.RFC3339Nano)})
	if e != nil {
		return e
	}
	if _, e = tx.Exec(ctx, "INSERT INTO settlement_auto_revocations(authorization_id,actor_id,actor_name,proof_hash,created_at)VALUES($1,$2,$3,$4,$5)", id, actor.ID, actor.Name, proof, at); e != nil {
		return e
	}
	if e = settlementPlanAudit(ctx, tx, actor.Role, actor.ID, actor.Name, "settlement-auto-authorizations", id, "revoke", map[string]any{"plan_id": fmt.Sprint(plan)}); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
