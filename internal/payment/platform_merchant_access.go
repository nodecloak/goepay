package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5"

	"goepay/internal/identity"
)

const platformAccessResource = "platform-merchant-access179"
const platformAccessHeadKey = "__platform_merchant_access179"
const platformSettingsLock int64 = 7170140001 // Original backoffice settings lock.

type PlatformMerchantAccess struct {
	CloseKeyLogin string `json:"close_keylogin"`
	UserRefund    string `json:"user_refund"`
	Fingerprint   string `json:"fingerprint"`
	CanUpdate     bool   `json:"can_update"`
}

type PublicMerchantAccess struct {
	CloseKeyLogin int `json:"close_keylogin"`
	UserRefund    int `json:"user_refund"`
}

type platformAccessHead struct {
	Revision      int64  `json:"revision,string"`
	CloseID       int64  `json:"close_id,string"`
	RefundID      int64  `json:"refund_id,string"`
	CloseKeyLogin string `json:"close_keylogin"`
	UserRefund    string `json:"user_refund"`
	Proof         []byte `json:"proof"`
}

type platformAccessState struct {
	PlatformMerchantAccess
	headID, closeID, refundID int64
	head                      platformAccessHead
	configured                bool
}

func platformFlag(v string) bool { return v == "0" || v == "1" }

func (s *Store) platformAccessProof(id int64, h platformAccessHead) ([]byte, error) {
	return s.referralDigest("platform-access179/head", []any{id, h.Revision, h.CloseID, h.RefundID, h.CloseKeyLogin, h.UserRefund})
}

// All normal settings writers take the same exclusive advisory lock before any
// settings row. Admissions hold its shared side through their durable boundary.
func (s *Store) platformAccessTx(ctx context.Context, tx pgx.Tx, write bool) (platformAccessState, error) {
	out := platformAccessState{PlatformMerchantAccess: PlatformMerchantAccess{CloseKeyLogin: "0", UserRefund: "1", CanUpdate: true}}
	lock := `SELECT pg_advisory_xact_lock_shared($1)`
	if write {
		lock = `SELECT pg_advisory_xact_lock($1)`
	}
	if _, e := tx.Exec(ctx, lock, platformSettingsLock); e != nil {
		return out, e
	}
	rows, e := tx.Query(ctx, `SELECT id,body FROM backoffice_records WHERE resource='settings' AND body->>'key' IN ('close_keylogin','user_refund') ORDER BY id FOR SHARE`)
	if e != nil {
		return out, e
	}
	seen := map[string]bool{}
	for rows.Next() {
		var id int64
		var raw []byte
		if e = rows.Scan(&id, &raw); e != nil {
			rows.Close()
			return out, e
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			rows.Close()
			return out, ErrConflict
		}
		for k := range fields {
			if k != "key" && k != "value" && k != "description" {
				rows.Close()
				return out, ErrConflict
			}
		}
		var key, value string
		if json.Unmarshal(fields["key"], &key) != nil || json.Unmarshal(fields["value"], &value) != nil || !platformFlag(value) || seen[key] {
			rows.Close()
			return out, ErrConflict
		}
		seen[key] = true
		if key == "close_keylogin" {
			out.CloseKeyLogin, out.closeID = value, id
		} else {
			out.UserRefund, out.refundID = value, id
		}
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return out, e
	}
	var revisions int64
	if e = tx.QueryRow(ctx, `SELECT count(*) FROM backoffice_audit WHERE resource=$1 AND operation='platform-access-update'`, platformAccessResource).Scan(&revisions); e != nil {
		return out, e
	}
	rows, e = tx.Query(ctx, `SELECT id,body FROM backoffice_records WHERE resource='settings' AND body->>'key'=$1 ORDER BY id FOR SHARE`, platformAccessHeadKey)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		if out.configured {
			rows.Close()
			return out, ErrConflict
		}
		var raw []byte
		if e = rows.Scan(&out.headID, &raw); e != nil {
			rows.Close()
			return out, e
		}
		var stored map[string]json.RawMessage
		var encoded string
		if json.Unmarshal(raw, &stored) != nil || len(stored) != 2 || json.Unmarshal(stored["value"], &encoded) != nil || json.Unmarshal([]byte(encoded), &out.head) != nil {
			rows.Close()
			return out, ErrConflict
		}
		canonical, encodeError := json.Marshal(out.head)
		if encodeError != nil || !bytes.Equal(canonical, []byte(encoded)) {
			rows.Close()
			return out, ErrConflict
		}
		out.configured = true
	}
	rows.Close()
	if e = rows.Err(); e != nil {
		return out, e
	}
	if out.configured {
		h := out.head
		proof, e := s.platformAccessProof(out.headID, h)
		if e != nil || h.Revision <= 0 || h.Revision != revisions || h.CloseID != out.closeID || h.RefundID != out.refundID || out.closeID <= 0 || out.refundID <= 0 || h.CloseKeyLogin != out.CloseKeyLogin || h.UserRefund != out.UserRefund || !hmac.Equal(proof, h.Proof) {
			return out, ErrConflict
		}
	} else if revisions != 0 {
		return out, ErrConflict
	}
	proof, e := s.referralDigest("platform-access179/state", []any{out.headID, out.configured, out.closeID, out.refundID, out.CloseKeyLogin, out.UserRefund, out.head.Revision, out.head.Proof})
	if e != nil {
		return out, e
	}
	out.Fingerprint = hex.EncodeToString(proof)
	return out, nil
}

