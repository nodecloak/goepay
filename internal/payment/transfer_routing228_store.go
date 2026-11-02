package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

const TransferRoutingKey228 = "__transfer_routing228"

var transferRoutingTypes228 = []string{"alipay", "wxpay", "qqpay", "bank"}

type TransferRoutingState228 struct {
	Configured  bool              `json:"configured"`
	Revision    string            `json:"revision"`
	Fingerprint string            `json:"fingerprint"`
	Routes      map[string]string `json:"routes"`
}

type transferRoutingBody228 struct {
	Key      string            `json:"key"`
	Routes   map[string]string `json:"routes"`
	Revision string            `json:"revision"`
	Proof    string            `json:"proof"`
}

func copyTransferRoutes228(in map[string]string) (map[string]string, error) {
	if len(in) != len(transferRoutingTypes228) {
		return nil, ErrInvalid
	}
	out := make(map[string]string, len(in))
	for _, kind := range transferRoutingTypes228 {
		route, err := ParseTransferRoute228(in[kind])
		if err != nil {
			return nil, err
		}
		out[kind] = route.String()
	}
	return out, nil
}

// Readers take the shared form of this lock at first-intent admission. This
// serializes configuration changes without serializing concurrent transfers.
func LockTransferRouting228(ctx context.Context, tx pgx.Tx, write bool) error {
	query := "SELECT pg_advisory_xact_lock_shared($1::bigint)"
	if write {
		query = "SELECT pg_advisory_xact_lock($1::bigint)"
	}
	_, err := tx.Exec(ctx, query, platformSettingsLock)
	return err
}

func (s *Store) TransferRoutingSnapshot228(ctx context.Context, q fundsReader) (TransferRoutingState228, error) {
	out := TransferRoutingState228{Revision: "0", Routes: map[string]string{}}
	for _, kind := range transferRoutingTypes228 {
		out.Routes[kind] = "inherit"
	}
	var id int64
	var raw []byte
	err := q.QueryRow(ctx, `SELECT id,body FROM backoffice_records WHERE resource='settings' AND body->>'key'=$1`, TransferRoutingKey228).Scan(&id, &raw)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return out, err
	}
	var count int64
	if e := q.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource='transfer-routing228' AND operation='transfer-routing-update'`).Scan(&count); e != nil {
		return out, e
	}
	if errors.Is(err, pgx.ErrNoRows) {
		if count != 0 {
			return out, ErrConflict
		}
	} else {
		var body transferRoutingBody228
		if json.Unmarshal(raw, &body) != nil || body.Key != TransferRoutingKey228 {
			return out, ErrConflict
		}
		revision, e := strconv.ParseInt(body.Revision, 10, 64)
		routes, re := copyTransferRoutes228(body.Routes)
		proof, pe := hex.DecodeString(body.Proof)
		want, we := s.referralDigest("transfer-routing228/policy", []any{body.Revision, routes})
		if e != nil || revision <= 0 || strconv.FormatInt(revision, 10) != body.Revision || revision != count || re != nil || pe != nil || we != nil || !hmac.Equal(proof, want) || hex.EncodeToString(proof) != body.Proof {
			return out, ErrConflict
		}
		out.Configured, out.Revision, out.Routes = true, body.Revision, routes
	}
	fingerprint, e := s.referralDigest("transfer-routing228/fingerprint", []any{id, out.Configured, out.Revision, out.Routes})
	if e != nil {
		return out, e
	}
	out.Fingerprint = hex.EncodeToString(fingerprint)
	return out, nil
}

func (s *Store) TransferRouting228(ctx context.Context) (TransferRoutingState228, error) {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return TransferRoutingState228{}, err
	}
	defer tx.Rollback(ctx)
	out, err := s.TransferRoutingSnapshot228(ctx, tx)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (s *Store) SaveTransferRouting228(ctx context.Context, actor ParentChannelActor, expected string, routes map[string]string) (TransferRoutingState228, error) {
	var out TransferRoutingState228
	routes, err := copyTransferRoutes228(routes)
	if err != nil || len(expected) != 64 {
		return out, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if err = actor.check(ctx, tx); err != nil {
		return out, err
	}
	if err = LockTransferRouting228(ctx, tx, true); err != nil {
		return out, err
	}
	current, err := s.TransferRoutingSnapshot228(ctx, tx)
	if err != nil {
		return out, err
	}
	if !hmac.Equal([]byte(expected), []byte(current.Fingerprint)) {
		return out, ErrConflict
	}
	revision, _ := strconv.ParseInt(current.Revision, 10, 64)
	if revision == int64(^uint64(0)>>1) {
		return out, ErrState
	}
	next := strconv.FormatInt(revision+1, 10)
	proof, err := s.referralDigest("transfer-routing228/policy", []any{next, routes})
	if err != nil {
		return out, err
	}
	body, err := json.Marshal(transferRoutingBody228{Key: TransferRoutingKey228, Routes: routes, Revision: next, Proof: hex.EncodeToString(proof)})
	if err != nil {
		return out, err
	}
	if err = actor.check(ctx, tx); err != nil {
		return out, err
	}
	var id int64
	if err = tx.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body)VALUES('settings',$1::jsonb) ON CONFLICT((body->>'key')) WHERE resource='settings' DO UPDATE SET body=EXCLUDED.body,updated_at=clock_timestamp() RETURNING id`, body).Scan(&id); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',$1,$2,'transfer-routing228',$3,'transfer-routing-update',jsonb_build_object('revision',$4::text))`, actor.User.ID, actor.User.Username, id, next); err != nil {
		return out, err
	}
	out, err = s.TransferRoutingSnapshot228(ctx, tx)
	if err != nil {
		return out, fmt.Errorf("read saved transfer routing: %w", err)
	}
	if err = actor.check(ctx, tx); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
