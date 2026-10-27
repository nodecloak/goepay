package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"goepay/internal/identity"
)

const scanJSResource202 = "scan-js-settings202"

type ScanJSBinding202 struct {
	ProfileID          int64
	ProfileRevision    int64
	ProfileFingerprint string
}
type ScanJSSettings202 struct {
	Enabled         bool             `json:"enabled"`
	Configured      bool             `json:"configured"`
	Revision        string           `json:"revision"`
	Fingerprint     string           `json:"fingerprint"`
	ProfileID       string           `json:"profile_id"`
	ProfileRevision string           `json:"profile_revision"`
	CanUpdate       bool             `json:"can_update"`
	Binding         ScanJSBinding202 `json:"-"`
}
type scanJSFacts202 struct {
	Version   int
	Revision  int64
	Enabled   bool
	Binding   ScanJSBinding202
	ActorID   int64
	ActorName string
	ChangedAt time.Time
}
type scanJSRecord202 struct {
	Facts scanJSFacts202 `json:"facts"`
	Proof string         `json:"proof"`
}
type scanJSState202 struct {
	id     int64
	record scanJSRecord202
}

func scanJSHex202(s string) bool {
	decoded, err := hex.DecodeString(s)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == s
}
func scanJSBindingValid202(b ScanJSBinding202) bool {
	return b.ProfileID > 0 && b.ProfileRevision > 0 && scanJSHex202(b.ProfileFingerprint)
}
func (s *Store) scanJSState202(ctx context.Context, tx pgx.Tx) (scanJSState202, error) {
	var out scanJSState202
	var count, audits, latest int64
	if err := tx.QueryRow(ctx, `SELECT(SELECT count(*)FROM backoffice_records WHERE resource=$1),(SELECT count(*)FROM backoffice_audit WHERE resource=$1),(SELECT coalesce(max(resource_id),0)FROM backoffice_audit WHERE resource=$1)`, scanJSResource202).Scan(&count, &audits, &latest); err != nil {
		return out, err
	}
	if count == 0 {
		if audits != 0 || latest != 0 {
			return out, ErrConflict
		}
		out.record.Facts.Version = 1
		return out, nil
	}
	if count != 1 || audits < 1 || latest != audits {
		return out, ErrConflict
	}
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT id,body FROM backoffice_records WHERE resource=$1 FOR SHARE`, scanJSResource202).Scan(&out.id, &raw); err != nil {
		return out, ErrConflict
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&out.record) != nil {
		return out, ErrConflict
	}
	f := out.record.Facts
	if f.Version != 1 || f.Revision != latest || f.ActorID <= 0 || f.ActorName == "" || f.ChangedAt.IsZero() || f.Enabled && !scanJSBindingValid202(f.Binding) || !f.Enabled && f.Binding != (ScanJSBinding202{}) {
		return out, ErrConflict
	}
	proof, err := s.referralDigest("scan-js202/settings", []any{out.id, f})
	if err != nil || !scanJSHex202(out.record.Proof) || !hmac.Equal([]byte(hex.EncodeToString(proof)), []byte(out.record.Proof)) {
		return out, ErrConflict
	}
	commitment := sha256.Sum256(proof)
	var matching int
	if err = tx.QueryRow(ctx, `SELECT count(*)FROM backoffice_audit WHERE resource=$1 AND resource_id=$2 AND operation='scan-js-update' AND actor_role='admin' AND actor_id=$3 AND actor_name=$4 AND changes=jsonb_build_object('revision',$2::bigint::text,'commitment',$5::text)`, scanJSResource202, f.Revision, f.ActorID, f.ActorName, hex.EncodeToString(commitment[:])).Scan(&matching); err != nil || matching != 1 {
		return out, ErrConflict
	}
	return out, nil
}
func (s *Store) scanJSView202(v scanJSState202) (ScanJSSettings202, error) {
	f := v.record.Facts
	proof, err := s.referralDigest("scan-js202/confirmation", []any{v.id, v.record})
	if err != nil {
		return ScanJSSettings202{}, err
	}
	out := ScanJSSettings202{Enabled: f.Enabled, Configured: v.id > 0, Revision: strconv.FormatInt(f.Revision, 10), Fingerprint: hex.EncodeToString(proof), Binding: f.Binding}
	if f.Enabled {
		out.ProfileID = strconv.FormatInt(f.Binding.ProfileID, 10)
		out.ProfileRevision = strconv.FormatInt(f.Binding.ProfileRevision, 10)
	}
	return out, nil
}

func (s *Store) ScanJSSettings202(ctx context.Context, a ParentChannelActor) (ScanJSSettings202, error) {
	var out ScanJSSettings202
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, platformSettingsLock); err != nil {
		return out, err
	}
	if err = a.check(ctx, tx); err != nil {
		return out, err
	}
	state, err := s.scanJSState202(ctx, tx)
	if err != nil {
		return out, err
	}
	out, err = s.scanJSView202(state)
	if err != nil {
		return out, err
	}
	out.CanUpdate = true
	if err = a.check(ctx, tx); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (s *Store) UpdateScanJSSettings202(ctx context.Context, a ParentChannelActor, enabled bool, binding ScanJSBinding202, expected string) (ScanJSSettings202, error) {
	var out ScanJSSettings202
	if !scanJSHex202(expected) || enabled && !scanJSBindingValid202(binding) {
		return out, ErrInvalid
	}
	if !enabled {
		binding = ScanJSBinding202{}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, platformSettingsLock); err != nil {
		return out, err
	}
	if err = a.check(ctx, tx); err != nil {
		return out, err
	}
	before, err := s.scanJSState202(ctx, tx)
	if err != nil {
		return out, err
	}
	view, err := s.scanJSView202(before)
	if err != nil || !hmac.Equal([]byte(expected), []byte(view.Fingerprint)) {
		return out, ErrConflict
	}
	if before.record.Facts.Revision == int64(^uint64(0)>>1) {
		return out, ErrConflict
	}
	id := before.id
	if id == 0 {
		if err = tx.QueryRow(ctx, `INSERT INTO backoffice_records(resource,body)VALUES($1,'{}')RETURNING id`, scanJSResource202).Scan(&id); err != nil {
			return out, err
		}
	}
	var now time.Time
	if err = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); err != nil {
		return out, err
	}
	facts := scanJSFacts202{Version: 1, Revision: before.record.Facts.Revision + 1, Enabled: enabled, Binding: binding, ActorID: a.User.ID, ActorName: a.User.Username, ChangedAt: now.UTC()}
	proof, err := s.referralDigest("scan-js202/settings", []any{id, facts})
	if err != nil {
		return out, err
	}
	record := scanJSRecord202{Facts: facts, Proof: hex.EncodeToString(proof)}
	raw, err := json.Marshal(record)
	if err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `UPDATE backoffice_records SET body=$2::jsonb,updated_at=clock_timestamp() WHERE resource=$3 AND id=$1`, id, string(raw), scanJSResource202); err != nil {
		return out, err
	}
	commitment := sha256.Sum256(proof)
	if _, err = tx.Exec(ctx, `INSERT INTO backoffice_audit(actor_role,actor_id,actor_name,resource,resource_id,operation,changes)VALUES('admin',$1,$2,$3,$4,'scan-js-update',jsonb_build_object('revision',$4::bigint::text,'commitment',$5::text))`, a.User.ID, a.User.Username, scanJSResource202, facts.Revision, hex.EncodeToString(commitment[:])); err != nil {
		return out, err
	}
	after, err := s.scanJSState202(ctx, tx)
	if err != nil {
		return out, err
	}
	out, err = s.scanJSView202(after)
	if err != nil {
		return out, err
	}
	out.CanUpdate = true
	if err = a.check(ctx, tx); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

// Read-only, durable owner authorization. The final check runs after acquiring
// settings locks so a wait cannot revive an expired or delegated session.
func (s *Store) MerchantScanJSSettings202(ctx context.Context, a MerchantRSAActor) (ScanJSSettings202, error) {
	var out ScanJSSettings202
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock_shared($1)`, platformSettingsLock); err != nil {
		return out, err
	}
	var enabled bool
	if err = tx.QueryRow(ctx, `SELECT enabled AND pay=1 FROM merchants WHERE id=$1 FOR SHARE`, a.User.ID).Scan(&enabled); err != nil || !enabled {
		return out, identity.ErrOriginalMerchantSession
	}
	if err = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); err != nil {
		return out, err
	}
	state, err := s.scanJSState202(ctx, tx)
	if err != nil {
		return out, err
	}
	out, err = s.scanJSView202(state)
	if err != nil {
		return out, err
	}
	if err = identity.CheckOriginalMerchantSessionTx(ctx, tx, a.User, a.Token, a.CSRF); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return out, nil
}
