package backoffice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type InvitationItem struct {
	ID        string          `json:"id"`
	Code      string          `json:"code"`
	Status    string          `json:"status"`
	Enabled   bool            `json:"enabled"`
	MaxUses   string          `json:"max_uses"`
	UseCount  int64           `json:"use_count"`
	CreatedAt time.Time       `json:"created_at"`
	UsedAt    *time.Time      `json:"used_at"`
	UsedBy    string          `json:"used_by"`
	Uses      json.RawMessage `json:"uses"`
}
type InvitationBatch struct {
	Items    []map[string]string `json:"items"`
	Replayed bool                `json:"replayed"`
}
type InvitationClear struct {
	Archived  int   `json:"archived"`
	Remaining int64 `json:"remaining"`
	Replayed  bool  `json:"replayed"`
}

var invitationRequestID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func (s *Service) Invitations(ctx context.Context, p Principal, q Query, status string) ([]InvitationItem, int64, error) {
	if _, e := definitionFor(p, "invitation-codes", false); e != nil {
		return nil, 0, e
	}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.Limit == 0 {
		q.Limit = 20
	}
	if q.Page < 1 || q.Page > 100000 || q.Limit < 1 || q.Limit > 100 || len(q.Search) > 256 || (status != "" && status != "used" && status != "unused") {
		return nil, 0, ErrInvalid
	}
	const where = `b.resource='invitation-codes' AND b.body->>'archived' IS DISTINCT FROM 'true' AND ($1::text='' OR strpos(lower(b.body->>'code'),lower($1))>0) AND ($2::text='' OR ($2='used')=EXISTS(SELECT 1 FROM enrollment_invitation_uses u WHERE u.invitation_id=b.id))`
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return nil, 0, e
	}
	defer tx.Rollback(ctx)
	var total int64
	if e = tx.QueryRow(ctx, "SELECT count(*) FROM backoffice_records b WHERE "+where, q.Search, status).Scan(&total); e != nil {
		return nil, 0, e
	}
	rows, e := tx.Query(ctx, `SELECT b.id::text,b.body->>'code',b.body->>'enabled'='true',COALESCE(b.body->>'max_uses','1'),b.created_at,COALESCE(u.n,0),u.used_at,COALESCE(u.used_by,''),COALESCE(u.uses,'[]'::jsonb)
 FROM backoffice_records b LEFT JOIN LATERAL(SELECT count(*) n,max(created_at) used_at,(array_agg(merchant_id::text ORDER BY created_at DESC,merchant_id DESC))[1] used_by,jsonb_agg(jsonb_build_object('merchant_id',merchant_id::text,'used_at',created_at) ORDER BY created_at DESC,merchant_id DESC) uses FROM enrollment_invitation_uses WHERE invitation_id=b.id)u ON true WHERE `+where+` ORDER BY b.id DESC LIMIT $3 OFFSET $4`, q.Search, status, q.Limit, (q.Page-1)*q.Limit)
	if e != nil {
		return nil, 0, e
	}
	items := []InvitationItem{}
	for rows.Next() {
		var item InvitationItem
		if e = rows.Scan(&item.ID, &item.Code, &item.Enabled, &item.MaxUses, &item.CreatedAt, &item.UseCount, &item.UsedAt, &item.UsedBy, &item.Uses); e != nil {
			rows.Close()
			return nil, 0, e
		}
		item.Status = "unused"
		if item.UseCount > 0 {
			item.Status = "used"
		}
		items = append(items, item)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, 0, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, 0, e
	}
	return items, total, nil
}