func (s *Store) PublicMerchantAccess(ctx context.Context) (PublicMerchantAccess, error) {
	var out PublicMerchantAccess
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	state, e := s.platformAccessTx(ctx, tx, false)
	if e != nil {
		return out, e
	}
	if state.CloseKeyLogin == "1" {
		out.CloseKeyLogin = 1
	}
	if state.UserRefund == "1" {
		out.UserRefund = 1
	}
	return out, tx.Commit(ctx)
}

func (s *Store) PlatformMerchantAccess(ctx context.Context, a MerchantCredentialActor) (PlatformMerchantAccess, error) {
	var out PlatformMerchantAccess
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	state, e := s.platformAccessTx(ctx, tx, false)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	return state.PlatformMerchantAccess, tx.Commit(ctx)
}

func (s *Store) UpdatePlatformMerchantAccess(ctx context.Context, a MerchantCredentialActor, closeKey, refund, expected string) (PlatformMerchantAccess, error) {
	var out PlatformMerchantAccess
	if !platformFlag(closeKey) || !platformFlag(refund) || len(expected) != 64 {
		return out, ErrInvalid
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(ctx)
	state, e := s.platformAccessTx(ctx, tx, true)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	if !hmac.Equal([]byte(expected), []byte(state.Fingerprint)) {
		return out, ErrConflict
	}
	for _, setting := range []struct {
		key, value string
		id         *int64
	}{{"close_keylogin", closeKey, &state.closeID}, {"user_refund", refund, &state.refundID}} {
		if *setting.id == 0 {
			e = tx.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value',$2::text)) RETURNING id`, setting.key, setting.value).Scan(setting.id)
		} else {
			_, e = tx.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_set(body,'{value}',to_jsonb($2::text)),updated_at=clock_timestamp() WHERE id=$1 AND resource='settings'`, *setting.id, setting.value)
		}
		if e != nil {
			return out, e
		}
	}
	if state.headID == 0 {
		if e = tx.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body) VALUES('settings',jsonb_build_object('key',$1::text,'value','{}')) RETURNING id`, platformAccessHeadKey).Scan(&state.headID); e != nil {
			return out, e
		}
	}
	head := platformAccessHead{Revision: state.head.Revision + 1, CloseID: state.closeID, RefundID: state.refundID, CloseKeyLogin: closeKey, UserRefund: refund}
	head.Proof, e = s.platformAccessProof(state.headID, head)
	if e != nil {
		return out, e
	}
	raw, e := json.Marshal(head)
	if e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `UPDATE backoffice_records SET body=jsonb_build_object('key',$3::text,'value',$2::text),updated_at=clock_timestamp() WHERE id=$1 AND resource='settings'`, state.headID, string(raw), platformAccessHeadKey); e != nil {
		return out, e
	}
	if _, e = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes) VALUES('admin',$1,$2,$3,$4,'platform-access-update',jsonb_build_object('close_keylogin',$5::text,'user_refund',$6::text,'revision',$7::text))`, a.User.ID, a.User.Username, platformAccessResource, state.headID, closeKey, refund, strconv.FormatInt(head.Revision, 10)); e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	state, e = s.platformAccessTx(ctx, tx, true)
	if e != nil {
		return out, e
	}
	if e = identity.CheckOriginalSuperAdminSessionTx(ctx, tx, a.User, a.SessionToken, a.Delegated); e != nil {
		return out, e
	}
	return state.PlatformMerchantAccess, tx.Commit(ctx)
}
