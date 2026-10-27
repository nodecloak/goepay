package payment

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// Reads selected public subject states with a constant number of SQL round
// trips, including all append-only action and independent audit proofs. The
// caller has already authorized these bounded resource IDs.
func (s *Store) coreRiskSubjectStatusesTx(ctx context.Context, tx pgx.Tx, kind string, ids []int64) (map[int64]CoreRiskStatus, error) {
	out := map[int64]CoreRiskStatus{}
	if kind != "merchant" && kind != "channel" && kind != "subchannel" || len(ids) > 10000 {
		return nil, ErrInvalid
	}
	keys := []string{}
	owners := map[string]int64{}
	for _, id := range ids {
		if id < 1 {
			return nil, ErrInvalid
		}
		key := fmt.Sprint(id)
		if _, seen := owners[key]; !seen {
			keys = append(keys, key)
			owners[key] = id
			out[id] = CoreRiskStatus{}
		}
	}
	if len(keys) == 0 {
		return out, nil
	}
	ready, e := s.coreRiskSchema(ctx, tx)
	if e != nil || !ready {
		return out, e
	}
	if _, _, e = s.coreRiskPolicyTx(ctx, tx, false); e != nil {
		return nil, e
	}
	rows, e := tx.Query(ctx, `SELECT a.id,a.subject_key,a.revision,a.facts,a.proof_hash,
 (SELECT count(*) FROM backoffice_audit u WHERE u.resource='core-risk-actions' AND u.resource_id=a.id AND u.operation=a.facts->>'Action' AND u.changes->>'source_hash'=encode(a.proof_hash,'hex') AND u.changes->>'revision'=a.revision::text AND u.actor_role=a.facts->>'ActorRole' AND u.actor_id::text=a.facts->>'ActorID' AND u.actor_name=a.facts->>'ActorName')
 FROM core_risk_actions a WHERE a.kind=$1 AND a.subject_key=ANY($2::text[]) ORDER BY a.subject_key,a.revision LIMIT 100001`, kind, keys)
	if e != nil {
		return nil, e
	}
	type item struct {
		id, revision, audits int64
		key                  string
		raw, proof           []byte
	}
	items := []item{}
	for rows.Next() {
		var it item
		if e = rows.Scan(&it.id, &it.key, &it.revision, &it.raw, &it.proof, &it.audits); e != nil {
			rows.Close()
			return nil, e
		}
		items = append(items, it)
		if len(items) > 100000 {
			rows.Close()
			return nil, ErrState
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	rows, e = tx.Query(ctx, `SELECT changes->>'subject_key',count(*)FROM backoffice_audit WHERE resource='core-risk-actions' AND operation IN('block','release') AND changes->>'kind'=$1 AND changes->>'subject_key'=ANY($2::text[]) GROUP BY changes->>'subject_key'`, kind, keys)
	if e != nil {
		return nil, e
	}
	commits := map[string]int64{}
	for rows.Next() {
		var key string
		var count int64
		if e = rows.Scan(&key, &count); e != nil {
			rows.Close()
			return nil, e
		}
		commits[key] = count
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	previous := map[string][]byte{}
	counts := map[string]int64{}
	last := map[string]*coreRiskAction{}
	lastID := map[string]int64{}
	for _, it := range items {
		counts[it.key]++
		a, e := s.decodeCoreRiskActionFact(it.raw, it.proof, kind, it.key, it.revision, counts[it.key], previous[it.key])
		if e != nil || it.audits != 1 {
			return nil, ErrConflict
		}
		previous[it.key] = it.proof
		last[it.key] = a
		lastID[it.key] = it.id
	}
	now, e := coreRiskDBNow(ctx, tx)
	if e != nil {
		return nil, e
	}
	for key, id := range owners {
		if counts[key] != commits[key] {
			return nil, ErrConflict
		}
		a := last[key]
		if a == nil {
			continue
		}
		v := CoreRiskStatus{ActionID: lastID[key], Revision: a.Revision, Blocked: coreRiskActive(a, now)}
		if v.Blocked {
			v.Reason = coreRiskReason(a.Rule)
			v.Until = a.Until
		}
		out[id] = v
	}
	return out, nil
}
