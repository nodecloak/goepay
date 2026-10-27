package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
	"sort"
	"strconv"
	"time"
)

type CoreRiskActionView struct {
	ID             int64      `json:"id,string"`
	Kind           string     `json:"kind"`
	TargetID       int64      `json:"target_id,string"`
	Revision       int64      `json:"revision,string"`
	Rule           string     `json:"rule"`
	Active         bool       `json:"active"`
	Until          *time.Time `json:"until"`
	ChangedAt      time.Time  `json:"changed_at"`
	SourceCount    int        `json:"source_count"`
	SourceOrderIDs []string   `json:"source_order_ids"`
	Fingerprint    string     `json:"fingerprint"`
}

func (s *Store) coreRiskActionView(ctx context.Context, tx pgx.Tx, a *coreRiskAction, proof []byte, now time.Time) (CoreRiskActionView, error) {
	var out CoreRiskActionView
	if a == nil {
		return out, ErrNotFound
	}
	var id int64
	if e := tx.QueryRow(ctx, `SELECT id FROM core_risk_actions WHERE kind=$1 AND subject_key=$2 AND revision=$3`, a.Kind, a.SubjectKey, a.Revision).Scan(&id); e != nil {
		return out, e
	}
	fp, e := s.referralDigest("risk102/public-action", []any{id, a.Kind, a.SubjectKey, a.Revision, proof})
	if e != nil {
		return out, e
	}
	sources := []string{}
	for i, source := range a.Sources {
		if i >= 20 {
			break
		}
		sources = append(sources, strconv.FormatInt(source, 10))
	}
	return CoreRiskActionView{ID: id, Kind: a.Kind, TargetID: a.TargetID, Revision: a.Revision, Rule: a.Rule, Active: coreRiskActive(a, now), Until: a.Until, ChangedAt: a.At, SourceCount: len(a.Sources), SourceOrderIDs: sources, Fingerprint: hex.EncodeToString(fp)}, nil
}
func (s *Store) CoreRiskActions(ctx context.Context, actor MerchantCredentialActor) ([]CoreRiskActionView, error) {
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT kind,subject_key FROM core_risk_actions UNION SELECT changes->>'kind',changes->>'subject_key'FROM backoffice_audit WHERE resource='core-risk-actions' AND operation IN('block','release')`)
	if e != nil {
		return nil, e
	}
	keys := [][2]string{}
	for rows.Next() {
		var key [2]string
		if e = rows.Scan(&key[0], &key[1]); e != nil {
			rows.Close()
			return nil, e
		}
		keys = append(keys, key)
		if len(keys) > 10000 {
			rows.Close()
			return nil, ErrState
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	now, e := coreRiskDBNow(ctx, tx)
	if e != nil {
		return nil, e
	}
	out := []CoreRiskActionView{}
	for _, key := range keys {
		a, proof, e := s.readCoreRiskAction(ctx, tx, key[0], key[1])
		if e != nil {
			return nil, e
		}
		v, e := s.coreRiskActionView(ctx, tx, a, proof, now)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
func (s *Store) ReleaseCoreRiskAction(ctx context.Context, actor MerchantCredentialActor, id int64, expected, policyExpected string) (CoreRiskActionView, error) {
	var out CoreRiskActionView
	if id < 1 || len(expected) != 64 || len(policyExpected) != 64 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('goepay/core-risk-check',0))`); e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	p, configured, e := s.coreRiskPolicyTx(ctx, tx, false)
	if e != nil {
		return out, e
	}
	v, e := s.coreRiskView(p, configured)
	if e != nil {
		return out, e
	}
	if !hmac.Equal([]byte(v.Fingerprint), []byte(policyExpected)) {
		return out, ErrConflict
	}
	var kind, key string
	if e = tx.QueryRow(ctx, `SELECT kind,subject_key FROM core_risk_actions WHERE id=$1`, id).Scan(&kind, &key); e == pgx.ErrNoRows {
		return out, ErrNotFound
	} else if e != nil {
		return out, e
	}
	a, proof, e := s.readCoreRiskAction(ctx, tx, kind, key)
	if e != nil {
		return out, e
	}
	now, e := coreRiskDBNow(ctx, tx)
	if e != nil {
		return out, e
	}
	current, e := s.coreRiskActionView(ctx, tx, a, proof, now)
	if e != nil {
		return out, e
	}
	if current.ID != id || !current.Active || !hmac.Equal([]byte(current.Fingerprint), []byte(expected)) {
		return out, ErrConflict
	}
	release := *a
	release.Action = "release"
	release.At = now
	release.Until = nil
	release.PolicyRevision = p.Revision
	release.ActorRole = "admin"
	release.ActorID = actor.User.ID
	release.ActorName = actor.User.Username
	if _, e = s.writeCoreRiskAction(ctx, tx, release); e != nil {
		return out, e
	}
	a, proof, e = s.readCoreRiskAction(ctx, tx, kind, key)
	if e != nil {
		return out, e
	}
	out, e = s.coreRiskActionView(ctx, tx, a, proof, now)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, actor.User, actor.SessionToken, actor.Delegated); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
