package payment

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	provider "goepay/internal/adapters/payment"
	"time"
)

const ProfileIdentityLinkPurpose = "wechat_mini_identity_url_link"

// Generation is transient and exists only on the sole committed Begin winner.
// The caller must never retry an external generation after that claim.
type ProfileIdentityLink struct {
	Purpose            string                         `json:"purpose"`
	RequestID          string                         `json:"request_id"`
	Provider           string                         `json:"provider"`
	AppID              string                         `json:"app_id"`
	Status             string                         `json:"status"`
	CreatedAt          time.Time                      `json:"created_at"`
	ExpiresAt          time.Time                      `json:"expires_at"`
	RequestedExpiresAt time.Time                      `json:"requested_expires_at"`
	URLLink            string                         `json:"url_link,omitempty"`
	Generation         provider.WechatMiniLinkRequest `json:"-"`
}

type profileLinkRecord struct {
	ProfileIdentityLink
	parentProof, sourceProof, stateProof, cipher []byte
	parentCreated                                time.Time
	actorClass                                   string
	completed                                    *time.Time
}

func (s *Store) profileLinkSource(q ProfileIdentityLookup, r profileLinkRecord) []byte {
	raw, _ := json.Marshal([]any{ProfileIdentityLinkPurpose, q.ID, q.sourceProof, q.stateHash, q.profileHash, q.session.sessionHash, q.session.actorUsername, q.session.actorRole, q.session.actorID, q.session.actorVersion, q.merchantRevision, r.actorClass, r.parentCreated.UTC(), r.CreatedAt.UTC(), r.ExpiresAt.UTC(), r.RequestedExpiresAt.UTC()})
	return s.oauthHash("identity-profile-link-source-v1", string(raw))
}
func (s *Store) profileLinkState(r profileLinkRecord) []byte {
	raw, _ := json.Marshal([]any{ProfileIdentityLinkPurpose, r.RequestID, r.sourceProof, r.Status, r.cipher, r.completed})
	return s.oauthHash("identity-profile-link-state-v1", string(raw))
}
func (s *Store) profileLinkParent(ctx context.Context, tx pgx.Tx, id, token, role string, b IdentityProfileBinding) (ProfileIdentityLookup, time.Time, string, error) {
	var birth time.Time
	var class string
	if !validOAuthBrowser(id) || len(token) != 43 || b.Provider != "wxpay" || b.Kind != "mini" || !profileBindingValid(b) || s.aead == nil || len(s.indexKey) < 32 {
		return ProfileIdentityLookup{}, birth, class, ErrIdentityLookup
	}
	q, e := s.profileRow(ctx, tx, "id=$1 AND session_hash=$2 AND actor_role=$3", id, lookupSessionHash(token), role)
	if e != nil || q.Status != "pending" || q.Provider != "wxpay" || q.Kind != "mini" || !s.MatchesProfileIdentityLookup(q, b) {
		return q, birth, class, ErrIdentityLookup
	}
	if e = profileAlive(ctx, tx, &q); e != nil {
		return q, birth, class, ErrIdentityLookup
	}
	if e = tx.QueryRow(ctx, `SELECT created_at FROM profile_identity_lookup_requests WHERE id=$1`, id).Scan(&birth); e != nil {
		return q, birth, class, ErrIdentityLookup
	}
	if e = tx.QueryRow(ctx, `SELECT admin_class FROM console_users WHERE username=$1`, q.session.actorUsername).Scan(&class); e != nil {
		return q, birth, class, ErrIdentityLookup
	}
	return q, birth, class, nil
}
func (s *Store) profileLinkRow(ctx context.Context, tx pgx.Tx, q ProfileIdentityLookup, birth time.Time, class string) (profileLinkRecord, error) {
	r := profileLinkRecord{ProfileIdentityLink: ProfileIdentityLink{Purpose: ProfileIdentityLinkPurpose, RequestID: q.ID, Provider: q.Provider, AppID: q.AppID}}
	e := tx.QueryRow(ctx, `SELECT parent_proof,parent_created_at,actor_class,created_at,expires_at,requested_expires_at,source_proof,state_proof,status,link_cipher,completed_at FROM profile_identity_links WHERE request_id=$1 FOR UPDATE`, q.ID).Scan(&r.parentProof, &r.parentCreated, &r.actorClass, &r.CreatedAt, &r.ExpiresAt, &r.RequestedExpiresAt, &r.sourceProof, &r.stateProof, &r.Status, &r.cipher, &r.completed)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, e
	}
	if e != nil || r.actorClass != class || !r.parentCreated.Equal(birth) || !r.ExpiresAt.Equal(q.ExpiresAt) || !r.RequestedExpiresAt.Equal(time.Unix(q.ExpiresAt.Unix(), 0)) || subtle.ConstantTimeCompare(r.parentProof, q.sourceProof) != 1 || subtle.ConstantTimeCompare(r.sourceProof, s.profileLinkSource(q, r)) != 1 || subtle.ConstantTimeCompare(r.stateProof, s.profileLinkState(r)) != 1 {
		return r, ErrIdentityLookup
	}
	var audits bool
	e = tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM profile_identity_link_events WHERE request_id=$1 AND operation IN ('created','claimed'))=2 AND ($2='creating' OR EXISTS(SELECT 1 FROM profile_identity_link_events WHERE request_id=$1 AND operation=$2))`, q.ID, r.Status).Scan(&audits)
	if e != nil || !audits {
		return r, ErrIdentityLookup
	}
	return r, nil
}
func profileLinkEvent(ctx context.Context, tx pgx.Tx, id, op string) error {
	_, e := tx.Exec(ctx, `INSERT INTO profile_identity_link_events(request_id,operation) VALUES($1,$2)`, id, op)
	return e
}
func profileLinkWindow(ctx context.Context, tx pgx.Tx, requested time.Time) error {
	var ok bool
	if e := tx.QueryRow(ctx, `SELECT $1::timestamptz>clock_timestamp()+interval '70 seconds'`, requested).Scan(&ok); e != nil || !ok {
		return ErrIdentityLookup
	}
	return nil
}
func (s *Store) profileLinkDTO(r profileLinkRecord) (ProfileIdentityLink, error) {
	if r.Status == "ready" {
		n := s.aead.NonceSize()
		if len(r.cipher) < n {
			return ProfileIdentityLink{}, ErrIdentityLookup
		}
		plain, e := s.aead.Open(nil, r.cipher[:n], r.cipher[n:], r.sourceProof)
		if e != nil || !profileIdentityLinkURL(string(plain)) {
			return ProfileIdentityLink{}, ErrIdentityLookup
		}
		r.URLLink = string(plain)
	}
	return r.ProfileIdentityLink, nil
}

// BeginProfileIdentityLink commits the audit and irreversible send claim before
// returning send=true. Any existing intent returns send=false, without state.
func (s *Store) BeginProfileIdentityLink(ctx context.Context, id, token, role, state string, b IdentityProfileBinding) (ProfileIdentityLink, bool, error) {
	var out ProfileIdentityLink
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return out, false, e
	}
	defer tx.Rollback(ctx)
	q, birth, class, e := s.profileLinkParent(ctx, tx, id, token, role, b)
	if e != nil || !validOAuthBrowser(state) || subtle.ConstantTimeCompare(q.stateHash, s.oauthHash("identity-profile-state-v1", state)) != 1 {
		return out, false, ErrIdentityLookup
	}
	r, e := s.profileLinkRow(ctx, tx, q, birth, class)
	if e == nil {
		out, e = s.profileLinkDTO(r)
		if e != nil {
			return ProfileIdentityLink{}, false, e
		}
		if e = profileAlive(ctx, tx, &q); e != nil {
			return ProfileIdentityLink{}, false, ErrIdentityLookup
		}
		if e = tx.Commit(ctx); e != nil {
			return ProfileIdentityLink{}, false, e
		}
		return out, false, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, false, ErrIdentityLookup
	}
	r = profileLinkRecord{ProfileIdentityLink: ProfileIdentityLink{Purpose: ProfileIdentityLinkPurpose, RequestID: id, Provider: q.Provider, AppID: q.AppID, Status: "creating", ExpiresAt: q.ExpiresAt, RequestedExpiresAt: time.Unix(q.ExpiresAt.Unix(), 0).UTC()}, parentProof: q.sourceProof, parentCreated: birth, actorClass: class}
	if e = profileLinkWindow(ctx, tx, r.RequestedExpiresAt); e != nil {
		return out, false, e
	}
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&r.CreatedAt); e != nil {
		return out, false, e
	}
	if birth.After(r.CreatedAt) || q.ExpiresAt.After(birth.Add(5*time.Minute)) {
		return out, false, ErrIdentityLookup
	}
	r.sourceProof = s.profileLinkSource(q, r)
	r.stateProof = s.profileLinkState(r)
	_, e = tx.Exec(ctx, `INSERT INTO profile_identity_links(request_id,parent_proof,parent_created_at,actor_class,created_at,expires_at,requested_expires_at,source_proof,state_proof,status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'creating')`, id, r.parentProof, birth, class, r.CreatedAt, r.ExpiresAt, r.RequestedExpiresAt, r.sourceProof, r.stateProof)
	if e != nil {
		return out, false, e
	}
	for _, op := range []string{"created", "claimed"} {
		if e = profileLinkEvent(ctx, tx, id, op); e != nil {
			return out, false, e
		}
	}
	if e = profileAlive(ctx, tx, &q); e != nil {
		return out, false, ErrIdentityLookup
	}
	if e = profileLinkWindow(ctx, tx, r.RequestedExpiresAt); e != nil {
		return out, false, e
	}
	if e = tx.Commit(ctx); e != nil {
		return out, false, e
	}
	out = r.ProfileIdentityLink
	out.Generation = provider.WechatMiniLinkRequest{RequestID: id, State: state, CreatedAt: birth, Deadline: q.ExpiresAt}
	return out, true, nil
}

func (s *Store) ReadProfileIdentityLink(ctx context.Context, id, token, role string, b IdentityProfileBinding) (ProfileIdentityLink, error) {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return ProfileIdentityLink{}, e
	}
	defer tx.Rollback(ctx)
	q, birth, class, e := s.profileLinkParent(ctx, tx, id, token, role, b)
	if e != nil {
		return ProfileIdentityLink{}, ErrIdentityLookup
	}
	r, e := s.profileLinkRow(ctx, tx, q, birth, class)
	if e != nil {
		return ProfileIdentityLink{}, ErrIdentityLookup
	}
	out, e := s.profileLinkDTO(r)
	if e != nil {
		return ProfileIdentityLink{}, e
	}
	if e = profileAlive(ctx, tx, &q); e != nil {
		return ProfileIdentityLink{}, ErrIdentityLookup
	}
	if e = tx.Commit(ctx); e != nil {
		return ProfileIdentityLink{}, e
	}
	return out, nil
}

func (s *Store) finishProfileIdentityLink(ctx context.Context, id, token, role string, b IdentityProfileBinding, result *provider.WechatMiniLinkResult, status string) error {
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q, birth, class, e := s.profileLinkParent(ctx, tx, id, token, role, b)
	if e != nil {
		return ErrIdentityLookup
	}
	r, e := s.profileLinkRow(ctx, tx, q, birth, class)
	if e != nil || r.Status != "creating" {
		return ErrIdentityLookup
	}
	if result != nil {
		if result.Provider != q.Provider || result.AppID != q.AppID || result.RequestID != id || !result.RequestedExpiresAt.Equal(r.RequestedExpiresAt) || !profileIdentityLinkURL(result.URLLink) {
			return ErrIdentityLookup
		}
		nonce := make([]byte, s.aead.NonceSize())
		if _, e = rand.Read(nonce); e != nil {
			return e
		}
		r.cipher = s.aead.Seal(nonce, nonce, []byte(result.URLLink), r.sourceProof)
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	r.completed = &now
	r.Status = status
	r.stateProof = s.profileLinkState(r)
	if _, e = tx.Exec(ctx, `UPDATE profile_identity_links SET status=$2,link_cipher=$3,completed_at=$4,state_proof=$5 WHERE request_id=$1`, id, status, r.cipher, now, r.stateProof); e != nil {
		return e
	}
	if e = profileLinkEvent(ctx, tx, id, status); e != nil {
		return e
	}
	if e = profileAlive(ctx, tx, &q); e != nil {
		return ErrIdentityLookup
	}
	return tx.Commit(ctx)
}

func (s *Store) CompleteProfileIdentityLink(ctx context.Context, id, token, role string, b IdentityProfileBinding, result provider.WechatMiniLinkResult) error {
	return s.finishProfileIdentityLink(ctx, id, token, role, b, &result, "ready")
}

// Fail consumes the claim permanently; uncertain adapter outcomes stay unknown.
func (s *Store) FailProfileIdentityLink(ctx context.Context, id, token, role string, b IdentityProfileBinding, cause error) error {
	if cause == nil {
		return ErrIdentityLookup
	}
	status := "unknown"
	var unknown *provider.WechatMiniLinkUnknownError
	if !errors.As(cause, &unknown) && (errors.Is(cause, provider.ErrWechatMiniLinkRejected) || errors.Is(cause, provider.ErrWechatMiniLinkLifetime) || cause == provider.ErrWechatMiniLink || errors.Is(cause, provider.ErrInvalid)) {
		status = "failed"
	}
	return s.finishProfileIdentityLink(ctx, id, token, role, b, nil, status)
}
