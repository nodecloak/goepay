package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/grouppolicy"
	"goepay/internal/merchantpolicy"
)

var ErrSettlementType = fmt.Errorf("%w: settlement recipient type closed", ErrState)

type settlementTypeSelection struct {
	Allowed       map[string]bool
	ValidUntil    time.Time
	MissingGlobal bool
}

func readSettlementTypeSelection(ctx context.Context, q referralReader, mid int64, groupID *int64, lock, setup bool) (settlementTypeSelection, error) {
	out := settlementTypeSelection{Allowed: map[string]bool{}}
	suffix := ""
	if lock {
		suffix = " FOR SHARE"
	}
	keys := []string{"settle_alipay", "settle_wxpay", "settle_bank"}
	rows, err := q.Query(ctx, `SELECT body->>'key',body->'value' FROM backoffice_records WHERE resource='settings' AND body->>'key'=ANY($1::text[]) ORDER BY id`+suffix, keys)
	if err != nil {
		return out, err
	}
	globals := map[string]json.RawMessage{}
	for rows.Next() {
		var key string
		var raw json.RawMessage
		if err = rows.Scan(&key, &raw); err != nil {
			rows.Close()
			return out, err
		}
		if _, ok := globals[key]; !ok {
			globals[key] = raw
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return out, err
	}
	group := merchantpolicy.EffectiveGroup{}
	if groupID != nil {
		if *groupID < 0 {
			return out, ErrInvalid
		}
		err = q.QueryRow(ctx, `SELECT body FROM backoffice_records WHERE resource='groups' AND id=$1`+suffix, *groupID).Scan(&group.Body)
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrNotFound
		}
		if err != nil {
			return out, err
		}
		var state struct {
			Enabled *bool `json:"enabled"`
		}
		if json.Unmarshal(group.Body, &state) != nil || state.Enabled != nil && !*state.Enabled {
			return out, ErrState
		}
	} else if mid > 0 {
		if setup {
			group, err = merchantpolicy.ReadEffectiveGroupForSetup(ctx, q, mid, lock)
		} else {
			group, err = merchantpolicy.ReadEffectiveGroup(ctx, q, mid, lock)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return out, ErrNotFound
		}
		if errors.Is(err, merchantpolicy.ErrGroupPolicy) {
			return out, ErrState
		}
		if err != nil {
			return out, err
		}
	} else {
		group.Body = json.RawMessage(`{}`)
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(group.Body, &fields) != nil || fields == nil {
		return out, ErrInvalid
	}
	for _, typ := range grouppolicy.SettlementRecipientTypes() {
		key, _ := grouppolicy.SettlementTypeKey(typ)
		global, exists := globals[key]
		if exists && len(global) == 0 {
			return out, ErrInvalid
		}
		out.MissingGlobal = out.MissingGlobal || !exists
		platform, err := grouppolicy.ParseSettlementTypeSwitch(global)
		if err != nil {
			return out, ErrInvalid
		}
		group, err := grouppolicy.ParseSettlementTypeSwitch(fields[key])
		if err != nil {
			return out, ErrInvalid
		}
		out.Allowed[typ] = (!platform.Override || platform.Enabled) && (!group.Override || group.Enabled)
	}
	out.ValidUntil = group.ValidUntil
	return out, nil
}

func (selection settlementTypeSelection) permits(typ string) bool {
	if typ != "" {
		return selection.Allowed[typ]
	}
	// Existing offline requests may omit recipient setup. Never invent a rail;
	// they remain unable to prepare a provider payout without an original snapshot.
	for _, allowed := range selection.Allowed {
		if allowed {
			return true
		}
	}
	return false
}

type settlementTypeGate struct {
	Selection  settlementTypeSelection
	MerchantID int64
	Type       string
	Setup      bool
}

func requireSettlementType(ctx context.Context, q referralReader, mid int64, typ string, setup bool) (settlementTypeGate, error) {
	selection, err := readSettlementTypeSelection(ctx, q, mid, nil, true, setup)
	gate := settlementTypeGate{Selection: selection, MerchantID: mid, Type: typ, Setup: setup}
	if err != nil {
		return gate, err
	}
	if !selection.permits(typ) {
		return gate, ErrSettlementType
	}
	return gate, nil
}

func (gate settlementTypeGate) recheck(ctx context.Context, q referralReader) error {
	if !gate.Selection.ValidUntil.IsZero() {
		var now time.Time
		if err := q.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
			return err
		}
		if !now.Before(gate.Selection.ValidUntil) {
			return ErrConflict
		}
	}
	if gate.Selection.MissingGlobal {
		current, err := readSettlementTypeSelection(ctx, q, gate.MerchantID, nil, true, gate.Setup)
		if err != nil {
			return err
		}
		if !current.permits(gate.Type) {
			return ErrSettlementType
		}
	}
	return nil
}

// Only supported recipient types are returned. This read cannot activate an
// upstream channel, merchant permission or automatic payout authority.
func (s *Store) AllowedSettlementRecipientTypes(ctx context.Context, mid int64, groupID *int64) ([]string, error) {
	if mid < 0 || mid > 0 && groupID != nil {
		return nil, ErrInvalid
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	selection, err := readSettlementTypeSelection(ctx, tx, mid, groupID, false, false)
	if err != nil {
		return nil, err
	}
	types := []string{}
	for _, typ := range grouppolicy.SettlementRecipientTypes() {
		if selection.Allowed[typ] {
			types = append(types, typ)
		}
	}
	return types, tx.Commit(ctx)
}
