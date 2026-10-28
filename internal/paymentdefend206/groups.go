package paymentdefend206

import (
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/merchantpolicy"
)

var ErrNotFound = errors.New("payment defend group unavailable")

const groupResource = "payment-defend206-groups"

// GroupView is one group's override of pay_verify (original gedit.php).
type GroupView struct {
	GroupID            int64  `json:"group_id,string"`
	GroupName          string `json:"group_name"`
	Configured         bool   `json:"configured"`
	Revision           int64  `json:"revision,string"`
	PayVerify          string `json:"pay_verify"`
	EffectivePayVerify string `json:"effective_pay_verify"`
	Fingerprint        string `json:"fingerprint"`
	CanUpdate          bool   `json:"can_update"`
}

type GroupUpdate struct {
	PayVerify           string `json:"pay_verify"`
	ExpectedFingerprint string `json:"expected_fingerprint"`
}

type groupRow struct {
	GroupID, Revision int64
	PayVerify         string
	SavedAt           time.Time
	Proof             []byte
}

func validGroupMode(v string) bool { return v == "" || v == "0" || v == "1" || v == "3" }

func (s *Store) groupProof(g groupRow) []byte {
	return s.sum("group-override", []any{g.GroupID, g.Revision, g.PayVerify, g.SavedAt.UTC().Format(time.RFC3339Nano)})
}

func lockGroup(ctx context.Context, tx pgx.Tx, id int64, write bool) error {
	function := "pg_advisory_xact_lock_shared"
	if write {
		function = "pg_advisory_xact_lock"
	}
	_, err := tx.Exec(ctx, "SELECT "+function+"(hashtextextended('goepay/payment-defend206/group/'||($1::bigint)::text,206))", id)
	return err
}

// groupTx loads and verifies one group override. A missing row with any audit
// history, a bad proof, or a revision/audit mismatch is a conflict, never an
// inherit default.
func (s *Store) groupTx(ctx context.Context, tx pgx.Tx, id int64, write bool) (groupRow, bool, error) {
	g := groupRow{GroupID: id}
	suffix := " FOR SHARE"
	if write {
		suffix = " FOR UPDATE"
	}
	err := tx.QueryRow(ctx, `SELECT revision,pay_verify,saved_at,proof_hash FROM payment_defend206_groups WHERE group_id=$1`+suffix, id).Scan(&g.Revision, &g.PayVerify, &g.SavedAt, &g.Proof)
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return g, false, err
	}
	var audits int64
	var ordered bool
	if err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(bool_and(operation='configure' AND actor_role='admin' AND actor_id>0 AND changes->>'revision'=ordinal::text),true)
 FROM(SELECT operation,actor_role,actor_id,changes,row_number() OVER(ORDER BY id) ordinal FROM backoffice_audit WHERE resource=$1 AND resource_id=$2) a`, groupResource, id).Scan(&audits, &ordered); err != nil {
		return g, false, err
	}
	if !found {
		if audits != 0 {
			return g, false, ErrConflict
		}
		return g, false, nil
	}
	g.SavedAt = g.SavedAt.UTC()
	if !validGroupMode(g.PayVerify) || audits != g.Revision || !ordered || !hmac.Equal(g.Proof, s.groupProof(g)) {
		return g, false, ErrConflict
	}
	return g, true, nil
}

// EffectiveModeTx returns the merchant's effective pay_verify: its effective
// group's override when set, otherwise the global setting. When no group
// override has ever been configured anywhere, the group relation is not read.
func (s *Store) EffectiveModeTx(ctx context.Context, tx pgx.Tx, p Policy, merchantID int64) (string, error) {
	if !s.valid() || tx == nil {
		return "", ErrUnavailable
	}
	global := p.Settings["pay_verify"]
	var any bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM payment_defend206_groups) OR EXISTS(SELECT 1 FROM backoffice_audit WHERE resource=$1)`, groupResource).Scan(&any); err != nil {
		return "", err
	}
	if !any {
		return global, nil
	}
	group, err := merchantpolicy.ReadEffectiveGroup(ctx, tx, merchantID, true)
	if err != nil {
		return "", err
	}
	if err = lockGroup(ctx, tx, group.ID, false); err != nil {
		return "", err
	}
	g, _, err := s.groupTx(ctx, tx, group.ID, false)
	if err != nil {
		return "", err
	}
	if g.PayVerify != "" {
		return g.PayVerify, nil
	}
	return global, nil
}