func (s *Service) GenerateInvitations(ctx context.Context, p Principal, requestID string, count int) (InvitationBatch, error) {
	out := InvitationBatch{Items: []map[string]string{}}
	if _, e := definitionFor(p, "invitation-codes", true); e != nil {
		return out, e
	}
	if !invitationRequestID.MatchString(requestID) || count < 1 || count > 1000 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/invitation-batch/%d/%s", p.SubjectID, requestID)); e != nil {
		return out, e
	}
	var prior []byte
	e = tx.QueryRow(ctx, `SELECT changes FROM backoffice_audit WHERE resource='invitation-batches' AND operation='generate' AND actor_role=$1 AND actor_id=$2 AND changes->>'request_id'=$3 ORDER BY id LIMIT 1`, p.Role, p.SubjectID, requestID).Scan(&prior)
	if e == nil {
		var saved struct {
			Count int
			Items []map[string]string
		}
		if json.Unmarshal(prior, &saved) != nil || saved.Count != count {
			return out, ErrConflict
		}
		out.Items = saved.Items
		out.Replayed = true
		return out, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	codes := make([]string, count)
	for i := range codes {
		b := make([]byte, 20)
		if _, e = rand.Read(b); e != nil {
			return out, e
		}
		codes[i] = hex.EncodeToString(b)
	}
	rows, e := tx.Query(ctx, `INSERT INTO backoffice_records(resource,body) SELECT 'invitation-codes',jsonb_build_object('code',c,'max_uses','1','enabled',true) FROM unnest($1::text[])c RETURNING id::text,body->>'code'`, codes)
	if e != nil {
		return out, businessError(e)
	}
	for rows.Next() {
		var id, code string
		if e = rows.Scan(&id, &code); e != nil {
			rows.Close()
			return out, e
		}
		out.Items = append(out.Items, map[string]string{"id": id, "code": code})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if e = audit(ctx, tx, p, "invitation-batches", 0, "generate", map[string]any{"request_id": requestID, "count": count, "items": out.Items}); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}

// Archives keep the invitation identity and immutable registration uses. Codes
// can never be reassigned to a different historical registration lifecycle.
func (s *Service) archiveInvitationsTx(ctx context.Context, tx pgx.Tx, p Principal, scope string, id int64) (InvitationClear, error) {
	var out InvitationClear
	rows, e := tx.Query(ctx, `SELECT id FROM backoffice_records b WHERE resource='invitation-codes' AND body->>'archived' IS DISTINCT FROM 'true' AND ($1::bigint=0 OR id=$1) AND ($2::text='all' OR EXISTS(SELECT 1 FROM enrollment_invitation_uses u WHERE u.invitation_id=b.id)) ORDER BY id LIMIT 1000 FOR UPDATE`, id, scope)
	if e != nil {
		return out, e
	}
	ids := []int64{}
	for rows.Next() {
		var n int64
		if e = rows.Scan(&n); e != nil {
			rows.Close()
			return out, e
		}
		ids = append(ids, n)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	if id > 0 && len(ids) == 0 {
		return out, ErrNotFound
	}
	if len(ids) > 0 {
		if _, e = tx.Exec(ctx, `UPDATE backoffice_records SET body=body||jsonb_build_object('enabled',false,'archived',true),updated_at=now() WHERE id=ANY($1::bigint[]) AND resource='invitation-codes'`, ids); e != nil {
			return out, e
		}
	}
	out.Archived = len(ids)
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_records b WHERE resource='invitation-codes' AND body->>'archived' IS DISTINCT FROM 'true' AND ($1::text='all' OR EXISTS(SELECT 1 FROM enrollment_invitation_uses u WHERE u.invitation_id=b.id))`, scope).Scan(&out.Remaining); e != nil {
		return out, e
	}
	if e = audit(ctx, tx, p, "invitation-codes", id, "archive", map[string]any{"scope": scope, "ids": ids, "archived": len(ids)}); e != nil {
		return out, e
	}
	return out, nil
}
func (s *Service) ClearInvitations(ctx context.Context, p Principal, requestID, scope string, confirm bool) (InvitationClear, error) {
	var out InvitationClear
	if _, e := definitionFor(p, "invitation-codes", true); e != nil {
		return out, e
	}
	if !confirm || !invitationRequestID.MatchString(requestID) || (scope != "all" && scope != "used") {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, fmt.Sprintf("goepay/invitation-clear/%d/%s", p.SubjectID, requestID)); e != nil {
		return out, e
	}
	var prior []byte
	e = tx.QueryRow(ctx, `SELECT changes FROM backoffice_audit WHERE resource='invitation-batches' AND operation='clear' AND actor_role=$1 AND actor_id=$2 AND changes->>'request_id'=$3 ORDER BY id LIMIT 1`, p.Role, p.SubjectID, requestID).Scan(&prior)
	if e == nil {
		var saved struct {
			Scope  string
			Result InvitationClear
		}
		if json.Unmarshal(prior, &saved) != nil || saved.Scope != scope {
			return out, ErrConflict
		}
		out = saved.Result
		out.Replayed = true
		return out, tx.Commit(ctx)
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, e
	}
	out, e = s.archiveInvitationsTx(ctx, tx, p, scope, 0)
	if e != nil {
		return out, e
	}
	if e = audit(ctx, tx, p, "invitation-batches", 0, "clear", map[string]any{"request_id": requestID, "scope": scope, "result": out}); e != nil {
		return out, e
	}
	return out, tx.Commit(ctx)
}
func (s *Service) archiveInvitation(ctx context.Context, p Principal, id int64) error {
	if id < 1 {
		return ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if _, e = s.archiveInvitationsTx(ctx, tx, p, "all", id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

func invitationID(raw string) (int64, error) {
	id, e := strconv.ParseInt(raw, 10, 64)
	if e != nil || id < 1 || strconv.FormatInt(id, 10) != raw {
		return 0, ErrInvalid
	}
	return id, nil
}
