package backoffice

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"goepay/internal/ordersubject"
	"goepay/internal/payment"
	"strconv"
)

type ParentChannelSubject struct {
	ChannelID   string `json:"channel_id"`
	OrderName   string `json:"ordername"`
	Rules       string `json:"ordername_rules"`
	Fingerprint string `json:"fingerprint"`
}

func readParentSubject(ctx context.Context, tx pgx.Tx, id int64) (ParentChannelSubject, []int64, error) {
	out := ParentChannelSubject{ChannelID: strconv.FormatInt(id, 10)}
	rows, e := tx.Query(ctx, `SELECT id,body FROM backoffice_records WHERE resource='order-subject-policies' AND body->>'scope'='channel' AND body->>'scope_id'=$1 ORDER BY id FOR UPDATE`, out.ChannelID)
	if e != nil {
		return out, nil, e
	}
	defer rows.Close()
	type record struct {
		ID   int64
		Body json.RawMessage
	}
	records := []record{}
	active := []int64{}
	for rows.Next() {
		var v record
		if e = rows.Scan(&v.ID, &v.Body); e != nil {
			return out, nil, e
		}
		records = append(records, v)
		if len(records) > 1000 {
			return out, nil, ErrConflict
		}
		var p struct {
			Template string `json:"template"`
			Rules    string `json:"rules"`
			Enabled  bool   `json:"enabled"`
		}
		if e = json.Unmarshal(v.Body, &p); e != nil {
			return out, nil, e
		}
		if (ordersubject.Policy{Scope: "channel", ScopeID: id, Template: p.Template, RulesText: p.Rules}).Validate() != nil {
			return out, nil, ErrConflict
		}
		if p.Enabled {
			active = append(active, v.ID)
			if p.Template != "" {
				out.OrderName = p.Template
			}
			if p.Rules != "" {
				out.Rules = p.Rules
			}
		}
	}
	if e = rows.Err(); e != nil {
		return out, nil, e
	}
	raw, _ := json.Marshal([]any{out.ChannelID, records})
	sum := sha256.Sum256(raw)
	out.Fingerprint = hex.EncodeToString(sum[:])
	return out, active, nil
}

// ParentChannelSubject uses the existing title policy resource and first-send
// snapshots. Blank strings retain its original independent inheritance rules.
func (s *Service) ParentChannelSubject(ctx context.Context, a payment.ParentChannelActor, id int64, input *ParentChannelSubject, expected string) (ParentChannelSubject, error) {
	var out ParentChannelSubject
	if s.store == nil || id <= 0 {
		return out, ErrInvalid
	}
	if input != nil && (len(input.Rules) > 20000 || (ordersubject.Policy{Scope: "channel", ScopeID: id, Template: input.OrderName, RulesText: input.Rules}).Validate() != nil) {
		return out, ErrInvalid
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if e = s.store.CheckParentChannelManagementTx(ctx, tx, a, id); e != nil {
		return out, e
	}
	out, active, e := readParentSubject(ctx, tx, id)
	if e != nil {
		return out, e
	}
	if input != nil {
		if len(expected) != 64 || !hmac.Equal([]byte(expected), []byte(out.Fingerprint)) {
			return out, ErrConflict
		}
		p := Principal{Role: "admin", SubjectID: a.User.ID, Username: a.User.Username, AdminClass: a.User.AdminClass}
		for _, old := range active {
			if _, e = tx.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{enabled}','false'::jsonb),updated_at=clock_timestamp() WHERE resource='order-subject-policies' AND id=$1`, old); e != nil {
				return out, e
			}
			if e = audit(ctx, tx, p, "order-subject-policies", old, "update", map[string]any{"enabled": false}); e != nil {
				return out, e
			}
		}
		v := map[string]any{"scope": "channel", "scope_id": out.ChannelID, "template": input.OrderName, "rules": input.Rules, "enabled": true}
		raw, _ := json.Marshal(v)
		var record int64
		if e = tx.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('order-subject-policies',$1::jsonb)RETURNING id`, string(raw)).Scan(&record); e != nil {
			return out, e
		}
		if e = audit(ctx, tx, p, "order-subject-policies", record, "create", v); e != nil {
			return out, e
		}
		out, _, e = readParentSubject(ctx, tx, id)
		if e != nil {
			return out, e
		}
	}
	if e = s.store.CheckParentChannelManagementTx(ctx, tx, a, id); e != nil {
		return out, e
	}
	e = tx.Commit(ctx)
	var pe *pgconn.PgError
	if errors.As(e, &pe) && pe.Code == "40001" {
		e = ErrConflict
	}
	return out, e
}
