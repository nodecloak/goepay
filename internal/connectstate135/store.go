// Package connectstate135 issues and consumes the quick-login OAuth state on
// connect_login_states135. The raw state and browser nonce are returned to the
// caller once; only their SHA-256 hashes are stored. Consume is single-use,
// expiry-checked, and verifies the browser nonce, so a state is useless from
// another browser or after one use.
package connectstate135

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrSource = errors.New("connect-login state source cannot be confirmed")
	ErrState  = errors.New("connect-login state invalid, expired, or already used")
)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func hash(s string) []byte { x := sha256.Sum256([]byte(s)); return x[:] }

func token() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return hex.EncodeToString(b), nil
}

type Issued struct {
	State   string // put in the provider authorize URL
	Browser string // set as the browser nonce cookie; echoed on callback
}

// Issue records a new state for (provider, purpose[, merchant]) and returns the
// raw state + browser nonce. merchant must be >0 for purpose "bind" and 0 for
// "login".
func (s *Store) Issue(ctx context.Context, provider, purpose string, merchant int64) (Issued, error) {
	if s == nil || s.pool == nil {
		return Issued{}, ErrSource
	}
	if (purpose == "bind") != (merchant > 0) {
		return Issued{}, ErrSource
	}
	state, e := token()
	if e != nil {
		return Issued{}, ErrSource
	}
	browser, e := token()
	if e != nil {
		return Issued{}, ErrSource
	}
	var mid any
	if merchant > 0 {
		mid = merchant
	}
	if _, e = s.pool.Exec(ctx,
		`INSERT INTO connect_login_states135(state_hash,provider,purpose,browser_hash,merchant_id)VALUES($1,$2,$3,$4,$5)`,
		hash(state), provider, purpose, hash(browser), mid); e != nil {
		return Issued{}, ErrSource
	}
	return Issued{State: state, Browser: browser}, nil
}

type Claim struct {
	Provider string
	Purpose  string
	Merchant int64 // 0 for login
}

// Consume validates the state against the browser nonce and marks it used. It
// succeeds at most once per state and never for an expired one.
func (s *Store) Consume(ctx context.Context, provider, state, browser string) (Claim, error) {
	if s == nil || s.pool == nil {
		return Claim{}, ErrSource
	}
	if state == "" || browser == "" {
		return Claim{}, ErrState
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Claim{}, ErrSource
	}
	defer tx.Rollback(ctx)
	var purpose string
	var browserHash []byte
	var merchant *int64
	var expired, consumed bool
	e = tx.QueryRow(ctx,
		`SELECT purpose,browser_hash,merchant_id,expires_at<clock_timestamp(),consumed_at IS NOT NULL
		 FROM connect_login_states135 WHERE state_hash=$1 AND provider=$2 FOR UPDATE`,
		hash(state), provider).Scan(&purpose, &browserHash, &merchant, &expired, &consumed)
	if errors.Is(e, pgx.ErrNoRows) {
		return Claim{}, ErrState
	}
	if e != nil {
		return Claim{}, ErrSource
	}
	if expired || consumed {
		return Claim{}, ErrState
	}
	want := hash(browser)
	if len(browserHash) != len(want) || !constantEqual(browserHash, want) {
		return Claim{}, ErrState
	}
	if _, e = tx.Exec(ctx, `UPDATE connect_login_states135 SET consumed_at=clock_timestamp()WHERE state_hash=$1`, hash(state)); e != nil {
		return Claim{}, ErrSource
	}
	out := Claim{Provider: provider, Purpose: purpose}
	if merchant != nil {
		out.Merchant = *merchant
	}
	if e = tx.Commit(ctx); e != nil {
		return Claim{}, ErrSource
	}
	return out, nil
}

func constantEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}

// IssuePending records a resolved-but-unbound provider identity (social_uid) for
// a browser, returning the raw pending-connect state the login page carries. The
// merchant then confirms with 商户ID + 密钥 to bind + log in.
func (s *Store) IssuePending(ctx context.Context, provider, socialUID, browser string) (string, error) {
	if s == nil || s.pool == nil || provider == "" || socialUID == "" || browser == "" {
		return "", ErrSource
	}
	state, e := token()
	if e != nil {
		return "", ErrSource
	}
	if _, e = s.pool.Exec(ctx,
		`INSERT INTO connect_pending136(state_hash,provider,social_uid,browser_hash)VALUES($1,$2,$3,$4)`,
		hash(state), provider, socialUID, hash(browser)); e != nil {
		return "", ErrSource
	}
	return state, nil
}

type Pending struct {
	Provider  string
	SocialUID string
}

// ConsumePending validates the pending-connect state against the browser nonce
// and marks it used (single-use), returning the provider + social_uid to bind.
func (s *Store) ConsumePending(ctx context.Context, state, browser string) (Pending, error) {
	if s == nil || s.pool == nil {
		return Pending{}, ErrSource
	}
	if state == "" || browser == "" {
		return Pending{}, ErrState
	}
	tx, e := s.pool.Begin(ctx)
	if e != nil {
		return Pending{}, ErrSource
	}
	defer tx.Rollback(ctx)
	var provider, socialUID string
	var browserHash []byte
	var expired, consumed bool
	e = tx.QueryRow(ctx,
		`SELECT provider,social_uid,browser_hash,expires_at<clock_timestamp(),consumed_at IS NOT NULL
		 FROM connect_pending136 WHERE state_hash=$1 FOR UPDATE`, hash(state)).Scan(&provider, &socialUID, &browserHash, &expired, &consumed)
	if errors.Is(e, pgx.ErrNoRows) {
		return Pending{}, ErrState
	}
	if e != nil {
		return Pending{}, ErrSource
	}
	if expired || consumed || !constantEqual(browserHash, hash(browser)) {
		return Pending{}, ErrState
	}
	if _, e = tx.Exec(ctx, `UPDATE connect_pending136 SET consumed_at=clock_timestamp()WHERE state_hash=$1`, hash(state)); e != nil {
		return Pending{}, ErrSource
	}
	if e = tx.Commit(ctx); e != nil {
		return Pending{}, ErrSource
	}
	return Pending{Provider: provider, SocialUID: socialUID}, nil
}
