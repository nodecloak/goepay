package payment

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"sort"
	"time"
)

type coreRiskAction struct {
	Kind, SubjectKey, Action, Rule     string
	TargetID, Revision, PolicyRevision int64
	Previous                           []byte
	At                                 time.Time
	Until                              *time.Time
	Sources                            []int64
	ActorRole, ActorName               string
	ActorID                            int64
}

func (s *Store) readCoreRiskAction(ctx context.Context, tx pgx.Tx, kind, key string) (*coreRiskAction, []byte, error) {
	rows, e := tx.Query(ctx, `SELECT a.id,a.revision,a.facts,a.proof_hash,`+coreRiskActionAuditMatches+` FROM core_risk_actions a WHERE a.kind=$1 AND a.subject_key=$2 ORDER BY a.revision`, kind, key)
	if e != nil {
		return nil, nil, e
	}
	type item struct {
		id, revision, audits int64
		raw, proof           []byte
	}
	items := []item{}
	for rows.Next() {
		var x item
		if e = rows.Scan(&x.id, &x.revision, &x.raw, &x.proof, &x.audits); e != nil {
			rows.Close()
			return nil, nil, e
		}
		items = append(items, x)
		if len(items) > 100000 {
			rows.Close()
			return nil, nil, ErrState
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, nil, e
	}
	var commits int64
	if e = tx.QueryRow(ctx, `SELECT count(*)FROM backoffice_audit WHERE resource='core-risk-actions' AND operation IN('block','release') AND changes->>'kind'=$1 AND changes->>'subject_key'=$2`, kind, key).Scan(&commits); e != nil {
		return nil, nil, e
	}
	if commits != int64(len(items)) {
		return nil, nil, ErrConflict
	}
	var previous []byte
	var last *coreRiskAction
	for i, x := range items {
		a, e := s.decodeCoreRiskActionFact(x.raw, x.proof, kind, key, x.revision, int64(i+1), previous)
		if e != nil {
			return nil, nil, e
		}
		if x.audits != 1 {
			return nil, nil, ErrConflict
		}
		last = a
		previous = x.proof
	}
	return last, previous, nil
}
func coreRiskActive(a *coreRiskAction, now time.Time) bool {
	return a != nil && a.Action == "block" && (a.Until == nil || now.Before(*a.Until))
}
func lockCoreRiskActionKeys(ctx context.Context, tx pgx.Tx, keys [][2]string) error {
	sort.Slice(keys, func(i, j int) bool { return keys[i][0]+"/"+keys[i][1] < keys[j][0]+"/"+keys[j][1] })
	for _, key := range keys {
		if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "goepay/core-risk-action/"+key[0]+"/"+key[1]); e != nil {
			return e
		}
	}
	return nil
}
func (s *Store) lockCoreRiskAdmissionActions(ctx context.Context, tx pgx.Tx, ips []string, buyer string, merchant, channel int64) error {
	keys := [][2]string{{"merchant", fmt.Sprint(merchant)}, {"channel", fmt.Sprint(channel)}}
	if buyer != "" {
		keys = append(keys, [2]string{"buyer", buyer})
	}
	for _, ip := range ips {
		key, e := s.coreRiskIPKey(ip)
		if e != nil {
			return e
		}
		keys = append(keys, [2]string{"ip", key})
	}
	return lockCoreRiskActionKeys(ctx, tx, keys)
}
func (s *Store) writeCoreRiskAction(ctx context.Context, tx pgx.Tx, a coreRiskAction) (bool, error) {
	if a.ActorRole == "" {
		a.ActorRole = "system"
		a.ActorName = "core-risk"
	}
	if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "goepay/core-risk-action/"+a.Kind+"/"+a.SubjectKey); e != nil {
		return false, e
	}
	old, proof, e := s.readCoreRiskAction(ctx, tx, a.Kind, a.SubjectKey)
	if e != nil {
		return false, e
	}
	// Release RiskCheck preserves an IP ban with more than four days left.
	// Repeated scheduler ticks must not extend it by a few seconds forever.
	if a.Kind == "ip" && a.Action == "block" && coreRiskActive(old, a.At) && (old.Until == nil || old.Until.After(a.At.Add(4*24*time.Hour))) {
		return false, nil
	}
	if a.Action == "block" && coreRiskActive(old, a.At) && (old.Until == nil || a.Until != nil && !old.Until.Before(*a.Until)) {
		return false, nil
	}
	a.Revision = 1
	a.Previous = proof
	if old != nil {
		a.Revision = old.Revision + 1
	}
	hash, e := s.referralDigest("risk102/action", a)
	if e != nil {
		return false, e
	}
	raw, e := json.Marshal(a)
	if e != nil {
		return false, e
	}
	var id int64
	if e = tx.QueryRow(ctx, `INSERT INTO core_risk_actions(kind,subject_key,revision,facts,proof_hash)VALUES($1,$2,$3,$4::jsonb,$5)RETURNING id`, a.Kind, a.SubjectKey, a.Revision, string(raw), hash).Scan(&id); e != nil {
		return false, e
	}
	_, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES($7,$8,$9,'core-risk-actions',$1,$2,jsonb_build_object('kind',$3::text,'subject_key',$4::text,'revision',$5::text,'source_hash',$6::text))`, id, a.Action, a.Kind, a.SubjectKey, fmt.Sprint(a.Revision), hex.EncodeToString(hash), a.ActorRole, a.ActorID, a.ActorName)
	return e == nil, e
}
func (s *Store) checkCoreRiskActions(ctx context.Context, tx pgx.Tx, ips []string, buyer string, merchant, channel int64, now time.Time) error {
	subjects := [][2]string{{"merchant", fmt.Sprint(merchant)}, {"channel", fmt.Sprint(channel)}}
	if buyer != "" {
		subjects = append(subjects, [2]string{"buyer", buyer})
	}
	for _, ip := range ips {
		key, e := s.coreRiskIPKey(ip)
		if e != nil {
			return e
		}
		subjects = append(subjects, [2]string{"ip", key})
	}
	for _, subject := range subjects {
		a, _, e := s.readCoreRiskAction(ctx, tx, subject[0], subject[1])
		if e != nil {
			return e
		}
		if coreRiskActive(a, now) {
			return ErrState
		}
	}
	return nil
}

func (s *Store) decodeCoreRiskActionFact(raw, proof []byte, kind, key string, storedRevision, expectedRevision int64, previous []byte) (*coreRiskAction, error) {
	var a coreRiskAction
	if decodeCoreRiskFacts(raw, &a) != nil || a.Kind != kind || a.SubjectKey != key || a.Revision != expectedRevision || a.Revision != storedRevision || !hmac.Equal(a.Previous, previous) || a.Action != "block" && a.Action != "release" {
		return nil, ErrConflict
	}
	expected, e := s.referralDigest("risk102/action", a)
	if e != nil || !hmac.Equal(expected, proof) {
		return nil, ErrConflict
	}
	if a.Action == "block" && (a.ActorRole != "system" || a.ActorID != 0 || a.ActorName != "core-risk") || a.Action == "release" && (a.ActorRole != "admin" || a.ActorID <= 0 || a.ActorName == "") {
		return nil, ErrConflict
	}
	return &a, nil
}

const coreRiskActionAuditMatches = `(SELECT count(*) FROM backoffice_audit u WHERE u.resource='core-risk-actions' AND u.resource_id=a.id AND u.operation=a.facts->>'Action' AND u.changes->>'source_hash'=encode(a.proof_hash,'hex') AND u.changes->>'revision'=a.revision::text AND u.actor_role=a.facts->>'ActorRole' AND u.actor_id::text=a.facts->>'ActorID' AND u.actor_name=a.facts->>'ActorName')`