func (s *Store) groupView(ctx context.Context, tx pgx.Tx, g groupRow, configured bool, p Policy) (GroupView, error) {
	out := GroupView{GroupID: g.GroupID, Configured: configured, Revision: g.Revision, PayVerify: g.PayVerify, EffectivePayVerify: p.Settings["pay_verify"], CanUpdate: true}
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT body FROM backoffice_records WHERE id=$1 AND resource='groups' FOR SHARE`, g.GroupID).Scan(&raw); errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	} else if err != nil {
		return out, err
	}
	var body struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &body) != nil || body.Name == "" {
		return out, ErrConflict
	}
	out.GroupName = body.Name
	if g.PayVerify != "" {
		out.EffectivePayVerify = g.PayVerify
	}
	out.Fingerprint = hex.EncodeToString(s.sum("group-view", []any{g.GroupID, configured, g.Revision, g.PayVerify, hex.EncodeToString(g.Proof), p.Fingerprint, body.Name}))
	return out, nil
}

func (s *Store) GroupRead(ctx context.Context, a Actor, id int64) (GroupView, error) {
	if !s.valid() {
		return GroupView{}, ErrUnavailable
	}
	if id < 0 {
		return GroupView{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return GroupView{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockPolicy(ctx, tx, false); err != nil {
		return GroupView{}, err
	}
	if err = lockGroup(ctx, tx, id, false); err != nil {
		return GroupView{}, err
	}
	if err = s.actor(ctx, tx, a, false); err != nil {
		return GroupView{}, err
	}
	rec, err := s.load(ctx, tx, false)
	if err != nil {
		return GroupView{}, err
	}
	g, configured, err := s.groupTx(ctx, tx, id, false)
	if err != nil {
		return GroupView{}, err
	}
	out, err := s.groupView(ctx, tx, g, configured, s.policy(rec))
	if err != nil {
		return GroupView{}, err
	}
	return out, tx.Commit(ctx)
}

func (s *Store) GroupUpdate(ctx context.Context, a Actor, id int64, in GroupUpdate) (GroupView, error) {
	if !s.valid() {
		return GroupView{}, ErrUnavailable
	}
	if id < 0 || !validGroupMode(in.PayVerify) || !hex64(in.ExpectedFingerprint) {
		return GroupView{}, ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return GroupView{}, err
	}
	defer tx.Rollback(ctx)
	if err = lockPolicy(ctx, tx, false); err != nil {
		return GroupView{}, err
	}
	if err = lockGroup(ctx, tx, id, true); err != nil {
		return GroupView{}, err
	}
	if err = s.actor(ctx, tx, a, true); err != nil {
		return GroupView{}, err
	}
	rec, err := s.load(ctx, tx, false)
	if err != nil {
		return GroupView{}, err
	}
	policy := s.policy(rec)
	g, configured, err := s.groupTx(ctx, tx, id, true)
	if err != nil {
		return GroupView{}, err
	}
	before, err := s.groupView(ctx, tx, g, configured, policy)
	if err != nil {
		return GroupView{}, err
	}
	if !hmac.Equal([]byte(before.Fingerprint), []byte(in.ExpectedFingerprint)) || g.Revision == math.MaxInt64 {
		return GroupView{}, ErrConflict
	}
	g.Revision++
	g.PayVerify = in.PayVerify
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&g.SavedAt); err != nil {
		return GroupView{}, err
	}
	g.SavedAt = g.SavedAt.UTC()
	g.Proof = s.groupProof(g)
	if configured {
		tag, err := tx.Exec(ctx, `UPDATE payment_defend206_groups SET revision=$2,pay_verify=$3,saved_at=$4,proof_hash=$5 WHERE group_id=$1 AND revision=$6`, id, g.Revision, g.PayVerify, g.SavedAt, g.Proof, g.Revision-1)
		if err != nil {
			return GroupView{}, err
		}
		if tag.RowsAffected() != 1 {
			return GroupView{}, ErrConflict
		}
	} else if _, err = tx.Exec(ctx, `INSERT INTO payment_defend206_groups(group_id,revision,pay_verify,saved_at,proof_hash) VALUES($1,$2,$3,$4,$5)`, id, g.Revision, g.PayVerify, g.SavedAt, g.Proof); err != nil {
		return GroupView{}, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,$3,$4,'configure',jsonb_build_object('revision',$5::text,'pay_verify',$6::text))`, a.User.ID, a.User.Username, groupResource, id, strconv.FormatInt(g.Revision, 10), g.PayVerify); err != nil {
		return GroupView{}, err
	}
	if err = s.actor(ctx, tx, a, true); err != nil {
		return GroupView{}, err
	}
	out, err := s.groupView(ctx, tx, g, true, policy)
	if err != nil {
		return GroupView{}, err
	}
	return out, tx.Commit(ctx)
}
